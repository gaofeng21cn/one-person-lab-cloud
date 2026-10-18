package identity_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// revocationAssembly builds the production Cloud issuer seam used by these
// lifecycle tests. External Gateway user-key creation is covered by the focused
// adapter tests; these tests only need a confirmed key before exercising the
// deliberately unimplemented lifecycle boundary.
func revocationAssembly(t *testing.T) (identity.ManagedKeyIssuer, *atomic.Int32) {
	t.Helper()
	var issuanceCalls atomic.Int32
	return &productionUserKeyIssuer{issuanceCalls: &issuanceCalls}, &issuanceCalls
}

type productionUserKeyIssuer struct{ issuanceCalls *atomic.Int32 }

func (i *productionUserKeyIssuer) IssueWorkspaceKey(context.Context, identity.ManagedKeyIssueRequest) (identity.ManagedKeyIssueResult, error) {
	i.issuanceCalls.Add(1)
	return identity.ManagedKeyIssueResult{Raw: "sk-managed", ExternalKeyID: "4242", GroupID: 9, ModelIDs: []string{"model-a"}}, nil
}
func (*productionUserKeyIssuer) RevokeWorkspaceKey(context.Context, string, string) error {
	return status.Error(codes.Unimplemented, "no approved Gateway user-key revocation operation is configured")
}
func (*productionUserKeyIssuer) SetWorkspaceKeyState(context.Context, string, string, identity.ManagedKeyExternalState) error {
	return status.Error(codes.Unimplemented, "no approved Gateway user-key lifecycle operation is configured")
}
func (*productionUserKeyIssuer) ReadWorkspaceKeyState(context.Context, string, string) (identity.ManagedKeyExternalState, error) {
	return "", status.Error(codes.Unimplemented, "no approved Gateway user-key lifecycle readback is configured")
}

// scriptedRevokeIssuer records the production refusal but can also script a lost
// outcome, so one test can show a definite refusal and an unknown attempt are
// distinguishable and neither erases the other.
type scriptedRevokeIssuer struct {
	mu      sync.Mutex
	results []error
	calls   int
}

func (s *scriptedRevokeIssuer) IssueWorkspaceKey(_ context.Context, _ identity.ManagedKeyIssueRequest) (identity.ManagedKeyIssueResult, error) {
	return identity.ManagedKeyIssueResult{Raw: "sk-managed", ExternalKeyID: "4242", GroupID: 9, ModelIDs: []string{"model-a"}}, nil
}

func (s *scriptedRevokeIssuer) RevokeWorkspaceKey(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.results) == 0 {
		return status.Error(codes.Unimplemented, "no approved Sub2API service revocation endpoint is configured")
	}
	result := s.results[0]
	if len(s.results) > 1 {
		s.results = s.results[1:]
	}
	return result
}

func (s *scriptedRevokeIssuer) SetWorkspaceKeyState(context.Context, string, string, identity.ManagedKeyExternalState) error {
	return status.Error(codes.Unimplemented, "no approved Sub2API service key disable/enable endpoint is configured")
}

func (s *scriptedRevokeIssuer) ReadWorkspaceKeyState(context.Context, string, string) (identity.ManagedKeyExternalState, error) {
	return "", status.Error(codes.Unimplemented, "no approved Sub2API service key-state readback endpoint is configured")
}

// recordedAttempts reads this owner's revocation records straight from the table,
// so the assertions do not depend on the same helper the fix relies on.
func recordedAttempts(t *testing.T, service *identity.Service) map[string]string {
	t.Helper()
	rows, err := service.GatewayStore.DB().QueryContext(t.Context(),
		`SELECT resource_id, COALESCE(response_body->>'outcome',''), COALESCE(response_body->>'reasonCode','')
		 FROM gateway.idempotency_records WHERE operation_name='RevokeManagedKey'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var id, outcome, reason string
		if err = rows.Scan(&id, &outcome, &reason); err != nil {
			t.Fatal(err)
		}
		found[id] = outcome + "/" + reason
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// TestRevokeManagedKeyRecordsTheRefusalUnderTheProductionIssuer is the control
// for issue #821. The production issuer refuses every revocation, so
// RevokeManagedKey always fails; the failure used to leave the binding
// byte-identical to one nobody ever asked to revoke. After the fix the attempt is
// a durable first-class record, while the key is still reported as not revoked.
func TestRevokeManagedKeyRecordsTheRefusalUnderTheProductionIssuer(t *testing.T) {
	issuer, issuanceCalls := revocationAssembly(t)
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if issuanceCalls.Load() != 1 {
		t.Fatalf("issuance calls=%d want 1: the production issuer must stay a live client", issuanceCalls.Load())
	}
	before := recordedAttempts(t, service)

	_, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("revoke error=%v want Unimplemented: this owner has no approved revocation route and must keep saying so", err)
	}
	if issuanceCalls.Load() != 1 {
		t.Fatalf("issuance calls=%d want 1: a revocation must never contact Sub2API", issuanceCalls.Load())
	}
	// The key itself must be untouched: recording an attempt is not revoking.
	binding, readErr := service.GatewayStore.ReadManagedKeyBinding(t.Context(), issued.GetKeyBindingId())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !binding.RevokedAt.IsZero() {
		t.Fatalf("a refused revocation marked the key revoked at %s", binding.RevokedAt)
	}

	// THE CONTROL: the refusal is a durable fact of its own, recorded outside the
	// binding so it cannot be confused with the issuance's 'confirmed'.
	after := recordedAttempts(t, service)
	if len(after) != len(before)+1 {
		t.Fatalf("recorded attempts=%d want %d: the refusal left no record", len(after), len(before)+1)
	}
	if got := after[issued.GetKeyBindingId()]; got != "refused/Unimplemented" {
		t.Fatalf("recorded attempt=%q want refused/Unimplemented", got)
	}
	// observation_result keeps exactly the two meanings it always had.
	if binding.ObservationResult != "confirmed" {
		t.Fatalf("observation_result=%q: the revocation record must not have been written into the binding's issuance column", binding.ObservationResult)
	}
	listed, err := service.GatewayStore.ListUnconfirmedManagedKeyRevocations(t.Context(), "tenant-coord")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].KeyBindingID != issued.GetKeyBindingId() ||
		listed[0].Outcome != "refused" || listed[0].ReasonCode != codes.Unimplemented.String() {
		t.Fatalf("listed=%+v", listed)
	}
}

// TestRevokeManagedKeyRecordsTheDefaultProductionState covers the state a
// deployment is actually in by default: Sub2API is not configured, so no issuer
// is wired at all. This used to return FailedPrecondition before anything could be
// recorded, which made the most common way this revoke fails the one way that
// left no trace.
func TestRevokeManagedKeyRecordsTheDefaultProductionState(t *testing.T) {
	issuer, _ := revocationAssembly(t)
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	// The deployment drops the Sub2API configuration and restarts.
	service.KeyIssuer = nil

	_, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke-no-issuer", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("revoke error=%v want FailedPrecondition", err)
	}
	if got := recordedAttempts(t, service)[issued.GetKeyBindingId()]; got != "refused/FailedPrecondition" {
		t.Fatalf("recorded attempt=%q want refused/FailedPrecondition: the default production state must still leave a trace", got)
	}
}

// TestRevokeManagedKeptDistinctAttemptOutcomes proves the cause is kept, not
// flattened: Unimplemented (definitely not revoked) and Unavailable (possibly
// revoked, answer lost) stay distinguishable, and a definite refusal is not
// erased by a later vaguer attempt.
func TestRevokeManagedKeptDistinctAttemptOutcomes(t *testing.T) {
	issuer := &scriptedRevokeIssuer{results: []error{
		status.Error(codes.Unavailable, "the revocation authority is unavailable"),
		status.Error(codes.Unimplemented, "no approved Sub2API service revocation endpoint is configured"),
	}}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if _, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke-lost", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	}); status.Code(err) != codes.Unavailable {
		t.Fatalf("first revoke error=%v want Unavailable", err)
	}
	if _, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke-refused", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("second revoke error=%v want Unimplemented", err)
	}
	var unknown, refused int
	for _, attempt := range mustListUnconfirmed(t, service) {
		switch attempt.Outcome {
		case "unknown":
			if attempt.ReasonCode != codes.Unavailable.String() {
				t.Fatalf("unknown attempt carries reason %q", attempt.ReasonCode)
			}
			unknown++
		case "refused":
			if attempt.ReasonCode != codes.Unimplemented.String() {
				t.Fatalf("refused attempt carries reason %q", attempt.ReasonCode)
			}
			refused++
		default:
			t.Fatalf("unexpected recorded outcome %q", attempt.Outcome)
		}
	}
	if unknown != 1 || refused != 1 {
		t.Fatalf("recorded attempts: unknown=%d refused=%d want 1 and 1: a later attempt must not erase the earlier one", unknown, refused)
	}
}

// TestRevokeManagedKeptReplayKeepsItsFirstAnswer proves one command identity is
// one recorded answer: replaying it cannot re-decide a definite refusal.
func TestRevokeManagedKeptReplayKeepsItsFirstAnswer(t *testing.T) {
	issuer := &scriptedRevokeIssuer{}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	command := &api.ManagedKeyRevoke{Context: grantedCaller("revoke-replay", "grant-workspace"),
		KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1"}
	if _, err = workspace.RevokeManagedKey(t.Context(), command); status.Code(err) != codes.Unimplemented {
		t.Fatalf("first revoke error=%v", err)
	}
	if _, err = workspace.RevokeManagedKey(t.Context(), command); status.Code(err) != codes.Unimplemented {
		t.Fatalf("replay revoke error=%v", err)
	}
	attempts := mustListUnconfirmed(t, service)
	if len(attempts) != 1 {
		t.Fatalf("recorded attempts=%d want 1: a replayed command is one recorded answer", len(attempts))
	}
}

// TestRevokeManagedKeyReportsAFailedRecording proves a refusal that could not be
// written down is reported instead of swallowed. Silently dropping it would leave
// the system in exactly the state this change exists to end.
func TestRevokeManagedKeyReportsAFailedRecording(t *testing.T) {
	issuer, _ := revocationAssembly(t)
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	// The recording table goes away after the key exists, so the issuer refuses
	// and the refusal then cannot be recorded at all. Dropping it needs the owner
	// role, so this borrows the same admin DSN the isolated fixtures run on.
	admin, ok := ownerstoretest.AdminDSN(os.Getenv)
	if !ok {
		t.Skip("set " + ownerstoretest.AdminDSNEnv + " to exercise the unrecordable refusal path")
	}
	parsed, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/opl_gateway"
	adminDB, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminDB.Close() })
	if _, err = adminDB.ExecContext(t.Context(), `DROP TABLE gateway.idempotency_records`); err != nil {
		t.Fatal(err)
	}
	_, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke-unrecordable", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("revoke error=%v: the issuer's own refusal must not be masked by a recording failure", err)
	}
	if !strings.Contains(status.Convert(err).Message(), "could not be recorded") {
		t.Fatalf("revoke error=%v: an unrecordable refusal was swallowed", err)
	}
}

// TestRevokeManagedKeyStillRefuses guards the discipline that matters most here.
// Making the refusal observable must not turn an absent capability into a
// success. This fails loudly if anyone later rewrites the Unimplemented return as
// a nil error.
func TestRevokeManagedKeyStillRefuses(t *testing.T) {
	issuer, _ := revocationAssembly(t)
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	operation, err := workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context: grantedCaller("revoke", "grant-workspace"), KeyBindingId: issued.GetKeyBindingId(), WorkspaceId: "ws-1",
	})
	if err == nil {
		t.Fatalf("a revoke with no approved route reported success: %v", operation)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("revoke error=%v want Unimplemented", err)
	}
	if operation != nil {
		t.Fatalf("a refused revoke returned an Operation: %v", operation)
	}
	binding, readErr := service.GatewayStore.ReadManagedKeyBinding(t.Context(), issued.GetKeyBindingId())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !binding.RevokedAt.IsZero() {
		t.Fatalf("the key was recorded revoked without an approved revocation route: %s", binding.RevokedAt)
	}
	if binding.GatewayKeyContract().GetStatus() == api.GatewayKeyStatusEnum_GATEWAY_KEY_STATUS_ENUM_REVOKED {
		t.Fatal("the read model reports the key revoked although nothing revoked it")
	}
}

func mustListUnconfirmed(t *testing.T, service *identity.Service) []identity.ManagedKeyRevocationAttempt {
	t.Helper()
	attempts, err := service.GatewayStore.ListUnconfirmedManagedKeyRevocations(t.Context(), "tenant-coord")
	if err != nil {
		t.Fatal(err)
	}
	return attempts
}
