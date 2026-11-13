package contracts

import (
	"errors"
	"testing"
)

func TestBakedProductSourceSHAReportsTheStampedRevision(t *testing.T) {
	previous := ProductSourceSHA
	t.Cleanup(func() { ProductSourceSHA = previous })

	ProductSourceSHA = "  0123456789abcdef0123456789abcdef01234567  "
	if got := BakedProductSourceSHA(); got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("baked product source sha=%q", got)
	}
}

func TestVerifyProductSourceSHARefusesAnUnstampedBinary(t *testing.T) {
	previous := ProductSourceSHA
	ProductSourceSHA = ""
	t.Cleanup(func() { ProductSourceSHA = previous })

	if err := VerifyProductSourceSHA("0123456789abcdef0123456789abcdef01234567"); !errors.Is(err, ErrProductSourceSHANotStamped) {
		t.Fatalf("unstamped binary error=%v, want %v", err, ErrProductSourceSHANotStamped)
	}
	if ProductSourceSHAIsCurrent("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("an unstamped binary must not count as current")
	}
}

func TestVerifyProductSourceSHARefusesAnUnclaimedDeployment(t *testing.T) {
	previous := ProductSourceSHA
	ProductSourceSHA = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { ProductSourceSHA = previous })

	for _, claimed := range []string{"", "   "} {
		if err := VerifyProductSourceSHA(claimed); !errors.Is(err, ErrProductSourceSHANotClaimed) {
			t.Fatalf("unclaimed deployment error=%v, want %v", err, ErrProductSourceSHANotClaimed)
		}
	}
}

func TestVerifyProductSourceSHAAcceptsTheSameRevision(t *testing.T) {
	previous := ProductSourceSHA
	ProductSourceSHA = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { ProductSourceSHA = previous })

	if err := VerifyProductSourceSHA("0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatalf("matching revision error=%v", err)
	}
	if !ProductSourceSHAIsCurrent("  0123456789abcdef0123456789abcdef01234567  ") {
		t.Fatal("a matching revision must count as current")
	}
}

func TestVerifyProductSourceSHARefusesADriftedRevision(t *testing.T) {
	previous := ProductSourceSHA
	ProductSourceSHA = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { ProductSourceSHA = previous })

	if err := VerifyProductSourceSHA("fedcba9876543210fedcba9876543210fedcba98"); !errors.Is(err, ErrProductSourceSHAMismatch) {
		t.Fatalf("drifted revision error=%v, want %v", err, ErrProductSourceSHAMismatch)
	}
	if ProductSourceSHAIsCurrent("fedcba9876543210fedcba9876543210fedcba98") {
		t.Fatal("a drifted revision must not count as current")
	}
}

func TestClaimedProductSourceSHAReadsTheDeploymentClaim(t *testing.T) {
	t.Setenv(ProductSourceSHAEnv, "  fedcba9876543210fedcba9876543210fedcba98  ")
	if got := ClaimedProductSourceSHA(); got != "fedcba9876543210fedcba9876543210fedcba98" {
		t.Fatalf("claimed product source sha=%q", got)
	}
}
