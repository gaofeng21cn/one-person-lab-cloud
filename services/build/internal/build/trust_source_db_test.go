package build

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// The Build owner publishes the workspace image trust source admission for the
// repository its own artifact was published to. These tests run that producer
// against the owner's real migrated database, so the intent, the retry and the
// open item are the rows the running owner keeps — not a description of them.

// fakeTrustSource is a Control Plane that keeps the trust source in memory.
type fakeTrustSource struct {
	source     contracts.WorkspaceImageTrustSource
	registered []contracts.WorkspaceImageReleaseActivationRequest
	failure    error
}

func (f *fakeTrustSource) TrustSource(context.Context) (contracts.WorkspaceImageTrustSource, error) {
	if f.failure != nil {
		return contracts.WorkspaceImageTrustSource{}, f.failure
	}
	return f.source, nil
}

func (f *fakeTrustSource) RegisterRepository(_ context.Context, request contracts.WorkspaceImageReleaseActivationRequest) (contracts.WorkspaceImageTrustSource, error) {
	if f.failure != nil {
		return contracts.WorkspaceImageTrustSource{}, f.failure
	}
	f.registered = append(f.registered, request)
	f.source.Repositories = append(f.source.Repositories, request.ReleaseRepository)
	f.source.Revision++
	return f.source, nil
}

// buildTrustSourceDatabase provisions the Build owner's isolated database and
// installs the owner's real migration entrypoint.
func buildTrustSourceDatabase(t *testing.T) *sql.DB {
	t.Helper()
	adminDSN := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	harness, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN:        adminDSN,
		Owner:           "build",
		Database:        "opl_build",
		SchemaOwnerRole: "opl_build_owner",
		WriterRole:      "opl_build_writer",
		RuntimeRole:     "opl_build_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated build database: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })
	if err := harness.Install(ctx, harness.OwnerDSN, harness.DatabaseName(), source); err != nil {
		t.Fatalf("install build migrations: %v", err)
	}
	db, err := harness.Open(ctx, harness.RuntimeDSN, harness.DatabaseName())
	if err != nil {
		t.Fatalf("open isolated build database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedPublishedArtifact records one confirmed build artifact and the admission
// intent the owner wrote with it.
func seedPublishedArtifact(t *testing.T, db *sql.DB, jobID, repository string) {
	t.Helper()
	ctx := context.Background()
	digest := "sha256:" + strings.Repeat("a", 64)
	descriptorDigest := "sha256:" + strings.Repeat("b", 64)
	descriptor := `{"schemaVersion":"opl-deployment-descriptor/v1","artifact":{"repository":"` + repository + `","digest":"` + digest + `"},"applicationRevision":{"image":"` + repository + `@` + digest + `"}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO build.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input) VALUES($1,'tenant','actor_1','build',$2,'awaiting_confirmation','registering','request_1','{}'::jsonb)`, "op_"+jobID, jobID); err != nil {
		t.Fatalf("insert build operation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO build.build_jobs(id,tenant_id,package_version_id,runtime_version_id,webui_version_id,input_digest,input_snapshot,status,stage,artifact_digest,request_id,created_by,operation_id,catalog_policy_id) VALUES($1,'tenant','pv_1','runtime_1','webui_1',$2,'{}'::jsonb,'registering','registering',$3,'request_1','actor_1',$4,'policy_1')`, jobID, digest, digest, "op_"+jobID); err != nil {
		t.Fatalf("insert build job: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO build.build_artifacts(id,build_job_id,artifact_repository,artifact_digest,size_bytes,provenance,verification_evidence_ref,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,1024,'{}'::jsonb,$3,$5,$6,$7)`, "artifact_"+jobID, jobID, repository, digest, descriptor, descriptorDigest, "build-artifact://artifact_"+jobID+"/descriptor@"+descriptorDigest); err != nil {
		t.Fatalf("insert build artifact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO build.build_trust_source_registrations(id,build_job_id,release_repository,release_version) VALUES($1,$2,$3,$4)`, "tstr_"+jobID, jobID, repository, jobID); err != nil {
		t.Fatalf("insert admission intent: %v", err)
	}
}

func registrationRow(t *testing.T, db *sql.DB, jobID string) (status string, revision sql.NullInt64, attempts int, lastError sql.NullString, nextAttempt time.Time) {
	t.Helper()
	var repository, version string
	if err := db.QueryRow(`SELECT status,trust_source_revision,attempt_count,last_error_code,next_attempt_at,release_repository,release_version FROM build.build_trust_source_registrations WHERE build_job_id=$1`, jobID).Scan(&status, &revision, &attempts, &lastError, &nextAttempt, &repository, &version); err != nil {
		t.Fatalf("read admission intent: %v", err)
	}
	return status, revision, attempts, lastError, nextAttempt
}

func TestTrustSourceRegistrationPublishesTheArtifactRepository(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	seedPublishedArtifact(t, db, "build_job_1", "registry.example.test/opl")
	control := &fakeTrustSource{source: contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1}}

	if err := registerPendingTrustSources(ctx, db, control); err != nil {
		t.Fatalf("publish admission: %v", err)
	}
	if len(control.registered) != 1 {
		t.Fatalf("published %d registrations, want 1", len(control.registered))
	}
	request := control.registered[0]
	if request.ReleaseRepository != "registry.example.test/opl" {
		t.Fatalf("registered repository = %q, want the repository the artifact was published to", request.ReleaseRepository)
	}
	if request.ReleaseVersion != "build_job_1" {
		t.Fatalf("registered version = %q, want the build job id", request.ReleaseVersion)
	}
	if request.ExpectedRevision != 1 {
		t.Fatalf("expected revision = %d, want the revision the producer read", request.ExpectedRevision)
	}
	if !strings.Contains(request.Reason, "build_job_1") || !strings.Contains(request.Reason, "pv_1") {
		t.Fatalf("reason = %q, want it to name the publication", request.Reason)
	}
	status, revision, attempts, lastError, _ := registrationRow(t, db, "build_job_1")
	if status != "registered" || !revision.Valid || revision.Int64 != 2 || attempts != 1 || lastError.Valid {
		t.Fatalf("admission intent = status %q revision %v attempts %d error %v", status, revision, attempts, lastError)
	}
}

func TestTrustSourceRegistrationDoesNotPublishTheSameRepositoryTwice(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	seedPublishedArtifact(t, db, "build_job_1", "registry.example.test/opl")
	control := &fakeTrustSource{source: contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1, Repositories: []string{"registry.example.test/opl"}}}

	if err := registerPendingTrustSources(ctx, db, control); err != nil {
		t.Fatalf("publish admission: %v", err)
	}
	if len(control.registered) != 0 {
		t.Fatalf("published %d registrations for a repository that is already admitted, want 0", len(control.registered))
	}
	status, revision, _, _, _ := registrationRow(t, db, "build_job_1")
	if status != "registered" {
		t.Fatalf("admission intent status = %q, want it closed", status)
	}
	if !revision.Valid || revision.Int64 != 1 {
		t.Fatalf("admission intent revision = %v, want the revision the trust source already had", revision)
	}
}

func TestTrustSourceRegistrationKeepsTheOpenItemWhenTheControlPlaneRefuses(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	seedPublishedArtifact(t, db, "build_job_1", "registry.example.test/opl")
	control := &fakeTrustSource{failure: &trustSourceFailure{code: trustSourceUnauthorized, status: 401}}

	if err := registerPendingTrustSources(ctx, db, control); err != nil {
		t.Fatalf("publish admission: %v", err)
	}
	status, revision, attempts, lastError, nextAttempt := registrationRow(t, db, "build_job_1")
	if status != "pending" {
		t.Fatalf("admission intent status = %q, want it open", status)
	}
	if revision.Valid {
		t.Fatalf("admission intent revision = %v, want none while the admission is open", revision)
	}
	if attempts != 1 || !lastError.Valid || lastError.String != trustSourceUnauthorized {
		t.Fatalf("admission intent = attempts %d error %v", attempts, lastError)
	}
	if !nextAttempt.After(time.Now().Add(-time.Second)) {
		t.Fatalf("next attempt at %s, want the retry to wait", nextAttempt)
	}
	var jobError sql.NullString
	if err := db.QueryRow(`SELECT error_code FROM build.build_jobs WHERE id=$1`, "build_job_1").Scan(&jobError); err != nil {
		t.Fatal(err)
	}
	if !jobError.Valid || jobError.String != trustSourceRegistrationFailed {
		t.Fatalf("build job error = %v, want the open item to be named", jobError)
	}
	var logs int
	if err := db.QueryRow(`SELECT count(*) FROM build.build_logs WHERE build_job_id=$1 AND level='error'`, "build_job_1").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Fatalf("build log error rows = %d, want the refusal to be logged once", logs)
	}
}

func TestTrustSourceRegistrationRetryLandsOnTheIntent(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	seedPublishedArtifact(t, db, "build_job_1", "registry.example.test/opl")
	refused := &fakeTrustSource{failure: &trustSourceFailure{code: trustSourceUnavailable}}
	if err := registerPendingTrustSources(ctx, db, refused); err != nil {
		t.Fatalf("publish admission: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE build.build_trust_source_registrations SET next_attempt_at=now() WHERE build_job_id=$1`, "build_job_1"); err != nil {
		t.Fatal(err)
	}
	control := &fakeTrustSource{source: contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1}}
	if err := registerPendingTrustSources(ctx, db, control); err != nil {
		t.Fatalf("retry admission: %v", err)
	}
	status, revision, attempts, lastError, _ := registrationRow(t, db, "build_job_1")
	if status != "registered" || !revision.Valid || revision.Int64 != 2 {
		t.Fatalf("admission intent = status %q revision %v", status, revision)
	}
	if attempts != 2 || lastError.Valid {
		t.Fatalf("admission intent = attempts %d error %v", attempts, lastError)
	}
	var jobError sql.NullString
	if err := db.QueryRow(`SELECT error_code FROM build.build_jobs WHERE id=$1`, "build_job_1").Scan(&jobError); err != nil {
		t.Fatal(err)
	}
	if jobError.Valid {
		t.Fatalf("build job error = %v, want the open item cleared", jobError)
	}
}

func TestTrustSourceRegistrationHoldsOneIntentPerBuildJob(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	seedPublishedArtifact(t, db, "build_job_1", "registry.example.test/opl")
	_, err := db.Exec(`INSERT INTO build.build_trust_source_registrations(id,build_job_id,release_repository,release_version) VALUES('tstr_another','build_job_1','registry.example.test/opl','build_job_1')`)
	if err == nil {
		t.Fatal("a second admission intent for one build job must be refused")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM build.build_trust_source_registrations WHERE build_job_id=$1`, "build_job_1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("admission intents = %d, want exactly one per build job", count)
	}
}

func TestTrustSourceRegistrationDoesNotRegisterAFailedBuild(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO build.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,error_code,request_id,accepted_input) VALUES('op_build_job_failed','tenant','actor_1','build','build_job_failed','failed','failed','build_failed','request_1','{}'::jsonb)`); err != nil {
		t.Fatalf("insert failed build operation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO build.build_jobs(id,tenant_id,package_version_id,runtime_version_id,webui_version_id,input_digest,input_snapshot,status,stage,error_code,request_id,created_by,operation_id,catalog_policy_id) VALUES('build_job_failed','tenant','pv_1','runtime_1','webui_1',$1,'{}'::jsonb,'failed','failed','build_failed','request_1','actor_1','op_build_job_failed','policy_1')`, digest); err != nil {
		t.Fatalf("insert failed build job: %v", err)
	}
	control := &fakeTrustSource{source: contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1}}
	if err := registerPendingTrustSources(ctx, db, control); err != nil {
		t.Fatalf("publish admission: %v", err)
	}
	if len(control.registered) != 0 {
		t.Fatalf("published %d registrations for a failed build, want 0", len(control.registered))
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM build.build_trust_source_registrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("admission intents = %d, want none for a failed build", count)
	}
}

// TestTrustSourceRegistrationIsRecordedWithTheConfirmedArtifact proves the
// trigger point itself: the owner writes the admission intent in the same
// transaction that records the artifact, so a confirmed artifact always carries
// one intent and a failed build never carries one.
func TestTrustSourceRegistrationIsRecordedWithTheConfirmedArtifact(t *testing.T) {
	db := buildTrustSourceDatabase(t)
	ctx := context.Background()
	digestValue := "sha256:" + strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO build.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input) VALUES('op_build_job_1','tenant','actor_1','build','build_job_1','running','building','request_1','{}'::jsonb)`); err != nil {
		t.Fatalf("insert build operation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO build.build_jobs(id,tenant_id,package_version_id,runtime_version_id,webui_version_id,input_digest,input_snapshot,status,stage,request_id,created_by,operation_id,catalog_policy_id) VALUES('build_job_1','tenant','pv_1','runtime_1','webui_1',$1,'{}'::jsonb,'building','building','request_1','actor_1','op_build_job_1','policy_1')`, digestValue); err != nil {
		t.Fatalf("insert build job: %v", err)
	}
	store, err := ownerstore.New(db, "build")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{store: store}
	runtimeContract, webuiContract := gapContracts(t)
	input := gapSnapshot(t, runtimeContract, webuiContract)
	confirmed := &record{
		Job:    &api.BuildJob{Id: "build_job_1", OperationId: "op_build_job_1", PackageVersionId: "pv_1", RuntimeVersionId: "runtime_1", WebuiVersionId: "webui_1"},
		Input:  input,
		Call:   &api.CallContext{RequestId: "request_1"},
		Tenant: "tenant",
		Actor:  "actor_1",
	}
	if err := service.confirm(ctx, confirmed, "registry.example.test/opl", Manifest{Digest: digestValue, Size: 1024}); err != nil {
		t.Fatalf("confirm build artifact: %v", err)
	}
	var artifacts int
	if err := db.QueryRow(`SELECT count(*) FROM build.build_artifacts WHERE build_job_id=$1`, "build_job_1").Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 {
		t.Fatalf("build artifacts = %d, want the confirmed artifact", artifacts)
	}
	status, revision, attempts, lastError, _ := registrationRow(t, db, "build_job_1")
	if status != "pending" || revision.Valid || attempts != 0 || lastError.Valid {
		t.Fatalf("admission intent = status %q revision %v attempts %d error %v", status, revision, attempts, lastError)
	}
	var repository, version string
	if err := db.QueryRow(`SELECT release_repository,release_version FROM build.build_trust_source_registrations WHERE build_job_id=$1`, "build_job_1").Scan(&repository, &version); err != nil {
		t.Fatal(err)
	}
	if repository != "registry.example.test/opl" {
		t.Fatalf("registered repository = %q, want the repository the artifact was published to", repository)
	}
	if version != "build_job_1" {
		t.Fatalf("registered version = %q, want the build job id", version)
	}
}
