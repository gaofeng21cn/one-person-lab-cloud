package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// allowCatalogFor is allowFor for a catalog read that names no single resource.
// The shared boundary compares the decision against the request it answers, and
// this read carries a kind but no id, so the decision must leave the id unset
// rather than present-and-empty.
func allowCatalogFor(actor, tenant string, audience api.OwnerEnum, action api.AuthorizationActionEnum) *api.AuthorizationDecision {
	decision := allowFor(actor, tenant, audience, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "")
	decision.Resource.Id = nil
	return decision
}

// deniedFor is the CloudIdentity refusal a caller gets for an action it does not
// hold. The publisher route must answer it without ever reaching the owner, so a
// widened permission on the administrator action would be visible here as a 200.
func deniedFor(actor, tenant string, audience api.OwnerEnum, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind) *api.AuthorizationDecision {
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}
	return &api.AuthorizationDecision{
		ActorId: actor, SessionId: ptr(owneridentity.SessionReference("session-1")),
		Scope:             scope,
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED,
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: action, AudienceOwner: audience,
		Resource: &api.AuthorizationResource{Kind: kind},
	}
}

// TestEffectiveBuildPolicyReadIsSeparateFromTheAdministratorPolicyRead proves the
// Console's Build input resolution can read the owner fact it needs without
// inheriting the administrator's policy boundary.
//
// The Publisher session may read the effective build policy — the Runtime Control
// owner's own catalog_policies row that names the default Runtime and the default
// WebUI a new Build freezes — through a member action on /api/v2/catalog/build-policy.
// The administrator's own read and command on /api/v2/admin/catalog/build-policy stay
// platform-only. Because publisherRoute authorizes by action alone and never by URL
// path, these are only separate permissions while they are separate actions, so this
// test pins all three facts: the member read succeeds, the administrator read refuses
// that same member, and the platform administrator still reads it.
func TestEffectiveBuildPolicyReadIsSeparateFromTheAdministratorPolicyRead(t *testing.T) {
	const (
		memberAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETEFFECTIVEBUILDRUNTIMEPOLICY
		adminAction  = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILDRUNTIMEPOLICY
	)
	if memberAction == adminAction {
		t.Fatal("the member effective-policy read must not reuse the administrator's action")
	}

	member := &fakeIdentity{
		session: &api.Session{ActorId: "actor-1", TenantId: ptr("tenant-1"), CsrfToken: "csrf-1"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			memberAction: allowCatalogFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, memberAction),
		},
		decision: deniedFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, adminAction,
			api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG),
	}
	probe := &runtimeControlProbe{}
	handler := NewPublisherHandler(nil, probe, nil, member)

	// A member session reads the effective policy the Publisher needs, in the
	// owner's own spelling, under its own tenant scope.
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/build-policy"))
	if response.Code != http.StatusOK {
		t.Fatalf("member effective policy read status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.effectiveReads != 1 || probe.effectiveRead == nil {
		t.Fatalf("effective policy owner reads=%d", probe.effectiveReads)
	}
	if read := probe.effectiveRead; read.GetContext().GetActorId() != "actor-1" || read.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("effective policy owner read context=%v", read.GetContext())
	}
	if len(member.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(member.requests))
	}
	if authorized := member.requests[0]; authorized.GetAction() != memberAction ||
		authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL ||
		authorized.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG ||
		authorized.GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("member authorization request=%v", authorized)
	}
	for _, want := range []string{`"id":"policy-1"`, `"runtimeVersionId":"runtime-1"`, `"defaultWebuiVersionId":"webui-1"`, `"policyVersion":"policy-v1"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("member response %s missing %s", response.Body.String(), want)
		}
	}

	// The administrator's own policy read refuses that same member before the owner
	// is called: granting the member fact did not widen this boundary.
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/admin/catalog/build-policy"))
	if response.Code != http.StatusForbidden || probe.policyReads != 0 {
		t.Fatalf("member on administrator read status=%d owner reads=%d body=%s", response.Code, probe.policyReads, response.Body.String())
	}

	// The platform administrator still reads the administrator policy read, and
	// only through the owner call that path names.
	administrator := platformIdentity(adminAction)
	administrator.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL
	handler = NewPublisherHandler(nil, probe, nil, administrator)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, adminSessionRequest(http.MethodGet, "/api/v2/admin/catalog/build-policy"))
	if response.Code != http.StatusOK {
		t.Fatalf("administrator policy read status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.policyReads != 1 || probe.readPolicy == nil {
		t.Fatalf("administrator owner reads=%d", probe.policyReads)
	}
	if len(administrator.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(administrator.requests))
	}
	if authorized := administrator.requests[0]; authorized.GetAction() != adminAction ||
		authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL ||
		authorized.GetScope().GetPlatform() == nil {
		t.Fatalf("administrator authorization request=%v", authorized)
	}
}
