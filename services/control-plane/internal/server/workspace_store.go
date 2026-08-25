package server

import "context"

// Callers of these methods through app.tables:
// - services/control-plane/internal/server/account_reconcile.go
// - services/control-plane/internal/server/app_state.go
// - services/control-plane/internal/server/billing_projection.go
// - services/control-plane/internal/server/customer_facts_test.go
// - services/control-plane/internal/server/ent_state_store_test.go
// - services/control-plane/internal/server/operational_alerts_test.go
// - services/control-plane/internal/server/renewal_worker.go
// - services/control-plane/internal/server/resource_facts.go
// - services/control-plane/internal/server/routes_admin.go
// - services/control-plane/internal/server/routes_gateway.go
// - services/control-plane/internal/server/routes_provider_acceptance.go
// - services/control-plane/internal/server/routes_workspace.go
// - services/control-plane/internal/server/routes_workspace_launch.go
// - services/control-plane/internal/server/server_test.go
// - services/control-plane/internal/server/wallet_adjustment.go
// - services/control-plane/internal/server/workspace_delete.go
// - services/control-plane/internal/server/workspace_delete_test.go
// - services/control-plane/internal/server/workspace_gateway.go
// - services/control-plane/internal/server/workspace_gateway_budget.go
// - services/control-plane/internal/server/workspace_launch_activation.go
// - services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go
// - services/control-plane/internal/server/workspace_launch_disposable_reset.go
// - services/control-plane/internal/server/workspace_launch_repair.go
// - services/control-plane/internal/server/workspace_launch_service.go
// - services/control-plane/internal/server/workspace_renewal.go
// - services/control-plane/internal/server/workspace_renewal_test.go
type WorkspaceStore interface {
	ListComputes(ctx context.Context, accountID string) ([]map[string]any, error)
	GetCompute(ctx context.Context, id string) (map[string]any, bool, error)
	SaveCompute(ctx context.Context, row map[string]any) error
	DeleteCompute(ctx context.Context, id string) error
	ListStorages(ctx context.Context, accountID string) ([]map[string]any, error)
	GetStorage(ctx context.Context, id string) (map[string]any, bool, error)
	SaveStorage(ctx context.Context, row map[string]any) error
	DeleteStorage(ctx context.Context, id string) error
	ListAttachments(ctx context.Context, accountID string) ([]map[string]any, error)
	GetAttachment(ctx context.Context, id string) (map[string]any, bool, error)
	SaveAttachment(ctx context.Context, row map[string]any) error
	DeleteAttachment(ctx context.Context, id string) error
	ListWorkspaces(ctx context.Context, accountID string) ([]map[string]any, error)
	GetWorkspace(ctx context.Context, id string) (map[string]any, bool, error)
	PageWorkspaces(ctx context.Context, accountID string, query tablePageQuery) (tablePage, error)
	CountWorkspaces(ctx context.Context) (int, error)
	CountWorkspacesByAccount(ctx context.Context, accountIDs []string) (map[string]int, error)
	SaveWorkspace(ctx context.Context, row map[string]any) error
	CompareAndSwapWorkspaceAPIKey(ctx context.Context, workspaceID string, expectedID, newID int64) error
	ClaimWorkspaceKeyRotation(ctx context.Context, row map[string]any) error
	ApplyWorkspaceRenewalIntent(ctx context.Context, update workspaceRenewalIntentCAS) error
	ClaimWorkspaceLaunchReconcile(ctx context.Context, claim workspaceLaunchReconcileClaim) error
	PersistWorkspaceLaunchReconcile(ctx context.Context, update workspaceLaunchReconcileCAS) error
	ApplyWorkspaceLaunchCanonicalFactRepair(ctx context.Context, update workspaceLaunchCanonicalFactRepairCAS) error
	ClaimWorkspaceRenewal(ctx context.Context, claim workspaceRenewalClaimCAS) error
	PersistWorkspaceRenewal(ctx context.Context, update workspaceRenewalPersistCAS) error
	ActivateWorkspaceLaunchProjection(ctx context.Context, row map[string]any) (map[string]any, error)
	ClaimWorkspaceCreate(ctx context.Context, workspace map[string]any, operation map[string]any) error
	ApplyWorkspaceDelete(ctx context.Context, mutation workspaceDeleteStoreMutation) error
	DeleteWorkspace(ctx context.Context, id string) error
	ListRuntimeOperations(ctx context.Context) ([]map[string]any, error)
	GetRuntimeOperation(ctx context.Context, id string) (map[string]any, bool, error)
	PageRuntimeOperations(ctx context.Context, query runtimeOperationQuery) (tablePage, error)
	SaveRuntimeOperation(ctx context.Context, row map[string]any) error
}
