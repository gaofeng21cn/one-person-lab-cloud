package server

import (
	"net/http"
	"net/http/httptest"
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

// TestCloudOperatorActivationRouteIsRetired proves the retained Control Plane
// no longer opens customer accounts. The account-opening write moved to the
// CloudIdentity owner, so the internal activation route is gone: a BFF-shaped
// request to it is refused and no account is created.
func TestCloudOperatorActivationRouteIsRetired(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	response := f.requestRaw(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-customer-1"))
	if response.code == http.StatusCreated {
		t.Fatalf("the retired activation route created an account: %d %s", response.code, response.body)
	}
	accounts, err := f.store.ListAccounts(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if customerAccounts(accounts) != 0 {
		t.Fatalf("the retired activation route created accounts: %v", accounts)
	}
}

// TestCloudOperatorActivationRefusesNonBFFCallers proves no caller reaches an
// account-opening writer: the route is retired, so a missing peer, token or
// live session changes nothing and no account is created.
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

// TestCloudOperatorActivationRouteStaysRetiredOnReplay proves the retired route
// stays retired for every caller: repeated activation requests neither create
// an account nor reach a provisioning writer.
func TestCloudOperatorActivationRouteStaysRetiredOnReplay(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	first := f.requestRaw(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-once"))
	second := f.requestRaw(t, http.MethodPost, "/internal/cloud/operator/accounts", operator, activationFixtureBody, activationHeaders("activate-again"))
	if first.code == http.StatusCreated || second.code == http.StatusCreated {
		t.Fatalf("the retired activation route created accounts: %d %d", first.code, second.code)
	}
	accounts, err := f.store.ListAccounts(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if customerAccounts(accounts) != 0 {
		t.Fatalf("the retired activation route created %d customer accounts: %v", customerAccounts(accounts), accounts)
	}
}

// The retired browser alias has no POST handler. A stale authenticated operator
// receives 404 without reaching the internal activation writer.
func TestRetiredOperatorActivationAliasRejectsPostWithoutCreatingAccount(t *testing.T) {
	store := newMemoryTableStore()
	f := newCloudAnnouncementHTTPFixture(t, store)
	session := reservedOperatorSessionForTest(t, f.handler)
	request := httptest.NewRequest(http.MethodPost, "/api/operator/accounts", strings.NewReader(activationFixtureBody))
	addAuth(request, session)
	activationHeaders("activate-retired-alias")(request.Header)
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("retired activation status=%d want 404; body=%s", response.Code, response.Body.String())
	}
	accounts, err := store.ListAccounts(t.Context(), "")
	if err != nil || customerAccounts(accounts) != 0 {
		t.Fatalf("retired activation changed accounts: accounts=%v err=%v", accounts, err)
	}
	user, found, err := store.GetUserByEmail(t.Context(), "customer@example.com", true)
	if err != nil || found {
		t.Fatalf("retired activation created a user: user=%v found=%v err=%v", user, found, err)
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
