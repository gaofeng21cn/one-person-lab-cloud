package migrations

import (
	"context"
	"database/sql"
	_ "embed"
)

// Execer executes SQL statements without ORM dialect abstractions.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Tx represents an open database transaction for transactional migrations.
type Tx interface {
	Execer
	Commit() error
	Rollback() error
}

// Driver executes schema and data migrations against *sql.DB or test recorders.
type Driver interface {
	Execer
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
}

type sqlDriver struct {
	db *sql.DB
}

// OpenDriver wraps a standard database/sql handle for control-plane migrations.
func OpenDriver(db *sql.DB) Driver {
	return sqlDriver{db: db}
}

func (d sqlDriver) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, query, args...)
}

func (d sqlDriver) BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error) {
	return d.db.BeginTx(ctx, opts)
}

//go:embed 202607140001_sub2api_monthly_hard_cut.sql
var monthlyHardCut string

//go:embed 202607150003_ent_schema.sql
var coreSchema string

//go:embed 202607160001_sub2api_user_unique.sql
var sub2APIUserUnique string

//go:embed 202607160002_primary_workspace.sql
var primaryWorkspace string

//go:embed 202607170001_invited_account_identity.sql
var invitedAccountIdentity string

//go:embed 202607170002_workspace_renewal.sql
var workspaceRenewal string

//go:embed 202607170003_workspace_auto_renew_audit.sql
var autoRenewAudit string

//go:embed 202607180001_customer_identity_hard_cut.sql
var customerIdentityHardCut string

//go:embed 202607190001_workspace_api_key_id.sql
var workspaceAPIKeyID string

//go:embed 202607190002_pilot_announcements.sql
var pilotAnnouncements string

//go:embed 202607230001_workspace_purchase_receipt_id.sql
var workspacePurchaseReceiptID string

//go:embed 202607240001_multi_workspace_pagination.sql
var multiWorkspacePagination string

//go:embed 202607250001_control_plane_capacity_indexes.sql
var controlPlaneCapacityIndexes string

//go:embed 202608170002_legacy_identity_table_custody.sql
var legacyIdentityTableCustody string

//go:embed 202608180001_workspace_purchase_eligibility.sql
var workspacePurchaseEligibility string

//go:embed 202609120001_workspace_application_binding.sql
var workspaceApplicationBinding string

//go:embed 202609120002_application_revisions.sql
var applicationRevisions string

//go:embed 202609120003_workspace_application_binding_version.sql
var workspaceApplicationBindingVersion string

//go:embed 202609130001_application_data_materials.sql
var applicationDataMaterials string

//go:embed 202609130001_workspace_application_selection.sql
var workspaceApplicationSelection string

func execMigration(ctx context.Context, driver Driver, query string) error {
	_, err := driver.ExecContext(ctx, query)
	return err
}

func execMigrationTx(ctx context.Context, driver Driver, query string) error {
	tx, err := driver.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, query); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func Apply(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, monthlyHardCut)
}

func ApplyCoreSchema(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, coreSchema)
}

func ApplySub2APIUserUniqueness(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, sub2APIUserUnique)
}

func ApplyPrimaryWorkspace(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, primaryWorkspace)
}

func ApplyInvitedAccountIdentity(ctx context.Context, driver Driver) error {
	return execMigrationTx(ctx, driver, invitedAccountIdentity)
}

func ApplyWorkspaceRenewal(ctx context.Context, driver Driver) error {
	return execMigrationTx(ctx, driver, workspaceRenewal)
}

func ApplyAutoRenewAudit(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, autoRenewAudit)
}

func ApplyCustomerIdentityHardCut(ctx context.Context, driver Driver) error {
	return execMigrationTx(ctx, driver, customerIdentityHardCut)
}

func ApplyWorkspaceAPIKeyID(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspaceAPIKeyID)
}

func ApplyPilotAnnouncements(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, pilotAnnouncements)
}

func ApplyWorkspacePurchaseReceiptID(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspacePurchaseReceiptID)
}

func ApplyMultiWorkspacePagination(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, multiWorkspacePagination)
}

func ApplyControlPlaneCapacityIndexes(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, controlPlaneCapacityIndexes)
}

func ApplyLegacyIdentityTableCustody(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, legacyIdentityTableCustody)
}

func ApplyWorkspacePurchaseEligibility(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspacePurchaseEligibility)
}

func ApplyWorkspaceApplicationBinding(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspaceApplicationBinding)
}

func ApplyApplicationRevisions(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, applicationRevisions)
}

func ApplyWorkspaceApplicationBindingVersion(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspaceApplicationBindingVersion)
}

func ApplyApplicationDataMaterials(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, applicationDataMaterials)
}

func ApplyWorkspaceApplicationSelection(ctx context.Context, driver Driver) error {
	return execMigration(ctx, driver, workspaceApplicationSelection)
}
