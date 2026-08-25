package server

import "context"

// IdentityStore callers through app.tables:
// - services/control-plane/internal/server/account_reconcile.go
// - services/control-plane/internal/server/admin_ops.go
// - services/control-plane/internal/server/app_state.go
// - services/control-plane/internal/server/auth_accounts.go
// - services/control-plane/internal/server/console_tenant_isolation_test.go
// - services/control-plane/internal/server/identity_hard_cut_test.go
// - services/control-plane/internal/server/identity_security_test.go
// - services/control-plane/internal/server/monthly_billing.go
// - services/control-plane/internal/server/provisioned_account_test.go
// - services/control-plane/internal/server/routes_admin.go
// - services/control-plane/internal/server/routes_provider_acceptance.go
// - services/control-plane/internal/server/routes_workspace_launch.go
// - services/control-plane/internal/server/server_test.go
// - services/control-plane/internal/server/session_credential_vault_test.go
// - services/control-plane/internal/server/wallet_adjustment.go
// - services/control-plane/internal/server/workspace_delete_test.go
type IdentityStore interface {
	ListAccounts(ctx context.Context, accountID string) ([]map[string]any, error)
	GetAccount(ctx context.Context, id string) (map[string]any, bool, error)
	PageAccounts(ctx context.Context, query tablePageQuery) (tablePage, error)
	CountAccountStatuses(ctx context.Context) (map[string]int, error)
	SaveAccount(ctx context.Context, row map[string]any) error
	ApplyWorkspacePurchaseEligibility(ctx context.Context, mutation workspacePurchaseEligibilityMutation) (map[string]any, error)
	CreateProvisionedAccount(ctx context.Context, account, user map[string]any) error
	ApplyUserLifecycle(ctx context.Context, user map[string]any) error
	ListUsers(ctx context.Context, includeDeleted bool) ([]map[string]any, error)
	GetUser(ctx context.Context, id string) (map[string]any, bool, error)
	GetUserByEmail(ctx context.Context, email string, includeDeleted bool) (map[string]any, bool, error)
	SaveUser(ctx context.Context, row map[string]any) error
	DeleteUser(ctx context.Context, id string) error
	ListSessions(ctx context.Context) (controlPlaneRecordSet, error)
	GetSession(ctx context.Context, id string) (map[string]any, bool, error)
	ListSessionsByUser(ctx context.Context, userID string) (controlPlaneRecordSet, error)
	SaveSession(ctx context.Context, row map[string]any) error
	DeleteSession(ctx context.Context, id string) error
}
