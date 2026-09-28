package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

func TestCapabilityVersionListFiltersTenantStatusAndCursor(t *testing.T) {
	db := capabilityClaimDB(t)
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	d := "sha256:" + strings.Repeat("1", 64)
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('publisher','publisher','official','registry.test','registry.test','receipt')`)
	schemaBytes, err := os.ReadFile("../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct{ Examples []json.RawMessage }
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	webui := &api.WebuiPublisherContract{}
	runtimeContract := &api.RuntimePublisherContract{}
	if err := publicjson.Unmarshal(schema.Examples[1], webui); err != nil {
		t.Fatal(err)
	}
	if err := publicjson.Unmarshal(schema.Examples[0], runtimeContract); err != nil {
		t.Fatal(err)
	}
	webui.PublisherNamespaceId = "publisher"
	webui.Image.Digest = d
	webui.Image.Repository = "registry.test/ui"
	webui.RuntimeAbiVersions = []string{"abi"}
	webuiJSON, err := publicjson.Marshal(webui)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES('ui','ui','1','registry.test/ui',$1,'actor',ARRAY['abi'],'opl-webui/v1','receipt','publisher',$1,$2,'ui-ref')`, d, webuiJSON)
	for _, entry := range []struct{ id, tenant, visibility, state string }{{"a", "tenant-one", "private", "ready"}, {"b", "tenant-one", "private", "deprecated"}, {"c", "tenant-two", "private", "ready"}, {"d", "tenant-two", "official", "ready"}} {
		mustExec(`INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES($1,$2,$1,'tenant_custom')`, "ns-"+entry.id, entry.tenant)
		mustExec(`INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES($1,$2,$1,$3,'actor')`, "pkg-"+entry.id, "ns-"+entry.id, entry.visibility)
		mustExec(`INSERT INTO capability.package_versions(id,package_id,version_label,sha256,size_bytes,created_by) VALUES($1,$2,'1',$3,1,'actor')`, "pv-"+entry.id, "pkg-"+entry.id, d)
		artifact := &api.ArtifactReference{Repository: "registry.test/agent", Digest: d, Platform: runtimeContract.Image.Platform}
		descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD, Artifact: artifact, PackageVersionId: proto.String("pv-" + entry.id), RuntimeContract: runtimeContract, WebuiContract: webui, RuntimeContractReference: &api.PublisherContractReference{VersionId: "runtime", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME}, WebuiContractReference: &api.PublisherContractReference{VersionId: "ui", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI}, ApplicationRevision: proto.Clone(runtimeContract.ApplicationRevisionTemplate).(*api.WorkspaceApplicationRevision)}
		descriptor.ApplicationRevision.Image = "registry.test/agent@" + d
		descriptorJSON, err := publicjson.Marshal(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		evidence := &api.BuildArtifactReadback{BuildJobId: "build-" + entry.id, VersionLabel: "1", Outcome: api.Observation_OBSERVATION_CONFIRMED, Input: &api.BuildInputSnapshot{PackageId: "pkg-" + entry.id, PackageVersionId: "pv-" + entry.id, RuntimeVersionId: "runtime", WebuiVersionId: "ui"}, Artifact: artifact, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: d, DeploymentDescriptorObjectRef: "descriptor-" + entry.id, DataCompatibility: &api.DataCompatibility{}}
		mustExec(`INSERT INTO capability.capability_versions(id,package_id,package_version_id,build_job_id,version_label,runtime_version_id,webui_version_id,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,'1','runtime','ui','registry.test/agent',$5,$6,'[]','{}',$7,'build',$8,$5,$9)`, entry.id, "pkg-"+entry.id, "pv-"+entry.id, "build-"+entry.id, d, entry.state, jsonBytes(evidence), descriptorJSON, "descriptor-"+entry.id)
	}
	mustExec(`INSERT INTO capability.reference_claims(id,target_type,capability_version_id,claimant_owner,claimant_resource_id,purpose,request_id) VALUES('claim-a','capability_version','a','serve','deploy-a','deploy','request-a')`)
	var authorized bool
	s := &Service{DB: db, Authorize: func(_ context.Context, _ *api.CallContext, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, scope ownerservice.ResourceScope) error {
		authorized = true
		if scope.TenantID != "tenant-one" || (action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCAPABILITYVERSIONS && resource.GetId() != "") {
			return fmt.Errorf("wrong authorization boundary")
		}
		return nil
	}}
	call := &api.CallContext{ActorId: "actor", RequestId: "request", SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-one"}}}}
	packages, err := s.ListPackages(ctx, &api.ListPackagesRpcRequest{Context: call})
	if err != nil || len(packages.GetItems()) != 3 {
		t.Fatalf("package list=%v err=%v", packages, err)
	}
	for _, item := range packages.Items {
		want := ""
		if item.Id == "pkg-a" {
			want = "a"
		}
		if item.Id == "pkg-d" {
			want = "d"
		}
		if item.GetLatestReadyVersionId() != want {
			t.Fatalf("package %s latest ready=%s, want %s", item.Id, item.GetLatestReadyVersionId(), want)
		}
	}
	item, err := s.GetPackage(ctx, &api.GetPackageRpcRequest{Context: call, PackageId: "pkg-a"})
	if err != nil || item.GetLatestReadyVersionId() != "a" {
		t.Fatalf("package detail=%v err=%v", item, err)
	}
	request := &api.ListCapabilityVersionsRpcRequest{Context: call, QueryLimit: proto.Int32(1), QueryStatus: api.ListCapabilityVersionsRpcRequestStatusEnum_LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_READY.Enum()}
	page, err := s.ListCapabilityVersions(ctx, request)
	if err != nil || !authorized || len(page.GetItems()) != 1 || page.Items[0].Id != "a" || page.Items[0].ReferenceCount != 1 || page.GetNextCursor() != "a" {
		t.Fatalf("first page=%v err=%v", page, err)
	}
	request.QueryCursor = page.NextCursor
	page, err = s.ListCapabilityVersions(ctx, request)
	if err != nil || len(page.GetItems()) != 1 || page.Items[0].Id != "d" || page.GetNextCursor() != "" {
		t.Fatalf("second page=%v err=%v", page, err)
	}
	request.QueryCursor = nil
	request.QueryPackageId = proto.String("pkg-c")
	page, err = s.ListCapabilityVersions(ctx, request)
	if err != nil || len(page.GetItems()) != 0 {
		t.Fatalf("foreign private package=%v err=%v", page, err)
	}
	request.QueryPackageId = proto.String("pkg-b")
	request.QueryStatus = nil
	page, err = s.ListCapabilityVersions(ctx, request)
	if err != nil || len(page.GetItems()) != 1 || page.Items[0].Status != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_DEPRECATED {
		t.Fatalf("package filter=%v err=%v", page, err)
	}
}
