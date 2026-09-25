// Package build owns durable Package-to-OCI jobs. Only Capability registers a
// deployable version; Build never treats an exporter acknowledgement as success.
package build

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

type Service struct {
	api.UnimplementedBuildProductServiceServer
	api.UnimplementedBuildCoordinationServer
	api.UnimplementedClaimUsageReadbackServer
	api.UnimplementedOwnerCommitReadbackServer
	api.UnimplementedDomainInboxServer
	store      *ownerstore.Store
	auth       *ownerservice.Authorizer
	capability api.CapabilityCoordinationClient
	versions   api.CapabilityProductServiceClient
	inboxes    map[string]api.DomainInboxClient
	identity   api.CloudIdentityAuthorizationClient
	runner     *Runner
}

func New(store *ownerstore.Store, auth *ownerservice.Authorizer, capability api.CapabilityCoordinationClient, versions api.CapabilityProductServiceClient, inboxes map[string]api.DomainInboxClient, identity api.CloudIdentityAuthorizationClient, runner *Runner) (*Service, error) {
	if store == nil || store.Schema() != "build" || capability == nil || versions == nil || runner == nil {
		return nil, errors.New("build store, Capability clients and isolated runner are required")
	}
	return &Service{store: store, auth: auth, capability: capability, versions: versions, inboxes: inboxes, identity: identity, runner: runner}, nil
}
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("BuildProductService", func(g *grpc.Server) {
		api.RegisterBuildProductServiceServer(g, s)
		api.RegisterBuildCoordinationServer(g, s)
		api.RegisterClaimUsageReadbackServer(g, s)
		api.RegisterOwnerCommitReadbackServer(g, s)
		api.RegisterDomainInboxServer(g, s)
	})
}
func id(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}
func tenant(c *api.CallContext) string { return c.GetScope().GetTenant().GetTenantId() }
func resource(kind api.AuthorizationResourceKind, value string) *api.AuthorizationResource {
	r := &api.AuthorizationResource{Kind: kind}
	if value != "" {
		r.Id = &value
	}
	return r
}
func (s *Service) authorize(ctx context.Context, c *api.CallContext, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind, value, tenantID string) error {
	return s.auth.Authorize(ctx, c, action, resource(kind, value), ownerservice.ResourceScope{TenantID: tenantID})
}
func wire(m proto.Message) []byte {
	b, err := protojson.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
func databaseError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "build not found")
	}
	return status.Error(codes.Unavailable, "build persistence is unavailable")
}

type record struct {
	Job                     *api.BuildJob
	Input                   *api.BuildInputSnapshot
	Call                    *api.CallContext
	Tenant, Actor, Executor string
	Created                 time.Time
}

const columns = `id,operation_id,package_version_id,runtime_version_id,webui_version_id,status,stage,COALESCE(artifact_digest,''),COALESCE(result_capability_version_id,''),COALESCE(retry_of_build_job_id,''),COALESCE(error_code,''),created_at,updated_at,input_snapshot,call_context,COALESCE(tenant_id,''),created_by,COALESCE(executor_ref,'')`

func scan(row interface{ Scan(...any) error }) (*record, error) {
	r := &record{Job: &api.BuildJob{}, Input: &api.BuildInputSnapshot{}, Call: &api.CallContext{}}
	var st, artifact, version, retry, ec string
	var created, updated time.Time
	var input, call []byte
	err := row.Scan(&r.Job.Id, &r.Job.OperationId, &r.Job.PackageVersionId, &r.Job.RuntimeVersionId, &r.Job.WebuiVersionId, &st, &r.Job.Stage, &artifact, &version, &retry, &ec, &created, &updated, &input, &call, &r.Tenant, &r.Actor, &r.Executor)
	if err != nil {
		return nil, err
	}
	if err = protojson.Unmarshal(input, r.Input); err != nil {
		return nil, err
	}
	if err = protojson.Unmarshal(call, r.Call); err != nil {
		return nil, err
	}
	r.Job.Status = api.BuildJobStatusEnum(api.BuildJobStatusEnum_value["BUILD_JOB_STATUS_ENUM_"+strings.ToUpper(st)])
	r.Job.CreatedAt = timestamppb.New(created)
	r.Job.UpdatedAt = timestamppb.New(updated)
	r.Created = created
	if artifact != "" {
		r.Job.ArtifactDigest = &artifact
	}
	if version != "" {
		r.Job.ResultCapabilityVersionId = &version
	}
	if retry != "" {
		r.Job.RetryOfBuildJobId = &retry
	}
	if ec != "" {
		v := api.ErrorCodeEnum(api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(ec)])
		r.Job.ErrorCode = &v
	}
	r.Job.RetryAllowed = st == "failed"
	for _, claim := range []string{r.Input.PackageClaimId, r.Input.RuntimeClaimId, r.Input.WebuiClaimId} {
		if claim != "" {
			r.Job.InputClaimIds = append(r.Job.InputClaimIds, claim)
		}
	}
	return r, nil
}
func (s *Service) read(ctx context.Context, buildID string) (*record, error) {
	r, err := scan(s.store.DB().QueryRowContext(ctx, `SELECT `+columns+` FROM build.build_jobs WHERE id=$1`, buildID))
	if err != nil {
		return nil, databaseError(err)
	}
	return r, nil
}
func (s *Service) CreateBuild(ctx context.Context, req *api.CreateBuildRpcRequest) (*api.BuildJob, error) {
	return s.create(ctx, req.GetContext(), req.GetBody(), "")
}
func (s *Service) create(ctx context.Context, call *api.CallContext, body *api.CreateBuildRequest, retry string) (*api.BuildJob, error) {
	if call.GetIdempotencyKey() == "" || body.GetPackageVersionId() == "" || body.GetWebuiVersionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency key, package version and WebUI version are required")
	}
	if tenant(call) == "" {
		return nil, status.Error(codes.PermissionDenied, "Build requires tenant scope")
	}
	if err := s.authorize(ctx, call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, body.PackageVersionId, tenant(call)); err != nil {
		return nil, err
	}
	normalized, _ := json.Marshal(struct{ Package, WebUI, Retry string }{body.PackageVersionId, body.WebuiVersionId, retry})
	operationName := "createBuild"
	if retry != "" {
		operationName = "retryBuild"
	}
	idem := ownerstore.IdempotencyInput{ID: id("idem_"), TenantScope: tenant(call), ActorScope: call.ActorId, OperationName: operationName, IdempotencyKey: call.IdempotencyKey, RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 201}
	// Serialize this exact intent, including owner readback, so concurrent requests
	// cannot resolve two catalog snapshots or allocate duplicate jobs.
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, databaseError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, idem.TenantScope+"/"+idem.ActorScope+"/"+operationName+"/"+idem.IdempotencyKey); err != nil {
		return nil, databaseError(err)
	}
	replay, found, err := s.store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return nil, databaseError(err)
	}
	if found {
		r, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM build.build_jobs WHERE id=$1`, replay.ResourceID))
		if err != nil {
			return nil, databaseError(err)
		}
		return r.Job, nil
	}
	input, err := s.capability.ResolveBuildInput(ctx, &api.BuildInputRequest{Context: call, PackageVersionId: body.PackageVersionId, WebuiVersionId: body.WebuiVersionId})
	if err != nil {
		return nil, err
	}
	if input.PackageVersionId != body.PackageVersionId || input.WebuiVersionId != body.WebuiVersionId {
		return nil, status.Error(codes.FailedPrecondition, "Capability input identity mismatch")
	}
	if err = s.runner.ValidateInput(input); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	jobID, opID := id("build_"), id("op_")
	now := time.Now().UTC()
	job := &api.BuildJob{Id: jobID, OperationId: opID, PackageVersionId: input.PackageVersionId, RuntimeVersionId: input.RuntimeVersionId, WebuiVersionId: input.WebuiVersionId, Status: api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_QUEUED, Stage: "queued", CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
	if retry != "" {
		job.RetryOfBuildJobId = &retry
	}
	_, err = s.store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: opID, TenantID: tenant(call), ActorID: call.ActorId, Kind: "build", ResourceID: jobID, Stage: "queued", RequestID: call.RequestId, AcceptedInput: wire(input)})
	if err != nil {
		return nil, databaseError(err)
	}
	var retryID any
	if retry != "" {
		retryID = retry
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO build.build_jobs(id,tenant_id,package_version_id,runtime_version_id,webui_version_id,input_digest,input_snapshot,status,stage,retry_of_build_job_id,request_id,created_by,operation_id,catalog_policy_id,call_context,executor_ref) VALUES($1,$2,$3,$4,$5,$6,$7,'queued','queued',$8,$9,$10,$11,$12,$13,$14)`, jobID, tenant(call), input.PackageVersionId, input.RuntimeVersionId, input.WebuiVersionId, input.SnapshotDigest, wire(input), retryID, call.RequestId, call.ActorId, opID, input.RuntimeVersionId, wire(call), s.runner.Repository(tenant(call), input.PackageId)+":"+jobID)
	if err != nil {
		return nil, databaseError(err)
	}
	idem.ResourceID = jobID
	idem.OperationID = opID
	idem.ResponseBody = wire(job)
	if err = s.store.RecordIdempotency(ctx, tx, idem); err != nil {
		return nil, databaseError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, databaseError(err)
	}
	return job, nil
}
func (s *Service) GetBuild(ctx context.Context, req *api.GetBuildRpcRequest) (*api.BuildJob, error) {
	r, err := s.read(ctx, req.GetBuildId())
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, req.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, r.Job.Id, r.Tenant); err != nil {
		return nil, err
	}
	return r.Job, nil
}
func limit(v int32) int32 {
	if v <= 0 {
		return 50
	}
	if v > 200 {
		return 200
	}
	return v
}
func (s *Service) ListBuilds(ctx context.Context, req *api.ListBuildsRpcRequest) (*api.BuildJobPage, error) {
	t := tenant(req.GetContext())
	if t == "" {
		return nil, status.Error(codes.PermissionDenied, "tenant scope is required")
	}
	if err := s.authorize(ctx, req.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTBUILDS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, t, t); err != nil {
		return nil, err
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT `+columns+` FROM build.build_jobs WHERE tenant_id=$1 AND ($2='' OR package_version_id=$2) AND ($3='' OR id<$3) ORDER BY id DESC LIMIT $4`, t, req.GetQueryPackageVersionId(), req.GetQueryCursor(), limit(req.GetQueryLimit())+1)
	if err != nil {
		return nil, databaseError(err)
	}
	defer rows.Close()
	page := &api.BuildJobPage{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, databaseError(err)
		}
		if len(page.Items) == int(limit(req.GetQueryLimit())) {
			cursor := page.Items[len(page.Items)-1].Id
			page.NextCursor = &cursor
			break
		}
		page.Items = append(page.Items, r.Job)
	}
	return page, rows.Err()
}
func (s *Service) ListBuildLogs(ctx context.Context, req *api.ListBuildLogsRpcRequest) (*api.BuildLogPage, error) {
	r, err := s.read(ctx, req.GetBuildId())
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, req.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTBUILDLOGS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, r.Job.Id, r.Tenant); err != nil {
		return nil, err
	}
	after := int64(-1)
	if req.GetQueryCursor() != "" {
		after, err = strconv.ParseInt(req.GetQueryCursor(), 10, 64)
		if err != nil || after < 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid log cursor")
		}
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id,sequence,stage,level,message,created_at FROM build.build_logs WHERE build_job_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`, r.Job.Id, after, limit(req.GetQueryLimit())+1)
	if err != nil {
		return nil, databaseError(err)
	}
	defer rows.Close()
	page := &api.BuildLogPage{}
	for rows.Next() {
		l := &api.BuildLog{BuildJobId: r.Job.Id}
		var level string
		var at time.Time
		if err = rows.Scan(&l.Id, &l.Sequence, &l.Stage, &level, &l.Message, &at); err != nil {
			return nil, databaseError(err)
		}
		if len(page.Items) == int(limit(req.GetQueryLimit())) {
			c := strconv.FormatInt(page.Items[len(page.Items)-1].Sequence, 10)
			page.NextCursor = &c
			break
		}
		l.Level = api.BuildLogLevelEnum(api.BuildLogLevelEnum_value["BUILD_LOG_LEVEL_ENUM_"+strings.ToUpper(level)])
		l.CreatedAt = timestamppb.New(at)
		page.Items = append(page.Items, l)
	}
	return page, rows.Err()
}
func (s *Service) RetryBuild(ctx context.Context, req *api.RetryBuildRpcRequest) (*api.BuildJob, error) {
	r, err := s.read(ctx, req.GetBuildId())
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, req.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETRYBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, r.Job.Id, r.Tenant); err != nil {
		return nil, err
	}
	if !r.Job.RetryAllowed {
		return nil, status.Error(codes.FailedPrecondition, "only a definitively failed job can be retried")
	}
	return s.create(ctx, req.GetContext(), &api.CreateBuildRequest{PackageVersionId: r.Job.PackageVersionId, WebuiVersionId: r.Job.WebuiVersionId}, r.Job.Id)
}
func requirePeer(ctx context.Context, peers ...owneridentity.Owner) error {
	p, ok := ownerservice.PeerOwner(ctx)
	if ok {
		for _, allowed := range peers {
			if p == allowed.Service() {
				return nil
			}
		}
	}
	return status.Error(codes.PermissionDenied, "calling service is not allowed for this coordination method")
}
func (s *Service) ReadOwnerCommit(ctx context.Context, req *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	if err := requirePeer(ctx, owneridentity.Tenant, owneridentity.Capability); err != nil {
		return nil, err
	}
	r, err := s.read(ctx, req.GetResourceId())
	if err != nil {
		return nil, err
	}
	if req.GetOwner() != api.OwnerEnum_OWNER_ENUM_BUILD || req.GetOperationId() != r.Job.OperationId {
		return nil, status.Error(codes.FailedPrecondition, "owner commit identity mismatch")
	}
	return evidence(r), nil
}
func evidence(r *record) *api.OwnerCommitEvidence {
	return &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_BUILD, OperationId: r.Job.OperationId, ResourceId: r.Job.Id, AcceptedInputDigest: r.Input.SnapshotDigest, CommittedVersion: 1, AcceptedAt: r.Job.CreatedAt, AuthorizationContextId: r.Call.AuthorizationContextId, ActorId: r.Actor, Scope: r.Call.Scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, AuthorizationResource: resource(api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.Input.PackageVersionId), ContinuationResources: []*api.AuthorizationResource{resource(api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.Input.PackageVersionId), resource(api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.Input.RuntimeVersionId), resource(api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.Input.WebuiVersionId)}}
}
func (s *Service) ReadClaimUsage(ctx context.Context, req *api.ReadClaimUsageRequest) (*api.ClaimUsageEvidence, error) {
	if err := requirePeer(ctx, owneridentity.Capability); err != nil {
		return nil, err
	}
	r, err := s.read(ctx, req.GetClaimantResourceId())
	if err != nil {
		return nil, err
	}
	if req.GetClaimantOperationId() != r.Job.OperationId {
		return nil, status.Error(codes.FailedPrecondition, "claim operation mismatch")
	}
	found := false
	for _, v := range r.Job.InputClaimIds {
		found = found || v == req.ClaimId
	}
	if !found {
		return nil, status.Error(codes.NotFound, "claim is not committed by this job")
	}
	active := r.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_FAILED && r.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_SUCCEEDED
	st := api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING
	if !active {
		st = api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED
		if r.Job.Status == api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_SUCCEEDED {
			st = api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED
		}
	}
	receipt := r.Job.OperationId
	return &api.ClaimUsageEvidence{ClaimId: req.ClaimId, Owner: api.OwnerEnum_OWNER_ENUM_BUILD, ResourceId: r.Job.Id, OperationId: r.Job.OperationId, AcceptedInputDigest: r.Input.SnapshotDigest, OperationStatus: st, ActivelyRequired: active, CommittedVersion: 1, TerminalReceiptId: &receipt, Outcome: api.Observation_OBSERVATION_CONFIRMED, ObservedAt: timestamppb.Now()}, nil
}

var _ = fmt.Sprintf
