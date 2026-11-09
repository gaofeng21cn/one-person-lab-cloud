package identity_test

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

const agentAudience = "opl-cloud-mcp"

func issueAgentCredential(t *testing.T, client api.TenantProductServiceClient, call *api.CallContext) *api.IssuedAgentCredential {
	t.Helper()
	issued, err := client.IssueAgentCredential(t.Context(), &api.IssueAgentCredentialRpcRequest{Context: call, Label: "Isolated Agent Host"})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func resolveAgentCredential(t *testing.T, client api.TenantProductServiceClient, issued *api.IssuedAgentCredential) *api.ResolvedAgentCredential {
	t.Helper()
	resolved, err := client.ResolveAgentCredential(t.Context(), &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func agentReadRequest(ref string, owner api.OwnerEnum, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind, id string) *api.AuthorizationRequest {
	resource := &api.AuthorizationResource{Kind: kind}
	if id != "" {
		resource.Id = proto.String(id)
	}
	return &api.AuthorizationRequest{
		Scope:   &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}},
		ActorId: "101", SessionId: proto.String(ref), AudienceOwner: owner, Action: action,
		Resource: resource, RequestId: "agent-read",
	}
}

func TestAgentCredentialIssueResolveAndRevokePostgres(t *testing.T) {
	s, db, client, _ := system(t)
	raw, session := login(t, client)
	call := memberCall(session, raw, "agent-issue")
	before := time.Now()
	issued := issueAgentCredential(t, client, call)
	metadata := issued.GetMetadata()
	if issued.GetCredential() == "" || issued.GetCredential() == raw || metadata.GetId() == "" || metadata.GetAudience() != agentAudience || metadata.GetTenantId() != session.GetTenantId() {
		t.Fatal("credential did not preserve its own identity and fixed audience")
	}
	if !metadata.GetIssuedAt().AsTime().After(before.Add(-time.Second)) || metadata.GetExpiresAt().AsTime().After(before.Add(time.Hour+time.Second)) || !metadata.GetExpiresAt().AsTime().After(before.Add(59*time.Minute)) {
		t.Fatal("credential lifetime is not bounded to one hour")
	}
	resolved := resolveAgentCredential(t, client, issued)
	if resolved.GetSessionReference() == call.GetSessionId() || resolved.GetSessionReference() != "agent:"+metadata.GetId() || resolved.GetSession().GetActorId() != session.GetActorId() || resolved.GetSession().GetTenantId() != session.GetTenantId() || resolved.GetSession().GetCsrfToken() != "" {
		t.Fatal("programmatic caller did not receive a separate non-browser reference")
	}
	if !proto.Equal(resolved.GetMetadata(), metadata) {
		t.Fatal("resolution changed credential metadata")
	}
	listed, err := client.ListAgentCredentials(t.Context(), &api.ListAgentCredentialsRpcRequest{Context: call})
	if err != nil || len(listed.GetItems()) != 1 || !proto.Equal(listed.GetItems()[0], metadata) {
		t.Fatal("credential list lost the issued metadata", err)
	}
	var stored string
	if err = db.QueryRowContext(t.Context(), `SELECT (SELECT COALESCE(string_agg(row_to_json(s)::text,' '),'') FROM tenant.sessions s)||(SELECT COALESCE(string_agg(row_to_json(a)::text,' '),'') FROM tenant.agent_credentials a)||(SELECT COALESCE(string_agg(row_to_json(e)::text,' '),'') FROM tenant.audit_events e)`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{raw, issued.GetCredential(), "private-gateway-token", "private-directory-token", "password"} {
		if strings.Contains(stored, secret) || strings.Contains(listed.String(), secret) || strings.Contains(resolved.String(), secret) {
			t.Fatal("a secret escaped the one-time credential response")
		}
	}
	for _, credential := range []string{issued.GetCredential(), resolved.GetSessionReference()} {
		if _, err = client.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &credential}}); err == nil {
			t.Fatal("agent secret or internal reference became a browser cookie")
		}
	}
	// These legacy owner RPCs resolve sessions directly rather than calling
	// AuthorizeAction. The agent reference must fail at that session boundary,
	// before an unavailable synthetic Gateway could hide a leaked permission.
	agentCall := proto.Clone(call).(*api.CallContext)
	agentCall.SessionId = proto.String(resolved.GetSessionReference())
	peerCtx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
	for name, invoke := range map[string]func() error{
		"wallet": func() error { _, err := s.GetWallet(peerCtx, &api.GetWalletRpcRequest{Context: agentCall}); return err },
		"usage-summary": func() error {
			_, err := s.GetUsageSummary(peerCtx, &api.GetUsageSummaryRpcRequest{Context: agentCall, Period: "day"})
			return err
		},
		"logout": func() error { _, err := s.Logout(peerCtx, &api.LogoutRpcRequest{Context: agentCall}); return err },
	} {
		t.Run("direct-session-"+name, func(t *testing.T) {
			code := status.Code(invoke())
			if code != codes.PermissionDenied && code != codes.Unauthenticated {
				t.Fatalf("direct session RPC did not reject agent authority: %s", code)
			}
		})
	}
	resolveAgentCredential(t, client, issued)
	for _, request := range []*api.ResolveAgentCredentialRequest{
		{Credential: issued.GetCredential(), Audience: "another-host"},
		{Credential: "unknown-agent-secret", Audience: agentAudience},
		{Credential: raw, Audience: agentAudience},
		{Credential: resolved.GetSessionReference(), Audience: agentAudience},
	} {
		if _, err = client.ResolveAgentCredential(t.Context(), request); err == nil {
			t.Fatal("resolver accepted a different audience or non-credential")
		}
	}
	if _, err = client.RevokeAgentCredential(t.Context(), &api.RevokeAgentCredentialRpcRequest{Context: call, CredentialId: metadata.GetId()}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ResolveAgentCredential(t.Context(), &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience}); err == nil {
		t.Fatal("revoked credential still resolved")
	}
	if _, err = client.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &raw}}); err != nil {
		t.Fatal("credential revocation revoked the browser session", err)
	}
}

func TestAgentCredentialNeverExceedsParentExpiryPostgres(t *testing.T) {
	_, db, client, _ := system(t)
	raw, session := login(t, client)
	call := memberCall(session, raw, "short-parent")
	var parentExpiry time.Time
	if err := db.QueryRowContext(t.Context(), `UPDATE tenant.sessions SET expires_at=now()+interval '15 minutes' WHERE id=$1 RETURNING expires_at`, call.GetSessionId()).Scan(&parentExpiry); err != nil {
		t.Fatal(err)
	}
	issued := issueAgentCredential(t, client, call)
	if issued.GetMetadata().GetExpiresAt().AsTime().After(parentExpiry) || parentExpiry.Sub(issued.GetMetadata().GetExpiresAt().AsTime()) > time.Second {
		t.Fatal("credential did not inherit the earlier parent expiry")
	}
}

func TestAgentCredentialManagementRequiresItsBrowserActorPostgres(t *testing.T) {
	s, client, _ := bindingSystem(t)
	first := bindingUserLogin(t, client, "testcloud4@medopl.com")
	second := bindingUserLogin(t, client, "other@example.test")
	issued := issueAgentCredential(t, client, first)
	resolved := resolveAgentCredential(t, client, issued)
	listed, err := client.ListAgentCredentials(t.Context(), &api.ListAgentCredentialsRpcRequest{Context: second})
	if err != nil || len(listed.GetItems()) != 0 {
		t.Fatal("another actor saw the credential", err)
	}
	if _, err = client.RevokeAgentCredential(t.Context(), &api.RevokeAgentCredentialRpcRequest{Context: second, CredentialId: issued.GetMetadata().GetId()}); err == nil {
		t.Fatal("another actor revoked the credential")
	}
	resolveAgentCredential(t, client, issued)
	agentCall := proto.Clone(first).(*api.CallContext)
	agentCall.SessionId = proto.String(resolved.GetSessionReference())
	for name, caller := range map[string]*api.CallContext{
		"agent":           agentCall,
		"forged-actor":    {ActorId: second.ActorId, Scope: first.Scope, SessionId: first.SessionId, RequestId: "forged-actor"},
		"missing-context": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.IssueAgentCredential(t.Context(), &api.IssueAgentCredentialRpcRequest{Context: caller, Label: "Rejected"}); err == nil {
				t.Fatal("non-browser actor issued a credential")
			}
			if _, err := client.ListAgentCredentials(t.Context(), &api.ListAgentCredentialsRpcRequest{Context: caller}); err == nil {
				t.Fatal("non-browser actor listed credentials")
			}
			if _, err := client.RevokeAgentCredential(t.Context(), &api.RevokeAgentCredentialRpcRequest{Context: caller, CredentialId: issued.GetMetadata().GetId()}); err == nil {
				t.Fatal("non-browser actor revoked a credential")
			}
		})
	}
	wrongPeer := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	if _, err = s.ResolveAgentCredential(wrongPeer, &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience}); err == nil {
		t.Fatal("another service resolved a secret as the Console BFF")
	}
	if _, err = s.IssueAgentCredential(wrongPeer, &api.IssueAgentCredentialRpcRequest{Context: first, Label: "Wrong peer"}); err == nil {
		t.Fatal("another service issued a credential")
	}
}

func TestAgentCredentialRechecksLiveAuthorityPostgres(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *sql.DB, api.TenantProductServiceClient, *api.CallContext, *api.IssuedAgentCredential, *gatewayFixture)
	}{
		{"parent-logout", func(t *testing.T, _ *sql.DB, c api.TenantProductServiceClient, call *api.CallContext, _ *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := c.Logout(t.Context(), &api.LogoutRpcRequest{Context: call}); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent-expired", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, call *api.CallContext, _ *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `UPDATE tenant.sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, call.GetSessionId()); err != nil {
				t.Fatal(err)
			}
		}},
		{"credential-expired", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, issued *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `UPDATE tenant.agent_credentials SET issued_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE id=$1`, issued.GetMetadata().GetId()); err != nil {
				t.Fatal(err)
			}
		}},
		{"membership-revoked", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, _ *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"tenant-suspended", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, _ *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenants SET status='suspended' WHERE id='tenant-test'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"gateway-disabled", func(_ *testing.T, _ *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, _ *api.IssuedAgentCredential, f *gatewayFixture) {
			f.setStatus("disabled")
		}},
		{"tenant-reassigned", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, _ *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenants(id,name) VALUES('tenant-other','Other'); UPDATE tenant.tenant_members SET tenant_id='tenant-other' WHERE actor_id='101'; UPDATE tenant.sessions SET tenant_id='tenant-other' WHERE actor_id='101'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"credential-actor-tampered", func(t *testing.T, db *sql.DB, _ api.TenantProductServiceClient, _ *api.CallContext, issued *api.IssuedAgentCredential, _ *gatewayFixture) {
			if _, err := db.ExecContext(t.Context(), `UPDATE tenant.agent_credentials SET actor_id='999' WHERE id=$1`, issued.GetMetadata().GetId()); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, db, client, _, fixture := systemWithFixture(t)
			raw, session := login(t, client)
			call := memberCall(session, raw, "live-parent")
			issued := issueAgentCredential(t, client, call)
			resolved := resolveAgentCredential(t, client, issued)
			request := agentReadRequest(resolved.GetSessionReference(), api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "")
			ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
			decision, err := s.AuthorizeAction(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(t, db, client, call, issued, fixture)
			if _, err = client.ResolveAgentCredential(t.Context(), &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience}); err == nil {
				t.Fatal("credential retained authority after its live parent changed")
			}
			if _, err = s.AuthorizeAction(ctx, request); err == nil {
				t.Fatal("cached reference bypassed fresh credential authority")
			}
			if _, err = s.GetAuthorizationContext(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), &api.GetAuthorizationContextRequest{AuthorizationContextId: decision.GetAuthorizationContextId(), ExpectedAudienceOwner: request.AudienceOwner, ExpectedAction: request.Action, ExpectedResource: request.Resource, RequestId: "read-existing-decision"}); err == nil {
				t.Fatal("existing owner decision bypassed live credential revocation")
			}
		})
	}
}

func TestAgentCredentialReadClosureAndCurrentPermissionsPostgres(t *testing.T) {
	s, db, client, _ := system(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='owner' WHERE actor_id='101'; UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id='tenant-test'`); err != nil {
		t.Fatal(err)
	}
	raw, session := login(t, client)
	issued := issueAgentCredential(t, client, memberCall(session, raw, "read-closure"))
	ref := resolveAgentCredential(t, client, issued).GetSessionReference()
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
	reads := []struct {
		owner  api.OwnerEnum
		action api.AuthorizationActionEnum
		kind   api.AuthorizationResourceKind
		id     string
	}{
		{api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""},
		{api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTSTORAGEPLANS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""},
		{api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""},
		{api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, ""},
		{api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"},
		{api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, "op-1"},
		{api.OwnerEnum_OWNER_ENUM_SERVE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"},
		{api.OwnerEnum_OWNER_ENUM_SERVE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"},
	}
	for _, read := range reads {
		t.Run(read.action.String(), func(t *testing.T) {
			request := agentReadRequest(ref, read.owner, read.action, read.kind, read.id)
			decision, err := s.AuthorizeAction(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			owner := owneridentity.Service(strings.ToLower(strings.TrimPrefix(read.owner.String(), "OWNER_ENUM_")))
			if _, err = s.GetAuthorizationContext(ownerservice.WithPeerOwner(t.Context(), owner), &api.GetAuthorizationContextRequest{AuthorizationContextId: decision.GetAuthorizationContextId(), ExpectedAudienceOwner: read.owner, ExpectedAction: read.action, ExpectedResource: request.Resource, RequestId: "owner-revalidation"}); err != nil {
				t.Fatal("owner could not revalidate the exact permitted read", err)
			}
			for name, mutate := range map[string]func(*api.AuthorizationRequest){
				"wrong-audience": func(r *api.AuthorizationRequest) { r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_BUILD },
				"wrong-resource-kind": func(r *api.AuthorizationRequest) {
					r.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT
				},
				"wrong-resource-shape": func(r *api.AuthorizationRequest) {
					if read.id == "" {
						r.Resource.Id = proto.String("unexpected")
					} else {
						r.Resource.Id = nil
					}
				},
				"another-actor": func(r *api.AuthorizationRequest) { r.ActorId = "999" },
				"another-tenant": func(r *api.AuthorizationRequest) {
					r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-other"}}}
				},
			} {
				t.Run(name, func(t *testing.T) {
					wrong := proto.Clone(request).(*api.AuthorizationRequest)
					mutate(wrong)
					if _, err := s.AuthorizeAction(ctx, wrong); err == nil {
						t.Fatal("read authority escaped its exact scope")
					}
				})
			}
		})
	}
	// Each excluded action is otherwise authorized for the browser actor. A
	// rejection therefore proves the credential's limit rather than an unrelated
	// owner, role or resource mismatch.
	blocked := []*api.AuthorizationRequest{
		agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEQUOTE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""),
		agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, ""),
		agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
		agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_GATEWAY, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "tenant-test"),
		agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
	}
	for _, request := range blocked {
		browserRequest := proto.Clone(request).(*api.AuthorizationRequest)
		browserRequest.SessionId = proto.String(owneridentity.SessionReference(raw))
		if _, err := s.AuthorizeAction(ctx, browserRequest); err != nil {
			t.Fatalf("excluded action %s was not otherwise authorized for the browser actor: %v", request.GetAction(), err)
		}
		if _, err := s.AuthorizeAction(ctx, request); err == nil {
			t.Fatal("agent credential acquired an action outside its fixed read surface")
		}
	}
	quote := agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "")
	if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='member' WHERE actor_id='101'; UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id='tenant-test'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeAction(ctx, quote); err == nil {
		t.Fatal("credential retained a role removed after issuance")
	}
	if _, err := s.AuthorizeAction(ctx, agentReadRequest(ref, api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "")); err != nil {
		t.Fatal("current member read was lost with the admin permission", err)
	}
}

func TestAgentCredentialResolvesAcrossReplicasPostgres(t *testing.T) {
	s, db, client, _, fixture := systemWithFixture(t)
	raw, session := login(t, client)
	call := memberCall(session, raw, "replica-agent")
	issued := issueAgentCredential(t, client, call)
	gateway, err := identity.NewGatewayWithDirectory(fixture.url, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	replica, err := identity.New(db, gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	replica.GatewayStore = s.GatewayStore
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	before := fixture.subjectReads()
	resolved, err := replica.ResolveAgentCredential(ctx, &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience})
	if err != nil || resolved.GetSession().GetActorId() != session.GetActorId() || fixture.subjectReads() <= before {
		t.Fatal("replica did not use persisted credential plus current identity", err)
	}
	if _, err = client.RevokeAgentCredential(t.Context(), &api.RevokeAgentCredentialRpcRequest{Context: call, CredentialId: issued.GetMetadata().GetId()}); err != nil {
		t.Fatal(err)
	}
	if _, err = replica.ResolveAgentCredential(ctx, &api.ResolveAgentCredentialRequest{Credential: issued.GetCredential(), Audience: agentAudience}); err == nil {
		t.Fatal("replica accepted a revoked credential")
	}
}
