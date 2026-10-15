package eventconsumer

import (
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/internal/ledger"
)

func TestTeamFinancialReceiptReadsOriginalMonetaryEvidenceWithoutPrivateActor(t *testing.T) {
	db, service, owners, writer, originalReader := coordinatedService(t)
	input := paidCoordinationRequest()
	input.QuoteAcceptance.Quote.TotalUsdMicros = 9007199254740993
	input.QuoteAcceptance.Quote.LineItems[0].AmountUsdMicros = 9007199254740993
	input.WalletOperation.AmountUsdMicros = 9007199254740993
	owners.quote, owners.commit, owners.wallet = input.QuoteAcceptance, input.OwnerCommitEvidence, input.WalletOperation
	receipt, err := writer.AppendReceipt(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := service.NewGRPC(ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: coordinationToken}})
	if err != nil {
		t.Fatal(err)
	}
	address := startCoordinationWire(t, wire)
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.ConsoleBFF, owneridentity.Ledger.Service(), coordinationToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := api.NewLedgerProductServiceClient(conn)
	call := proto.Clone(input.Context).(*api.CallContext)
	call.ActorId = "current-team-admin"
	call.SessionId = proto.String("live-customer-session")
	call.AcceptedOperationGrantId = nil
	read := &api.GetWorkspaceFinancialReceiptRpcRequest{Context: call, WorkspaceId: input.QuoteAcceptance.WorkspaceId, ReceiptId: receipt.Id}
	got, err := client.GetWorkspaceFinancialReceipt(t.Context(), read)
	if err != nil || !proto.Equal(got.GetReceipt(), receipt) || !proto.Equal(got.GetWalletOperation(), input.WalletOperation) || !proto.Equal(got.GetQuote(), input.QuoteAcceptance.Quote) {
		t.Fatalf("original monetary evidence changed: %v %v", got, err)
	}
	encoded, err := protojson.Marshal(got)
	if err != nil || !strings.Contains(string(encoded), `"amountUsdMicros":"9007199254740993"`) || strings.Contains(string(encoded), input.Context.ActorId) || strings.Contains(string(encoded), "ownerCommit") {
		t.Fatalf("customer projection precision/privacy: %s %v", encoded, err)
	}
	// Existing full evidence retains its actor fence for the same team's other actor.
	private := &api.GetReceiptByReferenceRequest{Context: proto.Clone(input.Context).(*api.CallContext), Owner: "workspace", OwnerEvidenceReference: input.OwnerEvidenceReference}
	private.Context.ActorId = call.ActorId
	if _, err := originalReader.ReadWalletActionReceipt(t.Context(), private); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("private actor fence changed: %v", err)
	}
	// Privacy redaction of mutable provenance cannot widen the private evidence or
	// erase the team's accepted financial facts.
	if _, err := service.store.PrivacyDeleteReceipt(t.Context(), ledger.ReceiptPrivacyDeleteInput{ReceiptID: receipt.Id, Reason: "test-request", IdempotencyKey: "redact-financial"}); err != nil {
		t.Fatal(err)
	}
	again, err := client.GetWorkspaceFinancialReceipt(t.Context(), read)
	if err != nil || !proto.Equal(got, again) {
		t.Fatalf("history changed after redaction: %v %v", again, err)
	}
	if _, err := originalReader.ReadWalletActionReceipt(t.Context(), private); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("redacted actor fence changed: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.GetWorkspaceFinancialReceiptRpcRequest)
		code   codes.Code
	}{
		{"other Tenant", func(r *api.GetWorkspaceFinancialReceiptRpcRequest) {
			r.Context.Scope.GetTenant().TenantId = "other-tenant"
		}, codes.NotFound},
		{"other receipt", func(r *api.GetWorkspaceFinancialReceiptRpcRequest) { r.ReceiptId = "other-receipt" }, codes.NotFound},
		{"other Workspace", func(r *api.GetWorkspaceFinancialReceiptRpcRequest) { r.WorkspaceId = "other-workspace" }, codes.PermissionDenied},
		{"grant substitution", func(r *api.GetWorkspaceFinancialReceiptRpcRequest) {
			r.Context.AcceptedOperationGrantId = proto.String("grant")
		}, codes.Unauthenticated},
		{"missing session", func(r *api.GetWorkspaceFinancialReceiptRpcRequest) { r.Context.SessionId = nil }, codes.Unauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := proto.Clone(read).(*api.GetWorkspaceFinancialReceiptRpcRequest)
			tc.mutate(changed)
			if _, err := client.GetWorkspaceFinancialReceipt(t.Context(), changed); status.Code(err) != tc.code {
				t.Fatalf("code=%v err=%v", status.Code(err), err)
			}
		})
	}
	// Exercise the persisted-data decoder through the actual authenticated reader.
	// The normal writer already rejects these shapes; this is not a claim that a
	// customer can forge a receipt through that writer.
	var persisted, originalPayload string
	if err := db.QueryRow(`SELECT payload_json, payload_json::jsonb #>> '{inputRefs,walletAction}' FROM evidence_receipts WHERE id=$1`, receipt.Id).Scan(&originalPayload, &persisted); err != nil {
		t.Fatal(err)
	}
	original := &api.WalletActionReceiptEvidence{}
	if err := protojson.Unmarshal([]byte(persisted), original); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.WalletActionReceiptEvidence)
	}{
		{"missing receipt", func(e *api.WalletActionReceiptEvidence) { e.Receipt = nil }},
		{"unconfirmed receipt", func(e *api.WalletActionReceiptEvidence) { e.Receipt.Outcome = 0 }},
		{"nonfinancial receipt", func(e *api.WalletActionReceiptEvidence) { e.Receipt.Kind = 0 }},
		{"other receipt owner", func(e *api.WalletActionReceiptEvidence) { e.Receipt.Owner = 0 }},
		{"missing wallet", func(e *api.WalletActionReceiptEvidence) { e.WalletOperation = nil }},
		{"missing wallet identity", func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.Id = "" }},
		{"unconfirmed charge", func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.Status = 0 }},
		{"noncharge operation", func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.Kind = 0 }},
		{"amount mismatch", func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.AmountUsdMicros-- }},
		{"missing quote acceptance", func(e *api.WalletActionReceiptEvidence) { e.QuoteAcceptance = nil }},
		{"missing quote", func(e *api.WalletActionReceiptEvidence) { e.QuoteAcceptance.Quote = nil }},
		{"unaccepted quote", func(e *api.WalletActionReceiptEvidence) { e.QuoteAcceptance.Quote.Status = 0 }},
		{"other quote workspace", func(e *api.WalletActionReceiptEvidence) {
			e.QuoteAcceptance.Quote.WorkspaceId = proto.String("other-workspace")
		}},
		{"zero funding", func(e *api.WalletActionReceiptEvidence) {
			e.QuoteAcceptance.Quote.TotalUsdMicros = 0
			e.WalletOperation.AmountUsdMicros = 0
		}},
		{"missing digest", func(e *api.WalletActionReceiptEvidence) { e.EvidenceDigest = "" }},
	} {
		t.Run("persisted "+tc.name, func(t *testing.T) {
			changed := proto.Clone(original).(*api.WalletActionReceiptEvidence)
			tc.mutate(changed)
			raw, err := protojson.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE evidence_receipts SET payload_json=jsonb_set($1::jsonb, '{inputRefs,walletAction}', $2::jsonb)::text WHERE id=$3`, originalPayload, string(raw), receipt.Id); err != nil {
				t.Fatal(err)
			}
			if got, err := client.GetWorkspaceFinancialReceipt(t.Context(), read); status.Code(err) != codes.InvalidArgument || got != nil {
				t.Fatalf("invalid persisted funding returned: %v %v", got, err)
			}
		})
	}
	if _, err := db.Exec(`UPDATE evidence_receipts SET payload_json=$1 WHERE id=$2`, originalPayload, receipt.Id); err != nil {
		t.Fatal(err)
	}
	if restored, err := client.GetWorkspaceFinancialReceipt(t.Context(), read); err != nil || !proto.Equal(got, restored) {
		t.Fatalf("valid persisted financial evidence changed: %v %v", restored, err)
	}
	owners.mu.Lock()
	owners.deny = true
	owners.mu.Unlock()
	if _, err := client.GetWorkspaceFinancialReceipt(t.Context(), read); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked live authority: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM evidence_receipts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reader wrote receipts: %d %v", count, err)
	}
}
