package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	bff "opl-cloud/apps/console-bff"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// activationHandlerReader adds the Workspace owner read to the dialed activation
// composition, so the same real handler serves both the operator activation and
// the customer's own workspace list.
type activationHandlerReader struct {
	bff.OperatorActivationClient
	workspace api.WorkspaceProductServiceClient
}

func (r activationHandlerReader) WorkspaceClient() api.WorkspaceProductServiceClient {
	return r.workspace
}

// activationControlPlane is the retained owner's activation writer over HTTP. It
// keeps the established BFF proof (peer, service token and live session), requires
// the idempotency key, and creates the Gateway user the receipt reads back.
func activationControlPlane(t *testing.T, system *activationSystem) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/cloud/operator/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(owneridentity.PeerHeader) != owneridentity.ConsoleBFF.String() ||
			r.Header.Get(owneridentity.TokenHeader) != system.token ||
			r.Header.Get(contracts.CloudSessionHeader) == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Idempotency-Key") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		email, _ := body["email"].(string)
		if email == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		subject := system.sub2api.resolve(email)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"operationId":   "account-provision-" + strconv.FormatInt(subject, 10),
			"accountId":     "acct-" + strconv.FormatInt(subject, 10),
			"sub2apiUserId": strconv.FormatInt(subject, 10),
			"status":        "succeeded", "workspacePurchaseEnabled": true,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func activationHandler(t *testing.T, system *activationSystem, control *httptest.Server) http.Handler {
	t.Helper()
	reader, err := bff.DialOperatorActivationReader(control.URL, system.token, system.address, system.token, system.token, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return bff.NewOperatorActivationHandler(activationHandlerReader{OperatorActivationClient: reader, workspace: system.workspace})
}

// TestLiveActivationAuthorizesTheCustomerWorkspaceRead is the acceptance proof for
// issue #1004: one operator activation admits the Tenant whose owner is the
// activated Gateway subject, binds that subject's billing, and the customer's own
// workspace read is then authorized and answers an empty page instead of 403.
func TestLiveActivationAuthorizesTheCustomerWorkspaceRead(t *testing.T) {
	system := activationLiveSystem(t)
	control := activationControlPlane(t, system)
	handler := activationHandler(t, system, control)

	operatorCookie, operatorCSRF := system.login(t, "operator@example.test")
	activation := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-customer-1",
		`{"email":"customer@example.test","password":"password","name":"Customer One","admission":"full_cloud_customer"}`)
	if activation.Code != http.StatusCreated {
		t.Fatalf("activation status=%d body=%s", activation.Code, activation.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(activation.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	tenantID, _ := receipt["tenantId"].(string)
	if receipt["status"] != "succeeded" || tenantID == "" || receipt["sub2apiUserId"] != strconv.Itoa(activationSubject) {
		t.Fatalf("activation receipt=%v", receipt)
	}
	binding, _ := receipt["billingBinding"].(map[string]any)
	if binding == nil || binding["status"] != "succeeded" || binding["billingSub2ApiUserId"] != strconv.Itoa(activationSubject) {
		t.Fatalf("activation billing readback=%v", receipt["billingBinding"])
	}

	// The Tenant owner persisted exactly one Tenant and one owner membership for the
	// activated subject, and the Gateway owner persisted exactly one binding.
	var tenants, owners, bindings int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants WHERE id=$1`, tenantID).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE tenant_id=$1 AND actor_id=$2 AND role='owner' AND revoked_at IS NULL`, tenantID, strconv.Itoa(activationSubject)).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE tenant_id=$1 AND billing_sub2api_user_id=$2 AND revoked_at IS NULL`, tenantID, strconv.Itoa(activationSubject)).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if tenants != 1 || owners != 1 || bindings != 1 {
		t.Fatalf("activation persisted tenants=%d owners=%d bindings=%d", tenants, owners, bindings)
	}

	// The customer can now use the console: the same route that was 403 is
	// authorized and the owner answers an empty page.
	customerCookie, _ := system.login(t, "customer@example.test")
	workspaces := system.read(handler, customerCookie, "/api/v2/workspaces?limit=100")
	if workspaces.Code != http.StatusOK {
		t.Fatalf("activated customer workspace read status=%d body=%s", workspaces.Code, workspaces.Body.String())
	}
	if !strings.Contains(workspaces.Body.String(), `"items":[]`) {
		t.Fatalf("activated customer workspace page=%s", workspaces.Body.String())
	}
}

// TestLiveActivationIsIdempotent proves re-running the one activation operation for
// the same customer converges: the same Tenant, one owner membership and one
// billing binding, and a receipt that reads them back rather than reporting a
// second admission.
func TestLiveActivationIsIdempotent(t *testing.T) {
	system := activationLiveSystem(t)
	control := activationControlPlane(t, system)
	handler := activationHandler(t, system, control)
	operatorCookie, operatorCSRF := system.login(t, "operator@example.test")
	body := `{"email":"customer@example.test","password":"password","name":"Customer One","admission":"full_cloud_customer"}`

	first := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-first", body)
	second := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-second", body)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("activation statuses=%d/%d first=%s second=%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	firstReceipt, secondReceipt := map[string]any{}, map[string]any{}
	_ = json.Unmarshal(first.Body.Bytes(), &firstReceipt)
	_ = json.Unmarshal(second.Body.Bytes(), &secondReceipt)
	if firstReceipt["tenantId"] == "" || firstReceipt["tenantId"] != secondReceipt["tenantId"] {
		t.Fatalf("re-run admitted a different tenant: %v vs %v", firstReceipt["tenantId"], secondReceipt["tenantId"])
	}
	firstBinding, _ := firstReceipt["billingBinding"].(map[string]any)
	secondBinding, _ := secondReceipt["billingBinding"].(map[string]any)
	if firstBinding == nil || secondBinding == nil || secondBinding["status"] != "succeeded" ||
		secondBinding["bindingVersion"] != firstBinding["bindingVersion"] || secondBinding["billingSub2ApiUserId"] != firstBinding["billingSub2ApiUserId"] {
		t.Fatalf("re-run changed the binding: %v vs %v", firstReceipt["billingBinding"], secondReceipt["billingBinding"])
	}
	var tenants, owners, bindings int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE role='owner' AND revoked_at IS NULL`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE revoked_at IS NULL`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if tenants != 1 || owners != 1 || bindings != 1 {
		t.Fatalf("re-run persisted tenants=%d owners=%d bindings=%d", tenants, owners, bindings)
	}
}
