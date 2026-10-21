package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"opl-cloud/packages/contracts/go/owneridentity"
)

const activationFixtureBody = `{"email":"customer@example.com","password":"CorrectHorseBatteryStaple!","name":"Customer One","admission":"full_cloud_customer"}`

func activationHeaders(key string) func(http.Header) {
	return func(h http.Header) {
		h.Set("Content-Type", "application/json")
		h.Set("Idempotency-Key", key)
	}
}

// TestCloudOperatorActivationSharesTheRetainedWriter proves the Console BFF can
// run the operator activation through the established peer/token/session proof,
// and that the internal route executes the same writer as the retained browser
// route: one account, one Gateway user and the same receipt, now including the
// Gateway subject the Tenant owner needs.
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

// customerAccounts counts the activated customer accounts. The fixture's own
// bootstrap administrator is not an activation result.
// TestRetainedOperatorActivationStillWorks proves the browser route the legacy
// console still calls keeps its established session, CSRF and idempotency guards
// and now reads the Gateway subject back in its receipt.
func TestRetainedOperatorActivationStillWorks(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
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
	body := make([]byte, 4096)
	n, _ := response.Body.Read(body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("retained activation status=%d body=%s", response.StatusCode, body[:n])
	}
	receipt := map[string]any{}
	if err := json.Unmarshal(body[:n], &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"] != "succeeded" || receipt["sub2apiUserId"] != "41" || receipt["accountId"] == "" {
		t.Fatalf("retained activation receipt=%v", receipt)
	}
}

func customerAccounts(accounts []map[string]any) int {
	count := 0
	for _, account := range accounts {
		if stringValue(account["id"]) != "acct-admin" {
			count++
		}
	}
	return count
}
