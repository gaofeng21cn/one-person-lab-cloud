package launch

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

// TestRenewalEvidenceNamesTheRenewalAction proves the commit evidence of a
// renewal names the renewal action and the Workspace it continues. A renewal
// that carried the launch's action would ask for the launch's grant and could
// replay a launch.
func TestRenewalEvidenceNamesTheRenewalAction(t *testing.T) {
	op, offer, _ := orderFixture()
	op.Kind = renewalKind
	input, _ := json.Marshal(acceptedOrder{Quote: wire(offer), AuthorizationContextID: "authorization-renewal", InputDigest: "sha256:renewal"})
	op.AcceptedInput, op.Result = input, json.RawMessage(`{}`)
	commit, err := renewalEvidence(op)
	if err != nil {
		t.Fatal(err)
	}
	if commit.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || commit.GetOperationId() != op.ID || commit.GetResourceId() != op.ResourceID {
		t.Fatalf("renewal commit identity=%v", commit)
	}
	if commit.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE {
		t.Fatalf("renewal commit action=%v", commit.GetAcceptedAction())
	}
	if commit.GetAuthorizationContextId() != "authorization-renewal" || commit.GetAcceptedInputDigest() != "sha256:renewal" {
		t.Fatalf("renewal commit acceptance=%v", commit)
	}
	if len(commit.GetContinuationResources()) != 1 || commit.GetContinuationResources()[0].GetId() != op.ResourceID {
		t.Fatalf("renewal commit resources=%v", commit.GetContinuationResources())
	}
	// The launch's own commit keeps naming the launch action: one owner, two
	// obligations, two commit identities.
	launch := op
	launch.Kind = "create_workspace"
	launchCommit, err := evidence(launch)
	if err != nil {
		t.Fatal(err)
	}
	if launchCommit.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE {
		t.Fatalf("launch commit action=%v", launchCommit.GetAcceptedAction())
	}
}

// TestRenewalContinuationSurfaceExcludesProvisioningAndRelease proves the
// renewal grant carries exactly the renewal surface. It may read the offer,
// charge it once, record its receipt and extend the prepaid period of the
// resources the Workspace already owns; it may never create resources, release
// them or refund a charge.
func TestRenewalContinuationSurfaceExcludesProvisioningAndRelease(t *testing.T) {
	allowed := map[api.AuthorizationActionEnum]bool{}
	for _, action := range renewalContinuationActions {
		if allowed[action] {
			t.Fatalf("duplicate renewal action %v", action)
		}
		allowed[action] = true
	}
	for _, action := range []api.AuthorizationActionEnum{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESIZEACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REFUNDPLANCHANGECOMPENSATION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REFUNDCONFIRMEDDELETION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE,
	} {
		if allowed[action] {
			t.Fatalf("renewal grant carries %v", action)
		}
	}
	for _, action := range []api.AuthorizationActionEnum{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READWALLETACTION,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT,
	} {
		if !allowed[action] {
			t.Fatalf("renewal grant misses %v", action)
		}
	}
}

// TestRenewalFailureNamesEachRefusal proves every refusal of a renewal has its
// own code. A renewal that ended its paid term is not a lost dependency, a
// Workspace without confirmed resources is not an ended term, and neither is a
// provider that refused to extend the prepaid deadline.
func TestRenewalFailureNamesEachRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		step string
		code codes.Code
		want string
	}{
		"ended term":     {step: "renewable_term", code: codes.FailedPrecondition, want: "RENEWAL_PERIOD_ELAPSED"},
		"gone resources": {step: "read_resources", code: codes.FailedPrecondition, want: "WORKSPACE_NOT_READY"},
		"provider":       {step: "renew_resources", code: codes.FailedPrecondition, want: "OWNER_CAPABILITY_UNAVAILABLE"},
		"refused offer":  {step: "accept_quote", code: codes.FailedPrecondition, want: "QUOTE_MISMATCH"},
		"repeat":         {step: "renewable_term", code: codes.AlreadyExists, want: "VERSION_CONFLICT"},
		"stale offer":    {step: "accept_quote", code: codes.NotFound, want: "QUOTE_MISMATCH"},
	} {
		t.Run(name, func(t *testing.T) {
			state, observation, code := renewalFailure(tc.step, status.Error(tc.code, "refused"))
			if state != "failed" || observation != "rejected" || code != tc.want {
				t.Fatalf("renewal failure state=%q observation=%q code=%q", state, observation, code)
			}
		})
	}
	// A lost answer is not a refusal: the renewal stays pending, because a lost
	// answer never proves that the customer was not charged.
	state, observation, code := renewalFailure("wallet_charge", status.Error(codes.Unavailable, "answer lost"))
	if state != "awaiting_confirmation" || observation != "unknown" || code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("lost answer state=%q observation=%q code=%q", state, observation, code)
	}
}

// TestRenewalOperationProjectsTheRenewalKind proves the renewal operation the
// caller polls is the renewal, not the launch it continues, and that its own
// refusal code survives the projection.
func TestRenewalOperationProjectsTheRenewalKind(t *testing.T) {
	op, _, _ := orderFixture()
	op.Kind = renewalKind
	op.Stage = "provider_renewal"
	op.Status = "awaiting_confirmation"
	op.Observation = "unknown"
	projected, err := renewalOperation(op)
	if err != nil {
		t.Fatal(err)
	}
	if projected.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RENEW_WORKSPACE || projected.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE {
		t.Fatalf("renewal operation kind=%v owner=%v", projected.GetKind(), projected.GetOwner())
	}
	if projected.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_PROVIDER_RENEWAL || projected.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION {
		t.Fatalf("renewal operation stage=%v status=%v", projected.GetStage(), projected.GetStatus())
	}
	if projected.GetPollAfterSeconds() != 5 {
		t.Fatalf("renewal operation poll=%v", projected.GetPollAfterSeconds())
	}
	// The create operation keeps its own kind: one owner, two projections.
	launch := op
	launch.Kind = "create_workspace"
	launchProjection, err := operation(launch)
	if err != nil {
		t.Fatal(err)
	}
	if launchProjection.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_WORKSPACE {
		t.Fatalf("launch operation kind=%v", launchProjection.GetKind())
	}
}
