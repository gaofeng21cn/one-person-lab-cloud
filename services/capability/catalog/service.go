// Package catalog owns Package metadata, immutable upload bytes and input claims.
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
	"regexp"
	"strings"

	"github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

type AuthorizeFunc func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error

type Service struct {
	api.UnimplementedCapabilityProductServiceServer
	api.UnimplementedCapabilityCoordinationServer
	api.UnimplementedDomainInboxServer
	DB         *sql.DB
	Store      *ownerstore.Store
	Authorize  AuthorizeFunc
	Runtime    api.RuntimeControlProductServiceClient
	Build      api.BuildCoordinationClient
	Usage      api.ClaimUsageReadbackClient
	Commit     api.OwnerCommitReadbackClient
	BuildInbox api.DomainInboxClient
	Objects    *Objects
}

// Register exposes the complete Capability owner surface behind the shared
// identity interceptor. The command entrypoint supplies only owner-local
// dependencies and peer clients; no other service writes Capability state.
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("CapabilityProductService", func(g *grpc.Server) {
		api.RegisterCapabilityProductServiceServer(g, s)
		api.RegisterCapabilityCoordinationServer(g, s)
		api.RegisterDomainInboxServer(g, s)
	})
}

func New(db *sql.DB, authorize AuthorizeFunc, objects *Objects) (*Service, error) {
	if db == nil || authorize == nil || objects == nil {
		return nil, fmt.Errorf("database, live authorization and object adapter are required")
	}
	store, err := ownerstore.New(db, "capability")
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Store: store, Authorize: authorize, Objects: objects}, nil
}
func (s *Service) auth(ctx context.Context, c *api.CallContext, action string, kind api.AuthorizationResourceKind, id string) error {
	if c == nil || c.GetActorId() == "" || c.GetRequestId() == "" || c.GetScope() == nil {
		return status.Error(codes.Unauthenticated, "authenticated request context is required")
	}
	if e := ownerservice.ValidateCallContext(ctx, c); e != nil {
		return e
	}
	scope := ownerservice.ResourceScope{TenantID: tenant(c)}
	if id != "" && (kind == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE || kind == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION) {
		var tid sql.NullString
		e := s.DB.QueryRowContext(ctx, `SELECT n.tenant_id FROM capability.namespaces n JOIN capability.packages p ON p.namespace_id=n.id WHERE p.id=$1 UNION ALL SELECT n.tenant_id FROM capability.namespaces n JOIN capability.packages p ON p.namespace_id=n.id JOIN capability.package_versions v ON v.package_id=p.id WHERE v.id=$1 UNION ALL SELECT n.tenant_id FROM capability.namespaces n JOIN capability.packages p ON p.namespace_id=n.id JOIN capability.package_versions v ON v.package_id=p.id JOIN capability.upload_sessions u ON u.package_version_id=v.id WHERE u.id=$1 UNION ALL SELECT n.tenant_id FROM capability.namespaces n JOIN capability.packages p ON p.namespace_id=n.id JOIN capability.capability_versions v ON v.package_id=p.id WHERE v.id=$1 LIMIT 1`, id).Scan(&tid)
		if e != nil {
			return dbError(e)
		}
		if tid.Valid {
			scope.TenantID = tid.String
		}
	}
	v, ok := api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+strings.ToUpper(action)]
	if !ok {
		return status.Error(codes.Internal, "unknown authorization action")
	}
	r := &api.AuthorizationResource{Kind: kind}
	if id != "" {
		r.Id = &id
	}
	return s.Authorize(ctx, c, api.AuthorizationActionEnum(v), r, scope)
}
func tenant(c *api.CallContext) string { return c.GetScope().GetTenant().GetTenantId() }
func id(prefix string) string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "resource not found")
	}
	var p *pq.Error
	if errors.As(err, &p) && p.Code == "23505" {
		return status.Error(codes.AlreadyExists, "immutable identity already exists")
	}
	return status.Error(codes.Internal, "owner persistence failed")
}
func limit(n int32) int {
	if n <= 0 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return int(n)
}
func jsonBytes(v proto.Message) []byte { b, _ := protojson.Marshal(v); return b }
func (s *Service) command(ctx context.Context, c *api.CallContext, name string, request, response proto.Message, run func(*sql.Tx) error) error {
	if c.GetIdempotencyKey() == "" {
		return status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return dbError(e)
	}
	defer tx.Rollback()
	scope := tenant(c)
	if scope == "" {
		scope = "platform"
	}
	key, _ := json.Marshal([]string{scope, c.ActorId, name, c.IdempotencyKey})
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(key)); e != nil {
		return dbError(e)
	}
	input := ownerstore.IdempotencyInput{ID: id("idem"), TenantScope: scope, ActorScope: c.ActorId, OperationName: name, IdempotencyKey: c.IdempotencyKey, RequestSHA256: strings.TrimPrefix(digest(jsonBytes(request)), "sha256:")}
	previous, found, e := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(e, ownerstore.ErrIdempotencyConflict) {
		return status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if e != nil {
		return dbError(e)
	}
	if found {
		return protojson.Unmarshal(previous.ResponseBody, response)
	}
	if e = run(tx); e != nil {
		return e
	}
	input.ResourceID = "command"
	input.ResponseStatus = 200
	input.ResponseBody = jsonBytes(response)
	if e = s.Store.RecordIdempotency(ctx, tx, input); e != nil {
		return dbError(e)
	}
	return dbError(tx.Commit())
}
func requireTenant(c *api.CallContext) error {
	if tenant(c) == "" {
		return status.Error(codes.InvalidArgument, "tenant scope is required")
	}
	return nil
}
func checkName(v string) error {
	if !nameRE.MatchString(v) {
		return status.Error(codes.InvalidArgument, "name must contain 1-128 letters, digits, dots, hyphens or underscores")
	}
	return nil
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
