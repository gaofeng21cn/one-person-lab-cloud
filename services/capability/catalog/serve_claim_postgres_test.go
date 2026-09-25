package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

type serveClaimReadback struct {
	api.OwnerCommitReadbackClient
	api.ClaimUsageReadbackClient
	evidence *api.OwnerCommitEvidence
	active   bool
	calls    int
}

func (f *serveClaimReadback) ReadOwnerCommit(context.Context, *api.ReadOwnerCommitRequest, ...grpc.CallOption) (*api.OwnerCommitEvidence, error) {
	f.calls++
	return proto.Clone(f.evidence).(*api.OwnerCommitEvidence), nil
}
func (f *serveClaimReadback) ReadClaimUsage(_ context.Context, r *api.ReadClaimUsageRequest, _ ...grpc.CallOption) (*api.ClaimUsageEvidence, error) {
	return &api.ClaimUsageEvidence{ClaimId: r.ClaimId, Owner: api.OwnerEnum_OWNER_ENUM_SERVE, ResourceId: r.ClaimantResourceId, OperationId: r.ClaimantOperationId, AcceptedInputDigest: f.evidence.AcceptedInputDigest, ActivelyRequired: f.active, Outcome: api.Observation_OBSERVATION_CONFIRMED, TerminalReceiptId: proto.String("termination-original")}, nil
}
func capabilityClaimDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip), Owner: "capability", Database: "opl_capability", SchemaOwnerRole: "opl_capability_owner", WriterRole: "opl_capability_writer", RuntimeRole: "opl_capability_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := migrations.Source()
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
func TestServeCapabilityClaimUsesExactClaimantOwner(t *testing.T) {
	db := capabilityClaimDB(t)
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Serve.Service())
	digest := "sha256:" + strings.Repeat("1", 64)
	descriptor, _ := json.Marshal(map[string]any{"schemaVersion": "opl-deployment-descriptor/v1", "artifact": map[string]string{"repository": "registry.test/agent", "digest": digest}, "provenance": "legacy_application", "legacyApplicationRevisionId": "legacy-ref", "applicationRevision": map[string]string{"image": "registry.test/agent@" + digest}})
	_, err := db.ExecContext(ctx, `INSERT INTO capability.capability_versions(id,version_label,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,legacy_application_revision_id,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES('version-claim','1','registry.test/agent',$1,'ready','[]','{}','{}','legacy_application','legacy-ref',$2,$1,'descriptor-ref')`, digest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture deliberately isolates reference lifecycle; no Ready Capability
	// or deployability evidence is asserted by this claim-only test.
	authCalls := 0
	s := &Service{DB: db, Authorize: func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		authCalls++
		return nil
	}}
	// Build lineage is a schema fixture for claim authorization only. This test
	// does not claim an OCI was built or a deployment succeeded.
	_, err = db.ExecContext(ctx, `INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES('n','tenant-claim','ns','tenant_default'); INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES('p','n','p','private','actor')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO capability.package_versions(id,package_id,version_label,sha256,size_bytes,created_by) VALUES('pv','p','1',$1,1,'actor')`, digest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('publisher','publisher','official','registry.test','registry.test','receipt')`)
	if err != nil {
		t.Fatal(err)
	}
	webui := map[string]any{"schemaVersion": "opl-publisher-contract/v1", "kind": "webui", "publisherNamespaceId": "publisher", "image": map[string]any{"repository": "registry.test/ui", "digest": digest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}}, "runtimeAbiVersions": []string{"abi"}, "uiProtocolVersion": "ui"}
	webuiRaw, _ := json.Marshal(webui)
	_, err = db.ExecContext(ctx, `INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES('ui','ui','1','registry.test/ui',$1,'actor',ARRAY['abi'],'ui','receipt','publisher',$1,$2,'ui-ref')`, digest, webuiRaw)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	json.Unmarshal(descriptor, &d)
	d["provenance"] = "build"
	delete(d, "legacyApplicationRevisionId")
	d["packageVersionId"] = "pv"
	d["runtimeContractReference"] = map[string]string{"versionId": "runtime"}
	d["webuiContractReference"] = map[string]string{"versionId": "ui"}
	descriptor, _ = json.Marshal(d)
	_, err = db.ExecContext(ctx, `UPDATE capability.capability_versions SET package_id='p',package_version_id='pv',build_job_id='build-fixture-only',runtime_version_id='runtime',webui_version_id='ui',provenance='build',legacy_application_revision_id=NULL,deployment_descriptor=$1 WHERE id='version-claim'`, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	call := &api.CallContext{ActorId: "actor", RequestId: "request", SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-claim"}}}}
	request := &api.ReferenceClaimRequest{Context: call, Target: &api.ReferenceTarget{Target: &api.ReferenceTarget_CapabilityVersionId{CapabilityVersionId: "version-claim"}}, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_SERVE, ClaimantResourceId: "deployment-original"}
	claim, err := s.AcquireReference(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.AcquireReference(ctx, request)
	if err != nil || replay.Id != claim.Id {
		t.Fatalf("replay=%v %v", replay, err)
	}
	var purpose, owner string
	if err = db.QueryRowContext(ctx, `SELECT purpose,claimant_owner FROM capability.reference_claims WHERE id=$1`, claim.Id).Scan(&purpose, &owner); err != nil || purpose != "deploy" || owner != "serve" {
		t.Fatalf("claim owner=%s purpose=%s err=%v", owner, purpose, err)
	}
	evidence := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_SERVE, OperationId: "operation-original", ResourceId: "deployment-original", AcceptedInputDigest: digest, AcceptedAt: timestamppb.New(time.Now()), CommittedVersion: 1}
	serve := &serveClaimReadback{evidence: evidence, active: true}
	build := &serveClaimReadback{evidence: evidence}
	s.ServeCommit = serve
	s.ServeUsage = serve
	s.Commit = build
	s.Usage = build
	bound, err := s.BindReference(ctx, &api.BindReferenceRequest{Context: call, ClaimId: claim.Id, OwnerCommitEvidence: evidence})
	if err != nil || bound.GetState() != api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND || serve.calls != 1 || build.calls != 0 {
		t.Fatalf("owner readback=%v %v", bound, err)
	}
	foreign := ownerservice.WithPeerOwner(ctx, owneridentity.Build.Service())
	if _, err = s.BindReference(foreign, &api.BindReferenceRequest{Context: call, ClaimId: claim.Id, OwnerCommitEvidence: evidence}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign bind=%v", err)
	}
	release := &api.ReleaseReferenceRequest{Context: call, ClaimId: claim.Id, ReleaseEvidence: &api.ReleaseEvidence{Owner: api.OwnerEnum_OWNER_ENUM_SERVE, OperationId: evidence.OperationId, ResourceId: evidence.ResourceId, TerminalReceiptId: "termination-original", TerminalStatus: api.TerminalOperationStatus_TERMINAL_OPERATION_STATUS_SUCCEEDED}}
	if _, err = s.ReleaseReference(ctx, release); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("active claim release=%v", err)
	}
	if authCalls < 3 {
		t.Fatal("live owner authorization was skipped")
	}
}
