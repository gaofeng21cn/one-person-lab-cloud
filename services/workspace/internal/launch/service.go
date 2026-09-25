// Package launch owns accepted Workspace orders and their recoverable resource intent.
package launch

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	api.UnimplementedWorkspaceProductServiceServer
	api.UnimplementedOwnerCommitReadbackServer
	Store      *ownerstore.Store
	Auth       *ownerservice.Authorizer
	Catalog    api.CatalogCoordinationClient
	Fabric     api.FabricCoordinationClient
	Identity   api.CloudIdentityAuthorizationClient
	Ledger     api.LedgerCoordinationClient
	Capability api.CapabilityProductServiceClient
	Serve      api.ServeAgentCoordinationClient
}

func New(db *sql.DB, auth *ownerservice.Authorizer, catalog api.CatalogCoordinationClient, fabric api.FabricCoordinationClient, identity api.CloudIdentityAuthorizationClient) (*Service, error) {
	if db == nil || auth == nil || catalog == nil || fabric == nil || identity == nil {
		return nil, errors.New("Workspace database, authorization, Catalog, Fabric and identity are required")
	}
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		return nil, err
	}
	return &Service{Store: store, Auth: auth, Catalog: catalog, Fabric: fabric, Identity: identity}, nil
}
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("WorkspaceProductService", func(g *grpc.Server) {
		api.RegisterWorkspaceProductServiceServer(g, s)
		api.RegisterOwnerCommitReadbackServer(g, s)
	})
}
func id(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}
func wire(m proto.Message) json.RawMessage {
	b, err := protojson.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
func dbError(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ownerstore.ErrOperationNotFound) {
		return status.Error(codes.NotFound, "Workspace order not found")
	}
	return status.Error(codes.Unavailable, "Workspace persistence unavailable")
}
func workspaceResource(value string) *api.AuthorizationResource {
	r := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE}
	if value != "" {
		r.Id = proto.String(value)
	}
	return r
}
func (s *Service) authorize(ctx context.Context, c *api.CallContext, a api.AuthorizationActionEnum, resource *api.AuthorizationResource, tid string) error {
	return s.Auth.Authorize(ctx, c, a, resource, ownerservice.ResourceScope{TenantID: tid})
}

// acceptedOrder is immutable user intent. Transport session secrets are never
// persisted; recovery uses the bounded grant issued for this exact commit.
type acceptedOrder struct {
	Request                json.RawMessage `json:"request"`
	Quote                  json.RawMessage `json:"quote"`
	AuthorizationContextID string          `json:"authorizationContextId"`
	InputDigest            string          `json:"inputDigest"`
}
type orderResult struct {
	GrantID               string          `json:"grantId,omitempty"`
	Acceptance            json.RawMessage `json:"acceptance,omitempty"`
	FabricOperationID     string          `json:"fabricOperationId,omitempty"`
	ResourceSetID         string          `json:"resourceSetId,omitempty"`
	ResourceReadback      json.RawMessage `json:"resourceReadback,omitempty"`
	ZeroChargeReceipt     json.RawMessage `json:"zeroChargeReceipt,omitempty"`
	RuntimeCapability     json.RawMessage `json:"runtimeCapability,omitempty"`
	RuntimeBinding        json.RawMessage `json:"runtimeBinding,omitempty"`
	RuntimeReservation    json.RawMessage `json:"runtimeReservation,omitempty"`
	RuntimeCommand        json.RawMessage `json:"runtimeCommand,omitempty"`
	RuntimeReadback       json.RawMessage `json:"runtimeReadback,omitempty"`
	RuntimeDeployAccepted bool            `json:"runtimeDeployAccepted,omitempty"`
}

func decodeOrder(op ownerstore.Operation) (acceptedOrder, orderResult, error) {
	var a acceptedOrder
	var r orderResult
	if json.Unmarshal(op.AcceptedInput, &a) != nil || json.Unmarshal(op.Result, &r) != nil {
		return a, r, status.Error(codes.DataLoss, "stored Workspace order is invalid")
	}
	return a, r, nil
}

func (s *Service) CreateWorkspace(ctx context.Context, r *api.CreateWorkspaceRpcRequest) (*api.Operation, error) {
	c, b := r.GetContext(), r.GetBody()
	tid := c.GetScope().GetTenant().GetTenantId()
	if tid == "" || b == nil || strings.TrimSpace(b.Name) == "" || len(b.Name) > 128 || b.QuoteId == "" || c.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, name, quote and idempotency key are required")
	}
	if b.RenewalMode != api.CreateWorkspaceRequestRenewalModeEnum_CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_MANUAL || b.GetAutomaticRenewalConsent() {
		return nil, status.Error(codes.FailedPrecondition, "first deployment currently supports manual renewal only")
	}
	if err := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, workspaceResource(""), tid); err != nil {
		return nil, err
	}
	normalized, err := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid Workspace input")
	}
	idem := ownerstore.IdempotencyInput{ID: id("idem_"), TenantScope: tid, ActorScope: c.ActorId, OperationName: "createWorkspace", IdempotencyKey: c.IdempotencyKey, RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tid+"/"+c.ActorId+"/createWorkspace/"+c.IdempotencyKey); err != nil {
		return nil, dbError(err)
	}
	replay, found, err := s.Store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return nil, dbError(err)
	}
	if found {
		tx.Rollback()
		op, err := s.Store.ReadOperation(ctx, replay.OperationID)
		if err != nil {
			return nil, dbError(err)
		}
		return operation(op), nil
	}
	readCall := proto.Clone(c).(*api.CallContext)
	readCall.AuthorizationContextId = ""
	quote, err := s.Catalog.ReadQuoteResourcePlan(ctx, &api.QuoteResourcePlanRequest{Context: readCall, QuoteId: b.QuoteId})
	if err != nil {
		return nil, err
	}
	q := quote.GetQuote()
	if q.GetId() != b.QuoteId || q.Purpose != api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY || q.Status != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED || q.ExpiresAt == nil || !q.ExpiresAt.AsTime().After(time.Now()) || quote.ResourcePlan == nil || q.GetCapabilityVersionId() == "" {
		return nil, status.Error(codes.FailedPrecondition, "an unexpired deploy quote with a frozen resource plan is required")
	}
	// Revalidate after the command lock and the quote read. A request queued
	// behind another transaction must not commit using a pre-lock permission.
	decisionRequest := &api.AuthorizationRequest{Scope: c.Scope, ActorId: c.ActorId, SessionId: c.SessionId, AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, Resource: workspaceResource(""), RequestId: c.RequestId}
	decision, err := s.Identity.AuthorizeAction(ctx, decisionRequest)
	if err != nil {
		return nil, err
	}
	if err = owneridentity.ValidateDecision(decisionRequest, decision, time.Now()); err != nil || decision.GetAuthorizationContextId() == "" {
		return nil, status.Error(codes.PermissionDenied, "fresh Workspace authorization required")
	}
	wid, oid := id("workspace_"), id("op_")
	material, _ := json.Marshal(struct {
		Request json.RawMessage `json:"request"`
		Quote   json.RawMessage `json:"quote"`
	}{wire(b), wire(quote)})
	accepted := acceptedOrder{Request: wire(b), Quote: wire(quote), AuthorizationContextID: decision.GetAuthorizationContextId(), InputDigest: "sha256:" + ownerstore.HashRequestBody(material)}
	raw, _ := json.Marshal(accepted)
	op, err := s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: oid, TenantID: tid, ActorID: c.ActorId, Kind: "create_workspace", ResourceID: wid, Stage: "quote_binding", RequestID: c.RequestId, AcceptedInput: raw})
	if err != nil {
		return nil, dbError(err)
	}
	// PostgreSQL now() is transaction-start time, which precedes the fresh
	// post-lock decision. Record the actual acceptance instant for grant proof.
	if err = tx.QueryRowContext(ctx, `UPDATE workspace.operations SET created_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 RETURNING created_at,updated_at`, oid).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		return nil, dbError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,active_operation_id,created_by,version) VALUES($1,$2,$3,'provisioning',$4,$5,$6,$7,1)`, wid, tid, b.Name, q.ComputePlanId, q.StoragePlanId, oid, c.ActorId); err != nil {
		return nil, dbError(err)
	}
	idem.ResourceID = wid
	idem.OperationID = oid
	idem.ResponseBody = wire(operation(op))
	if err = s.Store.RecordIdempotency(ctx, tx, idem); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	// The durable order precedes every cross-owner side effect. A lost response
	// leaves the original operation recoverable, never a second quote or order.
	_ = s.Resume(ctx, oid)
	op, err = s.Store.ReadOperation(ctx, oid)
	if err != nil {
		return nil, dbError(err)
	}
	return operation(op), nil
}
func operation(o ownerstore.Operation) *api.Operation {
	out := &api.Operation{OperationId: o.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_WORKSPACE, ResourceId: o.ResourceID, Status: api.OperationStatusEnum(api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(o.Status)]), Stage: api.OperationStageEnum(api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(o.Stage)]), RequestId: o.RequestID, CreatedAt: timestamppb.New(o.CreatedAt), UpdatedAt: timestamppb.New(o.UpdatedAt)}
	if o.Observation != "" {
		v := api.OperationObservationResultEnum(api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(o.Observation)])
		out.ObservationResult = &v
	}
	if !o.Terminal() {
		out.PollAfterSeconds = proto.Int32(5)
	}
	return out
}
func (s *Service) ReadOwnerCommit(ctx context.Context, r *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Tenant.Service() && peer != owneridentity.Ledger.Service()) {
		return nil, status.Error(codes.Unauthenticated, "CloudIdentity peer required")
	}
	op, err := s.Store.ReadOperation(ctx, r.GetOperationId())
	if err != nil {
		return nil, dbError(err)
	}
	if r.Owner != api.OwnerEnum_OWNER_ENUM_WORKSPACE || op.ResourceID != r.ResourceId || op.Kind != "create_workspace" {
		return nil, status.Error(codes.FailedPrecondition, "Workspace commit identity mismatch")
	}
	return evidence(op)
}
func evidence(op ownerstore.Operation) (*api.OwnerCommitEvidence, error) {
	a, _, err := decodeOrder(op)
	if err != nil {
		return nil, err
	}
	q := &api.QuoteAcceptance{}
	if protojson.Unmarshal(a.Quote, q) != nil {
		return nil, status.Error(codes.DataLoss, "stored quote is invalid")
	}
	return &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: op.ID, ResourceId: op.ResourceID, AcceptedInputDigest: a.InputDigest, CommittedVersion: 1, AcceptedAt: timestamppb.New(op.CreatedAt), AuthorizationContextId: a.AuthorizationContextID, ActorId: op.ActorID, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: op.TenantID}}}, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, AuthorizationResource: workspaceResource(""), ContinuationResources: []*api.AuthorizationResource{workspaceResource(op.ResourceID), {Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String(q.Quote.GetCapabilityVersionId())}}}, nil
}
