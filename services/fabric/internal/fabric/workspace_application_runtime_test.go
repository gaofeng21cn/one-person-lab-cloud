package fabric

import (
	"context"
	"errors"
	"fmt"
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
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		ScratchMounts:    []contracts.WorkspaceApplicationMount{{Name: "tmp", MountPath: "/tmp"}},
		HealthChecks:     []contracts.WorkspaceApplicationHealthCheck{{Port: 8080, Path: "/healthz", InitialDelaySeconds: 5}},
		Dependencies: []contracts.WorkspaceApplicationDependency{
			{Name: "retrieval", Image: "repo.example/apps/retrieval@sha256:" + strings.Repeat("b", 64)},
		},
		ExposurePolicy: "application", EntryPort: "http",
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
		if observation := p.observation.Load(); observation != nil {
			return *observation, *err
		}
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
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, "data-knowledge")
	if err != nil {
		panic(err)
	}
	return WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, DataBindingID: "data-knowledge",
		AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", ComputeID: "compute-alpha", VolumeID: "storage-alpha",
		AttachmentID: "attachment-alpha", AttachmentOperationID: "workspace-launch-alpha:attachment",
		RuntimeOperationID: key, Revision: revision, ConfigurationDigest: digest, IdempotencyKey: key,
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
	changed.Configuration = contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"MODE": "changed"}}
	changed.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(changed.Configuration, changed.SecretBindings, changed.DataBindingID)
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
	for _, accountID := range []string{"", "another-account"} {
		wrongAccount := applicationRuntimeInput("app-runtime-account", revision)
		wrongAccount.AccountID = accountID
		if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), wrongAccount); !errors.Is(err, ErrWorkspaceApplicationRuntimeInputInvalid) {
			t.Fatalf("account %q error=%v, want classified input rejection", accountID, err)
		}
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

func TestWorkspaceApplicationRuntimePendingConvergesOnlyAfterReady(t *testing.T) {
	for _, providerPendingError := range []bool{false, true} {
		t.Run(fmt.Sprintf("pendingError=%t", providerPendingError), func(t *testing.T) {
			provider := &recordingApplicationRuntimeProvider{}
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			input := applicationRuntimeInput("app-async", applicationRevisionForTest())
			pending := applicationObservationForTest(input.Revision, "pending")
			provider.observation.Store(&pending)
			if providerPendingError {
				pendingErr := error(ErrWorkspaceLaunchPending)
				provider.ensureErr.Store(&pendingErr)
			}
			assertStarted := func() {
				t.Helper()
				operations, err := store.List(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				for _, operation := range operations {
					if operation.Action == "create_workspace_application_runtime" && operation.Status != "started" {
						t.Fatalf("pending operation must remain started: %#v", operation)
					}
				}
			}
			first, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if !errors.Is(err, ErrWorkspaceLaunchPending) || first.Status != "pending" {
				t.Fatalf("first=%#v err=%v", first, err)
			}
			assertStarted()
			replay, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if !errors.Is(err, ErrWorkspaceLaunchPending) || replay.Status != "pending" {
				t.Fatalf("replay=%#v err=%v", replay, err)
			}
			assertStarted()
			ready := applicationObservationForTest(input.Revision, "ready")
			provider.observation.Store(&ready)
			final, err := service.WorkspaceApplicationRuntimeReadback(context.Background(), input)
			if err != nil || final.Status != "ready" {
				t.Fatalf("final=%#v err=%v", final, err)
			}
			if provider.ensureCalls.Load() != 1 || provider.readCalls.Load() != 2 {
				t.Fatalf("ensure=%d read=%d", provider.ensureCalls.Load(), provider.readCalls.Load())
			}
		})
	}
}

func TestWorkspaceApplicationRuntimeReadFailuresNeverReturnHistoricalReady(t *testing.T) {
	for _, mode := range []string{"unavailable", "invalid", "wrong-workspace", "wrong-runtime", "inconsistent-status"} {
		t.Run(mode, func(t *testing.T) {
			provider := &recordingApplicationRuntimeProvider{}
			service := runtimeTestService(provider, NewMemoryOperationStore())
			input := applicationRuntimeInput("app-live-errors", applicationRevisionForTest())
			if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			broken := applicationObservationForTest(input.Revision, "ready")
			switch mode {
			case "unavailable":
				readErr := errors.New("docker daemon unavailable")
				provider.readErr.Store(&readErr)
			case "invalid":
				broken.Components = nil
			case "wrong-workspace":
				broken.WorkspaceID = "another-workspace"
			case "wrong-runtime":
				broken.RuntimeID = "another-runtime"
			case "inconsistent-status":
				broken.Components[0].State = "pending"
			}
			provider.observation.Store(&broken)
			for _, read := range []func(context.Context, WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error){service.WorkspaceApplicationRuntimeReadback, service.CreateWorkspaceApplicationRuntime} {
				observation, err := read(context.Background(), input)
				if err == nil || observation.Status == "ready" {
					t.Fatalf("observation=%#v err=%v", observation, err)
				}
			}
		})
	}
}

func TestWorkspaceApplicationRuntimeFailedReadbackCannotConvergeSuccess(t *testing.T) {
	for _, state := range []string{"failed", "absent"} {
		t.Run(state, func(t *testing.T) {
			provider := &recordingApplicationRuntimeProvider{}
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			input := applicationRuntimeInput("app-pending-fails", applicationRevisionForTest())
			pending := applicationObservationForTest(input.Revision, "pending")
			provider.observation.Store(&pending)
			if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); !errors.Is(err, ErrWorkspaceLaunchPending) {
				t.Fatal(err)
			}
			failed := applicationObservationForTest(input.Revision, state)
			provider.observation.Store(&failed)
			observation, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if (state == "failed" && err != nil) || (state == "absent" && !errors.Is(err, ErrRuntimeOperationFailed)) || observation.Status != state {
				t.Fatalf("observation=%#v err=%v", observation, err)
			}
			operations, _ := store.List(context.Background())
			for _, operation := range operations {
				if operation.Action == "create_workspace_application_runtime" && operation.Status == "succeeded" {
					t.Fatal("unready readback marked succeeded")
				}
			}
		})
	}
}
