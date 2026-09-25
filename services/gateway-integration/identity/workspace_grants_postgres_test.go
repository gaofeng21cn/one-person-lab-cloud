package identity_test

import (
	"bytes"
	"context"
	"database/sql"
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

type workspaceCommitReader struct {
	api.OwnerCommitReadbackClient
	actual   *api.OwnerCommitEvidence
	requests []*api.ReadOwnerCommitRequest
}

func (r *workspaceCommitReader) ReadOwnerCommit(_ context.Context, request *api.ReadOwnerCommitRequest, _ ...grpc.CallOption) (*api.OwnerCommitEvidence, error) {
	r.requests = append(r.requests, proto.Clone(request).(*api.ReadOwnerCommitRequest))
	return proto.Clone(r.actual).(*api.OwnerCommitEvidence), nil
}

type workspaceGrantFixture struct {
	s        *identity.Service
	db       *sql.DB
	client   api.TenantProductServiceClient
	session  string
	decision *api.AuthorizationDecision
	reader   *workspaceCommitReader
	request  *api.AcceptedOperationGrantRequest
}

func workspaceGrantSystem(t *testing.T) workspaceGrantFixture {
	t.Helper()
	s, db, client, _ := system(t)
	raw, _ := login(t, client)
	ref := owneridentity.SessionReference(raw)
	r := &api.AuthorizationRequest{
		Scope:         &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}},
		ActorId:       "101",
		SessionId:     &ref,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE},
		RequestId:     "create-workspace",
	}
	d, err := s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), r)
	if err != nil {
		t.Fatal(err)
	}
	proof := &api.OwnerCommitEvidence{
		Owner:                  api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		OperationId:            "workspace-operation",
		ResourceId:             "workspace-original",
		AcceptedInputDigest:    "sha256:" + strings.Repeat("b", 64),
		CommittedVersion:       1,
		AcceptedAt:             timestamppb.Now(),
		AuthorizationContextId: d.GetAuthorizationContextId(),
		ActorId:                r.ActorId,
		Scope:                  r.Scope,
		AcceptedAction:         r.Action,
		AuthorizationResource:  r.Resource,
		ContinuationResources:  []*api.AuthorizationResource{{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("capability-original")}},
	}
	reader := &workspaceCommitReader{actual: proof}
	s.WorkspaceCommit = reader
	request := &api.AcceptedOperationGrantRequest{
		AuthorizationContextId: d.GetAuthorizationContextId(),
		OwnerCommitEvidence:    proto.Clone(proof).(*api.OwnerCommitEvidence),
		AllowedActions: []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT,
		},
	}
	return workspaceGrantFixture{s, db, client, ref, d, reader, request}
}

func (f workspaceGrantFixture) issue(t *testing.T) *api.AcceptedOperationGrant {
	t.Helper()
	g, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), f.request)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func workspaceContinuation(g *api.AcceptedOperationGrant, owner api.OwnerEnum, action api.AuthorizationActionEnum) *api.AuthorizationRequest {
	kind, id := api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, g.ResourceId
	if owner == api.OwnerEnum_OWNER_ENUM_CAPABILITY {
		kind, id = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "capability-original"
	}
	return &api.AuthorizationRequest{Scope: g.Scope, ActorId: g.ActorId, AcceptedOperationGrantId: &g.Id, AudienceOwner: owner, Action: action, Resource: &api.AuthorizationResource{Kind: kind, Id: &id}, RequestId: "continue-workspace"}
}

func workspaceAudience(action api.AuthorizationActionEnum) (api.OwnerEnum, owneridentity.Service) {
	switch action {
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES:
		return api.OwnerEnum_OWNER_ENUM_FABRIC, owneridentity.Fabric.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION:
		return api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, owneridentity.ResourceCatalog.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME:
		return api.OwnerEnum_OWNER_ENUM_SERVE, owneridentity.Serve.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT:
		return api.OwnerEnum_OWNER_ENUM_LEDGER, owneridentity.Ledger.Service()
	default:
		return api.OwnerEnum_OWNER_ENUM_CAPABILITY, owneridentity.Capability.Service()
	}
}

func TestWorkspaceAcceptedGrantPostgres(t *testing.T) {
	f := workspaceGrantSystem(t)
	g := f.issue(t)
	if g.AcceptedOperationOwner != api.OwnerEnum_OWNER_ENUM_WORKSPACE || g.AcceptedAction != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE || g.ResourceId != "workspace-original" {
		t.Fatal("grant lost its Workspace owner/action/resource", g)
	}
	if replay := f.issue(t); replay.Id != g.Id {
		t.Fatal("duplicate acceptance created a new grant")
	}
	wantRead := &api.ReadOwnerCommitRequest{Owner: g.AcceptedOperationOwner, OperationId: g.AcceptedOperationId, ResourceId: g.ResourceId}
	for _, read := range f.reader.requests {
		if !proto.Equal(read, wantRead) {
			t.Fatal("owner readback did not bind the original operation and resource", read)
		}
	}
	// An operation ID is local to its accepting owner. Existing Build grants
	// must remain independent even if another owner's operation has the same ID.
	buildRequest := &api.AuthorizationRequest{Scope: g.Scope, ActorId: g.ActorId, SessionId: &f.session, AudienceOwner: api.OwnerEnum_OWNER_ENUM_BUILD, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("package-version")}, RequestId: "build-with-same-operation-id"}
	buildDecision, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), buildRequest)
	if err != nil {
		t.Fatal(err)
	}
	buildProof := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_BUILD, OperationId: g.AcceptedOperationId, ResourceId: "build-original", AcceptedInputDigest: "sha256:" + strings.Repeat("c", 64), CommittedVersion: 1, AcceptedAt: timestamppb.Now(), AuthorizationContextId: buildDecision.GetAuthorizationContextId(), ActorId: buildRequest.ActorId, Scope: buildRequest.Scope, AcceptedAction: buildRequest.Action, AuthorizationResource: buildRequest.Resource}
	f.s.BuildCommit = &commitReader{actual: buildProof}
	buildGrant, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Build.Service()), &api.AcceptedOperationGrantRequest{AuthorizationContextId: buildDecision.GetAuthorizationContextId(), OwnerCommitEvidence: buildProof, AllowedActions: []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION}})
	if err != nil || buildGrant.GetId() == g.Id || buildGrant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_BUILD {
		t.Fatal("same operation ID collided across grant owners", err)
	}
	buildContinuation := &api.AuthorizationRequest{Scope: buildGrant.Scope, ActorId: buildGrant.ActorId, AcceptedOperationGrantId: &buildGrant.Id, AudienceOwner: api.OwnerEnum_OWNER_ENUM_CAPABILITY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, Id: &buildGrant.ResourceId}, RequestId: "read-build"}
	if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Capability.Service()), buildContinuation); err != nil {
		t.Fatal("Build grant recovered through the wrong owner", err)
	}
	for _, action := range f.request.AllowedActions {
		t.Run(action.String(), func(t *testing.T) {
			owner, peer := workspaceAudience(action)
			r := workspaceContinuation(g, owner, action)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), r); err != nil {
				t.Fatal("original continuation rejected", err)
			}
			wrong := proto.Clone(r).(*api.AuthorizationRequest)
			wrong.Resource.Id = proto.String("unrelated")
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), wrong); status.Code(err) != codes.PermissionDenied {
				t.Fatal("grant escaped its frozen resources", err)
			}
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("browser used an accepted grant", err)
			}
			wrong = proto.Clone(r).(*api.AuthorizationRequest)
			wrong.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), wrong); status.Code(err) != codes.PermissionDenied {
				t.Fatal("grant changed resource kinds", err)
			}
			wrong = proto.Clone(r).(*api.AuthorizationRequest)
			wrong.AudienceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), wrong); status.Code(err) != codes.PermissionDenied {
				t.Fatal("grant changed the action audience", err)
			}
		})
	}
	// A successful owner commit remains recoverable after its interactive
	// authorization window and cookie end, including after process restart.
	accepted := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.authorization_contexts SET issued_at=$2,expires_at=$3 WHERE id=$1`, f.decision.GetAuthorizationContextId(), accepted.Add(-time.Second), accepted.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.reader.actual.AcceptedAt = timestamppb.New(accepted)
	f.request.OwnerCommitEvidence.AcceptedAt = timestamppb.New(accepted)
	if _, err := f.client.Logout(t.Context(), &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: &f.session}}); err != nil {
		t.Fatal(err)
	}
	s, err := identity.New(f.db, f.s.Gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s.WorkspaceCommit = f.reader
	f.s = s
	if replay := f.issue(t); replay.Id != g.Id {
		t.Fatal("restart changed the original accepted grant")
	}
	r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES)
	if _, err := s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); err != nil {
		t.Fatal("expired interactive context erased accepted obligation", err)
	}
}

func TestWorkspaceGrantRejectsUnboundEvidencePostgres(t *testing.T) {
	f := workspaceGrantSystem(t)
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	for _, peer := range []owneridentity.Service{owneridentity.Build.Service(), owneridentity.Fabric.Service(), owneridentity.ConsoleBFF} {
		if _, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), peer), f.request); status.Code(err) != codes.PermissionDenied {
			t.Fatal("another peer issued a Workspace grant", peer, err)
		}
	}
	f.s.WorkspaceCommit = nil
	if _, err := f.s.IssueAcceptedOperationGrant(ctx, f.request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("grant issued without Workspace owner readback", err)
	}
	f.s.WorkspaceCommit = f.reader
	t.Run("scoped_authorization_cannot_retarget", func(t *testing.T) {
		if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.authorization_contexts SET resource_id='another-workspace' WHERE id=$1`, f.decision.GetAuthorizationContextId()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := f.db.ExecContext(context.Background(), `UPDATE tenant.authorization_contexts SET resource_id=NULL WHERE id=$1`, f.decision.GetAuthorizationContextId()); err != nil {
				t.Error(err)
			}
		})
		original := f.reader.actual
		defer func() { f.reader.actual = original }()
		r := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
		r.OwnerCommitEvidence.AuthorizationResource.Id = proto.String("another-workspace")
		f.reader.actual = r.OwnerCommitEvidence
		if _, err := f.s.IssueAcceptedOperationGrant(ctx, r); status.Code(err) != codes.PermissionDenied {
			t.Fatal("named Workspace authorization accepted a different resource", err)
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(*api.OwnerCommitEvidence)
	}{
		{"owner", func(e *api.OwnerCommitEvidence) { e.Owner = api.OwnerEnum_OWNER_ENUM_BUILD }},
		{"operation", func(e *api.OwnerCommitEvidence) { e.OperationId = "other-operation" }},
		{"resource", func(e *api.OwnerCommitEvidence) { e.ResourceId = "other-workspace" }},
		{"actor", func(e *api.OwnerCommitEvidence) { e.ActorId = "other-actor" }},
		{"scope", func(e *api.OwnerCommitEvidence) { e.Scope.GetTenant().TenantId = "other-tenant" }},
		{"digest", func(e *api.OwnerCommitEvidence) { e.AcceptedInputDigest = "other-digest" }},
		{"version", func(e *api.OwnerCommitEvidence) { e.CommittedVersion++ }},
	} {
		t.Run("claimed_"+tc.name, func(t *testing.T) {
			r := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
			tc.mutate(r.OwnerCommitEvidence)
			if _, err := f.s.IssueAcceptedOperationGrant(ctx, r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("caller-authored owner evidence accepted", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.OwnerCommitEvidence)
	}{
		{"actor", func(e *api.OwnerCommitEvidence) { e.ActorId = "other-actor" }},
		{"scope", func(e *api.OwnerCommitEvidence) { e.Scope.GetTenant().TenantId = "other-tenant" }},
		{"action", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE
		}},
		{"auth_resource", func(e *api.OwnerCommitEvidence) { e.AuthorizationResource.Id = proto.String("other-workspace") }},
		{"empty_operation", func(e *api.OwnerCommitEvidence) { e.OperationId = "" }},
		{"empty_resource", func(e *api.OwnerCommitEvidence) { e.ResourceId = "" }},
		{"empty_digest", func(e *api.OwnerCommitEvidence) { e.AcceptedInputDigest = "" }},
		{"uncommitted", func(e *api.OwnerCommitEvidence) { e.CommittedVersion = 0 }},
		{"before_authorization", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAt = timestamppb.New(f.decision.IssuedAt.AsTime().Add(-time.Second))
		}},
		{"after_authorization", func(e *api.OwnerCommitEvidence) {
			e.AcceptedAt = timestamppb.New(f.decision.ExpiresAt.AsTime().Add(time.Second))
		}},
		{"missing_acceptance_time", func(e *api.OwnerCommitEvidence) { e.AcceptedAt = nil }},
	} {
		t.Run("owner_"+tc.name, func(t *testing.T) {
			original := f.reader.actual
			defer func() { f.reader.actual = original }()
			r := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
			tc.mutate(r.OwnerCommitEvidence)
			f.reader.actual = r.OwnerCommitEvidence
			if _, err := f.s.IssueAcceptedOperationGrant(ctx, r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("owner acceptance escaped the authorization", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.AcceptedOperationGrantRequest)
	}{
		{"extra_action", func(r *api.AcceptedOperationGrantRequest) {
			r.AllowedActions = append(r.AllowedActions, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEQUOTE)
		}},
		{"duplicate_action", func(r *api.AcceptedOperationGrantRequest) {
			r.AllowedActions = append(r.AllowedActions, r.AllowedActions[0])
		}},
		{"no_actions", func(r *api.AcceptedOperationGrantRequest) { r.AllowedActions = nil }},
		{"renewal", func(r *api.AcceptedOperationGrantRequest) { r.RenewalConsentId = proto.String("renewal") }},
		{"period", func(r *api.AcceptedOperationGrantRequest) { r.SubscriptionPeriodId = proto.String("period") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
			tc.mutate(r)
			if _, err := f.s.IssueAcceptedOperationGrant(ctx, r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("grant scope expanded", err)
			}
		})
	}
	g := f.issue(t)
	for _, mutate := range []func(*api.AuthorizationRequest){
		func(r *api.AuthorizationRequest) { r.ActorId = "other-actor" },
		func(r *api.AuthorizationRequest) { r.Scope.GetTenant().TenantId = "other-tenant" },
	} {
		r := proto.Clone(workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES)).(*api.AuthorizationRequest)
		mutate(r)
		if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); status.Code(err) != codes.PermissionDenied {
			t.Fatal("grant changed the original actor or scope", err)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.OwnerCommitEvidence)
	}{
		{"owner", func(e *api.OwnerCommitEvidence) { e.Owner = api.OwnerEnum_OWNER_ENUM_BUILD }},
		{"operation", func(e *api.OwnerCommitEvidence) { e.OperationId = "other-operation" }},
		{"resource", func(e *api.OwnerCommitEvidence) { e.ResourceId = "other-workspace" }},
		{"actor", func(e *api.OwnerCommitEvidence) { e.ActorId = "other-actor" }},
		{"scope", func(e *api.OwnerCommitEvidence) { e.Scope.GetTenant().TenantId = "other-tenant" }},
		{"digest", func(e *api.OwnerCommitEvidence) { e.AcceptedInputDigest = "" }},
		{"version", func(e *api.OwnerCommitEvidence) { e.CommittedVersion = 0 }},
	} {
		t.Run("readback_"+tc.name, func(t *testing.T) {
			original := f.reader.actual
			defer func() { f.reader.actual = original }()
			f.reader.actual = proto.Clone(original).(*api.OwnerCommitEvidence)
			tc.mutate(f.reader.actual)
			r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("substituted owner readback authorized continuation", err)
			}
		})
	}
}

func TestWorkspaceGrantCloseoutPostgres(t *testing.T) {
	f := workspaceGrantSystem(t)
	g := f.issue(t)
	for _, tc := range []struct {
		name string
		sql  string
	}{
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
				owner, peer := workspaceAudience(action)
				r := workspaceContinuation(g, owner, action)
				_, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), r)
				readOrRelease := action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE || action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT
				if readOrRelease && err != nil {
					t.Fatalf("original closeout %s refused: %v", action, err)
				}
				if !readOrRelease && status.Code(err) != codes.PermissionDenied {
					t.Fatalf("new effect %s allowed after authority ended: %v", action, err)
				}
			}
			var mode string
			if err := f.db.QueryRowContext(t.Context(), `SELECT mode FROM tenant.accepted_operation_grants WHERE id=$1`, g.Id).Scan(&mode); err != nil || mode != "closeout_only" {
				t.Fatal("grant did not converge to closeout", mode, err)
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET revoked_at=NULL,role='admin';UPDATE tenant.tenants SET status='active'`); err != nil {
				t.Fatal(err)
			}
			r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); status.Code(err) != codes.PermissionDenied {
				t.Fatal("restored membership resurrected a closed-out grant", err)
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.accepted_operation_grants SET mode='continue_original'`); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"revoked_mode", `UPDATE tenant.accepted_operation_grants SET mode='revoked'`},
		{"revoked_grant", `UPDATE tenant.accepted_operation_grants SET revoked_at=now()`},
		{"completed", `UPDATE tenant.accepted_operation_grants SET obligation_completed_at=now()`},
		{"expired", `UPDATE tenant.accepted_operation_grants SET issued_at=now()-interval '2 hours',expires_at=now()-interval '1 hour'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.db.ExecContext(t.Context(), tc.sql); err != nil {
				t.Fatal(err)
			}
			r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); err == nil {
				t.Fatal("ended grant still authorized observation")
			}
			if _, err := f.db.ExecContext(t.Context(), `UPDATE tenant.accepted_operation_grants SET mode='continue_original',revoked_at=NULL,obligation_completed_at=NULL,expires_at=NULL`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
