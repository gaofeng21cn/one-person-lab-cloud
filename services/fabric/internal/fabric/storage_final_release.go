package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const StorageFinalReleaseConfirmation = "RELEASE_ORIGINAL_RECYCLED_CBS"
const storageFinalReleaseAction = "release_recycled_storage_volume"

// StorageFinalReleaseInput binds explicit final-release authorization to the
// original accepted destroy. It does not authorize a new destroy or settlement.
type StorageFinalReleaseInput struct {
	AccountID          string `json:"accountId"`
	WorkspaceID        string `json:"workspaceId"`
	DestroyOperationID string `json:"destroyOperationId"`
	Confirmation       string `json:"confirmation"`
}

type StorageFinalReleaseStatus string

const (
	StorageFinalReleaseStatusPending   StorageFinalReleaseStatus = "pending"
	StorageFinalReleaseStatusStarted   StorageFinalReleaseStatus = "started"
	StorageFinalReleaseStatusFailed    StorageFinalReleaseStatus = "failed"
	StorageFinalReleaseStatusSucceeded StorageFinalReleaseStatus = "succeeded"
)

type StorageFinalReleaseResult struct {
	OperationID       string                    `json:"operationId"`
	Status            StorageFinalReleaseStatus `json:"status"`
	DispatchAttempted bool                      `json:"dispatchAttempted"`
	AbsenceConfirmed  bool                      `json:"absenceConfirmed"`
}

type storageFinalReleaseJournalPayload struct {
	OriginalDestroyOperationID string `json:"originalDestroyOperationId"`
	DispatchAttempted          *bool  `json:"dispatchAttempted"`
}

type recycledStorageReleaser interface {
	ReleaseRecycledStorageVolume(context.Context, StorageVolume) (string, error)
}

// StorageFinalReleaseEligible accepts only a fresh readback proving the exact
// owned CBS is recycled and no longer bound to a workload.
func StorageFinalReleaseEligible(volume StorageVolume) bool {
	_, observedAtErr := time.Parse(time.RFC3339Nano, volume.ObservedAt)
	return observedAtErr == nil && volume.ReadbackID != "" && volume.Provider == "tencent-tke" && strings.HasPrefix(volume.ProviderResourceID, "disk-") &&
		volume.BindingPresent != nil && !*volume.BindingPresent && volume.CBSStatus == "TORECYCLE" &&
		volume.ProviderData["storageDestroyPhase"] == storageDestroyPhaseTerminateAttempted && volume.ProviderData["storageDestroyMutationCount"] == "1" &&
		volume.ProviderData["cbsStatus"] == "TORECYCLE" && volume.ProviderData["storageVolumeId"] == volume.ProviderResourceID &&
		strings.TrimSpace(volume.ProviderData["describeCbsRequestId"]) != ""
}

// ReadStorageDestroyStatus observes the frozen original destroy, including its
// phase and ownership context. It never hydrates or updates the resource cache,
// appends a journal entry, or continues binding deletion.
func (s *Service) ReadStorageDestroyStatus(ctx context.Context, id string) (StorageVolume, error) {
	_, frozen, err := s.storageFinalReleaseOriginal(ctx, id)
	if err != nil {
		return StorageVolume{}, err
	}
	return s.readFrozenStorageDestroyStatus(ctx, frozen)
}

func (s *Service) storageFinalReleaseOriginal(ctx context.Context, id string) (FabricOperation, StorageVolume, error) {
	operation, found, err := s.resourceOperations.LatestResourceOperation(ctx, "storage_volume", id)
	if err != nil {
		return FabricOperation{}, StorageVolume{}, err
	}
	frozen, valid := validStorageDestroyOperation(operation, id)
	if !found || !valid || operation.OperationID == "" || !decodeStrictJSON(mustJSON(operation.RedactedProviderPayload["resource"]), &frozen) {
		return FabricOperation{}, StorageVolume{}, fmt.Errorf("storage_final_release_original_destroy_mismatch")
	}
	return operation, frozen, nil
}

func (s *Service) readFrozenStorageDestroyStatus(ctx context.Context, frozen StorageVolume) (StorageVolume, error) {
	reader := s.optionalProviders.storageVolumeStatus
	if reader == nil {
		return StorageVolume{}, fmt.Errorf("storage_destroy_status_readback_unavailable")
	}
	ctx = context.WithValue(ctx, workspaceStorageDeleteOwnerContextKey{}, cloneStorageVolume(frozen))
	// Retain the original phase for the provider's binding observation, but
	// never let a sparse response reuse the journal's old CBS or binding proof.
	readInput := cloneStorageVolume(frozen)
	for _, key := range []string{"cbsStatus", "storageVolumeId", "describeCbsRequestId", "status", "terminateCbsRequestId"} {
		if _, present := readInput.ProviderData[key]; present {
			readInput.ProviderData[key] = ""
		}
	}
	readInput.CBSStatus, readInput.Status, readInput.ProviderRequestID = "", "", ""
	readInput.BindingPresent, readInput.ObservedAt, readInput.ReadbackID = nil, "", ""
	observed, err := reader.ReadStorageVolumeStatus(ctx, readInput)
	if err != nil {
		return observed, err
	}
	if !sameStorageDestroyStableIdentity(frozen, observed) {
		return observed, fmt.Errorf("storage_final_release_identity_mismatch")
	}
	observed.ObservedAt = s.now().Format(time.RFC3339Nano)
	observed.ReadbackID = stableID("storage-volume-readback", frozen.ID, observed.ObservedAt)
	return observed, nil
}

func storageFinalReleaseDispatchAttempted(previous, expected FabricOperation, destroyOperationID string) (bool, error) {
	var payload storageFinalReleaseJournalPayload
	if previous.Action != expected.Action || previous.ResourceID != expected.ResourceID || previous.RequestHash != expected.RequestHash || previous.OperationID != expected.OperationID || previous.ResourceKind != expected.ResourceKind || previous.AccountID != expected.AccountID || previous.WorkspaceID != expected.WorkspaceID || previous.IdempotencyKey != expected.IdempotencyKey || previous.Provider != expected.Provider || (previous.CallerService != "operator" && previous.CallerService != "workspace") ||
		!decodeStrictJSON(mustJSON(previous.RedactedProviderPayload), &payload) || payload.OriginalDestroyOperationID != destroyOperationID || payload.DispatchAttempted == nil {
		return false, fmt.Errorf("storage_final_release_original_destroy_mismatch")
	}
	switch StorageFinalReleaseStatus(previous.Status) {
	case StorageFinalReleaseStatusStarted, StorageFinalReleaseStatusFailed, StorageFinalReleaseStatusSucceeded:
		return *payload.DispatchAttempted, nil
	default:
		return false, fmt.Errorf("storage_final_release_original_destroy_mismatch")
	}
}

// ReleaseRecycledStorageVolume atomically claims one explicit final-release
// send. The stable dispatch primary key remains exclusive even if the original
// destroy lock's connection is lost. Every losing claim is read-only; the
// original destroy still owns local finalization and its receipt.
func (s *Service) ReleaseRecycledStorageVolume(ctx context.Context, id string, input StorageFinalReleaseInput) (StorageFinalReleaseResult, error) {
	return s.releaseRecycledStorageVolume(ctx, id, input, "operator")
}

// ReleaseRecycledStorageVolumeForDeletion binds final release to the original
// Workspace destroy journal; callers cannot supply a replacement destroy ID.
func (s *Service) ReleaseRecycledStorageVolumeForDeletion(ctx context.Context, id, accountID, workspaceID string) (StorageFinalReleaseResult, error) {
	destroy, found, err := s.resourceOperations.LatestResourceOperation(ctx, "storage_volume", id)
	if err != nil {
		return StorageFinalReleaseResult{}, err
	}
	if !found || destroy.AccountID != accountID || destroy.WorkspaceID != workspaceID {
		return StorageFinalReleaseResult{}, fmt.Errorf("storage_final_release_original_destroy_mismatch")
	}
	return s.releaseRecycledStorageVolume(ctx, id, StorageFinalReleaseInput{
		AccountID: accountID, WorkspaceID: workspaceID,
		DestroyOperationID: destroy.OperationID,
		Confirmation:       StorageFinalReleaseConfirmation,
	}, "workspace")
}

func (s *Service) releaseRecycledStorageVolume(ctx context.Context, id string, input StorageFinalReleaseInput, callerService string) (StorageFinalReleaseResult, error) {
	var out StorageFinalReleaseResult
	if input.Confirmation != StorageFinalReleaseConfirmation || strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.DestroyOperationID) == "" {
		return out, fmt.Errorf("storage_final_release_authorization_required")
	}
	err := s.resourceLocks.WithPoolLock(ctx, "storage-destroy:"+id, func(ctx context.Context) error {
		destroy, frozen, err := s.storageFinalReleaseOriginal(ctx, id)
		if err != nil {
			return err
		}
		if destroy.OperationID != input.DestroyOperationID || destroy.AccountID != input.AccountID || destroy.WorkspaceID != input.WorkspaceID {
			return fmt.Errorf("storage_final_release_original_destroy_mismatch")
		}
		_, deadlineErr := time.Parse(time.RFC3339Nano, frozen.Deadline)
		if _, _, valid := tencentStorageDestroyBindingIdentity(frozen); !valid || deadlineErr != nil || frozen.ProviderData["storageDestroyPhase"] == "" {
			return fmt.Errorf("storage_final_release_identity_mismatch")
		}
		previous, fenced, err := s.resourceOperations.LatestResourceOperation(ctx, "storage_final_release", id)
		if err != nil {
			return err
		}
		operation := newOperation(storageFinalReleaseAction, "storage_final_release", id, input.AccountID, input.WorkspaceID, id, hashInput(input), s.now())
		operation.CallerService, operation.Provider = callerService, frozen.Provider
		attempted := false
		if fenced {
			attempted, err = storageFinalReleaseDispatchAttempted(previous, operation, destroy.OperationID)
			if err != nil {
				return err
			}
			operation = previous
		}
		out = StorageFinalReleaseResult{OperationID: operation.OperationID, Status: StorageFinalReleaseStatusPending, DispatchAttempted: attempted}
		observed, err := s.readFrozenStorageDestroyStatus(ctx, frozen)
		if err != nil {
			return err
		}
		if observed.BindingPresent == nil || *observed.BindingPresent {
			return fmt.Errorf("storage_final_release_binding_absence_required")
		}
		absent := storageDestroyReadbackConfirmsAbsence(observed)
		if !absent && !fenced {
			// Validate the complete fresh precondition BEFORE creating the fence.
			// A refused or incomplete read must not consume the one authorized send.
			if observed.ProviderData["storageDestroyPhase"] != storageDestroyPhaseTerminateAttempted || observed.ProviderData["storageDestroyMutationCount"] != "1" ||
				observed.CBSStatus != "TORECYCLE" || observed.ProviderData["cbsStatus"] != "TORECYCLE" || observed.ProviderData["storageVolumeId"] != frozen.ProviderResourceID || strings.TrimSpace(observed.ProviderData["describeCbsRequestId"]) == "" {
				return fmt.Errorf("storage_final_release_recycled_cbs_required")
			}
			provider, ok := s.storageProvider.(recycledStorageReleaser)
			if !ok {
				return fmt.Errorf("storage_final_release_unavailable")
			}
			operation.ID = stableID("fop-storage-final-release-dispatch", id)
			operation.Status, operation.CreatedAt = string(StorageFinalReleaseStatusStarted), s.now()
			operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": destroy.OperationID, "dispatchAttempted": true}
			persisted, claimed, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
			if err != nil {
				return err
			}
			attempted, err := storageFinalReleaseDispatchAttempted(persisted, operation, destroy.OperationID)
			if err != nil || claimed && !attempted {
				return errors.Join(fmt.Errorf("storage_final_release_dispatch_claim_invalid"), err)
			}
			operation = persisted
			out.OperationID, out.DispatchAttempted = operation.OperationID, attempted
			if !claimed {
				// Lock ownership may have changed during the fresh read. Only the
				// atomic winner may send; a loser must not append another outcome.
				observed, err := s.readFrozenStorageDestroyStatus(ctx, frozen)
				if err != nil {
					return err
				}
				if observed.BindingPresent == nil || *observed.BindingPresent {
					return fmt.Errorf("storage_final_release_binding_absence_required")
				}
				if storageDestroyReadbackConfirmsAbsence(observed) {
					out.Status, out.AbsenceConfirmed = StorageFinalReleaseStatusSucceeded, true
				}
				return nil
			}
			requestID, mutationErr := provider.ReleaseRecycledStorageVolume(context.WithValue(ctx, workspaceStorageDeleteOwnerContextKey{}, cloneStorageVolume(frozen)), cloneStorageVolume(frozen))
			operation.ProviderRequestID = requestID
			observed, readErr := s.readFrozenStorageDestroyStatus(ctx, frozen)
			absent = readErr == nil && observed.BindingPresent != nil && !*observed.BindingPresent && storageDestroyReadbackConfirmsAbsence(observed)
			// Cancellation or a lost response cannot prevent persisting the outcome
			// after the fence. A failed outcome still never authorizes another send.
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if !absent {
				unconfirmed := errors.Join(fmt.Errorf("storage_final_release_unconfirmed"), mutationErr, readErr)
				return errors.Join(unconfirmed, s.recordOperation(writeCtx, operation, string(StorageFinalReleaseStatusFailed), nil, unconfirmed))
			}
			out.Status, out.AbsenceConfirmed = StorageFinalReleaseStatusSucceeded, true
			operation.ErrorCode = ""
			return s.recordOperation(writeCtx, operation, string(StorageFinalReleaseStatusSucceeded), nil, nil)
		}
		if absent {
			out.Status, out.AbsenceConfirmed = StorageFinalReleaseStatusSucceeded, true
			operation.CallerService, operation.Provider, operation.ErrorCode = callerService, frozen.Provider, ""
			operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": destroy.OperationID, "dispatchAttempted": out.DispatchAttempted}
			return s.recordOperation(ctx, operation, string(StorageFinalReleaseStatusSucceeded), nil, nil)
		}
		return nil
	})
	return out, err
}
