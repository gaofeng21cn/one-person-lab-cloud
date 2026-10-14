package eventconsumer

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	ledgerhttp "opl-cloud/services/ledger/internal/http"
	"opl-cloud/services/ledger/internal/ledger"
)

// This file is the read-boundary regression for the provider-action receipt kind.
// It drives the production HTTP handler with both guards enabled — the bearer
// token and the HMAC ledger capability — over the same real PostgreSQL store the
// typed coordination writer uses, because the defect was never in the writer: it
// was in the generic/enriched reader.

// foreignAccountCapability is the capability an audit-owned Control Plane caller
// holds for a different account with no Workspace restriction. It is a valid,
// correctly signed capability; nothing about it is forged.
const (
	httpBoundaryCapabilityKey = "ledger-capability-key-for-http-boundary-32ch"
	httpBoundaryAccount       = "acct-foreign"
	// httpBoundaryCapabilityHeader is the header the production Control Plane
	// client sets on every scoped Ledger request
	// (services/control-plane/internal/clients/ledger.go). It is written out
	// because the server's constant is unexported in internal/http.
	httpBoundaryCapabilityHeader = "X-OPL-Ledger-Capability"
)

// httpBoundaryClaims mirrors the claim shape the production Control Plane signer
// emits. It is written out here rather than imported because the production
// signer is unexported in the client package.
type httpBoundaryClaims struct {
	Version      int    `json:"version"`
	Caller       string `json:"caller"`
	AccountID    string `json:"accountId"`
	WorkspaceID  string `json:"workspaceId"`
	ResourceKind string `json:"resourceKind"`
	ResourceID   string `json:"resourceId"`
	Action       string `json:"action"`
	OperationID  string `json:"operationId"`
	ExpiresAt    int64  `json:"expiresAt"`
	BodySHA256   string `json:"bodySha256"`
}

func signHTTPCapability(t *testing.T, claims httpBoundaryClaims, body []byte) string {
	t.Helper()
	digest := sha256.Sum256(body)
	claims.BodySHA256 = hex.EncodeToString(digest[:])
	claims.ExpiresAt = time.Now().Add(time.Minute).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(httpBoundaryCapabilityKey))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func httpRequestOperation(method, target string) string {
	sum := sha256.Sum256([]byte(method + " " + target))
	return "request:" + hex.EncodeToString(sum[:])
}

// httpBoundaryReceiptTarget is the exact URL the production Control Plane client
// requests for a scoped single-receipt read: ReceiptForAccount appends the
// account it is authorized for and signs the capability with the same account.
// Without that query parameter the leak path below is never reached, so the
// regression must send what the real caller sends.
func httpBoundaryReceiptTarget(receiptID string) string {
	return "/ledger/receipts/" + receiptID + "?accountId=" + httpBoundaryAccount
}

// httpBoundaryRead issues one guarded read against the production handler with a
// valid capability naming the foreign account, exactly as the Control Plane client
// signs it for GET /ledger/receipts/{id}.
func httpBoundaryRead(t *testing.T, handler http.Handler, target, resourceKind, resourceID, action string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+coordinationToken)
	req.Header.Set(httpBoundaryCapabilityHeader,
		signHTTPCapability(t, httpBoundaryClaims{Version: 1, Caller: "control-plane", AccountID: httpBoundaryAccount,
			ResourceKind: resourceKind, ResourceID: resourceID, Action: action, OperationID: httpRequestOperation(http.MethodGet, target)}, nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// assertNoCommitActorLeak is the security assertion: whatever the status, the
// response must not carry the private typed evidence the generic reader would
// otherwise serialize.
func assertNoCommitActorLeak(t *testing.T, label string, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := rec.Body.String()
	for _, private := range []string{commitActor, "ownerCommitEvidence", "providerAction", "authorizationContextId"} {
		if strings.Contains(body, private) {
			t.Fatalf("%s leaked %q: status=%d body=%s", label, private, rec.Code, body)
		}
	}
}

// TestProviderActionReceiptIsNotReadableThroughTheGenericHTTPReader proves the new
// kind's generic read boundary inside Ledger.
//
// The provider-action payload persists the full Workspace owner commit, including
// the committing actor and its authorization context. That evidence is private and
// owner-authoritative: it is served only by the authenticated typed
// LedgerCoordination boundary, which binds the read to that immutable commit.
//
// The generic HTTP reader could not admit it safely. Its admission is an account
// and Workspace comparison against the stored generic row, and this kind's row
// carries no account at all, so there is nothing to bind the caller's requested
// account to: the requested value survived enrichment unchanged and every
// capability naming any account was admitted, together with the private commit.
//
// The regression drives the real guarded handler, not the writer:
//
//  1. both guards are genuinely enabled (missing bearer 401, missing capability 403);
//  2. the same capability, handler and request shape do serve an ordinary generic
//     receipt that really carries the requested account, so a refusal below is the
//     boundary and not a malformed capability;
//  3. a valid capability scoped to a different account with no Workspace
//     restriction cannot read the receipt and never sees the commit actor — before
//     the privacy mutation and again after it;
//  4. the collection reader cannot return it either;
//  5. the typed read still works for the committing actor;
//  6. the privacy fence still holds: the generic privacy mutation still reaches the
//     row, the stored provenance actor really is cleared, the immutable commit
//     survives, and a foreign actor stays PermissionDenied.
func TestProviderActionReceiptIsNotReadableThroughTheGenericHTTPReader(t *testing.T) {
	db, s, owners, client, _ := coordinatedService(t)
	ctx := context.Background()
	request := providerActionDeletionRequest("workspace-local", "op_delete_original")
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	appended, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatalf("append confirmed deletion evidence: %v", err)
	}
	handler := ledgerhttp.NewServerWithAuth(s.store, coordinationToken, httpBoundaryCapabilityKey)

	// 1. Both guards are on. These two legs would pass on an unguarded handler
	// only by accident, so they pin that the rest of this test is meaningful.
	anonymous := httptest.NewRequest(http.MethodGet, httpBoundaryReceiptTarget(appended.GetId()), nil)
	anonymous.Header.Set(httpBoundaryCapabilityHeader,
		signHTTPCapability(t, httpBoundaryClaims{Version: 1, Caller: "control-plane", AccountID: httpBoundaryAccount,
			ResourceKind: "receipt", ResourceID: appended.GetId(), Action: "read_receipt",
			OperationID: httpRequestOperation(http.MethodGet, httpBoundaryReceiptTarget(appended.GetId()))}, nil))
	anonymousRec := httptest.NewRecorder()
	handler.ServeHTTP(anonymousRec, anonymous)
	if anonymousRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status=%d body=%s, want 401", anonymousRec.Code, anonymousRec.Body.String())
	}
	unscoped := httptest.NewRequest(http.MethodGet, httpBoundaryReceiptTarget(appended.GetId()), nil)
	unscoped.Header.Set("Authorization", "Bearer "+coordinationToken)
	unscopedRec := httptest.NewRecorder()
	handler.ServeHTTP(unscopedRec, unscoped)
	if unscopedRec.Code != http.StatusForbidden {
		t.Fatalf("missing capability status=%d body=%s, want 403", unscopedRec.Code, unscopedRec.Body.String())
	}

	// 2. The same capability, the same handler and the same signed request shape do
	// serve an ordinary generic receipt that really carries the requested account.
	// Without this leg a wrong header name, a wrong signature or a wrong account
	// encoding would make every refusal below look like the boundary working.
	ordinary, err := s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: "source_check", Status: "completed", Surface: "cloud",
		AccountID: httpBoundaryAccount, WorkspaceID: "workspace-http-boundary", IdempotencyKey: "http-boundary-ordinary"})
	if err != nil {
		t.Fatalf("ordinary generic receipt: %v", err)
	}
	ordinaryRead := httpBoundaryRead(t, handler, httpBoundaryReceiptTarget(ordinary.ReceiptID), "receipt", ordinary.ReceiptID, "read_receipt")
	if ordinaryRead.Code != http.StatusOK || !strings.Contains(ordinaryRead.Body.String(), ordinary.ReceiptID) {
		t.Fatalf("account-scoped ordinary receipt status=%d body=%s, want 200 with the receipt", ordinaryRead.Code, ordinaryRead.Body.String())
	}

	// 3. The defect itself: an audit-owned capability for a different account, with
	// no Workspace restriction, must not read this kind's private evidence. This is
	// the assertion that returned 200 with the provider commit actor before the fix.
	readBefore := httpBoundaryRead(t, handler, httpBoundaryReceiptTarget(appended.GetId()), "receipt", appended.GetId(), "read_receipt")
	if readBefore.Code == http.StatusOK {
		t.Fatalf("foreign-account capability read status=%d body=%s, want the generic reader to refuse the private typed evidence", readBefore.Code, readBefore.Body.String())
	}
	assertNoCommitActorLeak(t, "foreign-account read before redaction", readBefore)

	// 4. The collection reader is the same generic surface and must not return it.
	collectionTarget := "/ledger/receipts?accountId=" + httpBoundaryAccount
	collection := httpBoundaryRead(t, handler, collectionTarget, "receipt_collection", httpBoundaryAccount, "list_receipts")
	assertNoCommitActorLeak(t, "foreign-account collection read", collection)
	if strings.Contains(collection.Body.String(), ProviderActionReceiptTypeProbe) {
		t.Fatalf("collection reader returned the private typed evidence: status=%d body=%s", collection.Code, collection.Body.String())
	}

	// 5. The typed read still works for the committing actor. The boundary moved
	// where private evidence is served; it did not remove the read the deletion
	// closure depends on.
	if _, err = client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, commitActor, commitTenant)); err != nil {
		t.Fatalf("committing actor typed read refused: %v", err)
	}

	// 6. The privacy mutation is the same generic endpoint the first regression
	// used, and it still reaches this row: a data-subject obligation is not fenced
	// off by this boundary.
	if _, err = s.store.PrivacyDeleteReceipt(ctx, ledger.ReceiptPrivacyDeleteInput{ReceiptID: appended.GetId(),
		IdempotencyKey: "privacy-http-read-boundary", Reason: "data subject request"}); err != nil {
		t.Fatalf("privacy delete of the provider-action receipt: %v", err)
	}
	redacted, err := ledger.NewPostgresStore(db).ReadProviderActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.ActorID != "" {
		t.Fatalf("stored provenance actor=%q, want empty after redaction", redacted.ActorID)
	}
	if got := redacted.Evidence.GetOwnerCommitEvidence().GetActorId(); got != commitActor {
		t.Fatalf("immutable commit actor=%q, want %q", got, commitActor)
	}

	// The generic reader is still closed on the other side of the mutation, and the
	// typed privacy fence still holds for the same row.
	readAfter := httpBoundaryRead(t, handler, httpBoundaryReceiptTarget(appended.GetId()), "receipt", appended.GetId(), "read_receipt")
	if readAfter.Code == http.StatusOK {
		t.Fatalf("foreign-account capability read after redaction status=%d body=%s, want a refusal", readAfter.Code, readAfter.Body.String())
	}
	assertNoCommitActorLeak(t, "foreign-account read after redaction", readAfter)
	if _, err = client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, foreignActor, commitTenant)); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign actor after redaction: code=%v err=%v, want PermissionDenied", status.Code(err), err)
	}
	readback, err := client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, commitActor, commitTenant))
	if err != nil || !proto.Equal(readback, appended) {
		t.Fatalf("authorized typed readback=%v err=%v, want the stored deletion receipt", readback, err)
	}
}

// ProviderActionReceiptTypeProbe is the persisted receipt type of the private
// evidence. It is referenced from the collection assertion as a literal-free probe
// so the test fails if the generic list ever starts serving the row.
const ProviderActionReceiptTypeProbe = "workspace.provider_action.v1"

// TestWalletActionReceiptIsNotReadableThroughTheGenericHTTPReader covers the
// funded sibling of the provider-action boundary. All three typed writers store
// private owner commitments without an Account projection. Real Ledger gRPC and
// PostgreSQL feed the production HTTP handler; upstream owners are typed stubs.
func TestWalletActionReceiptIsNotReadableThroughTheGenericHTTPReader(t *testing.T) {
	db, service, owners, client, fabric := coordinatedService(t)
	ctx := context.Background()
	request := paidCoordinationRequest()
	owners.quote = proto.Clone(request.QuoteAcceptance).(*api.QuoteAcceptance)
	owners.wallet = proto.Clone(request.WalletOperation).(*api.WalletOperation)
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	receipt, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	handler := ledgerhttp.NewServerWithAuth(service.store, coordinationToken, httpBoundaryCapabilityKey)
	// The same valid account capability must still serve its own ordinary receipt.
	ordinary, err := service.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: "source_check", Status: "completed", Surface: "cloud", AccountID: httpBoundaryAccount, WorkspaceID: "workspace-local", IdempotencyKey: "wallet-boundary-ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	positive := httpBoundaryRead(t, handler, httpBoundaryReceiptTarget(ordinary.ReceiptID), "receipt", ordinary.ReceiptID, "read_receipt")
	if positive.Code != http.StatusOK || !strings.Contains(positive.Body.String(), ordinary.ReceiptID) {
		t.Fatalf("ordinary account receipt status=%d body=%s", positive.Code, positive.Body.String())
	}
	check := func(stage string) {
		t.Helper()
		result := httpBoundaryRead(t, handler, httpBoundaryReceiptTarget(receipt.GetId()), "receipt", receipt.GetId(), "read_receipt")
		if result.Code != http.StatusForbidden {
			t.Errorf("%s wallet HTTP status=%d body=%s, want 403", stage, result.Code, result.Body.String())
		}
		for _, private := range []string{commitActor, "ownerCommitEvidence", "walletAction", "authorizationContextId"} {
			if strings.Contains(result.Body.String(), private) {
				t.Errorf("%s generic reader leaked %q", stage, private)
			}
		}
		// Both typed entry points retain the actor and tenant fences.
		for _, identity := range []struct{ actor, tenant string }{{foreignActor, commitTenant}, {commitActor, foreignTenant}} {
			read := redactingCall(request.OwnerEvidenceReference, identity.actor, identity.tenant)
			if _, err := client.ReadWalletActionReceipt(ctx, read); status.Code(err) != codes.PermissionDenied {
				t.Errorf("%s typed wallet read actor=%s tenant=%s: %v", stage, identity.actor, identity.tenant, err)
			}
			if _, err := client.ReadReceiptByReference(ctx, read); status.Code(err) != codes.PermissionDenied {
				t.Errorf("%s resolver actor=%s tenant=%s: %v", stage, identity.actor, identity.tenant, err)
			}
		}
		read := redactingCall(request.OwnerEvidenceReference, commitActor, commitTenant)
		typed, err := fabric.ReadWalletActionReceipt(ctx, read)
		if err != nil || !proto.Equal(typed.GetReceipt(), receipt) || !proto.Equal(typed.GetWalletOperation(), request.WalletOperation) {
			t.Fatalf("%s authorized Fabric funding read changed: %v", stage, err)
		}
		resolved, err := client.ReadReceiptByReference(ctx, read)
		if err != nil || !proto.Equal(resolved, receipt) {
			t.Fatalf("%s authorized resolver: %v", stage, err)
		}
	}
	check("before privacy")
	// Direct owner retention/privacy operations still reach the typed row.
	if _, err := service.store.UpdateReceiptRetention(ctx, ledger.ReceiptRetentionInput{ReceiptID: receipt.GetId(), IdempotencyKey: "wallet-http-retention", RetainUntil: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.PrivacyDeleteReceipt(ctx, ledger.ReceiptPrivacyDeleteInput{ReceiptID: receipt.GetId(), IdempotencyKey: "wallet-http-privacy", Reason: "synthetic privacy request"}); err != nil {
		t.Fatal(err)
	}
	stored, err := ledger.NewPostgresStore(db).ReadWalletActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ActorID != "" || stored.Evidence.GetOwnerCommitEvidence().GetActorId() != commitActor {
		t.Fatal("privacy mutation changed immutable authorization evidence or retained provenance")
	}
	check("after privacy")
	// Legacy Billing consumes an account-bound gateway adjustment, not the
	// accountless typed funding envelope. Keep its actual detail/list shapes on
	// both implementations while the PostgreSQL store also holds typed evidence.
	for _, legacy := range []struct {
		name  string
		store ledger.Store
	}{{"postgres", service.store}, {"memory", ledger.NewMemoryStore()}} {
		t.Run("legacy billing "+legacy.name, func(t *testing.T) {
			input := ledger.ReceiptInput{Type: "gateway.wallet_adjustment.v1", Status: "completed", Surface: "control_plane", AccountID: httpBoundaryAccount,
				RequestID: "wallet-adjustment-alpha", Actor: map[string]any{"userId": "usr-admin"},
				Execution: map[string]any{"operationId": "wallet-adjustment-alpha", "kind": "business_refund", "amountUsdMicros": int64(2_500_000)},
				InputRefs: map[string]any{"balanceHistoryRef": "sub2api:balance-history:41:history-alpha", "relatedOperationId": "workspace-launch-alpha"},
				Owner:     map[string]any{"accountId": httpBoundaryAccount}, IdempotencyKey: "wallet-adjustment-alpha:receipt"}
			created, err := legacy.store.RecordReceipt(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := legacy.store.RecordReceipt(ctx, input)
			if err != nil || !replay.Replayed || replay.ReceiptID != created.ReceiptID {
				t.Fatalf("legacy replay=%v err=%v", replay, err)
			}
			guarded := ledgerhttp.NewServerWithAuth(legacy.store, coordinationToken, httpBoundaryCapabilityKey)
			response := httpBoundaryRead(t, guarded, httpBoundaryReceiptTarget(created.ReceiptID), "receipt", created.ReceiptID, "read_receipt")
			var readback ledger.Receipt
			decoder := json.NewDecoder(strings.NewReader(response.Body.String()))
			decoder.UseNumber()
			if err := decoder.Decode(&readback); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || readback.ReceiptID != created.ReceiptID || readback.AccountID != input.AccountID || readback.Type != input.Type || readback.Execution["kind"] != "business_refund" {
				t.Fatalf("legacy detail status=%d receipt=%v", response.Code, readback)
			}
			if amount, ok := readback.Execution["amountUsdMicros"].(json.Number); !ok || amount.String() != "2500000" {
				t.Fatalf("legacy refund amount=%v", readback.Execution["amountUsdMicros"])
			}
			target := "/ledger/receipts?accountId=" + httpBoundaryAccount + "&typePrefix=billing.&includeType=gateway.wallet_adjustment.v1&includeExecutionKind=business_refund"
			response = httpBoundaryRead(t, guarded, target, "receipt_collection", httpBoundaryAccount, "list_receipts")
			var page ledger.ReceiptPage
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || len(page.Receipts) != 1 || page.Receipts[0].ReceiptID != created.ReceiptID {
				t.Fatalf("legacy inclusive Billing list status=%d page=%v", response.Code, page)
			}
		})
	}
	// This bearer-only handler observes the store filter directly, without an
	// account query hiding accountless rows before their type admission is tested.
	unguarded := ledgerhttp.NewServer(service.store, coordinationToken)
	paths := []struct {
		target   string
		ordinary bool
	}{
		{"/ledger/receipts?workspaceId=workspace-local", true},
		{"/ledger/receipts?workspaceId=workspace-local&type=" + ledger.WalletActionReceiptType, false},
		{"/ledger/receipts?workspaceId=workspace-local&typePrefix=workspace.", false},
		{"/ledger/receipts?workspaceId=workspace-local&typePrefix=source_&includeType=" + ledger.WalletActionReceiptType, true},
	}
	for _, tc := range paths {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+coordinationToken)
		rec := httptest.NewRecorder()
		unguarded.ServeHTTP(rec, req)
		var page ledger.ReceiptPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("collection %s status=%d", tc.target, rec.Code)
		}
		want := 0
		if tc.ordinary {
			want = 1
		}
		if len(page.Receipts) != want || (want == 1 && page.Receipts[0].ReceiptID != ordinary.ReceiptID) {
			t.Errorf("collection %s receipts=%v, want only expected ordinary rows", tc.target, page.Receipts)
		}
		for _, private := range []string{"walletAction", "ownerCommitEvidence", commitActor} {
			if strings.Contains(rec.Body.String(), private) {
				t.Errorf("collection %s leaked %q", tc.target, private)
			}
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/ledger/receipts/"+receipt.GetId(), nil)
	req.Header.Set("Authorization", "Bearer "+coordinationToken)
	rec := httptest.NewRecorder()
	unguarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("single store boundary status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
	// Idempotent append and owner-reference replay preserve the funded receipt.
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.IdempotencyKey += "-retry"
	repeated, err := client.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(repeated, receipt) {
		t.Fatalf("wallet replay changed: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.WalletActionReceiptType).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("wallet rows=%d err=%v, want one", rows, err)
	}
}
