package catalog

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

// allowIdentity is the CloudIdentity authorization authority stub: it allows
// exactly the request it receives, with a matching decision shape, because these
// tests prove the Runtime catalog's own admission and readback, not CloudIdentity.
type allowIdentity struct {
	api.CloudIdentityAuthorizationClient
}

func (allowIdentity) AuthorizeAction(_ context.Context, request *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	now := time.Now()
	return &api.AuthorizationDecision{
		Result:                   api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		Issuer:                   api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Scope:                    request.Scope,
		ActorId:                  request.ActorId,
		SessionId:                request.SessionId,
		AudienceOwner:            request.AudienceOwner,
		Action:                   request.Action,
		Resource:                 request.Resource,
		PermissionVersion:        1,
		IssuedAt:                 timestamppb.New(now),
		ExpiresAt:                timestamppb.New(now.Add(time.Hour)),
		AcceptedOperationGrantId: request.AcceptedOperationGrantId,
	}, nil
}

// admittedPublishers is the Capability readback the Runtime owner admits against.
type admittedPublishers struct {
	api.CapabilityProductServiceClient
	page *api.PublisherNamespacePage
}

func (a admittedPublishers) ListPublisherNamespaces(context.Context, *api.ListPublisherNamespacesRpcRequest, ...grpc.CallOption) (*api.PublisherNamespacePage, error) {
	return a.page, nil
}

const runtimePublisherSchemaPath = "../../../docs/spec/target/contracts/publisher-contract.schema.json"

// runtimeService builds the real Runtime catalog owner on a real isolated
// database, with a CloudIdentity stub and the Capability publisher readback.
func runtimeService(t *testing.T) *Service {
	t.Helper()
	return runtimeServiceWithPublishers(t, officialPublisherPage(api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED))
}

// officialPublisherPage is the mutable Capability readback a test can revoke to
// prove the Runtime owner re-reads publisher admission instead of caching it.
func officialPublisherPage(status api.PublisherNamespaceStatusEnum) *api.PublisherNamespacePage {
	return &api.PublisherNamespacePage{Items: []*api.PublisherNamespace{{Id: "pub_official", Name: "official", RegistryId: "registry.example", RepositoryPrefix: "registry.example/official", AdmissionReceiptId: "receipt", Status: status}}}
}

func runtimeServiceWithPublishers(t *testing.T, page *api.PublisherNamespacePage) *Service {
	t.Helper()
	db := newRuntimeControlHarness(t)
	schemaBytes, err := os.ReadFile(runtimePublisherSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(db.DB, ownerservice.NewAuthorizer(ownerservice.OwnerRuntimeControl, allowIdentity{}), admittedPublishers{page: page}, runtimePublisherSchemaPath, digest(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func runtimeExample(t *testing.T) (*api.RuntimePublisherContract, []byte) {
	t.Helper()
	raw, err := os.ReadFile(runtimePublisherSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	contract := &api.RuntimePublisherContract{}
	if err = publicjson.Unmarshal(document.Examples[0], contract); err != nil {
		t.Fatal(err)
	}
	canonical, err := publicjson.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	return contract, canonical
}

func adminCall(key string) *api.CallContext {
	return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, SessionId: proto.String("session"), IdempotencyKey: key, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}
}

// TestRuntimeReleaseBindsAndReadsBackTheExactPublisherContract proves a Runtime
// Release is admitted from an approved publisher and that its identity is the
// canonical publisher contract bytes, read back byte-identically.
func TestRuntimeReleaseBindsAndReadsBackTheExactPublisherContract(t *testing.T) {
	service := runtimeService(t)
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	contract, canonical := runtimeExample(t)
	admitted, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-1"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: contract, AdmissionReceiptId: "receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.ArtifactDigest != contract.Image.Digest || admitted.PublisherContractDigest != digest(canonical) || admitted.PublisherContractObjectRef != "runtime-contract:"+admitted.Id+"@"+digest(canonical) {
		t.Fatalf("admitted release=%v", admitted)
	}
	if admitted.Status != api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED || admitted.RuntimeAbiVersion != contract.RuntimeAbiVersion {
		t.Fatalf("admitted release readback=%v", admitted)
	}

	page, err := service.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: &api.CallContext{ActorId: "actor", RequestId: "request-list", SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-a"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetItems()) != 1 {
		t.Fatalf("runtime page=%v", page)
	}
	readback, err := publicjson.Marshal(page.Items[0].PublisherContract)
	if err != nil {
		t.Fatal(err)
	}
	if string(readback) != string(canonical) || page.Items[0].PublisherContractDigest != digest(canonical) {
		t.Fatalf("readback contract=%s digest=%s", readback, page.Items[0].PublisherContractDigest)
	}
	var storedDigest string
	if err = service.DB.QueryRowContext(ctx, `SELECT publisher_contract_digest FROM runtime_control.runtime_releases WHERE id=$1`, admitted.Id).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	if storedDigest != digest(canonical) {
		t.Fatalf("stored digest=%s", storedDigest)
	}
}

// TestRuntimeReleaseRefusesUnadmittedPublisherAndContract pins the admission
// boundary: a repository outside the admitted publisher prefix, a receipt-less
// admission and a contract that fails the approved schema are all refused.
func TestRuntimeReleaseRefusesUnadmittedPublisherAndContract(t *testing.T) {
	publishers := officialPublisherPage(api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED)
	service := runtimeServiceWithPublishers(t, publishers)
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	contract, _ := runtimeExample(t)

	foreign := proto.Clone(contract).(*api.RuntimePublisherContract)
	foreign.Image.Repository = "registry.example/third-party/acme/runtime"
	if _, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-2"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: foreign, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("image outside the admitted publisher=%v", err)
	}

	invalid := proto.Clone(contract).(*api.RuntimePublisherContract)
	invalid.Image.Digest = "not-an-immutable-digest"
	if _, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-3"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: invalid, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("contract failing the approved schema=%v", err)
	}

	if _, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-4"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: contract}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("admission without a receipt=%v", err)
	}

	// A namespace the Capability owner no longer admits is never trusted: the
	// Runtime owner re-reads publisher admission instead of caching an approval.
	publishers.Items[0].Status = api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_REVOKED
	if _, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-5"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: contract, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked publisher namespace=%v", err)
	}
}

func TestRuntimeReleaseRejectsForeignNamespaceContract(t *testing.T) {
	service := runtimeService(t)
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	contract, _ := runtimeExample(t)
	if _, err := service.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: adminCall("runtime-6"), Body: &api.RegisterRuntimeVersionRequest{Name: "runtime", VersionLabel: "1", PublisherNamespaceId: "pub_acme", PublisherContract: contract, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("contract from another publisher=%v", err)
	}
}
