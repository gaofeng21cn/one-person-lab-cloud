package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	bff "opl-cloud/apps/console-bff"
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

// activationHandler serves the real Console BFF routes over the real dialed
// CloudIdentity owner. The account-opening command is the owner's own now, so
// no retained Control Plane writer stands in for it.
func activationHandler(t *testing.T, system *activationSystem) http.Handler {
	t.Helper()
	reader, err := bff.DialOperatorActivationReader("", "", system.address, system.token, system.token, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return bff.NewOperatorActivationHandler(activationHandlerReader{OperatorActivationClient: reader, workspace: system.workspace})
}

// activationOperation is the owner's published command receipt.
type activationOperation struct {
	OperationID              string `json:"operationId"`
	AccountID                string `json:"accountId"`
	Status                   string `json:"status"`
	WorkspacePurchaseEnabled bool   `json:"workspacePurchaseEnabled"`
}

// TestLiveActivationAuthorizesTheCustomerWorkspaceRead is the acceptance proof for
// issue #1004: one operator activation opens the customer account, admits the
// Tenant whose owner is the activated Gateway subject, binds that subject's
// billing, and the customer's own workspace read is then authorized and answers
// an empty page instead of 403.
func TestLiveActivationAuthorizesTheCustomerWorkspaceRead(t *testing.T) {
	system := activationLiveSystem(t)
	handler := activationHandler(t, system)

	operatorCookie, operatorCSRF := system.login(t, "operator@example.test")
	activation := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-customer-1",
		`{"email":"customer@example.test","password":"password","name":"Customer One","admission":"full_cloud_customer"}`)
	if activation.Code != http.StatusAccepted {
		t.Fatalf("activation status=%d body=%s", activation.Code, activation.Body.String())
	}
	operation := activationOperationOf(t, activation)
	if operation.Status != "succeeded" || operation.OperationID == "" || operation.AccountID == "" {
		t.Fatalf("activation operation=%+v", operation)
	}
	if !operation.WorkspacePurchaseEnabled {
		t.Fatalf("the activated customer was not granted purchase eligibility: %+v", operation)
	}

	// The owner persisted exactly one account, one Tenant, one owner membership
	// and one wallet binding for the activated subject.
	var accounts, tenants, owners, bindings int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts WHERE email='customer@example.test'`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	tenantID := system.tenantOf(t, strconv.Itoa(activationSubject))
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants WHERE id=$1`, tenantID).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE tenant_id=$1 AND actor_id=$2 AND role='owner' AND revoked_at IS NULL`, tenantID, strconv.Itoa(activationSubject)).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE tenant_id=$1 AND billing_sub2api_user_id=$2 AND revoked_at IS NULL`, tenantID, strconv.Itoa(activationSubject)).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tenants != 1 || owners != 1 || bindings != 1 {
		t.Fatalf("activation persisted accounts=%d tenants=%d owners=%d bindings=%d", accounts, tenants, owners, bindings)
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

// TestLiveActivationIsIdempotent proves re-running the one activation command
// for the same customer converges: the same Tenant, one owner membership and one
// billing binding, and one account, instead of a second admission.
//
// The replay carries the same Idempotency-Key, so it is the same command the
// owner already reserved: the owner answers the same Operation and never
// dispatches the Gateway account write a second time.
func TestLiveActivationIsIdempotent(t *testing.T) {
	system := activationLiveSystem(t)
	handler := activationHandler(t, system)
	operatorCookie, operatorCSRF := system.login(t, "operator@example.test")
	body := `{"email":"customer@example.test","password":"password","name":"Customer One","admission":"full_cloud_customer"}`

	first := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-customer-1", body)
	second := system.post(handler, operatorCookie, operatorCSRF, "/api/v2/operator/accounts", "activate-customer-1", body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("activation statuses=%d/%d first=%s second=%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	firstOperation := activationOperationOf(t, first)
	secondOperation := activationOperationOf(t, second)
	if firstOperation.OperationID == "" || firstOperation.OperationID != secondOperation.OperationID {
		t.Fatalf("re-run reported a different operation: %v vs %v", firstOperation, secondOperation)
	}
	var accounts, tenants, owners, bindings int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE role='owner' AND revoked_at IS NULL`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE revoked_at IS NULL`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tenants != 1 || owners != 1 || bindings != 1 {
		t.Fatalf("re-run persisted accounts=%d tenants=%d owners=%d bindings=%d", accounts, tenants, owners, bindings)
	}
}

// activationOperationOf reads the owner's published Operation answer.
func activationOperationOf(t *testing.T, response *httptest.ResponseRecorder) activationOperation {
	t.Helper()
	var operation activationOperation
	if err := json.Unmarshal(response.Body.Bytes(), &operation); err != nil {
		t.Fatalf("activation operation %s: %v", response.Body.String(), err)
	}
	return operation
}
