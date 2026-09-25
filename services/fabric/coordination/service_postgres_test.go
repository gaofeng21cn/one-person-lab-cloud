package coordination_test

import (
	"context"
	"database/sql"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	"opl-cloud/services/fabric/ownermigrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

type identity struct {
	api.CloudIdentityAuthorizationClient
	mu        sync.Mutex
	deny      bool
	last      *api.AuthorizationRequest
	denyAfter int
	calls     int
}

func (i *identity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.last = proto.Clone(r).(*api.AuthorizationRequest)
	i.calls++
	if i.deny || (i.denyAfter > 0 && i.calls >= i.denyAfter) {
		return nil, status.Error(codes.PermissionDenied, "test refusal")
	}
	return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Scope: r.Scope, ActorId: r.ActorId, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}

type catalog struct {
	api.UnimplementedCatalogCoordinationServer
	mu          sync.Mutex
	acceptances map[string]*api.QuoteAcceptance
	calls       int
	last        *api.CallContext
	fail        bool
}

type ledger struct {
	api.LedgerCoordinationClient
	evidence *api.LocalNoChargeReceiptEvidence
}

func (l *ledger) ReadLocalNoChargeReceipt(_ context.Context, _ *api.GetReceiptByReferenceRequest, _ ...grpc.CallOption) (*api.LocalNoChargeReceiptEvidence, error) {
	return proto.Clone(l.evidence).(*api.LocalNoChargeReceiptEvidence), nil
}

type localFixture struct {
	calls int
	fail  bool
}

func (d *localFixture) EnsureLocal(_ context.Context, in coordination.LocalResourceIntent) (*coordination.LocalResourceResult, error) {
	d.calls++
	if d.fail {
		return nil, status.Error(codes.Unavailable, "fixture provider pending")
	}
	return &coordination.LocalResourceResult{Binding: &api.ResourceExecutionBinding{ComputeAllocationId: in.ComputeID, StorageVolumeId: in.StorageID, DataAttachmentId: "attachment-fixture", DataAttachmentOperationId: in.OperationID + ":attachment", AccountId: "explicit-legacy-account"}, Compute: fabric.ComputeAllocation{ID: in.ComputeID, AccountID: "explicit-legacy-account", WorkspaceID: in.WorkspaceID, Status: "running", Provider: "local-docker", ProviderResourceID: "network/fixture"}, Storage: fabric.StorageVolume{ID: in.StorageID, AccountID: "explicit-legacy-account", WorkspaceID: in.WorkspaceID, Status: "ready", Provider: "local-docker", ProviderResourceID: "directory/fixture"}, Attachment: fabric.StorageAttachment{ID: "attachment-fixture", OperationID: in.OperationID + ":attachment", WorkspaceID: in.WorkspaceID, ComputeID: in.ComputeID, VolumeID: in.StorageID, Status: "attached", ProviderAttachmentID: "docker/fixture"}, ObservedAt: time.Now().UTC()}, nil
}

func (c *catalog) ReadQuoteResourcePlan(_ context.Context, r *api.QuoteResourcePlanRequest) (*api.QuoteAcceptance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = proto.Clone(r.Context).(*api.CallContext)
	if c.fail {
		return nil, status.Error(codes.Unavailable, "Catalog unavailable")
	}
	a := c.acceptances[r.QuoteId]
	if a == nil {
		return nil, status.Error(codes.NotFound, "quote missing")
	}
	return proto.Clone(a).(*api.QuoteAcceptance), nil
}

func database(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "fabric", Database: "opl_fabric", SchemaOwnerRole: "opl_fabric_owner", WriterRole: "opl_fabric_writer", RuntimeRole: "opl_fabric_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := ownermigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func command(suffix string) *api.EnsureResourcesCommand {
	p := &api.ResourcePlanSnapshot{ComputePlanId: "cp", StoragePlanId: "sp", ProviderProfileId: "profile", ProviderComputeSkuId: "cpu-sku", ProviderStorageSkuId: "disk-sku", Vcpus: 2, MemoryMib: 2048, CapacityGib: 20, PrepaidMonths: 1, ProviderCapabilityVersion: "v1", Provider: "local", Region: "local", BillingMode: "LOCAL_NO_CHARGE"}
	a := &api.QuoteAcceptance{Quote: &api.Quote{Id: "quote-" + suffix, ComputePlanId: "cp", StoragePlanId: "sp", PeriodMonths: 1, Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY, Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED}, ObligationId: "obligation-" + suffix, AcceptanceId: "accept-" + suffix, SnapshotDigest: "sha256:accepted-" + suffix, ResourcePlan: p, WorkspaceId: "workspace-" + suffix}
	return &api.EnsureResourcesCommand{Context: &api.CallContext{ActorId: "actor", RequestId: "request-" + suffix, IdempotencyKey: "key-" + suffix, SessionId: proto.String("session"), AuthorizationContextId: "workspace-decision", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant"}}}}, WorkspaceId: a.WorkspaceId, ObligationId: a.ObligationId, Plan: proto.Clone(p).(*api.ResourcePlanSnapshot), QuoteAcceptance: a}
}

func TestResourceAcceptanceAndReadback(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	token := "isolated-fabric-workspace-token-00000000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token, owneridentity.Serve.Service(): token, owneridentity.Build.Service(): token}}
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
	r := command("original")
	bind(r)
	first, err := client.EnsureResources(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("durable pending resources are not provider confirmation", func(t *testing.T) {
		if first.ResourceId == "" || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || first.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_PAYMENT_AUTHORIZATION || first.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN || first.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE {
			t.Fatalf("dishonest acceptance: %v", first)
		}
		read, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: first.ResourceId})
		if err != nil {
			t.Fatal(err)
		}
		if read.GetOutcome() != api.Observation_OBSERVATION_UNKNOWN || read.ObservedAt != nil || read.ExecutionResources != nil || read.AbsenceConfirmed || len(read.Resources) != 2 {
			t.Fatalf("dishonest readback: %v", read)
		}
		for _, resource := range read.Resources {
			if resource.Id == "" || resource.State != "not_dispatched" || resource.OpaqueProviderReference != "" || resource.ReceiptId != "" {
				t.Fatalf("fabricated provider fact: %v", resource)
			}
		}
		var quote, workspace, obligation string
		if err = db.QueryRow(`SELECT rs.accepted_quote_id,rs.workspace_id,o.accepted_input->>'obligationId' FROM fabric.resource_sets rs JOIN fabric.operations o ON o.resource_id=rs.id WHERE rs.id=$1`, first.ResourceId).Scan(&quote, &workspace, &obligation); err != nil {
			t.Fatal(err)
		}
		if quote != r.QuoteAcceptance.Quote.Id || workspace != r.WorkspaceId || obligation != r.ObligationId {
			t.Fatal("original quote obligation binding lost")
		}
		catalogOwner.mu.Lock()
		defer catalogOwner.mu.Unlock()
		if catalogOwner.last.GetAuthorizationContextId() != "" {
			t.Fatal("previous owner decision was forwarded to Catalog")
		}
	})
	t.Run("same command replays while Catalog unavailable", func(t *testing.T) {
		catalogOwner.mu.Lock()
		catalogOwner.fail = true
		catalogOwner.mu.Unlock()
		defer func() { catalogOwner.mu.Lock(); catalogOwner.fail = false; catalogOwner.mu.Unlock() }()
		replay := proto.Clone(r).(*api.EnsureResourcesCommand)
		replay.Context.RequestId = "replay-request"
		replayed, err := client.EnsureResources(ctx, replay)
		if err != nil || !proto.Equal(first, replayed) {
			t.Fatalf("replay changed: %v %v", replayed, err)
		}
		replay.Context.IdempotencyKey = "new-key-same-order"
		replayed, err = client.EnsureResources(ctx, replay)
		if err != nil || replayed.OperationId != first.OperationId {
			t.Fatalf("new command duplicated order: %v %v", replayed, err)
		}
	})
	t.Run("same key different body refuses", func(t *testing.T) {
		changed := proto.Clone(r).(*api.EnsureResourcesCommand)
		changed.QuoteAcceptance.SnapshotDigest = "changed-immutable-acceptance"
		if _, err := client.EnsureResources(ctx, changed); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("changed body accepted: %v", err)
		}
	})
	t.Run("later unverified evidence retains original pending reference", func(t *testing.T) {
		changed := proto.Clone(r).(*api.EnsureResourcesCommand)
		changed.ConfirmedChargeReceiptId = "unverified-receipt"
		changed.InstanceAuthorizationReference = "unverified-instance"
		got, err := client.EnsureResources(ctx, changed)
		if err != nil || got.OperationId != first.OperationId || got.ResourceId != first.ResourceId || got.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION {
			t.Fatalf("evidence minted a new or confirmed resource: %v %v", got, err)
		}
		changed.Context.IdempotencyKey = "later-proof"
		got, err = client.EnsureResources(ctx, changed)
		if err != nil || got.OperationId != first.OperationId || got.ResourceId != first.ResourceId || got.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION {
			t.Fatalf("new evidence command minted a new or confirmed resource: %v %v", got, err)
		}
	})
	t.Run("workspace cannot bind a different obligation", func(t *testing.T) {
		changed := proto.Clone(r).(*api.EnsureResourcesCommand)
		changed.Context.IdempotencyKey = "other-order"
		changed.ObligationId = "other"
		changed.QuoteAcceptance.ObligationId = "other"
		if _, err := client.EnsureResources(ctx, changed); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("workspace rebound: %v", err)
		}
	})
	t.Run("tenant mismatch cannot replay or read", func(t *testing.T) {
		changed := proto.Clone(r).(*api.EnsureResourcesCommand)
		changed.Context.Scope.GetTenant().TenantId = "foreign"
		if _, err := client.EnsureResources(ctx, changed); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("foreign replay: %v", err)
		}
		if _, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: changed.Context, ResourceSetId: first.ResourceId}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("foreign read: %v", err)
		}
	})
	t.Run("Catalog mismatch rolls back all intent writes", func(t *testing.T) {
		changed := command("tampered")
		bind(changed)
		changed.QuoteAcceptance.SnapshotDigest = "forged"
		if _, err := client.EnsureResources(ctx, changed); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("forged acceptance: %v", err)
		}
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM fabric.resource_sets WHERE workspace_id=$1`, changed.WorkspaceId).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial intent retained: %d %v", count, err)
		}
	})
	t.Run("plan must match accepted frozen plan", func(t *testing.T) {
		changed := command("plan")
		bind(changed)
		changed.Plan.ProviderComputeSkuId = "different"
		if _, err := client.EnsureResources(ctx, changed); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("plan substituted: %v", err)
		}
	})
	t.Run("Serve reads exact workspace but cannot provision", func(t *testing.T) {
		if _, err := serve.EnsureResources(ctx, r); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Serve provisioned: %v", err)
		}
		if _, err := serve.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: first.ResourceId}); err != nil {
			t.Fatal(err)
		}
		authority.mu.Lock()
		defer authority.mu.Unlock()
		last := authority.last
		if last.AudienceOwner != api.OwnerEnum_OWNER_ENUM_FABRIC || last.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES || last.Resource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || last.Resource.GetId() != r.WorkspaceId {
			t.Fatalf("wrong scope: %v", last)
		}
	})
	t.Run("peer authentication and policy refusal", func(t *testing.T) {
		build := clientFor(owneridentity.Build.Service(), token)
		if _, err := build.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: first.ResourceId}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Build read resources: %v", err)
		}
		invalid := clientFor(owneridentity.Workspace.Service(), "invalid-isolated-peer-token-00000000000")
		if _, err := invalid.EnsureResources(ctx, r); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("invalid peer allowed: %v", err)
		}
		authority.mu.Lock()
		authority.deny = true
		authority.mu.Unlock()
		_, err := client.EnsureResources(ctx, r)
		authority.mu.Lock()
		authority.deny = false
		authority.mu.Unlock()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("policy denial ignored: %v", err)
		}
	})
	t.Run("parallel acceptance creates one complete intent", func(t *testing.T) {
		parallel := command("parallel")
		bind(parallel)
		const n = 8
		outputs := make(chan *api.Operation, n)
		failures := make(chan error, n)
		var wg sync.WaitGroup
		for k := 0; k < n; k++ {
			wg.Add(1)
			go func() { defer wg.Done(); o, e := client.EnsureResources(ctx, parallel); outputs <- o; failures <- e }()
		}
		wg.Wait()
		close(outputs)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var original string
		for out := range outputs {
			if original == "" {
				original = out.OperationId
			}
			if out.OperationId != original {
				t.Fatalf("duplicate operation %s and %s", original, out.OperationId)
			}
		}
		var sets, resources, operations, actions int
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM fabric.resource_sets WHERE workspace_id=$1),(SELECT count(*) FROM fabric.resources r JOIN fabric.resource_sets rs ON rs.id=r.resource_set_id WHERE rs.workspace_id=$1),(SELECT count(*) FROM fabric.operations o JOIN fabric.resource_sets rs ON rs.id=o.resource_id WHERE rs.workspace_id=$1),(SELECT count(*) FROM fabric.resource_actions a JOIN fabric.resource_sets rs ON rs.id=a.resource_set_id WHERE rs.workspace_id=$1)`, parallel.WorkspaceId).Scan(&sets, &resources, &operations, &actions); err != nil {
			t.Fatal(err)
		}
		if sets != 1 || resources != 2 || operations != 1 || actions != 1 {
			t.Fatalf("non-atomic intent %d %d %d %d", sets, resources, operations, actions)
		}
	})
	t.Run("authorization revoked before durable write leaves no intent", func(t *testing.T) {
		queued := command("revoked-before-write")
		bind(queued)
		authority.mu.Lock()
		authority.denyAfter = authority.calls + 2
		authority.mu.Unlock()
		_, err := client.EnsureResources(ctx, queued)
		authority.mu.Lock()
		authority.denyAfter = 0
		authority.mu.Unlock()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("write used stale allow: %v", err)
		}
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM fabric.resource_sets WHERE workspace_id=$1`, queued.WorkspaceId).Scan(&count); err != nil || count != 0 {
			t.Fatalf("revoked intent persisted: %d %v", count, err)
		}
	})
	t.Run("verified zero receipt resumes original refs with confirmed provider evidence", func(t *testing.T) {
		local := command("local")
		local.Plan.Provider = "local-docker"
		local.QuoteAcceptance.ResourcePlan.Provider = "local-docker"
		bind(local)
		pending, err := client.EnsureResources(ctx, local)
		if err != nil {
			t.Fatal(err)
		}
		dispatch := &localFixture{fail: true}
		proof := &api.LocalNoChargeReceiptEvidence{Receipt: &api.Receipt{Id: "zero-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(local.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED}, QuoteAcceptance: proto.Clone(local.QuoteAcceptance).(*api.QuoteAcceptance), OwnerCommitEvidence: &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: local.ObligationId, ResourceId: local.WorkspaceId, ActorId: local.Context.ActorId, Scope: local.Context.Scope, CommittedVersion: 1}, EvidenceDigest: local.QuoteAcceptance.SnapshotDigest}
		service.Dispatcher, service.Ledger = dispatch, &ledger{evidence: proof}
		defer func() { service.Dispatcher = nil; service.Ledger = nil }()
		local.ConfirmedChargeReceiptId = "forged-reference"
		got, err := client.EnsureResources(ctx, local)
		if err != nil || got.OperationId != pending.OperationId || dispatch.calls != 0 {
			t.Fatalf("forged receipt dispatched: %v %v calls=%d", got, err, dispatch.calls)
		}
		local.ConfirmedChargeReceiptId = "zero-receipt"
		proof.QuoteAcceptance.Quote.TotalUsdMicros = 1
		if _, err = client.EnsureResources(ctx, local); err != nil || dispatch.calls != 0 {
			t.Fatalf("mismatched quote dispatched: %v calls=%d", err, dispatch.calls)
		}
		proof.QuoteAcceptance.Quote.TotalUsdMicros = 0
		proof.EvidenceDigest = "mismatched-digest"
		if _, err = client.EnsureResources(ctx, local); err != nil || dispatch.calls != 0 {
			t.Fatalf("mismatched evidence digest dispatched: %v calls=%d", err, dispatch.calls)
		}
		proof.EvidenceDigest = local.QuoteAcceptance.SnapshotDigest
		authority.mu.Lock()
		authority.denyAfter = authority.calls + 3
		authority.mu.Unlock()
		_, err = client.EnsureResources(ctx, local)
		authority.mu.Lock()
		authority.denyAfter = 0
		authority.mu.Unlock()
		if status.Code(err) != codes.PermissionDenied || dispatch.calls != 0 {
			t.Fatalf("dispatch used authorization from before lock: %v calls=%d", err, dispatch.calls)
		}
		got, err = client.EnsureResources(ctx, local)
		if err != nil || got.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || got.Stage != api.OperationStageEnum_OPERATION_STAGE_ENUM_RESOURCE_PREFLIGHT || dispatch.calls != 1 {
			t.Fatalf("provider pending changed identity: %v %v calls=%d", got, err, dispatch.calls)
		}
		dispatch.fail = false
		got, err = client.EnsureResources(ctx, local)
		if err != nil || got.OperationId != pending.OperationId || got.ResourceId != pending.ResourceId || got.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || got.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
			t.Fatalf("provider evidence not confirmed: %v %v", got, err)
		}
		read, err := serve.ReadResources(ctx, &api.ResourceReadbackRequest{Context: local.Context, ResourceSetId: pending.ResourceId})
		if err != nil {
			t.Fatal(err)
		}
		if read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || read.ExecutionResources.GetAccountId() != "explicit-legacy-account" || read.ExecutionResources.GetDataAttachmentOperationId() != pending.OperationId+":attachment" || read.ObservedAt == nil {
			t.Fatalf("execution binding missing: %v", read)
		}
		for _, resource := range read.Resources {
			if resource.OpaqueProviderReference == "" || resource.State != "confirmed" {
				t.Fatalf("missing provider resource: %v", resource)
			}
		}
		if _, err = client.EnsureResources(ctx, local); err != nil || dispatch.calls != 2 {
			t.Fatalf("confirmed replay dispatched again: %v calls=%d", err, dispatch.calls)
		}
		var attachments int
		if err = db.QueryRow(`SELECT count(*) FROM fabric.attachments WHERE resource_set_id=$1`, pending.ResourceId).Scan(&attachments); err != nil || attachments != 0 {
			t.Fatalf("compute was invented as execution: %d %v", attachments, err)
		}
	})
}
