// Package coordination owns Fabric's target resource acceptance and readback.
// Accepted resource identities are durable intent, not evidence of a provider
// purchase or application readiness. Dispatch stays pending until the payment,
// Instance authorization and provider adapter evidence can be verified.
package coordination

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
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

type AuthorizeFunc func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error

type Service struct {
	api.UnimplementedFabricCoordinationServer
	DB         *sql.DB
	Store      *ownerstore.Store
	Authorize  AuthorizeFunc
	Catalog    api.CatalogCoordinationClient
	Ledger     api.LedgerCoordinationClient
	Dispatcher LocalResourceDispatcher
}

func New(db *sql.DB, authorize AuthorizeFunc, catalog api.CatalogCoordinationClient) (*Service, error) {
	if db == nil || authorize == nil || catalog == nil {
		return nil, errors.New("Fabric database, live authorization and Catalog coordination are required")
	}
	store, err := ownerstore.New(db, "fabric")
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Store: store, Authorize: authorize, Catalog: catalog}, nil
}

func (s *Service) Register(server *ownerservice.Server) error {
	if err := server.RequireProductGroups("FabricCoordination"); err != nil {
		return err
	}
	return server.RegisterGroup("FabricCoordination", func(g *grpc.Server) { api.RegisterFabricCoordinationServer(g, s) })
}

func peer(ctx context.Context, allowed ...owneridentity.Service) error {
	caller, ok := ownerservice.PeerOwner(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "verified calling service is required")
	}
	for _, value := range allowed {
		if caller == value {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "calling service may not access this Fabric operation")
}

func (s *Service) authorizeWorkspace(ctx context.Context, call *api.CallContext, workspace, tenant string, action api.AuthorizationActionEnum) error {
	if err := ownerservice.ValidateCallContext(ctx, call); err != nil {
		return err
	}
	if tenant == "" || call.GetScope().GetTenant().GetTenantId() != tenant {
		return status.Error(codes.PermissionDenied, "resource belongs to another tenant")
	}
	return s.Authorize(ctx, call, action, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: &workspace}, ownerservice.ResourceScope{TenantID: tenant})
}

func validateCommand(r *api.EnsureResourcesCommand) error {
	if r == nil || strings.TrimSpace(r.GetWorkspaceId()) == "" || strings.TrimSpace(r.GetObligationId()) == "" || strings.TrimSpace(r.GetContext().GetIdempotencyKey()) == "" {
		return status.Error(codes.InvalidArgument, "workspace, obligation and idempotency key are required")
	}
	a, p := r.GetQuoteAcceptance(), r.GetPlan()
	if a == nil || a.GetAcceptanceId() == "" || a.GetSnapshotDigest() == "" || a.GetQuote().GetId() == "" || a.GetQuote().GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED || a.GetQuote().GetPurpose() != api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY || a.GetObligationId() != r.GetObligationId() || a.GetWorkspaceId() != r.GetWorkspaceId() || p == nil || !proto.Equal(p, a.GetResourcePlan()) {
		return status.Error(codes.InvalidArgument, "accepted deployment quote, workspace, obligation and exact resource plan are required")
	}
	if p.GetComputePlanId() == "" || p.GetStoragePlanId() == "" || p.GetProviderProfileId() == "" || p.GetProviderComputeSkuId() == "" || p.GetProviderStorageSkuId() == "" || p.GetProvider() == "" || p.GetRegion() == "" || p.GetProviderCapabilityVersion() == "" || p.GetVcpus() <= 0 || p.GetMemoryMib() <= 0 || p.GetCapacityGib() <= 0 || p.GetPrepaidMonths() <= 0 || (p.GetBillingMode() != "PREPAID_MONTHLY" && p.GetBillingMode() != "LOCAL_NO_CHARGE") {
		return status.Error(codes.InvalidArgument, "approved provider resource plan is incomplete")
	}
	if p.GetComputePlanId() != a.GetQuote().GetComputePlanId() || p.GetStoragePlanId() != a.GetQuote().GetStoragePlanId() || p.GetPrepaidMonths() != a.GetQuote().GetPeriodMonths() {
		return status.Error(codes.InvalidArgument, "resource plan differs from accepted quote")
	}
	return nil
}

// EnsureResources commits one resource intent and its original operation. It
// deliberately does not dispatch provider requests: a caller-supplied receipt
// string is not verified payment or Instance authority.
func (s *Service) EnsureResources(ctx context.Context, r *api.EnsureResourcesCommand) (*api.Operation, error) {
	if err := peer(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if err := validateCommand(r); err != nil {
		return nil, err
	}
	tenant := r.Context.GetScope().GetTenant().GetTenantId()
	if err := s.authorizeWorkspace(ctx, r.Context, r.WorkspaceId, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	command := proto.Clone(r).(*api.EnsureResourcesCommand)
	command.Context = nil
	// Funding and Instance references are later evidence about this intent, not
	// its identity. They remain untrusted until their owner can be queried, and
	// must not prevent the same operation from being resumed with that evidence.
	command.ConfirmedChargeReceiptId = ""
	command.InstanceAuthorizationReference = ""
	body, err := protojson.Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "resource command cannot be encoded")
	}
	normalized, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "resource command cannot be encoded")
	}
	input := ownerstore.IdempotencyInput{ID: "idem_" + uuid.NewString(), TenantScope: tenant, ActorScope: r.Context.GetActorId(), OperationName: "EnsureResources", IdempotencyKey: r.Context.GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	// Every writer locks the original workspace before reading its resource set;
	// concurrent retries cannot allocate a second set or bind a different order.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:idem:"+tenant+":"+input.ActorScope+":"+input.IdempotencyKey); err != nil {
		return nil, persistenceError(err)
	}
	// Waiting for a competing command must not preserve an authorization that
	// was revoked or expired while queued. Re-check at the actual write boundary.
	if err = s.authorizeWorkspace(ctx, r.Context, r.WorkspaceId, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	record, found, err := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has a different resource command")
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	if found {
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.resume(ctx, r, record.OperationID)
	}
	var setID, operationID, storedTenant string
	var original []byte
	err = tx.QueryRowContext(ctx, `SELECT rs.id,rs.tenant_id,o.id,o.accepted_input FROM fabric.resource_sets rs JOIN fabric.operations o ON o.resource_id=rs.id AND o.kind='resource_provision' WHERE rs.workspace_id=$1`, r.WorkspaceId).Scan(&setID, &storedTenant, &operationID, &original)
	if err == nil {
		if storedTenant != tenant {
			return nil, status.Error(codes.PermissionDenied, "resource belongs to another tenant")
		}
		stored := &api.EnsureResourcesCommand{}
		if err = protojson.Unmarshal(original, stored); err != nil {
			return nil, status.Error(codes.DataLoss, "original resource command cannot be read")
		}
		if stored.GetWorkspaceId() != command.GetWorkspaceId() || stored.GetObligationId() != command.GetObligationId() || !proto.Equal(stored.GetPlan(), command.GetPlan()) || !proto.Equal(stored.GetQuoteAcceptance(), command.GetQuoteAcceptance()) {
			return nil, status.Error(codes.AlreadyExists, "workspace is bound to a different original resource command")
		}
		// Additional receipt references resume this original intent but cannot
		// create a second resource set or prove payment on their own.
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, persistenceError(err)
	} else {
		// Clear the previous owner's decision identity. Catalog independently
		// authorizes this same actor/session or accepted grant for its own audience.
		call := proto.Clone(r.Context).(*api.CallContext)
		call.AuthorizationContextId = ""
		accepted, readErr := s.Catalog.ReadQuoteResourcePlan(ctx, &api.QuoteResourcePlanRequest{Context: call, QuoteId: r.QuoteAcceptance.Quote.Id})
		if readErr != nil {
			return nil, readErr
		}
		if !proto.Equal(accepted, r.QuoteAcceptance) {
			return nil, status.Error(codes.FailedPrecondition, "Catalog acceptance differs from the resource command")
		}
		setID, operationID = "rset_"+uuid.NewString(), "op_"+uuid.NewString()
		plan, marshalErr := protojson.Marshal(r.Plan)
		if marshalErr != nil {
			return nil, status.Error(codes.InvalidArgument, "resource plan cannot be encoded")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'unknown')`, setID, tenant, r.WorkspaceId, r.Plan.Provider, r.Plan.ProviderProfileId, r.Plan.Region, r.Plan.ComputePlanId, r.Plan.StoragePlanId, r.QuoteAcceptance.Quote.Id, plan)
		if err != nil {
			return nil, persistenceError(err)
		}
		for _, kind := range []string{"compute", "storage"} {
			id := "res_" + uuid.NewString()
			_, err = tx.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ($1,$2,$3,$4,$5,$6,'unknown')`, id, setID, kind, "purchase:"+id, r.Plan.BillingMode, plan)
			if err != nil {
				return nil, persistenceError(err)
			}
		}
		_, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenant, ActorID: r.Context.ActorId, Kind: "resource_provision", ResourceID: setID, Stage: "payment_authorization", RequestID: r.Context.RequestId, AcceptedInput: body})
		if err != nil {
			return nil, persistenceError(err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',observation_result='unknown',error_code='DEPENDENCY_UNAVAILABLE' WHERE id=$1`, operationID)
		if err != nil {
			return nil, persistenceError(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_actions (id,resource_set_id,command_id,action,provider_idempotency_key,approved_input,observation_result,error_code) VALUES ($1,$2,$3,'allocate',$4,$5,'unknown','DEPENDENCY_UNAVAILABLE')`, "raction_"+uuid.NewString(), setID, operationID, "allocate:"+setID, body)
		if err != nil {
			return nil, persistenceError(err)
		}
	}
	input.ResourceID, input.OperationID, input.ResponseBody = setID, operationID, []byte(`{}`)
	if err = s.Store.RecordIdempotency(ctx, tx, input); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.resume(ctx, r, operationID)
}

func (s *Service) operation(ctx context.Context, id string) (*api.Operation, error) {
	r, err := s.Store.ReadOperation(ctx, id)
	if err != nil {
		return nil, persistenceError(err)
	}
	state, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(r.Status)]
	if !ok {
		return nil, status.Error(codes.DataLoss, "invalid Fabric operation status")
	}
	stage, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(r.Stage)]
	if !ok {
		return nil, status.Error(codes.DataLoss, "invalid Fabric operation stage")
	}
	out := &api.Operation{OperationId: r.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_PROVISION, ResourceId: r.ResourceID, Status: api.OperationStatusEnum(state), Stage: api.OperationStageEnum(stage), RequestId: r.RequestID, CreatedAt: timestamppb.New(r.CreatedAt), UpdatedAt: timestamppb.New(r.UpdatedAt)}
	if r.Observation != "" {
		observation, exists := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(r.Observation)]
		if !exists {
			return nil, status.Error(codes.DataLoss, "invalid Fabric operation observation")
		}
		out.ObservationResult = api.OperationObservationResultEnum(observation).Enum()
	}
	if r.ErrorCode != "" {
		value, exists := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(r.ErrorCode)]
		if !exists {
			return nil, status.Error(codes.DataLoss, "invalid Fabric operation error")
		}
		out.ErrorCode = api.ErrorCodeEnum(value).Enum()
	}
	if !r.Terminal() {
		out.PollAfterSeconds = proto.Int32(5)
	}
	return out, nil
}

func persistenceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "Fabric resource was not found")
	}
	return status.Error(codes.Internal, "Fabric persistence failed")
}
