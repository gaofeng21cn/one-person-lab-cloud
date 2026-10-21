package identity_test

import (
	"net/http"
	"strings"
	"testing"

	bff "opl-cloud/apps/console-bff"
)

// TestLiveRetainedActivationLeavesTheCustomerTenantless reproduces issue #1004 on
// the retained activation path: an operator activates the customer through the
// Control Plane writer, which creates the account and its Gateway user but no
// Tenant. The customer can then sign in, and every customer page is refused
// because the live session carries no tenant.
//
// This test names no new activation surface on purpose: it is the defect as it
// exists today, so it runs unchanged on the base revision and on the change.
func TestLiveRetainedActivationLeavesTheCustomerTenantless(t *testing.T) {
	system := activationLiveSystem(t)
	// Activation as today: the account and its Gateway user exist, and nothing
	// admits a Tenant for that subject.
	system.sub2api.resolve("customer@example.test")

	cookie, _ := system.login(t, "customer@example.test")
	handler := bff.NewWorkspaceHandler(system.workspace, system.reader)

	workspaces := system.read(handler, cookie, "/api/v2/workspaces?limit=100")
	if workspaces.Code != http.StatusForbidden {
		t.Fatalf("tenant-less customer workspace read status=%d body=%s", workspaces.Code, workspaces.Body.String())
	}
	if strings.Contains(workspaces.Body.String(), `"items"`) {
		t.Fatalf("a refused read returned workspace facts: %s", workspaces.Body.String())
	}
}
