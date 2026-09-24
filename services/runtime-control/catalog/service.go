// Package catalog owns admitted immutable Runtime Releases and selection policy.
package catalog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

type Service struct {
	api.UnimplementedRuntimeControlProductServiceServer
	DB         *sql.DB
	Store      *ownerstore.Store
	Auth       *ownerservice.Authorizer
	Capability api.CapabilityProductServiceClient
	Schema     *jsonschema.Schema
}

func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("RuntimeControlProductService", func(g *grpc.Server) {
		api.RegisterRuntimeControlProductServiceServer(g, s)
	})
}

func New(db *sql.DB, a *ownerservice.Authorizer, capability api.CapabilityProductServiceClient, schemaPath, schemaDigest string) (*Service, error) {
	if db == nil || a == nil || capability == nil {
		return nil, fmt.Errorf("database, authorization and publisher catalog are required")
	}
	b, e := os.ReadFile(schemaPath)
	if e != nil {
		return nil, e
	}
	if digest(b) != schemaDigest {
		return nil, fmt.Errorf("publisher schema digest mismatch")
	}
	var raw any
	if e = json.Unmarshal(b, &raw); e != nil {
		return nil, e
	}
	c := jsonschema.NewCompiler()
	if e = c.AddResource("publisher.json", raw); e != nil {
		return nil, e
	}
	schema, e := c.Compile("publisher.json")
	if e != nil {
		return nil, e
	}
	store, e := ownerstore.New(db, "runtime_control")
	if e != nil {
		return nil, e
	}
	return &Service{DB: db, Auth: a, Capability: capability, Schema: schema, Store: store}, nil
}
func id(prefix string) string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func dbError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "catalog entry not found")
	}
	var p *pq.Error
	if errors.As(e, &p) && p.Code == "23505" {
		return status.Error(codes.AlreadyExists, "catalog identity already exists")
	}
	return status.Error(codes.Internal, "runtime catalog persistence failed")
}
func (s *Service) auth(ctx context.Context, c *api.CallContext, action, id string, admin bool) error {
	scope := ownerservice.ResourceScope{}
	if !admin {
		scope.TenantID = c.GetScope().GetTenant().GetTenantId()
	}
	r := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	if id != "" {
		r.Id = &id
	}
	return s.Auth.Authorize(ctx, c, api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+strings.ToUpper(action)]), r, scope)
}
func wire(v proto.Message) []byte { b, _ := protojson.Marshal(v); return b }
func (s *Service) command(ctx context.Context, c *api.CallContext, name string, request, response proto.Message, run func(*sql.Tx) error) error {
	if c.GetIdempotencyKey() == "" {
		return status.Error(codes.InvalidArgument, "idempotency key required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return dbError(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, c.ActorId+":"+name+":"+c.IdempotencyKey); e != nil {
		return dbError(e)
	}
	in := ownerstore.IdempotencyInput{ID: id("idem"), TenantScope: "platform", ActorScope: c.ActorId, OperationName: name, IdempotencyKey: c.IdempotencyKey, RequestSHA256: strings.TrimPrefix(digest(wire(request)), "sha256:")}
	old, found, e := s.Store.LookupIdempotency(ctx, tx, in)
	if errors.Is(e, ownerstore.ErrIdempotencyConflict) {
		return status.Error(codes.AlreadyExists, "idempotency input conflict")
	}
	if e != nil {
		return dbError(e)
	}
	if found {
		return protojson.Unmarshal(old.ResponseBody, response)
	}
	if e = run(tx); e != nil {
		return e
	}
	in.ResourceID = "catalog"
	in.ResponseStatus = 200
	in.ResponseBody = wire(response)
	if e = s.Store.RecordIdempotency(ctx, tx, in); e != nil {
		return dbError(e)
	}
	return dbError(tx.Commit())
}
func (s *Service) approvedPublisher(ctx context.Context, c *api.CallContext, namespace, repository string) error {
	cursor := ""
	for {
		p, e := s.Capability.ListPublisherNamespaces(ctx, &api.ListPublisherNamespacesRpcRequest{Context: c, QueryCursor: &cursor})
		if e != nil {
			return e
		}
		for _, n := range p.Items {
			if n.Id == namespace {
				if n.Status != api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED || (!strings.HasPrefix(repository, n.RepositoryPrefix+"/") && repository != n.RepositoryPrefix) {
					return status.Error(codes.PermissionDenied, "publisher repository is not admitted")
				}
				return nil
			}
		}
		if p.GetNextCursor() == "" {
			break
		}
		cursor = p.GetNextCursor()
	}
	return status.Error(codes.NotFound, "publisher namespace not found")
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (s *Service) RegisterRuntimeVersion(ctx context.Context, r *api.RegisterRuntimeVersionRpcRequest) (*api.RuntimeVersion, error) {
	if e := s.auth(ctx, r.GetContext(), "RegisterRuntimeVersion", "", true); e != nil {
		return nil, e
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetName()) || !nameRE.MatchString(b.GetVersionLabel()) || b.GetAdmissionReceiptId() == "" || b.GetPublisherContract() == nil || b.PublisherNamespaceId != b.PublisherContract.PublisherNamespaceId {
		return nil, status.Error(codes.InvalidArgument, "invalid runtime admission")
	}
	raw, e := protojson.Marshal(b.PublisherContract)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid publisher contract")
	}
	var v any
	if e = json.Unmarshal(raw, &v); e != nil {
		return nil, e
	}
	if e = s.Schema.Validate(v); e != nil {
		return nil, status.Error(codes.InvalidArgument, "publisher contract fails approved schema")
	}
	if e = s.approvedPublisher(ctx, r.Context, b.PublisherNamespaceId, b.PublisherContract.GetImage().GetRepository()); e != nil {
		return nil, e
	}
	out := &api.RuntimeVersion{}
	e = s.command(ctx, r.Context, "RegisterRuntimeVersion", b, out, func(tx *sql.Tx) error {
		out.Id = id("runtime")
		out.Name = b.Name
		out.VersionLabel = b.VersionLabel
		out.ArtifactDigest = b.PublisherContract.Image.Digest
		out.Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED
		out.RuntimeAbiVersion = b.PublisherContract.RuntimeAbiVersion
		out.PackageFormatVersions = b.PublisherContract.PackageFormatVersions
		out.AdmissionReceiptId = b.AdmissionReceiptId
		out.PublisherNamespaceId = b.PublisherNamespaceId
		out.PublisherContract = b.PublisherContract
		out.PublisherContractDigest = digest(raw)
		out.PublisherContractObjectRef = "runtime-contract:" + out.Id + "@" + out.PublisherContractDigest
		out.CreatedAt = timestamppb.Now()
		_, e := tx.ExecContext(ctx, `INSERT INTO runtime_control.runtime_releases(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_version,package_format_versions,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, out.Id, out.Name, out.VersionLabel, b.PublisherContract.Image.Repository, out.ArtifactDigest, r.Context.ActorId, out.RuntimeAbiVersion, pq.Array(out.PackageFormatVersions), out.AdmissionReceiptId, out.PublisherNamespaceId, out.PublisherContractDigest, raw, out.PublisherContractObjectRef, out.CreatedAt.AsTime())
		return dbError(e)
	})
	return out, e
}

const cols = `r.id,r.name,r.version_label,r.artifact_digest,r.status,r.runtime_abi_version,r.package_format_versions,r.admission_receipt_id,r.created_at,r.publisher_namespace_id,r.publisher_contract_digest,r.publisher_contract,r.publisher_contract_object_ref,COALESCE(r.id=(SELECT runtime_version_id FROM runtime_control.catalog_policies WHERE effective_at<=now() ORDER BY effective_at DESC,id DESC LIMIT 1),false)`

type scanner interface{ Scan(...any) error }

func scanRuntime(row scanner) (*api.RuntimeVersion, error) {
	v := &api.RuntimeVersion{}
	var st string
	var raw []byte
	var created time.Time
	e := row.Scan(&v.Id, &v.Name, &v.VersionLabel, &v.ArtifactDigest, &st, &v.RuntimeAbiVersion, pq.Array(&v.PackageFormatVersions), &v.AdmissionReceiptId, &created, &v.PublisherNamespaceId, &v.PublisherContractDigest, &raw, &v.PublisherContractObjectRef, &v.DefaultForNewBuilds)
	if e != nil {
		return nil, dbError(e)
	}
	v.Status = api.RuntimeVersionStatusEnum(api.RuntimeVersionStatusEnum_value["RUNTIME_VERSION_STATUS_ENUM_"+strings.ToUpper(st)])
	v.CreatedAt = timestamppb.New(created)
	v.PublisherContract = &api.RuntimePublisherContract{}
	if e = protojson.Unmarshal(raw, v.PublisherContract); e != nil {
		return nil, status.Error(codes.DataLoss, "stored publisher contract invalid")
	}
	return v, nil
}
func (s *Service) ListRuntimeVersions(ctx context.Context, r *api.ListRuntimeVersionsRpcRequest) (*api.RuntimeVersionPage, error) {
	if e := s.auth(ctx, r.GetContext(), "ListRuntimeVersions", "", false); e != nil {
		return nil, e
	}
	n := int(r.GetQueryLimit())
	if n <= 0 || n > 100 {
		n = 50
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM runtime_control.runtime_releases r WHERE r.id>$1 ORDER BY r.id LIMIT $2`, r.GetQueryCursor(), n+1)
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	out := &api.RuntimeVersionPage{}
	for rows.Next() {
		v, e := scanRuntime(rows)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		cursor := out.Items[n-1].Id
		out.NextCursor = &cursor
	}
	return out, dbError(rows.Err())
}
func (s *Service) SetRuntimeVersionStatus(ctx context.Context, r *api.SetRuntimeVersionStatusRpcRequest) (*api.RuntimeVersion, error) {
	if e := s.auth(ctx, r.GetContext(), "SetRuntimeVersionStatus", r.VersionId, true); e != nil {
		return nil, e
	}
	st := strings.ToLower(strings.TrimPrefix(r.GetBody().GetStatus().String(), "CATALOG_STATUS_REQUEST_STATUS_ENUM_"))
	if st != "approved" && st != "deprecated" && st != "revoked" {
		return nil, status.Error(codes.InvalidArgument, "invalid status")
	}
	if strings.TrimSpace(r.GetBody().GetReason()) == "" {
		return nil, status.Error(codes.InvalidArgument, "reason required")
	}
	out := &api.RuntimeVersion{}
	e := s.command(ctx, r.Context, "SetRuntimeVersionStatus", &api.SetRuntimeVersionStatusRpcRequest{Body: r.Body, VersionId: r.VersionId}, out, func(tx *sql.Tx) error {
		v, e := scanRuntime(tx.QueryRowContext(ctx, `UPDATE runtime_control.runtime_releases r SET status=$1,updated_at=now() WHERE r.id=$2 RETURNING `+cols, st, r.VersionId))
		if e == nil {
			proto.Merge(out, v)
		}
		return e
	})
	return out, e
}
func scanPolicy(r scanner) (*api.BuildRuntimePolicy, error) {
	v := &api.BuildRuntimePolicy{}
	var webui string
	var effective, created time.Time
	e := r.Scan(&v.Id, &v.RuntimeVersionId, &webui, &v.PolicyVersion, &effective, &created)
	if webui != "" {
		v.DefaultWebuiVersionId = &webui
	}
	v.EffectiveAt = timestamppb.New(effective)
	v.CreatedAt = timestamppb.New(created)
	return v, dbError(e)
}
func (s *Service) GetBuildRuntimePolicy(ctx context.Context, r *api.GetBuildRuntimePolicyRpcRequest) (*api.BuildRuntimePolicy, error) {
	if e := s.auth(ctx, r.GetContext(), "GetBuildRuntimePolicy", "", false); e != nil {
		return nil, e
	}
	return scanPolicy(s.DB.QueryRowContext(ctx, `SELECT id,runtime_version_id,default_webui_version_id,policy_version,effective_at,created_at FROM runtime_control.catalog_policies WHERE effective_at<=now() ORDER BY effective_at DESC,id DESC LIMIT 1`))
}
func (s *Service) SetBuildRuntimePolicy(ctx context.Context, r *api.SetBuildRuntimePolicyRpcRequest) (*api.BuildRuntimePolicy, error) {
	if e := s.auth(ctx, r.GetContext(), "SetBuildRuntimePolicy", "", true); e != nil {
		return nil, e
	}
	b := r.GetBody()
	if b.GetRuntimeVersionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime version required")
	}
	if b.GetDefaultWebuiVersionId() != "" {
		cursor := ""
		found := false
		for {
			p, e := s.Capability.ListWebuiVersions(ctx, &api.ListWebuiVersionsRpcRequest{Context: r.Context, QueryCursor: &cursor})
			if e != nil {
				return nil, e
			}
			for _, v := range p.Items {
				if v.Id == b.GetDefaultWebuiVersionId() && v.Status == api.WebuiVersionStatusEnum_WEBUI_VERSION_STATUS_ENUM_APPROVED {
					found = true
				}
			}
			if p.GetNextCursor() == "" {
				break
			}
			cursor = p.GetNextCursor()
		}
		if !found {
			return nil, status.Error(codes.FailedPrecondition, "default WebUI is not approved")
		}
	}
	out := &api.BuildRuntimePolicy{}
	e := s.command(ctx, r.Context, "SetBuildRuntimePolicy", b, out, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('runtime_catalog_policy',0))`); e != nil {
			return dbError(e)
		}
		var current string
		e := tx.QueryRowContext(ctx, `SELECT policy_version FROM runtime_control.catalog_policies ORDER BY effective_at DESC,id DESC LIMIT 1`).Scan(&current)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return dbError(e)
		}
		if b.GetExpectedPolicyVersionId() != current {
			return status.Error(codes.Aborted, "runtime policy changed")
		}
		var st string
		if e = tx.QueryRowContext(ctx, `SELECT status FROM runtime_control.runtime_releases WHERE id=$1 FOR SHARE`, b.RuntimeVersionId).Scan(&st); e != nil {
			return dbError(e)
		}
		if st != "approved" {
			return status.Error(codes.FailedPrecondition, "runtime release not approved")
		}
		v, e := scanPolicy(tx.QueryRowContext(ctx, `INSERT INTO runtime_control.catalog_policies(id,runtime_version_id,default_webui_version_id,policy_version,published_by,effective_at) VALUES($1,$2,$3,$4,$5,now()) RETURNING id,runtime_version_id,default_webui_version_id,policy_version,effective_at,created_at`, id("policy"), b.RuntimeVersionId, b.GetDefaultWebuiVersionId(), id("policyversion"), r.Context.ActorId))
		if e == nil {
			proto.Merge(out, v)
		}
		return e
	})
	return out, e
}
