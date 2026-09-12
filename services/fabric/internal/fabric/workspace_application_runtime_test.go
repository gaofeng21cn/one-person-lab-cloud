package fabric

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func applicationRevisionForTest() contracts.WorkspaceApplicationRevision {
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image:            "repo.example/apps/knowledge@sha256:" + strings.Repeat("a", 64),
		Ports:            []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		Resources:        contracts.WorkspaceApplicationResources{CPU: 2, MemoryGB: 4},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		Dependencies: []contracts.WorkspaceApplicationDependency{
			{Name: "retrieval", Image: "repo.example/apps/retrieval@sha256:" + strings.Repeat("b", 64)},
		},
		ExposurePolicy: "application",
	}
}

func applicationObservationForTest(revision contracts.WorkspaceApplicationRevision, state string) contracts.WorkspaceApplicationRuntimeObservation {
	components := contracts.WorkspaceApplicationRuntimeComponents(revision)
	for index := range components {
		components[index].State = state
	}
	return contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: "workspace-alpha", Status: state, Components: components,
	}
}

type recordingApplicationRuntimeProvider struct {
	testProvider
	ensureCalls atomic.Int64
	readCalls   atomic.Int64
	observation atomic.Pointer[contracts.WorkspaceApplicationRuntimeObservation]
	ensureErr   atomic.Pointer[error]
	readErr     atomic.Pointer[error]
	lastInput   atomic.Pointer[WorkspaceApplicationRuntimeInput]
}

func (p *recordingApplicationRuntimeProvider) EnsureWorkspaceApplicationRuntime(_ context.Context, input WorkspaceApplicationRuntimeInput, _ ComputeAllocation, _ StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	p.ensureCalls.Add(1)
	p.lastInput.Store(&input)
	if err := p.ensureErr.Load(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, *err
	}
	if observation := p.observation.Load(); observation != nil {
		return *observation, nil
	}
	return applicationObservationForTest(input.Revision, "ready"), nil
}

func (p *recordingApplicationRuntimeProvider) ReadWorkspaceApplicationRuntime(_ context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	p.readCalls.Add(1)
	if err := p.readErr.Load(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, *err
	}
	if observation := p.observation.Load(); observation != nil {
		return *observation, nil
	}
	return applicationObservationForTest(input.Revision, "ready"), nil
}

func applicationRuntimeInput(key string, revision contracts.WorkspaceApplicationRevision) WorkspaceApplicationRuntimeInput {
	return WorkspaceApplicationRuntimeInput{
		WorkspaceID: "workspace-alpha", ComputeID: "compute-alpha", VolumeID: "storage-alpha",
		AttachmentID: "attachment-alpha", AttachmentOperationID: "workspace-launch-alpha:attachment",
		RuntimeOperationID: key, Revision: revision, ConfigurationDigest: strings.Repeat("c", 64), IdempotencyKey: key,
	}
}

func TestCreateWorkspaceApplicationRuntimeHappyPathAndReplay(t *testing.T) {
	revision := applicationRevisionForTest()
	provider := &recordingApplicationRuntimeProvider{}
	service := runtimeTestService(provider, NewMemoryOperationStore())
	input := applicationRuntimeInput("app-runtime-once", revision)

	first, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil {
		t.Fatalf("create application runtime: %v", err)
	}
	if len(first.Components) != 2 || first.Components[0].Name != "main" || first.Components[1].Name != "retrieval" ||
		first.Components[0].State != "ready" || first.RuntimeID == "" {
		t.Fatalf("first observation=%#v", first)
	}
	if provider.ensureCalls.Load() != 1 || provider.lastInput.Load().Revision.ApplicationID != "knowledge-app" {
		t.Fatalf("provider ensure calls=%d", provider.ensureCalls.Load())
	}

	replayed, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || replayed.RuntimeID != first.RuntimeID || provider.ensureCalls.Load() != 1 {
		t.Fatalf("replay observation=%#v err=%v ensureCalls=%d", replayed, err, provider.ensureCalls.Load())
	}

	changed := input
	changed.ConfigurationDigest = strings.Repeat("d", 64)
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), changed); !errors.Is(err, ErrRuntimeIdempotencyConflict) {
		t.Fatalf("changed replay error=%v, want ErrRuntimeIdempotencyConflict", err)
	}
}

func TestCreateWorkspaceApplicationRuntimeRejectsUnsupportedProviderAndInvalidObservation(t *testing.T) {
	revision := applicationRevisionForTest()
	plain := runtimeTestService(&testProvider{}, NewMemoryOperationStore())
	if _, err := plain.CreateWorkspaceApplicationRuntime(context.Background(), applicationRuntimeInput("app-runtime-plain", revision)); !errors.Is(err, ErrWorkspaceApplicationRuntimeProviderUnsupported) {
		t.Fatalf("unsupported provider error=%v, want %v", err, ErrWorkspaceApplicationRuntimeProviderUnsupported)
	}

	provider := &recordingApplicationRuntimeProvider{}
	service := runtimeTestService(provider, NewMemoryOperationStore())
	broken := applicationObservationForTest(revision, "ready")
	broken.Components = broken.Components[:1]
	observation := broken
	provider.observation.Store(&observation)
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), applicationRuntimeInput("app-runtime-broken", revision)); err == nil {
		t.Fatal("a partial observation must fail the creation")
	}
	if provider.ensureCalls.Load() != 1 {
		t.Fatalf("broken ensure calls=%d", provider.ensureCalls.Load())
	}

	provider.readCalls.Store(0)
	valid := applicationObservationForTest(revision, "ready")
	provider.observation.Store(&valid)
	recovered, err := service.CreateWorkspaceApplicationRuntime(context.Background(), applicationRuntimeInput("app-runtime-broken", revision))
	if err != nil || len(recovered.Components) != 2 {
		t.Fatalf("failed-claim recovery observation=%#v err=%v", recovered, err)
	}
	if provider.readCalls.Load() != 1 {
		t.Fatalf("failed claim must converge by readback, readCalls=%d", provider.readCalls.Load())
	}
}

func TestCreateWorkspaceApplicationRuntimeValidatesInputsBeforeClaim(t *testing.T) {
	revision := applicationRevisionForTest()
	provider := &recordingApplicationRuntimeProvider{}
	service := runtimeTestService(provider, NewMemoryOperationStore())

	unattached := applicationRuntimeInput("app-runtime-attach", revision)
	unattached.AttachmentOperationID = "wrong-operation"
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), unattached); err == nil {
		t.Fatal("an attachment identity mismatch must be rejected before the claim")
	}
	invalidRevision := applicationRuntimeInput("app-runtime-revision", revision)
	invalidRevision.Revision.ExposurePolicy = "public"
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), invalidRevision); err == nil {
		t.Fatal("an invalid revision must be rejected before the claim")
	}
	noDigest := applicationRuntimeInput("app-runtime-digest", revision)
	noDigest.ConfigurationDigest = ""
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), noDigest); err == nil {
		t.Fatal("a missing configuration digest must be rejected")
	}
	if provider.ensureCalls.Load() != 0 {
		t.Fatalf("provider must not be called for rejected inputs, calls=%d", provider.ensureCalls.Load())
	}
}

func TestWorkspaceApplicationRuntimeReadbackConvergesWithLiveProvider(t *testing.T) {
	revision := applicationRevisionForTest()
	provider := &recordingApplicationRuntimeProvider{}
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	input := applicationRuntimeInput("app-runtime-readback", revision)

	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
		t.Fatalf("create: %v", err)
	}
	pending := applicationObservationForTest(revision, "pending")
	provider.observation.Store(&pending)
	live, err := service.WorkspaceApplicationRuntimeReadback(context.Background(), input)
	if err != nil || live.Status != "pending" {
		t.Fatalf("live readback observation=%#v err=%v", live, err)
	}
	if provider.readCalls.Load() != 1 {
		t.Fatalf("readCalls=%d, want the live provider read", provider.readCalls.Load())
	}
	// The succeeded record keeps the creation-time ready observation; the
	// pending live read is returned to the caller without rewriting it.
	storedOperations, listErr := store.List(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, operation := range storedOperations {
		if operation.Action != "create_workspace_application_runtime" {
			continue
		}
		var record workspaceApplicationRuntimeRecord
		if !decodeOperationResource(operation, &record) || len(record.Observation.Components) != 2 || record.Observation.Components[0].State != "ready" {
			t.Fatalf("succeeded record was rewritten by the readback: %#v", record.Observation)
		}
	}
}
