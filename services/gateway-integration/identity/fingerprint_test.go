package identity

import (
	"testing"
)

// TestWalletRequestFingerprintIncludesObligationAndQuoteAcceptance verifies that
// obligationId and quoteAcceptanceId are part of the fingerprint calculation.
// Changes to these fields alone should produce different fingerprints.
func TestWalletRequestFingerprintIncludesObligationAndQuoteAcceptance(t *testing.T) {
	base := walletActionRequest{
		kind:              "charge",
		workspaceID:       "ws-123",
		businessKey:       "biz-key-1",
		amount:            1000000,
		obligation:        "obl-original",
		quoteAcceptanceID: "qa-original",
		original:          "orig-op-1",
		entitlement:       "",
		deletionReceipt:   "",
	}

	baseFingerprint := walletRequestFingerprint(base)

	// Test 1: Changing only obligationId produces different fingerprint
	differentObligation := base
	differentObligation.obligation = "obl-different"
	obligationFingerprint := walletRequestFingerprint(differentObligation)

	if baseFingerprint == obligationFingerprint {
		t.Errorf("Fingerprint should differ when obligation changes: base=%s, different=%s", baseFingerprint, obligationFingerprint)
	}

	// Test 2: Changing only quoteAcceptanceID produces different fingerprint
	differentQuoteAcceptance := base
	differentQuoteAcceptance.quoteAcceptanceID = "qa-different"
	quoteAcceptanceFingerprint := walletRequestFingerprint(differentQuoteAcceptance)

	if baseFingerprint == quoteAcceptanceFingerprint {
		t.Errorf("Fingerprint should differ when quoteAcceptanceID changes: base=%s, different=%s", baseFingerprint, quoteAcceptanceFingerprint)
	}

	// Test 3: Changing both fields produces different fingerprint
	differentBoth := base
	differentBoth.obligation = "obl-new"
	differentBoth.quoteAcceptanceID = "qa-new"
	bothFingerprint := walletRequestFingerprint(differentBoth)

	if baseFingerprint == bothFingerprint {
		t.Errorf("Fingerprint should differ when both obligation and quoteAcceptanceID change: base=%s, both=%s", baseFingerprint, bothFingerprint)
	}

	// Test 4: All three different fingerprints should be distinct
	if obligationFingerprint == quoteAcceptanceFingerprint || obligationFingerprint == bothFingerprint || quoteAcceptanceFingerprint == bothFingerprint {
		t.Errorf("All three variant fingerprints should be distinct: obl=%s, qa=%s, both=%s", obligationFingerprint, quoteAcceptanceFingerprint, bothFingerprint)
	}
}

// TestWalletRequestFingerprintStableForSameInputs verifies that identical
// requests produce identical fingerprints (idempotency property).
func TestWalletRequestFingerprintStableForSameInputs(t *testing.T) {
	req := walletActionRequest{
		kind:              "charge",
		workspaceID:       "ws-456",
		businessKey:       "biz-key-2",
		amount:            2000000,
		obligation:        "obl-stable",
		quoteAcceptanceID: "qa-stable",
		original:          "orig-op-2",
		entitlement:       "ent-1",
		deletionReceipt:   "",
	}

	fp1 := walletRequestFingerprint(req)
	fp2 := walletRequestFingerprint(req)

	if fp1 != fp2 {
		t.Errorf("Identical requests should produce identical fingerprints: fp1=%s, fp2=%s", fp1, fp2)
	}
}

// TestWalletRequestFingerprintAmountSensitivity verifies that amount changes
// still produce different fingerprints (existing behavior preserved).
func TestWalletRequestFingerprintAmountSensitivity(t *testing.T) {
	base := walletActionRequest{
		kind:              "charge",
		workspaceID:       "ws-789",
		businessKey:       "biz-key-3",
		amount:            1000000,
		obligation:        "obl-amt",
		quoteAcceptanceID: "qa-amt",
		original:          "",
		entitlement:       "",
		deletionReceipt:   "",
	}

	baseFingerprint := walletRequestFingerprint(base)

	differentAmount := base
	differentAmount.amount = 2000000
	amountFingerprint := walletRequestFingerprint(differentAmount)

	if baseFingerprint == amountFingerprint {
		t.Errorf("Fingerprint should differ when amount changes: base=%s, different=%s", baseFingerprint, amountFingerprint)
	}
}
