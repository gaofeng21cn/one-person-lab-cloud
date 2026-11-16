package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

// directCapabilityService builds the real Capability owner on a real isolated
// database with a direct-to-storage Store Provider, so upload reconciliation and
// assembly recovery run through the owner API rather than a stub.
func directCapabilityService(t *testing.T, db *sql.DB, store Storage) *Service {
	t.Helper()
	root := t.TempDir()
	schema := []byte(`{"type":"object"}`)
	path := filepath.Join(root, "schema.json")
	if err := os.WriteFile(path, schema, 0600); err != nil {
		t.Fatal(err)
	}
	objects, err := NewObjectsWithStorage(store, nil, UploadPolicy{MaxBytes: 1 << 20, PartBytes: 64, MaxExpandedBytes: 2 << 20, MaxFiles: 128, TTL: time.Hour, ManifestPath: "manifest.json", SchemaPath: path, SchemaDigest: digest(schema)})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(db, func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		return nil
	}, objects)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func seedUploadPackage(t *testing.T, db *sql.DB, tenant, namespace, pkg string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES($1,$2,$1,'tenant_default')`, namespace, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES($1,$2,$1,'private','actor')`, pkg, namespace); err != nil {
		t.Fatal(err)
	}
}

func uploadCall(tenant, key string) *api.CallContext {
	return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, SessionId: proto.String("session"), IdempotencyKey: key, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
}

// archiveParts splits the package archive into the policy-sized shards the owner
// admits, so the shard identity the client claims is derived from the exact
// bytes rather than hand-written.
func archiveParts(t *testing.T, archive []byte, partSize int64) []*api.UploadPart {
	t.Helper()
	var parts []*api.UploadPart
	for offset := int64(0); offset < int64(len(archive)); {
		size := partSize
		if remaining := int64(len(archive)) - offset; remaining < size {
			size = remaining
		}
		chunk := archive[offset : offset+size]
		parts = append(parts, &api.UploadPart{PartNumber: int32(len(parts) + 1), SizeBytes: size, Sha256: digest(chunk)})
		offset += size
	}
	return parts
}

func putShard(t *testing.T, client *http.Client, authorization *api.UploadPartAuthorization, archive []byte, part *api.UploadPart, partSize int64) string {
	t.Helper()
	offset := int64(part.PartNumber-1) * partSize
	request, err := http.NewRequest(http.MethodPut, authorization.Url, bytes.NewReader(archive[offset:offset+part.SizeBytes]))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = part.SizeBytes
	request.Header.Set(authorization.RequiredChecksumHeaderName, part.Sha256)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		t.Fatalf("presigned shard upload status=%d", response.StatusCode)
	}
	return response.Header.Get("ETag")
}

// startDirectUpload drives the owner's real upload API up to the point where the
// client holds a registered shard identity and a presigned provider URL.
func startDirectUpload(t *testing.T, service *Service, ctx context.Context, pkg, tenant, key string, archive []byte) (*api.UploadSession, []*api.UploadPart, []*api.UploadPartAuthorization) {
	t.Helper()
	call := uploadCall(tenant, key)
	session, err := service.CreateUpload(ctx, &api.CreateUploadRpcRequest{Context: call, PackageId: pkg, Body: &api.CreateUploadRequest{VersionLabel: "1", SizeBytes: int64(len(archive)), Sha256: digest(archive), FileName: "candidate.zip"}})
	if err != nil {
		t.Fatal(err)
	}
	parts := archiveParts(t, archive, session.PartSizeBytes)
	authorizations := make([]*api.UploadPartAuthorization, 0, len(parts))
	for _, part := range parts {
		authorization, err := service.CreateUploadPart(ctx, &api.CreateUploadPartRpcRequest{Context: call, UploadId: session.Id, Body: &api.CreateUploadPartRequest{PartNumber: part.PartNumber, SizeBytes: part.SizeBytes, Sha256: part.Sha256}})
		if err != nil {
			t.Fatal(err)
		}
		authorizations = append(authorizations, authorization)
	}
	return session, parts, authorizations
}

func TestGetUploadReconcilesDirectProviderShardsForResume(t *testing.T) {
	db := capabilityClaimDB(t)
	server := httptest.NewTLSServer(newFakeCOS())
	defer server.Close()
	service := directCapabilityService(t, db, fakeCOSStorage(t, server))
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	seedUploadPackage(t, db, "tenant-a", "ns-a", "pkg-a")

	archive, err := os.ReadFile(candidateArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	session, parts, authorizations := startDirectUpload(t, service, ctx, "pkg-a", "tenant-a", "resume", archive)
	if len(parts) < 2 {
		t.Fatalf("fixture must produce at least two shards, got %d", len(parts))
	}
	// The browser uploads only the first shard, then the page is refreshed.
	putShard(t, server.Client(), authorizations[0], archive, parts[0], session.PartSizeBytes)

	call := uploadCall("tenant-a", "resume")
	readback, err := service.GetUpload(ctx, &api.GetUploadRpcRequest{Context: call, UploadId: session.Id})
	if err != nil {
		t.Fatal(err)
	}
	if readback.Status != api.UploadSessionStatusEnum_UPLOAD_SESSION_STATUS_ENUM_UPLOADING {
		t.Fatalf("status=%v", readback.Status)
	}
	if len(readback.CompletedParts) != 1 || readback.CompletedParts[0].PartNumber != 1 {
		t.Fatalf("a resumed upload must see exactly the shard the provider holds: %+v", readback.CompletedParts)
	}
	if readback.CompletedParts[0].SizeBytes != parts[0].SizeBytes || readback.CompletedParts[0].Sha256 != parts[0].Sha256 {
		t.Fatalf("reconciled shard identity=%+v want=%+v", readback.CompletedParts[0], parts[0])
	}
	// The owner readback is a view, not a second writer: reconciliation must not
	// mutate the data-plane observation the local provider's ingest records.
	var observation string
	if err = db.QueryRowContext(ctx, `SELECT observation_result FROM capability.upload_chunks WHERE upload_session_id=$1 AND part_number=1`, session.Id).Scan(&observation); err != nil {
		t.Fatal(err)
	}
	if observation != "unknown" {
		t.Fatalf("reconciliation must not rewrite the owner data-plane observation, got %q", observation)
	}

	// A shard the provider never received is not reported as present.
	if remote := providerShardCount(t, service, session.Id, digest(archive)); remote != 1 {
		t.Fatalf("provider shards=%d", remote)
	}
	// Cross-tenant readback of the same upload id is refused.
	foreign := uploadCall("tenant-b", "resume")
	if _, err = service.GetUpload(ctx, &api.GetUploadRpcRequest{Context: foreign, UploadId: session.Id}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-tenant upload readback=%v", err)
	}
}

func providerShardCount(t *testing.T, service *Service, upload, digest string) int {
	t.Helper()
	ref, err := service.providerUploadRef(context.Background(), upload)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Objects.store.ListParts(context.Background(), upload, ref, digest)
	if err != nil {
		t.Fatal(err)
	}
	return len(view.Parts)
}

type uploadOperationIdentity struct {
	api.CloudIdentityAuthorizationClient
}

func (*uploadOperationIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, ActorId: r.ActorId, Scope: r.Scope, SessionId: r.SessionId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}

// Completion, both replay paths and the shared owner read must expose the same
// terminal evidence. Marshal through the BFF's public serializer as well: enum
// defaults or an unknown persisted error must not turn a valid owner result into
// a missing field or an unreadable operation.
func assertUploadCompletionEvidence(t *testing.T, service *Service, ctx context.Context, call *api.CallContext, uploadID string, parts []*api.UploadPart, operation *api.Operation, succeeded bool) {
	t.Helper()
	wantStatus, wantStage, wantObservation, wantError := "succeeded", "succeeded", "confirmed", ""
	if !succeeded {
		wantStatus, wantStage, wantObservation, wantError = "failed", "upload_verification", "rejected", "VALIDATION_FAILED"
	}
	assertPublic := func(label string, result *api.Operation) {
		t.Helper()
		raw, err := publicjson.Marshal(result)
		if err != nil {
			t.Fatalf("%s public operation: %v", label, err)
		}
		var body map[string]any
		if err = json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body["operationId"] != operation.OperationId || body["resourceId"] != operation.ResourceId || body["owner"] != "capability" || body["kind"] != "complete_upload" || body["status"] != wantStatus || body["stage"] != wantStage || body["observationResult"] != wantObservation || result.PollAfterSeconds != nil {
			t.Fatalf("%s terminal evidence=%s", label, raw)
		}
		if wantError == "" && result.ErrorCode != nil || wantError != "" && body["errorCode"] != wantError {
			t.Fatalf("%s error evidence=%s", label, raw)
		}
	}
	assertPublic("completion", operation)
	for _, key := range []string{call.IdempotencyKey, call.IdempotencyKey + "-completed-replay"} {
		replayCall := proto.Clone(call).(*api.CallContext)
		replayCall.IdempotencyKey = key
		replayCall.RequestId = "request-" + key
		replayed, err := service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: replayCall, UploadId: uploadID, Body: &api.CompleteUploadRequest{Parts: parts}})
		if err != nil || !proto.Equal(replayed, operation) {
			t.Fatalf("completion replay %q=%v error=%v, want %v", key, replayed, err, operation)
		}
		assertPublic("replay", replayed)
	}
	stored, err := service.Store.ReadOperation(ctx, operation.OperationId)
	if err != nil || stored.Status != wantStatus || stored.Stage != wantStage || stored.Observation != wantObservation || stored.ErrorCode != wantError || stored.CompletedAt.IsZero() {
		t.Fatalf("stored terminal evidence=%+v error=%v", stored, err)
	}
	persisted := &api.Operation{}
	if err = protojson.Unmarshal(stored.Result, persisted); err != nil || !proto.Equal(persisted, operation) {
		t.Fatalf("persisted completion=%v error=%v", persisted, err)
	}
	operations, err := ownerservice.NewOperations(ownerservice.OwnerCapability, service.Store, ownerservice.NewAuthorizer(ownerservice.OwnerCapability, &uploadOperationIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	readback, err := operations.Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: operation.OperationId})
	if err != nil {
		t.Fatalf("owner operation readback: %v", err)
	}
	assertPublic("owner readback", readback)
	var count int
	if err = service.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.operations WHERE resource_id=$1 AND kind='complete_upload'`, operation.ResourceId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("completion replay operation count=%d error=%v", count, err)
	}
}

func TestCompleteUploadFinalizesDirectProviderUploadAndReadsBack(t *testing.T) {
	db := capabilityClaimDB(t)
	server := httptest.NewTLSServer(newFakeCOS())
	defer server.Close()
	service := directCapabilityService(t, db, fakeCOSStorage(t, server))
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	seedUploadPackage(t, db, "tenant-a", "ns-a", "pkg-a")

	archive, err := os.ReadFile(candidateArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	session, parts, authorizations := startDirectUpload(t, service, ctx, "pkg-a", "tenant-a", "complete", archive)
	for index, part := range parts {
		putShard(t, server.Client(), authorizations[index], archive, part, session.PartSizeBytes)
	}
	call := uploadCall("tenant-a", "complete")
	// A client that claims a shard identity the owner never registered is refused.
	impostor := uploadCall("tenant-a", "complete")
	badParts := append([]*api.UploadPart{}, parts...)
	badParts[0] = &api.UploadPart{PartNumber: 1, SizeBytes: parts[0].SizeBytes, Sha256: digest([]byte("not the shard"))}
	if _, err = service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: impostor, UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: badParts}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unregistered shard identity=%v", err)
	}
	// A client that claims a partial shard list is refused.
	if _, err = service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: uploadCall("tenant-a", "complete"), UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: parts[:len(parts)-1]}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("partial shard list=%v", err)
	}

	readback, err := service.GetUpload(ctx, &api.GetUploadRpcRequest{Context: call, UploadId: session.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(readback.CompletedParts) != len(parts) {
		t.Fatalf("a refreshed client must see every provider shard: %+v", readback.CompletedParts)
	}
	operation, err := service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: call, UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: readback.CompletedParts}})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("operation=%v", operation)
	}
	assertUploadCompletionEvidence(t, service, ctx, call, session.Id, readback.CompletedParts, operation, true)

	var state, objectRef string
	if err = db.QueryRowContext(ctx, `SELECT status,object_ref FROM capability.package_versions WHERE id=$1`, session.PackageVersionId).Scan(&state, &objectRef); err != nil {
		t.Fatal(err)
	}
	if state != "uploaded" || objectRef != digest(archive) {
		t.Fatalf("package version state=%q object_ref=%q", state, objectRef)
	}
	var sessionStatus string
	if err = db.QueryRowContext(ctx, `SELECT status FROM capability.upload_sessions WHERE id=$1`, session.Id).Scan(&sessionStatus); err != nil {
		t.Fatal(err)
	}
	if sessionStatus != "completed" {
		t.Fatalf("session status=%q", sessionStatus)
	}

	// The immutable object is readable back through the data plane with the exact
	// admitted bytes, and a direct provider exposes no part ingest route.
	handler, err := service.DataHandler(strings.Repeat("t", 32))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/objects/"+strings.TrimPrefix(digest(archive), "sha256:"), nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body, _ := io.ReadAll(response.Result().Body)
	if response.Code != http.StatusOK || !bytes.Equal(body, archive) {
		t.Fatalf("artifact readback status=%d bytes=%d want=%d", response.Code, len(body), len(archive))
	}
	ingest := httptest.NewRecorder()
	handler.ServeHTTP(ingest, httptest.NewRequest(http.MethodPut, "/parts", nil))
	if ingest.Code != http.StatusNotFound {
		t.Fatalf("direct provider must not expose the ingest route, status=%d", ingest.Code)
	}
}

// TestCompleteUploadRecoversArtifactCopiedBeforeOwnerCommit reproduces the crash
// window the target design calls out: Assemble copied the admitted bytes to their
// immutable key and cleaned up staging, but the owner transaction never
// committed. The retried Complete must finalize from the immutable object.
func TestCompleteUploadRecoversArtifactCopiedBeforeOwnerCommit(t *testing.T) {
	db := capabilityClaimDB(t)
	server := httptest.NewTLSServer(newFakeCOS())
	defer server.Close()
	service := directCapabilityService(t, db, fakeCOSStorage(t, server))
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	seedUploadPackage(t, db, "tenant-a", "ns-a", "pkg-a")

	archive, err := os.ReadFile(candidateArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	session, parts, authorizations := startDirectUpload(t, service, ctx, "pkg-a", "tenant-a", "crash", archive)
	for index, part := range parts {
		putShard(t, server.Client(), authorizations[index], archive, part, session.PartSizeBytes)
	}
	confirmed := make([]ConfirmedPart, 0, len(parts))
	for _, part := range parts {
		confirmed = append(confirmed, ConfirmedPart{PartNumber: part.PartNumber, SizeBytes: part.SizeBytes, Sha256: part.Sha256})
	}
	ref, err := service.providerUploadRef(ctx, session.Id)
	if err != nil {
		t.Fatal(err)
	}
	// The effect the owner produced just before crashing: bytes assembled into
	// the immutable key, staging cleaned up, no owner row committed.
	assembled, err := service.Objects.store.Assemble(ctx, session.Id, ref, digest(archive), int64(len(archive)), confirmed)
	if err != nil {
		t.Fatal(err)
	}
	assembled.Cleanup()

	call := uploadCall("tenant-a", "crash")
	operation, err := service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: call, UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: parts}})
	if err != nil {
		t.Fatalf("retry after a pre-commit crash must recover: %v", err)
	}
	if operation.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("operation=%v", operation)
	}
	var state, objectRef string
	if err = db.QueryRowContext(ctx, `SELECT status,object_ref FROM capability.package_versions WHERE id=$1`, session.PackageVersionId).Scan(&state, &objectRef); err != nil {
		t.Fatal(err)
	}
	if state != "uploaded" || objectRef != digest(archive) {
		t.Fatalf("recovered package version state=%q object_ref=%q", state, objectRef)
	}
}

// TestCompleteUploadRejectsWrongDeclaredDigest pins that a customer-declared
// digest is verified against the assembled bytes, not trusted.
func TestCompleteUploadRejectsWrongDeclaredDigest(t *testing.T) {
	db := capabilityClaimDB(t)
	server := httptest.NewTLSServer(newFakeCOS())
	defer server.Close()
	service := directCapabilityService(t, db, fakeCOSStorage(t, server))
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	seedUploadPackage(t, db, "tenant-a", "ns-a", "pkg-a")

	archive, err := os.ReadFile(candidateArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	// The client declares the digest of different bytes than the shards it
	// uploads; the provider must not accept the object on the declared digest.
	declared := digest([]byte("other bytes with the same length!!"))
	if len(declared) != len(digest(archive)) {
		declared = digest([]byte("different content, same declared length as package"))
	}
	call := uploadCall("tenant-a", "mismatch")
	session, err := service.CreateUpload(ctx, &api.CreateUploadRpcRequest{Context: call, PackageId: "pkg-a", Body: &api.CreateUploadRequest{VersionLabel: "1", SizeBytes: int64(len(archive)), Sha256: declared, FileName: "candidate.zip"}})
	if err != nil {
		t.Fatal(err)
	}
	parts := archiveParts(t, archive, session.PartSizeBytes)
	for _, part := range parts {
		if _, err = service.CreateUploadPart(ctx, &api.CreateUploadPartRpcRequest{Context: call, UploadId: session.Id, Body: &api.CreateUploadPartRequest{PartNumber: part.PartNumber, SizeBytes: part.SizeBytes, Sha256: part.Sha256}}); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := service.providerUploadRef(ctx, session.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		offset := int64(part.PartNumber-1) * session.PartSizeBytes
		put, err := http.NewRequest(http.MethodPut, server.URL+"/staging/"+session.Id+"?partNumber="+strconv.Itoa(int(part.PartNumber))+"&uploadId="+ref, bytes.NewReader(archive[offset:offset+part.SizeBytes]))
		if err != nil {
			t.Fatal(err)
		}
		put.ContentLength = part.SizeBytes
		response, err := server.Client().Do(put)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("shard upload status=%d", response.StatusCode)
		}
	}
	operation, err := service.CompleteUpload(ctx, &api.CompleteUploadRpcRequest{Context: call, UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: parts}})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED {
		t.Fatalf("wrong declared digest must fail the operation: %v", operation)
	}
	assertUploadCompletionEvidence(t, service, ctx, call, session.Id, parts, operation, false)
	var state, validation string
	if err = db.QueryRowContext(ctx, `SELECT status,COALESCE(validation_error_code,'') FROM capability.package_versions WHERE id=$1`, session.PackageVersionId).Scan(&state, &validation); err != nil {
		t.Fatal(err)
	}
	if state != "rejected" || validation != "PACKAGE_DIGEST_MISMATCH" {
		t.Fatalf("rejected state=%q validation=%q", state, validation)
	}
}
