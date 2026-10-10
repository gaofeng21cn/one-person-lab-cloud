package coordination_test

import (
	"context"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerservice"
)

// deletionFixture is a provider-adapter fixture with the same durable claim the
// internal Fabric owner keeps: at most one provider mutation per released kind
// per workspace, and a kind whose mutation was already dispatched is only
// reconciled by readback. Every fact is scoped to the workspace that owns the
// handle, so one workspace's release can never be answered by another's state.
type deletionFixture struct {
	localFixture
	mu        sync.Mutex
	mutations map[string]map[string]int
	readbacks map[string]map[string]int
	absent    map[string]map[string]bool
	converge  map[string]map[string]bool
	owned     map[string]map[string]bool
}

func newDeletionFixture() *deletionFixture {
	return &deletionFixture{mutations: map[string]map[string]int{}, readbacks: map[string]map[string]int{}, absent: map[string]map[string]bool{}, converge: map[string]map[string]bool{}, owned: map[string]map[string]bool{}}
}

// EnsureResources records the provider-confirmed identity of each purchased
// handle, exactly like the internal Fabric owner keeps the purchase claim.
func (d *deletionFixture) EnsureResources(ctx context.Context, in coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	d.mu.Lock()
	d.owned[in.WorkspaceID] = map[string]bool{coordination.DeletionKindCompute: true, coordination.DeletionKindStorage: true}
	d.mu.Unlock()
	return d.localFixture.EnsureResources(ctx, in)
}

// entry resolves the durable per-workspace state of one fixture map.
func entry[V any](state map[string]map[string]V, workspace string) map[string]V {
	if state[workspace] == nil {
		state[workspace] = map[string]V{}
	}
	return state[workspace]
}

func (d *deletionFixture) DeleteResource(_ context.Context, in coordination.ResourceDeletionIntent, kind string) (coordination.ResourceDeletionFact, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if in.WorkspaceID == "" || in.ResourceSetID == "" || in.OperationID == "" {
		return coordination.ResourceDeletionFact{}, status.Error(codes.InvalidArgument, "deletion intent is incomplete")
	}
	fact := coordination.ResourceDeletionFact{Kind: kind, State: coordination.DeletionStatePending, ProviderStatus: "present"}
	switch kind {
	case coordination.DeletionKindRuntime:
		fact.ResourceID = in.WorkspaceID
	case coordination.DeletionKindSecret:
		fact.ResourceID = "opl-gateway-fixture"
	case coordination.DeletionKindAttachment:
		fact.ResourceID = in.AttachmentID
	case coordination.DeletionKindStorage:
		fact.ResourceID = in.StorageID
		if fact.ResourceID == "" || !entry(d.owned, in.WorkspaceID)[coordination.DeletionKindStorage] {
			return fact, status.Error(codes.InvalidArgument, "storage intent names another resource")
		}
	case coordination.DeletionKindCompute:
		fact.ResourceID = in.ComputeID
		if fact.ResourceID == "" || !entry(d.owned, in.WorkspaceID)[coordination.DeletionKindCompute] {
			return fact, status.Error(codes.InvalidArgument, "compute intent names another resource")
		}
	}
	absent := func() (coordination.ResourceDeletionFact, error) {
		fact.State, fact.ProviderStatus, fact.EvidenceRef = coordination.DeletionStateAbsent, "NOT_FOUND", "readback-"+kind
		return fact, nil
	}
	// A workspace that owns no such handle, and a handle the provider reports gone
	// before any mutation, are absent from the owning surface's first readback:
	// neither dispatches a mutation.
	if !entry(d.owned, in.WorkspaceID)[kind] ||
		entry(d.absent, in.WorkspaceID)[kind] && entry(d.mutations, in.WorkspaceID)[kind] == 0 {
		return absent()
	}
	if entry(d.mutations, in.WorkspaceID)[kind] == 0 {
		entry(d.mutations, in.WorkspaceID)[kind]++
		if entry(d.converge, in.WorkspaceID)[kind] {
			entry(d.absent, in.WorkspaceID)[kind] = true
			return absent()
		}
		fact.EvidenceRef = "dispatch-" + kind
		return fact, nil
	}
	// The mutation was already dispatched: reconcile read-only. A second mutation
	// here would be a real second terminate/delete.
	entry(d.readbacks, in.WorkspaceID)[kind]++
	if entry(d.absent, in.WorkspaceID)[kind] {
		return absent()
	}
	fact.EvidenceRef = "reconcile-" + kind
	return fact, nil
}

func (d *deletionFixture) dispatched(workspace string) map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]int{}
	for kind, count := range d.mutations[workspace] {
		out[kind] = count
	}
	return out
}

func (d *deletionFixture) reconciled(workspace, kind string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.readbacks[workspace][kind]
}

func (d *deletionFixture) markAbsent(workspace string, kinds ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, kind := range kinds {
		entry(d.absent, workspace)[kind] = true
	}
}

func (d *deletionFixture) markOwned(workspace string, kinds ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, kind := range kinds {
		entry(d.owned, workspace)[kind] = true
	}
}

func (d *deletionFixture) markConverge(workspace string, kinds ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, kind := range kinds {
		entry(d.converge, workspace)[kind] = true
	}
}

func deletionKinds() []string {
	return []string{coordination.DeletionKindRuntime, coordination.DeletionKindSecret, coordination.DeletionKindAttachment, coordination.DeletionKindStorage, coordination.DeletionKindCompute}
}

// TestDeleteResourcesConfirmsEachKindFromOwnReadback proves the release chain:
// each kind is confirmed from its own readback, a pending provider state is never
// absence, an already absent set completes without a dispatch, and a repeated
// call resumes the recorded operation without a second provider mutation.
func TestDeleteResourcesConfirmsEachKindFromOwnReadback(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{}}
	catalogServer := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(catalogServer, catalogOwner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go catalogServer.Serve(listener)
	t.Cleanup(catalogServer.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	authority := &identity{}
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, authority).Authorize, api.NewCatalogCoordinationClient(connection))
	if err != nil {
		t.Fatal(err)
	}
	fixture := newDeletionFixture()
	service.Dispatcher = fixture
	token := "isolated-fabric-workspace-token-000000000021"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token, owneridentity.Serve.Service(): token}}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	fabricListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(fabricListener)
	t.Cleanup(server.Stop)
	clientFor := func(peer owneridentity.Service, secret string) api.FabricCoordinationClient {
		opts, e := config.TLS.DialOptions(peer, owneridentity.Fabric.Service(), secret)
		if e != nil {
			t.Fatal(e)
		}
		conn, e := grpc.NewClient(fabricListener.Addr().String(), opts...)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { conn.Close() })
		return api.NewFabricCoordinationClient(conn)
	}
	client := clientFor(owneridentity.Workspace.Service(), token)
	serve := clientFor(owneridentity.Serve.Service(), token)
	bind := func(r *api.EnsureResourcesCommand) {
		catalogOwner.mu.Lock()
		catalogOwner.acceptances[r.QuoteAcceptance.Quote.Id] = proto.Clone(r.QuoteAcceptance).(*api.QuoteAcceptance)
		catalogOwner.mu.Unlock()
	}
	provision := func(suffix string) (*api.EnsureResourcesCommand, string) {
		r := command(suffix)
		r.Plan.Provider = "local-docker"
		r.QuoteAcceptance.ResourcePlan.Provider = "local-docker"
		r.ConfirmedChargeReceiptId = "zero-receipt-" + suffix
		evidence := &api.LocalNoChargeReceiptEvidence{
			Receipt:             &api.Receipt{Id: r.ConfirmedChargeReceiptId, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED},
			QuoteAcceptance:     proto.Clone(r.QuoteAcceptance).(*api.QuoteAcceptance),
			OwnerCommitEvidence: &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: r.ObligationId, ResourceId: r.WorkspaceId, ActorId: r.Context.ActorId, Scope: r.Context.Scope, CommittedVersion: 1},
			EvidenceDigest:      r.QuoteAcceptance.SnapshotDigest,
		}
		bind(r)
		service.Ledger = &ledger{evidence: evidence}
		operation, err := client.EnsureResources(ctx, r)
		if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("provision %s: %v %v", suffix, operation, err)
		}
		return r, operation.ResourceId
	}
	deleteCommand := func(r *api.EnsureResourcesCommand, setID string) *api.MutateResourcesCommand {
		call := proto.Clone(r.Context).(*api.CallContext)
		call.IdempotencyKey = "delete-" + setID
		return &api.MutateResourcesCommand{Context: call, WorkspaceId: r.WorkspaceId, ResourceSetId: setID}
	}
	stateOf := func(read *api.ResourceReadback, kind string) string {
		for _, fact := range read.GetResources() {
			if fact.GetKind() == kind {
				return fact.GetState()
			}
		}
		return ""
	}

	t.Run("every kind is confirmed absent from its own readback", func(t *testing.T) {
		r, setID := provision("release")
		fixture.markAbsent(r.WorkspaceId, deletionKinds()...)
		operation, err := client.DeleteResources(ctx, deleteCommand(r, setID))
		if err != nil {
			t.Fatal(err)
		}
		if operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_DELETE || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
			operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_ABSENCE_VERIFICATION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
			t.Fatalf("release was not confirmed per kind: %v", operation)
		}
		read, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: setID})
		if err != nil {
			t.Fatal(err)
		}
		if !read.GetAbsenceConfirmed() || read.GetWorkspaceId() != r.WorkspaceId || read.GetResourceSetId() != setID {
			t.Fatalf("absence readback does not identify the original set: %v", read)
		}
		for _, kind := range deletionKinds() {
			if state := stateOf(read, kind); state != coordination.DeletionStateAbsent {
				t.Fatalf("kind %s was not reported absent: %v", kind, read.GetResources())
			}
		}
		if dispatched := fixture.dispatched(r.WorkspaceId); len(dispatched) != 0 {
			t.Fatalf("an already absent set was destroyed again: %v", dispatched)
		}
		var unreleased int
		if err = db.QueryRow(`SELECT count(*) FROM fabric.resources WHERE resource_set_id=$1 AND (deleted_at IS NULL OR deletion_evidence_ref IS NULL)`, setID).Scan(&unreleased); err != nil || unreleased != 0 {
			t.Fatalf("resource rows lack the owner readback that released them: %d %v", unreleased, err)
		}
		var actions int
		if err = db.QueryRow(`SELECT count(*) FROM fabric.resource_actions WHERE resource_set_id=$1 AND action='delete' AND observation_result='confirmed'`, setID).Scan(&actions); err != nil || actions != 5 {
			t.Fatalf("per-kind evidence was not recorded: %d %v", actions, err)
		}
	})

	t.Run("pending provider state is never reported absent", func(t *testing.T) {
		r, setID := provision("recycle")
		// The prepaid volume is still being reclaimed by the provider, while the
		// workspace owns no runtime, Secret binding or mount binding at all.
		fixture.markConverge(r.WorkspaceId, coordination.DeletionKindCompute)
		operation, err := client.DeleteResources(ctx, deleteCommand(r, setID))
		if err != nil {
			t.Fatal(err)
		}
		if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN ||
			operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_STORAGE_DELETION || operation.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE {
			t.Fatalf("a pending storage destroy was reported as a release: %v", operation)
		}
		read, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: setID})
		if err != nil {
			t.Fatal(err)
		}
		if read.GetAbsenceConfirmed() || stateOf(read, coordination.DeletionKindStorage) != coordination.DeletionStatePending {
			t.Fatalf("pending storage was reported as absent: %v", read)
		}
		if stateOf(read, coordination.DeletionKindCompute) != "confirmed" {
			t.Fatalf("the un-reached compute handle lost its confirmed fact: %v", read.GetResources())
		}
		if dispatched := fixture.dispatched(r.WorkspaceId); dispatched[coordination.DeletionKindStorage] != 1 || dispatched[coordination.DeletionKindCompute] != 0 {
			t.Fatalf("storage was not destroyed exactly once before compute was reached: %v", dispatched)
		}
		// The provider finishes reclaiming the disk. The next call resumes the same
		// operation from its recorded state and reconciles by readback only.
		fixture.markAbsent(r.WorkspaceId, coordination.DeletionKindStorage)
		resumed, err := client.DeleteResources(ctx, deleteCommand(r, setID))
		if err != nil {
			t.Fatal(err)
		}
		if resumed.OperationId != operation.OperationId || resumed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("release did not resume the recorded operation: %v", resumed)
		}
		if dispatched := fixture.dispatched(r.WorkspaceId); dispatched[coordination.DeletionKindStorage] != 1 || dispatched[coordination.DeletionKindCompute] != 1 {
			t.Fatalf("resume dispatched a second storage destroy: %v", dispatched)
		}
		if fixture.reconciled(r.WorkspaceId, coordination.DeletionKindStorage) == 0 {
			t.Fatal("resume did not reconcile the dispatched storage destroy by readback")
		}
		read, err = client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: setID})
		if err != nil || !read.GetAbsenceConfirmed() {
			t.Fatalf("confirmed release is not read back as absence: %v %v", read, err)
		}
	})

	t.Run("a repeated release issues no second destroy", func(t *testing.T) {
		r, setID := provision("idempotent")
		// This workspace owns every kind, and every kind completes on its first
		// destroy: one mutation per kind is the whole provider effect.
		fixture.markOwned(r.WorkspaceId, deletionKinds()...)
		fixture.markConverge(r.WorkspaceId, deletionKinds()...)
		first, err := client.DeleteResources(ctx, deleteCommand(r, setID))
		if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("first release: %v %v", first, err)
		}
		second := deleteCommand(r, setID)
		second.Context.IdempotencyKey = "delete-again-" + setID
		again, err := client.DeleteResources(ctx, second)
		if err != nil || again.OperationId != first.OperationId || again.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("repeated release minted a second operation: %v %v", again, err)
		}
		dispatched := fixture.dispatched(r.WorkspaceId)
		for _, kind := range deletionKinds() {
			if dispatched[kind] != 1 {
				t.Fatalf("kind %s was destroyed %d times: %v", kind, dispatched[kind], dispatched)
			}
		}
	})

	t.Run("identity is bound to the declared workspace and tenant", func(t *testing.T) {
		r, setID := provision("identity")
		foreign := deleteCommand(r, setID)
		foreign.WorkspaceId = "another-workspace"
		if _, err := client.DeleteResources(ctx, foreign); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("another workspace could release this set: %v", err)
		}
		foreignTenant := deleteCommand(r, setID)
		foreignTenant.Context.Scope.GetTenant().TenantId = "another-tenant"
		if _, err := client.DeleteResources(ctx, foreignTenant); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("another tenant could release this set: %v", err)
		}
		missing := deleteCommand(r, setID)
		missing.ResourceSetId = "rset_does_not_exist"
		if _, err := client.DeleteResources(ctx, missing); status.Code(err) != codes.NotFound {
			t.Fatalf("an unknown resource set was accepted: %v", err)
		}
		if _, err := serve.DeleteResources(ctx, deleteCommand(r, setID)); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Serve could release Workspace resources: %v", err)
		}
	})

	t.Run("release is refused without the typed authorization", func(t *testing.T) {
		r, setID := provision("unauthorized")
		authority.mu.Lock()
		authority.deny = true
		authority.denyAfter = 0
		authority.mu.Unlock()
		if _, err := client.DeleteResources(ctx, deleteCommand(r, setID)); status.Code(err) != codes.PermissionDenied {
			authority.mu.Lock()
			authority.deny = false
			authority.mu.Unlock()
			t.Fatalf("an unauthorized release was accepted: %v", err)
		}
		authority.mu.Lock()
		authority.deny = false
		lastAction := authority.last.GetAction()
		lastResource := authority.last.GetResource()
		audience := authority.last.GetAudienceOwner()
		authority.mu.Unlock()
		if audience != api.OwnerEnum_OWNER_ENUM_FABRIC || lastAction != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES ||
			lastResource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || lastResource.GetId() != r.WorkspaceId {
			t.Fatalf("release was not authorized against the declared workspace: %v", authority.last)
		}
	})

	t.Run("a resource-only workspace that owns no runtime completes cleanly", func(t *testing.T) {
		r, setID := provision("resource-only")
		fixture.markAbsent(r.WorkspaceId, coordination.DeletionKindRuntime, coordination.DeletionKindSecret, coordination.DeletionKindAttachment, coordination.DeletionKindStorage, coordination.DeletionKindCompute)
		operation, err := client.DeleteResources(ctx, deleteCommand(r, setID))
		if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("resource-only release did not complete: %v %v", operation, err)
		}
		read, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: setID})
		if err != nil || !read.GetAbsenceConfirmed() {
			t.Fatalf("resource-only release is not read back as absence: %v %v", read, err)
		}
		var states []string
		byKind := map[string]string{}
		for _, fact := range read.GetResources() {
			states = append(states, fact.GetKind()+"="+fact.GetState())
			byKind[fact.GetKind()] = fact.GetState()
		}
		sort.Strings(states)
		for _, kind := range deletionKinds() {
			if byKind[kind] != coordination.DeletionStateAbsent {
				t.Fatalf("release readback is not per kind: %v", states)
			}
		}
		// Every persisted row of the set — including the provider network owned by
		// the compute allocation — is released with the owning readback.
		for kind, state := range byKind {
			if state != coordination.DeletionStateAbsent {
				t.Fatalf("row %s still reports %s: %v", kind, state, states)
			}
		}
	})
}
