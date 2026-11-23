package server

import (
	"context"
	"errors"
)

var (
	computeEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("owner_user_id", "ownerUserId"),
		sqlTextField("workspace_id", "workspaceId"),
		sqlTextField("name", "name"),
		sqlTextField("package_id", "packageId"),
		sqlTextField("provider", "provider"),
		sqlTextField("provider_resource_id", "providerResourceId"),
		sqlTextField("provider_request_id", "providerRequestId"),
		sqlTextField("operation_id", "operationId"),
		sqlTextField("status", "status"),
		sqlTextField("desired_status", "desiredStatus"),
		sqlTextField("provider_status", "providerStatus"),
		sqlTextField("last_provider_sync_at", "lastProviderSyncAt"),
		sqlTextField("last_provider_sync_error", "lastProviderSyncError"),
		sqlTextField("external_deleted_at", "externalDeletedAt"),
		sqlTextField("billing_status", "billingStatus"),
		sqlTextField("billing_operation_id", "billingOperationId"),
		sqlBillingJSONField("billing_state_json"),
		sqlTextField("pricing_version", "pricingVersion"),
		sqlTextField("evidence_id", "evidenceId"),
		sqlTextField("cvm_instance_id", "cvmInstanceId"),
		sqlTextField("instance_id", "instanceId"),
		sqlFloatField("cpu", "cpu"),
		sqlFloatField("memory_gb", "memoryGb"),
		sqlFloatField("disk_gb", "diskGb"),
	}
	storageEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("owner_user_id", "ownerUserId"),
		sqlTextField("workspace_id", "workspaceId"),
		sqlTextField("name", "name"),
		sqlTextField("package_id", "packageId"),
		sqlTextField("provider", "provider"),
		sqlTextField("provider_resource_id", "providerResourceId"),
		sqlTextField("provider_request_id", "providerRequestId"),
		sqlTextField("operation_id", "operationId"),
		sqlTextField("status", "status"),
		sqlTextField("desired_status", "desiredStatus"),
		sqlTextField("provider_status", "providerStatus"),
		sqlTextField("last_provider_sync_at", "lastProviderSyncAt"),
		sqlTextField("last_provider_sync_error", "lastProviderSyncError"),
		sqlTextField("external_deleted_at", "externalDeletedAt"),
		sqlTextField("billing_status", "billingStatus"),
		sqlTextField("billing_operation_id", "billingOperationId"),
		sqlBillingJSONField("billing_state_json"),
		sqlTextField("pricing_version", "pricingVersion"),
		sqlTextField("mount_path", "mountPath"),
		sqlFloatField("size_gb", "sizeGb"),
	}
	attachmentEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("workspace_id", "workspaceId"),
		sqlTextField("compute_allocation_id", "computeAllocationId"),
		sqlTextField("storage_id", "storageId"),
		sqlTextField("volume_id", "volumeId"),
		sqlTextField("operation_id", "operationId"),
		sqlTextField("provider", "provider"),
		sqlTextField("provider_request_id", "providerRequestId"),
		sqlTextField("status", "status"),
		sqlTextField("mount_path", "mountPath"),
	}
)

func (s *postgresEntStateStore) listScopedSQLRecords(ctx context.Context, table string, fields []sqlRecordField, accountID string) ([]map[string]any, error) {
	suffix := ""
	var args []any
	if accountID != "" {
		suffix = "WHERE account_id = ?"
		args = []any{accountID}
	}
	rows, err := s.loadSQLRecordSet(ctx, s.db, table, fields, suffix, args...)
	if err != nil {
		return nil, err
	}
	return filteredRecords(rows, accountID)
}

func (s *postgresEntStateStore) ListComputes(ctx context.Context, accountID string) ([]map[string]any, error) {
	return s.listScopedSQLRecords(ctx, "control_plane_compute_allocations", computeEntFields, accountID)
}

func (s *postgresEntStateStore) GetCompute(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_compute_allocations", id, computeEntFields, false)
}

func (s *postgresEntStateStore) SaveCompute(ctx context.Context, row map[string]any) error {
	return s.saveResourcePreservingAutoRenew(ctx, "compute", row)
}

func (s *postgresEntStateStore) DeleteCompute(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_compute_allocations", id)
}

func (s *postgresEntStateStore) ListStorages(ctx context.Context, accountID string) ([]map[string]any, error) {
	return s.listScopedSQLRecords(ctx, "control_plane_storage_volumes", storageEntFields, accountID)
}

func (s *postgresEntStateStore) GetStorage(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_storage_volumes", id, storageEntFields, false)
}

func (s *postgresEntStateStore) SaveStorage(ctx context.Context, row map[string]any) error {
	return s.saveResourcePreservingAutoRenew(ctx, "storage", row)
}

func (s *postgresEntStateStore) saveResourcePreservingAutoRenew(ctx context.Context, resourceType string, row map[string]any) error {
	id := stringValue(row["id"])
	if id == "" {
		return errors.New("missing_record_id")
	}
	var table string
	var fields []sqlRecordField
	switch resourceType {
	case "compute":
		table, fields = "control_plane_compute_allocations", computeEntFields
	case "storage":
		table, fields = "control_plane_storage_volumes", storageEntFields
	default:
		return errors.New("invalid_billing_resource_type")
	}
	for range 4 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rollback := func(err error) error {
			_ = tx.Rollback()
			return err
		}
		current, found, err := s.getSQLRecord(ctx, tx, table, id, fields, true)
		if err != nil {
			return rollback(err)
		}
		if !found {
			createErr := s.insertSQLRecord(ctx, tx, table, id, row, fields)
			if isSQLConstraintError(createErr) {
				_ = tx.Rollback()
				continue
			}
			if createErr != nil {
				return rollback(createErr)
			}
			return tx.Commit()
		}
		if stringValue(current["accountId"]) != stringValue(row["accountId"]) {
			return rollback(errIdempotencyConflict)
		}
		saved := preserveResourceAutoRenew(current, row)
		if err := s.updateSQLRecord(ctx, tx, table, id, saved, fields, true); err != nil {
			return rollback(err)
		}
		return tx.Commit()
	}
	return errors.New("resource_save_retry_exhausted")
}

func (s *postgresEntStateStore) DeleteStorage(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_storage_volumes", id)
}

func (s *postgresEntStateStore) ListAttachments(ctx context.Context, accountID string) ([]map[string]any, error) {
	return s.listScopedSQLRecords(ctx, "control_plane_storage_attachments", attachmentEntFields, accountID)
}

func (s *postgresEntStateStore) GetAttachment(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_storage_attachments", id, attachmentEntFields, false)
}

func (s *postgresEntStateStore) SaveAttachment(ctx context.Context, row map[string]any) error {
	return s.replaceRecord(ctx, "control_plane_storage_attachments", row, attachmentEntFields)
}

func (s *postgresEntStateStore) DeleteAttachment(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_storage_attachments", id)
}
