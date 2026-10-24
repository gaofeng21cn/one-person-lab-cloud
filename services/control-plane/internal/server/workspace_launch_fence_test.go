package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// seedRetainedWorkspaceLaunchForTest persists a Launch operation with the same
// identities and request shape the retained route admitted before the purchase
// cutover. It is an internal historical fixture: the HTTP route is not used, so
// a test cannot mistake a new POST for a historical obligation.
func seedRetainedWorkspaceLaunchForTest(
	t *testing.T,
	server http.Handler,
	accountID, ownerUserID string,
	sub2APIUserID int64,
	name, packageID, key string,
	autoRenew bool,
) workspaceLaunchReconcileOperation {
	t.Helper()
	return seedRetainedWorkspaceLaunchFixture(t, server, clients.SessionDelegatedCredential{}, accountID, ownerUserID, sub2APIUserID,
		name, packageID, key, autoRenew, contracts.WorkspaceProvisioningResourceOnly, false)
}

// seedRetainedWorkspaceLaunchWithDefaultApplicationForTest persists the
// historical pair the retained route admitted for a default-App request: a
// resource-only Launch operation plus the default application operation that
// froze the active image and Workspace Key group at admission time.
func seedRetainedWorkspaceLaunchWithDefaultApplicationForTest(
	t *testing.T,
	server http.Handler,
	session *httptest.ResponseRecorder,
	accountID, ownerUserID string,
	sub2APIUserID int64,
	name, packageID, key string,
	autoRenew bool,
) workspaceLaunchReconcileOperation {
	t.Helper()
	credential := sessionDelegatedCredentialForTest(t, server, session)
	return seedRetainedWorkspaceLaunchWithDefaultApplicationCredentialForTest(t, server, credential, accountID, ownerUserID, sub2APIUserID, name, packageID, key, autoRenew)
}

func seedRetainedWorkspaceLaunchWithDefaultApplicationCredentialForTest(
	t *testing.T,
	server http.Handler,
	credential clients.SessionDelegatedCredential,
	accountID, ownerUserID string,
	sub2APIUserID int64,
	name, packageID, key string,
	autoRenew bool,
) workspaceLaunchReconcileOperation {
	t.Helper()
	return seedRetainedWorkspaceLaunchFixture(t, server, credential, accountID, ownerUserID, sub2APIUserID,
		name, packageID, key, autoRenew, contracts.WorkspaceProvisioningResourceOnly, true)
}

func sessionDelegatedCredentialForTest(t *testing.T, server http.Handler, session *httptest.ResponseRecorder) clients.SessionDelegatedCredential {
	t.Helper()
	handler, ok := server.(*controlPlaneHTTPHandler)
	if !ok {
		t.Fatalf("unexpected control-plane test handler %T", server)
	}
	for _, cookie := range session.Result().Cookies() {
		if value, found := handler.app.sessionCredentials.Get(sessionLookupKey(cookie.Value)); found {
			return value
		}
	}
	t.Fatal("missing delegated session credential")
	return clients.SessionDelegatedCredential{}
}

func seedRetainedWorkspaceLaunchFixture(
	t *testing.T,
	server http.Handler,
	credential clients.SessionDelegatedCredential,
	accountID, ownerUserID string,
	sub2APIUserID int64,
	name, packageID, key string,
	autoRenew bool,
	mode contracts.WorkspaceProvisioningMode,
	defaultApplication bool,
) workspaceLaunchReconcileOperation {
	t.Helper()
	handler, ok := server.(*controlPlaneHTTPHandler)
	if !ok {
		t.Fatalf("unexpected control-plane test handler %T", server)
	}
	ctx := context.Background()
	catalog, err := handler.service.FabricCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pools := computePoolsFromFabricCatalog(catalog)
	storageGB, available := providerPackageStorageGB(pools, packageID)
	if !available {
		t.Fatalf("package %q is not available in the test catalog", packageID)
	}
	quote, err := handler.app.pricingPreviewResponse(ctx, map[string]any{"resourceType": "workspace", "packageId": packageID}, pools)
	if err != nil {
		t.Fatal(err)
	}
	quote = customerPricingPreviewDTO(quote)
	quote = handler.app.applyResourceBillingQuote(quote)
	imageDigest := ""
	if mode != contracts.WorkspaceProvisioningResourceOnly {
		imageDigest = "repo.example/workspace@sha256:" + strings.Repeat("b", 64)
	}
	descriptor, err := newWorkspaceLaunchDescriptorWithImage(
		accountID, ownerUserID, name, packageID, storageGB, autoRenew,
		stringValue(quote["priceVersion"]), key, imageDigest, mode,
	)
	if err != nil {
		t.Fatal(err)
	}
	preflightInput := clients.WorkspaceLaunchPreflightInput{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, LaunchOperationID: descriptor.OperationID,
		AccountID: accountID, WorkspaceID: descriptor.WorkspaceID, PackageID: packageID, SizeGB: storageGB,
		WorkspaceImageDigest: imageDigest, ProvisioningMode: string(mode), RequestHash: descriptor.RequestHash,
	}
	preflight, err := handler.service.PreflightWorkspaceLaunch(ctx, preflightInput)
	if err != nil {
		t.Fatal(err)
	}
	if !workspaceLaunchPreflightConfirmed(preflightInput, preflight) {
		t.Fatalf("historical preflight was not confirmed: %+v", preflight)
	}
	preChargeBalance := int64(0)
	if handler.app.deployment.resourceBillingEnabled() {
		balance, err := handler.service.Sub2APIBalance(ctx, sub2APIUserID)
		if err != nil {
			t.Fatal(err)
		}
		preChargeBalance = balance.USDMicros
	}
	workspaceKeyGroupID := int64(7)
	var defaultOperation map[string]any
	if defaultApplication {
		workspaceKeyGroupID, err = workspaceCodexGroupID(ctx, handler.service, credential, sub2APIUserID)
		if err != nil {
			t.Fatal(err)
		}
		policy, _, _, err := handler.app.currentWorkspaceImageReleasePolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defaultOperation, err = workspaceDefaultApplicationRow(workspaceDefaultApplicationRequest{
			SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID(descriptor.OperationID), LaunchOperationID: descriptor.OperationID,
			AccountID: accountID, WorkspaceID: descriptor.WorkspaceID, OwnerUserID: ownerUserID, Sub2APIUserID: sub2APIUserID,
			WorkspaceKeyGroupID: workspaceKeyGroupID, Revision: defaultOPLApplicationRevision(policy.ActiveImage), Phase: "credentials_required",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	created, err := handler.app.createWorkspaceLaunch(ctx, handler.service, credential, sub2APIUserID, workspaceLaunchReconcileCreate{
		DefaultApplicationOperation: defaultOperation,
		OperationID:                 descriptor.OperationID, RequestHash: descriptor.RequestHash, AccountID: accountID, OwnerUserID: ownerUserID,
		Sub2APIUserID: sub2APIUserID, WorkspaceID: descriptor.WorkspaceID,
		Name: name, PackageID: packageID, StorageGB: storageGB, AutoRenew: autoRenew,
		PriceVersion: stringValue(quote["priceVersion"]), TotalChargeUSDMicros: int64(numberField(quote, "totalChargeUsdMicros", 0)),
		ProviderProfileRef: preflight.ProviderProfileRef, PreflightBindingRef: preflight.BindingRef, SpecDigest: preflight.SpecDigest,
		WorkspaceImageDigest: imageDigest, WorkspaceKeyGroupID: workspaceKeyGroupID, PreChargeBalanceMicros: preChargeBalance,
		ResourceBillingEnabled: boolPtr(handler.app.deployment.resourceBillingEnabled()),
		Mode:                   mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaultOperation != nil {
		// The retained route ignored application-preparation errors here; the
		// Launch obligation and the frozen default request are the fixture.
		_ = handler.app.prepareDefaultWorkspaceApplication(ctx, handler.service, created.ID, credential, sub2APIUserID)
		_ = handler.app.runWorkspaceDefaultApplication(ctx, handler.service, workspaceDefaultApplicationOperationID(created.ID))
	}
	return created
}

func TestRetainedWorkspaceLaunchRefusesFreshPurchaseBeforeAnyWriter(t *testing.T) {
	server, store, client, _, events := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	beforeOperations, err := store.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeWorkspaces, err := store.ListWorkspaces(context.Background(), "acct-alpha")
	if err != nil {
		t.Fatal(err)
	}

	response := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches",
		`{"name":"Fresh purchase","packageId":"basic","autoRenew":false}`, "fresh-purchase-fence")
	if response.Code != http.StatusConflict {
		t.Fatalf("fresh purchase status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != retainedWorkspaceLaunchNewPurchaseRetired {
		t.Fatalf("fresh purchase error=%#v body=%s", body["error"], response.Body.String())
	}
	if body["owner"] != "workspace" {
		t.Fatalf("fresh purchase did not name the owner: %#v", body)
	}
	next, _ := body["next"].([]any)
	if len(next) != 2 || next[0] != "POST /api/v2/quotes" || next[1] != "POST /api/v2/workspaces" {
		t.Fatalf("fresh purchase did not name the owner route: %#v", body["next"])
	}

	afterOperations, err := store.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterWorkspaces, err := store.ListWorkspaces(context.Background(), "acct-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterOperations) != len(beforeOperations) || len(afterWorkspaces) != len(beforeWorkspaces) {
		t.Fatalf("refused purchase persisted facts: operations=%d->%d workspaces=%d->%d",
			len(beforeOperations), len(afterOperations), len(beforeWorkspaces), len(afterWorkspaces))
	}
	if len(client.charges) != 0 || len(*events) != 0 {
		t.Fatalf("refused purchase crossed a financial or provider boundary: charges=%#v events=%#v", client.charges, *events)
	}
}

func TestRetainedWorkspaceLaunchReplaysOnlyTheHistoricalKey(t *testing.T) {
	server, store, client, _, events := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	historical := seedRetainedWorkspaceLaunchForTest(t, server, "acct-alpha", "usr-alpha", 41, "Historical purchase", "basic", "historical-purchase", false)
	eventsBefore, chargesBefore := len(*events), len(client.charges)

	replay := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches",
		`{"name":"Historical purchase","packageId":"basic","autoRenew":false,"provisioningMode":"resource_only"}`, "historical-purchase")
	if replay.Code != http.StatusAccepted {
		t.Fatalf("historical replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayed map[string]any
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if stringValue(replayed["operationId"]) != historical.ID {
		t.Fatalf("historical replay returned operation %q, want %q", stringValue(replayed["operationId"]), historical.ID)
	}
	operations, err := store.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 1 {
		t.Fatalf("historical replay created another operation: %#v", operations)
	}
	if len(client.charges) != chargesBefore || len(*events) != eventsBefore {
		t.Fatalf("historical replay crossed a new boundary: charges=%#v events=%#v", client.charges, *events)
	}

	conflict := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches",
		`{"name":"Different purchase","packageId":"basic","autoRenew":false,"provisioningMode":"resource_only"}`, "historical-purchase")
	assertErrorResponse(t, conflict.Code, conflict.Body.String(), http.StatusConflict, errIdempotencyConflict.Error())

	fresh := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches",
		`{"name":"Another purchase","packageId":"basic","autoRenew":false}`, "another-purchase")
	if fresh.Code != http.StatusConflict || !strings.Contains(fresh.Body.String(), retainedWorkspaceLaunchNewPurchaseRetired) {
		t.Fatalf("fresh purchase after replay status=%d body=%s", fresh.Code, fresh.Body.String())
	}
}

func TestRetainedWorkspaceLaunchKeepsHistoricalReadsAndCrossAccountIsolation(t *testing.T) {
	server, store, _, _, _ := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	historical := seedRetainedWorkspaceLaunchForTest(t, server, "acct-alpha", "usr-alpha", 41, "Historical purchase", "basic", "historical-read", false)
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")

	read := requestWithSession(t, server, session, http.MethodGet, "/api/workspace-launches/"+historical.ID, "")
	if read.Code != http.StatusOK {
		t.Fatalf("historical read status=%d body=%s", read.Code, read.Body.String())
	}
	list := requestWithSession(t, server, session, http.MethodGet, "/api/workspace-launches", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), historical.ID) {
		t.Fatalf("historical list status=%d body=%s", list.Code, list.Body.String())
	}

	other := seedTenantMemberForFenceTest(t, store, "acct-other", "usr-other", "beta-fence@example.com")
	otherSession := loginForTest(t, server, other, "CorrectHorseBatteryStaple!")
	crossRead := requestWithSession(t, server, otherSession, http.MethodGet, "/api/workspace-launches/"+historical.ID, "")
	if crossRead.Code != http.StatusNotFound {
		t.Fatalf("cross-account read status=%d body=%s", crossRead.Code, crossRead.Body.String())
	}
	crossReplay := requestWithMutationKeyForTest(t, server, otherSession, http.MethodPost, "/api/workspace-launches",
		`{"name":"Historical purchase","packageId":"basic","autoRenew":false}`, "historical-read")
	if crossReplay.Code != http.StatusConflict || !strings.Contains(crossReplay.Body.String(), retainedWorkspaceLaunchNewPurchaseRetired) {
		t.Fatalf("cross-account replay status=%d body=%s", crossReplay.Code, crossReplay.Body.String())
	}
	if strings.Contains(crossReplay.Body.String(), historical.ID) {
		t.Fatalf("cross-account refusal leaked the historical operation: %s", crossReplay.Body.String())
	}
}

func seedTenantMemberForFenceTest(t *testing.T, store controlPlaneTableStore, accountID, userID, email string) string {
	t.Helper()
	seedTenantMember(t, store, accountID, "", userID, email)
	return email
}

func TestRetainedWorkspaceLaunchRefusalStillRequiresSessionAndCSRF(t *testing.T) {
	server, _, _, _, _ := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	withoutSession := httptest.NewRequest(http.MethodPost, "/api/workspace-launches", strings.NewReader(`{"name":"Fresh","packageId":"basic","autoRenew":false}`))
	withoutSession.Header.Set("Content-Type", "application/json")
	withoutSession.Header.Set("Idempotency-Key", "fence-without-session")
	withoutSessionRec := httptest.NewRecorder()
	server.ServeHTTP(withoutSessionRec, withoutSession)
	assertErrorResponse(t, withoutSessionRec.Code, withoutSessionRec.Body.String(), http.StatusUnauthorized, "not_authenticated")

	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/workspace-launches", strings.NewReader(`{"name":"Fresh","packageId":"basic","autoRenew":false}`))
	withoutCSRF.Header.Set("Content-Type", "application/json")
	withoutCSRF.Header.Set("Idempotency-Key", "fence-without-csrf")
	addSessionCookies(withoutCSRF, session)
	withoutCSRFRec := httptest.NewRecorder()
	server.ServeHTTP(withoutCSRFRec, withoutCSRF)
	if withoutCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("fence without CSRF status=%d body=%s", withoutCSRFRec.Code, withoutCSRFRec.Body.String())
	}
}
