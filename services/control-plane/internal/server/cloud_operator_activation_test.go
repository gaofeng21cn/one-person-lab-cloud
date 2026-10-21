package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const activationFixtureBody = `{"email":"customer@example.com","password":"CorrectHorseBatteryStaple!","name":"Customer One","admission":"full_cloud_customer"}`

func activationHeaders(key string) func(http.Header) {
	return func(h http.Header) {
		h.Set("Content-Type", "application/json")
		h.Set("Idempotency-Key", key)
	}
}

// TestCloudOperatorActivationSharesTheRetainedWriter proves the Console BFF can
// run the operator activation through the established peer/token/session proof.
// The internal route is the only route that still runs the shared activation
// writer; the retained browser route refuses before any write (#1007): one
// account, one Gateway user and the same receipt, now including the Gateway
// subject the Tenant owner needs.
func TestCloudOperatorActivationSharesTheRetainedWriter(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	response := f.request(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-customer-1"))
	if response.code != http.StatusCreated {
		t.Fatalf("activation status=%d body=%s", response.code, response.body)
	}
	receipt := map[string]any{}
	if err := json.Unmarshal(response.body, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"] != "succeeded" || receipt["accountId"] == "" || receipt["operationId"] == "" || receipt["workspacePurchaseEnabled"] != true {
		t.Fatalf("activation receipt=%v", receipt)
	}
	if receipt["sub2apiUserId"] != "41" {
		t.Fatalf("activation receipt did not read back the Gateway subject: %v", receipt)
	}
	account, found, err := f.store.GetAccount(t.Context(), receipt["accountId"].(string))
	if err != nil || !found {
		t.Fatalf("activated account was not persisted: found=%v err=%v", found, err)
	}
	if int64(numberField(account, "sub2apiUserId", 0)) != 41 {
		t.Fatalf("persisted account subject=%v", account)
	}
	assertNoCookieMutation(t, response)

}

// TestCloudOperatorActivationRefusesNonBFFCallers proves the new write route does
// not widen the established operator proof: a missing peer, token or live session
// is rejected before the writer runs, and no account is created.
func TestCloudOperatorActivationRefusesNonBFFCallers(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	for name, test := range map[string]struct {
		mutate func(http.Header)
		body   string
		path   string
	}{
		"missing peer":          {mutate: func(h http.Header) { h.Del(owneridentity.PeerHeader) }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"wrong peer":            {mutate: func(h http.Header) { h.Set(owneridentity.PeerHeader, owneridentity.ControlPlane.String()) }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"missing token":         {mutate: func(h http.Header) { h.Del(owneridentity.TokenHeader) }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"missing session":       {mutate: func(h http.Header) { h.Del("x-opl-cloud-session") }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"unknown session":       {mutate: func(h http.Header) { h.Set("x-opl-cloud-session", owneridentity.SessionReference("unknown")) }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"browser actor header":  {mutate: func(h http.Header) { h.Set("x-opl-actor", "forged") }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"browser tenant header": {mutate: func(h http.Header) { h.Set("x-opl-tenant", "forged") }, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"query selector":        {mutate: func(http.Header) {}, body: activationFixtureBody, path: "/internal/cloud/operator/accounts?accountId=acct-other"},
		"missing idempotency": {mutate: func(h http.Header) {
			h.Del("Idempotency-Key")
		}, body: activationFixtureBody, path: "/internal/cloud/operator/accounts"},
		"missing body": {mutate: func(h http.Header) { h.Set("Content-Length", "0") }, body: "", path: "/internal/cloud/operator/accounts"},
	} {
		t.Run(name, func(t *testing.T) {
			mutate := func(h http.Header) {
				activationHeaders("activate-refused")(h)
				test.mutate(h)
			}
			response := f.requestRaw(t, http.MethodPost, test.path, operator, test.body, mutate)
			if response.code == http.StatusCreated {
				t.Fatalf("refused caller created an account: %d %s", response.code, response.body)
			}
			if strings.Contains(string(response.body), `"status":"succeeded"`) {
				t.Fatalf("refused caller received a success receipt: %s", response.body)
			}
		})
	}
	if accounts, err := f.store.ListAccounts(t.Context(), ""); err != nil {
		t.Fatal(err)
	} else if customerAccounts(accounts) != 0 {
		t.Fatalf("refused callers created accounts: %v", accounts)
	}
}

// TestCloudOperatorActivationIsRepeatableForTheSameAccount proves re-running
// activation for the same customer converges on the one account and one Gateway
// subject rather than creating a second identity. The Tenant step built on this
// receipt is keyed from the same account.
func TestCloudOperatorActivationIsRepeatableForTheSameAccount(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	first := f.request(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-once"))
	second := f.request(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-again"))
	if first.code != http.StatusCreated || second.code != http.StatusCreated {
		t.Fatalf("activation statuses=%d/%d", first.code, second.code)
	}
	firstReceipt, secondReceipt := map[string]any{}, map[string]any{}
	_ = json.Unmarshal(first.body, &firstReceipt)
	_ = json.Unmarshal(second.body, &secondReceipt)
	if firstReceipt["accountId"] != secondReceipt["accountId"] || firstReceipt["sub2apiUserId"] != secondReceipt["sub2apiUserId"] {
		t.Fatalf("repeat activation changed the identity: %v vs %v", firstReceipt, secondReceipt)
	}
	accounts, err := f.store.ListAccounts(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if customerAccounts(accounts) != 1 {
		t.Fatalf("repeat activation created %d customer accounts: %v", customerAccounts(accounts), accounts)
	}
}

// activationBillingProbe is the Ledger owner's billing-record store. The retired
// route must not reach it, so the test counts RecordReceipt rather than trusting
// the status code alone.
type activationBillingProbe struct {
	fakeLedgerClient
	mu       sync.Mutex
	receipts int
}

func (p *activationBillingProbe) RecordReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	p.mu.Lock()
	p.receipts++
	p.mu.Unlock()
	return p.fakeLedgerClient.RecordReceipt(ctx, input, key)
}

func (p *activationBillingProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.receipts
}

// TestRetainedOperatorActivationIsMovedAndCreatesNothing is the #1007 acceptance
// proof. The retained browser route used to create a Gateway user and a billing
// account with no Tenant, so the customer could sign in and every tenant-scoped
// read was refused. The route now refuses with 409 before any write and names the
// composed route. The test reads every store the writer would have touched.
func TestRetainedOperatorActivationIsMovedAndCreatesNothing(t *testing.T) {
	store := newMemoryTableStore()
	billing := &activationBillingProbe{}
	gateway := &testSub2APIClient{balance: 1_000_000, charges: map[string]int64{}}
	f := newCloudAnnouncementHTTPFixture(t, store, controlplane.NewService(billing, &fakeFabricClient{}, gateway))
	session := reservedOperatorSessionForTest(t, f.handler)

	request, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/operator/accounts", strings.NewReader(activationFixtureBody))
	if err != nil {
		t.Fatal(err)
	}
	addAuth(request, session)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "activate-retained")
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	email := normalizeEmail("customer@example.com")
	accountID := "acct-" + stableID("account", email)[:18]
	accounts, accountsErr := store.ListAccounts(t.Context(), "")
	account, accountFound, accountErr := store.GetAccount(t.Context(), accountID)
	user, userFound, userErr := store.GetUserByEmail(t.Context(), email, true)
	events, eventsErr := store.ListAuditEvents(t.Context(), accountID)
	gatewayUsers := gateway.identityCount()
	tenantID := ""
	if accountFound {
		tenantID = stringValue(account["tenantId"])
	}
	t.Logf("legacy activation status=%d body=%s accounts=%v account=%v accountTenantId=%q gatewayUsers=%d billingRecords=%d",
		response.StatusCode, body, accounts, account, tenantID, gatewayUsers, billing.count())

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("retained activation status=%d want 409; body=%s", response.StatusCode, body)
	}
	if accountsErr != nil || accountErr != nil || userErr != nil || eventsErr != nil {
		t.Fatalf("read stores: accounts=%v account=%v user=%v events=%v", accountsErr, accountErr, userErr, eventsErr)
	}
	var refusal map[string]any
	if err := json.Unmarshal(body, &refusal); err != nil {
		t.Fatalf("refusal body=%s: %v", body, err)
	}
	if refusal["error"] != "activation_moved" {
		t.Fatalf("refusal code=%v body=%s", refusal["error"], body)
	}
	message, _ := refusal["message"].(string)
	if !strings.Contains(message, "POST /api/v2/operator/accounts") || !strings.Contains(strings.ToLower(message), "moved") {
		t.Fatalf("refusal message does not name the composed route: %q", message)
	}
	if customerAccounts(accounts) != 0 || accountFound {
		t.Fatalf("retained activation created an account: accounts=%v account=%v", accounts, account)
	}
	if userFound {
		t.Fatalf("retained activation created a local user: %v", user)
	}
	if len(events) != 0 {
		t.Fatalf("retained activation wrote an audit event: %v", events)
	}
	if gatewayUsers != 0 {
		t.Fatalf("retained activation created %d Gateway users", gatewayUsers)
	}
	if billing.count() != 0 {
		t.Fatalf("retained activation wrote %d billing records", billing.count())
	}
}

// customerAccounts counts the activated customer accounts. The fixture's own
// bootstrap administrator is not an activation result.
func customerAccounts(accounts []map[string]any) int {
	count := 0
	for _, account := range accounts {
		if stringValue(account["id"]) != "acct-admin" {
			count++
		}
	}
	return count
}
