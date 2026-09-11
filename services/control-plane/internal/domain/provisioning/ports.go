package provisioning

import (
	"context"
	"time"
)

// PaymentState is the resolved state of one debit reference.
type PaymentState string

const (
	// PaymentConfirmed means the wallet recorded the debit.
	PaymentConfirmed PaymentState = "confirmed"
	// PaymentPending means the debit may still be in flight.
	PaymentPending PaymentState = "pending"
	// PaymentUnknown means the financial outcome is not decidable; the
	// operation parks for review with the retained review semantics.
	PaymentUnknown PaymentState = "unknown"
	// PaymentNotAttempted means no debit exists for the reference yet.
	PaymentNotAttempted PaymentState = "not_attempted"
)

// PaymentOutcome reports the authoritative state of one debit reference.
type PaymentOutcome struct {
	State            PaymentState
	Reference        string
	ChargedUSDMicros int64
}

// PaymentConfirmer is the wallet port. A debit is charged once under a stable
// payment reference and recovered by lookup; the write itself is never
// replayed.
type PaymentConfirmer interface {
	Charge(ctx context.Context, paymentRef string, sub2apiUserID int64, amountUSDMicros int64) (PaymentOutcome, error)
	LookupCharge(ctx context.Context, paymentRef string, sub2apiUserID int64) (PaymentOutcome, error)
}

// ResourceRef identifies one delivered resource and its binding reference.
type ResourceRef struct {
	ID         string
	BindingRef string
}

// ResourceRequest carries the provider-scoped facts of one resource stage.
type ResourceRequest struct {
	AccountID   string
	WorkspaceID string
	ProviderRef string
	StorageGB   int
}

// ResourceProvisioner is the Fabric resource port for the compute, storage and
// attachment stages. Implementations must be idempotent by idempotency key and
// must not require an application image for resource-only provisioning.
type ResourceProvisioner interface {
	EnsureCompute(ctx context.Context, request ResourceRequest, idempotencyKey string) (ResourceRef, error)
	EnsureStorage(ctx context.Context, request ResourceRequest, idempotencyKey string) (ResourceRef, error)
	EnsureAttachment(ctx context.Context, request ResourceRequest, idempotencyKey string) (ResourceRef, error)
}

// PurchaseEvidence is the receipt payload of a completed resource purchase.
type PurchaseEvidence struct {
	AccountID       string
	OwnerUserID     string
	WorkspaceID     string
	OperationID     string
	AmountUSDMicros int64
}

// EvidenceWriter is the Ledger port. A failed receipt write retries only the
// evidence write; it never rewinds resources or repeats the debit.
type EvidenceWriter interface {
	RecordPurchaseReceipt(ctx context.Context, evidence PurchaseEvidence, idempotencyKey string) (receiptID string, err error)
}

// Clock supplies the reconciler's notion of time so response-loss and
// deadline rules stay testable.
type Clock interface {
	Now() time.Time
}
