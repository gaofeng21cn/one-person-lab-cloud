package catalog

import (
	"math/big"

	api "opl-cloud/packages/contracts/go/api"
)

// planChangePolicy is the fixed, user-approved D17 product policy. It is not a
// configurable formula: the spec freezes every rule and the arithmetic lives in
// this package so no other owner can reinterpret it.
type planChangePolicy struct {
	version string
}

func defaultPlanChangePolicy() *planChangePolicy {
	return &planChangePolicy{version: "workspace-plan-change-v1"}
}

// proto renders the typed policy that travels with every price policy version.
func (p *planChangePolicy) proto() *api.PlanChangePolicy {
	return &api.PlanChangePolicy{
		Version:                       api.PlanChangePolicyVersionEnum_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1,
		ApprovalStatus:                api.PlanChangePolicyApprovalStatusEnum_PLAN_CHANGE_POLICY_APPROVAL_STATUS_ENUM_APPROVED,
		Upgrade:                       upgradeRules(),
		Downgrade:                     downgradeRules(),
		Classification:                api.PlanChangePolicyClassificationEnum_PLAN_CHANGE_POLICY_CLASSIFICATION_ENUM_APPROVED_COMPARABLE_RESOURCES_NOT_PRICE_OR_SKU_NAME,
		MixedOrIncomparableTransition: api.PlanChangePolicyMixedOrIncomparableTransitionEnum_PLAN_CHANGE_POLICY_MIXED_OR_INCOMPARABLE_TRANSITION_ENUM_REJECT,
		NoOpTransition:                api.PlanChangePolicyNoOpTransitionEnum_PLAN_CHANGE_POLICY_NO_OP_TRANSITION_ENUM_REJECT,
		StorageShrink:                 api.PlanChangePolicyStorageShrinkEnum_PLAN_CHANGE_POLICY_STORAGE_SHRINK_ENUM_REJECT_UNLESS_OWNER_SUPPORTED_TRANSITION,
		Concurrency:                   api.PlanChangePolicyConcurrencyEnum_PLAN_CHANGE_POLICY_CONCURRENCY_ENUM_ONE_UNFINISHED_PLAN_CHANGE_PER_WORKSPACE,
		CancelAndReplace:              api.PlanChangePolicyCancelAndReplaceEnum_PLAN_CHANGE_POLICY_CANCEL_AND_REPLACE_ENUM_EXPLICIT_CANCEL_THEN_NEW_QUOTE,
		BaseRefundPolicy:              api.PlanChangePolicyBaseRefundPolicyEnum_PLAN_CHANGE_POLICY_BASE_REFUND_POLICY_ENUM_PRESERVE_ORIGINAL_BASE_ORDER_POLICY,
		ProviderExecutionPlan:         api.PlanChangePolicyProviderExecutionPlanEnum_PLAN_CHANGE_POLICY_PROVIDER_EXECUTION_PLAN_ENUM_APPROVED_STRATEGY_FROZEN_BEFORE_QUOTE,
	}
}

func upgradeRules() *api.UpgradePlanRules {
	return &api.UpgradePlanRules{
		Kind:                          api.UpgradePlanRulesKindEnum_UPGRADE_PLAN_RULES_KIND_ENUM_UPGRADE_IMMEDIATE,
		EffectiveWhen:                 api.UpgradePlanRulesEffectiveWhenEnum_UPGRADE_PLAN_RULES_EFFECTIVE_WHEN_ENUM_RESOURCE_AND_RUNTIME_READBACK_CONFIRMED,
		PreservePaidPeriod:            true,
		OldPriceSource:                api.UpgradePlanRulesOldPriceSourceEnum_UPGRADE_PLAN_RULES_OLD_PRICE_SOURCE_ENUM_ACCEPTED_APPLIED_SUBSCRIPTION_PLAN,
		ChargeClock:                   api.UpgradePlanRulesChargeClockEnum_UPGRADE_PLAN_RULES_CHARGE_CLOCK_ENUM_QUOTE_PRICING_BASIS_AT_FROZEN_ON_ACCEPTANCE,
		TimeUnit:                      api.UpgradePlanRulesTimeUnitEnum_UPGRADE_PLAN_RULES_TIME_UNIT_ENUM_UTC_INTEGER_MILLISECONDS,
		ChargeRounding:                api.UpgradePlanRulesChargeRoundingEnum_UPGRADE_PLAN_RULES_CHARGE_ROUNDING_ENUM_CEIL_ONCE_USD_MICRO,
		ZeroCharge:                    api.UpgradePlanRulesZeroChargeEnum_UPGRADE_PLAN_RULES_ZERO_CHARGE_ENUM_RECORD_EVIDENCE_SKIP_GATEWAY,
		KnownFailureCompensation:      api.UpgradePlanRulesKnownFailureCompensationEnum_UPGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_FULL_UNREFUNDED_ORIGINAL_SUPPLEMENT,
		UnknownOutcome:                api.UpgradePlanRulesUnknownOutcomeEnum_UPGRADE_PLAN_RULES_UNKNOWN_OUTCOME_ENUM_READ_ORIGINAL_ACTION_NO_REFUND,
		IrreversibleResidualCostOwner: api.UpgradePlanRulesIrreversibleResidualCostOwnerEnum_UPGRADE_PLAN_RULES_IRREVERSIBLE_RESIDUAL_COST_OWNER_ENUM_PLATFORM,
		SupplementDeleteRefund:        api.UpgradePlanRulesSupplementDeleteRefundEnum_UPGRADE_PLAN_RULES_SUPPLEMENT_DELETE_REFUND_ENUM_FLOOR_UNUSED_SUPPLEMENT_COVERAGE,
	}
}

func downgradeRules() *api.DowngradePlanRules {
	return &api.DowngradePlanRules{
		Kind:                               api.DowngradePlanRulesKindEnum_DOWNGRADE_PLAN_RULES_KIND_ENUM_DOWNGRADE_NEXT_PERIOD,
		PlannedBoundary:                    api.DowngradePlanRulesPlannedBoundaryEnum_DOWNGRADE_PLAN_RULES_PLANNED_BOUNDARY_ENUM_ORIGINAL_PAID_THROUGH,
		CurrentPeriodRefund:                api.DowngradePlanRulesCurrentPeriodRefundEnum_DOWNGRADE_PLAN_RULES_CURRENT_PERIOD_REFUND_ENUM_NONE,
		NextPeriodPrice:                    api.DowngradePlanRulesNextPeriodPriceEnum_DOWNGRADE_PLAN_RULES_NEXT_PERIOD_PRICE_ENUM_ACCEPTED_TARGET_PLAN_QUOTE,
		RequiresConfirmedNextPeriodPayment: true,
		CancelBefore:                       api.DowngradePlanRulesCancelBeforeEnum_DOWNGRADE_PLAN_RULES_CANCEL_BEFORE_ENUM_NEXT_PERIOD_PAYMENT_OBLIGATION_ACCEPTED,
		EarlyPaidChange:                    api.DowngradePlanRulesEarlyPaidChangeEnum_DOWNGRADE_PLAN_RULES_EARLY_PAID_CHANGE_ENUM_DO_NOT_REDUCE_RESOURCES_BEFORE_ORIGINAL_PAID_THROUGH,
		ManualUnpaidBoundary:               api.DowngradePlanRulesManualUnpaidBoundaryEnum_DOWNGRADE_PLAN_RULES_MANUAL_UNPAID_BOUNDARY_ENUM_AWAITING_PAYMENT_SUSPEND_UNPAID_USAGE,
		KnownFailureCompensation:           api.DowngradePlanRulesKnownFailureCompensationEnum_DOWNGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_FULL_UNREFUNDED_TARGET_PERIOD_CHARGE,
		Fallback:                           api.DowngradePlanRulesFallbackEnum_DOWNGRADE_PLAN_RULES_FALLBACK_ENUM_NONE_NO_OLD_PRICE_RENEWAL_OR_SKU_SUBSTITUTION,
	}
}

// renewalPolicy is the frozen one-month renewal rule the contract stores on every
// price policy version. It uses the accepted price snapshot and the original
// paid-through boundary; it never silently reprices.
func renewalPolicy() *api.RenewalPolicy {
	return &api.RenewalPolicy{
		Version:                   api.RenewalPolicyVersionEnum_RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1,
		Trigger:                   api.RenewalPolicyTriggerEnum_RENEWAL_POLICY_TRIGGER_ENUM_MANUAL_OR_EXPLICITLY_CONSENTED_AUTOMATIC,
		EffectiveStart:            api.RenewalPolicyEffectiveStartEnum_RENEWAL_POLICY_EFFECTIVE_START_ENUM_PREVIOUS_PAID_THROUGH,
		Months:                    1,
		UsesAcceptedPriceSnapshot: true,
	}
}

// upgradeChargeMicros computes the D17 immediate-upgrade supplement:
//
//	Delta      = max(Pnew-Pold, 0)
//	charge     = ceil(Delta * R / D)
//
// where D is the actual paid-period length in milliseconds and R the remaining
// milliseconds. The multiplication and division use arbitrary-precision
// integers and the single rounding step happens last, so a large monthly delta
// cannot overflow and cannot be double-rounded. Callers must already have
// validated 0 < R <= D; invalid windows are refused before this runs.
func upgradeChargeMicros(oldMonthly, newMonthly, periodMillis, remainingMillis int64) (int64, bool) {
	if periodMillis <= 0 || remainingMillis <= 0 || remainingMillis > periodMillis {
		return 0, false
	}
	delta := newMonthly - oldMonthly
	if delta <= 0 {
		return 0, true
	}
	numerator := new(big.Int).Mul(big.NewInt(delta), big.NewInt(remainingMillis))
	divisor := big.NewInt(periodMillis)
	quotient, remainder := new(big.Int).QuoRem(numerator, divisor, new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

// supplementDeleteRefundMicros computes the D17 supplement refund on deletion:
//
//	floor(confirmedSupplement * max(E-deleteAt, 0) / (E-T))
//
// It floors a single time and never exceeds the original confirmed supplement.
func supplementDeleteRefundMicros(confirmed, coverageMillis, elapsedMillis int64) (int64, bool) {
	if coverageMillis <= 0 || elapsedMillis < 0 || elapsedMillis > coverageMillis || confirmed < 0 {
		return 0, false
	}
	remaining := coverageMillis - elapsedMillis
	numerator := new(big.Int).Mul(big.NewInt(confirmed), big.NewInt(remaining))
	refund := new(big.Int).Quo(numerator, big.NewInt(coverageMillis))
	if !refund.IsInt64() {
		return 0, false
	}
	return refund.Int64(), true
}
