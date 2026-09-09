package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	d5OriginalImage = "registry.example/workspace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	d5TargetImage   = "registry.example/workspace@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	d5NextImage     = "registry.example/workspace@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type d5ImageFixture struct {
	store    *memoryTableStore
	fabric   *runtimeImageReplacementRouteFabric
	handler  *controlPlaneHTTPHandler
	operator *httptest.ResponseRecorder
	launch   workspaceLaunchReconcileOperation
}

func newD5ImageFixture(t *testing.T) d5ImageFixture {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_IMAGE", d5OriginalImage)
	t.Setenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED", "0")
	catalog, err := json.Marshal(contracts.WorkspaceImageReleaseCatalog{SchemaVersion: 1, Releases: []contracts.WorkspaceImageRelease{
		{Version: "original", Image: d5OriginalImage}, {Version: "target", Image: d5TargetImage}, {Version: "next", Image: d5NextImage},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(contracts.WorkspaceImageReleasesEnv, string(catalog))
	store := newMemoryTableStore()
	fabric := &runtimeImageReplacementRouteFabric{
		fakeFabricClient:   fakeFabricClient{runtimeStatus: clientsWorkspaceRuntimeForReleaseTest(d5OriginalImage)},
		replacementRuntime: clientsWorkspaceRuntimeForReleaseTest(d5TargetImage),
	}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	launch := seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	return d5ImageFixture{store: store, fabric: fabric, handler: server.(*controlPlaneHTTPHandler), operator: reservedOperatorSessionForTest(t, server), launch: launch}
}

func (f d5ImageFixture) replace(t *testing.T, key, target string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(workspaceRuntimeImageReplacementRequest{ReplacementImageDigest: target, Reason: "D5 business update"})
	if err != nil {
		t.Fatal(err)
	}
	return requestWithMutationKeyForTest(t, f.handler, f.operator, http.MethodPost, "/api/operator/workspaces/ws-alpha/runtime-image-replacements", string(body), key)
}

func TestD5ReplacementPinsCatalogTargetWithoutChangingPurchaseDefault(t *testing.T) {
	fixture := newD5ImageFixture(t)
	preview := requestWithSession(t, fixture.handler, fixture.operator, http.MethodGet,
		"/api/operator/workspaces/ws-alpha/runtime-image-replacements/preview?replacementImageDigest="+url.QueryEscape(d5TargetImage), "")
	var projected runtimeImageReplacementPreviewEnvelope
	if preview.Code != http.StatusOK || json.NewDecoder(preview.Body).Decode(&projected) != nil || projected.Data.TargetImageDigest != d5TargetImage || !projected.Data.CanReplace {
		t.Fatalf("target preview: status=%d result=%#v", preview.Code, projected)
	}
	created := fixture.replace(t, "d5-fixed-target", d5TargetImage)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	activation := requestWithMutationKeyForTest(t, fixture.handler, fixture.operator, http.MethodPost, "/api/operator/workspace-image-release-activations", workspaceImageReleaseActivationBodyForTest(t, "next", 1, "independent later release"), "d5-next-default")
	if activation.Code != http.StatusOK {
		t.Fatalf("activation %d %s", activation.Code, activation.Body.String())
	}
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
		t.Fatal(err)
	}
	if fixture.fabric.replacementInput.ReplacementImageDigest != d5TargetImage || fixture.fabric.replacementInput.PreviousImageDigest != d5OriginalImage {
		t.Fatalf("target drifted: %#v", fixture.fabric.replacementInput)
	}
	policy, _, _, err := fixture.handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.ActiveImage != d5NextImage {
		t.Fatalf("replacement changed default: %#v %v", policy, err)
	}
	// A rollback of one Workspace also leaves the purchase default untouched.
	fixture.fabric.runtimeStatus = fixture.fabric.replacementRuntime
	fixture.fabric.replacementRuntime = clientsWorkspaceRuntimeForReleaseTest(d5OriginalImage)
	rolledBack := fixture.replace(t, "d5-fixed-rollback", d5OriginalImage)
	if rolledBack.Code != http.StatusAccepted {
		t.Fatalf("rollback %d %s", rolledBack.Code, rolledBack.Body.String())
	}
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
		t.Fatal(err)
	}
	policy, _, _, err = fixture.handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.ActiveImage != d5NextImage || fixture.fabric.replacementInput.ReplacementImageDigest != d5OriginalImage {
		t.Fatalf("rollback changed default or target: %#v %v", policy, err)
	}
}

func TestD5UnapprovedTargetCannotMutateWorkspaceOrDefault(t *testing.T) {
	fixture := newD5ImageFixture(t)
	unapproved := "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
	for _, target := range []string{unapproved, "registry.example/workspace:latest"} {
		response := fixture.replace(t, "d5-unapproved", target)
		if response.Code != http.StatusConflict && response.Code != http.StatusBadRequest {
			t.Fatalf("target %s: %d %s", target, response.Code, response.Body.String())
		}
		preview := requestWithSession(t, fixture.handler, fixture.operator, http.MethodGet, "/api/operator/workspaces/ws-alpha/runtime-image-replacements/preview?replacementImageDigest="+url.QueryEscape(target), "")
		if preview.Code != http.StatusConflict {
			t.Fatalf("unapproved preview: %d %s", preview.Code, preview.Body.String())
		}
	}
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
		t.Fatal(err)
	}
	if fixture.fabric.replacementCalls.Load() != 0 {
		t.Fatal("unapproved target dispatched")
	}
	policy, _, _, err := fixture.handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.ActiveImage != d5OriginalImage {
		t.Fatalf("default mutated: %#v %v", policy, err)
	}
}

func TestD5QueuedReplacementDoesNotRestartIneligibleWorkspace(t *testing.T) {
	for _, state := range []string{"suspended", "expired", "deleting", "deleted"} {
		t.Run(state, func(t *testing.T) {
			fixture := newD5ImageFixture(t)
			const key = "d5-state-race"
			created := fixture.replace(t, key, d5TargetImage)
			if created.Code != http.StatusAccepted {
				t.Fatalf("create %d %s", created.Code, created.Body.String())
			}
			workspace, _, _ := fixture.store.GetWorkspace(context.Background(), "ws-alpha")
			switch state {
			case "suspended":
				workspace["state"], workspace["status"] = "suspended", "suspended"
			case "expired":
				deadline := time.Now().Add(-time.Second).UTC()
				workspace["periodStart"], workspace["paidThrough"] = deadline.AddDate(0, -1, 0).Format(time.RFC3339), deadline.Format(time.RFC3339)
				workspace["nextRenewalAt"] = deadline.Add(-monthlyRenewalLead).Format(time.RFC3339)
			case "deleting":
				operation := workspaceDeleteOperation{SchemaVersion: 2, OperationID: workspaceDeleteOperationID("ws-alpha"), WorkspaceID: "ws-alpha", AccountID: "acct-alpha", Status: "started", Phase: "runtime"}
				// Invalid deletion state must also block image mutation, never authorize it.
				mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), workspaceDeleteOperationRow(operation)))
			case "deleted":
				fixture.store.mu.Lock()
				delete(fixture.store.workspaces, "ws-alpha")
				fixture.store.mu.Unlock()
			}
			if state != "deleted" {
				mustStore(t, fixture.store.SaveWorkspace(context.Background(), workspace))
			}
			if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err == nil {
				t.Fatal("ineligible update succeeded")
			}
			if fixture.fabric.replacementCalls.Load() != 0 {
				t.Fatal("ineligible update reached provider")
			}
			replayed := fixture.replace(t, key, d5TargetImage)
			if replayed.Code != http.StatusAccepted {
				t.Fatalf("lost original operation after lifecycle change: %d %s", replayed.Code, replayed.Body.String())
			}
		})
	}
}

func TestD5ReplacementRetriesSameOperationAndPersistsTerminalAudit(t *testing.T) {
	fixture := newD5ImageFixture(t)
	const key = "d5-retry"
	created := fixture.replace(t, key, d5TargetImage)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	fixture.fabric.replacementErr = errors.New("temporary provider readback failure")
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err == nil {
		t.Fatal("expected retryable failure")
	}
	id := workspaceRuntimeImageReplacementOperationID("ws-alpha", key)
	row, _, _ := fixture.store.GetRuntimeOperation(context.Background(), id)
	if row["status"] != "started" {
		t.Fatalf("retry lost operation: %#v", row)
	}
	fixture.fabric.replacementErr = nil
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
		t.Fatal(err)
	}
	row, _, _ = fixture.store.GetRuntimeOperation(context.Background(), id)
	if row["status"] != "succeeded" || row["errorCode"] != "" || fixture.fabric.replacementInput.IdempotencyKey != id {
		t.Fatalf("retry did not converge original: %#v", row)
	}
	events, err := fixture.store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil {
		t.Fatal(err)
	}
	started, succeeded := 0, 0
	for _, event := range events {
		if event["action"] != workspaceRuntimeImageReplacementAction {
			continue
		}
		if event["result"] == "started" {
			started++
		}
		if event["result"] == "succeeded" {
			succeeded++
		}
	}
	if started != 1 || succeeded != 1 {
		t.Fatalf("audit evidence: started=%d succeeded=%d", started, succeeded)
	}
}

func TestD5ReplacementLaunchLookupIsWorkspaceScoped(t *testing.T) {
	fixture := newD5ImageFixture(t)
	store := &workspaceRenewalQueryStore{memoryTableStore: fixture.store}
	launch, err := successfulWorkspaceLaunchForReplacement(context.Background(), store, "ws-alpha")
	if err != nil || launch.ID != fixture.launch.ID {
		t.Fatalf("launch lookup: %#v %v", launch, err)
	}
	if len(store.queries) != 1 || store.queries[0].WorkspaceID != "ws-alpha" || store.queries[0].Action != workspaceLaunchAction || len(store.queries[0].Statuses) != 1 || store.queries[0].Statuses[0] != "succeeded" {
		t.Fatalf("unbounded launch lookup: %#v", store.queries)
	}
}

type d5UnavailableImageWorkspaceStore struct{ *memoryTableStore }

func (store *d5UnavailableImageWorkspaceStore) GetWorkspace(context.Context, string) (map[string]any, bool, error) {
	return nil, false, errors.New("temporary database read failure")
}

func TestD5ReplacementRetriesBeforeFirstFabricWrite(t *testing.T) {
	fixture := newD5ImageFixture(t)
	const key = "d5-prewrite-read"
	created := fixture.replace(t, key, d5TargetImage)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	fixture.handler.app.tables = &d5UnavailableImageWorkspaceStore{fixture.store}
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err == nil {
		t.Fatal("expected read failure")
	}
	if fixture.fabric.replacementCalls.Load() != 0 {
		t.Fatal("failed prewrite read reached Fabric")
	}
	fixture.handler.app.tables = fixture.store
	if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
		t.Fatal(err)
	}
	if fixture.fabric.replacementCalls.Load() != 1 || fixture.fabric.replacementInput.IdempotencyKey != workspaceRuntimeImageReplacementOperationID("ws-alpha", key) {
		t.Fatal("prewrite retry lost original identity")
	}
}

func TestD5ReplacementAfterRenewalRequiresCommittedPaymentAndKeepsCurrentKey(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed", false: "unconfirmed"}[confirmed], func(t *testing.T) {
			fixture := newD5ImageFixture(t)
			workspace, _, _ := fixture.store.GetWorkspace(context.Background(), "ws-alpha")
			account, user := provisionedAccountRowsFor("acct-alpha", "usr-alpha", "owner@example.invalid", 41)
			mustStore(t, fixture.store.SaveAccount(context.Background(), account))
			mustStore(t, fixture.store.SaveUser(context.Background(), user))
			renewal, err := newWorkspaceRenewalOperation(workspace, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			renewal.Status, renewal.Phase, renewal.EntitlementCommitted = "active", "complete", true
			if confirmed {
				renewal.ChargeConfirmation = map[string]any{"code": renewal.RedeemCode, "userId": int64(41), "chargeUsdMicros": renewal.TotalUSDMicros, "status": "used"}
			}
			mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(renewal)))
			workspace["periodStart"], workspace["paidThrough"] = renewal.PaidThrough, renewal.RenewedThrough
			deadline, _ := time.Parse(time.RFC3339, renewal.RenewedThrough)
			workspace["nextRenewalAt"] = deadline.Add(-monthlyRenewalLead).Format(time.RFC3339)
			workspace["workspaceApiKeyId"] = int64(999) // a current rotated Key is independent of image identity
			mustStore(t, fixture.store.SaveWorkspace(context.Background(), workspace))
			created := fixture.replace(t, "d5-renewed-workspace", d5TargetImage)
			if !confirmed {
				if created.Code != http.StatusConflict || fixture.fabric.replacementCalls.Load() != 0 {
					t.Fatalf("unconfirmed renewal authorized update: %d %s", created.Code, created.Body.String())
				}
				return
			}
			if created.Code != http.StatusAccepted {
				t.Fatalf("confirmed renewal rejected: %d %s", created.Code, created.Body.String())
			}
			if err := fixture.handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), fixture.handler.service); err != nil {
				t.Fatal(err)
			}
			after, _, _ := fixture.store.GetWorkspace(context.Background(), "ws-alpha")
			if after["workspaceApiKeyId"] != int64(999) || after["paidThrough"] != renewal.RenewedThrough || fixture.fabric.replacementCalls.Load() != 1 {
				t.Fatalf("update changed paid period or key: %#v", after)
			}
		})
	}
}
