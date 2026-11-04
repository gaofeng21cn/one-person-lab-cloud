package launch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const purchaseEligibilityTestToken = "workspace-purchase-owner-test-token-0001"

// purchaseEligibilityReadFunc injects an explicit owner answer, including for
// live fixtures whose acceptance goal does not exercise retained Account policy.
type purchaseEligibilityReadFunc func(context.Context, *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error)

func (f purchaseEligibilityReadFunc) Read(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	return f(ctx, call)
}

func allowPurchaseEligibilityForTest(_ context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	enabled := true
	return &contracts.WorkspacePurchaseEligibility{ActorID: call.GetActorId(), AccountID: "account-purchase-fixture", WorkspacePurchaseEnabled: &enabled}, nil
}

func purchaseEligibilityCall() *api.CallContext {
	return &api.CallContext{ActorId: "actor-purchase", SessionId: proto.String(owneridentity.SessionReference("purchase-browser-cookie-fixture")), RequestId: "request-purchase"}
}

func TestPurchaseEligibilityHTTPReadback(t *testing.T) {
	valid := `{"actorId":"actor-purchase","accountId":"account-purchase","workspacePurchaseEnabled":true}`
	tests := []struct {
		name    string
		body    string
		code    codes.Code
		enabled bool
	}{
		{name: "enabled", body: valid, code: codes.OK, enabled: true},
		{name: "explicit false remains known", body: strings.Replace(valid, "true", "false", 1), code: codes.OK},
		{name: "missing bool", body: `{"actorId":"actor-purchase","accountId":"account-purchase"}`, code: codes.DataLoss},
		{name: "null bool", body: strings.Replace(valid, "true", "null", 1), code: codes.DataLoss},
		{name: "wrong bool type", body: strings.Replace(valid, "true", `"true"`, 1), code: codes.DataLoss},
		{name: "wrong actor", body: strings.Replace(valid, "actor-purchase", "other-actor", 1), code: codes.DataLoss},
		{name: "missing actor", body: `{"accountId":"account-purchase","workspacePurchaseEnabled":true}`, code: codes.DataLoss},
		{name: "empty account", body: strings.Replace(valid, "account-purchase", "", 1), code: codes.DataLoss},
		{name: "blank account", body: strings.Replace(valid, "account-purchase", "   ", 1), code: codes.DataLoss},
		{name: "padded account", body: strings.Replace(valid, "account-purchase", " account-purchase ", 1), code: codes.DataLoss},
		{name: "unknown field", body: strings.TrimSuffix(valid, "}") + `,"unexpected":true}`, code: codes.DataLoss},
		{name: "trailing JSON", body: valid + ` {"second":true}`, code: codes.DataLoss},
		{name: "trailing garbage", body: valid + ` invalid`, code: codes.DataLoss},
		{name: "truncated JSON", body: strings.TrimSuffix(valid, "}"), code: codes.DataLoss},
		{name: "null object", body: `null`, code: codes.DataLoss},
		{name: "exactly 4KiB", body: valid + strings.Repeat(" ", 4096-len(valid)), code: codes.OK, enabled: true},
		{name: "over 4KiB", body: valid + strings.Repeat(" ", 4097-len(valid)), code: codes.DataLoss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/internal/cloud/workspace-purchase-eligibility" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get(owneridentity.PeerHeader) != owneridentity.Workspace.String() || r.Header.Get(owneridentity.TokenHeader) != purchaseEligibilityTestToken || r.Header.Get(contracts.CloudSessionHeader) != purchaseEligibilityCall().GetSessionId() || r.Header.Get("Accept") != "application/json" {
					t.Error("purchase owner request does not carry the exact peer, token, live session and JSON accept headers")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			reader, err := NewPurchaseEligibilityReader(server.URL, purchaseEligibilityTestToken)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := reader.Read(t.Context(), purchaseEligibilityCall())
			if status.Code(err) != tt.code {
				t.Fatalf("code=%s want=%s: %v", status.Code(err), tt.code, err)
			}
			if tt.code != codes.OK {
				if fact != nil {
					t.Fatal("invalid readback returned an eligibility fact")
				}
			} else if fact == nil || fact.ActorID != "actor-purchase" || fact.AccountID != "account-purchase" || fact.WorkspacePurchaseEnabled == nil || *fact.WorkspacePurchaseEnabled != tt.enabled {
				t.Fatalf("owner boolean or identity lost: %+v", fact)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests=%d want=1", requests.Load())
			}
		})
	}
}

func TestPurchaseEligibilityHTTPStatus(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   codes.Code
	}{
		{http.StatusUnauthorized, codes.Unauthenticated},
		{http.StatusForbidden, codes.PermissionDenied},
		{http.StatusConflict, codes.FailedPrecondition},
		{http.StatusNotFound, codes.Unavailable},
		{http.StatusTooManyRequests, codes.Unavailable},
		{http.StatusInternalServerError, codes.Unavailable},
		{http.StatusServiceUnavailable, codes.Unavailable},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, purchaseEligibilityTestToken+purchaseEligibilityCall().GetSessionId())
			}))
			defer server.Close()
			reader, err := NewPurchaseEligibilityReader(server.URL, purchaseEligibilityTestToken)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := reader.Read(t.Context(), purchaseEligibilityCall())
			if status.Code(err) != tt.code || fact != nil {
				t.Fatalf("fact=%+v code=%s want=%s", fact, status.Code(err), tt.code)
			}
			if strings.Contains(err.Error(), purchaseEligibilityTestToken) || strings.Contains(err.Error(), purchaseEligibilityCall().GetSessionId()) {
				t.Fatal("owner error leaked a transport secret")
			}
		})
	}
}

func TestPurchaseEligibilityHTTPRejectsRedirectWithoutLeakingSecrets(t *testing.T) {
	for _, responseStatus := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(responseStatus), func(t *testing.T) {
			var destinationRequests atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destinationRequests.Add(1)
				if r.Header.Get(owneridentity.TokenHeader) != "" || r.Header.Get(contracts.CloudSessionHeader) != "" {
					t.Error("redirect destination received a secret")
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer destination.Close()
			var originRequests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originRequests.Add(1)
				http.Redirect(w, r, destination.URL+"/capture", responseStatus)
			}))
			defer origin.Close()
			reader, err := NewPurchaseEligibilityReader(origin.URL, purchaseEligibilityTestToken)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := reader.Read(t.Context(), purchaseEligibilityCall())
			if fact != nil || status.Code(err) != codes.Unavailable {
				t.Fatalf("redirect fact=%+v err=%v", fact, err)
			}
			if originRequests.Load() != 1 || destinationRequests.Load() != 0 {
				t.Fatalf("origin requests=%d destination requests=%d", originRequests.Load(), destinationRequests.Load())
			}
			if strings.Contains(err.Error(), purchaseEligibilityTestToken) || strings.Contains(err.Error(), purchaseEligibilityCall().GetSessionId()) {
				t.Fatal("redirect error leaked a secret")
			}
		})
	}
}

func TestPurchaseEligibilityHTTPRequiresLiveSession(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	reader, err := NewPurchaseEligibilityReader(server.URL, purchaseEligibilityTestToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []*api.CallContext{nil, {}, {ActorId: "actor-purchase"}, {SessionId: proto.String("purchase-session-secret")}} {
		fact, err := reader.Read(t.Context(), call)
		if fact != nil || status.Code(err) != codes.Unauthenticated {
			t.Fatalf("missing session or actor: fact=%+v err=%v", fact, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid session reached the owner")
	}
}

func TestPurchaseEligibilityHTTPContextCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-t.Context().Done():
		}
	}))
	defer server.Close()
	reader, err := NewPurchaseEligibilityReader(server.URL, purchaseEligibilityTestToken)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := reader.Read(ctx, purchaseEligibilityCall()); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach owner")
	}
	cancel()
	select {
	case err := <-done:
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("cancelled transport code=%s: %v", status.Code(err), err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request did not return")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not reach HTTP owner")
	}
}

func TestPurchaseEligibilityReaderConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, origin, token   string
		unconfigured, invalid bool
	}{
		{name: "both absent", unconfigured: true},
		{name: "both blank", origin: " ", token: " ", unconfigured: true},
		{name: "missing origin", token: purchaseEligibilityTestToken, invalid: true},
		{name: "missing token", origin: "http://127.0.0.1", invalid: true},
		{name: "short token", origin: "http://127.0.0.1", token: "short", invalid: true},
		{name: "relative origin", origin: "/owner", token: purchaseEligibilityTestToken, invalid: true},
		{name: "wrong scheme", origin: "ftp://owner.test", token: purchaseEligibilityTestToken, invalid: true},
		{name: "userinfo", origin: "http://user:secret@owner.test", token: purchaseEligibilityTestToken, invalid: true},
		{name: "path", origin: "http://owner.test/path", token: purchaseEligibilityTestToken, invalid: true},
		{name: "query", origin: "http://owner.test?secret=value", token: purchaseEligibilityTestToken, invalid: true},
		{name: "fragment", origin: "http://owner.test#fragment", token: purchaseEligibilityTestToken, invalid: true},
		{name: "valid HTTP", origin: "http://owner.test/", token: purchaseEligibilityTestToken},
		{name: "valid HTTPS", origin: "https://owner.test", token: purchaseEligibilityTestToken},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := NewPurchaseEligibilityReader(tt.origin, tt.token)
			if tt.invalid {
				if err == nil || reader != nil {
					t.Fatal("invalid owner configuration was accepted")
				}
			} else if err != nil || (reader == nil) != tt.unconfigured {
				t.Fatalf("configured=%t unconfigured=%t err=%v", reader != nil, tt.unconfigured, err)
			}
		})
	}
}
