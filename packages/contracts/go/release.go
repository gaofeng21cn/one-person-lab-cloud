package contracts

import (
	"errors"
	"os"
	"strings"
)

// ProductSourceSHAEnv is the deployment's claim about which product revision
// is running.
const ProductSourceSHAEnv = "OPL_RELEASE_SHA"

// ProductSourceSHA is the product revision this binary was built from. The
// build stamps it with
//
//	-ldflags "-X opl-cloud/packages/contracts/go.ProductSourceSHA=<sha>"
//
// and the Dockerfile takes that value from the same commit the image label
// names, so a running binary states its own revision instead of only
// repeating what the deployment claims about it.
var ProductSourceSHA = ""

func init() {
	// Keep the stamped value reachable from linked code. The linker drops
	// unreferenced variables, and -X can only set a symbol that survived into
	// the binary, so every binary that imports this package has to reach this
	// variable from somewhere it runs.
	_ = ProductSourceSHA
}

var (
	// ErrProductSourceSHANotStamped reports a binary that carries no build
	// stamp, so it cannot state which revision it is.
	ErrProductSourceSHANotStamped = errors.New("product_source_sha_not_stamped")
	// ErrProductSourceSHANotClaimed reports a deployment that claims no
	// product revision for the running binary.
	ErrProductSourceSHANotClaimed = errors.New("product_source_sha_not_claimed")
	// ErrProductSourceSHAMismatch reports a running binary and a deployment
	// that disagree about the product revision.
	ErrProductSourceSHAMismatch = errors.New("product_source_sha_mismatch")
)

// BakedProductSourceSHA reports the product revision this binary was built
// from, or an empty string when the binary carries no stamp.
func BakedProductSourceSHA() string {
	return strings.TrimSpace(ProductSourceSHA)
}

// ClaimedProductSourceSHA reports the product revision the deployment claims
// for the running binary.
func ClaimedProductSourceSHA() string {
	return strings.TrimSpace(os.Getenv(ProductSourceSHAEnv))
}

// VerifyProductSourceSHA fails when the running binary cannot state the same
// product revision the deployment claims.
//
// A deployment claim is operator-supplied text. Comparing the claim with
// another claim only proves the two claims agree. Comparing it with the
// revision baked into the running binary is what makes the running binary
// attributable to the image it was deployed from.
//
// The claimed revision is a parameter because a process may hold its
// environment in a map rather than reading the process environment directly.
func VerifyProductSourceSHA(claimed string) error {
	baked := BakedProductSourceSHA()
	if baked == "" {
		return ErrProductSourceSHANotStamped
	}
	claimed = strings.TrimSpace(claimed)
	if claimed == "" {
		return ErrProductSourceSHANotClaimed
	}
	if baked != claimed {
		return ErrProductSourceSHAMismatch
	}
	return nil
}

// ProductSourceSHAIsCurrent reports whether the running binary and the
// deployment claim the same product revision.
func ProductSourceSHAIsCurrent(claimed string) bool {
	return VerifyProductSourceSHA(claimed) == nil
}
