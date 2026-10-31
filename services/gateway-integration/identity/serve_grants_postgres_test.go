package identity_test

import (
	"bytes"
	"context"
	"database/sql"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

type serveUpdateCommitReader struct {
	api.UnimplementedOwnerCommitReadbackServer
	actual   *api.OwnerCommitEvidence
	requests []*api.ReadOwnerCommitRequest
}

func (r *serveUpdateCommitReader) ReadOwnerCommit(ctx context.Context, request *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	peer, verified := ownerservice.PeerOwner(ctx)
	if !verified || peer != owneridentity.Tenant.Service() {
		return nil, status.Error(codes.PermissionDenied, "Tenant readback peer required")
	}
	r.requests = append(r.requests, proto.Clone(request).(*api.ReadOwnerCommitRequest))
	if request.GetOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || request.GetOperationId() != r.actual.OperationId || request.GetResourceId() != r.actual.ResourceId {
		return nil, status.Error(codes.FailedPrecondition, "original Serve commitment required")
	}
	return proto.Clone(r.actual).(*api.OwnerCommitEvidence), nil
}

type serveGrantFixture struct {
	s        *identity.Service
	db       *sql.DB
	client   api.TenantProductServiceClient
	session  string
	decision *api.AuthorizationDecision
	reader   *serveUpdateCommitReader
	request  *api.AcceptedOperationGrantRequest
}

func serveGrantSystem(t *testing.T) serveGrantFixture {
	t.Helper()
	s, db, client, _ := system(t)
	raw, _ := login(t, client)
	session := owneridentity.SessionReference(raw)
	request := &api.AuthorizationRequest{
		Scope:   &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}},
		ActorId: "101", SessionId: &session, AudienceOwner: api.OwnerEnum_OWNER_ENUM_SERVE,
		Action:    api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION,
		Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-original")},
		RequestId: "update-workspace",
	}
	decision, err := s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), request)
	if err != nil {
		t.Fatal(err)
	}
	proof := &api.OwnerCommitEvidence{
		Owner: api.OwnerEnum_OWNER_ENUM_SERVE, OperationId: "serve-update-original", ResourceId: "deployment-next",
		AcceptedInputDigest: "sha256:" + strings.Repeat("c", 64), CommittedVersion: 1, AcceptedAt: timestamppb.Now(),
		AuthorizationContextId: decision.GetAuthorizationContextId(), ActorId: request.ActorId, Scope: request.Scope,
		AcceptedAction: request.Action, AuthorizationResource: request.Resource,
		ContinuationResources: []*api.AuthorizationResource{
			{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("runtime-next")},
			proto.Clone(request.Resource).(*api.AuthorizationResource),
		},
	}
	reader := &serveUpdateCommitReader{actual: proof}
	const token = "isolated-tenant-serve-readback-token-0001"
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owneridentity.Serve, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Tenant.Service(): token}})
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Register(func(g *grpc.Server) { api.RegisterOwnerCommitReadbackServer(g, reader) }); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeOn(listener) }()
	t.Cleanup(server.Stop)
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.Tenant.Service(), owneridentity.Serve.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s.ServeCommit = api.NewOwnerCommitReadbackClient(conn)
	return serveGrantFixture{s, db, client, session, decision, reader, &api.AcceptedOperationGrantRequest{
		AuthorizationContextId: decision.GetAuthorizationContextId(), OwnerCommitEvidence: proto.Clone(proof).(*api.OwnerCommitEvidence),
		AllowedActions: []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE,
		},
	}}
}

func (f serveGrantFixture) issue(t *testing.T) *api.AcceptedOperationGrant {
	t.Helper()
	grant, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service()), f.request)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func serveContinuation(grant *api.AcceptedOperationGrant, action api.AuthorizationActionEnum) (*api.AuthorizationRequest, owneridentity.Service) {
	owner, peer, resource := api.OwnerEnum_OWNER_ENUM_CAPABILITY, owneridentity.Capability.Service(), &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("runtime-next")}
	switch action {
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS:
		owner, peer, resource = api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, owneridentity.RuntimeControl.Service(), &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES:
		owner, peer, resource = api.OwnerEnum_OWNER_ENUM_FABRIC, owneridentity.Fabric.Service(), &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-original")}
	}
	return &api.AuthorizationRequest{ActorId: grant.ActorId, Scope: grant.Scope, AcceptedOperationGrantId: &grant.Id, AudienceOwner: owner, Action: action, Resource: resource, RequestId: "continue-update"}, peer
}

func TestServeUpdateAcceptedGrantPostgres(t *testing.T) {
	f := serveGrantSystem(t)
	original := proto.Clone(f.reader.actual)
	grant := f.issue(t)
	if grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || grant.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION || grant.ResourceId != "deployment-next" || grant.AcceptedOperationId != "serve-update-original" || grant.ActorId != "101" || !proto.Equal(grant.Scope, f.reader.actual.Scope) || grant.AcceptedPermissionVersion != f.decision.PermissionVersion {
		t.Fatal("grant lost the original Serve update identity", grant)
	}
	if replay := f.issue(t); !proto.Equal(replay, grant) {
		t.Fatal("duplicate issuance changed original grant")
	}
	for _, action := range f.request.AllowedActions {
		request, peer := serveContinuation(grant, action)
		decision, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), request)
		if err != nil {
			t.Fatalf("%v: %v", action, err)
		}
		if err = owneridentity.ValidateDecision(request, decision, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if !proto.Equal(original, f.reader.actual) {
		t.Fatal("continuation rewrote owner commitment")
	}
	for _, read := range f.reader.requests {
		if read.Owner != grant.AcceptedOperationOwner || read.OperationId != grant.AcceptedOperationId || read.ResourceId != grant.ResourceId {
			t.Fatal("readback lost original operation/deployment", read)
		}
	}
	// The same committed acceptance recovers after logout and the interactive
	// context's expiry, without renewing its acceptance timestamp or context.
	accepted := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.authorization_contexts SET issued_at=$2,expires_at=$3 WHERE id=$1`, f.decision.GetAuthorizationContextId(), accepted.Add(-time.Second), accepted.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.reader.actual.AcceptedAt = timestamppb.New(accepted)
	f.request.OwnerCommitEvidence.AcceptedAt = timestamppb.New(accepted)
	if _, err := f.client.Logout(t.Context(), &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: &f.session}}); err != nil {
		t.Fatal(err)
	}
	restarted, err := identity.New(f.db, f.s.Gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	restarted.GatewayStore, restarted.ServeCommit = f.s.GatewayStore, f.s.ServeCommit
	f.s = restarted
	recovered := proto.Clone(f.reader.actual)
	if replay := f.issue(t); !proto.Equal(replay, grant) {
		t.Fatal("restart minted a second grant")
	}
	request, peer := serveContinuation(grant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE)
	if _, err = f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), request); err != nil {
		t.Fatal("accepted obligation depended on live cookie", err)
	}
	if !proto.Equal(recovered, f.reader.actual) {
		t.Fatal("recovery rewrote original acceptance")
	}
}

func TestServeUpdateGrantRejectsUnboundEvidencePostgres(t *testing.T) {
	f := serveGrantSystem(t)
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service())
	for _, tc := range []struct {
		name   string
		mutate func(*api.OwnerCommitEvidence)
	}{
		{"actor", func(e *api.OwnerCommitEvidence) { e.ActorId = "another-actor" }},
		{"scope", func(e *api.OwnerCommitEvidence) { e.Scope.GetTenant().TenantId = "another-tenant" }},
		{"action", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME
		}},
		{"context", func(e *api.OwnerCommitEvidence) { e.AuthorizationContextId = "another-context" }},
		{"digest", func(e *api.OwnerCommitEvidence) { e.AcceptedInputDigest = "sha256:" + strings.Repeat("d", 64) }},
		{"time", func(e *api.OwnerCommitEvidence) { e.AcceptedAt = timestamppb.New(time.Now().Add(time.Hour)) }},
	} {
		t.Run("claimed_"+tc.name, func(t *testing.T) {
			request := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
			tc.mutate(request.OwnerCommitEvidence)
			if _, err := f.s.IssueAcceptedOperationGrant(ctx, request); status.Code(err) != codes.PermissionDenied {
				t.Fatal("caller-authored evidence accepted", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.OwnerCommitEvidence)
	}{
		{"actor", func(e *api.OwnerCommitEvidence) { e.ActorId = "another-actor" }},
		{"scope", func(e *api.OwnerCommitEvidence) { e.Scope.GetTenant().TenantId = "another-tenant" }},
		{"action", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ROLLBACKWORKSPACE
		}},
		{"context", func(e *api.OwnerCommitEvidence) { e.AuthorizationContextId = "another-context" }},
		{"empty_digest", func(e *api.OwnerCommitEvidence) { e.AcceptedInputDigest = "" }},
		{"before_context", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAt = timestamppb.New(f.decision.IssuedAt.AsTime().Add(-time.Second))
		}},
		{"after_context", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAt = timestamppb.New(f.decision.ExpiresAt.AsTime().Add(time.Second))
		}},
		{"authorization_workspace", func(e *api.OwnerCommitEvidence) { e.AuthorizationResource.Id = proto.String("another-workspace") }},
		{"continuation_workspace", func(e *api.OwnerCommitEvidence) { e.ContinuationResources[1].Id = proto.String("another-workspace") }},
		{"missing_workspace", func(e *api.OwnerCommitEvidence) { e.ContinuationResources = e.ContinuationResources[:1] }},
		{"empty_version", func(e *api.OwnerCommitEvidence) { e.ContinuationResources[0].Id = nil }},
		{"extra_version", func(e *api.OwnerCommitEvidence) {
			e.ContinuationResources = append(e.ContinuationResources, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("another-version")})
		}},
	} {
		t.Run("owner_"+tc.name, func(t *testing.T) {
			original := f.reader.actual
			defer func() { f.reader.actual = original }()
			request := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
			tc.mutate(request.OwnerCommitEvidence)
			f.reader.actual = request.OwnerCommitEvidence
			if _, err := f.s.IssueAcceptedOperationGrant(ctx, request); err == nil {
				t.Fatal("invalid committed evidence accepted")
			}
		})
	}
	for _, peer := range []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), owneridentity.Build.Service()} {
		if _, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), peer), f.request); status.Code(err) != codes.PermissionDenied {
			t.Fatal("other peer issued Serve grant", peer, err)
		}
	}
	f.s.ServeCommit = nil
	if _, err := f.s.IssueAcceptedOperationGrant(ctx, f.request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("grant issued without Serve readback", err)
	}
}

func TestServeUpdateGrantRejectsOtherContinuationsPostgres(t *testing.T) {
	f := serveGrantSystem(t)
	grant := f.issue(t)
	for _, action := range f.request.AllowedActions {
		t.Run(action.String(), func(t *testing.T) {
			original, peer := serveContinuation(grant, action)
			for _, tc := range []struct {
				name   string
				mutate func(*api.AuthorizationRequest)
			}{
				{"other_resource", func(r *api.AuthorizationRequest) { r.Resource.Id = proto.String("another-resource") }},
				{"deployment_as_workspace", func(r *api.AuthorizationRequest) {
					r.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(grant.ResourceId)}
				}},
				{"other_actor", func(r *api.AuthorizationRequest) { r.ActorId = "another-actor" }},
				{"other_scope", func(r *api.AuthorizationRequest) { r.Scope.GetTenant().TenantId = "another-tenant" }},
				{"other_audience", func(r *api.AuthorizationRequest) { r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_SERVE }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					request := proto.Clone(original).(*api.AuthorizationRequest)
					tc.mutate(request)
					requestPeer := peer
					if tc.name == "other_audience" {
						requestPeer = owneridentity.Serve.Service()
					}
					if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), requestPeer), request); status.Code(err) != codes.PermissionDenied {
						t.Fatal("grant escaped original action/resource", err)
					}
				})
			}
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), original); status.Code(err) != codes.PermissionDenied {
				t.Fatal("browser used accepted grant", err)
			}
		})
	}
	for _, action := range []api.AuthorizationActionEnum{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION,
	} {
		request := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
		request.AllowedActions = append(request.AllowedActions, action)
		if _, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service()), request); status.Code(err) != codes.PermissionDenied {
			t.Fatal("unused or financial action issued", action, err)
		}
	}
	// A presented owner peer and cookie cannot replace the accepted grant.
	acquire, peer := serveContinuation(grant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE)
	acquire.AcceptedOperationGrantId = nil
	acquire.SessionId = &f.session
	if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), acquire); status.Code(err) != codes.PermissionDenied {
		t.Fatal("public acquireReference permission added", err)
	}
	changed := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
	changed.AllowedActions = changed.AllowedActions[:1]
	if _, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service()), changed); status.Code(err) != codes.PermissionDenied {
		t.Fatal("retry changed persisted action surface", err)
	}
	// Even corrupted persisted allowed_actions cannot widen the canonical workflow.
	if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.accepted_operation_grants SET allowed_actions=array_append(allowed_actions,'PROVISIONACCEPTEDRESOURCES') WHERE id=$1`, grant.Id); err != nil {
		t.Fatal(err)
	}
	provision, _ := serveContinuation(grant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES)
	provision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES
	if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), provision); status.Code(err) != codes.PermissionDenied {
		t.Fatal("persisted action widened canonical authority", err)
	}
}

func TestServeUpdateGrantCloseoutPostgres(t *testing.T) {
	f := serveGrantSystem(t)
	grant := f.issue(t)
	for _, tc := range []struct{ name, sql string }{
		{"revoked_member", `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'`},
		{"demoted_member", `UPDATE tenant.tenant_members SET role='member' WHERE actor_id='101'`},
		{"inactive_tenant", `UPDATE tenant.tenants SET status='suspended' WHERE id='tenant-test'`},
		{"closeout_grant", `UPDATE tenant.accepted_operation_grants SET mode='closeout_only'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.db.ExecContext(t.Context(), tc.sql); err != nil {
				t.Fatal(err)
			}
			for _, action := range f.request.AllowedActions {
				request, peer := serveContinuation(grant, action)
				_, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), request)
				mutation := action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE
				if mutation && status.Code(err) != codes.PermissionDenied {
					t.Fatalf("mutation %v survived closeout: %v", action, err)
				}
				if !mutation && err != nil {
					t.Fatalf("original read/release %v rejected: %v", action, err)
				}
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET revoked_at=NULL,role='admin';UPDATE tenant.tenants SET status='active'`); err != nil {
				t.Fatal(err)
			}
			request, peer := serveContinuation(grant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), request); status.Code(err) != codes.PermissionDenied {
				t.Fatal("restored membership resurrected grant", err)
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.accepted_operation_grants SET mode='continue_original'`); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct{ name, sql string }{
		{"revoked_mode", `UPDATE tenant.accepted_operation_grants SET mode='revoked'`},
		{"revoked", `UPDATE tenant.accepted_operation_grants SET revoked_at=now()`},
		{"completed", `UPDATE tenant.accepted_operation_grants SET obligation_completed_at=now()`},
		{"expired", `UPDATE tenant.accepted_operation_grants SET issued_at=now()-interval '2 hours',expires_at=now()-interval '1 hour'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.db.ExecContext(t.Context(), tc.sql); err != nil {
				t.Fatal(err)
			}
			request, peer := serveContinuation(grant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), request); err == nil {
				t.Fatal("ended grant still authorized readback")
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.accepted_operation_grants SET mode='continue_original',revoked_at=NULL,obligation_completed_at=NULL,expires_at=NULL`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
