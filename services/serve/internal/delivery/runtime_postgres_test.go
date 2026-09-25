package delivery_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/serve/internal/delivery"
)

// deployCommand builds the typed command Serve's own Deploy path would receive.
// Only the fields the runtime-instance row requires are set; the deployment row
// itself must already exist in Serve's own store.
func deployCommand(t *testing.T, workspace, deploymentID string, epoch int64) *api.RuntimeDeployCommand {
	t.Helper()
	descriptor := &api.DeploymentDescriptor{
		SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
		Artifact:      &api.ArtifactReference{Repository: "registry.test/app", Digest: artifactDigest},
		Provenance:    api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD,
		ApplicationRevision: &api.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationId: "knowledge-app", Version: "1", Platform: "linux/amd64",
			Image:          "registry.test/app@" + artifactDigest,
			ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION,
		},
	}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return &api.RuntimeDeployCommand{
		WorkspaceId: workspace, DeploymentId: deploymentID, RuntimeInstanceId: "rt_" + deploymentID,
		CapabilityVersionId: "cv_1", ResourceSetId: "rs_" + deploymentID, DataAttachmentId: "att_" + deploymentID,
		DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: "sha256:" + hex.EncodeToString(sum[:]),
		DeploymentDescriptorObjectRef: "serve-descriptor://" + deploymentID,
		ExecutionEpoch:                epoch,
	}
}

// applicationEntry is the provider-published entry shape for an application that
// publishes its own endpoint.
func applicationEntry() *contracts.WorkspaceApplicationEntry {
	return &contracts.WorkspaceApplicationEntry{URL: "https://ws.example/app"}
}

func runtimeReady(entry *contracts.WorkspaceApplicationEntry, url, readiness string) delivery.RuntimeObservation {
	return delivery.RuntimeObservation{
		State:                api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY,
		ApplicationEntry:     entry,
		AccessURL:            url,
		ReadinessEvidenceRef: readiness,
		ObservedAt:           time.Now().UTC(),
	}
}

func refusedReason(t *testing.T, err error) string {
	t.Helper()
	var refused delivery.ErrRuntimeObservationRefused
	if !errors.As(err, &refused) {
		t.Fatalf("expected a typed refusal, got %v", err)
	}
	return refused.Reason
}

func countInstances(t *testing.T, db *sql.DB, deploymentID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM serve.agent_runtime_instances WHERE deployment_id=$1`, deploymentID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestServeRecordsRuntimeObservationFails closed proves Serve records a real
// runtime observation and refuses every observation that would overstate it.
func TestServeRecordsRuntimeObservation(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seedObservationDeployment(t, db, "ws-rt", tenant, "dep-rt", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	// A genuinely ready application with its own publishable entry is recorded.
	record, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-rt", "dep-rt", 1), runtimeReady(applicationEntry(), "https://ws-rt.example/app", "readiness://dep-rt"))
	if err != nil {
		t.Fatalf("recording a ready observation: %v", err)
	}
	if record.Status != "ready" || !record.ApplicationOpen || record.AccessURL != "https://ws-rt.example/app" {
		t.Fatalf("ready record = %+v", record)
	}
	var status, url, readiness string
	if err := db.QueryRowContext(ctx, `SELECT status, access_url, readiness_evidence_ref FROM serve.agent_runtime_instances WHERE deployment_id='dep-rt'`).Scan(&status, &url, &readiness); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || url != "https://ws-rt.example/app" || readiness != "readiness://dep-rt" {
		t.Fatalf("persisted instance = %q %q %q", status, url, readiness)
	}

	// Same-epoch replay converges on one instance rather than creating a second.
	if _, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-rt", "dep-rt", 1), runtimeReady(applicationEntry(), "https://ws-rt.example/app", "readiness://dep-rt")); err != nil {
		t.Fatal(err)
	}
	if n := countInstances(t, db, "dep-rt"); n != 1 {
		t.Fatalf("replay created %d instances, want 1", n)
	}
}

func TestServeRefusesOverstatedObservations(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("resource_ready_is_not_application_ready", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-a", tenant, "dep-a", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		// The runtime reports READY, but nothing publishes an entry: a provisioned
		// resource is not a reachable application.
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-a", "dep-a", 1), delivery.RuntimeObservation{
			State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY, ObservedAt: time.Now().UTC(),
		})
		if reason := refusedReason(t, err); reason != delivery.ReasonAppAccessUnavailable {
			t.Fatalf("reason = %q", reason)
		}
		if n := countInstances(t, db, "dep-a"); n != 1 {
			t.Fatalf("refused observation wrote %d instances", n)
		}
	})

	t.Run("ready_without_readiness_evidence", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-b", tenant, "dep-b", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-b", "dep-b", 1), runtimeReady(applicationEntry(), "https://ws-b.example/app", ""))
		if reason := refusedReason(t, err); reason != delivery.ReasonAppAccessUnavailable {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("ready_without_publishable_url", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-c", tenant, "dep-c", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_CLOUD_PRIVATE)
		// A cloud_private Agent has no anonymous/public URL to publish.
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-c", "dep-c", 1), runtimeReady(nil, "", "readiness://dep-c"))
		if reason := refusedReason(t, err); reason != delivery.ReasonAppAccessUnavailable {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("undecidable_state_is_refused", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-d", tenant, "dep-d", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-d", "dep-d", 1), delivery.RuntimeObservation{ObservedAt: time.Now().UTC()})
		if reason := refusedReason(t, err); reason != delivery.ReasonUnknownState {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("invalid_descriptor_is_refused", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-e", tenant, "dep-e", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		cmd := deployCommand(t, "ws-e", "dep-e", 1)
		cmd.DeploymentDescriptorDigest = "sha256:" + strings.Repeat("9", 64)
		_, err := service.RecordDeploymentObservation(ctx, cmd, runtimeReady(applicationEntry(), "https://ws-e.example/app", "readiness://dep-e"))
		if reason := refusedReason(t, err); reason != delivery.ReasonInvalidDescriptor {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("foreign_workspace_is_refused", func(t *testing.T) {
		seedObservationDeployment(t, db, "ws-f", tenant, "dep-f", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-other", "dep-f", 1), runtimeReady(applicationEntry(), "https://ws-f.example/app", "readiness://dep-f"))
		if reason := refusedReason(t, err); reason != delivery.ReasonIdentityMismatch {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("unknown_deployment_is_not_found", func(t *testing.T) {
		_, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-a", "dep-missing", 1), runtimeReady(applicationEntry(), "https://x.example/app", "readiness://x"))
		if status.Code(err) != codes.NotFound {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestServeRuntimeObservationFencesStaleEpochs proves a superseded execution epoch
// can neither create nor overwrite Serve's instance record.
func TestServeRuntimeObservationFencesStaleEpochs(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seedObservationDeployment(t, db, "ws-g", tenant, "dep-g", "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	// The deployment advances to epoch 3 and records its ready observation.
	if _, err := db.ExecContext(ctx, `UPDATE serve.agent_deployments SET execution_epoch=3 WHERE id='dep-g'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET execution_epoch=3 WHERE deployment_id='dep-g'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-g", "dep-g", 3), runtimeReady(applicationEntry(), "https://ws-g.example/app", "readiness://dep-g")); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET execution_epoch=3 WHERE deployment_id='dep-g'`); err != nil {
		t.Fatal(err)
	}
	// An observation from the superseded epoch 2 must be refused and must not
	// overwrite the epoch-3 fact.
	_, err = service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-g", "dep-g", 2), delivery.RuntimeObservation{
		State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED, ObservedAt: time.Now().UTC(),
	})
	if reason := refusedReason(t, err); reason != delivery.ReasonStaleEpoch {
		t.Fatalf("reason = %q", reason)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM serve.agent_runtime_instances WHERE deployment_id='dep-g'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("a stale epoch overwrote the record: %q", status)
	}
}

// TestServeRuntimeStatusVocabulary proves every non-ready provider state maps to
// Serve's own vocabulary without ever claiming an open application.
func TestServeRuntimeStatusVocabulary(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, tc := range []struct {
		state  api.AgentRuntimeObservationState
		status string
	}{
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING, "pending"},
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING, "starting"},
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STOPPED, "stopped"},
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED, "failed"},
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATING, "terminating"},
		{api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED, "terminated"},
	} {
		deploymentID := "dep-v" + string(rune('a'+i))
		seedObservationDeployment(t, db, "ws-v", tenant, deploymentID, "deploying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
		record, err := service.RecordDeploymentObservation(ctx, deployCommand(t, "ws-v", deploymentID, 1), delivery.RuntimeObservation{State: tc.state, ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatalf("%s: %v", tc.status, err)
		}
		if record.Status != tc.status || record.ApplicationOpen || record.AccessURL != "" {
			t.Fatalf("%s mapped to %+v", tc.status, record)
		}
	}
}

func seedObservationDeployment(t *testing.T, db *sql.DB, workspace, tenant, deploymentID, deploymentStatus, runtimeStatus, accessURL string, exposure api.WorkspaceApplicationRevisionExposurePolicyEnum) {
	t.Helper()
	seedDeployment(t, db, workspace, tenant, deploymentID, deploymentStatus, "", "", exposure)
	cmd := deployCommand(t, workspace, deploymentID, 1)
	descriptor, _ := publicjson.Marshal(cmd.DeploymentDescriptor)
	attachment, _ := json.Marshal(map[string]string{"attachmentId": cmd.DataAttachmentId})
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_deployments SET runtime_instance_id=$2 WHERE id=$1`, deploymentID, cmd.RuntimeInstanceId); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO serve.agent_runtime_instances(id,workspace_id,deployment_id,artifact_digest,fabric_resource_set_id,status,data_attachment_contract,execution_epoch,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,$5,'pending',$6,1,$7,$8,$9)`, cmd.RuntimeInstanceId, workspace, deploymentID, artifactDigest, cmd.ResourceSetId, attachment, descriptor, cmd.DeploymentDescriptorDigest, cmd.DeploymentDescriptorObjectRef); err != nil {
		t.Fatal(err)
	}
}
