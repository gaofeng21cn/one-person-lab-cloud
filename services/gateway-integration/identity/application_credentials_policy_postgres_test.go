package identity_test

import (
	"database/sql"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

type applicationCredentialsPolicyFixture struct {
	service *identity.Service
	db      *sql.DB
	client  api.TenantProductServiceClient
	reveal  *api.AuthorizationRequest
}

func newApplicationCredentialsPolicyFixture(t *testing.T) applicationCredentialsPolicyFixture {
	t.Helper()
	service, db, client, _ := system(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role=$1 WHERE id=$2`, "owner", "member-test"); err != nil {
		t.Fatal(err)
	}
	cookie, session := login(t, client)
	if session.GetRole() != api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER || session.GetTenantId() != "tenant-test" {
		t.Fatal("fixture requires a real owner login")
	}
	return applicationCredentialsPolicyFixture{service, db, client, &api.AuthorizationRequest{
		Scope:   &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: session.GetTenantId()}}},
		ActorId: session.GetActorId(), SessionId: proto.String(owneridentity.SessionReference(cookie)),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-reveal")},
		RequestId:     "reveal-application-credentials",
	}}
}

func (f applicationCredentialsPolicyFixture) continuation(t *testing.T) (*api.AuthorizationRequest, *api.AuthorizationDecision) {
	t.Helper()
	original, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), f.reveal)
	if err != nil {
		t.Fatalf("canonical owner reveal was refused: %v", err)
	}
	if err := owneridentity.ValidateDecision(f.reveal, original, time.Now()); err != nil {
		t.Fatalf("canonical reveal decision is invalid: %v", err)
	}
	request := proto.Clone(f.reveal).(*api.AuthorizationRequest)
	request.AudienceOwner = api.OwnerEnum_OWNER_ENUM_FABRIC
	request.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES
	request.AuthorizationContextId = proto.String(original.GetAuthorizationContextId())
	request.RequestId = "observe-for-application-credentials"
	return request, original
}

// Exercise the receiving owner adapter over gRPC against the real Identity and
// PostgreSQL. Authorizer must accept an observe decision, not the reveal decision
// merely relabeled ALLOWED: its production ValidateDecision checks the binding.
func TestWorkspaceApplicationCredentialsObserveContinuationPostgres(t *testing.T) {
	f := newApplicationCredentialsPolicyFixture(t)
	request, original := f.continuation(t)
	const token = "isolated-application-credentials-policy-token-0001"
	server, err := ownerservice.NewServer(ownerservice.Config{
		Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.Fabric.Service(): token},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Register(func(g *grpc.Server) { api.RegisterCloudIdentityAuthorizationServer(g, f.service) }); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeOn(listener) }()
	t.Cleanup(server.Stop)
	authorizer, conn, err := ownerservice.AuthorizerFromConfig(ownerservice.Config{
		Owner: owneridentity.Fabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		CloudIdentityAddr: listener.Addr().String(), CloudIdentityToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	call := &api.CallContext{
		Scope: request.Scope, ActorId: request.ActorId, SessionId: request.SessionId,
		AuthorizationContextId: request.GetAuthorizationContextId(), RequestId: request.RequestId,
	}
	if err := authorizer.Authorize(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), call,
		request.Action, request.Resource, ownerservice.ResourceScope{TenantID: "tenant-test"}); err != nil {
		t.Fatalf("real Fabric Authorizer rejected the same-session reveal continuation: %v", err)
	}
	decision, err := api.NewCloudIdentityAuthorizationClient(conn).AuthorizeAction(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := owneridentity.ValidateDecision(request, decision, time.Now()); err != nil {
		t.Fatalf("Fabric decision cannot be consumed: %v", err)
	}
	if decision.GetAuthorizationContextId() == "" || decision.GetAcceptedOperationGrantId() != "" || decision.GetPermissionVersion() != original.GetPermissionVersion() {
		t.Fatal("continuation lost its live-session authority")
	}
}

func TestWorkspaceApplicationCredentialsObserveBoundaryPostgres(t *testing.T) {
	f := newApplicationCredentialsPolicyFixture(t)
	otherCookie, _ := login(t, f.client)
	for _, tc := range []struct {
		name   string
		peer   owneridentity.Service
		change func(*api.AuthorizationRequest)
	}{
		{"bare-owner-observe", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.AuthorizationContextId = nil }},
		{"empty-context", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.AuthorizationContextId = proto.String("") }},
		{"unknown-context", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.AuthorizationContextId = proto.String("auth_unknown-reveal") }},
		{"bff-peer", owneridentity.ConsoleBFF, nil},
		{"serve-peer", owneridentity.Serve.Service(), nil},
		{"other-workspace", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.Resource.Id = proto.String("workspace-other") }},
		{"missing-workspace", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.Resource.Id = nil }},
		{"empty-workspace", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.Resource.Id = proto.String("") }},
		{"other-kind", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) {
			r.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT
		}},
		{"other-tenant", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.Scope.GetTenant().TenantId = "tenant-other" }},
		{"platform-scope", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) {
			r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
		}},
		{"other-actor", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) { r.ActorId = "202" }},
		{"other-live-session", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) {
			r.SessionId = proto.String(owneridentity.SessionReference(otherCookie))
		}},
		{"mutating-action", owneridentity.Fabric.Service(), func(r *api.AuthorizationRequest) {
			r.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES
		}},
		// Match the peer to the wrong audience so the audience check cannot be
		// satisfied merely by the general caller/audience fence rejecting it.
		{"other-audience", owneridentity.Workspace.Service(), func(r *api.AuthorizationRequest) { r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, _ := f.continuation(t)
			if tc.change != nil {
				tc.change(request)
			}
			_, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), tc.peer), request)
			if code := status.Code(err); code != codes.PermissionDenied && code != codes.Unauthenticated {
				t.Fatalf("continuation escaped its boundary: code=%s error=%v", code, err)
			}
		})
	}
	// A valid read decision for the very same actor/session/workspace is still
	// not a credential reveal and must not open this internal Fabric read.
	t.Run("other-original-action", func(t *testing.T) {
		request, _ := f.continuation(t)
		read := proto.Clone(f.reveal).(*api.AuthorizationRequest)
		read.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE
		original, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), read)
		if err != nil {
			t.Fatal(err)
		}
		request.AuthorizationContextId = proto.String(original.GetAuthorizationContextId())
		if _, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), request); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("ordinary workspace read became a reveal continuation: %v", err)
		}
	})
}

func TestWorkspaceApplicationCredentialsObserveOriginalContextPostgres(t *testing.T) {
	f := newApplicationCredentialsPolicyFixture(t)
	otherCookie, _ := login(t, f.client)
	// Each case changes only the persisted original decision. Requests keep the
	// genuine live owner session, revealing checks a request-only matrix misses.
	for _, tc := range []struct {
		name  string
		query string
		value any
	}{
		{"audience", `UPDATE tenant.authorization_contexts SET audience_owner=$2 WHERE id=$1`, "serve"},
		{"resource-kind", `UPDATE tenant.authorization_contexts SET resource_kind=$2 WHERE id=$1`, "tenant"},
		{"workspace", `UPDATE tenant.authorization_contexts SET resource_id=$2 WHERE id=$1`, "workspace-other"},
		{"actor", `UPDATE tenant.authorization_contexts SET actor_id=$2 WHERE id=$1`, "202"},
		{"session", `UPDATE tenant.authorization_contexts SET session_id=$2 WHERE id=$1`, owneridentity.SessionReference(otherCookie)},
		{"scope", `UPDATE tenant.authorization_contexts SET scope_type=$2,tenant_id=NULL WHERE id=$1`, "platform"},
		{"permission-version", `UPDATE tenant.authorization_contexts SET permission_version=permission_version+1 WHERE id=$1`, nil},
		{"revoked", `UPDATE tenant.authorization_contexts SET revoked_at=now() WHERE id=$1`, nil},
		{"expired", `UPDATE tenant.authorization_contexts SET issued_at=now()-interval '2 minutes',expires_at=now()-interval '1 minute' WHERE id=$1`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, original := f.continuation(t)
			args := []any{original.GetAuthorizationContextId()}
			if tc.value != nil {
				args = append(args, tc.value)
			}
			if _, err := f.db.ExecContext(t.Context(), tc.query, args...); err != nil {
				t.Fatal(err)
			}
			_, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), request)
			if code := status.Code(err); code != codes.PermissionDenied && code != codes.Unauthenticated {
				t.Fatalf("invalid original context authorized observe: code=%s error=%v", code, err)
			}
		})
	}
}

// Matching invalid resources must still fail: checking equality alone would let
// an unnamed Workspace or a different resource kind inherit the reveal context.
func TestWorkspaceApplicationCredentialsObserveResourceShapePostgres(t *testing.T) {
	f := newApplicationCredentialsPolicyFixture(t)
	for _, tc := range []struct {
		name     string
		resource *api.AuthorizationResource
		kind     string
	}{
		{"missing-workspace", &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE}, "workspace"},
		{"empty-workspace", &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("")}, "workspace"},
		{"other-kind", &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("workspace-reveal")}, "tenant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, original := f.continuation(t)
			var id any
			if tc.resource.Id != nil {
				id = tc.resource.GetId()
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.authorization_contexts SET resource_kind=$2,resource_id=$3 WHERE id=$1`, original.GetAuthorizationContextId(), tc.kind, id); err != nil {
				t.Fatal(err)
			}
			request.Resource = tc.resource
			if _, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), request); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("matching invalid resource opened the continuation: %v", err)
			}
		})
	}
}

func TestWorkspaceApplicationCredentialsObserveLiveAuthorityPostgres(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		arg   any
	}{
		{"admin", `UPDATE tenant.tenant_members SET role=$1 WHERE id='member-test'`, "admin"},
		{"member", `UPDATE tenant.tenant_members SET role=$1 WHERE id='member-test'`, "member"},
		{"membership-revoked", `UPDATE tenant.tenant_members SET revoked_at=now() WHERE id=$1`, "member-test"},
		{"permission-version-changed", `UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id=$1`, "tenant-test"},
		{"session-expired", `UPDATE tenant.sessions SET expires_at=now()-interval '1 minute' WHERE id=$1`, nil},
		{"logout", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplicationCredentialsPolicyFixture(t)
			request, _ := f.continuation(t)
			if tc.name == "logout" {
				if _, err := f.client.Logout(t.Context(), &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: request.SessionId}}); err != nil {
					t.Fatal(err)
				}
			} else {
				arg := tc.arg
				if tc.name == "session-expired" {
					arg = request.GetSessionId()
				}
				if _, err := f.db.ExecContext(t.Context(), tc.query, arg); err != nil {
					t.Fatal(err)
				}
			}
			// Do not advance permission_version in the role cases: a live role
			// downgrade alone must remove both canonical reveal and continuation.
			if tc.name == "admin" || tc.name == "member" {
				if _, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), f.reveal); status.Code(err) != codes.PermissionDenied {
					t.Fatalf("non-owner inherited canonical reveal permission: %v", err)
				}
			}
			_, err := f.service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), request)
			if code := status.Code(err); code != codes.PermissionDenied && code != codes.Unauthenticated {
				t.Fatalf("lost live authority still authorized observe: code=%s error=%v", code, err)
			}
		})
	}
}
