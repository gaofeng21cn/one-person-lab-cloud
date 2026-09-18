package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The platform refund for a deleted Workspace is a separate operation owned by
// Control Plane. It is dispatched only when Fabric's authoritative readback for
// the same original Delete operation proves that every Tencent resource is
// gone. Delete success and refund success are independent states: a refused or
// failed refund never rewrites the deletion result, and running reads never
// mutate provider resources.
const (
	workspaceDeleteRefundAction = "workspace.delete.refund.v1"

	// A refund readback must be produced by the current attempt, after the
	// platform-confirmed deletion, and never older than this window.
	workspaceDeleteRefundReadbackMaxAge = 15 * time.Minute

	workspaceDeleteRefundSystemActor = "system:workspace-delete-refund"
	workspaceDeleteRefundReason      = "OPL Workspace deletion platform refund"

	workspaceDeleteRefundStatusBlocked      = "blocked"
	workspaceDeleteRefundStatusPending      = "pending"
	workspaceDeleteRefundStatusManualReview = "manual_review"
	workspaceDeleteRefundStatusSucceeded    = "succeeded"
	workspaceDeleteRefundStatusNotDue       = "not_due"

	// Wallet-side reason codes. They never describe a deletion or provider fact.
	workspaceDeleteRefundReasonWalletUnavailable   = "wallet_refund_operation_unavailable"
	workspaceDeleteRefundReasonDispatchUnavailable = "wallet_refund_dispatch_unavailable"
)

// errWorkspaceDeleteRefundIdentity distinguishes a provider identity conflict
// from an unavailable readback, so the two are never collapsed into one state.
var errWorkspaceDeleteRefundIdentity = errors.New("workspace_delete_refund_provider_identity_mismatch")

type workspaceDeleteRefundOperation struct {
	SchemaVersion               int    `json:"schemaVersion"`
	AccountID                   string `json:"accountId"`
	WorkspaceID                 string `json:"workspaceId"`
	DeleteOperationID           string `json:"deleteOperationId"`
	LaunchOperationID           string `json:"launchOperationId"`
	PolicyVersion               string `json:"policyVersion"`
	Status                      string `json:"status"`
	ReasonCode                  string `json:"reasonCode,omitempty"`
	OriginalChargeCode          string `json:"originalChargeCode,omitempty"`
	OriginalChargeUSDMicros     int64  `json:"originalChargeUsdMicros,omitempty"`
	RefundUSDMicros             int64  `json:"refundUsdMicros,omitempty"`
	UsedHours                   int64  `json:"usedHours,omitempty"`
	RefundHours                 int64  `json:"refundHours,omitempty"`
	WalletAdjustmentOperationID string `json:"walletAdjustmentOperationId,omitempty"`
	// RefundOrderOperationID identifies the confirmed order this refund is reserved
	// against: the renewal when one paid for the period in use, otherwise the
	// original purchase.
	RefundOrderOperationID string `json:"refundOrderOperationId,omitempty"`
	RefundReceiptID        string `json:"refundReceiptId,omitempty"`
	DeletedAt              string `json:"deletedAt,omitempty"`
	ResourceFulfilledAt    string `json:"resourceFulfilledAt,omitempty"`
	ReadbackID             string `json:"readbackId,omitempty"`
	ReadbackProvider       string `json:"readbackProvider,omitempty"`
	ReadbackObservedAt     string `json:"readbackObservedAt,omitempty"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
}

func workspaceDeleteRefundOperationID(deleteOperationID string) string {
	return "workspace-delete-refund-" + stableID(deleteOperationID)[:24]
}

func workspaceDeleteRefundWalletOperationID(deleteOperationID string) string {
	return "wallet-adjustment-delete-" + stableID(deleteOperationID)[:24]
}

func workspaceDeleteRefundRow(operationID string, operation workspaceDeleteRefundOperation) map[string]any {
	return map[string]any{
		"id": operationID, "operationId": operationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceKind": "workspace", "resourceId": operation.WorkspaceID, "action": workspaceDeleteRefundAction,
		"status": operation.Status, "result": string(mustJSON(operation)), "createdAt": operation.CreatedAt,
	}
}

func decodeWorkspaceDeleteRefund(row map[string]any) (workspaceDeleteRefundOperation, error) {
	var operation workspaceDeleteRefundOperation
	if stringValue(row["action"]) != workspaceDeleteRefundAction || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil ||
		operation.AccountID == "" || operation.WorkspaceID == "" || stringValue(row["accountId"]) != operation.AccountID ||
		operation.DeleteOperationID == "" || operation.PolicyVersion != contracts.WorkspaceRefundPolicyVersion ||
		operation.RefundUSDMicros < 0 || operation.OriginalChargeUSDMicros < 0 ||
		(operation.Status != workspaceDeleteRefundStatusBlocked && operation.Status != workspaceDeleteRefundStatusPending &&
			operation.Status != workspaceDeleteRefundStatusManualReview && operation.Status != workspaceDeleteRefundStatusSucceeded &&
			operation.Status != workspaceDeleteRefundStatusNotDue) {
		return workspaceDeleteRefundOperation{}, errWorkspaceDeleteStateRead
	}
	return operation, nil
}

func (app *controlPlaneServer) workspaceDeleteRefundRecord(ctx context.Context, workspaceID string) (workspaceDeleteRefundOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteRefundOperationID(workspaceDeleteOperationID(workspaceID)))
	if err != nil || !found {
		return workspaceDeleteRefundOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteRefund(row)
	return operation, err == nil, err
}

func (app *controlPlaneServer) persistWorkspaceDeleteRefund(ctx context.Context, next workspaceDeleteRefundOperation) error {
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return app.tables.SaveRuntimeOperation(ctx, workspaceDeleteRefundRow(workspaceDeleteRefundOperationID(next.DeleteOperationID), next))
}

// workspaceDeleteRefundIdentity binds the refund to the exact resources the
// original Delete operation destroyed for this Workspace.
func workspaceDeleteRefundIdentity(operation workspaceDeleteOperation) contracts.WorkspaceDeleteIdentity {
	return contracts.WorkspaceDeleteIdentity{
		DeleteOperationID: operation.OperationID, LaunchOperationID: operation.LaunchOperationID, AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID,
		RuntimeID: operation.RuntimeID, ComputeID: operation.ComputeID, StorageID: operation.StorageID, AttachmentID: operation.AttachmentID,
		StorageProviderResourceID: operation.StorageProviderResourceID, ComputeMachineName: operation.ComputeMachineName, ComputeInstanceID: operation.ComputeCVMInstanceID,
		ResourceFulfilledAt: operation.LaunchFulfilledAt, WorkspaceDeletedAt: operation.DeletedAt,
	}
}

// workspaceDeleteRefundIdentityComplete reports whether the original Delete
// operation recorded the identities the refund must bind to.
//
// The set follows the resources the operation actually owns: a resource-only
// purchase has no Runtime controller, so requiring a Runtime identity would refuse
// a refund for a Workspace that correctly has none. The provider-scoped machine and
// CVM identities are not required here because the readback itself decides whether
// the provider owns one; a recorded identity that the readback omits or contradicts
// is still refused by the identity check.
func workspaceDeleteRefundIdentityComplete(operation workspaceDeleteOperation) bool {
	identity := workspaceDeleteRefundIdentity(operation)
	if identity.DeleteOperationID == "" || identity.LaunchOperationID == "" || identity.AccountID == "" || identity.WorkspaceID == "" ||
		identity.ComputeID == "" || identity.StorageID == "" || identity.AttachmentID == "" ||
		identity.StorageProviderResourceID == "" || identity.ResourceFulfilledAt == "" || identity.WorkspaceDeletedAt == "" ||
		operation.DeletionReceiptID == "" {
		return false
	}
	// Only a purchase that created a Runtime controller must bind its identity.
	return operation.ProvisioningMode == string(contracts.WorkspaceProvisioningResourceOnly) || identity.RuntimeID != ""
}

// observeWorkspaceDeleteRefundReadback performs one fresh, read-only,
// identity-bound readback through Fabric's owning read surfaces. It never
// mutates provider resources and never reuses a cached destroy result.
func (app *controlPlaneServer) observeWorkspaceDeleteRefundReadback(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) (contracts.WorkspaceDeleteReadback, error) {
	identity := workspaceDeleteRefundIdentity(operation)
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	readbackID := stableID("workspace-delete-readback", operation.OperationID, observedAt)
	runtime, err := service.ObserveWorkspaceDeleteRuntime(ctx, operation.WorkspaceID)
	if err != nil {
		return contracts.WorkspaceDeleteReadback{}, err
	}
	secret, err := service.ObserveWorkspaceDeleteRuntimeGatewaySecret(ctx, operation.WorkspaceID)
	if err != nil {
		return contracts.WorkspaceDeleteReadback{}, err
	}
	residuals, err := service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
	if err != nil {
		return contracts.WorkspaceDeleteReadback{}, err
	}
	storage, err := service.ReadWorkspaceDeleteStorage(ctx, operation.StorageID)
	if err != nil {
		return contracts.WorkspaceDeleteReadback{}, err
	}
	compute, err := service.WorkspaceDeleteComputeStatus(ctx, operation.ComputeID)
	if err != nil {
		return contracts.WorkspaceDeleteReadback{}, err
	}
	provider := firstNonEmpty(storage.Provider, compute.Provider)
	if workspaceDeleteReadbackIdentityMismatch(identity, runtime, secret, residuals, storage, compute) {
		return contracts.WorkspaceDeleteReadback{}, errWorkspaceDeleteRefundIdentity
	}
	if provider == "" {
		return contracts.WorkspaceDeleteReadback{}, errWorkspaceDeleteUnconfirmed
	}

	residualKinds := map[string]string{}
	for _, residual := range residuals.Residuals {
		if _, exists := residualKinds[residual.Kind]; exists {
			return contracts.WorkspaceDeleteReadback{}, errWorkspaceDeleteUnconfirmed
		}
		residualKinds[residual.Kind] = residual.Name
	}
	gatewaySecretRef := firstNonEmpty(operation.GatewaySecretRef, stringValue(runtimeSecretRef(secret)))
	environmentSecretPresent := false
	gatewaySecretResidual := false
	if name, present := residualKinds["Secret"]; present {
		if gatewaySecretRef != "" && name == gatewaySecretRef {
			gatewaySecretResidual = true
		} else {
			environmentSecretPresent = true
		}
	}
	runtimeAbsent := runtime.State == clients.WorkspaceOwnerObservationAbsent && residuals.State == clients.WorkspaceOwnerObservationAbsent
	gatewaySecretAbsent := secret.State == clients.WorkspaceOwnerObservationAbsent && !gatewaySecretResidual
	bindingPresent := storage.BindingPresent != nil && *storage.BindingPresent
	bindingObserved := storage.BindingPresent != nil
	machinePresent := compute.MachinePresent != nil && *compute.MachinePresent
	machineObserved := compute.MachinePresent != nil

	readback := contracts.WorkspaceDeleteReadback{
		SchemaVersion: contracts.WorkspaceDeleteReadbackSchemaVersion, Provider: provider,
		DeleteOperationID: operation.OperationID, LaunchOperationID: operation.LaunchOperationID,
		AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID, ObservedAt: observedAt, ReadbackID: readbackID,
		Result: contracts.WorkspaceDeleteResultWaiting, MutationCount: 0,
	}
	addFact := func(kind, resourceID string, present, observed bool, status string) {
		readback.Facts = append(readback.Facts, contracts.WorkspaceDeleteResourceFact{
			Kind: kind, ResourceID: resourceID, Present: present, Observed: observed, ProviderStatus: status,
			ObservedAt: observedAt, ReadbackID: readbackID, MutationCount: 0,
		})
	}
	absent := contracts.WorkspaceDeleteProviderStatusNotFound
	// Every fact carries the identity the readback reported. A fact never restates
	// the recorded expectation, so the gate compares observation against record
	// instead of comparing a record with itself.
	runtimeResourceID, _ := identity.ExpectedResourceID(contracts.WorkspaceDeleteResourceRuntimeController)
	storageResourceID := storage.ProviderResourceID
	attachmentResourceID, _ := identity.ExpectedResourceID(contracts.WorkspaceDeleteResourceAttachment)
	machineResourceID := observedMachine(compute)
	cvmResourceID := observedInstance(compute)
	addFact(contracts.WorkspaceDeleteResourceRuntimeController, firstNonEmpty(observedRuntimeID(runtime), runtimeResourceID), !runtimeAbsent, true, providerAbsenceStatus(runtimeAbsent, absent))
	addFact(contracts.WorkspaceDeleteResourceDeployment, firstNonEmpty(observedRuntimeID(runtime), runtimeResourceID), !runtimeAbsent, true, providerAbsenceStatus(runtimeAbsent, absent))
	for _, workload := range []struct{ kind, residualKind string }{
		{contracts.WorkspaceDeleteResourceReplicaSet, "ReplicaSet"},
		{contracts.WorkspaceDeleteResourcePod, "Pod"},
		{contracts.WorkspaceDeleteResourceService, "Service"},
		{contracts.WorkspaceDeleteResourceNetworkPolicy, "NetworkPolicy"},
	} {
		_, present := residualKinds[workload.residualKind]
		addFact(workload.kind, firstNonEmpty(observedRuntimeID(runtime), runtimeResourceID), present, true, providerAbsenceStatus(!present, absent))
	}
	addFact(contracts.WorkspaceDeleteResourceEnvironmentSecret, firstNonEmpty(observedRuntimeID(runtime), runtimeResourceID), environmentSecretPresent, true, providerAbsenceStatus(!environmentSecretPresent, absent))
	addFact(contracts.WorkspaceDeleteResourceGatewaySecret, firstNonEmpty(observedRuntimeID(runtime), runtimeResourceID), !gatewaySecretAbsent, true, providerAbsenceStatus(gatewaySecretAbsent, absent))
	for _, kind := range []string{contracts.WorkspaceDeleteResourceAttachment, contracts.WorkspaceDeleteResourcePersistentVolumeClaim, contracts.WorkspaceDeleteResourcePersistentVolume} {
		resourceID := attachmentResourceID
		if kind != contracts.WorkspaceDeleteResourceAttachment {
			resourceID = storageResourceID
		}
		addFact(kind, resourceID, bindingPresent, bindingObserved, providerAbsenceStatus(!bindingPresent, absent))
	}
	cbsAbsent := storage.Status == "external_deleted" && strings.EqualFold(strings.TrimSpace(storage.CBSStatus), contracts.WorkspaceDeleteProviderStatusNotFound)
	cbsObserved := strings.TrimSpace(storage.CBSStatus) != ""
	addFact(contracts.WorkspaceDeleteResourceCBS, storageResourceID, !cbsAbsent, cbsObserved, cbsReadbackStatus(storage.CBSStatus, cbsAbsent))
	addFact(contracts.WorkspaceDeleteResourceMachine, machineResourceID, machinePresent, machineObserved, computeReadbackStatus(compute.TKEStatus, machineObserved && !machinePresent))
	cvmAbsent := strings.EqualFold(strings.TrimSpace(compute.CVMStatus), contracts.WorkspaceDeleteProviderStatusNotFound)
	addFact(contracts.WorkspaceDeleteResourceCVM, cvmResourceID, !cvmAbsent, strings.TrimSpace(compute.CVMStatus) != "", computeReadbackStatus(compute.CVMStatus, cvmAbsent))

	for _, fact := range readback.Facts {
		if !contracts.WorkspaceDeleteFactProvesAbsence(fact) {
			readback.Result = contracts.WorkspaceDeleteResultWaiting
			readback.ReasonCode = contracts.WorkspaceDeleteGateResourcePresent
			return readback, nil
		}
	}
	readback.Result = contracts.WorkspaceDeleteResultCompleted
	return readback, nil
}

func workspaceDeleteReadbackIdentityMismatch(identity contracts.WorkspaceDeleteIdentity, runtime clients.WorkspaceRuntimeObservation, secret clients.WorkspaceRuntimeGatewaySecretObservation,
	residuals clients.WorkspaceRuntimeDeleteObservation, storage clients.StorageVolume, compute clients.ComputeAllocation) bool {
	if runtime.WorkspaceID != identity.WorkspaceID || secret.WorkspaceID != identity.WorkspaceID ||
		residuals.WorkspaceID != identity.WorkspaceID || storage.ID != identity.StorageID || storage.WorkspaceID != identity.WorkspaceID ||
		compute.ID != identity.ComputeID || compute.WorkspaceID != identity.WorkspaceID {
		return true
	}
	if runtime.Runtime != nil && runtime.Runtime.ID != "" && runtime.Runtime.ID != identity.RuntimeID {
		return true
	}
	if secret.Binding != nil && secret.Binding.WorkspaceID != identity.WorkspaceID {
		return true
	}
	// The identity the readback reports must be the identity this Delete operation
	// destroyed. A readback that omits the identity is not an identity-bound
	// readback: refusing it is the only safe answer, because accepting it would let
	// the platform refund a Workspace without proving which resource it read, and
	// filling the gap from the expected identity would present an expectation as an
	// observation.
	if storage.ProviderResourceID != identity.StorageProviderResourceID {
		return true
	}
	if observedMachine(compute) != expectedMachine(identity) {
		return true
	}
	if observedInstance(compute) != expectedInstance(identity) {
		return true
	}
	return false
}

// observedMachine reports the machine identity the provider readback actually
// reported. A provider that owns no TKE machine reports the compute allocation it
// scopes that absence to, which is an observed value, not the recorded one.
func observedMachine(compute clients.ComputeAllocation) string {
	return firstNonEmpty(compute.MachineName, compute.ID)
}

// observedInstance reports the CVM instance identity the provider readback
// actually reported.
func observedInstance(compute clients.ComputeAllocation) string {
	return firstNonEmpty(compute.CVMInstanceID, compute.InstanceID, compute.ID)
}

func expectedMachine(identity contracts.WorkspaceDeleteIdentity) string {
	return firstNonEmpty(identity.ComputeMachineName, identity.ComputeID)
}

func expectedInstance(identity contracts.WorkspaceDeleteIdentity) string {
	return firstNonEmpty(identity.ComputeInstanceID, identity.ComputeID)
}

// observedRuntimeID reports the runtime identity the readback actually reported.
// An absent runtime reports nothing, so the Workspace-scoped readback that proved
// the absence is the binding, and the recorded runtime identity remains the
// expected value the gate compares against.
func observedRuntimeID(observation clients.WorkspaceRuntimeObservation) string {
	if observation.Runtime == nil {
		return ""
	}
	return observation.Runtime.ID
}

func runtimeSecretRef(observation clients.WorkspaceRuntimeGatewaySecretObservation) string {
	if observation.Binding == nil {
		return ""
	}
	return observation.Binding.SecretRef
}

func providerAbsenceStatus(absent bool, absentStatus string) string {
	if absent {
		return absentStatus
	}
	return "PRESENT"
}

func cbsReadbackStatus(status string, absent bool) string {
	if absent {
		return contracts.WorkspaceDeleteProviderStatusNotFound
	}
	if strings.TrimSpace(status) == "" {
		return ""
	}
	return status
}

func computeReadbackStatus(status string, absent bool) string {
	if absent {
		return contracts.WorkspaceDeleteProviderStatusNotFound
	}
	return strings.TrimSpace(status)
}

// workspaceDeleteReceiptRecorded proves the deletion receipt this refund binds
// to is the one Ledger actually holds for this exact operation.
func (app *controlPlaneServer) workspaceDeleteReceiptRecorded(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) (bool, error) {
	if operation.DeletionReceiptID == "" {
		return false, nil
	}
	// Rebuild the payload exactly as it was written: the receipt attests the stages
	// that preceded it, so the receipt's own stage is excluded here.
	recorded := operation
	recorded.StageEvidence = workspaceDeleteRecordedReceiptEvidence(operation)
	expected := workspaceDeletionReceiptInput(recorded)
	receipt, err := service.BillingReceiptForAccount(ctx, operation.AccountID, operation.WorkspaceID, operation.DeletionReceiptID)
	if err != nil {
		return false, err
	}
	if receipt.ReceiptID != operation.DeletionReceiptID || !workspaceLaunchReceiptInputMatches(receipt.ReceiptInput, expected) {
		return false, nil
	}
	return true, nil
}

func (app *controlPlaneServer) recordWorkspaceDeleteRefundOutcome(ctx context.Context, operation workspaceDeleteOperation, previous *workspaceDeleteRefundOperation, status, reasonCode string) error {
	// A refund that is still refused writes nothing new: the worker re-evaluates
	// it every cycle and must not append an identical row each time.
	if previous != nil && previous.Status == status && previous.ReasonCode == reasonCode {
		return nil
	}
	next := workspaceDeleteRefundOperation{
		SchemaVersion: 1, AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID, DeleteOperationID: operation.OperationID,
		LaunchOperationID: operation.LaunchOperationID, PolicyVersion: contracts.WorkspaceRefundPolicyVersion, Status: status, ReasonCode: reasonCode,
		DeletedAt: operation.DeletedAt, ResourceFulfilledAt: operation.LaunchFulfilledAt, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if previous != nil {
		next = *previous
		next.Status, next.ReasonCode = status, reasonCode
	}
	return app.persistWorkspaceDeleteRefund(ctx, next)
}

// runWorkspaceDeleteRefund evaluates the platform refund precondition for one
// completed Delete operation and dispatches the platform wallet refund only
// when the complete authoritative readback allows it.
func (app *controlPlaneServer) runWorkspaceDeleteRefund(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) error {
	unlock := app.lockResource("workspace-refund", operation.WorkspaceID)
	defer unlock()
	existing, found, err := app.workspaceDeleteRefundRecord(ctx, operation.WorkspaceID)
	if err != nil {
		return err
	}
	if found && (existing.Status == workspaceDeleteRefundStatusSucceeded || existing.Status == workspaceDeleteRefundStatusNotDue) {
		return nil
	}
	var previous *workspaceDeleteRefundOperation
	if found {
		previous = &existing
	}
	if operation.Phase != "complete" || operation.Status != "succeeded" {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateOperationIncomplete)
	}
	if !workspaceDeleteRefundIdentityComplete(operation) {
		// Without the provider identity this Delete operation actually destroyed,
		// no readback can be bound to it. Refuse instead of reading or guessing.
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateOperationIncomplete)
	}
	readback, readbackErr := app.observeWorkspaceDeleteRefundReadback(ctx, service, operation)
	if readbackErr != nil {
		reason := contracts.WorkspaceDeleteGateReadbackUnavailable
		if errors.Is(readbackErr, errWorkspaceDeleteRefundIdentity) {
			reason = contracts.WorkspaceDeleteGateReadbackIdentity
		}
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, reason)
	}
	recorded, err := app.workspaceDeleteReceiptRecorded(ctx, service, operation)
	if err != nil {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateReceiptMissing)
	}
	allowed, reason := contracts.PlatformRefundDispatchAllowed(contracts.WorkspaceDeleteRefundGateInput{
		Identity: workspaceDeleteRefundIdentity(operation), Readback: readback, DeleteReceiptRecorded: recorded,
		Now: time.Now().UTC(), MaxReadbackAge: workspaceDeleteRefundReadbackMaxAge,
	})
	if !allowed {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, reason)
	}
	return app.dispatchWorkspaceDeleteRefund(ctx, service, operation, readback, previous)
}

// workspaceDeleteRefundBase is the confirmed charge that paid for the period the
// Workspace was using when it was deleted, together with the start of that period.
type workspaceDeleteRefundBase struct {
	// OriginalOperationID identifies the order the refund is reserved against. It is
	// the renewal when a renewal paid for the period in use, and the original
	// purchase otherwise.
	OriginalOperationID string
	Charge              walletRefundCharge
	// PeriodStart is the start of the paid period the charge covers.
	PeriodStart time.Time
}

// workspaceDeleteRefundOrderFacts are the confirmed order facts the refund owner
// needs from a renewal: which Workspace and account it paid for, and the period it
// covers. The charge carries the paying user, which the wallet owner validates, so
// the order facts only bind the order to this Workspace and account.
type workspaceDeleteRefundOrderFacts struct {
	AccountID      string `json:"accountId"`
	WorkspaceID    string `json:"workspaceId"`
	PaidThrough    string `json:"paidThrough"`
	RenewedThrough string `json:"renewedThrough"`
}

// workspaceDeleteRefundBase resolves what the customer actually paid for the period
// in use when the Workspace was deleted.
//
// A renewal that is in force takes precedence over the original purchase: the
// customer paid for the current period with that renewal, so the refund is computed
// from it and reserved against that order. Otherwise the original purchase and the
// platform-confirmed fulfilment of the resources it paid for are used.
func (app *controlPlaneServer) workspaceDeleteRefundBase(ctx context.Context, operation workspaceDeleteOperation) (workspaceDeleteRefundBase, error) {
	deletedAt, err := time.Parse(time.RFC3339Nano, workspaceDeleteRefundIdentity(operation).WorkspaceDeletedAt)
	if err != nil {
		return workspaceDeleteRefundBase{}, errWorkspaceDeleteUnconfirmed
	}
	rows, err := app.tables.ListRuntimeOperations(ctx)
	if err != nil {
		return workspaceDeleteRefundBase{}, errWorkspaceDeleteUnconfirmed
	}
	latest := workspaceDeleteRefundBase{}
	for _, row := range rows {
		if stringValue(row["action"]) != "workspace.renewal" || stringValue(row["workspaceId"]) != operation.WorkspaceID ||
			stringValue(row["accountId"]) != operation.AccountID {
			continue
		}
		// The refund owner needs the order's charge facts and the period it paid for,
		// not the renewal state machine, so it reads exactly those and binds them to
		// this Workspace and account.
		var facts workspaceDeleteRefundOrderFacts
		if json.Unmarshal([]byte(stringValue(row["result"])), &facts) != nil ||
			facts.AccountID != operation.AccountID || facts.WorkspaceID != operation.WorkspaceID {
			continue
		}
		periodStart, startErr := time.Parse(time.RFC3339Nano, facts.PaidThrough)
		renewedThrough, endErr := time.Parse(time.RFC3339Nano, facts.RenewedThrough)
		if startErr != nil || endErr != nil || !renewedThrough.After(periodStart) || deletedAt.Before(periodStart) {
			continue
		}
		charge, chargeErr := refundableWalletOperationCharge(row)
		if chargeErr != nil || charge.UserID != operation.Sub2APIUserID {
			continue
		}
		// The latest period in force wins, so an older renewal can never be refunded
		// while a newer one paid for the period in use.
		if latest.OriginalOperationID != "" && !periodStart.After(latest.PeriodStart) {
			continue
		}
		latest = workspaceDeleteRefundBase{OriginalOperationID: stringValue(row["id"]), Charge: charge, PeriodStart: periodStart}
	}
	if latest.OriginalOperationID != "" {
		return latest, nil
	}
	launchRow, found, err := app.tables.GetRuntimeOperation(ctx, operation.LaunchOperationID)
	if err != nil || !found {
		return workspaceDeleteRefundBase{}, errWorkspaceDeleteUnconfirmed
	}
	charge, err := refundableWalletOperationCharge(launchRow)
	if err != nil || charge.UserID != operation.Sub2APIUserID {
		return workspaceDeleteRefundBase{}, errWorkspaceDeleteUnconfirmed
	}
	// The platform-confirmed fulfilment of the purchased resources is when the paid
	// period began for the customer, not the provider's order time.
	fulfilledAt, err := time.Parse(time.RFC3339Nano, operation.LaunchFulfilledAt)
	if err != nil {
		return workspaceDeleteRefundBase{}, errWorkspaceDeleteUnconfirmed
	}
	return workspaceDeleteRefundBase{OriginalOperationID: operation.LaunchOperationID, Charge: charge, PeriodStart: fulfilledAt}, nil
}

// workspaceDeleteRefundRemaining reports how much of the refunded order is still
// refundable: its confirmed charge minus every refund already reserved against it.
// refundOrderOf reports the order one recorded refund is reserved against. A refund
// recorded before the order was named belongs to the original purchase.
func refundOrderOf(operation workspaceDeleteRefundOperation, deleted workspaceDeleteOperation) string {
	if operation.RefundOrderOperationID != "" {
		return operation.RefundOrderOperationID
	}
	return deleted.LaunchOperationID
}

func (app *controlPlaneServer) workspaceDeleteRefundRemaining(ctx context.Context, originalOperationID, ownOperationID string, chargedUSDMicros int64) (int64, error) {
	operations, err := app.tables.ListRuntimeOperations(ctx)
	if err != nil {
		return 0, errWalletAdjustmentState
	}
	// The wallet owner's remaining-amount rule reads wallet adjustment rows, so the
	// refund owner hands it exactly those — excluding this Delete operation's own
	// refund. That reservation is the refund being evaluated, and counting it as
	// already spent would shrink the amount on every retry and make an unresolved
	// response unresolvable.
	rows := make([]map[string]any, 0, len(operations))
	for _, row := range operations {
		if stringValue(row["action"]) != "gateway.wallet_adjustment.v1" || stringValue(row["id"]) == ownOperationID {
			continue
		}
		rows = append(rows, row)
	}
	return walletRefundRemaining(chargedUSDMicros, originalOperationID, rows)
}

func (app *controlPlaneServer) dispatchWorkspaceDeleteRefund(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation,
	readback contracts.WorkspaceDeleteReadback, previous *workspaceDeleteRefundOperation) error {
	identity := workspaceDeleteRefundIdentity(operation)
	base, err := app.workspaceDeleteRefundBase(ctx, operation)
	if err != nil {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateReadbackIdentity)
	}
	charge := base.Charge
	deletedAt, deletedErr := time.Parse(time.RFC3339Nano, identity.WorkspaceDeletedAt)
	if deletedErr != nil || !deletedAt.After(base.PeriodStart) {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateOperationIncomplete)
	}
	refundMicros, err := contracts.PlatformWorkspaceDeleteRefundMicros(charge.AmountUSDMicros, base.PeriodStart, deletedAt)
	if err != nil {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateNotApplicable)
	}
	usedHours, refundHours := workspaceRefundBilledHours(base.PeriodStart, deletedAt)
	// The refund can never exceed what remains of the order it is reserved against:
	// the charge it paid, minus what was already refunded for that same order.
	walletOperationID := workspaceDeleteRefundWalletOperationID(operation.OperationID)
	remaining, err := app.workspaceDeleteRefundRemaining(ctx, base.OriginalOperationID, walletOperationID, charge.AmountUSDMicros)
	if err != nil {
		return app.recordWorkspaceDeleteRefundOutcome(ctx, operation, previous, workspaceDeleteRefundStatusBlocked, contracts.WorkspaceDeleteGateReadbackIdentity)
	}
	if refundMicros > remaining {
		refundMicros = remaining
	}
	// An unresolved dispatch keeps its frozen money facts. Recomputing them from a
	// later clock would change the idempotency key and the reserved amount, turning a
	// recoverable unknown response into a conflict. A refund recorded before this
	// order was named belongs to the original purchase.
	if previous != nil && previous.WalletAdjustmentOperationID != "" && refundOrderOf(*previous, operation) == base.OriginalOperationID {
		refundMicros, usedHours, refundHours = previous.RefundUSDMicros, previous.UsedHours, previous.RefundHours
	}
	record := workspaceDeleteRefundOperation{
		SchemaVersion: 1, AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID, DeleteOperationID: operation.OperationID,
		LaunchOperationID: operation.LaunchOperationID, RefundOrderOperationID: base.OriginalOperationID,
		PolicyVersion: contracts.WorkspaceRefundPolicyVersion,
		Status:        workspaceDeleteRefundStatusPending, OriginalChargeCode: charge.Code, OriginalChargeUSDMicros: charge.AmountUSDMicros,
		RefundUSDMicros: refundMicros, UsedHours: usedHours, RefundHours: refundHours,
		DeletedAt: identity.WorkspaceDeletedAt, ResourceFulfilledAt: base.PeriodStart.Format(time.RFC3339Nano),
		ReadbackID: readback.ReadbackID, ReadbackProvider: readback.Provider, ReadbackObservedAt: readback.ObservedAt,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if previous != nil {
		record.CreatedAt = previous.CreatedAt
	}
	if refundMicros == 0 {
		record.Status, record.ReasonCode = workspaceDeleteRefundStatusNotDue, ""
		return app.persistWorkspaceDeleteRefund(ctx, record)
	}
	record.WalletAdjustmentOperationID = walletOperationID
	requestHash := stableID("workspace-delete-refund-v2", operation.OperationID, base.OriginalOperationID, contracts.WorkspaceRefundPolicyVersion, charge.Code, formatWalletUSD(refundMicros))
	if err := app.ensureWorkspaceDeleteRefundWalletOperation(ctx, walletOperationID, requestHash, operation, base, refundMicros); err != nil {
		record.Status, record.ReasonCode = workspaceDeleteRefundStatusBlocked, workspaceDeleteRefundReasonWalletUnavailable
		return app.persistWorkspaceDeleteRefund(ctx, record)
	}
	audit := walletAdjustmentAuditIdentity{Actor: auditActor{UserID: workspaceDeleteRefundSystemActor, Role: "system"}}
	walletOperation, found, err := app.walletAdjustment(ctx, walletOperationID, "")
	if err != nil || !found {
		record.Status, record.ReasonCode = workspaceDeleteRefundStatusBlocked, workspaceDeleteRefundReasonWalletUnavailable
		return app.persistWorkspaceDeleteRefund(ctx, record)
	}
	if walletOperation.Status != "succeeded" {
		walletOperation, err = app.runWalletAdjustment(ctx, service, walletOperationID, walletOperation, audit)
		if err != nil && walletOperation.Status == "" {
			record.Status, record.ReasonCode = workspaceDeleteRefundStatusBlocked, workspaceDeleteRefundReasonDispatchUnavailable
			return app.persistWorkspaceDeleteRefund(ctx, record)
		}
	}
	confirmed, confirmErr := confirmWorkspaceLaunchRefund(ctx, service, walletRefundOperation{ID: walletOperationID, Operation: walletOperation}, operation.AccountID, charge.UserID)
	if confirmErr != nil {
		record.Status, record.ReasonCode = workspaceDeleteRefundStatusManualReview, "sub2api_refund_mismatch"
		return app.persistWorkspaceDeleteRefund(ctx, record)
	}
	if !confirmed {
		record.Status = workspaceDeleteRefundStatusManualReview
		if walletOperation.Status == "manual_review" {
			record.ReasonCode = firstNonEmpty(walletOperation.ErrorCode, "sub2api_refund_unconfirmed")
		} else {
			record.Status, record.ReasonCode = workspaceDeleteRefundStatusPending, "sub2api_refund_unconfirmed"
		}
		return app.persistWorkspaceDeleteRefund(ctx, record)
	}
	record.Status, record.ReasonCode, record.RefundReceiptID = workspaceDeleteRefundStatusSucceeded, "", walletOperation.ReceiptID
	return app.persistWorkspaceDeleteRefund(ctx, record)
}

// ensureWorkspaceDeleteRefundWalletOperation creates the single, deterministic
// platform refund operation bound to the original charge. The store validates
// the reservation against the original debit, so a Workspace can never be
// refunded beyond what the platform charged for it.
func (app *controlPlaneServer) ensureWorkspaceDeleteRefundWalletOperation(ctx context.Context, walletOperationID, requestHash string,
	operation workspaceDeleteOperation, base workspaceDeleteRefundBase, refundMicros int64) error {
	existing, found, err := app.walletAdjustment(ctx, walletOperationID, "")
	if err != nil {
		return err
	}
	if found {
		if existing.RequestHash == requestHash {
			return nil
		}
		// A refund that was already created for this Delete operation keeps its
		// reservation. It may carry an older identity-key form, but it must still
		// describe this Workspace's account, the same paying user and the same order,
		// so a different order or payer is never adopted silently.
		if existing.Kind == "business_refund" && existing.AccountID == operation.AccountID &&
			existing.RelatedOperationID == base.OriginalOperationID && existing.Sub2APIUserID == base.Charge.UserID {
			return nil
		}
		return errIdempotencyConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	refund := walletAdjustmentOperation{
		RequestHash: requestHash, Phase: "before_balance", AccountID: operation.AccountID, Sub2APIUserID: base.Charge.UserID,
		Kind: "business_refund", AmountUSDMicros: refundMicros, AmountUSD: formatWalletUSD(refundMicros), Reason: workspaceDeleteRefundReason,
		// The reservation belongs to the order that paid for the period in use, so
		// the wallet owner refunds against exactly that charge.
		RelatedOperationID: base.OriginalOperationID, ActorUserID: workspaceDeleteRefundSystemActor,
		CanonicalRedeemCode: walletAdjustmentRedeemCode(walletOperationID), RedeemCodeVersion: "v2",
		CreatedAt: now, UpdatedAt: now, Status: "pending",
	}
	if _, saveErr := app.tables.SaveWalletAdjustment(ctx, walletOperationID, refund); saveErr != nil && !errors.Is(saveErr, errIdempotencyConflict) {
		return saveErr
	}
	return nil
}

func workspaceRefundBilledHours(resourceFulfilledAt, workspaceDeletedAt time.Time) (int64, int64) {
	used := workspaceDeletedAt.Sub(resourceFulfilledAt)
	usedHours := int64(used / contracts.WorkspaceRefundHourDuration)
	if used%contracts.WorkspaceRefundHourDuration != 0 {
		usedHours++
	}
	refundHours := int64(contracts.WorkspaceRefundMonthlyHours) - usedHours
	if refundHours < 0 {
		refundHours = 0
	}
	return usedHours, refundHours
}
