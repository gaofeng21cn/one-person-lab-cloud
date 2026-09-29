package coordination_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	"opl-cloud/services/internal/ownerservice"
)

type walletLedger struct {
	api.LedgerCoordinationClient
	receipt *api.Receipt
	fail    bool
}

func (l *walletLedger) ReadReceiptByReference(context.Context, *api.GetReceiptByReferenceRequest, ...grpc.CallOption) (*api.Receipt, error) {
	if l.fail || l.receipt == nil {
		return nil, os.ErrNotExist
	}
	return proto.Clone(l.receipt).(*api.Receipt), nil
}

// tencentFixture is a non-local dispatcher fixture: it proves the coordination
// service routes a Tencent prepaid intent to the Tencent provider path and
// persists its original provider effect identity.
type tencentFixture struct {
	calls int
}

func (d *tencentFixture) Provider() string { return "tencent-tke" }

func (d *tencentFixture) BindSecret(context.Context, coordination.SecretBindIntent) (coordination.SecretBindResult, error) {
	return coordination.SecretBindResult{}, fmt.Errorf("fixture_provider_bind_unavailable")
}

func (d *tencentFixture) EnsureResources(_ context.Context, in coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	d.calls++
	if in.ComputeID == "" || in.StorageID == "" || in.Plan.GetProvider() != "tencent-tke" || in.Plan.GetBillingMode() != "PREPAID_MONTHLY" {
		return nil, os.ErrInvalid
	}
	return &coordination.ResourceResult{
		Binding:    &api.ResourceExecutionBinding{ComputeAllocationId: in.ComputeID, StorageVolumeId: in.StorageID, DataAttachmentId: "att-" + in.ComputeID, DataAttachmentOperationId: in.OperationID + ":attachment", AccountId: "acct-a"},
		Compute:    fabric.ComputeAllocation{ID: in.ComputeID, AccountID: "acct-a", WorkspaceID: in.WorkspaceID, Status: "running", Provider: "tencent-tke", ProviderResourceID: "ins-" + in.ComputeID, ProviderRequestID: "req-cvm-" + in.ComputeID, NodePoolID: "np-basic", MachineName: "machine-" + in.ComputeID, NodeName: "10.66.0.10", PrivateIP: "10.66.0.10", Zone: "ap-guangzhou-3", ChargeType: "PREPAID", RenewFlag: "NOTIFY_AND_MANUAL_RENEW", Deadline: "2026-10-29T00:00:00Z"},
		Storage:    fabric.StorageVolume{ID: in.StorageID, AccountID: "acct-a", WorkspaceID: in.WorkspaceID, Status: "ready", Provider: "tencent-tke", ProviderResourceID: "disk-" + in.StorageID, ProviderRequestID: "req-cbs-" + in.StorageID, SizeGB: 10, Zone: "ap-guangzhou-3", DiskType: "CLOUD_BSSD", Deadline: "2026-10-29T00:00:00Z"},
		Attachment: fabric.StorageAttachment{ID: "att-" + in.ComputeID, OperationID: in.OperationID + ":attachment", WorkspaceID: in.WorkspaceID, ComputeID: in.ComputeID, VolumeID: in.StorageID, Status: "attached", Provider: "tencent-tke", ProviderAttachmentID: "pv/pv-x:pvc/pvc-x", ProviderRequestID: "req-att-" + in.ComputeID},
		Network:    &coordination.NetworkFact{ProviderReference: "tke-node-pool/np-basic", Zone: "ap-guangzhou-3", Region: "ap-guangzhou"},
		ObservedAt: time.Now().UTC(),
	}, nil
}

func tencentCommand(suffix string) *api.EnsureResourcesCommand {
	p := &api.ResourcePlanSnapshot{ComputePlanId: "cp", StoragePlanId: "sp", ProviderProfileId: "tencent-tke", ProviderComputeSkuId: "pool-basic-2c4g", ProviderStorageSkuId: "CLOUD_BSSD", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10, PrepaidMonths: 1, ProviderCapabilityVersion: "v1", Provider: "tencent-tke", Region: "ap-guangzhou", BillingMode: "PREPAID_MONTHLY"}
	a := &api.QuoteAcceptance{Quote: &api.Quote{Id: "quote-" + suffix, ComputePlanId: "cp", StoragePlanId: "sp", PeriodMonths: 1, TotalUsdMicros: 4200, Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY, Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED}, ObligationId: "obligation-" + suffix, AcceptanceId: "accept-" + suffix, SnapshotDigest: "sha256:accepted-" + suffix, ResourcePlan: p, WorkspaceId: "workspace-" + suffix}
	return &api.EnsureResourcesCommand{Context: &api.CallContext{ActorId: "actor", RequestId: "request-" + suffix, IdempotencyKey: "key-" + suffix, SessionId: proto.String("session"), AuthorizationContextId: "workspace-decision", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant"}}}}, WorkspaceId: a.WorkspaceId, ObligationId: a.ObligationId, Plan: proto.Clone(p).(*api.ResourcePlanSnapshot), QuoteAcceptance: a, ConfirmedChargeReceiptId: "wallet-receipt"}
}

func TestTencentPrepaidResourcesConfirmOnlyWithVerifiedOwnerFunding(t *testing.T) {
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
	r := tencentCommand("tencent")
	catalogOwner.acceptances[r.QuoteAcceptance.Quote.Id] = proto.Clone(r.QuoteAcceptance).(*api.QuoteAcceptance)

	dispatcher := &tencentFixture{}
	ledger := &walletLedger{}
	service.Dispatcher, service.Ledger = dispatcher, ledger
	t.Cleanup(func() { service.Dispatcher = nil; service.Ledger = nil })

	pending, err := client.EnsureResources(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if pending.OperationId == "" || pending.ResourceId == "" {
		t.Fatalf("original intent missing: %v", pending)
	}

	read, err := client.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: pending.ResourceId})
	if err != nil {
		t.Fatal(err)
	}
	if read.GetOutcome() != api.Observation_OBSERVATION_UNKNOWN || read.ExecutionResources != nil || len(read.Resources) != 2 {
		t.Fatalf("unverified tencent funding was not left unknown: %v", read)
	}

	// A receipt for another obligation, kind, or outcome is not owner evidence.
	for _, receipt := range []*api.Receipt{
		{Id: "wallet-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String("other"), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED},
		{Id: "wallet-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED},
		{Id: "wallet-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_REJECTED},
		{Id: "another-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED},
	} {
		ledger.receipt = receipt
		got, err := client.EnsureResources(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if got.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || dispatcher.calls != 0 {
			t.Fatalf("unverified receipt %v dispatched: %v calls=%d", receipt, got, dispatcher.calls)
		}
	}

	ledger.receipt = &api.Receipt{Id: "wallet-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.ObligationId), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED}
	confirmed, err := client.EnsureResources(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.OperationId != pending.OperationId || confirmed.ResourceId != pending.ResourceId ||
		confirmed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || confirmed.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("verified funding did not confirm the original intent: %v", confirmed)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("verified funding dispatch calls=%d", dispatcher.calls)
	}
	if read, err = serve.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.Context, ResourceSetId: pending.ResourceId}); err != nil {
		t.Fatal(err)
	}
	if read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || read.ExecutionResources.GetAccountId() != "acct-a" ||
		read.ExecutionResources.GetDataAttachmentOperationId() != pending.OperationId+":attachment" || read.ObservedAt == nil {
		t.Fatalf("execution binding missing for Serve: %v", read)
	}
	computeRef := "ins-" + read.ExecutionResources.GetComputeAllocationId()
	kinds := map[string]string{}
	for _, resource := range read.Resources {
		kinds[resource.Kind] = resource.OpaqueProviderReference
	}
	if kinds["compute"] != computeRef || kinds["network"] != "tke-node-pool/np-basic" || kinds["storage"] == "" {
		t.Fatalf("provider resource facts missing: %v", kinds)
	}

	// The original provider effect identity, including its funding receipt, is
	// persisted so a restart observes the same purchase instead of issuing a new
	// one.
	var effect []byte
	var funding sql.NullString
	if err = db.QueryRow(`SELECT approved_input->'resourceEffectIdentity', approved_input->>'localNoChargeEvidence' FROM fabric.resource_actions WHERE command_id=$1`, pending.OperationId).Scan(&effect, &funding); err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Provider string `json:"provider"`
		Compute  struct {
			ProviderReference string `json:"providerReference"`
			ProviderRequestID string `json:"providerRequestId"`
		} `json:"compute"`
		Network struct {
			ProviderReference string `json:"providerReference"`
		} `json:"network"`
	}
	if json.Unmarshal(effect, &identity) != nil || identity.Provider != "tencent-tke" ||
		identity.Compute.ProviderReference != computeRef || !strings.HasPrefix(identity.Compute.ProviderRequestID, "req-cvm-") ||
		identity.Network.ProviderReference != "tke-node-pool/np-basic" {
		t.Fatalf("effect identity=%s", effect)
	}
	if funding.Valid {
		t.Fatal("local no-charge evidence leaked into a prepaid action")
	}
	var expiry sql.NullTime
	if err = db.QueryRow(`SELECT provider_expires_at FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, pending.ResourceId).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if !expiry.Valid || !expiry.Time.After(time.Now().UTC()) {
		t.Fatalf("prepaid expiry was not persisted: %v", expiry)
	}

	// A confirmed replay never purchases again.
	if _, err = client.EnsureResources(ctx, r); err != nil || dispatcher.calls != 1 {
		t.Fatalf("confirmed replay: err=%v calls=%d", err, dispatcher.calls)
	}
}
