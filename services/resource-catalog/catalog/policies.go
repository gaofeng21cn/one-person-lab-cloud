package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// Price policy versions bind exact compute/storage plan pairs to approved
// one-month amounts. A combination exists only once an administrator publishes
// it: there is no fallback rate and no default.
func (s *Service) ListPricePolicyVersions(ctx context.Context, r *api.ListPricePolicyVersionsRpcRequest) (*api.PricePolicyVersionPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListPricePolicyVersions", true); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT id,version_label,compute_plan_id,storage_plan_id,compute_monthly_usd_micros,storage_monthly_usd_micros,product_monthly_usd_micros,currency,renewal_rules,valid_from,valid_until,created_at,period_months,plan_change_policy_version FROM resource_catalog.price_policy_versions WHERE id>$1 ORDER BY id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.PricePolicyVersionPage{}
	for rows.Next() {
		v, err := s.scanPricePolicy(rows)
		if err != nil {
			return nil, dbError(err)
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) scanPricePolicy(row rowScanner) (*api.PricePolicyVersion, error) {
	var (
		v                    api.PricePolicyVersion
		currency, policyName string
		rules                []byte
		validFrom, createdAt time.Time
		validUntil           sql.NullTime
	)
	if err := row.Scan(&v.Id, &v.VersionLabel, &v.ComputePlanId, &v.StoragePlanId, &v.ComputeMonthlyUsdMicros, &v.StorageMonthlyUsdMicros, &v.ProductMonthlyUsdMicros, &currency, &rules, &validFrom, &validUntil, &createdAt, &v.PeriodMonths, &policyName); err != nil {
		return nil, err
	}
	v.Currency = api.PricePolicyVersionCurrencyEnum(api.PricePolicyVersionCurrencyEnum_value["PRICE_POLICY_VERSION_CURRENCY_ENUM_"+upper(currency)])
	v.RenewalPolicy = &api.RenewalPolicy{}
	if err := publicjson.Unmarshal(rules, v.RenewalPolicy); err != nil {
		return nil, status.Error(codes.DataLoss, "stored renewal policy is not readable")
	}
	v.PlanChangePolicyVersion = api.PricePolicyVersionPlanChangePolicyVersionEnum(api.PricePolicyVersionPlanChangePolicyVersionEnum_value["PRICE_POLICY_VERSION_PLAN_CHANGE_POLICY_VERSION_ENUM_"+upper(policyName)])
	// The typed D17 policy is fixed by version, not stored per row: the frozen
	// policy travels with the version so a reader never re-derives rules.
	v.PlanChangePolicy = s.Policy.proto()
	v.ValidFrom = timestamppb.New(validFrom.UTC())
	if validUntil.Valid {
		v.ValidUntil = timestamppb.New(validUntil.Time.UTC())
	}
	v.CreatedAt = timestamppb.New(createdAt.UTC())
	return &v, nil
}

func (s *Service) CreatePricePolicyVersion(ctx context.Context, r *api.CreatePricePolicyVersionRpcRequest) (*api.PricePolicyVersion, error) {
	if err := s.auth(ctx, r.GetContext(), "CreatePricePolicyVersion", true); err != nil {
		return nil, err
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetVersionLabel()) || b.GetPeriodMonths() != 1 || b.GetComputePlanId() == "" || b.GetStoragePlanId() == "" ||
		b.GetComputeMonthlyUsdMicros() < 0 || b.GetStorageMonthlyUsdMicros() < 0 || b.GetProductMonthlyUsdMicros() < 0 || !tsValid(b.GetValidFrom()) {
		return nil, status.Error(codes.InvalidArgument, "price policy requires a label, periodMonths=1, a compute/storage plan pair, nonnegative amounts and validFrom")
	}
	if b.GetValidUntil() != nil && (b.GetValidUntil().CheckValid() != nil || !b.GetValidUntil().AsTime().After(b.GetValidFrom().AsTime())) {
		return nil, status.Error(codes.InvalidArgument, "validUntil must be after validFrom")
	}
	if b.GetPlanChangePolicyVersion() != api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1 {
		return nil, status.Error(codes.InvalidArgument, "the only approved plan-change policy is workspace-plan-change-v1")
	}
	if !validRenewalPolicy(b.GetRenewalPolicy(), b.GetPeriodMonths()) {
		return nil, status.Error(codes.InvalidArgument, "renewal policy must be the frozen one-month accepted-snapshot policy")
	}
	out := &api.PricePolicyVersion{}
	err := s.command(ctx, r.GetContext(), "CreatePricePolicyVersion", b, out, func(tx *sql.Tx) error {
		// The exact compute/storage pair must already be an approved plan pair; the
		// FK enforces existence and this read keeps a retired plan from being priced.
		if err := assertApprovedPlanPair(ctx, tx, b.ComputePlanId, b.StoragePlanId); err != nil {
			return err
		}
		out.Id = id("price")
		out.VersionLabel = b.VersionLabel
		out.ComputePlanId = b.ComputePlanId
		out.StoragePlanId = b.StoragePlanId
		out.ComputeMonthlyUsdMicros = b.ComputeMonthlyUsdMicros
		out.StorageMonthlyUsdMicros = b.StorageMonthlyUsdMicros
		out.ProductMonthlyUsdMicros = b.ProductMonthlyUsdMicros
		out.Currency = api.PricePolicyVersionCurrencyEnum_PRICE_POLICY_VERSION_CURRENCY_ENUM_USD
		out.PeriodMonths = b.PeriodMonths
		// The renewal rules the contract freezes for a one-month product period are
		// stored in public JSON vocabulary, because the DDL check reads the stored
		// document's own field names.
		out.RenewalPolicy = renewalPolicy()
		renewalRulesJSON, err := publicjson.Marshal(out.RenewalPolicy)
		if err != nil {
			return status.Error(codes.Internal, "encode renewal policy")
		}
		out.PlanChangePolicyVersion = api.PricePolicyVersionPlanChangePolicyVersionEnum_PRICE_POLICY_VERSION_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1
		out.PlanChangePolicy = s.Policy.proto()
		out.ValidFrom = timestamppb.New(b.ValidFrom.AsTime().UTC())
		if b.ValidUntil != nil {
			out.ValidUntil = timestamppb.New(b.ValidUntil.AsTime().UTC())
		}
		out.CreatedAt = timestamppb.Now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_catalog.price_policy_versions(id,version_label,compute_plan_id,storage_plan_id,compute_monthly_usd_micros,storage_monthly_usd_micros,product_monthly_usd_micros,currency,renewal_rules,valid_from,valid_until,published_by,created_at,period_months,plan_change_policy_version) VALUES($1,$2,$3,$4,$5,$6,$7,'USD',$8,$9,$10,$11,now(),$12,$13)`,
			out.Id, out.VersionLabel, out.ComputePlanId, out.StoragePlanId, out.ComputeMonthlyUsdMicros, out.StorageMonthlyUsdMicros, out.ProductMonthlyUsdMicros, renewalRulesJSON, b.ValidFrom.AsTime().UTC(), tsOrNil(b.ValidUntil), r.GetContext().GetActorId(), out.PeriodMonths, "workspace-plan-change-v1"); err != nil {
			return dbError(err)
		}
		// A new price version is a catalog policy change the contract publishes to
		// Workspace and Ledger; the event is written in the same transaction.
		return s.emitPolicyChanged(ctx, tx, r.GetContext(), out)
	})
	return out, err
}

func validRenewalPolicy(p *api.RenewalPolicy, months int32) bool {
	return p != nil &&
		p.GetVersion() == api.RenewalPolicyVersionEnum_RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1 &&
		p.GetTrigger() == api.RenewalPolicyTriggerEnum_RENEWAL_POLICY_TRIGGER_ENUM_MANUAL_OR_EXPLICITLY_CONSENTED_AUTOMATIC &&
		p.GetEffectiveStart() == api.RenewalPolicyEffectiveStartEnum_RENEWAL_POLICY_EFFECTIVE_START_ENUM_PREVIOUS_PAID_THROUGH &&
		p.GetMonths() == months &&
		p.GetUsesAcceptedPriceSnapshot()
}

func assertApprovedPlanPair(ctx context.Context, tx *sql.Tx, computePlanID, storagePlanID string) error {
	var computeStatus, storageStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM resource_catalog.compute_plans WHERE id=$1`, computePlanID).Scan(&computeStatus); err != nil {
		return dbError(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM resource_catalog.storage_plans WHERE id=$1`, storagePlanID).Scan(&storageStatus); err != nil {
		return dbError(err)
	}
	if computeStatus != "approved" || storageStatus != "approved" {
		return status.Error(codes.FailedPrecondition, "both plans in a price pair must be approved")
	}
	return nil
}

func (s *Service) ListRefundPolicyVersions(ctx context.Context, r *api.ListRefundPolicyVersionsRpcRequest) (*api.RefundPolicyVersionPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListRefundPolicyVersions", true); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT id,version_label,algorithm,retention_policy_version_id,customer_terms,valid_from,valid_until,created_at FROM resource_catalog.refund_policy_versions WHERE id>$1 ORDER BY id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.RefundPolicyVersionPage{}
	for rows.Next() {
		var (
			v                    api.RefundPolicyVersion
			algorithm            string
			validFrom, createdAt time.Time
			validUntil           sql.NullTime
		)
		if err := rows.Scan(&v.Id, &v.VersionLabel, &algorithm, &v.RetentionPolicyVersionId, &v.CustomerTerms, &validFrom, &validUntil, &createdAt); err != nil {
			return nil, dbError(err)
		}
		v.Algorithm = api.RefundPolicyVersionAlgorithmEnum(api.RefundPolicyVersionAlgorithmEnum_value["REFUND_POLICY_VERSION_ALGORITHM_ENUM_"+upper(algorithm)])
		v.ValidFrom = timestamppb.New(validFrom.UTC())
		if validUntil.Valid {
			v.ValidUntil = timestamppb.New(validUntil.Time.UTC())
		}
		v.CreatedAt = timestamppb.New(createdAt.UTC())
		out.Items = append(out.Items, &v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) CreateRefundPolicyVersion(ctx context.Context, r *api.CreateRefundPolicyVersionRpcRequest) (*api.RefundPolicyVersion, error) {
	if err := s.auth(ctx, r.GetContext(), "CreateRefundPolicyVersion", true); err != nil {
		return nil, err
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetVersionLabel()) || b.GetRetentionPolicyVersionId() == "" || b.GetCustomerTerms() == "" || !tsValid(b.GetValidFrom()) {
		return nil, status.Error(codes.InvalidArgument, "refund policy requires a label, retention policy, customer terms and validFrom")
	}
	if b.GetValidUntil() != nil && (b.GetValidUntil().CheckValid() != nil || !b.GetValidUntil().AsTime().After(b.GetValidFrom().AsTime())) {
		return nil, status.Error(codes.InvalidArgument, "validUntil must be after validFrom")
	}
	if b.GetAlgorithm() != api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1 {
		return nil, status.Error(codes.InvalidArgument, "the only approved refund algorithm is workspace-delete-refund-v1")
	}
	out := &api.RefundPolicyVersion{}
	err := s.command(ctx, r.GetContext(), "CreateRefundPolicyVersion", b, out, func(tx *sql.Tx) error {
		if err := assertRetentionVersion(ctx, tx, b.RetentionPolicyVersionId); err != nil {
			return err
		}
		out.Id = id("refund")
		out.VersionLabel = b.VersionLabel
		out.Algorithm = api.RefundPolicyVersionAlgorithmEnum_REFUND_POLICY_VERSION_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1
		out.RetentionPolicyVersionId = b.RetentionPolicyVersionId
		out.CustomerTerms = b.CustomerTerms
		out.ValidFrom = timestamppb.New(b.ValidFrom.AsTime().UTC())
		if b.ValidUntil != nil {
			out.ValidUntil = timestamppb.New(b.ValidUntil.AsTime().UTC())
		}
		out.CreatedAt = timestamppb.Now()
		// The refund rules are the frozen workspace-delete-refund-v1 algorithm,
		// recorded as a typed document so a reader never reinvents a ratio.
		rules, err := json.Marshal(deleteRefundRules())
		if err != nil {
			return status.Error(codes.Internal, "encode refund rules")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO resource_catalog.refund_policy_versions(id,version_label,rules,customer_terms,valid_from,valid_until,published_by,created_at,algorithm,retention_policy_version_id) VALUES($1,$2,$3,$4,$5,$6,$7,now(),$8,$9)`,
			out.Id, out.VersionLabel, rules, out.CustomerTerms, b.ValidFrom.AsTime().UTC(), tsOrNil(b.ValidUntil), r.GetContext().GetActorId(), "workspace-delete-refund-v1", out.RetentionPolicyVersionId)
		return dbError(err)
	})
	return out, err
}

// deleteRefundRules documents the fixed workspace-delete-refund-v1 algorithm:
// usedHours=ceil((deletedAt-resourceFulfilledAt)/1h) and
// refund=floor(originalPlatformChargeUSDMicros*max(720-usedHours,0)/720), using
// integer/rational arithmetic only. It is recorded, not re-derived per quote.
func deleteRefundRules() map[string]any {
	return map[string]any{
		"version":           "workspace-delete-refund-v1",
		"unit":              "usd_micros",
		"usedHoursRounding": "ceil",
		"refundRounding":    "floor",
		"freeHours":         720,
	}
}

func assertRetentionVersion(ctx context.Context, tx *sql.Tx, retentionPolicyVersionID string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT true FROM resource_catalog.retention_policy_versions WHERE id=$1`, retentionPolicyVersionID).Scan(&exists); err != nil {
		return dbError(err)
	}
	return nil
}

func (s *Service) ListRetentionPolicyVersions(ctx context.Context, r *api.ListRetentionPolicyVersionsRpcRequest) (*api.RetentionPolicyVersionPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListRetentionPolicyVersions", true); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT id,version_label,workspace_data_disposition,package_history_disposition,build_history_disposition,tenant_restore_days,customer_terms,created_at FROM resource_catalog.retention_policy_versions WHERE id>$1 ORDER BY id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.RetentionPolicyVersionPage{}
	for rows.Next() {
		var (
			v              api.RetentionPolicyVersion
			ws, pkg, build string
			createdAt      time.Time
		)
		if err := rows.Scan(&v.Id, &v.VersionLabel, &ws, &pkg, &build, &v.TenantRestoreDays, &v.CustomerTerms, &createdAt); err != nil {
			return nil, dbError(err)
		}
		v.WorkspaceDataDisposition = api.RetentionPolicyVersionWorkspaceDataDispositionEnum(api.RetentionPolicyVersionWorkspaceDataDispositionEnum_value["RETENTION_POLICY_VERSION_WORKSPACE_DATA_DISPOSITION_ENUM_"+upper(ws)])
		v.PackageHistoryDisposition = api.RetentionPolicyVersionPackageHistoryDispositionEnum(api.RetentionPolicyVersionPackageHistoryDispositionEnum_value["RETENTION_POLICY_VERSION_PACKAGE_HISTORY_DISPOSITION_ENUM_"+upper(pkg)])
		v.BuildHistoryDisposition = api.RetentionPolicyVersionBuildHistoryDispositionEnum(api.RetentionPolicyVersionBuildHistoryDispositionEnum_value["RETENTION_POLICY_VERSION_BUILD_HISTORY_DISPOSITION_ENUM_"+upper(build)])
		v.CreatedAt = timestamppb.New(createdAt.UTC())
		out.Items = append(out.Items, &v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) CreateRetentionPolicyVersion(ctx context.Context, r *api.CreateRetentionPolicyVersionRpcRequest) (*api.RetentionPolicyVersion, error) {
	if err := s.auth(ctx, r.GetContext(), "CreateRetentionPolicyVersion", true); err != nil {
		return nil, err
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetVersionLabel()) || b.GetCustomerTerms() == "" {
		return nil, status.Error(codes.InvalidArgument, "retention policy requires a label and customer terms")
	}
	out := &api.RetentionPolicyVersion{}
	err := s.command(ctx, r.GetContext(), "CreateRetentionPolicyVersion", b, out, func(tx *sql.Tx) error {
		out.Id = id("retention")
		out.VersionLabel = b.VersionLabel
		out.WorkspaceDataDisposition = api.RetentionPolicyVersionWorkspaceDataDispositionEnum_RETENTION_POLICY_VERSION_WORKSPACE_DATA_DISPOSITION_ENUM_DESTROY_AFTER_CONFIRMED_DELETION
		out.PackageHistoryDisposition = api.RetentionPolicyVersionPackageHistoryDispositionEnum_RETENTION_POLICY_VERSION_PACKAGE_HISTORY_DISPOSITION_ENUM_RETAIN
		out.BuildHistoryDisposition = api.RetentionPolicyVersionBuildHistoryDispositionEnum_RETENTION_POLICY_VERSION_BUILD_HISTORY_DISPOSITION_ENUM_RETAIN
		out.TenantRestoreDays = 15
		out.CustomerTerms = b.CustomerTerms
		out.CreatedAt = timestamppb.Now()
		// Retention has no per-row valid_from in the request; the contract fixes a
		// single effective version created now, recorded with an effective timestamp.
		rules, err := json.Marshal(map[string]any{
			"version":                   "retention-policy/v1",
			"workspaceDataDisposition":  "destroy_after_confirmed_deletion",
			"packageHistoryDisposition": "retain",
			"buildHistoryDisposition":   "retain",
			"tenantRestoreDays":         15,
		})
		if err != nil {
			return status.Error(codes.Internal, "encode retention rules")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO resource_catalog.retention_policy_versions(id,version_label,rules,customer_terms,valid_from,valid_until,published_by,created_at,workspace_data_disposition,package_history_disposition,build_history_disposition,tenant_restore_days) VALUES($1,$2,$3,$4,now(),NULL,$5,now(),'destroy_after_confirmed_deletion','retain','retain',15)`,
			out.Id, out.VersionLabel, rules, out.CustomerTerms, r.GetContext().GetActorId())
		return dbError(err)
	})
	return out, err
}
