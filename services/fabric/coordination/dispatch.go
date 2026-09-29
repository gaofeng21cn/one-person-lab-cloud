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
