package fabric

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// An operator explicitly accepts the unresolved original CBS terminate dispatch
// and authorizes exactly one completion of the already-recycled original
// prepaid disk. This is never an automatic retry: the original destroy fence
// and its outcome are never rewritten, the provider keeps its own
// recycled-disk precondition, and the platform's own absence readback still
// owns the deletion's outcome.
const StoragePrepaidTerminationReconciliationConfirmation = "RECONCILE_ORIGINAL_PREPAID_TERMINATION"
const storagePrepaidTerminationReconciliationAction = "reconcile_prepaid_storage_termination"
const storagePrepaidTerminationReconciliationResourceKind = "storage_termination_reconciliation"

type StoragePrepaidTerminationReconciliationInput struct {
	AccountID              string `json:"accountId"`
	WorkspaceID            string `json:"workspaceId"`
	DestroyOperationID     string `json:"destroyOperationId"`
	AuthorizationReference string `json:"authorizationReference"`
	Confirmation           string `json:"confirmation"`
}

type StoragePrepaidTerminationReconciliationResult struct {
	OperationID       string `json:"operationId"`
	DispatchAttempted bool   `json:"dispatchAttempted"`
	AbsenceConfirmed  bool   `json:"absenceConfirmed"`
}

var storagePrepaidTerminationToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// storagePrepaidTerminationReconciliationOperation binds one reconciliation to
// the original destroy it reconciles. The slot identity is resource-wide and
// independent of the operator's reference, so a second reference can never
// claim the same disk.
func storagePrepaidTerminationReconciliationOperation(id string, input StoragePrepaidTerminationReconciliationInput, provider string, now time.Time) FabricOperation {
	key := input.DestroyOperationID + ":" + storagePrepaidTerminationReconciliationAction
	operation := newOperation(storagePrepaidTerminationReconciliationAction, storagePrepaidTerminationReconciliationResourceKind, id, input.AccountID, input.WorkspaceID, key, hashInput(input), now)
	operation.CallerService, operation.Provider = "operator", provider
	operation.RedactedProviderPayload = map[string]any{
		"originalDestroyOperationId": input.DestroyOperationID,
		"authorizationReference":      input.AuthorizationReference,
		"dispatchAttempted":           false,
	}
	return operation
}

// storagePrepaidTerminationReconciliationReplay reads the one claim this disk
// already owns. A different operator reference conflicts instead of opening a
// second irreversible completion for the same original prepaid disk.
func storagePrepaidTerminationReconciliationReplay(history []FabricOperation, id string, input StoragePrepaidTerminationReconciliationInput, provider string) (FabricOperation, bool, error) {
	expected := storagePrepaidTerminationReconciliationOperation(id, input, provider, time.Time{})
	var claimed FabricOperation
	found := false
	for _, row := range history {
		if row.ResourceID != id || row.Action != storagePrepaidTerminationReconciliationAction {
			continue
		}
		reference, ok := row.RedactedProviderPayload["authorizationReference"].(string)
		if !ok || !storagePrepaidTerminationToken.MatchString(reference) {
			return FabricOperation{}, false, fmt.Errorf("storage_prepaid_termination_reconciliation_conflict")
		}
		if row.OperationID != expected.OperationID || row.ResourceKind != expected.ResourceKind ||
			row.AccountID != input.AccountID || row.WorkspaceID != input.WorkspaceID || row.CallerService != "operator" {
			return FabricOperation{}, false, fmt.Errorf("storage_prepaid_termination_reconciliation_conflict")
		}
		if reference != input.AuthorizationReference {
			return FabricOperation{}, false, fmt.Errorf("storage_prepaid_termination_reconciliation_conflict")
		}
		claimed, found = row, true
	}
	return claimed, found, nil
}

// ReconcilePrepaidStorageTermination completes one original prepaid CBS whose
// terminate dispatch was never confirmed. Only an unresolved original dispatch
// is eligible: a recorded terminate or a decided outcome is never rewritten.
// The provider still proves the disk is recycled and unbound before it
// terminates it, and the platform's own absence readback still decides when
// the deletion is complete.
func (s *Service) ReconcilePrepaidStorageTermination(ctx context.Context, id string, input StoragePrepaidTerminationReconciliationInput) (StoragePrepaidTerminationReconciliationResult, error) {
	var out StoragePrepaidTerminationReconciliationResult
	if input.Confirmation != StoragePrepaidTerminationReconciliationConfirmation {
		return out, fmt.Errorf("storage_prepaid_termination_reconciliation_authorization_required")
	}
	for _, value := range []string{id, input.AccountID, input.WorkspaceID, input.DestroyOperationID, input.AuthorizationReference} {
		if !storagePrepaidTerminationToken.MatchString(value) {
			return out, fmt.Errorf("storage_prepaid_termination_reconciliation_authorization_required")
		}
	}
	if err := s.hydrateMissingResourceState(ctx); err != nil {
		return out, err
	}
	err := s.resourceLocks.WithPoolLock(ctx, "storage-destroy:"+id, func(lockCtx context.Context) error {
		destroy, found, err := s.resourceOperations.LatestResourceOperation(lockCtx, "storage_volume", id)
		if err != nil {
			return err
		}
		frozen, valid := validStorageDestroyOperation(destroy, id)
		if !found || !valid || destroy.OperationID != input.DestroyOperationID ||
			frozen.AccountID != input.AccountID || frozen.WorkspaceID != input.WorkspaceID {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_original_destroy_mismatch")
		}
		if frozen.Provider != "tencent-tke" || !strings.HasPrefix(frozen.ProviderResourceID, "disk-") {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_identity_mismatch")
		}
		// The same prepaid binding identity the provider's own release requires:
		// one original disk, its owner tags, its PV/PVC names and its region.
		if _, _, valid := tencentStorageDestroyBindingIdentity(frozen); !valid {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_identity_mismatch")
		}
		if _, err := time.Parse(time.RFC3339Nano, frozen.Deadline); err != nil {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_identity_mismatch")
		}
		// Only an unresolved original dispatch may be reconciled. A recorded
		// terminate or a decided outcome is never rewritten.
		if frozen.ProviderData["storageDestroyPhase"] != storageDestroyPhaseDispatchAuthorized ||
			frozen.ProviderData["storageDestroyMutationCount"] != "0" {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_unresolved_dispatch_required")
		}
		// One irreversible completion belongs to one owner. A workspace final
		// release that already owns this disk keeps it.
		if previous, found, err := s.resourceOperations.LatestResourceOperation(lockCtx, "storage_final_release", id); err != nil {
			return err
		} else if found && previous.OperationID != "" {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_release_already_claimed")
		}
		provider, ok := s.storageProvider.(recycledStorageReleaser)
		if !ok {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_unavailable")
		}
		history, err := s.operationHistory.List(lockCtx)
		if err != nil {
			return err
		}
		claimed, found, err := storagePrepaidTerminationReconciliationReplay(history, id, input, frozen.Provider)
		if err != nil {
			return err
		}
		if found {
			attempted, _ := claimed.RedactedProviderPayload["dispatchAttempted"].(bool)
			out.OperationID, out.DispatchAttempted = claimed.OperationID, attempted
			observed, readErr := s.readFrozenStorageDestroyStatus(lockCtx, frozen)
			out.AbsenceConfirmed = readErr == nil && observed.BindingPresent != nil && !*observed.BindingPresent && storageDestroyReadbackConfirmsAbsence(observed)
			return nil
		}
		operation := storagePrepaidTerminationReconciliationOperation(id, input, frozen.Provider, s.now())
		operation.Status, operation.CreatedAt = "started", s.now()
		operation.RedactedProviderPayload["dispatchAttempted"] = true
		persisted, isClaim, err := s.runtimeOperations.ClaimRuntime(lockCtx, operation)
		if err != nil {
			return err
		}
		if !isClaim {
			return fmt.Errorf("storage_prepaid_termination_reconciliation_conflict")
		}
		operation = persisted
		out.OperationID, out.DispatchAttempted = operation.OperationID, true
		requestID, mutationErr := provider.ReleaseRecycledStorageVolume(
			context.WithValue(lockCtx, workspaceStorageDeleteOwnerContextKey{}, cloneStorageVolume(frozen)), cloneStorageVolume(frozen))
		operation.ProviderRequestID = requestID
		observed, readErr := s.readFrozenStorageDestroyStatus(lockCtx, frozen)
		absent := readErr == nil && observed.BindingPresent != nil && !*observed.BindingPresent && storageDestroyReadbackConfirmsAbsence(observed)
		// A lost response cannot prevent persisting the outcome after the
		// fence. A failed outcome still never authorizes another completion.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(lockCtx), 5*time.Second)
		defer cancel()
		if !absent {
			unconfirmed := errors.Join(fmt.Errorf("storage_prepaid_termination_reconciliation_unconfirmed"), mutationErr, readErr)
			return errors.Join(unconfirmed, s.recordOperation(writeCtx, operation, "failed", nil, unconfirmed))
		}
		out.AbsenceConfirmed = true
		operation.ErrorCode = ""
		return s.recordOperation(writeCtx, operation, "succeeded", nil, nil)
	})
	return out, err
}
