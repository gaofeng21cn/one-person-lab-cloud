package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// WorkspaceDeleteReadbackSchemaVersion identifies the typed authoritative
// deletion readback that Fabric owns and Control Plane consumes.
const WorkspaceDeleteReadbackSchemaVersion = 1

// Deletion stages. A stage may only advance when its owner readback confirms it.
const (
	WorkspaceDeleteStageRuntimeAbsent    = "runtime_absent"
	WorkspaceDeleteStageAttachmentAbsent = "attachment_absent"
	WorkspaceDeleteStageStorageAbsent    = "storage_absent"
	WorkspaceDeleteStageComputeAbsent    = "compute_absent"
	WorkspaceDeleteStageWorkspaceAbsent  = "workspace_absent"
	WorkspaceDeleteStageReceiptRecorded  = "receipt_recorded"
)

// Customer-visible deletion page states. They describe progress only; they never
// carry a business conclusion the owning operation has not proven.
const (
	WorkspaceDeletePageStateWaiting   = "waiting"
	WorkspaceDeletePageStateRetrying  = "retrying"
	WorkspaceDeletePageStateBlocked   = "blocked"
	WorkspaceDeletePageStateCompleted = "completed"
)

// WorkspaceDeleteStageOrder returns the platform deletion order. The order is a
// cross-owner invariant: Fabric retires resources in it, Control Plane advances
// its operation in it, and Console renders progress in it. A caller that needs to
// know which stage is in progress walks this order against the owner-confirmed
// facts instead of interpreting Control Plane's internal phase encoding.
func WorkspaceDeleteStageOrder() []string {
	return []string{
		WorkspaceDeleteStageRuntimeAbsent,
		WorkspaceDeleteStageAttachmentAbsent,
		WorkspaceDeleteStageStorageAbsent,
		WorkspaceDeleteStageComputeAbsent,
		WorkspaceDeleteStageWorkspaceAbsent,
		WorkspaceDeleteStageReceiptRecorded,
	}
}

// Result classification for one deletion readback attempt.
const (
	WorkspaceDeleteResultWaiting           = "waiting"
	WorkspaceDeleteResultRetryableReadback = "retryable_readback"
	WorkspaceDeleteResultBlockedIdentity   = "blocked_identity"
	WorkspaceDeleteResultCompleted         = "completed"
	WorkspaceDeleteResultUnknown           = "unknown"
)

// Resource kinds whose absence the platform refund precondition observes. Every
// kind is reported by the provider that owns it; a provider that owns none of
// the underlying resources reports absent with an explicit observed status.
const (
	WorkspaceDeleteResourceRuntimeController     = "runtime_controller"
	WorkspaceDeleteResourceDeployment            = "deployment"
	WorkspaceDeleteResourceReplicaSet            = "replicaset"
	WorkspaceDeleteResourcePod                   = "pod"
	WorkspaceDeleteResourceService               = "service"
	WorkspaceDeleteResourceNetworkPolicy         = "network_policy"
	WorkspaceDeleteResourceEnvironmentSecret     = "environment_secret"
	WorkspaceDeleteResourceGatewaySecret         = "gateway_secret"
	WorkspaceDeleteResourceAttachment            = "tke_attachment"
	WorkspaceDeleteResourcePersistentVolumeClaim = "persistent_volume_claim"
	WorkspaceDeleteResourcePersistentVolume      = "persistent_volume"
	WorkspaceDeleteResourceCBS                   = "cbs"
	WorkspaceDeleteResourceMachine               = "tke_machine"
	WorkspaceDeleteResourceCVM                   = "cvm"
)

// Provider statuses that prove a resource is gone.
const (
	WorkspaceDeleteProviderStatusNotFound = "NOT_FOUND"
	WorkspaceDeleteProviderStatusAbsent   = "ABSENT"
)

// Platform refund policy. The monthly baseline and the billing precision are
// versioned constants of the platform billing policy, owned by Control Plane.
const (
	WorkspaceRefundPolicyVersion = "workspace-delete-refund-v1"
	WorkspaceRefundMonthlyHours  = 720
	WorkspaceRefundHourDuration  = time.Hour
)

// Refund gate reason codes. They are stable, machine-readable, and never a
// substitute for the owner readback they describe.
const (
	WorkspaceDeleteGateReadbackUnavailable = "provider_readback_unavailable"
	WorkspaceDeleteGateReadbackIdentity    = "provider_identity_mismatch"
	WorkspaceDeleteGateReadbackStale       = "provider_readback_stale"
	WorkspaceDeleteGateResourcePresent     = "provider_resource_present"
	WorkspaceDeleteGateProviderUnconfirmed = "provider_status_unconfirmed"
	WorkspaceDeleteGateReceiptMissing      = "workspace_delete_receipt_missing"
	WorkspaceDeleteGateOperationIncomplete = "workspace_delete_operation_incomplete"
	WorkspaceDeleteGateNotApplicable       = "workspace_delete_scope_invalid"
)

// WorkspaceDeleteIdentity binds every readback fact to the original Launch and
// Delete operation of exactly one Workspace.
type WorkspaceDeleteIdentity struct {
	DeleteOperationID         string
	LaunchOperationID         string
	AccountID                 string
	WorkspaceID               string
	RuntimeID                 string
	ComputeID                 string
	StorageID                 string
	AttachmentID              string
	StorageProviderResourceID string
	ComputeMachineName        string
	ComputeInstanceID         string
	ResourceFulfilledAt       string
	WorkspaceDeletedAt        string
}

// WorkspaceDeleteResourceFact is one provider-authoritative observation.
type WorkspaceDeleteResourceFact struct {
	Kind           string
	ResourceID     string
	Present        bool
	Observed       bool
	ProviderStatus string
	ObservedAt     string
	ReadbackID     string
	MutationCount  int
}

// WorkspaceDeleteReadback is the Fabric-owned authoritative deletion readback
// for one original Delete operation. A readback is only authoritative when it
// is fresh, identity-bound, and produced without provider mutation.
type WorkspaceDeleteReadback struct {
	SchemaVersion     int
	Provider          string
	DeleteOperationID string
	LaunchOperationID string
	AccountID         string
	WorkspaceID       string
	ObservedAt        string
	ReadbackID        string
	Result            string
	ReasonCode        string
	MutationCount     int
	Facts             []WorkspaceDeleteResourceFact
}

// WorkspaceDeleteRequiredResourceKinds returns the complete set of facts the
// platform refund precondition requires, in stable order.
func WorkspaceDeleteRequiredResourceKinds() []string {
	return []string{
		WorkspaceDeleteResourceRuntimeController,
		WorkspaceDeleteResourceDeployment,
		WorkspaceDeleteResourceReplicaSet,
		WorkspaceDeleteResourcePod,
		WorkspaceDeleteResourceService,
		WorkspaceDeleteResourceNetworkPolicy,
		WorkspaceDeleteResourceEnvironmentSecret,
		WorkspaceDeleteResourceGatewaySecret,
		WorkspaceDeleteResourceAttachment,
		WorkspaceDeleteResourcePersistentVolumeClaim,
		WorkspaceDeleteResourcePersistentVolume,
		WorkspaceDeleteResourceCBS,
		WorkspaceDeleteResourceMachine,
		WorkspaceDeleteResourceCVM,
	}
}

// Fact returns the single observation recorded for a resource kind.
func (r WorkspaceDeleteReadback) Fact(kind string) (WorkspaceDeleteResourceFact, bool) {
	for _, fact := range r.Facts {
		if fact.Kind == kind {
			return fact, true
		}
	}
	return WorkspaceDeleteResourceFact{}, false
}

// ExpectedResourceID binds a resource kind to the identity recorded by the
// original Launch/Delete operation.
func (i WorkspaceDeleteIdentity) ExpectedResourceID(kind string) (string, bool) {
	switch kind {
	case WorkspaceDeleteResourceRuntimeController, WorkspaceDeleteResourceDeployment, WorkspaceDeleteResourceReplicaSet, WorkspaceDeleteResourcePod,
		WorkspaceDeleteResourceService, WorkspaceDeleteResourceNetworkPolicy, WorkspaceDeleteResourceEnvironmentSecret,
		WorkspaceDeleteResourceGatewaySecret:
		// The Runtime readback is scoped to the Workspace. A purchase that created no
		// Runtime controller — a resource-only purchase, where no application was ever
		// deployed — is therefore bound to its Workspace identity rather than refused
		// for lacking a Runtime it correctly does not own.
		return firstNonEmptyContractString(i.RuntimeID, i.WorkspaceID), i.RuntimeID != "" || i.WorkspaceID != ""
	case WorkspaceDeleteResourceAttachment:
		return i.AttachmentID, i.AttachmentID != ""
	case WorkspaceDeleteResourcePersistentVolumeClaim, WorkspaceDeleteResourcePersistentVolume:
		return i.StorageProviderResourceID, i.StorageProviderResourceID != ""
	case WorkspaceDeleteResourceCBS:
		return i.StorageProviderResourceID, i.StorageProviderResourceID != ""
	case WorkspaceDeleteResourceMachine:
		return firstNonEmptyContractString(i.ComputeMachineName, i.ComputeID), i.ComputeMachineName != "" || i.ComputeID != ""
	case WorkspaceDeleteResourceCVM:
		return firstNonEmptyContractString(i.ComputeInstanceID, i.ComputeID), i.ComputeInstanceID != "" || i.ComputeID != ""
	default:
		return "", false
	}
}

// Deletion outcome classifications. The physical owner (Fabric) writes exactly
// one of these on a provider result when a deletion it owns is unfinished but
// not failed, and Control Plane's delete state machine reads it to decide between
// retrying the same operation and recording a terminal result.
//
// They are not provider statuses and not deletion stages: they are the stable
// answer to "may the owning operation try again, and will that attempt mutate
// provider resources?" An empty classification means the deletion either
// completed or failed unverifiably, and no caller may treat it as retryable.
const (
	// WorkspaceDeleteOutcomePendingRetry: the deletion is unfinished and the
	// provider owner has not dispatched a mutation for this attempt. Retrying is
	// expected to make progress, and the owner may still dispatch once its
	// preconditions hold.
	WorkspaceDeleteOutcomePendingRetry = "pending_retry"
	// WorkspaceDeleteOutcomeUnconfirmedSend: the deletion is unfinished and a
	// provider mutation may already have been dispatched. Retrying is read-only
	// evidence reconciliation; the owner must not dispatch again.
	WorkspaceDeleteOutcomeUnconfirmedSend = "unconfirmed_send"
)

// WorkspaceDeleteOutcomeRetryable reports whether this classification keeps the
// owning deletion operation open instead of terminal.
func WorkspaceDeleteOutcomeRetryable(outcome string) bool {
	return outcome == WorkspaceDeleteOutcomePendingRetry || outcome == WorkspaceDeleteOutcomeUnconfirmedSend
}

// Evidence kinds. Each stage confirmation records who actually observed the fact,
// because a local state transition must never be presented as a provider readback.
const (
	// WorkspaceDeleteEvidenceProviderReadback is a real provider readback.
	WorkspaceDeleteEvidenceProviderReadback = "provider_readback"
	// WorkspaceDeleteEvidenceLocalTransition is a committed local state change, such
	// as releasing the mount binding or removing the Workspace projection.
	WorkspaceDeleteEvidenceLocalTransition = "local_transition"
	// WorkspaceDeleteEvidenceLedgerReceipt is the deletion receipt itself.
	WorkspaceDeleteEvidenceLedgerReceipt = "ledger_receipt"
)

// Stage confirmation results. A confirmed result names what the owning operation
// proved about that stage and is the only result a receipt may attest.
const (
	WorkspaceDeleteEvidenceAbsent   = "absent"
	WorkspaceDeleteEvidenceReleased = "released"
	WorkspaceDeleteEvidenceRemoved  = "removed"
	WorkspaceDeleteEvidenceRecorded = "recorded"
)

// Non-confirming stage results. A stage that is still waiting or that failed is
// recorded too, so a stalled deletion can be explained from persisted state
// instead of only from a transient log. These never satisfy a receipt.
const (
	WorkspaceDeleteEvidenceWaiting = "waiting"
	WorkspaceDeleteEvidenceFailed  = "failed"
)

// WorkspaceDeleteEvidenceUnavailable states that the owning operation tried to
// observe a stage and could not obtain a provider readback. It is the honest
// alternative to inventing an observation: the entry names no readback because none
// was returned, it is never a confirmation, and it never appears in a receipt. Its
// ObservedAt is the time of the attempt, which the entry's own kind identifies as an
// attempt rather than a provider observation.
const WorkspaceDeleteEvidenceUnavailable = "unavailable"

// WorkspaceDeleteStageEvidence records the latest observation of one stage of an
// original Delete operation. It binds the result to the exact resource identity,
// the time the observing owner observed it, and the readback record it came from.
//
// ObservedAt is when the observing owner observed the fact — or, for an
// WorkspaceDeleteEvidenceUnavailable entry, when it attempted a read that returned
// nothing. It is never a local save time presented as a provider observation.
// ReadAttempts and MutationAttempts are separate: a query can never stand in for a
// destroy. There is at most one entry per stage; a confirmed entry is immutable,
// while a waiting, failed, or unavailable entry is replaced by the next observation
// of that same stage.
type WorkspaceDeleteStageEvidence struct {
	Stage              string `json:"stage"`
	Result             string `json:"result"`
	ReasonCode         string `json:"reasonCode,omitempty"`
	EvidenceKind       string `json:"evidenceKind"`
	ResourceID         string `json:"resourceId"`
	ProviderResourceID string `json:"providerResourceId,omitempty"`
	ObservedAt         string `json:"observedAt"`
	ReadbackID         string `json:"readbackId,omitempty"`
	ReadAttempts       int    `json:"readAttempts,omitempty"`
	MutationAttempts   int    `json:"mutationAttempts,omitempty"`
}

// Confirmed reports whether this entry proves its stage finished. Only a confirmed
// entry can appear in a deletion receipt.
func (e WorkspaceDeleteStageEvidence) Confirmed() bool {
	result, _, ok := WorkspaceDeleteStageEvidenceExpected(e.Stage)
	return ok && e.Result == result
}

// WorkspaceDeleteStageEvidenceExpected reports the result and evidence kind the
// frozen deletion contract requires for one stage. It is the single owner of that
// mapping, so Control Plane, Ledger and Console cannot disagree about it.
func WorkspaceDeleteStageEvidenceExpected(stage string) (result string, kind string, ok bool) {
	switch stage {
	case WorkspaceDeleteStageRuntimeAbsent:
		// The Runtime and its controller tree are read back from the provider.
		return WorkspaceDeleteEvidenceAbsent, WorkspaceDeleteEvidenceProviderReadback, true
	case WorkspaceDeleteStageAttachmentAbsent:
		// Releasing the mount binding is a local transition. The physical CBS
		// detach is proven later by the storage stage, not claimed here.
		return WorkspaceDeleteEvidenceReleased, WorkspaceDeleteEvidenceLocalTransition, true
	case WorkspaceDeleteStageStorageAbsent:
		return WorkspaceDeleteEvidenceAbsent, WorkspaceDeleteEvidenceProviderReadback, true
	case WorkspaceDeleteStageComputeAbsent:
		return WorkspaceDeleteEvidenceAbsent, WorkspaceDeleteEvidenceProviderReadback, true
	case WorkspaceDeleteStageWorkspaceAbsent:
		// Removing the Workspace projection is a committed local transaction fact.
		return WorkspaceDeleteEvidenceRemoved, WorkspaceDeleteEvidenceLocalTransition, true
	case WorkspaceDeleteStageReceiptRecorded:
		return WorkspaceDeleteEvidenceRecorded, WorkspaceDeleteEvidenceLedgerReceipt, true
	default:
		return "", "", false
	}
}

// ValidWorkspaceDeleteStageEvidence checks one stage observation against the frozen
// contract. It rejects a result that does not belong to the stage, a mismatched
// evidence kind, a missing resource identity, an unparseable observation time, and
// a provider or ledger observation that does not name the readback it came from —
// so a fabricated, mislabelled, or unattributable observation can never enter the
// record or the receipt.
func ValidWorkspaceDeleteStageEvidence(evidence WorkspaceDeleteStageEvidence) bool {
	result, kind, ok := WorkspaceDeleteStageEvidenceExpected(evidence.Stage)
	if !ok {
		return false
	}
	switch evidence.Result {
	case result, WorkspaceDeleteEvidenceWaiting, WorkspaceDeleteEvidenceFailed:
	default:
		return false
	}
	if strings.TrimSpace(evidence.ResourceID) == "" || evidence.ReadAttempts < 0 || evidence.MutationAttempts < 0 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt); err != nil {
		return false
	}
	if evidence.ReasonCode != strings.TrimSpace(evidence.ReasonCode) {
		return false
	}
	switch evidence.EvidenceKind {
	case kind:
		// A provider or ledger observation must name the readback it came from:
		// without it the fact cannot be traced back to what was actually observed. A
		// local state transition has no readback to name.
		return kind == WorkspaceDeleteEvidenceLocalTransition || strings.TrimSpace(evidence.ReadbackID) != ""
	case WorkspaceDeleteEvidenceUnavailable:
		// An unavailable observation may only be an unfinished stage, and it names no
		// readback because none was returned. It can never carry the stage's
		// confirmation result, so it cannot be mistaken for an observation.
		return evidence.Result != result && strings.TrimSpace(evidence.ReadbackID) == ""
	default:
		return false
	}
}

// WorkspaceDeleteStageEvidenceComplete reports whether an evidence list confirms
// exactly the frozen deletion sequence: every stage present once, in order, each
// valid and confirmed. The final deletion receipt and the refund gate both rely on
// this, so a stage that is still waiting or that failed can never satisfy it.
func WorkspaceDeleteStageEvidenceComplete(evidence []WorkspaceDeleteStageEvidence) bool {
	order := WorkspaceDeleteStageOrder()
	if len(evidence) != len(order) {
		return false
	}
	for index, stage := range order {
		if evidence[index].Stage != stage || !ValidWorkspaceDeleteStageEvidence(evidence[index]) || !evidence[index].Confirmed() {
			return false
		}
	}
	return true
}

// WorkspaceDeleteStageEvidenceLatest returns the recorded observation of one stage.
func WorkspaceDeleteStageEvidenceLatest(evidence []WorkspaceDeleteStageEvidence, stage string) (WorkspaceDeleteStageEvidence, bool) {
	for _, entry := range evidence {
		if entry.Stage == stage {
			return entry, true
		}
	}
	return WorkspaceDeleteStageEvidence{}, false
}

// WorkspaceDeleteReceiptEvidenceComplete reports whether an evidence list contains
// every stage the deletion receipt can attest, in order and valid. The receipt is
// written by the receipt stage, so it carries the confirmations that preceded it;
// the receipt's own stage is recorded afterwards against the written receipt.
func WorkspaceDeleteReceiptEvidenceComplete(evidence []WorkspaceDeleteStageEvidence) bool {
	order := WorkspaceDeleteStageOrder()
	if len(evidence) != len(order)-1 {
		return false
	}
	for index := range evidence {
		if evidence[index].Stage != order[index] || !ValidWorkspaceDeleteStageEvidence(evidence[index]) || !evidence[index].Confirmed() {
			return false
		}
	}
	return true
}

// WorkspaceDeleteStageEvidenceDigest is the bounded, redacted stage confirmation
// embedded in the deletion receipt and exposed to its owner. It carries the
// confirmation, the time the observing owner observed it, and an opaque reference
// that binds the digest to the exact resource identity and readback it came from.
//
// The reference is a one-way digest, so the receipt stays identity-sensitive — a
// different resource or a different readback can never produce the same receipt —
// while private provider and readback identifiers never leave their owner.
type WorkspaceDeleteStageEvidenceDigest struct {
	Stage        string `json:"stage"`
	Result       string `json:"result"`
	EvidenceKind string `json:"evidenceKind"`
	ObservedAt   string `json:"observedAt"`
	ReasonCode   string `json:"reasonCode,omitempty"`
	EvidenceRef  string `json:"evidenceRef"`
}

// WorkspaceDeleteStageEvidenceReference derives the opaque reference that binds one
// observation to the resource identity and readback record it came from.
func WorkspaceDeleteStageEvidenceReference(evidence WorkspaceDeleteStageEvidence) string {
	return "deletion-evidence:" + workspaceDeleteEvidenceDigest(evidence.Stage, evidence.ResourceID, evidence.ProviderResourceID, evidence.ReadbackID, evidence.ObservedAt)
}

// WorkspaceDeleteStageEvidenceDigests projects recorded observations onto the digest
// that the receipt and its owner receive.
func WorkspaceDeleteStageEvidenceDigests(evidence []WorkspaceDeleteStageEvidence) []WorkspaceDeleteStageEvidenceDigest {
	digests := make([]WorkspaceDeleteStageEvidenceDigest, 0, len(evidence))
	for _, stage := range evidence {
		digests = append(digests, WorkspaceDeleteStageEvidenceDigest{
			Stage: stage.Stage, Result: stage.Result, EvidenceKind: stage.EvidenceKind,
			ObservedAt: stage.ObservedAt, ReasonCode: stage.ReasonCode,
			EvidenceRef: WorkspaceDeleteStageEvidenceReference(stage),
		})
	}
	return digests
}

func workspaceDeleteEvidenceDigest(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

// WorkspaceDeleteStageEvidenceDigestList parses the digest list that a receipt or a
// response carries. It exists because the same value arrives either as an
// in-process Go slice or as JSON-decoded data, and shape guessing would let a
// malformed summary through.
func WorkspaceDeleteStageEvidenceDigestList(value any) ([]WorkspaceDeleteStageEvidenceDigest, bool) {
	if value == nil {
		return nil, false
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var digests []WorkspaceDeleteStageEvidenceDigest
	if err := decoder.Decode(&digests); err != nil {
		return nil, false
	}
	return digests, true
}

// ValidWorkspaceDeleteStageEvidenceDigests validates a digest list against the
// frozen contract: every stage the receipt can attest, once, in order, with the
// result and evidence kind that stage requires and a real observation time.
func ValidWorkspaceDeleteStageEvidenceDigests(digests []WorkspaceDeleteStageEvidenceDigest) bool {
	order := WorkspaceDeleteStageOrder()
	if len(order) == 0 || len(digests) != len(order)-1 {
		return false
	}
	for index, digest := range digests {
		result, kind, ok := WorkspaceDeleteStageEvidenceExpected(order[index])
		if !ok || digest.Stage != order[index] || digest.Result != result || digest.EvidenceKind != kind ||
			digest.ReasonCode != strings.TrimSpace(digest.ReasonCode) || strings.TrimSpace(digest.EvidenceRef) == "" {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, digest.ObservedAt); err != nil {
			return false
		}
	}
	return true
}

// WorkspaceDeleteRefundGateInput is everything the refund precondition may use.
type WorkspaceDeleteRefundGateInput struct {
	Identity              WorkspaceDeleteIdentity
	Readback              WorkspaceDeleteReadback
	DeleteReceiptRecorded bool
	Now                   time.Time
	MaxReadbackAge        time.Duration
}

// PlatformRefundDispatchAllowed is the single authoritative precondition for
// creating and dispatching a platform refund for one deleted Workspace. It
// returns the decision and, when refused, one stable reason code.
//
// It is intentionally fail-closed: any missing, unknown, stale, or
// identity-mismatched fact refuses the refund, and no weaker signal (SHUTDOWN,
// suspended, autoRenew=false, data_deleted, a single absent resource, an
// unavailable readback) can substitute for a complete authoritative readback.
// Workspace lifecycle state is deliberately not an input: a refund is decided
// only from provider facts about the resources this Delete operation destroyed.
// The requested deletion operation must also be complete; a pending,
// manual_review or unknown deletion can never satisfy this gate.
func PlatformRefundDispatchAllowed(input WorkspaceDeleteRefundGateInput) (bool, string) {
	identity, readback := input.Identity, input.Readback
	if identity.WorkspaceID == "" || identity.DeleteOperationID == "" || identity.AccountID == "" ||
		identity.LaunchOperationID == "" || identity.StorageProviderResourceID == "" {
		return false, WorkspaceDeleteGateNotApplicable
	}
	if !input.DeleteReceiptRecorded {
		return false, WorkspaceDeleteGateReceiptMissing
	}
	if _, err := time.Parse(time.RFC3339Nano, identity.WorkspaceDeletedAt); err != nil {
		return false, WorkspaceDeleteGateOperationIncomplete
	}
	if readback.SchemaVersion != WorkspaceDeleteReadbackSchemaVersion || readback.MutationCount != 0 ||
		readback.Result != WorkspaceDeleteResultCompleted || strings.TrimSpace(readback.ReadbackID) == "" ||
		readback.Provider == "" {
		return false, WorkspaceDeleteGateReadbackUnavailable
	}
	if readback.DeleteOperationID != identity.DeleteOperationID || readback.WorkspaceID != identity.WorkspaceID ||
		readback.AccountID != identity.AccountID || readback.LaunchOperationID != identity.LaunchOperationID {
		return false, WorkspaceDeleteGateReadbackIdentity
	}
	observedAt, err := time.Parse(time.RFC3339Nano, readback.ObservedAt)
	if err != nil {
		return false, WorkspaceDeleteGateReadbackUnavailable
	}
	deletedAt, err := time.Parse(time.RFC3339Nano, identity.WorkspaceDeletedAt)
	if err != nil {
		return false, WorkspaceDeleteGateOperationIncomplete
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if observedAt.Before(deletedAt) {
		// A readback taken before this deletion cannot prove this deletion.
		return false, WorkspaceDeleteGateReadbackStale
	}
	if input.MaxReadbackAge > 0 && now.Sub(observedAt) > input.MaxReadbackAge {
		return false, WorkspaceDeleteGateReadbackStale
	}
	if observedAt.After(now) {
		return false, WorkspaceDeleteGateReadbackStale
	}
	if len(readback.Facts) != len(WorkspaceDeleteRequiredResourceKinds()) {
		return false, WorkspaceDeleteGateReadbackUnavailable
	}
	for _, kind := range WorkspaceDeleteRequiredResourceKinds() {
		fact, ok := readback.Fact(kind)
		if !ok || !fact.Observed || strings.TrimSpace(fact.ObservedAt) == "" || fact.ReadbackID != readback.ReadbackID || fact.MutationCount != 0 {
			return false, WorkspaceDeleteGateReadbackUnavailable
		}
		if fact.ObservedAt != readback.ObservedAt {
			return false, WorkspaceDeleteGateReadbackStale
		}
		expected, ok := identity.ExpectedResourceID(kind)
		if !ok || expected == "" || fact.ResourceID != expected {
			return false, WorkspaceDeleteGateReadbackIdentity
		}
		if fact.Present {
			return false, WorkspaceDeleteGateResourcePresent
		}
		if !WorkspaceDeleteFactProvesAbsence(fact) {
			return false, WorkspaceDeleteGateProviderUnconfirmed
		}
	}
	return true, ""
}

// WorkspaceDeleteFactProvesAbsence reports whether one readback fact is an
// observed, non-present resource with an explicit provider status.
func WorkspaceDeleteFactProvesAbsence(fact WorkspaceDeleteResourceFact) bool {
	return fact.Observed && !fact.Present && workspaceDeleteProviderStatusProvesAbsence(fact.ProviderStatus)
}

// workspaceDeleteProviderStatusProvesAbsence accepts only explicit provider
// statuses that mean "read successfully, resource does not exist".
func workspaceDeleteProviderStatusProvesAbsence(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case WorkspaceDeleteProviderStatusNotFound, WorkspaceDeleteProviderStatusAbsent:
		return true
	default:
		return false
	}
}

// ClassifyWorkspaceDeleteReadback maps one provider readback onto the stable
// result classification. Identity conflicts are never downgraded to a wait.
func ClassifyWorkspaceDeleteReadback(identity WorkspaceDeleteIdentity, readback WorkspaceDeleteReadback, now time.Time, maxAge time.Duration) (string, string) {
	if readback.SchemaVersion != WorkspaceDeleteReadbackSchemaVersion || readback.ReadbackID == "" || readback.Provider == "" {
		return WorkspaceDeleteResultUnknown, WorkspaceDeleteGateReadbackUnavailable
	}
	if readback.DeleteOperationID != identity.DeleteOperationID || readback.WorkspaceID != identity.WorkspaceID || readback.AccountID != identity.AccountID {
		return WorkspaceDeleteResultBlockedIdentity, WorkspaceDeleteGateReadbackIdentity
	}
	observedAt, err := time.Parse(time.RFC3339Nano, readback.ObservedAt)
	if err != nil {
		return WorkspaceDeleteResultUnknown, WorkspaceDeleteGateReadbackUnavailable
	}
	if maxAge > 0 && now.Sub(observedAt) > maxAge {
		return WorkspaceDeleteResultUnknown, WorkspaceDeleteGateReadbackStale
	}
	// Facts decide. A provider result value can never upgrade an unobserved or
	// still-present resource into a completed deletion.
	for _, fact := range readback.Facts {
		if fact.Present {
			return WorkspaceDeleteResultWaiting, WorkspaceDeleteGateResourcePresent
		}
		if !fact.Observed {
			return WorkspaceDeleteResultRetryableReadback, WorkspaceDeleteGateReadbackUnavailable
		}
	}
	if readback.Result == WorkspaceDeleteResultCompleted && readback.ReasonCode == "" && len(readback.Facts) == len(WorkspaceDeleteRequiredResourceKinds()) {
		return WorkspaceDeleteResultCompleted, ""
	}
	if readback.Result == WorkspaceDeleteResultBlockedIdentity {
		return WorkspaceDeleteResultBlockedIdentity, readback.ReasonCode
	}
	return WorkspaceDeleteResultRetryableReadback, WorkspaceDeleteGateReadbackUnavailable
}

// PlatformWorkspaceDeleteRefundMicros returns the platform refund for one
// deleted Workspace in USD micros. Billing precision is one hour, the monthly
// baseline is WorkspaceRefundMonthlyHours, and the refund never exceeds the
// original platform charge.
func PlatformWorkspaceDeleteRefundMicros(originalChargeUSDMicros int64, resourceFulfilledAt, workspaceDeletedAt time.Time) (int64, error) {
	if originalChargeUSDMicros <= 0 {
		return 0, fmt.Errorf("workspace_refund_original_charge_required")
	}
	if resourceFulfilledAt.IsZero() || workspaceDeletedAt.IsZero() || !workspaceDeletedAt.After(resourceFulfilledAt) {
		return 0, fmt.Errorf("workspace_refund_period_invalid")
	}
	used := workspaceDeletedAt.Sub(resourceFulfilledAt)
	usedHours := int64(used / WorkspaceRefundHourDuration)
	if used%WorkspaceRefundHourDuration != 0 {
		usedHours++
	}
	refundHours := int64(WorkspaceRefundMonthlyHours) - usedHours
	if refundHours <= 0 {
		return 0, nil
	}
	if refundHours > int64(WorkspaceRefundMonthlyHours) {
		refundHours = int64(WorkspaceRefundMonthlyHours)
	}
	refund := originalChargeUSDMicros * refundHours / int64(WorkspaceRefundMonthlyHours)
	if refund < 0 {
		return 0, fmt.Errorf("workspace_refund_amount_invalid")
	}
	return refund, nil
}

// WorkspaceDeleteRefundIdempotencyKey binds one refund to the original Delete
// operation and the platform refund policy version.
func WorkspaceDeleteRefundIdempotencyKey(deleteOperationID, policyVersion string) string {
	return strings.TrimSpace(deleteOperationID) + ":refund:" + strings.TrimSpace(policyVersion)
}

// SortedWorkspaceDeleteResourceKinds is a deterministic view used by evidence.
func SortedWorkspaceDeleteResourceKinds(kinds []string) []string {
	ordered := append([]string(nil), kinds...)
	sort.Strings(ordered)
	return ordered
}

func firstNonEmptyContractString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
