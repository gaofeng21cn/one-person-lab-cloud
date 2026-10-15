package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type financialReceiptOwner struct {
	api.UnimplementedLedgerProductServiceServer
	request  *api.GetWorkspaceFinancialReceiptRpcRequest
	response *api.WorkspaceFinancialReceipt
	err      error
}

func (o *financialReceiptOwner) GetWorkspaceFinancialReceipt(_ context.Context, r *api.GetWorkspaceFinancialReceiptRpcRequest) (*api.WorkspaceFinancialReceipt, error) {
	o.request = r
	return o.response, o.err
}

func TestFinancialReceiptBFFUsesLedgerWireAndExactIdentity(t *testing.T) {
	owner := &financialReceiptOwner{response: &api.WorkspaceFinancialReceipt{WorkspaceId: "ws-1", Receipt: &api.Receipt{Id: "receipt-1", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Original confirmed charge", CreatedAt: timestamppb.Now()}, WalletOperation: &api.WalletOperation{Id: "charge-1", WorkspaceId: proto.String("ws-1"), ReceiptId: proto.String("receipt-1"), AmountUsdMicros: 9007199254740993, Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now()}, Quote: &api.Quote{Id: "quote-1", Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY, RuntimeReadbackRequirement: api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED, ComputePlanId: "compute-1", StoragePlanId: "storage-1", PeriodMonths: 1, PeriodStart: timestamppb.Now(), PeriodEnd: timestamppb.Now(), PricePolicyVersionId: "price-1", RefundPolicyVersionId: "refund-1", RetentionPolicyVersionId: "retention-1", RefundTerms: "Original refund terms", RetentionTerms: "Original retention terms", ExpectedInterruption: "none", Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED, TotalUsdMicros: 9007199254740993, LineItems: []*api.QuoteLine{}, ExpiresAt: timestamppb.Now(), CreatedAt: timestamppb.Now()}, EvidenceDigest: "sha256:" + strings.Repeat("a", 64)}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	api.RegisterLedgerProductServiceServer(server, owner)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	c, err := clients.Dial(clients.Config{Addresses: map[owneridentity.Owner]string{owneridentity.Ledger: listener.Addr().String()}, Tokens: map[owneridentity.Owner]string{owneridentity.Ledger: "isolated-ledger-client-token-00000001"}, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEFINANCIALRECEIPT
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_LEDGER
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("ws-1")}
	handler := NewServer(c, identity).Handler()
	read := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, sessionRequest(http.MethodGet, path))
		return r
	}
	r := read("/api/v2/workspaces/ws-1/financial-receipts/receipt-1")
	if r.Code != http.StatusOK || owner.request.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") || owner.request.GetReceiptId() != "receipt-1" || !strings.Contains(r.Body.String(), `"amountUSDMicros":"9007199254740993"`) {
		t.Fatalf("wire=%d %s request=%v", r.Code, r.Body.String(), owner.request)
	}
	owner.response.WorkspaceId = "foreign"
	if r := read("/api/v2/workspaces/ws-1/financial-receipts/receipt-1"); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("wrong Workspace=%d %s", r.Code, r.Body.String())
	}
	owner.response.WorkspaceId = "ws-1"
	owner.err = status.Error(codes.NotFound, "original receipt absent")
	if r := read("/api/v2/workspaces/ws-1/financial-receipts/receipt-1"); r.Code != http.StatusNotFound {
		t.Fatalf("missing receipt=%d %s", r.Code, r.Body.String())
	}
	owner.request = nil
	if r := read("/api/v2/workspaces/ws-1/financial-receipts/receipt-1?tenantId=foreign"); r.Code != http.StatusBadRequest || owner.request != nil {
		t.Fatalf("query injection=%d %s", r.Code, r.Body.String())
	}
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	if r := read("/api/v2/workspaces/ws-1/financial-receipts/receipt-1"); r.Code != http.StatusForbidden {
		t.Fatalf("role denial=%d %s", r.Code, r.Body.String())
	}
}
