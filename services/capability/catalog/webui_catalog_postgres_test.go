package catalog

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

const publisherSchemaPath = "../../../docs/spec/target/contracts/publisher-contract.schema.json"

func publisherSchemaExamples(t *testing.T) (schema []byte, example []json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(publisherSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return raw, document.Examples
}

// TestWebuiCatalogBindsAndReadsBackTheExactPublisherContract proves a WebUI
// version is admitted from the approved publisher and that the readback is the
// exact admitted contract bytes, not a re-derived summary.
func TestWebuiCatalogBindsAndReadsBackTheExactPublisherContract(t *testing.T) {
	db := capabilityClaimDB(t)
	schema, examples := publisherSchemaExamples(t)
	store, err := ownerstore.New(db, "capability")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: db, Store: store, Authorize: func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		return nil
	}}
	if err = service.ConfigurePublisherSchema(publisherSchemaPath, digest(schema)); err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	if _, err = db.ExecContext(ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('pub_official','official','official','registry.example','registry.example/official','receipt')`); err != nil {
		t.Fatal(err)
	}
	contract := &api.WebuiPublisherContract{}
	if err = publicjson.Unmarshal(examples[1], contract); err != nil {
		t.Fatal(err)
	}
	wantBytes, err := publicjson.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	call := &api.CallContext{ActorId: "actor", RequestId: "request", SessionId: proto.String("session"), IdempotencyKey: "webui-1", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}
	admitted, err := service.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: call, Body: &api.RegisterWebuiVersionRequest{Name: "ui", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: contract, AdmissionReceiptId: "receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.PublisherContractDigest != digest(wantBytes) || admitted.PublisherContractObjectRef != "webui-contract:"+admitted.Id+"@"+digest(wantBytes) {
		t.Fatalf("admitted contract identity=%s ref=%s", admitted.PublisherContractDigest, admitted.PublisherContractObjectRef)
	}
	// publisher_contract is a jsonb column, so PostgreSQL re-serializes the
	// object it stores (key order and spacing). The admitted identity is the
	// owner's canonical encoding, so the stored object must decode back to
	// exactly that canonical encoding and digest.
	var stored []byte
	if err = db.QueryRowContext(ctx, `SELECT publisher_contract FROM capability.webui_versions WHERE id=$1`, admitted.Id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	decoded := &api.WebuiPublisherContract{}
	if err = publicjson.Unmarshal(stored, decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := publicjson.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(wantBytes) || digest(canonical) != admitted.PublisherContractDigest {
		t.Fatalf("stored contract canonical=%s want=%s", canonical, wantBytes)
	}

	page, err := service.ListWebuiVersions(ctx, &api.ListWebuiVersionsRpcRequest{Context: call})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetItems()) != 1 {
		t.Fatalf("webui page=%v", page)
	}
	readback, err := publicjson.Marshal(page.Items[0].PublisherContract)
	if err != nil {
		t.Fatal(err)
	}
	if string(readback) != string(wantBytes) || page.Items[0].PublisherContractDigest != digest(wantBytes) {
		t.Fatalf("readback contract bytes=%s digest=%s", readback, page.Items[0].PublisherContractDigest)
	}
	if page.Items[0].ArtifactDigest != contract.Image.Digest || page.Items[0].PublisherNamespaceId != contract.PublisherNamespaceId {
		t.Fatalf("readback artifact=%s", page.Items[0])
	}
}

// TestWebuiCatalogRefusesUnadmittedPublisherAndContract pins the admission
// boundary: an image outside the admitted publisher prefix and a contract that
// does not satisfy the approved schema are both refused.
func TestWebuiCatalogRefusesUnadmittedPublisherAndContract(t *testing.T) {
	db := capabilityClaimDB(t)
	schema, examples := publisherSchemaExamples(t)
	store, err := ownerstore.New(db, "capability")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: db, Store: store, Authorize: func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		return nil
	}}
	if err = service.ConfigurePublisherSchema(publisherSchemaPath, digest(schema)); err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	if _, err = db.ExecContext(ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('pub_official','official','official','registry.example','registry.example/official','receipt')`); err != nil {
		t.Fatal(err)
	}
	call := &api.CallContext{ActorId: "actor", RequestId: "request", SessionId: proto.String("session"), IdempotencyKey: "webui-2", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}

	foreign := &api.WebuiPublisherContract{}
	if err = publicjson.Unmarshal(examples[1], foreign); err != nil {
		t.Fatal(err)
	}
	foreign.Image.Repository = "registry.example/third-party/acme/webui"
	if _, err = service.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: call, Body: &api.RegisterWebuiVersionRequest{Name: "ui", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: foreign, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("image outside the admitted publisher=%v", err)
	}

	invalid := proto.Clone(&api.WebuiPublisherContract{}).(*api.WebuiPublisherContract)
	if err = publicjson.Unmarshal(examples[1], invalid); err != nil {
		t.Fatal(err)
	}
	invalid.Image.Digest = "not-an-immutable-digest"
	if _, err = service.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: call, Body: &api.RegisterWebuiVersionRequest{Name: "ui", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: invalid, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("contract failing the approved schema=%v", err)
	}

	// The owning namespace must be admitted; a revoked publisher admits nothing.
	if _, err = db.ExecContext(ctx, `UPDATE capability.publisher_namespaces SET status='revoked' WHERE id='pub_official'`); err != nil {
		t.Fatal(err)
	}
	ok := &api.WebuiPublisherContract{}
	if err = publicjson.Unmarshal(examples[1], ok); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: call, Body: &api.RegisterWebuiVersionRequest{Name: "ui", VersionLabel: "1", PublisherNamespaceId: "pub_official", PublisherContract: ok, AdmissionReceiptId: "receipt"}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked publisher namespace=%v", err)
	}
}
