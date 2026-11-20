package contracts

import "testing"

// TestCompositionFailureLineFormat proves the canonical failure line has the
// one shape an installation reads: the code from the vocabulary and the owner
// the failure belongs to, and nothing else.
func TestCompositionFailureLineFormat(t *testing.T) {
	line := FormatCompositionFailure(CompositionFailureOwnerStartRefused, "workspace")
	if line != "opl-cloud-composition: failure code=owner_startup_refused owner=workspace" {
		t.Fatalf("the canonical failure line must have the published shape, got %q", line)
	}
}

// TestCompositionFailureCodesAreDistinct proves every code in the vocabulary
// has its own spelling, so an installation can tell two reasons apart.
func TestCompositionFailureCodesAreDistinct(t *testing.T) {
	codes := []CompositionFailureCode{
		CompositionFailureOwnerTopologyRequired,
		CompositionFailureOwnerTopologyInvalid,
		CompositionFailureOwnerPlanInvalid,
		CompositionFailureOwnerCertificateMissing,
		CompositionFailureOwnerStartFailed,
		CompositionFailureOwnerStartRefused,
		CompositionFailureOwnerDependencyNotReady,
		CompositionFailureOwnerReadinessAborted,
		CompositionFailureOwnerReadinessTimeout,
		CompositionFailureOwnerExited,
	}
	seen := map[CompositionFailureCode]bool{}
	for _, code := range codes {
		if code == "" {
			t.Fatal("every canonical code must have a spelling")
		}
		if seen[code] {
			t.Fatalf("the canonical code %q is declared twice", code)
		}
		seen[code] = true
	}
}
