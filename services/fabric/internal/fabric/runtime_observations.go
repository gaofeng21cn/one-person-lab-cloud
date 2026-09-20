package fabric

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// Provider discovery starts with physical workloads; registration only enriches
// the result. An absent Fabric record must never erase a discovered workload.
type runtimeProviderObservation struct {
	contracts.RuntimeObservation
	operationID          string
	serviceName          string
	bindingValid         bool
	desiredFromOperation bool
	legacyReady          bool
	// application marks a deployment of an admitted workspace application
	// revision, declared by its publisher manifest labels. Its ownership is
	// resolved against the durable workspace_application_runtime record, never
	// against the legacy WorkspaceRuntime shape.
	application  bool
	componentName string
}

type runtimeObservationsProvider interface {
	readRuntimeObservations(context.Context) ([]runtimeProviderObservation, error)
}

func (s *Service) RuntimeObservations(ctx context.Context) (contracts.RuntimeObservations, error) {
	provider := s.optionalProviders.runtimeObservations
	if provider == nil {
		return contracts.RuntimeObservations{}, fmt.Errorf("runtime_observations_unavailable")
	}
	readCtx, cancel := context.WithTimeout(ctx, runtimeHealthSummaryTimeout)
	defer cancel()
	discovered, err := provider.readRuntimeObservations(readCtx)
	if err != nil {
		return contracts.RuntimeObservations{}, err
	}
	result := contracts.RuntimeObservations{Items: make([]contracts.RuntimeObservation, 0, len(discovered))}
	seenObjects := map[string]bool{}
	ownersByWorkspace := map[string][]FabricOperation{}
	applicationOwnersByWorkspace := map[string][]FabricOperation{}
	for _, item := range discovered {
		if item.ObjectRef == "" || seenObjects[item.ObjectRef] {
			return contracts.RuntimeObservations{}, fmt.Errorf("runtime_observations_identity_invalid")
		}
		seenObjects[item.ObjectRef] = true
		item.Ownership = contracts.RuntimeOwnershipConflict
		if !item.bindingValid {
			item.ReasonCode = "runtime_binding_conflict"
		} else if item.application {
			owners, cached := applicationOwnersByWorkspace[item.WorkspaceID]
			if !cached {
				owners, err = s.runtimeRead.applicationOwners.WorkspaceApplicationRuntimeOwnerCandidates(readCtx, item.WorkspaceID)
				if err != nil {
					return contracts.RuntimeObservations{}, err
				}
				applicationOwnersByWorkspace[item.WorkspaceID] = owners
			}
			item.Ownership, item.ReasonCode = verifyApplicationRuntimeOwner(owners, item)
		} else {
			owners, cached := ownersByWorkspace[item.WorkspaceID]
			if !cached {
				owners, err = s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(readCtx, item.WorkspaceID)
				if err != nil {
					if errors.Is(err, ErrLaunchStageBindingConflict) {
						item.ReasonCode = "runtime_owner_conflict"
						result.Items = append(result.Items, item.RuntimeObservation)
						continue
					}
					return contracts.RuntimeObservations{}, err
				}
				ownersByWorkspace[item.WorkspaceID] = owners
			}
			switch len(owners) {
			case 0:
				item.Ownership = contracts.RuntimeOwnershipUnregistered
			case 1:
				var runtime WorkspaceRuntime
				owner := owners[0]
				if owner.AccountID == item.AccountID && decodeOperationResource(owner, &runtime) && runtime.WorkspaceID == item.WorkspaceID && runtime.ID == item.RuntimeID && runtime.OperationID == item.operationID && runtime.ServiceName == item.serviceName {
					item.Ownership = contracts.RuntimeOwnershipVerified
					if item.desiredFromOperation {
						item.DesiredState = contracts.ResourceObservedRunning
						power, found, readErr := s.resourceOperations.LatestResourceOperation(readCtx, "workspace_runtime_power", item.WorkspaceID)
						if readErr != nil {
							return contracts.RuntimeObservations{}, readErr
						}
						if found {
							var input WorkspaceRuntimePowerInput
							if power.Action != workspaceRuntimePowerAction || !decodeWorkspaceLaunchCloseoutPayload(power.RedactedProviderPayload["power"], &input) ||
								input.AccountID != item.AccountID || input.WorkspaceID != item.WorkspaceID || input.RuntimeID != item.RuntimeID || input.RuntimeOperationID != item.operationID ||
								(input.DesiredState != "running" && input.DesiredState != "suspended") || power.RequestHash != hashInput(input) {
								item.DesiredState = contracts.ResourceObservedUnknown
								item.ReasonCode = "runtime_power_binding_conflict"
							} else {
								item.DesiredState = contracts.ResourceObservedState(input.DesiredState)
							}
						}
					}

				} else {
					item.ReasonCode = "runtime_owner_conflict"
				}
			default:
				item.ReasonCode = "runtime_owner_conflict"
			}
		}
		result.Items = append(result.Items, item.RuntimeObservation)
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].ObjectRef < result.Items[j].ObjectRef })
	result.ObservedAt = s.now().Format(time.RFC3339Nano)
	return result, nil
}

// This retained API is physical running/not-running telemetry, not Workspace
// entitlement health. In particular, a normally suspended runtime is unready.
func runtimeInventoryLegacySummary(items []runtimeProviderObservation) (RuntimeHealthSummary, error) {
	result := RuntimeHealthSummary{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.WorkspaceID == "" {
			continue
		}
		if seen[item.WorkspaceID] {
			return RuntimeHealthSummary{}, fmt.Errorf("workspace_runtime_summary_duplicate_deployment")
		}
		seen[item.WorkspaceID] = true
		result.Total++
		if item.legacyReady {
			result.Ready++
		} else {
			result.Unready++
		}
	}
	return result, nil
}

// verifyApplicationRuntimeOwner resolves one discovered application component
// against the durable workspace_application_runtime records of its workspace.
// Ownership is verified only when one succeeded creation record matches the
// physical identity exactly (account, workspace, runtime, operation), carries
// the request hash that the claim computed from that input, and declares the
// observed component in its revision. Every other shape - missing record,
// mismatched identity, corrupted record, undeclared component - fails closed:
// the platform does not guess an owner for a workload it did not record.
func verifyApplicationRuntimeOwner(owners []FabricOperation, item runtimeProviderObservation) (contracts.RuntimeOwnership, string) {
	if len(owners) == 0 {
		return contracts.RuntimeOwnershipUnregistered, ""
	}
	matches := 0
	verified := false
	for _, owner := range owners {
		var record workspaceApplicationRuntimeRecord
		if !decodeOperationResource(owner, &record) {
			continue
		}
		if record.RuntimeID != item.RuntimeID || record.WorkspaceID != item.WorkspaceID ||
			record.Input.AccountID != item.AccountID || owner.AccountID != item.AccountID ||
			record.Input.RuntimeOperationID != item.operationID {
			continue
		}
		matches++
		if owner.Status != "succeeded" || owner.RequestHash == "" || hashInput(record.Input) != owner.RequestHash {
			continue
		}
		if !applicationRevisionDeclaresComponent(record.Input, item.componentName, item.serviceName) {
			continue
		}
		verified = true
	}
	if matches == 0 {
		return contracts.RuntimeOwnershipUnregistered, ""
	}
	if matches > 1 || !verified {
		return contracts.RuntimeOwnershipConflict, "runtime_owner_conflict"
	}
	return contracts.RuntimeOwnershipVerified, ""
}

// applicationRevisionDeclaresComponent ties the observed physical deployment to
// one component the admitted revision actually declared. The deployment name is
// the revision-derived component resource name, so both identities must agree;
// several components of one revision are legitimate and each verifies on its
// own identity, never as an owner conflict.
func applicationRevisionDeclaresComponent(input WorkspaceApplicationRuntimeInput, componentName, serviceName string) bool {
	if componentName == "" {
		return false
	}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if component.Name == componentName && workspaceApplicationComponentResourceName(input, componentName) == serviceName {
			return true
		}
	}
	return false
}
