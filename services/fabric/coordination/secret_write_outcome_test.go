package coordination_test

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/services/fabric/internal/fabric"
)

// managedSecretOutcomeCase is one provider outcome the managed-Secret write
// boundary must classify. The error is placed verbatim on the dispatcher
// boundary, exactly as the local or Tencent provider returns it from
// UpsertGatewaySecret.
type managedSecretOutcomeCase struct {
	workspace   string
	providerErr error
	want        codes.Code
}

// runManagedSecretOutcomes runs each case through the real gRPC boundary and
// returns the observed status code per case name.
func runManagedSecretOutcomes(t *testing.T, cases map[string]managedSecretOutcomeCase) map[string]codes.Code {
	t.Helper()
	dispatcher := &managedSecretDispatcher{}
	tenant, _, _ := managedSecretSystem(t, dispatcher)
	observed := map[string]codes.Code{}
	for name, tc := range cases {
		dispatcher.providerErr = tc.providerErr
		command := managedSecretCommand(tc.workspace, "raw-key-value-for-"+tc.workspace, "op-outcome:"+tc.workspace+":put_managed_secret")
		_, err := tenant.PutManagedSecret(t.Context(), command)
		observed[name] = status.Code(err)
	}
	return observed
}

// TestPutManagedSecretMapsTypedRefusalsToNonRetryableStatus pins the two
// definite refusals the reachable PutManagedSecret path produces as typed
// sentinels. Fabric's own operation claim raises
// fabric.ErrGatewaySecretIdempotencyConflict when one idempotency key is reused
// for a different Secret (services/fabric/internal/fabric/workspace_runtime.go),
// and the provider readback raises fabric.ErrLaunchStageBindingConflict when
// the stored Secret identity does not match the requested identity (the Tencent
// adapter's decodeTencentGatewaySecretReadback and the local adapter's
// ReadGatewaySecretByDigest). Neither may surface as the retryable Unavailable
// outcome the boundary reserves for an unconfirmed write, so a caller cannot
// loop on a refusal.
func TestPutManagedSecretMapsTypedRefusalsToNonRetryableStatus(t *testing.T) {
	cases := map[string]managedSecretOutcomeCase{
		"operation claim idempotency conflict": {workspace: "workspace-managed-secret-typed-conflict", providerErr: fabric.ErrGatewaySecretIdempotencyConflict, want: codes.AlreadyExists},
		"readback identity conflict":           {workspace: "workspace-managed-secret-readback-conflict", providerErr: fabric.ErrLaunchStageBindingConflict, want: codes.FailedPrecondition},
	}
	observed := runManagedSecretOutcomes(t, cases)
	for name, tc := range cases {
		got := observed[name]
		if got != tc.want {
			t.Errorf("%s: status=%v want %v (a typed refusal must not be reported as retryable)", name, got, tc.want)
		}
		if got == codes.Unavailable {
			t.Errorf("%s: a typed refusal was reported as retryable unavailable", name)
		}
	}
}

// TestPutManagedSecretKeepsUnclassifiedProviderFailuresRetryable proves every
// provider failure that this call path does not produce as a typed definite
// refusal stays unknown and retryable (Unavailable):
//   - the production adapter returns a plain kubectl error for a provider
//     refusal such as an RBAC rejection, with no typed signal to recognize it;
//   - an absent readback means the write is not confirmed yet
//     (decodeTencentGatewaySecretReadback returns ErrWorkspaceLaunchResourceAbsent);
//   - the launch-chain sentinels (a frozen closeout, launch input validation,
//     compute dispatch pending, ownership pending) are raised only by the
//     Workspace launch chain, never by PutManagedSecret, so the mapping must
//     not assert a refusal for them.
func TestPutManagedSecretKeepsUnclassifiedProviderFailuresRetryable(t *testing.T) {
	plainKubectlRefusal := errors.New(`exec: exit status 1: Error from server (Forbidden): secrets "opl-gateway-workspace-managed-secret-plain" is forbidden: User "system:serviceaccount:opl:opl-fabric" cannot get resource "secrets" in API group "" in the namespace "opl"`)
	cases := map[string]managedSecretOutcomeCase{
		"plain kubectl refusal":           {workspace: "workspace-managed-secret-plain", providerErr: plainKubectlRefusal, want: codes.Unavailable},
		"absent provider readback":        {workspace: "workspace-managed-secret-absent", providerErr: fabric.ErrWorkspaceLaunchResourceAbsent, want: codes.Unavailable},
		"launch-chain pending":            {workspace: "workspace-managed-secret-launch-pending", providerErr: fabric.ErrWorkspaceLaunchPending, want: codes.Unavailable},
		"launch closeout frozen":          {workspace: "workspace-managed-secret-launch-frozen", providerErr: fabric.ErrWorkspaceLaunchFrozen, want: codes.Unavailable},
		"launch input validation":         {workspace: "workspace-managed-secret-launch-invalid", providerErr: fabric.ErrWorkspaceLaunchInputInvalid, want: codes.Unavailable},
		"launch compute dispatch pending": {workspace: "workspace-managed-secret-launch-compute", providerErr: fabric.ErrWorkspaceLaunchComputeDispatchPending, want: codes.Unavailable},
		"launch ownership pending":        {workspace: "workspace-managed-secret-launch-ownership", providerErr: fabric.ErrWorkspaceLaunchOwnershipPending, want: codes.Unavailable},
	}
	observed := runManagedSecretOutcomes(t, cases)
	for name, tc := range cases {
		if got := observed[name]; got != tc.want {
			t.Errorf("%s: status=%v want %v (an unproven refusal must stay unknown and retryable)", name, got, tc.want)
		}
	}
}
