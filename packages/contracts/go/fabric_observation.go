package contracts

// ResourceObservation is the current read-only view of a provider resource.
// It does not replace retained ProviderFact validation or authorize a mutation.
type ResourceObservation struct {
	Available             bool                            `json:"available"`
	State                 ResourceObservedState           `json:"state"`
	ObservedAt            string                          `json:"observedAt"`
	ReasonCode            string                          `json:"reasonCode,omitempty"`
	ProviderID            string                          `json:"providerId,omitempty"`
	PackageOrSpec         string                          `json:"packageOrSpec,omitempty"`
	Zone                  string                          `json:"zone,omitempty"`
	CreatedAt             string                          `json:"createdAt,omitempty"`
	ExpiresAt             string                          `json:"expiresAt,omitempty"`
	ComputeRuntimeBinding *WorkspaceComputeRuntimeBinding `json:"computeRuntimeBinding,omitempty"`
}

type ResourceObservedState string

const (
	ResourceObservedReady     ResourceObservedState = "ready"
	ResourceObservedRunning   ResourceObservedState = "running"
	ResourceObservedStopped   ResourceObservedState = "stopped"
	ResourceObservedSuspended ResourceObservedState = "suspended"
	ResourceObservedAttached  ResourceObservedState = "attached"
	ResourceObservedDetached  ResourceObservedState = "detached"
	ResourceObservedPending   ResourceObservedState = "pending"
	ResourceObservedAbsent    ResourceObservedState = "absent"
	ResourceObservedUnknown   ResourceObservedState = "unknown"
)

type RuntimeOwnership string

const (
	RuntimeOwnershipVerified     RuntimeOwnership = "verified"
	RuntimeOwnershipUnregistered RuntimeOwnership = "unregistered"
	RuntimeOwnershipConflict     RuntimeOwnership = "conflict"
)

// RuntimeObservation retains discovered binding claims even when unregistered.
// Only verified ownership can establish a resource's authoritative binding.
type RuntimeObservation struct {
	ObjectRef     string                `json:"objectRef"`
	AccountID     string                `json:"accountId,omitempty"`
	WorkspaceID   string                `json:"workspaceId,omitempty"`
	RuntimeID     string                `json:"runtimeId,omitempty"`
	Ownership     RuntimeOwnership      `json:"ownership"`
	DesiredState  ResourceObservedState `json:"desiredState"`
	ObservedState ResourceObservedState `json:"observedState"`
	ReasonCode    string                `json:"reasonCode,omitempty"`
}

// RuntimeObservations is complete for the configured installation scope.
// Incomplete discovery must fail instead of proving that an object is absent.
type RuntimeObservations struct {
	ObservedAt string               `json:"observedAt"`
	Items      []RuntimeObservation `json:"items"`
}

// FabricReadiness keeps service operation separate from strict image qualification.
// Neither readiness fact grants purchase or lifecycle authority.
type FabricReadiness struct {
	Provider             string   `json:"provider,omitempty"`
	Ready                bool     `json:"ready"`
	ServiceReady         bool     `json:"serviceReady"`
	CloudImagesReady     bool     `json:"cloudImagesReady"`
	WorkspaceImagesReady bool     `json:"workspaceImagesReady"`
	ImmutableImagesReady bool     `json:"immutableImagesReady"`
	MissingEnv           []string `json:"missingEnv"`
	MissingTools         []string `json:"missingTools"`
	FailedChecks         []string `json:"failedChecks"`
}
