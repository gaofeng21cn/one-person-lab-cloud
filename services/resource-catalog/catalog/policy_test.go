package catalog

import (
	"encoding/json"
	"os"
	"testing"
)

// TestUpgradeChargeMatchesD17Vectors replays the contract's independent D17
// acceptance vectors through this owner's real calculator. A vector that the
// calculator cannot satisfy is a real product-policy defect, not a fixture detail,
// so the test fails instead of skipping.
func TestUpgradeChargeMatchesD17Vectors(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/spec/target/checks/d17_acceptance_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		PolicyVersion string `json:"policyVersion"`
		Cases         []struct {
			Name                  string `json:"name"`
			OldMonthlyUSDMicros   string `json:"oldMonthlyUSDMicros"`
			NewMonthlyUSDMicros   string `json:"newMonthlyUSDMicros"`
			PeriodMilliseconds    string `json:"periodMilliseconds"`
			RemainingMilliseconds string `json:"remainingMilliseconds"`
			ExpectedSupplement    string `json:"expectedSupplementUSDMicros"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.PolicyVersion != defaultPlanChangePolicy().version {
		t.Fatalf("vector policy version %q differs from the implemented %q", document.PolicyVersion, defaultPlanChangePolicy().version)
	}
	for _, vector := range document.Cases {
		t.Run(vector.Name, func(t *testing.T) {
			got, ok := upgradeChargeMicros(parseMicros(t, vector.OldMonthlyUSDMicros), parseMicros(t, vector.NewMonthlyUSDMicros), parseMicros(t, vector.PeriodMilliseconds), parseMicros(t, vector.RemainingMilliseconds))
			if !ok {
				t.Fatalf("calculator rejected a valid D17 vector")
			}
			want := parseMicros(t, vector.ExpectedSupplement)
			if got != want {
				t.Fatalf("charge = %d, want %d", got, want)
			}
		})
	}
}

// TestUpgradeChargeRejectsInvalidWindows proves the calculator refuses a window
// the policy does not define instead of inventing a charge: T must sit inside the
// paid period.
func TestUpgradeChargeRejectsInvalidWindows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		period    int64
		remaining int64
	}{
		{"zero period", 0, 100},
		{"zero remaining", 100, 0},
		{"remaining beyond period", 100, 101},
		{"negative remaining", 100, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := upgradeChargeMicros(1, 2, tc.period, tc.remaining); ok {
				t.Fatal("an invalid paid-period window was accepted")
			}
		})
	}
}

// TestUpgradeChargeRoundsOnceUpward proves the single final ceil: 3 micros of
// delta over 1 of 3 remaining milliseconds yields exactly 1 micro, not a
// double-rounded 1 from a per-component ceil.
func TestUpgradeChargeRoundsOnceUpward(t *testing.T) {
	got, ok := upgradeChargeMicros(0, 2, 3, 1)
	if !ok || got != 1 {
		t.Fatalf("charge = %d (ok=%v), want 1", got, ok)
	}
}

// TestUpgradeChargeFreeIncrementHasNoNegativeCredit proves a target price at or
// below the source price yields zero, never a negative credit.
func TestUpgradeChargeFreeIncrementHasNoNegativeCredit(t *testing.T) {
	got, ok := upgradeChargeMicros(40_000_000, 30_000_000, 2_592_000_000, 1_296_000_000)
	if !ok || got != 0 {
		t.Fatalf("charge = %d (ok=%v), want 0", got, ok)
	}
}

// TestSupplementDeleteRefundFloorsAndCaps proves the deletion refund floors a
// single time and never exceeds the confirmed supplement.
func TestSupplementDeleteRefundFloorsAndCaps(t *testing.T) {
	// Half of the coverage remaining on a 10_000_000 micro supplement.
	got, ok := supplementDeleteRefundMicros(10_000_000, 2_592_000_000, 1_296_000_000)
	if !ok || got != 5_000_000 {
		t.Fatalf("refund = %d (ok=%v), want 5000000", got, ok)
	}
	// Before coverage: the full supplement, never more.
	got, ok = supplementDeleteRefundMicros(10_000_000, 2_592_000_000, 0)
	if !ok || got != 10_000_000 {
		t.Fatalf("refund = %d (ok=%v), want 10000000", got, ok)
	}
	// After coverage: zero, and an out-of-range window is rejected.
	if got, ok = supplementDeleteRefundMicros(10_000_000, 2_592_000_000, 2_592_000_000); !ok || got != 0 {
		t.Fatalf("refund = %d (ok=%v), want 0", got, ok)
	}
	if _, ok := supplementDeleteRefundMicros(10_000_000, 100, 101); ok {
		t.Fatal("an elapsed-beyond-coverage window was accepted")
	}
}

func parseMicros(t *testing.T, value string) int64 {
	t.Helper()
	var parsed int64
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}
