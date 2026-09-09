package fabric

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"strings"

	"opl-cloud/services/fabric/internal/protectedresource"
)

func (p *LocalDockerProvider) ReadWorkspaceLaunchCloseoutResource(ctx context.Context, request WorkspaceLaunchProviderRequest) (workspaceLaunchCloseoutResourceReadback, error) {
	input, binding := request.Input, request.Input.Binding
	if _, err := decodeLocalDockerPlanEnvelope(request.ProviderPlan, input.PackageID, input.SizeGB); err != nil {
		return workspaceLaunchCloseoutResourceReadback{}, err
	}
	state, stateErr := decodeLocalDockerWorkspaceLaunchState(request.Current)
	if len(request.Current.ProviderState) > 0 && stateErr != nil {
		return workspaceLaunchCloseoutResourceReadback{}, stateErr
	}
	switch binding.Stage {
	case "secret":
		_, err := p.ReadGatewaySecretByDigest(ctx, workspaceLaunchCloseoutSecretInput(request))
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			return workspaceLaunchCloseoutResourceReadback{State: "absent"}, nil
		}
		return workspaceLaunchCloseoutResourceReadback{State: "present"}, err
	case "ensure_compute_allocation":
		compute := ComputeAllocation{ID: workspaceLaunchComputeID(binding), OperationID: binding.FabricOperationID, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID, PackageID: input.PackageID, Provider: "local-docker"}
		if state.Compute != nil {
			compute = *state.Compute
		}
		if compute.ID != workspaceLaunchComputeID(binding) || compute.AccountID != binding.AccountID || compute.WorkspaceID != binding.WorkspaceID {
			return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
		}
		readback, err := p.ReadComputeAllocation(ctx, compute)
		result := workspaceLaunchCloseoutResourceReadback{State: "present", Compute: &readback}
		if readback.Status == "external_deleted" && err != nil && err.Error() == "local_docker_compute_not_found" {
			result.State = "absent"
			return result, nil
		}
		return result, err
	case "storage":
		volume := StorageVolume{ID: workspaceLaunchStorageID(binding), OperationID: binding.IdempotencyKey, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID, SizeGB: input.SizeGB, Provider: "local-docker"}
		if state.Storage != nil {
			volume = *state.Storage
		}
		if volume.ID != workspaceLaunchStorageID(binding) || volume.AccountID != binding.AccountID || volume.WorkspaceID != binding.WorkspaceID || volume.SizeGB != input.SizeGB {
			return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
		}
		readback, err := p.ReadStorageVolume(ctx, volume)
		result := workspaceLaunchCloseoutResourceReadback{State: "present", Storage: &readback}
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			result.State = "absent"
			return result, nil
		}
		return result, err
	}
	return workspaceLaunchCloseoutResourceReadback{}, ErrWorkspaceLaunchInputInvalid
}

func (p *TencentProvider) ReadWorkspaceLaunchCloseoutResource(ctx context.Context, request WorkspaceLaunchProviderRequest) (workspaceLaunchCloseoutResourceReadback, error) {
	input, binding := request.Input, request.Input.Binding
	plan, err := decodeTencentWorkspacePlanEnvelope(request.ProviderPlan, input.PackageID, input.SizeGB)
	if err != nil {
		return workspaceLaunchCloseoutResourceReadback{}, err
	}
	ctx = withTencentWorkspacePlan(ctx, plan)
	state, stateErr := decodeTencentWorkspaceLaunchState(request.Current)
	if len(request.Current.ProviderState) > 0 && stateErr != nil {
		return workspaceLaunchCloseoutResourceReadback{}, stateErr
	}
	switch binding.Stage {
	case "secret":
		_, err := p.ReadGatewaySecretByDigest(ctx, workspaceLaunchCloseoutSecretInput(request))
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			return workspaceLaunchCloseoutResourceReadback{State: "absent"}, nil
		}
		return workspaceLaunchCloseoutResourceReadback{State: "present"}, err
	case "ensure_compute_allocation":
		if state.Compute == nil {
			state, err = p.tencentWorkspaceLaunchComputeStateFromMutation(ctx, binding, input.PackageID)
			// Create records its exact child before dispatch. No child under the
			// Launch fence proves this stage cannot have called the provider.
			if errors.Is(err, ErrOperationNotFound) {
				return workspaceLaunchCloseoutResourceReadback{State: "absent"}, nil
			}
			if err != nil {
				return workspaceLaunchCloseoutResourceReadback{}, err
			}
		}
		if state.Compute == nil || state.ComputePlan == nil {
			return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
		}
		compute := *state.Compute
		if compute.ID != workspaceLaunchComputeID(binding) || compute.AccountID != binding.AccountID || compute.WorkspaceID != binding.WorkspaceID || compute.NodePoolID != plan.NodePoolID {
			return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
		}

		if compute.MachineName == "" || compute.InstanceID == "" || compute.NodeName == "" || compute.PrivateIP == "" {
			compute, err = p.DiscoverComputeAllocation(ctx, compute, *state.ComputePlan)
			// A dispatched request without its exact Machine is still uncertain.
			if err != nil {
				return workspaceLaunchCloseoutResourceReadback{State: "unknown"}, err
			}
		}
		owner, ownerErr := workspaceLaunchComputeOwnership(compute)
		if ownerErr != nil {
			return workspaceLaunchCloseoutResourceReadback{}, ownerErr
		}
		// The ownership protocol derives these tags from this exact Machine binding;
		// cleanup claims/proves the same owner before admitting the existing destroy.
		compute.CostTags = oplCostTags(compute.AccountID, compute.WorkspaceID, compute.ID, owner.ID)
		if !validTencentComputeDestroyStableIdentity(compute) {
			return workspaceLaunchCloseoutResourceReadback{State: "unknown"}, ErrLaunchStageBindingConflict
		}

		readback, err := p.ReadComputeDestroyStatus(ctx, compute)
		result := workspaceLaunchCloseoutResourceReadback{State: "present", Compute: &readback}
		if err == nil && isExternallyDeletedComputeStatus(readback.Status) && validTencentComputeAbsenceEvidence(readback) {
			result.State = "absent"
		}
		return result, err
	case "storage":
		if state.Storage == nil {
			compute, computeErr := decodeTencentWorkspaceLaunchState(request.Prior["ensure_compute_allocation"])
			if computeErr != nil || compute.Compute == nil {
				return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
			}
			volume, recoverErr := p.tencentWorkspaceLaunchStorageFromMutation(ctx, binding, StorageVolumeInput{
				ID: workspaceLaunchStorageID(binding), AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID, ComputeID: compute.Compute.ID,
				SizeGB: input.SizeGB, Zone: plan.Zone, OperationID: binding.FabricOperationID, IdempotencyKey: binding.IdempotencyKey,
			})
			if errors.Is(recoverErr, ErrOperationNotFound) {
				return workspaceLaunchCloseoutResourceReadback{State: "absent"}, nil
			}
			if recoverErr != nil {
				return workspaceLaunchCloseoutResourceReadback{}, recoverErr
			}
			state.Storage = &volume
		}
		volume := *state.Storage
		if volume.ID != workspaceLaunchStorageID(binding) || volume.AccountID != binding.AccountID || volume.WorkspaceID != binding.WorkspaceID {
			return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
		}

		journal := providerMutationJournalFromContext(ctx)
		latest, found, latestErr := journal.operations.LatestResourceOperation(ctx, "storage_volume", volume.ID)
		if latestErr != nil {
			return workspaceLaunchCloseoutResourceReadback{}, latestErr
		}
		if found && latest.Action == "destroy_storage_volume" {
			destroyed, valid := validStorageDestroyOperation(latest, volume.ID)
			if !valid || destroyed.AccountID != binding.AccountID || destroyed.WorkspaceID != binding.WorkspaceID || destroyed.OperationID != binding.IdempotencyKey {
				return workspaceLaunchCloseoutResourceReadback{}, ErrLaunchStageBindingConflict
			}
			volume = destroyed
		}
		readback, err := p.ReadCBSVolume(ctx, StorageVolumeInput{
			ID: volume.ID, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID, SizeGB: input.SizeGB,
			Zone: plan.Zone, OperationID: binding.FabricOperationID, IdempotencyKey: binding.IdempotencyKey,
		}, volume)
		result := workspaceLaunchCloseoutResourceReadback{State: "present", Storage: &readback}
		if err != nil {
			return result, err
		}
		if readback.Status != "external_deleted" {
			return result, nil
		}
		pv, pvc := storageBindingNames(volume)
		raw, err := p.callKubectl(ctx, []string{"get", "pv/" + pv, "pvc/" + pvc, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
		if err != nil {
			return result, err
		}
		items, err := workspaceLaunchCloseoutKubectlItems(raw)
		if err == nil && len(items) == 0 {
			result.State = "absent"
		}
		return result, err
	}
	return workspaceLaunchCloseoutResourceReadback{}, ErrWorkspaceLaunchInputInvalid
}

type workspaceLaunchCloseoutComputeOwner interface {
	ClaimWorkspaceLaunchCloseoutCompute(context.Context, WorkspaceLaunchProviderRequest, ComputeAllocation) error
}

func (p *TencentProvider) ClaimWorkspaceLaunchCloseoutCompute(ctx context.Context, request WorkspaceLaunchProviderRequest, compute ComputeAllocation) error {
	plan, err := decodeTencentWorkspacePlanEnvelope(request.ProviderPlan, request.Input.PackageID, request.Input.SizeGB)
	if err != nil {
		return err
	}
	ctx = withTencentWorkspacePlan(ctx, plan)
	state, err := p.tencentWorkspaceLaunchComputeStateFromMutation(ctx, request.Input.Binding, request.Input.PackageID)
	if err != nil || state.ComputePlan == nil {
		return firstNonNil(err, ErrLaunchStageBindingConflict)
	}
	// Persist the authoritative allocation recovered from the original create
	// before claiming ownership. The parent Launch remains frozen and failed.
	journal := providerMutationJournalFromContext(ctx)
	childID := providerMutationOperationID(request.Input.Binding, "tencent_compute_allocation_create", "compute_allocation", compute.ID, plan.NodePoolID)
	child, err := journal.operations.Get(ctx, childID)
	if err != nil {
		return err
	}
	if child.Status != "succeeded" {
		next := child
		next.RedactedProviderPayload = maps.Clone(child.RedactedProviderPayload)
		next.Status, next.ErrorCode, next.Retryable, next.FinishedAt = "succeeded", "", false, journal.now()
		fillOperationResource(&next, compute)
		if err := journal.operations.ConvergeProviderMutationReadback(ctx, child, next); err != nil {
			return err
		}
	}
	_, err = p.ensureWorkspaceLaunchComputeOwnership(ctx, compute, *state.ComputePlan)
	return err
}

func workspaceLaunchCloseoutSecretInput(request WorkspaceLaunchProviderRequest) GatewaySecretReadbackInput {
	fingerprint := request.Current.Resources.GatewaySecretFingerprint
	return GatewaySecretReadbackInput{AccountID: request.Input.Binding.AccountID, WorkspaceID: request.Input.Binding.WorkspaceID, WorkspaceAPIKeyID: request.Current.GatewayKeyID,
		SecretRef: gatewaySecretName(request.Input.Binding.WorkspaceID), Fingerprint: fingerprint, KeyDigest: strings.TrimPrefix(fingerprint, "sha256:")}
}

func (p *TencentProvider) DestroyWorkspaceLaunchGatewaySecret(ctx context.Context, request WorkspaceLaunchProviderRequest) error {
	input := workspaceLaunchCloseoutSecretInput(request)
	return p.destroyGatewaySecret(ctx, input, request.Input.Binding.IdempotencyKey)
}

func (p *TencentProvider) destroyGatewaySecret(ctx context.Context, input GatewaySecretReadbackInput, idempotencyKey string) error {
	secret, err := p.ReadGatewaySecretByDigest(ctx, input)
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	attempt, err := beginProviderMutation(ctx, "tencent_gateway_secret_delete", "gateway_secret", secret.SecretRef, secret.Version)
	if err != nil {
		return err
	}
	_, deleteErr := p.callKubectl(ctx, []string{"delete", "secret/" + secret.SecretRef, "--ignore-not-found=true", "--wait=true"}, nil, protectedresource.Target{})
	_, readErr := p.ReadGatewaySecretByDigest(ctx, input)
	if !errors.Is(readErr, ErrWorkspaceLaunchResourceAbsent) {
		return firstNonNil(readErr, deleteErr, ErrWorkspaceLaunchPending)
	}
	return attempt.complete(ctx, providerRequestID("gateway-secret-delete", idempotencyKey), secret, nil)
}

func (p *LocalDockerProvider) DestroyWorkspaceLaunchGatewaySecret(ctx context.Context, request WorkspaceLaunchProviderRequest) error {
	_, err := p.DestroyWorkspaceRuntime(ctx, request.Input.Binding.WorkspaceID)
	return err
}

type workspaceLaunchStorageCloseoutContextKey struct{}

func workspaceLaunchCloseoutKubectlItems(raw []byte) ([]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return strictKubectlItems(raw)
}

// Failure closeout additionally binds deletion to the frozen original plan.
func (p *TencentProvider) verifyWorkspaceLaunchPartialStorage(ctx context.Context, request WorkspaceLaunchProviderRequest, volume StorageVolume) error {
	binding := request.Input.Binding
	if binding.Stage != "storage" || volume.ID != workspaceLaunchStorageID(binding) || volume.AccountID != binding.AccountID || volume.WorkspaceID != binding.WorkspaceID || volume.OperationID != binding.IdempotencyKey {
		return ErrLaunchStageBindingConflict
	}
	plan, err := decodeTencentWorkspacePlanEnvelope(request.ProviderPlan, request.Input.PackageID, request.Input.SizeGB)
	if err != nil || plan.Region != volume.ProviderData["region"] || plan.Zone != volume.Zone || plan.Storage.SizeGB != volume.SizeGB || plan.Storage.DiskType != volume.DiskType {
		return ErrLaunchStageBindingConflict
	}
	_, err = p.readStorageDeleteBindings(ctx, volume)
	return err
}
