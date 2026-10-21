package identity_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

func TestBuildPolicyWebuiContinuationPostgres(t *testing.T) {
	base, db, client, _ := system(t)
	bff := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
	tenantCookie, tenantSession := login(t, client)
	if tenantSession.GetTenantId() != "tenant-test" || tenantSession.GetRole() != api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN {
		t.Fatal("negative fixture must be a real tenant administrator session")
	}
	// This service has no platform administrators. Its real tenant administrator
	// must not obtain a platform-scoped catalog read through the member role.
	if _, err := base.AuthorizeAction(bff, &api.AuthorizationRequest{
		Scope:   &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		ActorId: tenantSession.GetActorId(), SessionId: proto.String(owneridentity.SessionReference(tenantCookie)),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_CAPABILITY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS,
		Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, RequestId: "tenant-cannot-admit",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("tenant role inherited platform permission: %v", err)
	}
	// Remove the membership before another real login. A platform-scoped request
	// alone is insufficient: the session itself must have no Tenant or role, so
	// this positive case cannot pass through the pre-existing member permission.
	if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	var memberships int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE actor_id='101' AND revoked_at IS NULL`).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("platform administrator still has tenant memberships: count=%d error=%v", memberships, err)
	}
	service, err := identity.New(db, base.Gateway, bytes.Repeat([]byte("s"), 32), []string{"101"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service.GatewayStore = base.GatewayStore
	const token = "isolated-platform-policy-test-token-0001"
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owneridentity.Tenant,
		TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.ConsoleBFF, owneridentity.Tenant.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	platformClient := api.NewTenantProductServiceClient(conn)
	cookie, platformSession := login(t, platformClient)
	readback, err := platformClient.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(cookie)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []*api.Session{platformSession, readback} {
		if session.TenantId != nil || session.Role != nil || session.GetActorId() != "101" {
			t.Fatal("platform administrator login/readback must have no Tenant or tenant role")
		}
		found := false
		for _, action := range session.Permissions {
			found = found || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS
		}
		if !found {
			t.Fatal("platform administrator session lacks the public WebUI catalog permission")
		}
	}
	request := &api.AuthorizationRequest{
		Scope:   &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(cookie)),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, RequestId: "policy-admission",
	}
	// Prove the role permission independently of the policy continuation.
	direct := proto.Clone(request).(*api.AuthorizationRequest)
	direct.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	direct.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS
	direct.RequestId = "platform-webui-direct"
	if _, err := service.AuthorizeAction(bff, direct); err != nil {
		t.Fatalf("platform administrator without a tenant role cannot read WebUI catalog: %v", err)
	}
	decision, err := service.AuthorizeAction(bff, request)
	if err != nil {
		t.Fatal(err)
	}
	continuation := proto.Clone(request).(*api.AuthorizationRequest)
	continuation.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	continuation.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS
	continuation.AuthorizationContextId = decision.AuthorizationContextId
	capability := ownerservice.WithPeerOwner(t.Context(), owneridentity.Capability.Service())
	if _, err := service.AuthorizeAction(capability, continuation); err != nil {
		t.Fatalf("original platform policy cannot read approved WebUI catalog: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*api.AuthorizationRequest)
	}{
		{"other-resource", func(r *api.AuthorizationRequest) { r.Resource.Id = proto.String("other") }},
		{"other-kind", func(r *api.AuthorizationRequest) {
			r.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION
		}},
		{"other-action", func(r *api.AuthorizationRequest) {
			r.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERWEBUIVERSION
		}},
		{"other-scope", func(r *api.AuthorizationRequest) {
			r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(continuation).(*api.AuthorizationRequest)
			tc.change(r)
			if _, err := service.AuthorizeAction(capability, r); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("widened continuation accepted: %v", err)
			}
		})
	}
	if _, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Build.Service()), continuation); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign peer accepted: %v", err)
	}
	// Runtime registration may list publishers, but cannot inherit this policy-only WebUI read.
	other := proto.Clone(request).(*api.AuthorizationRequest)
	other.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION
	otherDecision, err := service.AuthorizeAction(bff, other)
	if err != nil {
		t.Fatal(err)
	}
	wrong := proto.Clone(continuation).(*api.AuthorizationRequest)
	wrong.AuthorizationContextId = otherDecision.AuthorizationContextId
	if _, err := service.AuthorizeAction(capability, wrong); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another original action accepted: %v", err)
	}
}

func TestSessionPermissionsSurvivePublicReadbackPostgres(t *testing.T) {
	_, _, client, _ := system(t)
	cookie, loginSession := login(t, client)
	session, err := client.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(cookie)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, actual := range []*api.Session{loginSession, session} {
		encoded, err := publicjson.Marshal(actual)
		if err != nil {
			t.Fatalf("real owner Session cannot cross BFF JSON boundary: %v", err)
		}
		decoded := &api.Session{}
		if err := publicjson.Unmarshal(encoded, decoded); err != nil || !proto.Equal(actual, decoded) {
			t.Fatalf("Session changed across public readback: %v", err)
		}
		found := false
		for _, action := range decoded.Permissions {
			if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEFINANCIALRECEIPT {
				found = true
			}
		}
		if !found {
			t.Fatal("real administrator Session lost its owner financial-read permission")
		}
	}
}
