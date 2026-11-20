package contracts

import "fmt"

// CompositionFailureCode is the canonical code the one composed Cloud backend
// reports when it cannot run the owners the owner topology contract declares.
//
// An installation reads this code from the container's own output, so a
// failed rollout names the owner and the reason in one machine-readable line
// instead of leaving the operator with an exit code and no cause. The line
// carries no free text: the reason vocabulary lives here, so an installation
// can persist the code without persisting anything else the owner wrote.
type CompositionFailureCode string

const (
	// CompositionFailureOwnerTopologyRequired is the failure of an
	// installation that starts the composed backend without the owner
	// topology contract.
	CompositionFailureOwnerTopologyRequired CompositionFailureCode = "owner_topology_required"
	// CompositionFailureOwnerTopologyInvalid is the failure of an owner
	// topology contract the composition cannot read.
	CompositionFailureOwnerTopologyInvalid CompositionFailureCode = "owner_topology_invalid"
	// CompositionFailureOwnerPlanInvalid is the failure of a contract the
	// composition cannot turn into a runnable plan.
	CompositionFailureOwnerPlanInvalid CompositionFailureCode = "owner_plan_invalid"
	// CompositionFailureOwnerCertificateMissing is the failure of an
	// installation that asks for transport security without providing every
	// owner's certificate and key.
	CompositionFailureOwnerCertificateMissing CompositionFailureCode = "owner_certificate_material_missing"
	// CompositionFailureOwnerStartFailed is the failure of an owner process
	// the composition cannot start at all.
	CompositionFailureOwnerStartFailed CompositionFailureCode = "owner_start_failed"
	// CompositionFailureOwnerStartRefused is the failure of an owner process
	// that starts and then refuses to run: it exits before it satisfies its
	// readiness criterion.
	CompositionFailureOwnerStartRefused CompositionFailureCode = "owner_startup_refused"
	// CompositionFailureOwnerDependencyNotReady is the failure of an owner
	// whose dependency is not ready.
	CompositionFailureOwnerDependencyNotReady CompositionFailureCode = "owner_dependency_not_ready"
	// CompositionFailureOwnerReadinessAborted is the failure of an owner that
	// never became ready because the composition stopped first.
	CompositionFailureOwnerReadinessAborted CompositionFailureCode = "owner_readiness_aborted"
	// CompositionFailureOwnerReadinessTimeout is the failure of an owner that
	// did not satisfy its readiness criterion in time.
	CompositionFailureOwnerReadinessTimeout CompositionFailureCode = "owner_readiness_timeout"
	// CompositionFailureOwnerExited is the failure of an owner that exits
	// while the composition is running.
	CompositionFailureOwnerExited CompositionFailureCode = "owner_exited"
)

// CompositionFailureLineFormat is the one shape the composed backend prints a
// canonical failure in. The code comes from the vocabulary above and the
// owner is the owner the failure belongs to.
const CompositionFailureLineFormat = "opl-cloud-composition: failure code=%s owner=%s"

// FormatCompositionFailure renders the canonical failure line of the composed
// backend.
func FormatCompositionFailure(code CompositionFailureCode, owner string) string {
	return fmt.Sprintf(CompositionFailureLineFormat, code, owner)
}
