package eventconsumer

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// providerActionRequest is the receipt Workspace issues once it has confirmed a
// deletion: the original application runtime and the provider resources it used
// are both absent. It is built the way the Workspace deletion path builds it, so
// the dispatch sees the same shape that currently reaches Ledger in production.
func providerActionRequest() *api.AppendReceiptRequest {
	return &api.AppendReceiptRequest{
		Context: &api.CallContext{
			RequestId: "request-delete-alpha", IdempotencyKey: "deletion-receipt-alpha", ActorId: "actor-alpha",
			Scope:                    &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-alpha"}}},
			AcceptedOperationGrantId: proto.String("grant-alpha"),
		},
		Receipt: &api.Receipt{
			Kind:            api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION,
			Owner:           api.OwnerEnum_OWNER_ENUM_WORKSPACE,
			OperationId:     proto.String("op-delete-alpha"),
			Outcome:         api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED,
			EvidenceSummary: "Workspace deletion confirmed: the original application runtime and provider resources are absent.",
		},
		OwnerEvidenceReference: "op-delete-alpha",
	}
}

const unsupportedKindMessage = "unsupported receipt kind for this owner"

// TestAppendReceiptAdmitsProviderActionIntoItsOwnWriter records the choice this
// change makes: a confirmed deletion is a provider action, it is not a zero-charge
// order and it is not a funded wallet action, and it now has its own evidence
// authority instead of being refused.
//
//   - its persisted evidence message is ProviderActionReceiptEvidence, which
//     carries the Workspace owner commit and the digest that commit froze and no
//     quote acceptance or wallet operation, so the Workspace identity is the
//     commit's own resource id rather than a funding identity;
//   - its store writer takes that Workspace id from the owner commit, not from a
//     QuoteAcceptance a deletion never carries.
//
// The dispatch admits only this kind into the new writer. The two funded kinds
// stay in their own writers, and every other kind below is still refused.
func TestAppendReceiptAdmitsProviderActionIntoItsOwnWriter(t *testing.T) {
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	_, err := (&Server{}).AppendReceipt(ctx, providerActionRequest())
	if err == nil || err.Error() == "rpc error: code = "+codes.InvalidArgument.String()+" desc = "+unsupportedKindMessage {
		t.Fatalf("err=%v, want the dispatch to admit the provider-action kind into its own writer", err)
	}
}

// TestAppendReceiptKeepsTheTwoFundedKindsInTheirOwnWriters is the control for the
// test above: the same zero-value Server admits the two funded kinds past the
// dispatch, so the refusals are decisions about the kind and not artifacts of the
// empty Server.
func TestAppendReceiptKeepsTheTwoFundedKindsInTheirOwnWriters(t *testing.T) {
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	for name, kind := range map[string]api.ReceiptKindEnum{
		"local no charge": api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE,
		"wallet action":   api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION,
	} {
		t.Run(name, func(t *testing.T) {
			request := providerActionRequest()
			request.Receipt.Kind = kind
			_, err := (&Server{}).AppendReceipt(ctx, request)
			if err == nil || err.Error() == "rpc error: code = "+codes.InvalidArgument.String()+" desc = "+unsupportedKindMessage {
				t.Fatalf("kind=%v err=%v, want the dispatch to admit this kind into its own writer", kind, err)
			}
		})
	}
}

// TestAppendReceiptDecidesKindBeforeAnyOwnerAccess proves the dispatch chooses the
// evidence authority before it touches a store or an authorizer. The zero-value
// Server has no store, catalog, workspace or authorizer, so the only way to observe
// a decision rather than a nil dereference is to be refused by the dispatch itself.
func TestAppendReceiptDecidesKindBeforeAnyOwnerAccess(t *testing.T) {
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	for name, kind := range map[string]api.ReceiptKindEnum{
		"deployment":  api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT,
		"unspecified": api.ReceiptKindEnum_RECEIPT_KIND_ENUM_UNSPECIFIED,
		"release":     api.ReceiptKindEnum_RECEIPT_KIND_ENUM_RELEASE,
	} {
		t.Run(name, func(t *testing.T) {
			request := providerActionRequest()
			request.Receipt.Kind = kind
			if _, err := (&Server{}).AppendReceipt(ctx, request); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code=%v err=%v, want InvalidArgument", status.Code(err), err)
			}
		})
	}
}
