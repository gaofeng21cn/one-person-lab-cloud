package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// modelReadIdentity answers the Workspace owner's GETWORKSPACEMODELS authorization
// with the exact decision CloudIdentity would issue for the request, so the test
// exercises the real authorizer and decision validator instead of bypassing them.
type modelReadIdentity struct {
	api.CloudIdentityAuthorizationClient
	denial error
}

func (i *modelReadIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	if i.denial != nil {
		return nil, i.denial
	}
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

// modelCallContext is the Console BFF's session-scoped read of one Workspace.
func modelCallContext(tenant string) (*api.CallContext, context.Context) {
	session := "session-models"
	call := &api.CallContext{
		ActorId: "actor-original", SessionId: &session, RequestId: "request-models",
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}},
	}
	return call, ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

// modelConfigurationWorkspace seeds one delivered Workspace whose accepted order
// froze explicit model selections, and returns the owner service plus those frozen
// selections.
func modelConfigurationWorkspace(t *testing.T, db *sql.DB) (*Service, ownerstore.Operation, []*api.ModelSelection) {
	t.Helper()
	service, op, accepted, _ := seedRuntimeOrder(t, db)
	frozen := []*api.ModelSelection{{Slot: "chat", ModelId: "model-original"}, {Slot: "embedding", ModelId: "embedding-original"}}
	quote := proto.Clone(accepted.GetQuote()).(*api.Quote)
	quote.ModelSelections = frozen
	input, err := json.Marshal(acceptedOrder{Quote: wire(&api.QuoteAcceptance{Quote: quote}), AuthorizationContextID: "authorization-original", InputDigest: "sha256:original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET accepted_input=$2 WHERE id=$1`, op.ID, input); err != nil {
		t.Fatal(err)
	}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	return service, op, frozen
}

// recordModelConfiguration persists one configuration intent exactly as the change
// path does: the row names its version, owning operation and typed selections, and
// the operation that carries the change records its own lifecycle state.
func recordModelConfiguration(t *testing.T, db *sql.DB, workspaceID string, version int64, observation, operationState string) *api.ModelSelection {
	t.Helper()
	ctx := t.Context()
	selections := []*api.ModelSelection{{Slot: "chat", ModelId: "model-" + operationState}}
	raw, err := modelSelectionsJSON(selections)
	if err != nil {
		t.Fatal(err)
	}
	operationID := "operation-models-" + operationState
	if _, err = db.ExecContext(ctx, `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,observation_result)
		VALUES($1,'tenant-original','actor-original','update_models',$2,$3,'reload','request-models','{}'::jsonb,$4)`, operationID, workspaceID, operationState, observation); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO workspace.model_configurations(id,workspace_id,version,gateway_key_binding_id,operation_id,runtime_reload_observation,created_by,selections)
		VALUES($1,$2,$3,'gateway-key-original',$4,$5,'actor-original',$6)`, "configuration-"+operationState, workspaceID, version, operationID, observation, raw); err != nil {
		t.Fatal(err)
	}
	return selections[0]
}

func TestGetWorkspaceModelsAnswersTheAppliedLaunchConfiguration(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, frozen := modelConfigurationWorkspace(t, db)
	call, ctx := modelCallContext("tenant-original")
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetWorkspaceId() != "workspace-original" || configuration.GetVersion() != 0 || configuration.GetAppliedVersion() != 0 {
		t.Fatalf("configuration=%v", configuration)
	}
	if configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED {
		t.Fatalf("accepted launch configuration is not reported as applied: %v", configuration)
	}
	if !proto.Equal(configuration.GetUpdatedAt(), timestamppb.New(dbWorkspaceUpdated(t, db))) {
		t.Fatal("configuration time is not the Workspace's own recorded time")
	}
	if len(configuration.GetSelections()) != len(frozen) || configuration.GetSelections()[0].GetModelId() != "model-original" || configuration.GetSelections()[1].GetModelId() != "embedding-original" {
		t.Fatalf("launch selections=%v, want the accepted order's frozen selections", configuration.GetSelections())
	}
	// The Workspace read carries the same applied version as its own row, so the
	// BFF and the console read one fact.
	workspace, err := service.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.GetModelConfigurationVersion() != configuration.GetAppliedVersion() {
		t.Fatalf("Workspace model configuration version %d differs from the model read %d", workspace.GetModelConfigurationVersion(), configuration.GetAppliedVersion())
	}
}

func dbWorkspaceUpdated(t *testing.T, db *sql.DB) time.Time {
	t.Helper()
	var updated time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT updated_at FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestGetWorkspaceModelsReportsAPendingChangeAsUnapplied(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, _ := modelConfigurationWorkspace(t, db)
	call, ctx := modelCallContext("tenant-original")
	recordModelConfiguration(t, db, "workspace-original", 1, "unknown", "awaiting_confirmation")
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetVersion() != 1 || configuration.GetAppliedVersion() != 0 || configuration.GetOperationId() != "operation-models-awaiting_confirmation" {
		t.Fatalf("unconfirmed change=%v", configuration)
	}
	if configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("an unconfirmed reload is presented as %v", configuration.GetStatus())
	}
	if configuration.GetSelections()[0].GetModelId() != "model-awaiting_confirmation" {
		t.Fatalf("selections=%v, want the requested configuration", configuration.GetSelections())
	}
	// The applied version never advances from the request: only the runtime's own
	// confirmed reload may move it.
	if applied := appliedModelVersion(t, db); applied != 0 {
		t.Fatalf("applied version advanced to %d without a confirmed reload", applied)
	}
}

func appliedModelVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var applied int64
	if err := db.QueryRowContext(t.Context(), `SELECT model_configuration_version FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	return applied
}

func TestGetWorkspaceModelsDerivesStatusFromTheOwningOperation(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, _ := modelConfigurationWorkspace(t, db)
	call, ctx := modelCallContext("tenant-original")
	recordModelConfiguration(t, db, "workspace-original", 1, "unknown", "failed")
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_FAILED {
		t.Fatalf("failed change is presented as %v", configuration.GetStatus())
	}
	// A confirmed reload that the Workspace recorded is applied at its own version.
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET status='succeeded',completed_at=now() WHERE id='operation-models-failed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.model_configurations SET runtime_reload_observation='confirmed' WHERE operation_id='operation-models-failed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.workspaces SET model_configuration_version=1 WHERE id='workspace-original'`); err != nil {
		t.Fatal(err)
	}
	configuration, err = service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetVersion() != 1 || configuration.GetAppliedVersion() != 1 || configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED {
		t.Fatalf("confirmed configuration=%v", configuration)
	}
	// A change still waiting on its operation is pending, never applied.
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET status='running' WHERE id='operation-models-failed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.model_configurations SET runtime_reload_observation='unknown' WHERE operation_id='operation-models-failed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.workspaces SET model_configuration_version=0 WHERE id='workspace-original'`); err != nil {
		t.Fatal(err)
	}
	configuration, err = service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION || configuration.GetAppliedVersion() != 0 {
		t.Fatalf("unconfirmed configuration=%v", configuration)
	}
}

func TestModelConfigurationStatusFollowsTheOwningFacts(t *testing.T) {
	for name, want := range map[string]api.ModelConfigurationStatusEnum{
		"confirmed at the applied version":       api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED,
		"confirmed ahead of the applied version": api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_PENDING,
		"rejected without a failed operation":    api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_PENDING,
		"unconfirmed observation":                api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION,
		"needs attention operation":              api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION,
		"failed operation":                       api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_FAILED,
	} {
		t.Run(name, func(t *testing.T) {
			var row modelConfigurationRow
			switch name {
			case "confirmed at the applied version":
				row = modelConfigurationRow{version: 2, observation: "confirmed", operationState: "succeeded"}
			case "confirmed ahead of the applied version":
				row = modelConfigurationRow{version: 3, observation: "confirmed", operationState: "succeeded"}
			case "rejected without a failed operation":
				row = modelConfigurationRow{version: 2, observation: "rejected", operationState: "running"}
			case "unconfirmed observation":
				row = modelConfigurationRow{version: 2, observation: "unknown", operationState: "running"}
			case "needs attention operation":
				row = modelConfigurationRow{version: 2, observation: "rejected", operationState: "needs_attention"}
			case "failed operation":
				row = modelConfigurationRow{version: 2, observation: "unknown", operationState: "failed"}
			}
			if got := modelConfigurationStatus(row, 2); got != want {
				t.Fatalf("status=%v want %v", got, want)
			}
		})
	}
}

func TestGetWorkspaceModelsAdmitsOnlyTheOwningTenant(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, _ := modelConfigurationWorkspace(t, db)
	call, ctx := modelCallContext("tenant-other")
	if _, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another tenant err=%v want permission denied", err)
	}
	// A refused policy decision never becomes an answer about the configuration.
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{denial: status.Error(codes.PermissionDenied, "member role required")})
	call, ctx = modelCallContext("tenant-original")
	if _, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied member err=%v want permission denied", err)
	}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	if _, err := service.GetWorkspaceModels(context.Background(), &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unverified caller err=%v want unauthenticated", err)
	}
	if _, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing workspace id err=%v want invalid argument", err)
	}
	if _, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown workspace err=%v want not found", err)
	}
}

func TestModelSelectionsCodecRejectsAnUnreadableRow(t *testing.T) {
	raw, err := modelSelectionsJSON([]*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})
	if err != nil {
		t.Fatal(err)
	}
	selections, err := decodeModelSelections(raw)
	if err != nil || len(selections) != 1 || selections[0].GetSlot() != "chat" || selections[0].GetModelId() != "model-a" {
		t.Fatalf("round trip=%v err=%v", selections, err)
	}
	for name, invalid := range map[string]string{"not a list": `{"slot":"chat"}`, "empty slot": `[{"modelId":"model-a"}]`, "empty model": `[{"slot":"chat"}]`} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeModelSelections([]byte(invalid)); status.Code(err) != codes.DataLoss {
				t.Fatalf("stored selections %s err=%v want data loss", invalid, err)
			}
		})
	}
}
