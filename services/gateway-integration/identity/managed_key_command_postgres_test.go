package identity_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
)

// managedKeyIssuerStub is one approved Gateway key issuer. It counts issuances,
// can record whether the durable original command already existed when the
// external issuance was attempted, and can drop its response after the key
// exists, so a test can prove the owner never mints a second key.
type managedKeyIssuerStub struct {
	mu             sync.Mutex
	issues         int
	revokes        int
	applyThenFail  bool
	refuse         bool
	probe          func() bool
	registered     bool
	probed         bool
	lastRequest    *identity.ManagedKeyIssueRequest
	resolvedModels []string
}

// resolvedScope models the issuance authority resolving an empty declared scope
// into the approved concrete group allowlist. An explicit declared selection is
// returned as-is, exactly like the real service does for a bound group.
func (s *managedKeyIssuerStub) resolvedScope(declared []string) []string {
	if len(declared) > 0 {
		return append([]string(nil), declared...)
	}
	if len(s.resolvedModels) > 0 {
		return append([]string(nil), s.resolvedModels...)
	}
	return []string{"default-model"}
}

func (s *managedKeyIssuerStub) IssueWorkspaceKey(_ context.Context, request identity.ManagedKeyIssueRequest) (identity.ManagedKeyIssueResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues++
	s.lastRequest = &request
	if s.probe != nil {
		s.registered = s.probe()
		s.probed = true
	}
	if s.refuse {
		return identity.ManagedKeyIssueResult{}, status.Error(codes.PermissionDenied, "model not allowed for this subject")
	}
	if s.applyThenFail {
		// The external key exists; its response is lost. The owner never repeats
		// this call without an approved readback of the original issue identity.
		return identity.ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "response lost after key creation")
	}
	declared := append([]string(nil), request.ModelIDs...)
	resolved := s.resolvedScope(declared)
	return identity.ManagedKeyIssueResult{
		Raw:           "raw-managed-key-" + request.WorkspaceID,
		ExternalKeyID: managedKeyExternalIDForTest(request.WorkspaceID, resolved),
		GroupID:       9, ModelIDs: resolved,
	}, nil
}

func (s *managedKeyIssuerStub) RevokeWorkspaceKey(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokes++
	return nil
}

func (s *managedKeyIssuerStub) issued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issues
}

func (s *managedKeyIssuerStub) registrationObserved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probed && s.registered
}

// managedKeyExternalIDForTest is the deterministic numeric external key id the
// fixture's authority issues, matching the real service contract (key_id is a
// positive integer).
func managedKeyExternalIDForTest(workspaceID string, resolved []string) string {
	digest := sha256.Sum256([]byte(workspaceID + ":" + strings.Join(resolved, "_")))
	return strconv.FormatInt(int64(binary.BigEndian.Uint64(digest[:8])%100000)+1, 10)
}

// managedKeySecretStore is one approved Secret store fixture. It keeps the raw
// value out of its returned delivery and can fail the write to model an
// unconfirmed delivery.
type managedKeySecretStore struct {
	mu                     sync.Mutex
	writes                 int
	fail                   bool
	refs                   map[string]string
	lastRaw                string
	lastHash               string
	lastFingerprint        string
	lastExternalKeyID      string
	lastCallContextMissing bool
	lastCall               *api.CallContext
	referenceOverride      string
	fingerprintOverride    string
}

func (s *managedKeySecretStore) PutSecret(_ context.Context, delivery identity.SecretDelivery) (identity.SecretDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	s.lastRaw = delivery.Raw
	s.lastFingerprint = delivery.Fingerprint
	s.lastExternalKeyID = delivery.ExternalKeyID
	s.lastCallContextMissing = delivery.Call == nil
	if delivery.Call != nil {
		s.lastCall = proto.Clone(delivery.Call).(*api.CallContext)
	}
	if s.fail {
		return identity.SecretDelivery{}, status.Error(codes.Unavailable, "approved Secret store unavailable")
	}
	if s.refs == nil {
		s.refs = map[string]string{}
	}
	ref, ok := s.refs[delivery.WorkspaceID]
	if !ok {
		// The approved store returns the Workspace's canonical Gateway Secret
		// reference, exactly as Fabric's provider readback does.
		ref = contracts.WorkspaceGatewaySecretRef(delivery.WorkspaceID)
		s.refs[delivery.WorkspaceID] = ref
	}
	delivered := delivery
	delivered.Reference = ref
	if s.referenceOverride != "" {
		delivered.Reference = s.referenceOverride
	}
	if s.fingerprintOverride != "" {
		delivered.Fingerprint = s.fingerprintOverride
	}
	delivered.Raw = ""
	return delivered, nil
}

func (s *managedKeySecretStore) writeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// managedKeyCoordination returns the owner service and the Workspace-audience
// client over the shared isolated coordination fixture, with the accepted
// obligation grant and an active wallet binding already in place.
func managedKeyCoordination(t *testing.T, issuer identity.ManagedKeyIssuer, secrets identity.SecretStore) (*identity.Service, api.GatewayCoordinationClient) {
	t.Helper()
	service, tenantDB, cookie, console, workspace := coordinationSystem(t, &gatewayWalletStub{})
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	service.KeyIssuer = issuer
	service.SecretStore = secrets
	return service, workspace
}

func managedKeyCommand(step string, models ...string) *api.ManagedKeyCommand {
	return &api.ManagedKeyCommand{Context: grantedCaller(step, "grant-workspace"), WorkspaceId: "ws-1", ModelIds: models, TargetRuntimeInstanceId: "runtime-1"}
}

// TestManagedKeyCommandIsRegisteredBeforeEffectAndReplaysOneKey proves the
// original command is durably registered in this owner's existing
// gateway.idempotency_records before the external issuance is attempted, and a
// replay of the same command returns the recorded binding without a second key
// or a second Secret write.
func TestManagedKeyCommandIsRegisteredBeforeEffectAndReplaysOneKey(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	secrets := &managedKeySecretStore{}
	service, workspace := managedKeyCoordination(t, issuer, secrets)
	db := service.GatewayStore.DB()
	issuer.probe = func() bool {
		var pending int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.idempotency_records
			WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key' AND response_status = 202`).Scan(&pending); err != nil {
			return false
		}
		return pending == 1
	}
	command := managedKeyCommand("managed-key", "model-b", "model-a")
	first, err := workspace.CreateManagedKey(t.Context(), command)
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if first.GetKeyBindingId() == "" || first.GetFingerprint() == "" || first.GetSecretDeliveryReference() == "" ||
		first.GetWorkspaceId() != "ws-1" || first.GetTargetRuntimeInstanceId() != "runtime-1" || first.GetExpiresAt() == nil {
		t.Fatalf("incomplete managed key binding: %v", first)
	}
	if !issuer.registrationObserved() {
		t.Fatal("the external issuance was attempted before the original command was durably registered")
	}
	replay, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a", "model-b"))
	if err != nil {
		t.Fatalf("replay managed key: %v", err)
	}
	if replay.GetKeyBindingId() != first.GetKeyBindingId() || replay.GetFingerprint() != first.GetFingerprint() ||
		replay.GetSecretDeliveryReference() != first.GetSecretDeliveryReference() {
		t.Fatalf("replay returned a different managed key: %v vs %v", first, replay)
	}
	if issuer.issued() != 1 || secrets.writeCount() != 1 {
		t.Fatalf("replay re-issued the managed key: issuances=%d secret writes=%d", issuer.issued(), secrets.writeCount())
	}
	var bindings, records int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.idempotency_records WHERE operation_name = 'CreateManagedKey'`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if bindings != 1 || records != 1 {
		t.Fatalf("replay wrote another original: bindings=%d command records=%d", bindings, records)
	}
	var body string
	if err = db.QueryRowContext(t.Context(), `SELECT response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "raw-managed-key") || strings.Contains(body, secrets.lastRaw) && secrets.lastRaw != "" {
		t.Fatalf("the original command answer leaked the raw key: %s", body)
	}
}

// TestManagedKeyCommandDifferentInputUnderSameKeyIsRefused proves one
// idempotency key can never be reinterpreted as a second, different managed-key
// command.
func TestManagedKeyCommandDifferentInputUnderSameKeyIsRefused(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	if _, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a")); err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	_, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-b"))
	if err == nil || status.Code(err) != codes.AlreadyExists {
		t.Fatalf("a different managed key command reused the original key: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("a conflicting command still issued a key: issuances=%d", issuer.issued())
	}
	var bindings int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 1 {
		t.Fatalf("a conflicting command wrote another binding: %d", bindings)
	}
}

// TestManagedKeyCommandLostAckStaysPendingAndIsNeverReissued proves a lost
// external response leaves the original command explicitly unresolved: it is
// read back on replay and never re-dispatched, because this owner has no
// approved lookup of an existing key by its original issue identity.
func TestManagedKeyCommandLostAckStaysPendingAndIsNeverReissued(t *testing.T) {
	issuer := &managedKeyIssuerStub{applyThenFail: true}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	_, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("lost ACK was not reported as unresolved: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("unexpected issuance count after lost ACK: %d", issuer.issued())
	}
	_, err = workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("a pending command was not read back as unresolved: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("a pending command was re-issued: issuances=%d", issuer.issued())
	}
	var stored int
	var body string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_status, response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key'`).Scan(&stored, &body); err != nil {
		t.Fatal(err)
	}
	if stored != 202 || !strings.Contains(body, "pending") {
		t.Fatalf("pending command answer was not preserved: status=%d body=%s", stored, body)
	}
	var bindings int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("an unconfirmed issuance recorded a binding: %d", bindings)
	}
}

// TestManagedKeyDefiniteRefusalIsRecordedAndSettledOnce proves a definite
// issuer refusal is recorded on the original command and replayed as the same
// refusal, without a second issuance attempt.
func TestManagedKeyDefiniteRefusalIsRecordedAndSettledOnce(t *testing.T) {
	issuer := &managedKeyIssuerStub{refuse: true}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	_, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("definite refusal was not reported: %v", err)
	}
	_, err = workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("recorded refusal was not replayed: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("a recorded refusal was re-dispatched: issuances=%d", issuer.issued())
	}
	var stored int
	var body string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_status, response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key'`).Scan(&stored, &body); err != nil {
		t.Fatal(err)
	}
	if stored != 409 || !strings.Contains(body, "PermissionDenied") {
		t.Fatalf("refusal answer was not recorded with its cause: status=%d body=%s", stored, body)
	}
}

// TestManagedKeyLocalPreconditionFailureWritesNoCommandRecord proves the wallet
// binding is read before the original command is registered: a local
// precondition failure neither attempts an issuance nor leaves an orphan
// pending command.
func TestManagedKeyLocalPreconditionFailureWritesNoCommandRecord(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	service, tenantDB, _, _, workspace := coordinationSystem(t, &gatewayWalletStub{})
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	service.KeyIssuer = issuer
	service.SecretStore = &managedKeySecretStore{}
	_, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a missing wallet binding was not refused: %v", err)
	}
	if issuer.issued() != 0 {
		t.Fatalf("a missing wallet binding still issued a key: %d", issuer.issued())
	}
	var records int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.idempotency_records WHERE operation_name = 'CreateManagedKey'`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("a local precondition failure wrote an orphan command record: %d", records)
	}
}

// TestManagedKeyCommandRequiresIdempotencyIdentity proves the original command
// cannot be registered without an immutable caller identity.
func TestManagedKeyCommandRequiresIdempotencyIdentity(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	command := managedKeyCommand("managed-key", "model-a")
	command.GetContext().IdempotencyKey = ""
	_, err := workspace.CreateManagedKey(t.Context(), command)
	if err == nil || status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a command without an idempotency key was accepted: %v", err)
	}
	if issuer.issued() != 0 {
		t.Fatalf("a command without an idempotency key still issued a key: %d", issuer.issued())
	}
}

// TestManagedKeyLostSecretDeliveryRecordsObservedKeyAndNeverReissues proves the
// exact external key identity is durable even when the Secret delivery is lost,
// so the unresolved command is read back as that one original key instead of a
// second issuance.
func TestManagedKeyLostSecretDeliveryRecordsObservedKeyAndNeverReissues(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	secrets := &managedKeySecretStore{fail: true}
	service, workspace := managedKeyCoordination(t, issuer, secrets)
	_, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("a lost Secret delivery was not reported unresolved: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("unexpected issuance count: %d", issuer.issued())
	}
	_, err = workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("the unresolved command was not read back: %v", err)
	}
	if issuer.issued() != 1 {
		t.Fatalf("the unresolved original key was re-issued: issuances=%d", issuer.issued())
	}
	var stored int
	var body string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_status, response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key'`).Scan(&stored, &body); err != nil {
		t.Fatal(err)
	}
	if stored != 202 || !strings.Contains(body, managedKeyExternalIDForTest("ws-1", []string{"model-a"})) {
		t.Fatalf("the observed external key was not recorded: status=%d body=%s", stored, body)
	}
	var bindings int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("an unconfirmed delivery recorded a binding: %d", bindings)
	}
}

// TestManagedKeyBindingAndCommandAnswerCommitTogether proves the recorded
// binding and the original command's confirmed answer are one transaction: after
// a confirmed call the stored command answer names a binding that exists, and no
// window exists where a replay could mint a second key.
func TestManagedKeyBindingAndCommandAnswerCommitTogether(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	first, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	var storedStatus int
	var storedResourceID, body string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_status, resource_id, response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = 'opl-test:managed-key'`).Scan(&storedStatus, &storedResourceID, &body); err != nil {
		t.Fatal(err)
	}
	if storedStatus != 200 || storedResourceID != first.GetKeyBindingId() {
		t.Fatalf("the command answer did not commit with its binding: status=%d resource=%s", storedStatus, storedResourceID)
	}
	var bindings int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE id = $1`, storedResourceID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 1 {
		t.Fatalf("the recorded command answer names a missing binding: %d", bindings)
	}
}
