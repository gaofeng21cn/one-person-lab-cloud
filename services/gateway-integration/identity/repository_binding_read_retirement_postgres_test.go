package identity_test

import (
	"database/sql"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
)

// The runtime fallback read of the frozen Tenant history is retired. The
// Gateway owner holds the only live destination mapping, and the one-time
// handover moved the attributable historical destinations into it. These tests
// prove that the owner's answer no longer depends on the retired table, and
// that a destination the owner holds cannot be taken by another actor.

// TestUserExternalDestinationResolvesFromOwnerAfterHandover proves the
// resolution path reads the owner alone. The historical row is removed after
// the handover and the owner's answer does not change.
func TestUserExternalDestinationResolvesFromOwnerAfterHandover(t *testing.T) {
	s, tenant, address, owner := bindingSystemWithOwnerHistory(t, func(db *sql.DB) {
		seedHistoricalDestination(t, db, []string{"201"}, "old-approved-path", "reserved")
	})
	summary, err := s.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Moved != 1 || summary.Unattributable != 0 || summary.Conflicting != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	if got := selfBinding(t, console, call).Repository; got != "old-approved-path" {
		t.Fatalf("handover destination lost: %s", got)
	}
	// The frozen history is provenance from here on. Removing the row changes
	// nothing about the owner's answer, which is the proof that the runtime
	// resolution path no longer reads the retired table.
	if _, err := owner.ExecContext(t.Context(), `DELETE FROM tenant.tenant_repository_bindings WHERE tenant_id='tenant-history'`); err != nil {
		t.Fatal(err)
	}
	renamed := bindingUserLogin(t, tenant, "renamed@medopl.com")
	if got := selfBinding(t, console, renamed).Repository; got != "old-approved-path" {
		t.Fatalf("destination after the history was removed=%s", got)
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_repository_bindings`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("the history was not removed: %d %v", count, err)
	}
}

// TestUserExternalDestinationResolvesFromTheOwnerOnly proves the retired
// fallback read is gone: a historical reservation for the actor's own tenant
// is not an input to the answer. The destination comes from the owner, and the
// owner holds one only after the handover moved it there.
func TestUserExternalDestinationResolvesFromTheOwnerOnly(t *testing.T) {
	s, tenant, address := bindingSystemWithHistory(t, func(db *sql.DB) {
		seedHistoricalDestination(t, db, []string{"201"}, "old-approved-path", "reserved")
	})
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	// The handover has not run, so the owner holds no destination for this
	// actor. The historical reservation names one, and the answer ignores it:
	// the candidate the verified email derives is what the owner is given.
	call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	if got := selfBinding(t, console, call).Repository; got != "testcloud4" {
		t.Fatalf("the retired table was read: %s", got)
	}
	// The historical row is untouched and still names its destination.
	var preserved string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT repository FROM tenant.tenant_repository_bindings WHERE tenant_id='tenant-history'`).Scan(&preserved); err != nil || preserved != "old-approved-path" {
		t.Fatalf("history=%q err=%v", preserved, err)
	}
}

// TestUserExternalDestinationDerivesCandidateForNewActor proves that an actor
// without a historical destination still receives the candidate the verified
// email derives, on the installation registry.
func TestUserExternalDestinationDerivesCandidateForNewActor(t *testing.T) {
	s, tenant, address := bindingSystem(t)
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	for _, tc := range []struct{ email, actor, repository string }{
		{"testcloud4@medopl.com", "201", "testcloud4"},
		{"other@example.test", "202", "other"},
	} {
		call := bindingUserLogin(t, tenant, tc.email)
		if call.ActorId != tc.actor {
			t.Fatalf("actor=%s want %s", call.ActorId, tc.actor)
		}
		if got := selfBinding(t, console, call); got.Repository != tc.repository || got.RegistryHost != "uswccr.ccs.tencentyun.com" || got.RegistryNamespace != "oplcloud" {
			t.Fatalf("binding=%v want repository %s", got, tc.repository)
		}
	}
	// The candidate is derived from the verified email, never from the history.
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_repository_bindings`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("the runtime wrote a historical binding: %d %v", count, err)
	}
}

// TestRepositoryBindingHandoverResidualRefusesService proves the startup gate
// decision: a clean handover lets the owner serve, and every residual row
// refuses it with an actionable message.
func TestRepositoryBindingHandoverResidualRefusesService(t *testing.T) {
	clean := identity.RepositoryBindingHandoverSummary{Scanned: 3, Moved: 2, Already: 1, Released: 0}
	if err := clean.ResidualError(); err != nil {
		t.Fatalf("a clean handover was refused: %v", err)
	}
	for _, tc := range []struct {
		name    string
		summary identity.RepositoryBindingHandoverSummary
		ref     string
	}{
		{
			name:    "unattributable",
			summary: identity.RepositoryBindingHandoverSummary{Scanned: 1, Unattributable: 1, UnattributableRefs: []string{"tenant=tenant-a destination=uswccr.ccs.tencentyun.com/oplcloud/old-path"}},
			ref:     "tenant=tenant-a",
		},
		{
			name:    "conflicting",
			summary: identity.RepositoryBindingHandoverSummary{Scanned: 1, Conflicting: 1, ConflictingRefs: []string{"tenant=tenant-b destination=uswccr.ccs.tencentyun.com/oplcloud/old-path reason=held_different_destination"}},
			ref:     "tenant=tenant-b",
		},
		{
			name:    "failed",
			summary: identity.RepositoryBindingHandoverSummary{Scanned: 1, Failed: 1, FailedRefs: []string{"tenant=tenant-c destination=uswccr.ccs.tencentyun.com/oplcloud/old-path error=unavailable"}},
			ref:     "tenant=tenant-c",
		},
		{
			name:    "released without a holder",
			summary: identity.RepositoryBindingHandoverSummary{Scanned: 1, Released: 1, ReleasedUnheld: 1, ReleasedUnheldRefs: []string{"tenant=tenant-d registered_owners=1 destination=uswccr.ccs.tencentyun.com/oplcloud/old-path reason=destination_unheld"}},
			ref:     "tenant=tenant-d",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			residual := tc.summary.ResidualError()
			if residual == nil {
				t.Fatal("a residual handover was allowed to serve")
			}
			message := residual.Error()
			if !strings.Contains(message, tc.ref) {
				t.Fatalf("gate message=%q", message)
			}
			if !strings.Contains(message, "unowned") {
				t.Fatalf("gate message=%q", message)
			}
		})
	}
	// The message names how many rows need an operator and shows the first
	// ones, so the refusal is actionable without the log.
	many := identity.RepositoryBindingHandoverSummary{Unattributable: 7, UnattributableRefs: []string{"tenant=one", "tenant=two", "tenant=three", "tenant=four", "tenant=five", "tenant=six", "tenant=seven"}}
	manyResidual := many.ResidualError()
	if manyResidual == nil {
		t.Fatal("a residual handover was allowed to serve")
	}
	message := manyResidual.Error()
	if !strings.Contains(message, "7") {
		t.Fatalf("gate message=%q", message)
	}
	for _, ref := range []string{"tenant=one", "tenant=five"} {
		if !strings.Contains(message, ref) {
			t.Fatalf("gate message=%q", message)
		}
	}
	if strings.Contains(message, "tenant=six") {
		t.Fatalf("the gate message named every row: %q", message)
	}
	if !strings.Contains(message, "+2 more") {
		t.Fatalf("gate message=%q", message)
	}
}

// TestRepositoryBindingHandoverUnattributableHistoryBlocksTheOwner proves the
// gate over the real handover: a historical destination nobody owns is a
// destination the owner does not hold, so the service must not serve.
func TestRepositoryBindingHandoverUnattributableHistoryBlocksTheOwner(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-gate-unattributable", TenantStatus: "active", Owners: []string{"201", "202"}, Repository: "shared-path", Status: "active"},
	}, nil)

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Unattributable != 1 || summary.Moved != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	residual := summary.ResidualError()
	if residual == nil {
		t.Fatal("an unattributable destination was allowed to serve")
	}
	if !strings.Contains(residual.Error(), "tenant-gate-unattributable") {
		t.Fatalf("gate message=%q", residual.Error())
	}
	// No actor is invented for an ambiguous owner, so nothing is held and
	// nothing is written.
	if count := mappingCount(t, system.gateway); count != 0 {
		t.Fatalf("unattributable history wrote %d mapping rows", count)
	}
}

// TestRepositoryBindingHandoverCleanHistoryLetsTheOwnerServe proves the other
// side of the gate over the real handover.
func TestRepositoryBindingHandoverCleanHistoryLetsTheOwnerServe(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-gate-clean", TenantStatus: "active", Owners: []string{"201"}, Repository: "clean-path", Status: "reserved"},
	}, nil)

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Moved != 1 || summary.ResidualError() != nil {
		t.Fatalf("summary=%+v residual=%v", summary, summary.ResidualError())
	}
}

// TestRepositoryBindingHandoverReleasedUnheldDestinationBlocksTheOwner proves
// the uniform rule over a released historical row: the row still records a
// destination, and when the owner does not hold it, that destination is one any
// new actor could take. The gate refuses to serve and names the row.
func TestRepositoryBindingHandoverReleasedUnheldDestinationBlocksTheOwner(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-gate-released", TenantStatus: "active", Owners: []string{"201"}, Repository: "released-path", Status: "retired"},
	}, nil)

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The row stays a release in the receipt and is a residual as well.
	if summary.Released != 1 || summary.ReleasedUnheld != 1 || summary.Moved != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	residual := summary.ResidualError()
	if residual == nil {
		t.Fatal("a released destination nobody holds was allowed to serve")
	}
	message := residual.Error()
	if !strings.Contains(message, "tenant=tenant-gate-released") {
		t.Fatalf("gate message=%q", message)
	}
	if !strings.Contains(message, "released-path") {
		t.Fatalf("gate message=%q", message)
	}
	if !strings.Contains(message, "reason=destination_unheld") {
		t.Fatalf("gate message=%q", message)
	}
	// Nothing is written for a released row, and the historical row stays as it
	// was.
	if count := mappingCount(t, system.gateway); count != 0 {
		t.Fatalf("a released history wrote %d mapping rows", count)
	}
}

// TestRepositoryBindingHandoverReleasedHeldDestinationLetsTheOwnerServe proves
// the other side of the uniform rule: a released historical row whose
// destination the owner already holds leaves no unowned destination, so the
// gate allows the service to serve.
func TestRepositoryBindingHandoverReleasedHeldDestinationLetsTheOwnerServe(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-gate-released-held", TenantStatus: "active", Owners: []string{"201"}, Repository: "released-held-path", Status: "retired"},
	}, func(db *sql.DB) {
		// The destination is already held in the Gateway owner, so no new actor
		// can take it and the released row leaves nothing unowned.
		seedMapping(t, db, "201", "uswccr.ccs.tencentyun.com", "oplcloud", "released-held-path", true, true, false)
	})
	mappingBefore := mappingRow(t, system.gateway, "201")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Released != 1 || summary.ReleasedUnheld != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if summary.ResidualError() != nil {
		t.Fatalf("a held destination was refused: %v", summary.ResidualError())
	}
	// The live mapping is untouched.
	if after := mappingRow(t, system.gateway, "201"); after != mappingBefore {
		t.Fatalf("the held mapping changed:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
}

// TestRepositoryBindingHandoverUnverifiedDestinationStaysResidual proves the
// unverified destination stays a residual under the new failure semantics: the
// owner holds the destination but cannot serve it, so the gate refuses.
func TestRepositoryBindingHandoverUnverifiedDestinationStaysResidual(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-gate-unverified", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		seedMapping(t, db, "201", "uswccr.ccs.tencentyun.com", "oplcloud", "historical-path", true, false, false)
	})

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Conflicting != 1 || summary.Moved != 0 || summary.Already != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.ConflictingRefs) != 1 || !strings.Contains(summary.ConflictingRefs[0], "reason=destination_unverified") {
		t.Fatalf("conflicting refs=%v", summary.ConflictingRefs)
	}
	if summary.ResidualError() == nil {
		t.Fatal("an unverified destination was allowed to serve")
	}
}

// TestUserExternalDestinationRejectsSecondActorForHeldDestination proves the
// guarantee that replaces the retired guard: the owner's unique index refuses
// a second actor for a destination the owner already holds, on the login path
// and on the platform-admin command path.
func TestUserExternalDestinationRejectsSecondActorForHeldDestination(t *testing.T) {
	t.Run("login path", func(t *testing.T) {
		s, tenant, address := bindingSystem(t)
		console := bindingClient(t, address, owneridentity.ConsoleBFF)
		first := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
		if got := selfBinding(t, console, first).Repository; got != "testcloud4" {
			t.Fatalf("binding=%s", got)
		}
		// The second actor's candidate names the destination the owner already
		// holds, so the owner refuses the second claim.
		var header metadata.MD
		if _, err := tenant.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
			t.Fatal(err)
		}
		challenge := header.Get(owneridentity.SessionCookieHeader)[0]
		if _, err := tenant.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "testcloud4@other.example", Password: "password"}}); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("the held destination was taken: %v", err)
		}
		var count int
		if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='203'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("the refused login persisted a User destination: %d %v", count, err)
		}
		// The first actor keeps the destination.
		if got := selfBinding(t, console, first).Repository; got != "testcloud4" {
			t.Fatalf("binding=%s", got)
		}
	})
	t.Run("admin command path", func(t *testing.T) {
		s, tenant, address := bindingSystem(t)
		// A login without an installation registry records the identity alone,
		// so the destination is assigned by the platform-admin command.
		s.ConfigureRegistry("", "")
		console := bindingClient(t, address, owneridentity.ConsoleBFF)
		admin := platformAdminCall(t, tenant)
		first := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
		second := bindingUserLogin(t, tenant, "other@example.test")
		s.ConfigureRegistry("uswccr.ccs.tencentyun.com", "oplcloud")
		assigned := &api.UserExternalBindingCommand{Context: admin, ActorId: first.ActorId, Sub2ApiUserId: first.ActorId,
			RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "assigned"}
		if _, err := console.BindUserExternalIdentity(t.Context(), assigned); err != nil {
			t.Fatal(err)
		}
		// The second actor has no destination yet, so the owner's unique index
		// is what refuses the claim.
		duplicate := &api.UserExternalBindingCommand{Context: admin, ActorId: second.ActorId, Sub2ApiUserId: second.ActorId,
			RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "assigned"}
		if _, err := console.BindUserExternalIdentity(t.Context(), duplicate); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("the held destination was taken: %v", err)
		}
		var count int
		if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='202' AND COALESCE(repository,'')<>'assigned'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("the refused actor holds a destination: %d %v", count, err)
		}
		if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='201' AND repository='assigned'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("the first actor lost the destination: %d %v", count, err)
		}
	})
}
