package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// derivedID is the deterministic owner-local identity one original order fixes
// for the rows it creates. Every writer and every replay of the same order
// therefore names the same subscription and period, so an interrupted delivery
// resumes the original rows instead of inserting a second entitlement.
func derivedID(prefix string, parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return prefix + hex.EncodeToString(sum[:16])
}

// billingSubjectRef is the billing subject this Workspace's subscription belongs
// to. The Gateway Integration owner holds the authoritative tenant wallet
// binding; the current typed contract exposes no Workspace-to-Gateway binding
// readback, so the reference this owner can itself prove is its verified tenant
// scope. It is a tenant-scoped reference and never a fabricated Gateway binding
// id.
func billingSubjectRef(op ownerstore.Operation) string {
	return "tenant:" + op.TenantID
}

// activatePaidOrder completes the paid order's entitlement: one quoted
// subscription, its first immutable confirmed period, the Workspace becoming
// active and the original operation reaching its terminal confirmed state. It
// runs only after Serve reported its own readiness and route activation for the
// exact original runtime, and it commits every write in one owner transaction so
// no reader can observe an entitlement without its serving runtime, or the
// reverse.
func (s *Service) activatePaidOrder(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, result *orderResult, observed *api.RuntimeReadback) error {
	evidence, err := result.paidFundingEvidence()
	if err != nil {
		return err
	}
	if err = validatePaidFundingEvidence(op, accepted, evidence); err != nil {
		return err
	}
	quote, charge, receipt := accepted.GetQuote(), evidence.GetWalletOperation(), evidence.GetReceipt()
	periodStart, periodEnd := quote.GetPeriodStart(), quote.GetPeriodEnd()
	if periodStart == nil || periodEnd == nil || periodStart.CheckValid() != nil || periodEnd.CheckValid() != nil ||
		!periodEnd.AsTime().After(periodStart.AsTime()) || quote.GetPeriodMonths() <= 0 ||
		quote.GetPricePolicyVersionId() == "" || quote.GetTotalUsdMicros() <= 0 {
		return status.Error(codes.DataLoss, "the accepted quote cannot define a confirmed paid period")
	}
	// Readiness is Serve's own owner fact: it proves the deployment, the
	// application probe and the route this Workspace now entitles. A readback
	// without those facts is not an activation.
	if observed.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || observed.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED ||
		!observed.GetProcessReady() || !observed.GetApplicationAvailable() || observed.GetReadinessReceiptId() == "" {
		return status.Error(codes.DataLoss, "Serve has not confirmed the application this entitlement activates")
	}
	snapshot, err := protojson.Marshal(accepted)
	if err != nil {
		return status.Error(codes.Internal, "the accepted quote snapshot cannot be encoded")
	}
	subscriptionID, periodID := derivedID("subscription-", op.ID), derivedID("period-", op.ID)
	billingKey := "billing:" + op.ID
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace result cannot be encoded")
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	// The entitlement is one owner transaction fenced by the same worker lease as
	// every other step. A late worker cannot activate an order whose lease it
	// already lost, and an interrupted pass leaves either all or none of the rows.
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscriptions
		(id, workspace_id, accepted_quote_id, accepted_quote_snapshot, billing_subject_ref,
		 current_period_start, current_period_end, last_charge_wallet_operation_id, period_months,
		 provenance, renewal_mode, renewal_settings_version, version,
		 current_price_policy_version_id, current_monthly_usd_micros, billing_anchor_day)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'quoted','manual',0,0,$10,$11,$12)
		ON CONFLICT DO NOTHING`,
		subscriptionID, op.ResourceID, quote.GetId(), snapshot, billingSubjectRef(op),
		periodStart.AsTime().UTC(), periodEnd.AsTime().UTC(), charge.GetId(), quote.GetPeriodMonths(),
		quote.GetPricePolicyVersionId(), quote.GetTotalUsdMicros(), int(periodStart.AsTime().UTC().Day())); err != nil {
		return dbError(err)
	}
	if err = validateStoredSubscription(ctx, tx, subscriptionID, op, accepted, charge); err != nil {
		return err
	}
	// The confirmed period is append-only history; a replay with the same
	// original identities inserts nothing, and the readback below still has to
	// equal this order's exact charge receipt.
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscription_periods
		(id, subscription_id, quote_id, accepted_quote_snapshot, period_start, period_end,
		 billing_key, charge_wallet_operation_id, charge_receipt_id, provenance)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'quoted')
		ON CONFLICT DO NOTHING`,
		periodID, subscriptionID, quote.GetId(), snapshot, periodStart.AsTime().UTC(), periodEnd.AsTime().UTC(),
		billingKey, charge.GetId(), receipt.GetId()); err != nil {
		return dbError(err)
	}
	if err = validateStoredPeriod(ctx, tx, periodID, subscriptionID, billingKey, accepted, charge, receipt.GetId()); err != nil {
		return err
	}
	activated, err := tx.ExecContext(ctx, `UPDATE workspace.workspaces SET status='active',version=version+1,updated_at=now()
		WHERE id=$1 AND active_operation_id=$2 AND status='provisioning'`, op.ResourceID, op.ID)
	if err != nil {
		return dbError(err)
	}
	if changed, err := activated.RowsAffected(); err != nil {
		return dbError(err)
	} else if changed != 1 {
		return status.Error(codes.Aborted, "the Workspace this order activates is not its own provisioning target")
	}
	completed, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$3,status='succeeded',stage='succeeded',observation_result='confirmed',error_code=NULL,updated_at=now(),completed_at=now()
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token, raw)
	if err = fenced(completed, err); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

// validateStoredSubscription reads the subscription this order owns and refuses a
// row that does not name the original accepted quote, the confirmed charge and
// the exact accepted period. An existing row that differs is unreadable evidence,
// never something to overwrite: the entitlement is immutable history.
func validateStoredSubscription(ctx context.Context, tx *sql.Tx, subscriptionID string, op ownerstore.Operation, accepted *api.QuoteAcceptance, charge *api.WalletOperation) error {
	quote := accepted.GetQuote()
	var storedID, workspaceID, quoteID, subjectRef, chargeOpID, provenance, renewalMode, pricePolicy string
	var snapshot []byte
	var periodStart, periodEnd time.Time
	var months, monthlyMicros, anchorDay int64
	err := tx.QueryRowContext(ctx, `SELECT id, workspace_id, accepted_quote_id, accepted_quote_snapshot, billing_subject_ref,
		current_period_start, current_period_end, last_charge_wallet_operation_id, period_months, provenance, renewal_mode,
		current_price_policy_version_id, current_monthly_usd_micros, billing_anchor_day
		FROM workspace.subscriptions WHERE id=$1`, subscriptionID).
		Scan(&storedID, &workspaceID, &quoteID, &snapshot, &subjectRef, &periodStart, &periodEnd, &chargeOpID, &months, &provenance, &renewalMode, &pricePolicy, &monthlyMicros, &anchorDay)
	if err != nil {
		return dbError(err)
	}
	readback := &api.QuoteAcceptance{}
	if protojson.Unmarshal(snapshot, readback) != nil || !proto.Equal(readback, accepted) {
		return status.Error(codes.DataLoss, "the stored subscription is not this order's accepted quote")
	}
	expectedStart, expectedEnd := quote.GetPeriodStart().AsTime().UTC(), quote.GetPeriodEnd().AsTime().UTC()
	if storedID != subscriptionID || workspaceID != op.ResourceID || quoteID != quote.GetId() || subjectRef != billingSubjectRef(op) ||
		chargeOpID != charge.GetId() || months != int64(quote.GetPeriodMonths()) || provenance != "quoted" || renewalMode != "manual" ||
		pricePolicy != quote.GetPricePolicyVersionId() || monthlyMicros != quote.GetTotalUsdMicros() ||
		anchorDay != int64(expectedStart.Day()) || !periodStart.UTC().Equal(expectedStart) || !periodEnd.UTC().Equal(expectedEnd) {
		return status.Error(codes.DataLoss, "the stored subscription differs from this order's confirmed paid period")
	}
	return nil
}

// validateStoredPeriod reads the first confirmed period and refuses a row that
// does not name the exact original charge receipt this order's funding proof
// recorded. The period row is append-only, so a mismatch is a defect to surface,
// not a value to repair.
func validateStoredPeriod(ctx context.Context, tx *sql.Tx, periodID, subscriptionID, billingKey string, accepted *api.QuoteAcceptance, charge *api.WalletOperation, receiptID string) error {
	quote := accepted.GetQuote()
	var storedID, storedSubscription, quoteID, storedBillingKey, chargeOpID, storedReceipt, provenance string
	var snapshot []byte
	var periodStart, periodEnd time.Time
	err := tx.QueryRowContext(ctx, `SELECT id, subscription_id, quote_id, accepted_quote_snapshot, period_start, period_end,
		billing_key, charge_wallet_operation_id, charge_receipt_id, provenance
		FROM workspace.subscription_periods WHERE id=$1`, periodID).
		Scan(&storedID, &storedSubscription, &quoteID, &snapshot, &periodStart, &periodEnd, &storedBillingKey, &chargeOpID, &storedReceipt, &provenance)
	if err != nil {
		return dbError(err)
	}
	readback := &api.QuoteAcceptance{}
	if protojson.Unmarshal(snapshot, readback) != nil || !proto.Equal(readback, accepted) {
		return status.Error(codes.DataLoss, "the stored subscription period is not this order's accepted quote")
	}
	expectedStart, expectedEnd := quote.GetPeriodStart().AsTime().UTC(), quote.GetPeriodEnd().AsTime().UTC()
	if storedID != periodID || storedSubscription != subscriptionID || quoteID != quote.GetId() || storedBillingKey != billingKey ||
		chargeOpID != charge.GetId() || storedReceipt != receiptID || provenance != "quoted" ||
		!periodStart.UTC().Equal(expectedStart) || !periodEnd.UTC().Equal(expectedEnd) {
		return status.Error(codes.DataLoss, "the stored subscription period differs from this order's confirmed charge receipt")
	}
	return nil
}
