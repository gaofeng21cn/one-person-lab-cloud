package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceDeleteFabric struct {
	fakeFabricClient
	mu                         sync.Mutex
	calls                      []string
	failStage                  string
	failures                   int
	mismatchStage              string
	unknownStage               string
	storageStatus              string
	computeReads               []string
	computeTerminal            <-chan struct{}
	destroyed                  bool
	clearObservationsOnDestroy bool
	runtimeResponseLost        bool
	observeState               string
	secretObserveState         string
	residualObserveState       string
	observeErr                 error
	observeRuntimeID           string
	observeKeyID               int64
	observeSecretRef           string
	observeFingerprint         string
	residualObservedAt         string
	residualReadbackID         *string
	storageResults             []workspaceDeleteStorageResult
	storageDestroyed           bool
	storageReadbackResults     []clients.StorageVolume
	storageReadObservedAt      *string
	storageReadbackID          *string
	computeReadbackResults     []clients.ComputeAllocation
	computeDestroyResults      []clients.ComputeAllocation
	storageReadStatus          string
	storageReadCBSStatus       string
	storageReadProvider        string
	storageProviderResourceID  string
	storageDestroyResourceID   string
	storageBindingPresent      *bool
	computeReadCVMStatus       string
	computeReadTKEStatus       string
	computeReadMachinePresent  *bool
	computeReadProvider        string
	computeReadMachineName     string
	computeReadInstanceID      string
	computeReadErr             error
	storageReadErr             error
	events                     *workspaceDeleteEvents
}

type workspaceDeleteEvents struct {
	mu    sync.Mutex
	items []string
}

func (e *workspaceDeleteEvents) add(value string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.items = append(e.items, value)
}

func (e *workspaceDeleteEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.items...)
}

func (f *workspaceDeleteFabric) call(stage, id, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, stage+":"+id+":"+key)
	f.events.add("fabric:" + stage)
	if f.failStage == stage && f.failures > 0 {
		f.failures--
		return errors.New("fabric owner unavailable")
	}
	return nil
}

func (f *workspaceDeleteFabric) DestroyWorkspaceRuntime(_ context.Context, _, workspaceID, key string) (clients.WorkspaceRuntime, error) {
	if err := f.call("runtime", workspaceID, key); err != nil {
		return clients.WorkspaceRuntime{}, err
	}
	if f.mismatchStage == "runtime" {
		workspaceID = "ws-other"
	}
	status := "destroyed"
	if f.unknownStage == "runtime" {
		status = "unknown"
	}
	f.mu.Lock()
	f.destroyed = status == "destroyed"
	if f.destroyed && f.clearObservationsOnDestroy {
		f.observeState, f.secretObserveState, f.residualObserveState = "", "", ""
	}
	f.mu.Unlock()
	if f.runtimeResponseLost {
		return clients.WorkspaceRuntime{}, errors.New("runtime destroy response lost")
	}
	return clients.WorkspaceRuntime{ID: "runtime-alpha", WorkspaceID: workspaceID, Status: status}, nil
}

func (f *workspaceDeleteFabric) ObserveWorkspaceRuntime(_ context.Context, workspaceID string) (clients.WorkspaceRuntimeObservation, error) {
	if err := f.call("runtime-read", workspaceID, ""); err != nil {
		return clients.WorkspaceRuntimeObservation{}, err
	}
	f.mu.Lock()
	destroyed, state, observeErr := f.destroyed, f.observeState, f.observeErr
	f.mu.Unlock()
	if observeErr != nil {
		return clients.WorkspaceRuntimeObservation{}, observeErr
	}
	if state == "" {
		state = clients.WorkspaceOwnerObservationReady
		if destroyed {
			state = clients.WorkspaceOwnerObservationAbsent
		}
	}
	observation := clients.WorkspaceRuntimeObservation{SchemaVersion: clients.WorkspaceOwnerObservationSchemaVersion, State: state, WorkspaceID: workspaceID}
	if state == clients.WorkspaceOwnerObservationReady || state == clients.WorkspaceOwnerObservationPending {
		runtimeID := f.observeRuntimeID
		if runtimeID == "" {
			runtimeID = "runtime-alpha"
		}
		observation.Runtime = &clients.WorkspaceRuntime{ID: runtimeID, WorkspaceID: workspaceID, Status: "running", Ready: state == clients.WorkspaceOwnerObservationReady}
	}
	return observation, nil
}

func (f *workspaceDeleteFabric) ObserveWorkspaceRuntimeGatewaySecret(_ context.Context, workspaceID string) (clients.WorkspaceRuntimeGatewaySecretObservation, error) {
	if err := f.call("secret-read", workspaceID, ""); err != nil {
		return clients.WorkspaceRuntimeGatewaySecretObservation{}, err
	}
	f.mu.Lock()
	destroyed, state, observeErr := f.destroyed, f.secretObserveState, f.observeErr
	f.mu.Unlock()
	if observeErr != nil {
		return clients.WorkspaceRuntimeGatewaySecretObservation{}, observeErr
	}
	if state == "" {
		state = f.observeState
		if state == "" {
			state = clients.WorkspaceOwnerObservationReady
		}
		if destroyed && f.secretObserveState == "" {
			state = clients.WorkspaceOwnerObservationAbsent
		}
	}
	observation := clients.WorkspaceRuntimeGatewaySecretObservation{SchemaVersion: clients.WorkspaceOwnerObservationSchemaVersion, State: state, WorkspaceID: workspaceID}
	if state == clients.WorkspaceOwnerObservationReady || state == clients.WorkspaceOwnerObservationPending {
		keyID := f.observeKeyID
		if keyID == 0 {
			keyID = 19
		}
		secretRef := f.observeSecretRef
		if secretRef == "" {
			secretRef = "opl-gateway-ws-alpha"
		}
		fingerprint := f.observeFingerprint
		if fingerprint == "" {
			fingerprint = "sha256:" + strings.Repeat("a", 64)
		}
		observation.Binding = &clients.WorkspaceRuntimeGatewaySecretBinding{
			WorkspaceID: workspaceID, WorkspaceAPIKeyID: keyID, SecretRef: secretRef, Fingerprint: fingerprint, Bound: state == clients.WorkspaceOwnerObservationReady,
		}
	}
	return observation, nil
}

func (f *workspaceDeleteFabric) ObserveWorkspaceRuntimeDelete(_ context.Context, workspaceID string) (clients.WorkspaceRuntimeDeleteObservation, error) {
	if err := f.call("runtime-residual-read", workspaceID, ""); err != nil {
		return clients.WorkspaceRuntimeDeleteObservation{}, err
	}
	f.mu.Lock()
	destroyed, observeErr := f.destroyed, f.observeErr
	observedAt, readbackOverride := f.residualObservedAt, f.residualReadbackID
	residuals, state := f.residualsForObservation(workspaceID), f.residualObserveState
	f.mu.Unlock()
	if observeErr != nil {
		return clients.WorkspaceRuntimeDeleteObservation{}, observeErr
	}
	if state == "" {
		state = clients.WorkspaceOwnerObservationAbsent
		if !destroyed {
			state = clients.WorkspaceRuntimeDeleteObservationPresent
		}
	}
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	readbackID := "run-readback-" + workspaceID
	if readbackOverride != nil {
		readbackID = *readbackOverride
	}
	observation := clients.WorkspaceRuntimeDeleteObservation{
		SchemaVersion: clients.WorkspaceRuntimeDeleteObservationSchemaVersion,
		State:         state,
		WorkspaceID:   workspaceID,
		// Real Fabric stamps its own observation time and readback identity.
		ObservedAt: observedAt,
		ReadbackID: readbackID,
		Residuals:  residuals,
	}
	if state == clients.WorkspaceOwnerObservationAbsent {
		observation.State = clients.WorkspaceOwnerObservationAbsent
		observation.Residuals = nil
	}
	return observation, nil
}

func (f *workspaceDeleteFabric) DetachStorageAttachment(_ context.Context, _, _, attachmentID, key string) (clients.StorageAttachment, error) {
	if err := f.call("attachment", attachmentID, key); err != nil {
		return clients.StorageAttachment{}, err
	}
	workspaceID := "ws-alpha"
	if f.mismatchStage == "attachment" {
		workspaceID = "ws-other"
	}
	status := "detached"
	if f.unknownStage == "attachment" {
		status = "unknown"
	}
	return clients.StorageAttachment{ID: attachmentID, WorkspaceID: workspaceID, ComputeID: "compute-alpha", VolumeID: "storage-alpha", Status: status}, nil
}

// workspaceDeleteStorageResult models one Fabric storage deletion outcome: its
// lifecycle status plus the stable retryable classification Fabric reports.
type workspaceDeleteStorageResult struct {
	Status       string
	DestroyState string
}

func (f *workspaceDeleteFabric) DestroyStorageVolume(_ context.Context, _, _, storageID, key string) (clients.StorageVolume, error) {
	if err := f.call("storage", storageID, key); err != nil {
		return clients.StorageVolume{}, err
	}
	workspaceID := "ws-alpha"
	if f.mismatchStage == "storage" {
		workspaceID = "ws-other"
	}
	result := workspaceDeleteStorageResult{Status: "destroyed"}
	f.mu.Lock()
	if len(f.storageResults) > 0 {
		result, f.storageResults = f.storageResults[0], f.storageResults[1:]
	}
	if f.storageStatus != "" {
		result.Status = f.storageStatus
	}
	if f.unknownStage == "storage" {
		result.Status, result.DestroyState = "unknown", ""
	}
	providerResourceID := f.storageDestroyResourceID
	if result.Status == "destroyed" || result.Status == "external_deleted" {
		f.storageDestroyed = true
	}
	f.mu.Unlock()
	return clients.StorageVolume{
		ID: storageID, WorkspaceID: workspaceID, Status: result.Status, ProviderResourceID: providerResourceID,
		DestroyState: result.DestroyState, ProviderRequestID: "req-storage-destroy",
	}, nil
}

// residualsForObservation reports the labelled Runtime objects this fake's readback
// observed. It is a separate helper so the observation time and readback identity stay
// independently pinnable by a test.
func (f *workspaceDeleteFabric) residualsForObservation(workspaceID string) []clients.WorkspaceRuntimeDeleteResidual {
	if f.residualObserveState == clients.WorkspaceOwnerObservationAbsent {
		return nil
	}
	return []clients.WorkspaceRuntimeDeleteResidual{{Kind: "NetworkPolicy", Name: workspaceID}}
}

func (f *workspaceDeleteFabric) ReadStorageVolume(_ context.Context, storageID string) (clients.StorageVolume, error) {
	if err := f.call("storage-read", storageID, ""); err != nil {
		return clients.StorageVolume{}, err
	}
	f.mu.Lock()
	destroyed, readErr := f.destroyed, f.storageReadErr
	status, cbsStatus, provider := f.storageReadStatus, f.storageReadCBSStatus, f.storageReadProvider
	providerResourceID, bindingPresent := f.storageProviderResourceID, f.storageBindingPresent
	mismatch := f.mismatchStage == "storage-read"
	storageDestroyed := f.storageDestroyed
	observedAt, readbackID := f.storageReadObservedAt, f.storageReadbackID
	var queued *clients.StorageVolume
	if len(f.storageReadbackResults) > 0 {
		readback := f.storageReadbackResults[0]
		queued = &readback
		f.storageReadbackResults = f.storageReadbackResults[1:]
	}
	f.mu.Unlock()
	if readErr != nil && queued == nil {
		return clients.StorageVolume{}, readErr
	}
	workspaceID := "ws-alpha"
	if mismatch {
		workspaceID = "ws-other"
	}
	if provider == "" {
		provider = "fabric"
	}
	if status == "" && storageDestroyed {
		status, cbsStatus = "external_deleted", contracts.WorkspaceDeleteProviderStatusNotFound
	}
	if providerResourceID == "" && provider == "fabric" {
		providerResourceID = "volume-" + storageID
	}
	volume := clients.StorageVolume{ID: storageID, WorkspaceID: workspaceID, Provider: provider, ProviderResourceID: providerResourceID, Status: status, CBSStatus: cbsStatus}
	if bindingPresent != nil {
		volume.BindingPresent = bindingPresent
	} else if destroyed || storageDestroyed {
		absent := false
		volume.BindingPresent = &absent
	}
	if queued != nil {
		volume = *queued
	}
	volume.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	volume.ReadbackID = "storage-readback-" + storageID
	if observedAt != nil {
		volume.ObservedAt = *observedAt
	}
	if readbackID != nil {
		volume.ReadbackID = *readbackID
	}
	return volume, nil
}

// ReadComputeDestroyStatus is the provider-authoritative absence readback. The
// fake models it with the same sequencing as its persisted compute read so the
// deletion chain and the refund readback observe one provider truth.
func (f *workspaceDeleteFabric) ReadComputeDestroyStatus(ctx context.Context, computeID string) (clients.ComputeAllocation, error) {
	return f.ReadComputeAllocation(ctx, computeID)
}

func (f *workspaceDeleteFabric) DestroyComputeAllocation(_ context.Context, _, _, computeID, key string) (clients.ComputeAllocation, error) {
	if err := f.call("compute", computeID, key); err != nil {
		return clients.ComputeAllocation{}, err
	}
	workspaceID := "ws-alpha"
	if f.mismatchStage == "compute" {
		workspaceID = "ws-other"
	}
	status := "destroying"
	destroyState := ""
	f.mu.Lock()
	if len(f.computeDestroyResults) > 0 {
		result := f.computeDestroyResults[0]
		f.computeDestroyResults = f.computeDestroyResults[1:]
		status, destroyState = result.Status, result.DestroyState
	}
	if f.unknownStage == "compute" {
		status, destroyState = "unknown", ""
	}
	f.mu.Unlock()
	return clients.ComputeAllocation{
		ID: computeID, WorkspaceID: workspaceID, Status: status, DestroyState: destroyState,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ReadbackID: "compute-readback-" + computeID,
		ProviderRequestID: "req-compute-destroy",
	}, nil
}

func (f *workspaceDeleteFabric) ReadComputeAllocation(_ context.Context, computeID string) (clients.ComputeAllocation, error) {
	if err := f.call("compute-read", computeID, ""); err != nil {
		return clients.ComputeAllocation{}, err
	}
	f.mu.Lock()
	var typed *clients.ComputeAllocation
	if len(f.computeReadbackResults) > 0 {
		copy := f.computeReadbackResults[0]
		typed, f.computeReadbackResults = &copy, f.computeReadbackResults[1:]
	}
	f.mu.Unlock()
	if typed != nil {
		readback := *typed
		readback.ID, readback.WorkspaceID = computeID, "ws-alpha"
		if readback.ObservedAt == "" {
			readback.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if readback.ReadbackID == "" {
			readback.ReadbackID = "compute-readback-" + computeID
		}
		if f.mismatchStage == "compute-read" {
			readback.WorkspaceID = "ws-other"
		}
		return readback, nil
	}
	f.mu.Lock()
	status := "destroyed"
	if f.computeTerminal != nil {
		select {
		case <-f.computeTerminal:
			status = "destroyed"
		default:
			status = "destroying"
		}
	} else if len(f.computeReads) > 0 {
		status = f.computeReads[0]
		f.computeReads = f.computeReads[1:]
	}
	if f.unknownStage == "compute-read" {
		status = "unknown"
	}
	cvmStatus, tkeStatus, machinePresent, provider, readErr := f.computeReadCVMStatus, f.computeReadTKEStatus, f.computeReadMachinePresent, f.computeReadProvider, f.computeReadErr
	machineName, instanceID := f.computeReadMachineName, f.computeReadInstanceID
	f.mu.Unlock()
	if readErr != nil {
		return clients.ComputeAllocation{}, readErr
	}
	workspaceID := "ws-alpha"
	if f.mismatchStage == "compute-read" {
		workspaceID = "ws-other"
	}
	allocation := clients.ComputeAllocation{
		ID: computeID, WorkspaceID: workspaceID, Status: status, Provider: provider, CVMStatus: cvmStatus, TKEStatus: tkeStatus,
		MachinePresent: machinePresent, MachineName: machineName, InstanceID: instanceID,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ReadbackID: "compute-readback-" + computeID,
	}
	if provider == "" {
		allocation.Provider = "fabric"
	}
	return allocation, nil
}

// RuntimeObservations answers the operator observation read. This fake owns no
// runtime inventory, so it reports an empty typed result.
func (f *workspaceDeleteFabric) RuntimeObservations(_ context.Context) (contracts.RuntimeObservations, error) {
	// The typed readback requires a non-nil item list, not a missing one.
	return contracts.RuntimeObservations{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []contracts.RuntimeObservation{}}, nil
}

func (f *workspaceDeleteFabric) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type workspaceDeleteFixture struct {
	server             http.Handler
	store              controlPlaneTableStore
	session            *httptest.ResponseRecorder
	fabric             *workspaceDeleteFabric
	workspace          map[string]any
	billingPeriodStart time.Time
}

type workspaceDeleteSub2API struct {
	*testSub2APIClient
	mu                 sync.Mutex
	userID             int64
	keyID              int64
	keyExists          bool
	keyReads           int
	keyDeletes         int
	keyDeleteKeys      []string
	history            map[string]clients.Sub2APIBalanceHistoryEntry
	historyReads       [][]string
	refunds            []clients.Sub2APIRefundInput
	keyReadErr         error
	keyDeleteErr       error
	keyResponseLost    bool
	refundResponseLost bool
	keyName            string
	keyUserID          int64
	keyStatus          string
	events             *workspaceDeleteEvents
}

func (s *workspaceDeleteSub2API) UserKey(_ context.Context, _ clients.SessionDelegatedCredential, userID, keyID int64) (clients.Sub2APIWorkspaceKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyReads++
	s.events.add("sub2api:key-get")
	if s.keyReadErr != nil {
		return clients.Sub2APIWorkspaceKey{}, s.keyReadErr
	}
	if userID != s.userID || keyID != s.keyID || !s.keyExists {
		return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIKeyNotFound
	}
	returnedUserID := userID
	if s.keyUserID != 0 {
		returnedUserID = s.keyUserID
	}
	name := s.keyName
	if name == "" {
		name = workspaceReservedKeyName("ws-alpha")
	}
	status := s.keyStatus
	if status == "" {
		status = "active"
	}
	return clients.Sub2APIWorkspaceKey{ID: keyID, UserID: returnedUserID, Name: name, Status: status}, nil
}

func (s *workspaceDeleteSub2API) DeleteUserKey(_ context.Context, _ clients.SessionDelegatedCredential, userID, keyID int64) error {
	return s.DeleteUserKeyIdempotent(context.Background(), clients.SessionDelegatedCredential{}, userID, keyID, "")
}

func (s *workspaceDeleteSub2API) DeleteUserKeyIdempotent(_ context.Context, _ clients.SessionDelegatedCredential, userID, keyID int64, idempotencyKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyDeletes++
	s.keyDeleteKeys = append(s.keyDeleteKeys, idempotencyKey)
	s.events.add("sub2api:key-delete")
	if s.keyDeleteErr != nil {
		return s.keyDeleteErr
	}
	if userID != s.userID || keyID != s.keyID || !s.keyExists {
		return clients.ErrSub2APIKeyNotFound
	}
	s.keyExists = false
	if s.keyResponseLost {
		return errors.New("key delete response lost")
	}
	return nil
}

func (s *workspaceDeleteSub2API) CreateUserKey(context.Context, clients.SessionDelegatedCredential, int64, clients.Sub2APICreateKeyInput, string) (clients.Sub2APIWorkspaceKey, error) {
	return clients.Sub2APIWorkspaceKey{}, errors.New("unexpected key create")
}

func (s *workspaceDeleteSub2API) UpdateUserKey(context.Context, clients.SessionDelegatedCredential, int64, int64, clients.Sub2APIUpdateKeyInput) (clients.Sub2APIWorkspaceKey, error) {
	return clients.Sub2APIWorkspaceKey{}, errors.New("unexpected key update")
}

func (s *workspaceDeleteSub2API) FinancialBalanceHistoryByCodes(_ context.Context, userID int64, codes []string) (map[string]clients.Sub2APIBalanceHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyReads = append(s.historyReads, append([]string(nil), codes...))
	if len(codes) == 1 && codes[0] == "opl:workspace-purchase-alpha" {
		s.events.add("sub2api:debit-get")
	} else {
		s.events.add("sub2api:refund-get")
	}
	result := make(map[string]clients.Sub2APIBalanceHistoryEntry, len(codes))
	for _, code := range codes {
		if entry, ok := s.history[code]; ok {
			result[code] = entry
		}
	}
	return result, nil
}

func (s *workspaceDeleteSub2API) Refund(_ context.Context, input clients.Sub2APIRefundInput) (clients.Sub2APIRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refunds = append(s.refunds, input)
	s.events.add("sub2api:refund-post")
	usedBy := input.UserID
	now := time.Now().UTC()
	s.history[input.Code] = clients.Sub2APIBalanceHistoryEntry{
		Code: input.Code, Type: "balance", ValueUSDMicros: input.RefundUSDMicros, Status: "used", UsedBy: &usedBy, UsedAt: &now,
	}
	if s.refundResponseLost {
		return clients.Sub2APIRefund{}, errors.New("refund response lost")
	}
	return clients.Sub2APIRefund{Code: input.Code, UserID: input.UserID, RefundUSDMicros: input.RefundUSDMicros, Status: "used"}, nil
}

type workspaceDeleteLedger struct {
	fakeLedgerClient
	mu               sync.Mutex
	purchase         clients.Receipt
	reads            int
	receipts         []clients.ReceiptInput
	keys             []string
	recorded         map[string]clients.Receipt
	failures         int
	receiptCreatedAt string
	events           *workspaceDeleteEvents
}

func (l *workspaceDeleteLedger) Receipt(_ context.Context, receiptID string) (clients.Receipt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads++
	l.events.add("ledger:purchase-get")
	if receiptID == l.purchase.ReceiptID && receiptID != "" {
		return l.purchase, nil
	}
	if receipt, ok := l.recorded[receiptID]; ok {
		return receipt, nil
	}
	return clients.Receipt{}, errors.New("receipt not found")
}

func (l *workspaceDeleteLedger) ReceiptForAccount(_ context.Context, accountID, workspaceID, receiptID string) (clients.Receipt, error) {
	receipt, err := l.Receipt(context.Background(), receiptID)
	// An empty workspace scope means "no workspace filter", matching the real
	// Ledger client, which omits the query parameter instead of requiring it.
	if err != nil || receipt.AccountID != accountID || workspaceID != "" && receipt.WorkspaceID != workspaceID {
		return clients.Receipt{}, errors.New("receipt scope mismatch")
	}
	return receipt, nil
}

func (l *workspaceDeleteLedger) RecordReceipt(_ context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = append(l.receipts, input)
	l.keys = append(l.keys, key)
	l.events.add("ledger:deletion-receipt")
	if l.failures > 0 {
		l.failures--
		return clients.Receipt{}, errors.New("ledger receipt unavailable")
	}
	receiptID := "receipt-delete-alpha"
	if len(l.receipts) > 1 {
		receiptID = "receipt-delete-alpha-" + strconv.Itoa(len(l.receipts))
	}
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	if l.receiptCreatedAt != "" {
		createdAt = l.receiptCreatedAt
	}
	receipt := clients.Receipt{ReceiptInput: input, ReceiptID: receiptID, CreatedAt: createdAt}
	if l.recorded == nil {
		l.recorded = map[string]clients.Receipt{}
	}
	l.recorded[receiptID] = receipt
	return receipt, nil
}

type workspaceDeleteNoopDeleteStore struct {
	controlPlaneTableStore
}

type workspaceDeleteEventStore struct {
	controlPlaneTableStore
	events *workspaceDeleteEvents
}

type workspaceDeleteAuditFailStore struct {
	controlPlaneTableStore
	failAudit bool
}

func (s workspaceDeleteEventStore) ApplyWorkspaceDelete(ctx context.Context, mutation workspaceDeleteStoreMutation) error {
	err := s.controlPlaneTableStore.ApplyWorkspaceDelete(ctx, mutation)
	if err == nil && mutation.DeleteWorkspace {
		s.events.add("control-plane:workspace-absent")
	}
	return err
}

func (s workspaceDeleteNoopDeleteStore) ApplyWorkspaceDelete(ctx context.Context, mutation workspaceDeleteStoreMutation) error {
	mutation.DeleteWorkspace = false
	return s.controlPlaneTableStore.ApplyWorkspaceDelete(ctx, mutation)
}

func (s *workspaceDeleteAuditFailStore) SaveAuditEvent(ctx context.Context, event map[string]any) error {
	if s.failAudit {
		return errors.New("injected audit failure")
	}
	return s.controlPlaneTableStore.SaveAuditEvent(ctx, event)
}

func newWorkspaceDeleteFixture(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric) workspaceDeleteFixture {
	t.Helper()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, store, fabric)
	return fixture
}

func newWorkspaceDeleteFixtureWithService(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric, service *controlplane.Service) workspaceDeleteFixture {
	t.Helper()
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	handler := server.(*controlPlaneHTTPHandler)
	users, err := handler.app.tables.ListUsers(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	var owner map[string]any
	for _, user := range users {
		if stringValue(user["accountId"]) == "acct-alpha" {
			owner = user
			break
		}
	}
	if owner == nil {
		t.Fatal("tenant owner missing")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	workspace := map[string]any{
		"id": "ws-alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": owner["id"],
		"state": "running", "status": "running", "currentComputeAllocationId": "compute-alpha", "storageId": "storage-alpha",
		"currentAttachmentId": "attachment-alpha", "runtimeId": "runtime-alpha", "createdAt": now, "updatedAt": now,
	}
	if err := handler.app.tables.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	return workspaceDeleteFixture{server: server, store: handler.app.tables, session: session, fabric: fabric, workspace: workspace}
}

func newWorkspaceDeleteCompletionFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger) {
	t.Helper()
	return newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), &workspaceDeleteFabric{})
}

func newWorkspaceDeleteEventFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger, *workspaceDeleteEvents) {
	t.Helper()
	events := &workspaceDeleteEvents{}
	fabric := &workspaceDeleteFabric{events: events}
	store := workspaceDeleteEventStore{controlPlaneTableStore: newMemoryTableStore(), events: events}
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, store, fabric)
	sub2API.events, ledger.events = events, events
	return fixture, sub2API, ledger, events
}

// The delete fixture models a Workspace that was purchased a fixed age before
// the fixture clock, so its paid period and its refund window are a fixed
// distance from the deletion moment.
//
// A calendar-anchored period cannot do this: the purchase then sits anywhere
// between 0 and 31 days in the past, which lands on the edge of the platform's
// 720-hour monthly refund baseline twice a month. On those days the refund is
// correctly zero, the refund is not dispatched, and the suite fails on a
// calendar date instead of on the behaviour under test.
const workspaceDeleteFixturePurchaseAge = 15 * 24 * time.Hour

// The refund window of a delete fixture must stay this many hours away from
// both ends of the monthly baseline. A fixture that drifts onto an edge makes
// the refund either expire or cover the whole order, and the failure that
// follows is a vague "no refund was dispatched" instead of a named cause.
const workspaceDeleteFixtureRefundMarginHours = 24

// workspaceDeleteFixtureBillingPeriod returns the paid period of a Workspace
// purchased at a fixed age before the clock. The paid period is the calendar
// month the purchase started, computed exactly like the billing owner computes
// it, so the fixture's anchor day stays consistent with its own period.
func workspaceDeleteFixtureBillingPeriod(clock time.Time) (time.Time, time.Time) {
	periodStart := clock.UTC().Add(-workspaceDeleteFixturePurchaseAge)
	return periodStart, nextBillingMonth(periodStart, periodStart.Day())
}

func newWorkspaceDeleteCompletionFixtureWith(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger) {
	t.Helper()
	sub2API := &workspaceDeleteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000_000, charges: map[string]int64{}},
		userID:            41, keyID: 19, keyExists: true, history: map[string]clients.Sub2APIBalanceHistoryEntry{},
	}
	ledger := &workspaceDeleteLedger{}
	service := controlplane.NewService(ledger, fabric, sub2API)
	fixture := newWorkspaceDeleteFixtureWithService(t, store, fabric, service)
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "workspace-launch-alpha", "acct-alpha", stringValue(fixture.workspace["ownerUserId"]), 41
	command.WorkspaceID, command.Name = "ws-alpha", "Alpha"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	fixtureClock := time.Now().UTC()
	periodStart, paidThrough := workspaceDeleteFixtureBillingPeriod(fixtureClock)
	fixture.billingPeriodStart = periodStart
	ledger.receiptCreatedAt = fixtureClock.Format(time.RFC3339Nano)
	readyFacts := map[string]any{
		"sub2apiRedeemCode": "opl:workspace-purchase-alpha",
		"workspaceApiKeyId": int64(19), "workspaceKeyStatus": workspaceKeyCodexGroupBound, "workspaceKeyFingerprint": "sha256:" + strings.Repeat("a", 64),
		"chargeAttempted": true, "chargeConfirmation": map[string]any{"code": "opl:workspace-purchase-alpha", "userId": int64(41), "chargeUsdMicros": int64(52_580_000), "status": "used"},
		"postChargeBalanceUsdMicros": int64(947_420_000), "postChargeBalanceKnown": true, "billingPeriodState": "frozen",
		"periodStart": periodStart.Format(time.RFC3339Nano), "paidThrough": paidThrough.Format(time.RFC3339Nano), "billingAnchorDay": periodStart.Day(),
		"computeAllocationId": "compute-alpha", "computeBindingRef": "workspace-launch-alpha:compute", "storageId": "storage-alpha", "storageBindingRef": "workspace-launch-alpha:storage",
		"attachmentId": "attachment-alpha", "attachmentBindingRef": "workspace-launch-alpha:attachment",
		"gatewaySecretRef": "opl-gateway-ws-alpha", "gatewaySecretVersion": "v1", "secretBindingRef": "workspace-launch-alpha:secret",
		"runtimeId": "runtime-alpha", "runtimeReady": true, "runtimeServiceName": "runtime-alpha", "runtimeBindingRef": "workspace-launch-alpha:runtime",
		"url": "https://workspace.example/alpha", "runtimeUsername": "opl", "credentialStatus": "configured", "credentialVersion": "v1", "credentialSecretRef": "runtime-secret-alpha",
		"activationOperationId": "workspace-launch-alpha:activation", "workspaceActivatedAt": periodStart.Format(time.RFC3339Nano),
		"receiptId": "receipt-purchase-alpha", "receiptOperationId": "workspace-launch-alpha:purchase-receipt",
	}
	for key, value := range readyFacts {
		operation.raw[key], _ = json.Marshal(value)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), launchRow); err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	workspace["purchaseReceiptId"] = "receipt-purchase-alpha"
	workspace["sub2apiUserId"], workspace["sub2apiRedeemCode"] = int64(41), "opl:workspace-purchase-alpha"
	purchaseInput, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	ledger.purchase = clients.Receipt{ReceiptInput: purchaseInput, ReceiptID: "receipt-purchase-alpha"}
	usedBy := int64(41)
	usedAt := periodStart
	sub2API.history["opl:workspace-purchase-alpha"] = clients.Sub2APIBalanceHistoryEntry{
		Code: "opl:workspace-purchase-alpha", Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy, UsedAt: &usedAt,
	}
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveStorage(context.Background(), map[string]any{
		"id": "storage-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "available",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveAttachment(context.Background(), map[string]any{
		"id": "attachment-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "volumeId": "storage-alpha", "status": "attached",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.workspace = workspace
	return fixture, sub2API, ledger
}

// The delete command still owns the durable, identity-bound authorization of the
// customer's deletion, so it accepts the exact current and the historical launch
// receipt forms and records the operation. The delete execution engine is retired:
// the command advances no stage, so it moves no money and records no receipt.
func TestWorkspaceDeleteAcceptsExactCurrentAndHistoricalLaunchReceiptsWithoutMoneyCalls(t *testing.T) {
	t.Run("historical charged", func(t *testing.T) {
		fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
		ledger.purchase.ReceiptInput = workspaceLaunchHistoricalChargedReceiptInput(ledger.purchase.ReceiptInput)
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-historical-charged")
		if response.Code != http.StatusAccepted || len(sub2API.historyReads) != 0 || len(sub2API.refunds) != 0 || sub2API.keyDeletes != 0 || len(ledger.receipts) != 0 {
			t.Fatalf("historical charged status=%d history=%#v refunds=%d keyDeletes=%d receipts=%d body=%s", response.Code, sub2API.historyReads, len(sub2API.refunds), sub2API.keyDeletes, len(ledger.receipts), response.Body.String())
		}
		assertWorkspaceDeleteAuthorizationRecorded(t, fixture)
	})

	for _, legacy := range []bool{false, true} {
		name := "current zero-cost"
		if legacy {
			name = "legacy zero-cost"
		}
		t.Run(name, func(t *testing.T) {
			fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
			configureWorkspaceDeleteZeroCostLaunch(t, fixture, ledger, legacy)
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-zero-cost")
			if response.Code != http.StatusAccepted || len(sub2API.historyReads) != 0 || len(sub2API.refunds) != 0 || sub2API.keyDeletes != 0 || len(ledger.receipts) != 0 {
				t.Fatalf("zero-cost legacy=%v status=%d history=%#v refunds=%d keyDeletes=%d receipts=%#v body=%s", legacy, response.Code, sub2API.historyReads, len(sub2API.refunds), sub2API.keyDeletes, ledger.receipts, response.Body.String())
			}
			assertWorkspaceDeleteAuthorizationRecorded(t, fixture)
		})
	}
}

// assertWorkspaceDeleteAuthorizationRecorded proves that the command persisted
// the customer's deletion as one identity-bound operation bound to the original
// launch receipt, without executing it.
func assertWorkspaceDeleteAuthorizationRecorded(t *testing.T, fixture workspaceDeleteFixture) {
	t.Helper()
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
	if err != nil || !found {
		t.Fatalf("delete operation found=%v err=%v", found, err)
	}
	operation, decodeErr := decodeWorkspaceDeleteOperation(row)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if operation.LaunchReceiptID != "receipt-purchase-alpha" || operation.LaunchOperationID != "workspace-launch-alpha" ||
		operation.Phase != "claimed" || operation.Status != "running" || operation.DeletionReceiptID != "" {
		t.Fatalf("delete authorization operation=%#v", operation)
	}
}

// seedCompletedWorkspaceDeleteOperation persists a completed Delete operation, the
// terminal record the retired execution engine used to write. The Workspace
// projection is removed with it, exactly as the committed deletion removed it.
func seedCompletedWorkspaceDeleteOperation(t *testing.T, fixture workspaceDeleteFixture) {
	t.Helper()
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-seed-complete")
	if response.Code != http.StatusAccepted {
		t.Fatalf("seed delete status=%d body=%s", response.Code, response.Body.String())
	}
	seedWorkspaceDeleteOperation(t, fixture, func(operation *workspaceDeleteOperation) {
		operation.Phase, operation.Status = "complete", "succeeded"
		operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
		operation.AttachmentStatus, operation.StorageStatus = "absent", "absent"
		operation.ComputeStatus = "absent"
		operation.ComputeReadbacks, operation.MaxComputeReadbacks = 1, workspaceDeleteComputeReadbackBudget
		operation.DeletionReceiptID = "receipt-delete-alpha"
	})
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
}

func configureWorkspaceDeleteZeroCostLaunch(t *testing.T, fixture workspaceDeleteFixture, ledger *workspaceDeleteLedger, legacy bool) {
	t.Helper()
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), "workspace-launch-alpha")
	if err != nil || !found {
		t.Fatalf("launch found=%v err=%v", found, err)
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{
		"resourceBillingEnabled": false, "chargeAttempted": false, "chargeConfirmation": map[string]any{"status": "not_charged"},
		"preChargeBalanceUsdMicros": int64(0), "postChargeBalanceUsdMicros": int64(0), "postChargeBalanceKnown": true, "billingPeriodState": "not_billed",
		"totalChargeUsdMicros": int64(0),
	} {
		operation.raw[key], _ = json.Marshal(value)
	}
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), launchRow); err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	workspace["purchaseReceiptId"] = "receipt-purchase-alpha"
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	input, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		input = workspaceLaunchLegacyCreatedReceiptInput(operation)
	}
	ledger.purchase = clients.Receipt{ReceiptInput: input, ReceiptID: "receipt-purchase-alpha"}
}

func TestWorkspaceDeleteHistoricalV1IsReadOnly(t *testing.T) {
	if workspaceDeleteOperationID("ws-alpha") == workspaceDeleteLegacyOperationID("ws-alpha") {
		t.Fatal("v2 and historical v1 operation IDs must differ")
	}

	t.Run("completed replay with Workspace absent", func(t *testing.T) {
		fixture, sub2API, ledger, events := newWorkspaceDeleteEventFixture(t)
		legacy := workspaceDeleteLegacyFixtureRow(fixture, ledger.purchase.ReceiptInput, "complete", "succeeded")
		if err := fixture.store.SaveRuntimeOperation(context.Background(), legacy); err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
			t.Fatal(err)
		}
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-v1-complete-replay")
		var payload map[string]any
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload["operationId"] != workspaceDeleteLegacyOperationID("ws-alpha") || payload["historical"] != true ||
			len(events.snapshot()) != 0 || len(fixture.fabric.recordedCalls()) != 0 || sub2API.keyDeletes != 0 || len(sub2API.historyReads) != 0 || len(sub2API.refunds) != 0 || len(ledger.receipts) != 0 {
			t.Fatalf("historical terminal status=%d payload=%#v events=%#v calls=%#v keyDeletes=%d history=%#v refunds=%d receipts=%d", response.Code, payload, events.snapshot(), fixture.fabric.recordedCalls(), sub2API.keyDeletes, sub2API.historyReads, len(sub2API.refunds), len(ledger.receipts))
		}
	})

	t.Run("active operation conflicts before mutation", func(t *testing.T) {
		fixture, sub2API, ledger, events := newWorkspaceDeleteEventFixture(t)
		legacy := workspaceDeleteLegacyFixtureRow(fixture, ledger.purchase.ReceiptInput, "storage_destroyed", "running")
		if err := fixture.store.SaveRuntimeOperation(context.Background(), legacy); err != nil {
			t.Fatal(err)
		}
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-v1-active-conflict")
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), errWorkspaceDeleteHistoricalConflict.Error()) ||
			len(events.snapshot()) != 0 || len(fixture.fabric.recordedCalls()) != 0 || sub2API.keyDeletes != 0 || len(sub2API.historyReads) != 0 || len(sub2API.refunds) != 0 || len(ledger.receipts) != 0 {
			t.Fatalf("historical active status=%d body=%s events=%#v calls=%#v keyDeletes=%d history=%#v refunds=%d receipts=%d", response.Code, response.Body.String(), events.snapshot(), fixture.fabric.recordedCalls(), sub2API.keyDeletes, sub2API.historyReads, len(sub2API.refunds), len(ledger.receipts))
		}
	})

	for _, testCase := range []struct {
		name   string
		mutate func(*workspaceDeleteLegacyOperation)
	}{
		{name: "request hash drift", mutate: func(operation *workspaceDeleteLegacyOperation) { operation.RequestHash = "drift" }},
		{name: "resource identity drift", mutate: func(operation *workspaceDeleteLegacyOperation) {
			operation.RuntimeID = "runtime-other"
			operation.RequestHash = workspaceDeleteLegacyRequestHash(*operation)
		}},
		{name: "terminal evidence missing", mutate: func(operation *workspaceDeleteLegacyOperation) { operation.RefundReceiptID = "" }},
		{name: "v2-only compute terminal", mutate: func(operation *workspaceDeleteLegacyOperation) { operation.ComputeStatus = "absent" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture, sub2API, ledger, events := newWorkspaceDeleteEventFixture(t)
			legacy := workspaceDeleteLegacyFixtureRow(fixture, ledger.purchase.ReceiptInput, "complete", "succeeded")
			var operation workspaceDeleteLegacyOperation
			if json.Unmarshal([]byte(stringValue(legacy["result"])), &operation) != nil {
				t.Fatal("legacy fixture decode failed")
			}
			testCase.mutate(&operation)
			legacy["result"] = string(mustJSON(operation))
			if err := fixture.store.SaveRuntimeOperation(context.Background(), legacy); err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
				t.Fatal(err)
			}
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-v1-corrupt-"+testCase.name)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), errWorkspaceDeleteHistoricalConflict.Error()) ||
				len(events.snapshot()) != 0 || len(fixture.fabric.recordedCalls()) != 0 || sub2API.keyDeletes != 0 || len(sub2API.historyReads) != 0 || len(sub2API.refunds) != 0 || len(ledger.receipts) != 0 {
				t.Fatalf("corrupt historical status=%d body=%s events=%#v calls=%#v", response.Code, response.Body.String(), events.snapshot(), fixture.fabric.recordedCalls())
			}
		})
	}
}

func workspaceDeleteLegacyFixtureRow(fixture workspaceDeleteFixture, purchaseReceipt clients.ReceiptInput, phase, status string) map[string]any {
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	operationID := workspaceDeleteLegacyOperationID("ws-alpha")
	operation := workspaceDeleteLegacyOperation{
		OperationID: operationID, AccountID: "acct-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceID: "ws-alpha",
		LaunchOperationID: "workspace-launch-alpha", RuntimeID: "runtime-alpha", ComputeID: "compute-alpha", StorageID: "storage-alpha", AttachmentID: "attachment-alpha",
		WorkspaceAPIKeyID: 19, GatewaySecretRef: "opl-gateway-ws-alpha", GatewayFingerprint: "sha256:" + strings.Repeat("a", 64),
		DebitCode: "opl:workspace-purchase-alpha", PurchaseReceiptID: "receipt-purchase-alpha", PurchaseReceipt: purchaseReceipt,
		RefundCode: monthlyRefundCode(monthlyEnvironment(), operationID), TotalUSDMicros: 52_580_000, Phase: phase, Status: status, CreatedAt: createdAt,
	}
	if phase == "complete" && status == "succeeded" {
		operation.RuntimeStatus, operation.SecretStatus, operation.AttachmentStatus = "absent", "absent", "detached"
		operation.StorageStatus, operation.ComputeStatus, operation.KeyStatus = "destroyed", "destroyed", "absent"
		operation.RefundReceiptID = "receipt-refund-alpha"
		operation.RefundConfirmation = map[string]any{"code": operation.RefundCode, "userId": int64(41), "refundUsdMicros": int64(52_580_000), "status": "used"}
	}
	operation.RequestHash = workspaceDeleteLegacyRequestHash(operation)
	result, _ := json.Marshal(operation)
	return map[string]any{
		"id": operation.OperationID, "operationId": operation.OperationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceId": operation.WorkspaceID, "resourceKind": "workspace", "action": workspaceDeleteLegacyAction, "status": status, "result": string(result),
		"computeAllocationId": operation.ComputeID, "storageId": operation.StorageID, "attachmentId": operation.AttachmentID, "runtimeId": operation.RuntimeID,
		"createdAt": createdAt,
	}
}

func TestWorkspaceDeleteCompletedOperationRejectsRecreatedWorkspaceWithoutMutation(t *testing.T) {
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
	seedCompletedWorkspaceDeleteOperation(t, fixture)
	operationID := workspaceDeleteOperationID("ws-alpha")
	terminalRow, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found {
		t.Fatalf("terminal operation found=%v err=%v", found, err)
	}
	terminalResult := stringValue(terminalRow["result"])
	if err := fixture.store.SaveWorkspace(context.Background(), cloneMap(fixture.workspace)); err != nil {
		t.Fatal(err)
	}
	fabricCalls, keyDeletes, refunds, receipts := len(fixture.fabric.recordedCalls()), sub2API.keyDeletes, len(sub2API.refunds), len(ledger.receipts)
	for attempt := 0; attempt < 2; attempt++ {
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-recreated-"+strconv.Itoa(attempt))
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), errWorkspaceDeleteTerminalConflict.Error()) {
			t.Fatalf("recreated Workspace attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if len(fixture.fabric.recordedCalls()) != fabricCalls || sub2API.keyDeletes != keyDeletes || len(sub2API.refunds) != refunds || len(ledger.receipts) != receipts {
		t.Fatalf("terminal conflict repeated mutation Fabric=%d/%d key=%d/%d refunds=%d/%d receipts=%d/%d", len(fixture.fabric.recordedCalls()), fabricCalls, sub2API.keyDeletes, keyDeletes, len(sub2API.refunds), refunds, len(ledger.receipts), receipts)
	}
	afterRow, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	operation, decodeErr := decodeWorkspaceDeleteOperation(afterRow)
	if err != nil || !found || decodeErr != nil || operation.Phase != "complete" || operation.Status != "succeeded" || stringValue(afterRow["result"]) != terminalResult {
		t.Fatalf("terminal operation changed operation=%#v found=%v err=%v decode=%v", operation, found, err, decodeErr)
	}
	audits, err := fixture.store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil {
		t.Fatal(err)
	}
	auditID := "audit-" + stableID("workspace.delete.terminal_conflict", operationID, errWorkspaceDeleteTerminalConflict.Error())[:12]
	matching := make([]map[string]any, 0, 1)
	for _, audit := range audits {
		if stringValue(audit["id"]) == auditID {
			matching = append(matching, audit)
		}
	}
	if len(matching) != 1 || stringValue(matching[0]["action"]) != "workspace.delete.terminal_conflict" || stringValue(matching[0]["targetAccountId"]) != "acct-alpha" ||
		stringValue(matching[0]["resourceKind"]) != "workspace" || stringValue(matching[0]["resourceId"]) != "ws-alpha" || stringValue(matching[0]["result"]) != "conflict" ||
		stringValue(mapField(matching[0], "after")["operationId"]) != operationID || stringValue(mapField(matching[0], "after")["error"]) != errWorkspaceDeleteTerminalConflict.Error() {
		t.Fatalf("terminal conflict audits=%#v", audits)
	}
}

func TestWorkspaceDeleteCompletedOperationRejectsInvalidTerminalEvidenceWithoutMutation(t *testing.T) {
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
	seedCompletedWorkspaceDeleteOperation(t, fixture)
	operationID := workspaceDeleteOperationID("ws-alpha")
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found {
		t.Fatalf("terminal operation found=%v err=%v", found, err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &result); err != nil {
		t.Fatal(err)
	}
	delete(result, "deletionReceiptId")
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	row["result"] = string(encoded)
	if err := fixture.store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	terminalResult := string(encoded)
	fabricCalls, keyDeletes, refunds, receipts := len(fixture.fabric.recordedCalls()), sub2API.keyDeletes, len(sub2API.refunds), len(ledger.receipts)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-terminal-evidence-drift")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), errWorkspaceDeleteTerminalConflict.Error()) ||
		len(fixture.fabric.recordedCalls()) != fabricCalls || sub2API.keyDeletes != keyDeletes || len(sub2API.refunds) != refunds || len(ledger.receipts) != receipts {
		t.Fatalf("terminal evidence conflict status=%d body=%s Fabric=%d/%d key=%d/%d refunds=%d/%d receipts=%d/%d", response.Code, response.Body.String(), len(fixture.fabric.recordedCalls()), fabricCalls, sub2API.keyDeletes, keyDeletes, len(sub2API.refunds), refunds, len(ledger.receipts), receipts)
	}
	afterRow, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found || stringValue(afterRow["result"]) != terminalResult || stringValue(afterRow["status"]) != "succeeded" {
		t.Fatalf("terminal evidence conflict changed operation=%#v found=%v err=%v", afterRow, found, err)
	}
	audits, err := fixture.store.ListAuditEvents(context.Background(), "acct-alpha")
	auditID := "audit-" + stableID("workspace.delete.terminal_conflict", operationID, errWorkspaceDeleteTerminalConflict.Error())[:12]
	if err != nil || !slices.ContainsFunc(audits, func(audit map[string]any) bool { return stringValue(audit["id"]) == auditID }) {
		t.Fatalf("terminal evidence conflict audit=%#v err=%v", audits, err)
	}
}

func TestWorkspaceDeleteTerminalConflictAuditFailureFailsClosed(t *testing.T) {
	store := &workspaceDeleteAuditFailStore{controlPlaneTableStore: newMemoryTableStore()}
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, store, &workspaceDeleteFabric{})
	seedCompletedWorkspaceDeleteOperation(t, fixture)
	operationID := workspaceDeleteOperationID("ws-alpha")
	terminalRow, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found {
		t.Fatalf("terminal operation found=%v err=%v", found, err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), cloneMap(fixture.workspace)); err != nil {
		t.Fatal(err)
	}
	store.failAudit = true
	fabricCalls, keyDeletes, refunds, receipts := len(fixture.fabric.recordedCalls()), sub2API.keyDeletes, len(sub2API.refunds), len(ledger.receipts)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-audit-failure")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "state_persist_failed") ||
		len(fixture.fabric.recordedCalls()) != fabricCalls || sub2API.keyDeletes != keyDeletes || len(sub2API.refunds) != refunds || len(ledger.receipts) != receipts {
		t.Fatalf("audit failure status=%d body=%s Fabric=%d/%d key=%d/%d refunds=%d/%d receipts=%d/%d", response.Code, response.Body.String(), len(fixture.fabric.recordedCalls()), fabricCalls, sub2API.keyDeletes, keyDeletes, len(sub2API.refunds), refunds, len(ledger.receipts), receipts)
	}
	afterRow, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found || stringValue(afterRow["result"]) != stringValue(terminalRow["result"]) || stringValue(afterRow["status"]) != "succeeded" {
		t.Fatalf("audit failure changed terminal operation=%#v found=%v err=%v", afterRow, found, err)
	}
}

func TestWorkspaceDeletePersistedIdentityDriftFailsClosed(t *testing.T) {
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
	first := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-identity-drift")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
	if err != nil || !found {
		t.Fatalf("operation found=%v err=%v", found, err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &result); err != nil {
		t.Fatal(err)
	}
	result["runtimeId"] = "runtime-drifted"
	encoded, _ := json.Marshal(result)
	row["result"] = string(encoded)
	if err := fixture.store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	fabricCalls, keyDeletes, refunds, receipts := len(fixture.fabric.recordedCalls()), sub2API.keyDeletes, len(sub2API.refunds), len(ledger.receipts)
	second := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-identity-drift-replay")
	if second.Code != http.StatusInternalServerError || len(fixture.fabric.recordedCalls()) != fabricCalls || sub2API.keyDeletes != keyDeletes || len(sub2API.refunds) != refunds || len(ledger.receipts) != receipts {
		t.Fatalf("drift status=%d body=%s Fabric=%d/%d key=%d/%d refunds=%d/%d receipts=%d/%d", second.Code, second.Body.String(), len(fixture.fabric.recordedCalls()), fabricCalls, sub2API.keyDeletes, keyDeletes, len(sub2API.refunds), refunds, len(ledger.receipts), receipts)
	}
}

func TestWorkspaceDeleteStoreLifecycleMemoryAndSQLite(t *testing.T) {
	for _, storeCase := range []struct {
		name string
		new  func(*testing.T) controlPlaneTableStore
	}{
		{name: "memory", new: func(*testing.T) controlPlaneTableStore { return newMemoryTableStore() }},
		{name: "sqlite", new: func(t *testing.T) controlPlaneTableStore {
			return NewTestEntStateStore(t, t.TempDir()+"/workspace-delete.sqlite")
		}},
	} {
		t.Run(storeCase.name, func(t *testing.T) {
			exerciseWorkspaceDeleteStoreLifecycle(t, storeCase.new(t))
		})
	}
}

func TestWorkspaceDeleteRenewalMutualExclusionMemoryAndSQLite(t *testing.T) {
	for _, storeCase := range []struct {
		name string
		new  func(*testing.T) controlPlaneTableStore
	}{
		{name: "memory", new: func(*testing.T) controlPlaneTableStore { return newMemoryTableStore() }},
		{name: "sqlite", new: func(t *testing.T) controlPlaneTableStore {
			return NewTestEntStateStore(t, t.TempDir()+"/workspace-delete-renewal.sqlite")
		}},
	} {
		t.Run(storeCase.name, func(t *testing.T) {
			exerciseWorkspaceDeleteRenewalMutualExclusion(t, storeCase.new(t))
		})
	}
}

func TestPostgresWorkspaceDeleteStoreLifecycle(t *testing.T) {
	exerciseWorkspaceDeleteStoreLifecycle(t, newPostgresWorkspaceRenewalStore(t))
}

func TestPostgresWorkspaceDeleteRenewalMutualExclusion(t *testing.T) {
	exerciseWorkspaceDeleteRenewalMutualExclusion(t, newPostgresWorkspaceRenewalStore(t))
}

func exerciseWorkspaceDeleteRenewalMutualExclusion(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	for _, renewalStatus := range []string{"claimed", "insufficient", "debit_pending", "debited", "provider_renewing", "verifying", "refund_pending", "manual_review"} {
		workspace := currentWorkspaceRenewalAPIRow()
		workspaceID := "workspace-delete-blocked-by-renewal-" + renewalStatus
		workspace["id"], workspace["accountId"], workspace["ownerAccountId"], workspace["ownerUserId"] = workspaceID, "acct-delete", "acct-delete", "usr-delete"
		workspace["autoRenew"], workspace["authorizedBy"], workspace["authorizedAt"] = true, "usr-delete", time.Now().UTC().Format(time.RFC3339Nano)
		workspace["workspaceApiKeyId"] = int64(19)
		if err := store.SaveWorkspace(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		renewal, err := newWorkspaceRenewalOperation(workspace, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		renewal.Status = renewalStatus
		if err := store.SaveRuntimeOperation(ctx, workspaceRenewalOperationRow(renewal)); err != nil {
			t.Fatal(err)
		}
		operation := workspaceDeleteStoreOperationForWorkspace(workspace, time.Now().UTC().Format(time.RFC3339Nano))
		if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(operation)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
			t.Fatalf("renewal %s did not block Delete claim: %v", renewalStatus, err)
		}
	}
	for _, renewalStatus := range []string{"active", "cancelled", "refunded", "expired_unpaid"} {
		workspace := currentWorkspaceRenewalAPIRow()
		workspaceID := "workspace-delete-after-terminal-renewal-" + renewalStatus
		workspace["id"], workspace["accountId"], workspace["ownerAccountId"], workspace["ownerUserId"] = workspaceID, "acct-delete", "acct-delete", "usr-delete"
		workspace["autoRenew"], workspace["authorizedBy"], workspace["authorizedAt"] = true, "usr-delete", time.Now().UTC().Format(time.RFC3339Nano)
		workspace["workspaceApiKeyId"] = int64(19)
		if err := store.SaveWorkspace(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		renewal, err := newWorkspaceRenewalOperation(workspace, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		renewal.Status = renewalStatus
		if renewalStatus == "refunded" || renewalStatus == "expired_unpaid" {
			renewal.Phase = "complete"
		}
		if err := store.SaveRuntimeOperation(ctx, workspaceRenewalOperationRow(renewal)); err != nil {
			t.Fatal(err)
		}
		operation := workspaceDeleteStoreOperationForWorkspace(workspace, time.Now().UTC().Format(time.RFC3339Nano))
		if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(operation)}); err != nil {
			t.Fatalf("terminal renewal %s blocked Delete claim: %v", renewalStatus, err)
		}
	}
	invalidWorkspace := currentWorkspaceRenewalAPIRow()
	invalidWorkspaceID := "workspace-delete-blocked-by-invalid-renewal"
	invalidWorkspace["id"], invalidWorkspace["accountId"], invalidWorkspace["ownerAccountId"], invalidWorkspace["ownerUserId"] = invalidWorkspaceID, "acct-delete", "acct-delete", "usr-delete"
	invalidWorkspace["autoRenew"], invalidWorkspace["authorizedBy"], invalidWorkspace["authorizedAt"] = true, "usr-delete", time.Now().UTC().Format(time.RFC3339Nano)
	invalidWorkspace["workspaceApiKeyId"] = int64(19)
	if err := store.SaveWorkspace(ctx, invalidWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, map[string]any{
		"id": "invalid-renewal-blocking-delete", "operationId": "invalid-renewal-blocking-delete", "accountId": "acct-delete", "workspaceId": invalidWorkspaceID,
		"resourceId": invalidWorkspaceID, "resourceKind": "workspace_renewal", "action": "workspace.renewal", "status": "active", "result": `{}`,
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	invalidDelete := workspaceDeleteStoreOperationForWorkspace(invalidWorkspace, time.Now().UTC().Format(time.RFC3339Nano))
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(invalidDelete)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("invalid Renewal did not fail closed before Delete claim: %v", err)
	}
}

func exerciseWorkspaceDeleteStoreLifecycle(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	workspace := map[string]any{
		"id": "ws-delete-store", "accountId": "acct-delete", "ownerAccountId": "acct-delete", "ownerUserId": "usr-delete",
		"currentComputeAllocationId": "compute-delete", "storageId": "storage-delete", "currentAttachmentId": "attachment-delete",
		"runtimeId": "runtime-delete", "workspaceApiKeyId": int64(19),
	}
	if err := store.SaveWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCompute(ctx, map[string]any{
		"id": "compute-delete", "accountId": "acct-delete", "ownerUserId": "usr-delete", "workspaceId": "ws-delete-store", "status": "destroyed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStorage(ctx, map[string]any{
		"id": "storage-delete", "accountId": "acct-delete", "ownerUserId": "usr-delete", "workspaceId": "ws-delete-store", "status": "destroyed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttachment(ctx, map[string]any{
		"id": "attachment-delete", "accountId": "acct-delete", "workspaceId": "ws-delete-store", "computeAllocationId": "compute-delete", "storageId": "storage-delete", "volumeId": "storage-delete", "status": "detached",
	}); err != nil {
		t.Fatal(err)
	}
	claimed := workspaceDeleteStoreOperation(now)
	clearWorkspaceDeleteKeyProjection(t, store, claimed.WorkspaceID)
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(claimed)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("invalid Workspace Key projection did not block Delete claim: %v", err)
	}
	if _, found, err := store.GetRuntimeOperation(ctx, claimed.OperationID); err != nil || found {
		t.Fatalf("failed Delete claim persisted operation found=%v err=%v", found, err)
	}
	workspace["workspaceApiKeyId"] = int64(19)
	if err := store.SaveWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(claimed)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(claimed)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("duplicate claim err=%v", err)
	}
	invalid := claimed
	invalid.Phase = "workspace_absent"
	expected := stringValue(workspaceDeleteOperationRow(claimed)["result"])
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{DeleteWorkspace: true, ExpectedResult: expected, DesiredOperation: workspaceDeleteOperationRow(invalid)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("phase jump without cumulative evidence err=%v", err)
	}
	advance := func(current, next workspaceDeleteOperation, deleteWorkspace, requireAbsent bool) workspaceDeleteOperation {
		t.Helper()
		if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
			DeleteWorkspace: deleteWorkspace, RequireWorkspaceAbsent: requireAbsent,
			ExpectedResult: stringValue(workspaceDeleteOperationRow(current)["result"]), DesiredOperation: workspaceDeleteOperationRow(next),
		}); err != nil {
			t.Fatal(err)
		}
		return next
	}
	runtimeAbsent := claimed
	runtimeAbsent.Phase, runtimeAbsent.RuntimeStatus, runtimeAbsent.SecretStatus = "runtime_secret_absent", "absent", "absent"
	current := advance(claimed, runtimeAbsent, false, false)
	attachmentAbsent := current
	attachmentAbsent.Phase, attachmentAbsent.AttachmentStatus = "attachment_absent", "absent"
	current = advance(current, attachmentAbsent, false, false)
	storageAbsent := current
	storageAbsent.Phase, storageAbsent.StorageStatus = "storage_absent", "absent"
	current = advance(current, storageAbsent, false, false)
	impossibleComputeSchedule := current
	impossibleComputeSchedule.ComputeStatus = "destroying"
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
		ExpectedResult: stringValue(workspaceDeleteOperationRow(current)["result"]), DesiredOperation: workspaceDeleteOperationRow(impossibleComputeSchedule),
	}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("destroying compute without readback schedule err=%v", err)
	}
	computeAbsent := current
	computeAbsent.Phase, computeAbsent.ComputeStatus = "compute_absent", "absent"
	computeAbsent.ComputeReadbacks, computeAbsent.MaxComputeReadbacks = 1, workspaceDeleteComputeReadbackBudget
	current = advance(current, computeAbsent, false, false)
	clearWorkspaceDeleteKeyProjection(t, store, claimed.WorkspaceID)
	deleted := current
	deleted.Phase = "workspace_absent"
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
		DeleteWorkspace: true, ExpectedResult: stringValue(workspaceDeleteOperationRow(current)["result"]), DesiredOperation: workspaceDeleteOperationRow(deleted),
	}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("invalid Workspace Key projection did not roll back atomic delete: %v", err)
	}
	if _, found, err := store.GetWorkspace(ctx, claimed.WorkspaceID); err != nil || !found {
		t.Fatalf("failed Key projection check removed Workspace found=%v err=%v", found, err)
	}
	if row, found, err := store.GetRuntimeOperation(ctx, claimed.OperationID); err != nil || !found || stringValue(row["result"]) != stringValue(workspaceDeleteOperationRow(current)["result"]) {
		t.Fatalf("failed Key projection check advanced cursor found=%v row=%#v err=%v", found, row, err)
	}
	workspace["workspaceApiKeyId"] = int64(19)
	if err := store.SaveWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCompute(ctx, map[string]any{
		"id": "compute-delete", "accountId": "acct-delete", "ownerUserId": "usr-delete", "workspaceId": "ws-other", "status": "destroyed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
		DeleteWorkspace: true, ExpectedResult: stringValue(workspaceDeleteOperationRow(current)["result"]), DesiredOperation: workspaceDeleteOperationRow(deleted),
	}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("mismatched resource projection did not block atomic delete: %v", err)
	}
	if _, found, err := store.GetWorkspace(ctx, claimed.WorkspaceID); err != nil || !found {
		t.Fatalf("failed projection delete removed Workspace found=%v err=%v", found, err)
	}
	if err := store.SaveCompute(ctx, map[string]any{
		"id": "compute-delete", "accountId": "acct-delete", "ownerUserId": "usr-delete", "workspaceId": "ws-delete-store", "status": "destroyed",
	}); err != nil {
		t.Fatal(err)
	}
	current = advance(current, deleted, true, false)
	if _, found, err := store.GetWorkspace(ctx, claimed.WorkspaceID); err != nil || found {
		t.Fatalf("Workspace after atomic delete found=%v err=%v", found, err)
	}
	if _, found, err := store.GetCompute(ctx, claimed.ComputeID); err != nil || found {
		t.Fatalf("Compute projection after atomic delete found=%v err=%v", found, err)
	}
	if _, found, err := store.GetStorage(ctx, claimed.StorageID); err != nil || found {
		t.Fatalf("Storage projection after atomic delete found=%v err=%v", found, err)
	}
	if _, found, err := store.GetAttachment(ctx, claimed.AttachmentID); err != nil || found {
		t.Fatalf("Attachment projection after atomic delete found=%v err=%v", found, err)
	}
	receiptRecorded := current
	receiptRecorded.Phase, receiptRecorded.DeletionReceiptID = "deletion_receipt_recorded", "receipt-delete-store"
	current = advance(current, receiptRecorded, false, true)
	complete := current
	complete.Phase, complete.Status = "complete", "succeeded"
	advance(current, complete, false, true)
	row, found, err := store.GetRuntimeOperation(ctx, complete.OperationID)
	if err != nil || !found || stringValue(row["status"]) != "succeeded" {
		t.Fatalf("terminal operation=%#v found=%v err=%v", row, found, err)
	}
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{RequireWorkspaceAbsent: true, ExpectedResult: "stale", DesiredOperation: workspaceDeleteOperationRow(complete)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("stale terminal update err=%v", err)
	}
	if err := store.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
		RequireWorkspaceAbsent: true, ExpectedResult: stringValue(workspaceDeleteOperationRow(complete)["result"]), DesiredOperation: workspaceDeleteOperationRow(complete),
	}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("terminal operation accepted a write: %v", err)
	}
}

func clearWorkspaceDeleteKeyProjection(t *testing.T, store controlPlaneTableStore, workspaceID string) {
	t.Helper()
	switch typed := store.(type) {
	case *memoryTableStore:
		typed.mu.Lock()
		defer typed.mu.Unlock()
		row := cloneMap(typed.workspaces[workspaceID])
		delete(row, "workspaceApiKeyId")
		typed.workspaces[workspaceID] = row
	case *postgresEntStateStore:
		if _, err := typed.execSQL(context.Background(), typed.db, `UPDATE control_plane_workspaces SET workspace_api_key_id = NULL, updated_at = ? WHERE id = ?`, time.Now().UTC(), workspaceID); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported Workspace Delete store %T", store)
	}
}

func workspaceDeleteStoreOperation(now string) workspaceDeleteOperation {
	return workspaceDeleteStoreOperationForWorkspace(map[string]any{"id": "ws-delete-store", "accountId": "acct-delete", "ownerUserId": "usr-delete"}, now)
}

func workspaceDeleteStoreOperationForWorkspace(workspace map[string]any, now string) workspaceDeleteOperation {
	workspaceID := stringValue(workspace["id"])
	workspaceAPIKeyID := int64(numberField(workspace, "workspaceApiKeyId", 19))
	operation := workspaceDeleteOperation{
		SchemaVersion: 2, OperationID: workspaceDeleteOperationID(workspaceID), AccountID: stringValue(workspace["accountId"]), OwnerUserID: stringValue(workspace["ownerUserId"]), Sub2APIUserID: 41,
		WorkspaceID: workspaceID, ResourceType: "workspace", ResourceID: workspaceID, LaunchOperationID: "workspace-launch-" + workspaceID, LaunchReceiptID: "receipt-launch-" + workspaceID,
		RuntimeID: "runtime-delete", RuntimeServiceName: "runtime-delete",
		ComputeID: "compute-delete", StorageID: "storage-delete", AttachmentID: "attachment-delete", WorkspaceAPIKeyID: workspaceAPIKeyID,
		GatewaySecretRef: "opl-gateway-ws-delete-store", GatewayFingerprint: "sha256:" + strings.Repeat("a", 64),
		Phase: "claimed", Status: "running", CreatedAt: now,
	}
	operation.RequestHash = workspaceDeleteRequestHash(operation)
	return operation
}
