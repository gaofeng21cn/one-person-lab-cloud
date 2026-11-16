package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
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
}

func seedTenantMemberForFenceTest(t *testing.T, store controlPlaneTableStore, accountID, userID, email string) string {
	t.Helper()
	seedTenantMember(t, store, accountID, "", userID, email)
	return email
}

func codexKeyGroupIDForTest(t *testing.T, ctx context.Context, service *controlplane.Service, credential clients.SessionDelegatedCredential, userID int64) int64 {
	t.Helper()
	groups, err := service.GatewayUserGroups(ctx, credential, userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.ID > 0 && group.Name == "Codex" && strings.EqualFold(strings.TrimSpace(group.Status), "active") {
			return group.ID
		}
	}
	t.Fatalf("no active Codex key group for user %d", userID)
	return 0
}

// seedRetainedWorkspaceLaunchFixture persists the historical Launch operation
// pair the retained route admitted before the purchase cutover.
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
	// The retained launch execution that called the provider preflight is
	// retired, so the historical seed records the same confirmed provider
	// binding the Fabric owner returned instead of calling it again.
	preflight := clients.WorkspaceLaunchPreflight{
		SchemaVersion:      clients.WorkspaceLaunchFabricSchemaVersion,
		Available:          true,
		Reason:             "none",
		LaunchOperationID:  descriptor.OperationID,
		RequestHash:        descriptor.RequestHash,
		ProviderProfileRef: "provider-profile",
		BindingRef:         "workspace-binding-" + stableID(descriptor.OperationID)[:12],
		SpecDigest:         strings.Repeat("a", 64),
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
		workspaceKeyGroupID = codexKeyGroupIDForTest(t, ctx, handler.service, credential, sub2APIUserID)
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
	created, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchReconcileCreate{
		OperationID: descriptor.OperationID, RequestHash: descriptor.RequestHash, AccountID: accountID, OwnerUserID: ownerUserID,
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
		if err := handler.app.tables.SaveRuntimeOperation(ctx, defaultOperation); err != nil {
			t.Fatal(err)
		}
	}
	row, err := workspaceLaunchReconcileOperationRow(created)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.app.tables.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}
	return created
}
