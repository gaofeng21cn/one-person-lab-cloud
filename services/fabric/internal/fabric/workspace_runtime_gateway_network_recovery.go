package fabric

import (
	"context"
	"errors"
	"strings"
)

const workspaceRuntimeGatewayNetworkRecoveryAction = "recover_workspace_runtime_gateway_network"

var (
	ErrWorkspaceRuntimeGatewayNetworkRecoveryUnavailable  = errors.New("workspace_runtime_gateway_network_recovery_unavailable")
	ErrWorkspaceRuntimeGatewayNetworkRecoveryInputInvalid = errors.New("workspace_runtime_gateway_network_recovery_input_invalid")
	ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict     = errors.New("workspace_runtime_gateway_network_recovery_conflict")
)

func validWorkspaceRuntimeGatewayNetworkRecoveryInput(input WorkspaceRuntimeGatewayNetworkRecoveryInput) bool {
	for _, value := range []string{input.AccountID, input.WorkspaceID, input.ComputeID, input.RuntimeID, input.RuntimeOperationID, input.RuntimeServiceName, input.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func (s *Service) RecoverWorkspaceRuntimeGatewayNetwork(ctx context.Context, input WorkspaceRuntimeGatewayNetworkRecoveryInput) (WorkspaceRuntimeGatewayNetworkRecoveryResult, error) {
	if !validWorkspaceRuntimeGatewayNetworkRecoveryInput(input) {
		return WorkspaceRuntimeGatewayNetworkRecoveryResult{}, ErrWorkspaceRuntimeGatewayNetworkRecoveryInputInvalid
	}
	provider := s.optionalProviders.runtimeGatewayNetworkRecovery
	if provider == nil || s.providerDescriptor.Descriptor().Name != "local-docker" {
		return WorkspaceRuntimeGatewayNetworkRecoveryResult{}, ErrWorkspaceRuntimeGatewayNetworkRecoveryUnavailable
	}
	s.mu.Lock()
	compute, computeOK := s.computes[input.ComputeID]
	s.mu.Unlock()
	if !computeOK || compute.ID != input.ComputeID || compute.AccountID != input.AccountID || compute.WorkspaceID != input.WorkspaceID ||
		compute.Provider != "local-docker" || !isReadyResourceStatus(compute.Status) {
		return WorkspaceRuntimeGatewayNetworkRecoveryResult{}, ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict
	}
	predecessorOperation, found, err := s.previousRuntimeOperation(ctx, input.WorkspaceID, input.RuntimeOperationID, input.AccountID)
	if err != nil {
		return WorkspaceRuntimeGatewayNetworkRecoveryResult{}, err
	}
	var predecessor WorkspaceRuntime
	if !found || predecessorOperation.Status != "succeeded" || !decodeOperationResource(predecessorOperation, &predecessor) ||
		predecessor.ID != input.RuntimeID || predecessor.OperationID != input.RuntimeOperationID || predecessor.WorkspaceID != input.WorkspaceID ||
		predecessor.ServiceName != input.RuntimeServiceName {
		return WorkspaceRuntimeGatewayNetworkRecoveryResult{}, ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict
	}

	requestHash := hashInput(input)
	now := s.now()
	operation := newOperation(workspaceRuntimeGatewayNetworkRecoveryAction, "workspace_runtime_gateway_network", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, requestHash, now)
	operation.ID = "fop_runtime_gateway_network_recovery_" + stableSuffix(workspaceRuntimeGatewayNetworkRecoveryAction, input.IdempotencyKey)[:18]
	operation.OperationID = input.IdempotencyKey
	operation.Status, operation.CreatedAt = "started", now
	operation.RedactedProviderPayload = map[string]any{"recovery": input}
	var result WorkspaceRuntimeGatewayNetworkRecoveryResult
	err = s.resourceLocks.WithPoolLock(ctx, "workspace-runtime-gateway-network-recovery:"+input.WorkspaceID, func(lockCtx context.Context) error {
		stored, claimed, claimErr := s.claimRuntimeOperation(lockCtx, operation)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			if stored.RequestHash != requestHash {
				return ErrRuntimeIdempotencyConflict
			}
			if stored.Status == "succeeded" {
				return decodeWorkspaceRuntimeGatewayNetworkRecoveryResult(stored, &result)
			}
			if stored.Status == "failed" {
				return ErrRuntimeOperationFailed
			}
			if stored.Status != "started" {
				return ErrRuntimeOperationFailed
			}
			operation = stored
		}
		result, err = provider.RecoverWorkspaceRuntimeGatewayNetwork(s.providerMutationContextForRuntimeGatewayNetworkRecovery(lockCtx, operation, input), input, compute)
		if err != nil {
			_ = s.saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(lockCtx, operation, input, result, "failed", err)
			return err
		}
		if !workspaceRuntimeGatewayNetworkRecoveryResultMatches(result, input) {
			err = ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict
			_ = s.saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(lockCtx, operation, input, result, "failed", err)
			return err
		}
		return s.saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(lockCtx, operation, input, result, "succeeded", nil)
	})
	return result, err
}

func workspaceRuntimeGatewayNetworkRecoveryResultMatches(result WorkspaceRuntimeGatewayNetworkRecoveryResult, input WorkspaceRuntimeGatewayNetworkRecoveryInput) bool {
	return result.SchemaVersion == 1 && result.OperationID == input.IdempotencyKey && result.AccountID == input.AccountID &&
		result.WorkspaceID == input.WorkspaceID && result.ComputeID == input.ComputeID && result.RuntimeID == input.RuntimeID &&
		result.RuntimeServiceName == input.RuntimeServiceName && result.GatewayContainerID != "" && result.NetworkID != "" && result.NetworkName != "" &&
		result.Status == "succeeded" && result.Runtime.Ready && result.Runtime.ID == input.RuntimeID && result.Runtime.OperationID == input.RuntimeOperationID &&
		result.Runtime.WorkspaceID == input.WorkspaceID && result.Runtime.ServiceName == input.RuntimeServiceName
}

func (s *Service) providerMutationContextForRuntimeGatewayNetworkRecovery(ctx context.Context, operation FabricOperation, input WorkspaceRuntimeGatewayNetworkRecoveryInput) context.Context {
	binding := WorkspaceLaunchStageBinding{
		SchemaVersion: 1, LaunchOperationID: input.RuntimeOperationID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID,
		Stage: "runtime", Action: "ensure_runtime", FabricOperationID: operation.OperationID,
		IdempotencyKey: operation.IdempotencyKey, RequestHash: operation.RequestHash, ExpectedResourceBinding: input.RuntimeServiceName,
	}
	return context.WithValue(ctx, providerMutationJournalContextKey{}, &providerMutationJournal{
		operations: s.providerMutations, machineOwnership: s.machineOwnership, parent: binding,
		parentOperation: operation, provider: s.providerDescriptor.Descriptor().Name, now: s.now,
	})
}

func (s *Service) saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(ctx context.Context, operation FabricOperation, input WorkspaceRuntimeGatewayNetworkRecoveryInput, result WorkspaceRuntimeGatewayNetworkRecoveryResult, status string, operationErr error) error {
	operation.Status, operation.ErrorCode, operation.Retryable = status, errorCode(operationErr), false
	operation.FinishedAt = s.now()
	operation.RedactedProviderPayload = map[string]any{"recovery": input, "result": result}
	return s.runtimeOperations.SaveRuntime(ctx, operation)
}

func decodeWorkspaceRuntimeGatewayNetworkRecoveryResult(operation FabricOperation, result *WorkspaceRuntimeGatewayNetworkRecoveryResult) error {
	if result == nil || operation.Status != "succeeded" {
		return ErrRuntimeOperationFailed
	}
	raw, ok := operation.RedactedProviderPayload["result"]
	if !ok || !decodeStrictJSON(mustJSON(raw), result) || result.Status != "succeeded" {
		return ErrRuntimeOperationFailed
	}
	return nil
}
