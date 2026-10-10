package coordination

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/internal/fabric"
)

// Provider names are the one provider identity each dispatcher may mutate.
const (
	providerLocalDocker = "local-docker"
	providerTencentTKE  = "tencent-tke"
	// Billing modes are the only approved provider purchase shapes.
	billingLocalNoCharge  = "LOCAL_NO_CHARGE"
	billingPrepaidMonthly = "PREPAID_MONTHLY"
)

// errResourceDispatchPending means the provider accepted the original request
// but has not confirmed it yet. It is never an absence, and the next attempt
// replays the same persisted operation, resource and provider keys instead of
// minting a second purchase.
var errResourceDispatchPending = errors.New("resource_dispatch_pending")

// ResourceIntent is one committed Fabric resource intent. Its IDs were allocated
// in the same transaction as the original operation, so every retry after a lost
// response observes the same resources and keys.
type ResourceIntent struct {
	TenantID      string
	WorkspaceID   string
	OperationID   string
	ResourceSetID string
	ComputeID     string
	StorageID     string
	Plan          *api.ResourcePlanSnapshot
}

// ResourceResult is a provider-confirmed resource readback. It exists only after
// the provider authority returned the exact resources this intent requested.
type ResourceResult struct {
	Binding    *api.ResourceExecutionBinding `json:"binding"`
	Compute    fabric.ComputeAllocation      `json:"compute"`
	Storage    fabric.StorageVolume          `json:"storage"`
	Attachment fabric.StorageAttachment      `json:"attachment"`
	Network    *NetworkFact                  `json:"network,omitempty"`
	ObservedAt time.Time                     `json:"observedAt"`
}

// NetworkFact is the provider-authoritative network placement of the compute
// resource: the concrete provider network the compute joined plus its zone and
// region. It is a read projection of the same provider mutation, not a second
// resource authority.
type NetworkFact struct {
	ProviderReference string `json:"providerReference"`
	Zone              string `json:"zone,omitempty"`
	Region            string `json:"region,omitempty"`
}

// ResourceDispatcher executes one Fabric resource intent against the process's
// single provider adapter and returns only authoritative readback. A dispatcher
// names the one provider it may mutate; the coordination service never routes an
// intent to another provider or adds a second resource authority.
type ResourceDispatcher interface {
	Provider() string
	EnsureResources(context.Context, ResourceIntent) (*ResourceResult, error)
	// BindSecret asks the provider to confirm the exact approved-store Secret for
	// one runtime and return its observed identity. It never writes a credential.
	BindSecret(context.Context, SecretBindIntent) (SecretBindResult, error)
	// DeleteResource releases exactly one kind of one accepted resource set and
	// returns the owning surface's observation. It is idempotent for the same
	// intent and kind: a repeated call replays the adapter's durable dispatch
	// claim and reconciles by readback, so no second provider terminate or delete
	// is dispatched for a handle whose mutation may already have been sent.
	DeleteResource(context.Context, ResourceDeletionIntent, string) (ResourceDeletionFact, error)
}

// SecretBindIntent is one committed request to confirm an approved-store Secret
// for a runtime. It carries only opaque identities, keyed by the Workspace and
// runtime instance so a retry names the same credential.
type SecretBindIntent struct {
	TenantID          string
	WorkspaceID       string
	RuntimeInstanceID string
	SecretRef         string
	Fingerprint       string
	TargetSlot        string
}

// SecretBindResult is the provider-confirmed Secret identity: the reference, its
// exact version and fingerprint, as read back from the approved store.
type SecretBindResult struct {
	SecretRef   string
	Version     string
	Fingerprint string
}

// Deletion kinds name the exact per-kind handles one DeleteResources call
// releases. The first three are released through their owning Fabric surfaces
// (runtime, Secret binding, mount binding); storage and compute keep the resource
// row vocabulary the accepted resource set already uses.
const (
	DeletionKindRuntime    = "workspace_runtime"
	DeletionKindSecret     = "gateway_secret_binding"
	DeletionKindAttachment = "storage_attachment"
	DeletionKindStorage    = "storage"
	DeletionKindCompute    = "compute"
)

// Deletion states are the only facts a deletion observation may report. A
// pending or unknown handle is never reported as absent, and absence exists only
// as the owning surface's own readback.
const (
	DeletionStateAbsent  = "absent"
	DeletionStatePending = "pending"
	DeletionStateUnknown = "unknown"
)

// deleteResourceKinds returns the release order of one resource set: the
// injected Secret binding and the mount binding are released before the storage
// and compute handles they were built on.
func deleteResourceKinds() []string {
	return []string{DeletionKindRuntime, DeletionKindSecret, DeletionKindAttachment, DeletionKindStorage, DeletionKindCompute}
}

// deletionStage maps one deletion kind onto the contract's Operation stage
// vocabulary, so a Workspace polling the operation can tell exactly which handle
// is still being released.
func deletionStage(kind string) string {
	switch kind {
	case DeletionKindRuntime:
		return "runtime_deletion"
	case DeletionKindSecret:
		return "secret_unbinding"
	case DeletionKindAttachment:
		return "attachment_deletion"
	case DeletionKindStorage:
		return "storage_deletion"
	case DeletionKindCompute:
		return "compute_deletion"
	default:
		return "absence_verification"
	}
}

// ResourceDeletionIntent identifies the exact resource set one provider adapter
// must release. Every identity was allocated by the original accepted resource
// intent, so a retry names the same runtime, mount, Secret, volume and machine.
type ResourceDeletionIntent struct {
	TenantID      string
	WorkspaceID   string
	OperationID   string
	ResourceSetID string
	AccountID     string
	ComputeID     string
	StorageID     string
	AttachmentID  string
}

// ResourceDeletionFact is one owning-surface observation of one deletion kind.
// State is absent, pending or unknown; ProviderStatus carries the owning
// surface's own status or classification, and EvidenceRef names the readback the
// confirmation rests on.
type ResourceDeletionFact struct {
	Kind              string
	ResourceID        string
	ProviderReference string
	ProviderStatus    string
	// DestroyState is the owning surface's classification of an unfinished
	// deletion (for example pending_retry), empty once the deletion completed.
	DestroyState string
	State        string
	EvidenceRef  string
	ObservedAt   time.Time
}

func validResourceResult(in ResourceIntent, r *ResourceResult, provider string) error {
	if r == nil || r.Binding == nil || r.ObservedAt.IsZero() || r.Network == nil || strings.TrimSpace(r.Network.ProviderReference) == "" ||
		r.Binding.ComputeAllocationId != in.ComputeID || r.Binding.StorageVolumeId != in.StorageID ||
		r.Binding.AccountId == "" || r.Binding.DataAttachmentId == "" || r.Binding.DataAttachmentOperationId == "" ||
		r.Compute.ID != in.ComputeID || r.Compute.WorkspaceID != in.WorkspaceID || r.Compute.AccountID != r.Binding.AccountId ||
		r.Compute.Provider != provider || r.Compute.ProviderResourceID == "" ||
		r.Storage.ID != in.StorageID || r.Storage.WorkspaceID != in.WorkspaceID || r.Storage.AccountID != r.Binding.AccountId ||
		r.Storage.Provider != provider || r.Storage.ProviderResourceID == "" ||
		r.Attachment.ID != r.Binding.DataAttachmentId || r.Attachment.OperationID != r.Binding.DataAttachmentOperationId ||
		r.Attachment.WorkspaceID != in.WorkspaceID || r.Attachment.ComputeID != in.ComputeID ||
		r.Attachment.VolumeID != in.StorageID || r.Attachment.ProviderAttachmentID == "" {
		return errors.New("resource readback differs from the original intent")
	}
	return nil
}

// shortDigest is a deterministic identity for provider-neutral derived rows.
func shortDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])[:18]
}
