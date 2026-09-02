package server

import "context"

// Callers of these methods via app.tables:
// - admin_ops.go: SaveAuditEvent
// - app_state.go: ListAuditEvents
// - billing_projection.go: BillingReconciliation
// - routes_admin.go: ListAuditEvents, BillingReconciliation
// - routes_announcements.go: ListAnnouncements, ListAnnouncementReads, MarkAnnouncementRead, ApplyAnnouncementMutation
// - routes_billing.go: BillingReconciliation, ApplyBillingReconciliation
// - routes_gateway.go: SaveAuditEvent
// - wallet_adjustment.go: SaveAuditEvent
// - workspace_delete.go: SaveAuditEvent
// - workspace_gateway.go: SaveAuditEvent
// - workspace_gateway_budget.go: SaveAuditEvent, ListAuditEvents
// - workspace_renewal_test.go: ListAuditEvents
type SharedStore interface {
	ListAuditEvents(ctx context.Context, accountID string) ([]map[string]any, error)
	SaveAuditEvent(ctx context.Context, row map[string]any) error
	ListAnnouncements(ctx context.Context) ([]map[string]any, error)
	ApplyAnnouncementMutation(ctx context.Context, mutation announcementMutation) (map[string]any, error)
	ListAnnouncementReads(ctx context.Context, userID string) ([]map[string]any, error)
	MarkAnnouncementRead(ctx context.Context, announcementID, userID, readAt string) (map[string]any, error)
	ReserveProductionE2EAttempt(ctx context.Context, claim productionE2EAttemptClaim) (map[string]any, error)
	GetProductionE2EAttempt(ctx context.Context, id string) (map[string]any, bool, error)
	CompleteProductionE2EAttempt(ctx context.Context, id, binding string) (map[string]any, error)
	BillingReconciliation(ctx context.Context) (map[string]any, bool, error)
	ApplyBillingReconciliation(ctx context.Context, mutation billingReconciliationMutation) error
}
