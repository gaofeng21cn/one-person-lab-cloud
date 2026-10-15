package identity_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

func bindingClient(t *testing.T, address string, peer owneridentity.Service) api.GatewayCoordinationClient {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(peer, bindingPeerToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewGatewayCoordinationClient(conn)
}

func bindingUserLogin(t *testing.T, c api.TenantProductServiceClient, email string) *api.CallContext {
	t.Helper()
	var header metadata.MD
	if _, err := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	session, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: email, Password: "password"}}, grpc.Header(&header))
	if err != nil {
		t.Fatal(err)
	}
	ref := owneridentity.SessionReference(header.Get(owneridentity.SessionCookieHeader)[0])
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if session.GetTenantId() != "" {
		scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: session.GetTenantId()}}}
	}
	return &api.CallContext{ActorId: session.ActorId, SessionId: &ref, Scope: scope, RequestId: "binding-" + session.ActorId, IdempotencyKey: "bind-" + session.ActorId}
}

func selfBinding(t *testing.T, c api.GatewayCoordinationClient, call *api.CallContext) *api.UserExternalBindingReadback {
	t.Helper()
	b, err := c.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: call})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUserExternalBindingsAuthenticatePersistAndReadAcrossReplica(t *testing.T) {
	s, tenant, address := bindingSystem(t)
	admin := platformAdminCall(t, tenant)
	call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	build := bindingClient(t, address, owneridentity.Build.Service())
	b := selfBinding(t, console, &api.CallContext{SessionId: call.SessionId})
	if b.ActorId != "201" || b.Sub2ApiUserId != "201" || b.RegistryHost != "uswccr.ccs.tencentyun.com" || b.RegistryNamespace != "oplcloud" || b.Repository != "testcloud4" || b.Status != api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE || b.Verification != api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED || b.VerifiedAt == nil || b.DestinationVerifiedAt == nil {
		t.Fatalf("binding=%v", b)
	}
	// The native assignment is authorized by a real platform-admin session, and
	// replays the already assigned identity rather than creating another writer.
	command := &api.UserExternalBindingCommand{Context: admin, ActorId: b.ActorId, Sub2ApiUserId: b.Sub2ApiUserId, RegistryHost: b.RegistryHost, RegistryNamespace: b.RegistryNamespace, Repository: b.Repository}
	assigned, err := console.BindUserExternalIdentity(t.Context(), command)
	if err != nil || assigned.Repository != b.Repository {
		t.Fatalf("admin bind=%v err=%v", assigned, err)
	}
	read, err := build.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: call, ActorId: call.ActorId})
	if err != nil || !proto.Equal(read, assigned) {
		t.Fatalf("Build read=%v err=%v", read, err)
	}
	// A new owner/store instance has no login or member credential cache.
	store, err := identity.NewGatewayStore(s.GatewayStore.DB())
	if err != nil {
		t.Fatal(err)
	}
	replica, err := identity.New(s.DB, s.Gateway, []byte(strings.Repeat("b", 32)), []string{"103"}, s.InvitationTTL)
	if err != nil {
		t.Fatal(err)
	}
	replica.GatewayStore = store
	replica.ConfigureRegistry(b.RegistryHost, b.RegistryNamespace)
	fresh, err := replica.GetSelfUserExternalBinding(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), &api.GetSelfUserExternalBindingRequest{Context: &api.CallContext{SessionId: call.SessionId}})
	if err != nil || !proto.Equal(fresh, assigned) {
		t.Fatalf("replica=%v err=%v", fresh, err)
	}
	renamed := bindingUserLogin(t, tenant, "renamed@medopl.com")
	if after := selfBinding(t, console, renamed); after.Repository != "testcloud4" || !proto.Equal(after.VerifiedAt, b.VerifiedAt) {
		t.Fatalf("email rename changed binding=%v", after)
	}
	var raw string
	if err = s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT row_to_json(m)::text FROM gateway.identity_mappings m WHERE actor_id='201'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"password", "admin-token", "private-gateway-token"} {
		if strings.Contains(raw, secret) {
			t.Fatal("credential persisted in mapping")
		}
	}
}

func TestUserExternalBindingOwnerGates(t *testing.T) {
	s, tenant, address := bindingSystem(t)
	admin := platformAdminCall(t, tenant)
	user := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	other := bindingUserLogin(t, tenant, "other@example.test")
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	build := bindingClient(t, address, owneridentity.Build.Service())
	base := &api.UserExternalBindingCommand{Context: admin, ActorId: "201", Sub2ApiUserId: "201", RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "testcloud4"}
	for _, tc := range []struct {
		name   string
		mutate func(*api.UserExternalBindingCommand)
		code   codes.Code
	}{
		{"missing", func(r *api.UserExternalBindingCommand) { r.ActorId = "missing" }, codes.NotFound},
		{"different subject", func(r *api.UserExternalBindingCommand) { r.Sub2ApiUserId = "opaque-other" }, codes.AlreadyExists},
		{"host override", func(r *api.UserExternalBindingCommand) { r.RegistryHost = "other.example" }, codes.InvalidArgument},
		{"namespace override", func(r *api.UserExternalBindingCommand) { r.RegistryNamespace = "other" }, codes.InvalidArgument},
		{"traversal", func(r *api.UserExternalBindingCommand) { r.Repository = "../testcloud4" }, codes.InvalidArgument},
		{"nested repository", func(r *api.UserExternalBindingCommand) { r.Repository = "another/testcloud4" }, codes.InvalidArgument},
		{"reassignment", func(r *api.UserExternalBindingCommand) { r.Repository = "other" }, codes.AlreadyExists},
		{"ordinary user", func(r *api.UserExternalBindingCommand) { r.Context = user }, codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(base).(*api.UserExternalBindingCommand)
			tc.mutate(r)
			if _, err := console.BindUserExternalIdentity(t.Context(), r); status.Code(err) != tc.code {
				t.Fatalf("code=%v want %v err=%v", status.Code(err), tc.code, err)
			}
		})
	}
	if _, err := build.BindUserExternalIdentity(t.Context(), base); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Build wrote binding: %v", err)
	}
	if _, err := console.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: user, ActorId: user.ActorId}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("BFF used Build read: %v", err)
	}
	if _, err := build.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: user, ActorId: other.ActorId}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("User read another User: %v", err)
	}
	forged := proto.Clone(user).(*api.CallContext)
	forged.ActorId = other.ActorId
	if _, err := build.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: forged, ActorId: other.ActorId}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged actor accepted: %v", err)
	}
	if _, err := console.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: forged}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged self accepted: %v", err)
	}
	if _, err := console.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{}); err == nil {
		t.Fatal("anonymous self read")
	}
	// Fault injection is confined to the disposable owner harness. Normal setup
	// above uses successful authentication and the real owner RPC.
	if _, err := s.GatewayStore.DB().ExecContext(t.Context(), `DELETE FROM gateway.identity_mappings WHERE actor_id='201'`); err != nil {
		t.Fatal(err)
	}
	if _, err := build.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: user, ActorId: user.ActorId}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing read=%v", err)
	}
}

func TestUserExternalBindingRevokedAndUnverifiedFailClosed(t *testing.T) {
	for _, fault := range []struct{ name, sql string }{
		{"revoked", `UPDATE gateway.identity_mappings SET revoked_at=now() WHERE actor_id='201'`},
		{"unverified identity", `UPDATE gateway.identity_mappings SET verified_at=NULL WHERE actor_id='201'`},
		{"unverified destination", `UPDATE gateway.identity_mappings SET destination_verified_at=NULL WHERE actor_id='201'`},
	} {
		t.Run(fault.name, func(t *testing.T) {
			s, tenant, address := bindingSystem(t)
			call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
			if _, err := s.GatewayStore.DB().ExecContext(t.Context(), fault.sql); err != nil {
				t.Fatal(err)
			}
			client := bindingClient(t, address, owneridentity.Build.Service())
			if _, err := client.ReadUserExternalIdentity(t.Context(), &api.UserExternalBindingReadRequest{Context: call, ActorId: call.ActorId}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("invalid mapping read=%v", err)
			}
			if _, err := bindingClient(t, address, owneridentity.ConsoleBFF).GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: call}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("invalid self=%v", err)
			}
		})
	}
}

func TestUserExternalBindingConcurrentAdminReplayAndUniqueDestination(t *testing.T) {
	s, tenant, address := bindingSystem(t)
	// Login establishes only identity when no installation destination is declared.
	s.ConfigureRegistry("", "")
	admin := platformAdminCall(t, tenant)
	user := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	other := bindingUserLogin(t, tenant, "other@example.test")
	s.ConfigureRegistry("uswccr.ccs.tencentyun.com", "oplcloud")
	c := bindingClient(t, address, owneridentity.ConsoleBFF)
	command := &api.UserExternalBindingCommand{Context: admin, ActorId: user.ActorId, Sub2ApiUserId: user.ActorId, RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "assigned"}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.BindUserExternalIdentity(context.Background(), command)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	duplicate := proto.Clone(command).(*api.UserExternalBindingCommand)
	duplicate.ActorId = other.ActorId
	duplicate.Sub2ApiUserId = other.ActorId
	if _, err := c.BindUserExternalIdentity(t.Context(), duplicate); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate destination=%v", err)
	}
	b := selfBinding(t, c, user)
	if b.Repository != "assigned" {
		t.Fatalf("binding=%v", b)
	}
	var rows int
	if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id=$1`, user.ActorId).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("mapping count=%d err=%v", rows, err)
	}
}

// seedHistoricalDestination runs as the isolated schema owner because 0004 has
// frozen the retired runtime writer. These are preexisting historical facts,
// not a new product path for creating a User binding.
func seedHistoricalDestination(t *testing.T, db *sql.DB, owners []string, repository, status string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenants(id,name) VALUES('tenant-history','Historical')`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range owners {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES($1,'tenant-history',$2,'owner')`, "owner-"+actor, actor); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository,status) VALUES('tenant-history','uswccr.ccs.tencentyun.com','oplcloud',$1,$2)`, repository, status); err != nil {
		t.Fatal(err)
	}
}

func TestUserExternalBindingPreservesHistoricalOwnerDestination(t *testing.T) {
	s, tenant, address := bindingSystemWithHistory(t, func(db *sql.DB) { seedHistoricalDestination(t, db, []string{"201"}, "old-approved-path", "reserved") })
	call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	b := selfBinding(t, bindingClient(t, address, owneridentity.ConsoleBFF), call)
	if b.Repository != "old-approved-path" {
		t.Fatalf("historical path lost: %v", b)
	}
	renamed := bindingUserLogin(t, tenant, "renamed@medopl.com")
	if got := selfBinding(t, bindingClient(t, address, owneridentity.ConsoleBFF), renamed).Repository; got != "old-approved-path" {
		t.Fatalf("rename changed historical path: %s", got)
	}
	var preserved string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT repository FROM tenant.tenant_repository_bindings WHERE tenant_id='tenant-history'`).Scan(&preserved); err != nil || preserved != "old-approved-path" {
		t.Fatalf("history=%q err=%v", preserved, err)
	}
	// A runtime connection from an older image cannot race the new writer after
	// 0004. SELECT remains available for provenance and owner-local handover.
	for _, statement := range []string{
		`UPDATE tenant.tenant_repository_bindings SET repository='stolen' WHERE tenant_id='tenant-history'`,
		`DELETE FROM tenant.tenant_repository_bindings WHERE tenant_id='tenant-history'`,
		`INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository) VALUES('unused','uswccr.ccs.tencentyun.com','oplcloud','injected')`,
	} {
		if _, err := s.DB.ExecContext(t.Context(), statement); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("retired runtime writer was not frozen: %v", err)
		}
	}
}

func TestUserExternalBindingRejectsHistoricalPathTakeover(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		owners                    []string
		repository, status, email string
	}{
		{"another owner", []string{"202"}, "testcloud4", "reserved", "testcloud4@medopl.com"},
		{"multiple owners", []string{"201", "202"}, "old-shared-path", "active", "testcloud4@medopl.com"},
		{"no owner", nil, "testcloud4", "reserved", "testcloud4@medopl.com"},
		{"retired path", []string{"202"}, "testcloud4", "retired", "testcloud4@medopl.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, _ := bindingSystemWithHistory(t, func(db *sql.DB) { seedHistoricalDestination(t, db, tc.owners, tc.repository, tc.status) })
			var header metadata.MD
			if _, err := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
				t.Fatal(err)
			}
			challenge := header.Get(owneridentity.SessionCookieHeader)[0]
			if _, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: tc.email, Password: "password"}}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("historical takeover was not rejected: %v", err)
			}
			var count int
			if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='201'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed login persisted User destination: %d %v", count, err)
			}
		})
	}
}

func TestCreateTenantDoesNotWriteHistoricalRepositoryBinding(t *testing.T) {
	s, tenant, _ := bindingSystem(t)
	admin := platformAdminCall(t, tenant)
	op, err := tenant.CreateTenant(t.Context(), &api.CreateTenantRpcRequest{Context: admin, Body: &api.CreateTenantRequest{Name: "User-owned OCI", OwnerGatewaySubjectId: "201", OwnerEmail: "testcloud4@medopl.com"}})
	if err != nil || op.GetResourceId() == "" {
		t.Fatalf("CreateTenant=%v err=%v", op, err)
	}
	var count int
	if err = s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_repository_bindings`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("Tenant wrote a repository: %d %v", count, err)
	}
}

func TestUserExternalBindingMissingGatewayStoreFailsClosed(t *testing.T) {
	s, c, _ := bindingSystem(t)
	s.GatewayStore = nil
	var header metadata.MD
	if _, err := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	if _, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "testcloud4@medopl.com", Password: "password"}}); status.Code(err) != codes.Unavailable {
		t.Fatalf("login without Gateway DB: %v", err)
	}
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	if err = s.Register(server); err == nil {
		t.Fatal("registered ready handlers without Gateway store")
	}
}

func TestUserExternalBindingIdentityConflictsCannotReauthenticateOrDuplicate(t *testing.T) {
	s, c, address := bindingSystem(t)
	user := bindingUserLogin(t, c, "testcloud4@medopl.com")
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	original := selfBinding(t, console, user)
	// The pre-existing binding claims the upstream subject under another actor.
	// A later login cannot silently create a second mapping for that subject.
	if _, err := s.GatewayStore.DB().ExecContext(t.Context(), `UPDATE gateway.identity_mappings SET actor_id='existing-opaque-actor' WHERE actor_id='201'`); err != nil {
		t.Fatal(err)
	}
	var header metadata.MD
	if _, err := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	if _, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "testcloud4@medopl.com", Password: "password"}}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate upstream subject accepted: %v", err)
	}
	var count int
	if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE sub2api_user_id=$1`, original.Sub2ApiUserId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate subject count=%d err=%v", count, err)
	}
	// A tampered actor binding must not be overwritten by successful upstream
	// authentication either. Its destination and opaque subject remain unchanged.
	if _, err := s.GatewayStore.DB().ExecContext(t.Context(), `UPDATE gateway.identity_mappings SET actor_id='201',sub2api_user_id='opaque-conflicting-subject' WHERE actor_id='existing-opaque-actor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "testcloud4@medopl.com", Password: "password"}}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting actor binding overwritten: %v", err)
	}
}

func TestUserExternalBindingLoginCollisionDoesNotAllocateAlternateDestination(t *testing.T) {
	s, c, address := bindingSystem(t)
	first := bindingUserLogin(t, c, "testcloud4@medopl.com")
	console := bindingClient(t, address, owneridentity.ConsoleBFF)
	original := selfBinding(t, console, first)
	var header metadata.MD
	if _, err := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	if _, err := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "testcloud4@other.example", Password: "password"}}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("destination collision accepted: %v", err)
	}
	var count int
	if err := s.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='203'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed login left alternate destination count=%d err=%v", count, err)
	}
	if !proto.Equal(original, selfBinding(t, console, first)) {
		t.Fatal("collision changed first User binding")
	}
}

// The retained reader resolves the actual persisted external mapping through the
// same live session authority. It receives no target-User or mutation privilege.
func TestRetainedOwnerResolvesOnlyLiveSessionBinding(t *testing.T) {
	s, tenant, address := bindingSystem(t)
	call := bindingUserLogin(t, tenant, "testcloud4@medopl.com")
	retained := bindingClient(t, address, owneridentity.ControlPlane)
	binding, err := retained.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: &api.CallContext{SessionId: call.SessionId}})
	if err != nil || binding.GetActorId() != call.ActorId || binding.GetSub2ApiUserId() != "201" {
		t.Fatalf("self binding=%v err=%v", binding, err)
	}
	_, err = retained.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: &api.CallContext{SessionId: call.SessionId, ActorId: "202"}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged actor err=%v", err)
	}
	if _, err = s.DB.ExecContext(t.Context(), `UPDATE tenant.sessions SET revoked_at=now() WHERE id=$1`, call.GetSessionId()); err != nil {
		t.Fatal(err)
	}
	_, err = retained.GetSelfUserExternalBinding(t.Context(), &api.GetSelfUserExternalBindingRequest{Context: &api.CallContext{SessionId: call.SessionId}})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked session err=%v", err)
	}
	_, err = retained.BindUserExternalIdentity(t.Context(), &api.UserExternalBindingCommand{Context: call, ActorId: call.ActorId})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retained owner identity mutation err=%v", err)
	}
}
