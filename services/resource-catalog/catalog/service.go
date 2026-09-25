// Package catalog owns the approved resource plans, the versioned price,
// refund and retention policies, and the immutable quotes derived from them.
//
// It is the single writer for resource_catalog state. It never executes a
// provider resource action, never charges a wallet, and never writes another
// owner's subscription obligation: those live with Fabric, Gateway and
// Workspace. A quote is a priced offer, not a capacity reservation and not a
// payment.
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

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// Profile is the deployment owner's approved provider profile. The resource
// catalog never lets a customer pick a provider: the instance declares which
// provider, region and billing mode its approved plans belong to, and the
// administrator publishes individual plans inside that envelope.
type Profile struct {
	Provider                  string
	Region                    string
	BillingMode               string
	ProviderCapabilityVersion string
}

// Service is the resource catalog owner's typed gRPC surface.
type Service struct {
	api.UnimplementedResourceCatalogProductServiceServer

	DB      *sql.DB
	Store   *ownerstore.Store
	Auth    *ownerservice.Authorizer
	Profile Profile
	Policy  *planChangePolicy
	// Ledger is the current consumer of catalog.policy_changed.v1. It is optional
	// at wiring time so the owner can serve reads before a peer address is set.
	Ledger api.DomainInboxClient
}

// Register exposes the complete Resource Catalog owner surface behind the shared
// identity interceptor. Only this owner's store is written.
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("ResourceCatalogProductService", func(g *grpc.Server) {
		api.RegisterResourceCatalogProductServiceServer(g, s)
	})
}

// New binds the owner to its own database, the live CloudIdentity authority, and
// the instance's approved provider profile.
func New(db *sql.DB, auth *ownerservice.Authorizer, profile Profile) (*Service, error) {
	if db == nil || auth == nil {
		return nil, fmt.Errorf("resource catalog database and live authorization are required")
	}
	if strings.TrimSpace(profile.Provider) == "" || strings.TrimSpace(profile.Region) == "" {
		return nil, fmt.Errorf("resource catalog provider and region profile are required")
	}
	if profile.BillingMode != "PREPAID_MONTHLY" && profile.BillingMode != "LOCAL_NO_CHARGE" {
		return nil, fmt.Errorf("resource catalog billing mode must be PREPAID_MONTHLY or LOCAL_NO_CHARGE")
	}
	if strings.TrimSpace(profile.ProviderCapabilityVersion) == "" {
		return nil, fmt.Errorf("resource catalog provider capability version is required")
	}
	store, err := ownerstore.New(db, "resource_catalog")
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Store: store, Auth: auth, Profile: profile, Policy: defaultPlanChangePolicy()}, nil
}

func id(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func upper(s string) string  { return strings.ToUpper(s) }

func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "resource catalog entry not found")
	}
	var p *pq.Error
	if errors.As(err, &p) {
		switch p.Code {
		case "23505":
			return status.Error(codes.AlreadyExists, "resource catalog immutable identity already exists")
		case "23503":
			return status.Error(codes.FailedPrecondition, "referenced resource catalog version does not exist")
		}
	}
	return status.Error(codes.Internal, "resource catalog persistence failed")
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

func wire(v proto.Message) []byte { b, _ := protojson.Marshal(v); return b }

// auth asks the live CloudIdentity authority for a decision on this owner's exact
// action and resource. A platform administrator action carries no tenant scope;
// a customer action must carry its own tenant.
func (s *Service) auth(ctx context.Context, c *api.CallContext, action string, admin bool) error {
	if c == nil || c.GetActorId() == "" || c.GetRequestId() == "" || c.GetScope() == nil {
		return status.Error(codes.Unauthenticated, "authenticated request context is required")
	}
	if err := ownerservice.ValidateCallContext(ctx, c); err != nil {
		return err
	}
	value, ok := api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+upper(action)]
	if !ok {
		return status.Error(codes.Internal, "unknown authorization action")
	}
	scope := ownerservice.ResourceScope{}
	if !admin {
		scope.TenantID = c.GetScope().GetTenant().GetTenantId()
		if scope.TenantID == "" {
			return status.Error(codes.InvalidArgument, "tenant scope is required")
		}
	}
	return s.Auth.Authorize(ctx, c, api.AuthorizationActionEnum(value), &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, scope)
}

// command runs one idempotent owner-local mutation. The idempotency identity is
// keyed on the caller's scope, actor, operation name and key, so a retry with the
// same input returns the original stored response instead of a second write.
func (s *Service) command(ctx context.Context, c *api.CallContext, name string, request, response proto.Message, run func(*sql.Tx) error) error {
	if c.GetIdempotencyKey() == "" {
		return status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	scope := c.GetScope().GetTenant().GetTenantId()
	if scope == "" {
		scope = "platform"
	}
	key, _ := json.Marshal([]string{scope, c.ActorId, name, c.IdempotencyKey})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(key)); err != nil {
		return dbError(err)
	}
	input := ownerstore.IdempotencyInput{ID: id("idem"), TenantScope: scope, ActorScope: c.ActorId, OperationName: name, IdempotencyKey: c.IdempotencyKey, RequestSHA256: strings.TrimPrefix(digest(wire(request)), "sha256:")}
	previous, found, err := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return dbError(err)
	}
	if found {
		return protojson.Unmarshal(previous.ResponseBody, response)
	}
	if err = run(tx); err != nil {
		return err
	}
	input.ResourceID = "catalog"
	input.ResponseStatus = 200
	input.ResponseBody = wire(response)
	if err = s.Store.RecordIdempotency(ctx, tx, input); err != nil {
		return dbError(err)
	}
	return dbError(tx.Commit())
}

// tsValid reports whether an optional wire timestamp is present and well-formed.
// The contract pins an explicit RFC 3339 value; a malformed one is refused by the
// caller rather than defaulted to "now".
func tsValid(t interface {
	CheckValid() error
}) bool {
	return t != nil && t.CheckValid() == nil
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
