package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// quoteValidity is the offer lifetime this owner grants every quote. The contract
// requires a definite expiry the customer can see, not a per-caller value, so the
// owner names one.
const quoteValidity = 30 * time.Minute

// priceResolution is the exact effective version set for one approved plan pair at
// one instant. A pair with no published price has no resolution and cannot be
// quoted; there is no fallback rate.
type priceResolution struct {
	Price     *api.PricePolicyVersion
	Refund    *api.RefundPolicyVersion
	Retention *api.RetentionPolicyVersion
}

// resolvePrice selects the single effective price, refund and retention version
// for one approved pair. The latest version whose valid_from is not after the
// instant wins; an expired version is never replaced by an older one, because
// silently falling back would reprice an offer.
func (s *Service) resolvePrice(ctx context.Context, tx *sql.Tx, computePlanID, storagePlanID string, at time.Time) (*priceResolution, error) {
	resolution := &priceResolution{}
	var err error
	if resolution.Price, err = s.effectivePricePolicy(ctx, tx, computePlanID, storagePlanID, at); err != nil {
		return nil, err
	}
	if resolution.Refund, err = s.effectiveRefundPolicy(ctx, tx, at); err != nil {
		return nil, err
	}
	if resolution.Retention, err = s.effectiveRetentionPolicy(ctx, tx, at); err != nil {
		return nil, err
	}
	return resolution, nil
}

func (s *Service) effectivePricePolicy(ctx context.Context, tx *sql.Tx, computePlanID, storagePlanID string, at time.Time) (*api.PricePolicyVersion, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,version_label,compute_plan_id,storage_plan_id,compute_monthly_usd_micros,storage_monthly_usd_micros,product_monthly_usd_micros,currency,renewal_rules,valid_from,valid_until,created_at,period_months,plan_change_policy_version FROM resource_catalog.price_policy_versions WHERE compute_plan_id=$1 AND storage_plan_id=$2 AND valid_from<=$3 ORDER BY valid_from DESC,id DESC LIMIT 1`, computePlanID, storagePlanID, at)
	version, err := s.scanPricePolicy(row)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the exact compute and storage plan pair has no approved price")
	}
	if version.GetValidUntil() != nil && !at.Before(version.GetValidUntil().AsTime()) {
		return nil, status.Error(codes.FailedPrecondition, "the effective price version has expired; a new price version is required")
	}
	return version, nil
}

func (s *Service) effectiveRefundPolicy(ctx context.Context, tx *sql.Tx, at time.Time) (*api.RefundPolicyVersion, error) {
	v, err := scanRefundPolicy(tx.QueryRowContext(ctx, `SELECT id,version_label,algorithm,retention_policy_version_id,customer_terms,valid_from,valid_until,created_at FROM resource_catalog.refund_policy_versions WHERE valid_from<=$1 ORDER BY valid_from DESC,id DESC LIMIT 1`, at))
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved refund policy is effective")
	}
	if v.GetValidUntil() != nil && !at.Before(v.GetValidUntil().AsTime()) {
		return nil, status.Error(codes.FailedPrecondition, "the effective refund policy has expired; a new version is required")
	}
	return v, nil
}

func (s *Service) effectiveRetentionPolicy(ctx context.Context, tx *sql.Tx, at time.Time) (*api.RetentionPolicyVersion, error) {
	return scanRetentionPolicy(tx.QueryRowContext(ctx, `SELECT id,version_label,workspace_data_disposition,package_history_disposition,build_history_disposition,tenant_restore_days,customer_terms,created_at FROM resource_catalog.retention_policy_versions WHERE valid_from<=$1 ORDER BY valid_from DESC,id DESC LIMIT 1`, at))
}

// quoteSnapshot is the immutable basis a quote is bound to. It is stored so a
// later read explains exactly what was priced without consulting current tables.
type quoteSnapshot struct {
	ComputePlanID            string                    `json:"computePlanId"`
	StoragePlanID            string                    `json:"storagePlanId"`
	PricePolicyVersionID     string                    `json:"pricePolicyVersionId"`
	RefundPolicyVersionID    string                    `json:"refundPolicyVersionId"`
	RetentionPolicyVersionID string                    `json:"retentionPolicyVersionId"`
	PeriodMonths             int32                     `json:"periodMonths"`
	PricingBasisUnixMillis   int64                     `json:"pricingBasisUnixMillis"`
	ResourcePlan             *api.ResourcePlanSnapshot `json:"resourcePlan,omitempty"`
	AcceptedWorkspaceID      string                    `json:"acceptedWorkspaceId,omitempty"`
}

// CreateQuote prices one deploy request exactly once and stores the immutable
// offer. This owner charges nothing, reserves no capacity and writes no
// subscription obligation: it fixes the price and the policy versions the
// customer will later accept.
//
// A resize or renew request is refused with the gap named. Those variants depend
// on a Workspace subscription, a paid period and an accepted plan change, none of
// which exists yet, and inventing them here would be a second subscription model.
func (s *Service) CreateQuote(ctx context.Context, r *api.CreateQuoteRpcRequest) (*api.Quote, error) {
	if err := s.auth(ctx, r.GetContext(), "CreateQuote", false); err != nil {
		return nil, err
	}
	request := r.GetBody()
	if err := validateDeployQuoteRequest(request); err != nil {
		return nil, err
	}
	out := &api.Quote{}
	err := s.command(ctx, r.GetContext(), "CreateQuote", request, out, func(tx *sql.Tx) error {
		// The original response wins before current catalog admission or pricing:
		// a retry must not lose its quote when a plan or policy later expires.
		now := time.Now().UTC()
		resolution, err := s.resolvePrice(ctx, tx, request.ComputePlanId, request.StoragePlanId, now)
		if err != nil {
			return err
		}
		if err := assertAvailablePlanPair(ctx, tx, request.ComputePlanId, request.StoragePlanId, now); err != nil {
			return err
		}
		plan, err := frozenResourcePlan(ctx, tx, request.ComputePlanId, request.StoragePlanId, request.PeriodMonths)
		if err != nil {
			return err
		}
		periodStart, periodEnd := now.UTC(), now.UTC().AddDate(0, 1, 0)
		lines, total, err := pricedLineItems(resolution.Price)
		if err != nil {
			return err
		}
		tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
		snapshot, err := json.Marshal(quoteSnapshot{
			ComputePlanID:            request.ComputePlanId,
			StoragePlanID:            request.StoragePlanId,
			PricePolicyVersionID:     resolution.Price.Id,
			RefundPolicyVersionID:    resolution.Refund.Id,
			RetentionPolicyVersionID: resolution.Retention.Id,
			PeriodMonths:             request.PeriodMonths,
			PricingBasisUnixMillis:   now.UnixMilli(),
			ResourcePlan:             plan,
		})
		if err != nil {
			return status.Error(codes.Internal, "encode the quote admission snapshot")
		}
		models, err := json.Marshal(modelSelections(request.ModelSelections))
		if err != nil {
			return status.Error(codes.Internal, "encode the quote model selections")
		}
		out.Id = id("quote")
		out.Purpose = api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY
		out.ComputePlanId = request.ComputePlanId
		out.StoragePlanId = request.StoragePlanId
		out.ModelSelections = request.ModelSelections
		out.PeriodMonths = request.PeriodMonths
		out.PeriodStart = timestamppb.New(periodStart)
		out.PeriodEnd = timestamppb.New(periodEnd)
		out.PricePolicyVersionId = resolution.Price.Id
		out.RefundPolicyVersionId = resolution.Refund.Id
		out.RetentionPolicyVersionId = resolution.Retention.Id
		out.RefundTerms = resolution.Refund.CustomerTerms
		out.RetentionTerms = resolution.Retention.CustomerTerms
		out.ExpectedInterruption = "none_before_execution"
		out.RuntimeReadbackRequirement = api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED
		out.LineItems = lines
		out.TotalUsdMicros = total
		out.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED
		out.ExpiresAt = timestamppb.New(now.Add(quoteValidity))
		out.CreatedAt = timestamppb.Now()
		if request.GetCapabilityVersionId() != "" {
			out.CapabilityVersionId = proto.String(request.GetCapabilityVersionId())
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_catalog.quotes(id,tenant_id,actor_id,purpose,capability_version_id,compute_plan_id,storage_plan_id,price_policy_version_id,refund_policy_version_id,retention_policy_version_id,total_usd_micros,period_start,period_end,status,input_digest,admission_snapshot,expires_at,created_at,model_selections,period_months,refund_terms,retention_terms,expected_interruption) VALUES($1,$2,$3,'deploy',$4,$5,$6,$7,$8,$9,$10,$11,$12,'offered',$13,$14,$15,now(),$16,$17,$18,$19,$20)`,
			out.Id, tenantID, r.GetContext().GetActorId(), nullable(request.GetCapabilityVersionId()), out.ComputePlanId, out.StoragePlanId,
			out.PricePolicyVersionId, out.RefundPolicyVersionId, out.RetentionPolicyVersionId, out.TotalUsdMicros,
			periodStart, periodEnd, quoteInputDigest(request, snapshot), snapshot, now.Add(quoteValidity),
			models, out.PeriodMonths, out.RefundTerms, out.RetentionTerms, out.ExpectedInterruption); err != nil {
			return dbError(err)
		}
		return insertQuoteItems(ctx, tx, out)
	})
	return out, err
}

// validateDeployQuoteRequest admits the deploy variant this slice implements. The
// other variants are refused with the specific reason rather than answered with a
// deploy-shaped offer.
func validateDeployQuoteRequest(request *api.QuoteRequest) error {
	if request == nil {
		return status.Error(codes.InvalidArgument, "a quote request is required")
	}
	switch request.GetPurpose() {
	case api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_RESIZE, api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_RENEW:
		return status.Error(codes.Unimplemented, "this slice prices only the deploy variant; resize and renew depend on a Workspace subscription, paid period and accepted plan change that do not exist yet")
	case api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY:
	default:
		return status.Error(codes.InvalidArgument, "purpose must be deploy, resize or renew")
	}
	if request.GetWorkspaceId() != "" {
		return status.Error(codes.InvalidArgument, "a deploy quote is not bound to an existing workspace")
	}
	if request.GetCapabilityVersionId() == "" {
		return status.Error(codes.InvalidArgument, "a deploy quote requires capabilityVersionId")
	}
	if request.GetComputePlanId() == "" || request.GetStoragePlanId() == "" {
		return status.Error(codes.InvalidArgument, "computePlanId and storagePlanId are required")
	}
	if request.GetPeriodMonths() != 1 {
		return status.Error(codes.InvalidArgument, "periodMonths must be 1")
	}
	if request.GetScheduledPlanChangeId() != "" {
		return status.Error(codes.InvalidArgument, "scheduledPlanChangeId is not valid for the deploy variant")
	}
	return nil
}

// Quote admission uses the same availability as the customer plan list. Hold
// both plan rows until the offer commits so retirement cannot race admission.
func assertAvailablePlanPair(ctx context.Context, tx *sql.Tx, computePlanID, storagePlanID string, at time.Time) error {
	var computeStatus, storageStatus string
	var computeFrom, storageFrom time.Time
	var computeUntil, storageUntil sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT c.status,c.valid_from,c.valid_until,s.status,s.valid_from,s.valid_until FROM resource_catalog.compute_plans c JOIN resource_catalog.storage_plans s ON s.id=$2 WHERE c.id=$1 FOR SHARE OF c,s`, computePlanID, storagePlanID).
		Scan(&computeStatus, &computeFrom, &computeUntil, &storageStatus, &storageFrom, &storageUntil)
	if err != nil {
		return dbError(err)
	}
	compute, err := availability(computeStatus, computeFrom, nullOrZero(computeUntil), at)
	if err != nil {
		return err
	}
	storage, err := availability(storageStatus, storageFrom, nullOrZero(storageUntil), at)
	if err != nil {
		return err
	}
	if compute != "available" || storage != "available" {
		return status.Error(codes.FailedPrecondition, "both plans in a quote must be available at pricing time")
	}
	return nil
}

// pricedLineItems builds the offer lines and the total under the contract money
// rule: total = sum(compute, storage, product) minus sum(adjustment_credit), every
// line non-negative, the additions checked so a pair of lines cannot wrap.
func pricedLineItems(price *api.PricePolicyVersion) ([]*api.QuoteLine, int64, error) {
	lines := make([]*api.QuoteLine, 0, 3)
	add := func(kind api.QuoteLineKindEnum, description string, amount int64) {
		if amount == 0 {
			return
		}
		lines = append(lines, &api.QuoteLine{Kind: kind, Description: description, Quantity: 1, AmountUsdMicros: amount})
	}
	add(api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, "compute monthly", price.GetComputeMonthlyUsdMicros())
	add(api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_STORAGE, "storage monthly", price.GetStorageMonthlyUsdMicros())
	add(api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_PRODUCT, "product monthly", price.GetProductMonthlyUsdMicros())
	total, err := sumQuoteLines(lines)
	if err != nil {
		return nil, 0, err
	}
	return lines, total, nil
}

func sumQuoteLines(lines []*api.QuoteLine) (int64, error) {
	var positive, credit int64
	for _, line := range lines {
		amount := line.GetAmountUsdMicros()
		if amount < 0 {
			return 0, status.Error(codes.InvalidArgument, "a quote line amount must not be negative")
		}
		var err error
		if line.GetKind() == api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_ADJUSTMENT_CREDIT {
			credit, err = checkedQuoteAdd(credit, amount)
		} else {
			positive, err = checkedQuoteAdd(positive, amount)
		}
		if err != nil {
			return 0, err
		}
	}
	if credit > positive {
		return 0, status.Error(codes.InvalidArgument, "quote credits exceed the charges they offset")
	}
	return positive - credit, nil
}

func checkedQuoteAdd(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, status.Error(codes.InvalidArgument, "the quote total exceeds the int64 micro-dollar range")
	}
	return left + right, nil
}

func modelSelections(selections []*api.ModelSelection) []map[string]string {
	out := make([]map[string]string, 0, len(selections))
	for _, selection := range selections {
		out = append(out, map[string]string{"slot": selection.GetSlot(), "modelId": selection.GetModelId()})
	}
	return out
}

// quoteInputDigest binds the offer to its exact input so an acceptance can prove
// it accepted this offer and not another with the same plan pair.
func quoteInputDigest(request *api.QuoteRequest, snapshot []byte) string {
	material, _ := json.Marshal([]any{
		request.GetPurpose().String(), request.GetComputePlanId(), request.GetStoragePlanId(),
		request.GetCapabilityVersionId(), request.GetPeriodMonths(), modelSelections(request.GetModelSelections()),
		json.RawMessage(snapshot),
	})
	return digest(material)
}

func insertQuoteItems(ctx context.Context, tx *sql.Tx, quote *api.Quote) error {
	for index, line := range quote.LineItems {
		calculation, err := json.Marshal(map[string]any{"quantity": line.Quantity})
		if err != nil {
			return status.Error(codes.Internal, "encode a quote line")
		}
		var credit any
		if line.CreditSource != nil {
			raw, err := protojson.Marshal(line.CreditSource)
			if err != nil {
				return status.Error(codes.Internal, "encode a quote credit source")
			}
			credit = raw
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_catalog.quote_items(id,quote_id,kind,description,amount_usd_micros,calculation,sort_order,created_at,quantity,credit_source) VALUES($1,$2,$3,$4,$5,$6,$7,now(),$8,$9)`,
			id("line"), quote.Id, quoteLineKind(line.GetKind()), line.GetDescription(), line.GetAmountUsdMicros(), calculation, index, line.GetQuantity(), credit); err != nil {
			return dbError(err)
		}
	}
	return nil
}

// quoteLineKind is the stored vocabulary, which the contract fixes as the public
// enum text rather than the protobuf name.
func quoteLineKind(kind api.QuoteLineKindEnum) string {
	switch kind {
	case api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE:
		return "compute"
	case api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_STORAGE:
		return "storage"
	case api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_PRODUCT:
		return "product"
	case api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_ADJUSTMENT_CREDIT:
		return "adjustment_credit"
	default:
		return ""
	}
}

// GetQuote reads one offer the caller's own tenant owns. Another tenant's offer is
// simply absent rather than resolved. An offer past its expiry is reported as
// expired; the stored status is not rewritten by a read.
func (s *Service) GetQuote(ctx context.Context, r *api.GetQuoteRpcRequest) (*api.Quote, error) {
	if err := s.auth(ctx, r.GetContext(), "GetQuote", false); err != nil {
		return nil, err
	}
	if r.GetQuoteId() == "" {
		return nil, status.Error(codes.InvalidArgument, "quoteId is required")
	}
	quote, err := s.readQuote(ctx, r.GetQuoteId(), r.GetContext().GetScope().GetTenant().GetTenantId())
	if err != nil {
		return nil, err
	}
	if quote.Status == api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED && !time.Now().UTC().Before(quote.ExpiresAt.AsTime()) {
		quote.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_EXPIRED
	}
	return quote, nil
}

func (s *Service) readQuote(ctx context.Context, quoteID, tenantID string) (*api.Quote, error) {
	var (
		quote                                             api.Quote
		purpose, storedState                              string
		workspaceID, capabilityVersionID, scheduledChange sql.NullString
		models, admission                                 []byte
		periodStart, periodEnd, expiresAt, createdAt      time.Time
		sourceSubscriptionVersion                         sql.NullInt64
		planChange                                        []byte
	)
	err := s.DB.QueryRowContext(ctx, `SELECT id,purpose,workspace_id,capability_version_id,compute_plan_id,storage_plan_id,model_selections,period_months,period_start,period_end,price_policy_version_id,refund_policy_version_id,retention_policy_version_id,refund_terms,retention_terms,expected_interruption,total_usd_micros,status,admission_snapshot,expires_at,created_at,source_subscription_version,plan_change_calculation,scheduled_plan_change_id FROM resource_catalog.quotes WHERE id=$1 AND tenant_id=$2`, quoteID, tenantID).
		Scan(&quote.Id, &purpose, &workspaceID, &capabilityVersionID, &quote.ComputePlanId, &quote.StoragePlanId, &models, &quote.PeriodMonths, &periodStart, &periodEnd, &quote.PricePolicyVersionId, &quote.RefundPolicyVersionId, &quote.RetentionPolicyVersionId, &quote.RefundTerms, &quote.RetentionTerms, &quote.ExpectedInterruption, &quote.TotalUsdMicros, &storedState, &admission, &expiresAt, &createdAt, &sourceSubscriptionVersion, &planChange, &scheduledChange)
	if err != nil {
		return nil, dbError(err)
	}
	quote.Purpose = api.QuotePurposeEnum(api.QuotePurposeEnum_value["QUOTE_PURPOSE_ENUM_"+upper(purpose)])
	quote.Status = api.QuoteStatusEnum(api.QuoteStatusEnum_value["QUOTE_STATUS_ENUM_"+upper(storedState)])
	quote.PeriodStart = timestamppb.New(periodStart.UTC())
	quote.PeriodEnd = timestamppb.New(periodEnd.UTC())
	quote.ExpiresAt = timestamppb.New(expiresAt.UTC())
	quote.CreatedAt = timestamppb.New(createdAt.UTC())
	if workspaceID.Valid {
		quote.WorkspaceId = proto.String(workspaceID.String)
	}
	if capabilityVersionID.Valid {
		quote.CapabilityVersionId = proto.String(capabilityVersionID.String)
	}
	if scheduledChange.Valid {
		quote.ScheduledPlanChangeId = proto.String(scheduledChange.String)
	}
	if sourceSubscriptionVersion.Valid {
		value := sourceSubscriptionVersion.Int64
		quote.SourceSubscriptionVersion = &value
	}
	if len(planChange) > 0 {
		quote.PlanChangeCalculation = &api.PlanChangeCalculation{}
		if err := protojson.Unmarshal(planChange, quote.PlanChangeCalculation); err != nil {
			return nil, status.Error(codes.DataLoss, "the stored plan change calculation is not readable")
		}
	}
	var selections []map[string]string
	if err := json.Unmarshal(models, &selections); err != nil {
		return nil, status.Error(codes.DataLoss, "the stored model selections are not readable")
	}
	for _, selection := range selections {
		quote.ModelSelections = append(quote.ModelSelections, &api.ModelSelection{Slot: selection["slot"], ModelId: selection["modelId"]})
	}
	var snapshot quoteSnapshot
	if err := json.Unmarshal(admission, &snapshot); err != nil {
		return nil, status.Error(codes.DataLoss, "the stored quote admission snapshot is not readable")
	}
	quote.RuntimeReadbackRequirement = runtimeReadbackRequirementFor(snapshot, quote.GetCapabilityVersionId())
	if quote.LineItems, err = s.readQuoteLines(ctx, quoteID); err != nil {
		return nil, err
	}
	return &quote, nil
}

// runtimeReadbackRequirement is derived from the stored admission basis, not from
// the caller: a quote that names a capability version will deliver an agent and
// therefore needs a runtime readback, while a resource-only quote does not.
func runtimeReadbackRequirementFor(_ quoteSnapshot, capabilityVersionID string) api.QuoteRuntimeReadbackRequirementEnum {
	if capabilityVersionID == "" {
		return api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_NOT_APPLICABLE
	}
	return api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED
}

func (s *Service) readQuoteLines(ctx context.Context, quoteID string) ([]*api.QuoteLine, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT kind,description,quantity,amount_usd_micros,credit_source FROM resource_catalog.quote_items WHERE quote_id=$1 ORDER BY sort_order`, quoteID)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	var lines []*api.QuoteLine
	for rows.Next() {
		var (
			line      api.QuoteLine
			kind      string
			creditRaw []byte
		)
		if err := rows.Scan(&kind, &line.Description, &line.Quantity, &line.AmountUsdMicros, &creditRaw); err != nil {
			return nil, dbError(err)
		}
		line.Kind = api.QuoteLineKindEnum(api.QuoteLineKindEnum_value["QUOTE_LINE_KIND_ENUM_"+upper(kind)])
		if len(creditRaw) > 0 {
			credit := &api.CreditSource{}
			if err := protojson.Unmarshal(creditRaw, credit); err != nil {
				return nil, status.Error(codes.DataLoss, "the stored quote credit source is not readable")
			}
			line.CreditSource = credit
		}
		lines = append(lines, &line)
	}
	return lines, dbError(rows.Err())
}
