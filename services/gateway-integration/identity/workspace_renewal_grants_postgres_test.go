package identity_test

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerservice"
)

// renewalGrantFixture is the renewal counterpart of the launch grant: one
// accepted renew_workspace obligation and the bounded group it may carry.
type renewalGrantFixture struct {
	s        *identity.Service
	session  string
	decision *api.AuthorizationDecision
	reader   *workspaceCommitReader
	request  *api.AcceptedOperationGrantRequest
}

func renewalGrantSystem(t *testing.T) renewalGrantFixture {
	t.Helper()
	s, _, client, _ := system(t)
	raw, _ := login(t, client)
	ref := owneridentity.SessionReference(raw)
	r := &api.AuthorizationRequest{
		Scope:         &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}},
		ActorId:       "101",
		SessionId:     &ref,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-renewal")},
		RequestId:     "renew-workspace",
	}
	d, err := s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), r)
	if err != nil {
		t.Fatal(err)
	}
	proof := &api.OwnerCommitEvidence{
		Owner:                  api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		OperationId:            "workspace-renewal-operation",
		ResourceId:             "workspace-renewal",
		AcceptedInputDigest:    "sha256:" + strings.Repeat("d", 64),
		CommittedVersion:       1,
		AcceptedAt:             timestamppb.Now(),
		AuthorizationContextId: d.GetAuthorizationContextId(),
		ActorId:                r.ActorId,
		Scope:                  r.Scope,
		AcceptedAction:         r.Action,
		AuthorizationResource:  r.Resource,
	}
	reader := &workspaceCommitReader{actual: proof}
	s.WorkspaceCommit = reader
	request := &api.AcceptedOperationGrantRequest{
		AuthorizationContextId: d.GetAuthorizationContextId(),
		OwnerCommitEvidence:    proto.Clone(proof).(*api.OwnerCommitEvidence),
		AllowedActions: []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READWALLETACTION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT,
		},
	}
	return renewalGrantFixture{s: s, session: ref, decision: d, reader: reader, request: request}
}

// renewalAudience names the owner and the peer that carry one renewal
// continuation.
func renewalAudience(action api.AuthorizationActionEnum) (api.OwnerEnum, owneridentity.Service) {
	switch action {
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES:
		return api.OwnerEnum_OWNER_ENUM_FABRIC, owneridentity.Fabric.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION:
		return api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, owneridentity.ResourceCatalog.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT:
		return api.OwnerEnum_OWNER_ENUM_LEDGER, owneridentity.Ledger.Service()
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READWALLETACTION:
		return api.OwnerEnum_OWNER_ENUM_GATEWAY, owneridentity.Gateway.Service()
	default:
		return api.OwnerEnum_OWNER_ENUM_FABRIC, owneridentity.Fabric.Service()
	}
}

// TestWorkspaceRenewalGrantAuthorizesTheProviderExtension proves that an
// accepted renewal obligation can reach the provider extension and the charge
// that funds it, and that the renewal surface stays separate from the launch
// and deletion surfaces.
func TestWorkspaceRenewalGrantAuthorizesTheProviderExtension(t *testing.T) {
	f := renewalGrantSystem(t)
	g, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if g.AcceptedOperationOwner != api.OwnerEnum_OWNER_ENUM_WORKSPACE || g.AcceptedAction != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE || g.ResourceId != "workspace-renewal" {
		t.Fatal("renewal grant lost its Workspace owner/action/resource", g)
	}
	t.Run("the provider extension is admitted at the Fabric audience", func(t *testing.T) {
		r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES)
		if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); err != nil {
			t.Fatal("the renewal grant could not reach the provider extension", err)
		}
	})
	t.Run("every renewal continuation is admitted at its own owner", func(t *testing.T) {
		for _, action := range f.request.AllowedActions {
			owner, peer := renewalAudience(action)
			r := workspaceContinuation(g, owner, action)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), r); err != nil {
				t.Fatalf("renewal continuation %s was rejected: %v", action, err)
			}
			wrong := proto.Clone(r).(*api.AuthorizationRequest)
			wrong.Resource.Id = proto.String("unrelated-workspace")
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), peer), wrong); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("renewal grant escaped its own workspace for %s: %v", action, err)
			}
		}
	})
	t.Run("a renewal grant cannot provision, release or refund", func(t *testing.T) {
		for _, action := range []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEACCEPTEDRESOURCES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REFUNDCONFIRMEDDELETION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET,
		} {
			r := workspaceContinuation(g, api.OwnerEnum_OWNER_ENUM_FABRIC, action)
			if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("the renewal grant carried %s: %v", action, err)
			}
		}
	})
	t.Run("a renewal grant is refused an action outside its group", func(t *testing.T) {
		request := proto.Clone(f.request).(*api.AcceptedOperationGrantRequest)
		request.AllowedActions = append(request.AllowedActions, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES)
		if _, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), request); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("the renewal grant was widened: %v", err)
		}
	})
	t.Run("a launch grant cannot reach the provider extension", func(t *testing.T) {
		launchRequest := &api.AuthorizationRequest{
			Scope:         f.request.GetOwnerCommitEvidence().GetScope(),
			ActorId:       f.request.GetOwnerCommitEvidence().GetActorId(),
			SessionId:     &f.session,
			AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
			Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE,
			Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-launch")},
			RequestId:     "launch-workspace",
		}
		launchDecision, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), launchRequest)
		if err != nil {
			t.Fatal(err)
		}
		launchProof := &api.OwnerCommitEvidence{
			Owner:                  api.OwnerEnum_OWNER_ENUM_WORKSPACE,
			OperationId:            "workspace-launch-operation",
			ResourceId:             "workspace-launch",
			AcceptedInputDigest:    "sha256:" + strings.Repeat("e", 64),
			CommittedVersion:       1,
			AcceptedAt:             timestamppb.Now(),
			AuthorizationContextId: launchDecision.GetAuthorizationContextId(),
			ActorId:                launchRequest.ActorId,
			Scope:                  launchRequest.Scope,
			AcceptedAction:         launchRequest.Action,
			AuthorizationResource:  launchRequest.Resource,
		}
		launchReader := &workspaceCommitReader{actual: launchProof}
		f.s.WorkspaceCommit = launchReader
		defer func() { f.s.WorkspaceCommit = f.reader }()
		launchGrant, err := f.s.IssueAcceptedOperationGrant(ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service()), &api.AcceptedOperationGrantRequest{
			AuthorizationContextId: launchDecision.GetAuthorizationContextId(),
			OwnerCommitEvidence:    launchProof,
			AllowedActions: []api.AuthorizationActionEnum{
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		// The launch grant is otherwise a working grant: only the renewal
		// action is outside its surface.
		provision := workspaceContinuation(launchGrant, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES)
		if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), provision); err != nil {
			t.Fatal("the launch grant lost its own provisioning surface", err)
		}
		r := workspaceContinuation(launchGrant, api.OwnerEnum_OWNER_ENUM_FABRIC, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES)
		if _, err := f.s.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.Fabric.Service()), r); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("the launch grant reached the provider extension: %v", err)
		}
	})
}
