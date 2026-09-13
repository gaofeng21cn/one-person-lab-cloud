package fabric

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func applicationLifecycleInput(input WorkspaceApplicationRuntimeInput, state, key string) WorkspaceApplicationRuntimeLifecycleInput {
	return WorkspaceApplicationRuntimeLifecycleInput{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, RuntimeID: workspaceApplicationRuntimeID(input.RuntimeOperationID), RuntimeOperationID: input.RuntimeOperationID, DesiredState: state, IdempotencyKey: key}
}
func TestWorkspaceApplicationLifecycleFencesUncreatedGeneration(t *testing.T) {
	for _, state := range []string{"absent", "suspended"} {
		t.Run(state, func(t *testing.T) {
			provider := &recordingApplicationRuntimeProvider{}
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			input := applicationRuntimeInput("late-create", applicationRevisionForTest())
			result, err := service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), applicationLifecycleInput(input, state, "cancel-first"))
			if err != nil || result.State != "absent" {
				t.Fatalf("fence=%#v err=%v", result, err)
			}
			service = runtimeTestService(provider, store)
			if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err == nil || provider.ensureCalls.Load() != 0 {
				t.Fatalf("late creation escaped fence: err=%v calls=%d", err, provider.ensureCalls.Load())
			}
		})
	}
}

func TestWorkspaceApplicationLifecycleReplacementKeepsNewGenerationAndData(t *testing.T) {
	ctx := context.Background()
	provider, runner, paths := applicationRuntimeProviderFixture(t, "workspace-alpha")
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID = ""
	volume.SizeGB = 10
	service.volumes["storage-alpha"] = volume
	old := applicationRuntimeInput("old-generation", applicationRevisionForTest())
	old.Configuration.Environment = map[string]string{"APP_MODE": "old"}
	old.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(old.Configuration, old.SecretBindings, old.DataBindingID)
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, old); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(paths.Data, contracts.WorkspaceApplicationDataDirectory(old.DataBindingID), "data", "preserve.txt")
	if err := os.WriteFile(dataFile, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	next := applicationRuntimeInput("new-generation", applicationRevisionForTest())
	next.Revision.ApplicationID = "another-app"
	next.Revision.Image = "repo.example/new@sha256:" + strings.Repeat("d", 64)
	next.DataBindingID = "new-application-data"
	next.Configuration.Environment = map[string]string{"APP_MODE": "new"}
	next.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(next.Configuration, next.SecretBindings, next.DataBindingID)
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, next); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"suspended", "running", "absent"} {
		result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(old, state, "old-"+state))
		if err != nil || result.State != state {
			t.Fatalf("%s result=%#v err=%v", state, result, err)
		}
		// A successful same-key replay only reads current state; it must not
		// rewrite the immutable succeeded journal or repeat image retirement.
		replayed, replayErr := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(old, state, "old-"+state))
		if replayErr != nil || replayed.State != state {
			t.Fatalf("replay %s result=%#v err=%v", state, replayed, replayErr)
		}
		current, err := service.WorkspaceApplicationRuntimeReadback(ctx, next)
		if err != nil || current.Status != "ready" {
			t.Fatalf("new generation changed by %s: %#v %v", state, current, err)
		}
	}
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("old data removed: %v", err)
	}
	newData := filepath.Join(paths.Data, contracts.WorkspaceApplicationDataDirectory(next.DataBindingID), "data", "preserve.txt")
	if _, err := os.Stat(newData); !os.IsNotExist(err) {
		t.Fatalf("old data leaked to new application: %v", err)
	}
	if len(runner.removedImages) != 1 || runner.removedImages[0] != old.Revision.Image {
		t.Fatalf("removed images=%v", runner.removedImages)
	}
	service = runtimeTestService(provider, store)
	if _, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(old, "running", "revive")); err == nil {
		t.Fatal("retired generation revived after service restart")
	}
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, old); err == nil {
		t.Fatal("old ensure replay revived retired generation")
	}
}

func TestWorkspaceApplicationConfigurationRejectsDigestDriftBeforeMutation(t *testing.T) {
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	service := runtimeTestService(provider, NewMemoryOperationStore())
	input := applicationRuntimeInput("config-drift", applicationRevisionForTest())
	input.Configuration.Environment = map[string]string{"MODE": "not-in-digest"}
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err == nil || runner.runCount() != 0 {
		t.Fatalf("configuration drift mutated provider: %v runs=%d", err, runner.runCount())
	}
}

func TestWorkspaceApplicationHistoricalReadbackAdoptsExactOriginalInput(t *testing.T) {
	ctx := context.Background()
	provider, runner, paths := applicationRuntimeProviderFixture(t, "workspace-alpha")
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	input := applicationRuntimeInput("historical-create", applicationRevisionForTest())
	input.SchemaVersion = 0
	input.DataBindingID = ""
	input.ConfigurationDigest = strings.Repeat("c", 64)
	compute := service.computes[input.ComputeID]
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if _, err := provider.ensureWorkspaceApplicationComponent(ctx, input, compute, localDockerName("opl-compute", compute.ID), paths, component); err != nil {
			t.Fatal(err)
		}
	}
	observation, err := provider.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil || observation.Status != "ready" {
		t.Fatalf("legacy live=%#v err=%v", observation, err)
	}
	operation := newOperation("create_workspace_application_runtime", "workspace_application_runtime", contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID), input.AccountID, input.WorkspaceID, input.RuntimeOperationID, historicalApplicationRequestHash(input), service.now())
	operation.ID = "historical-operation"
	operation.Status = "succeeded"
	operation.CreatedAt = service.now()
	operation.RedactedProviderPayload = map[string]any{"resource": map[string]any{"runtimeId": observation.RuntimeID, "workspaceId": input.WorkspaceID, "observation": observation}}
	if _, _, err := service.runtimeOperations.ClaimRuntime(ctx, operation); err != nil {
		t.Fatal(err)
	}
	forged := input
	forged.ComputeID = "another-compute"
	if _, err := service.WorkspaceApplicationRuntimeReadback(ctx, forged); err == nil {
		t.Fatal("historical hash mismatch adopted")
	}
	readback, err := service.WorkspaceApplicationRuntimeReadback(ctx, input)
	if err != nil || readback.RuntimeID != contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID) || runner.runCount() != 2 {
		t.Fatalf("adopt=%#v err=%v", readback, err)
	}
	lifecycle := applicationLifecycleInput(input, "absent", "retire-historical")
	lifecycle.RuntimeID = readback.RuntimeID
	lifecycle.HistoricalApplicationRuntime = true
	if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle); err != nil || result.State != "absent" {
		t.Fatalf("legacy retire=%#v err=%v", result, err)
	}
	if readback, err := service.WorkspaceApplicationRuntimeReadback(ctx, input); err != nil || readback.Status != "absent" {
		t.Fatalf("retired historical source cannot be re-read: state=%s err=%v", readback.Status, err)
	}
	reuse := applicationRuntimeInput("return-to-historical-app", applicationRevisionForTest())
	reuse.DataLayout = "legacy_application"
	reuse.DataSourceRuntimeOperationID = input.RuntimeOperationID
	if err := service.validateApplicationDataLayout(ctx, reuse); err != nil {
		t.Fatalf("retired historical source lost data provenance: %v", err)
	}
	newHistorical := input
	newHistorical.RuntimeOperationID = "unclaimed-old-input"
	newHistorical.IdempotencyKey = newHistorical.RuntimeOperationID
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, newHistorical); err == nil || runner.runCount() != 2 {
		t.Fatalf("unclaimed historical input recreated: %v", err)
	}
}

func TestWorkspaceApplicationHistoricalReadbackUncreatedCanBeFenced(t *testing.T) {
	for _, desired := range []string{"absent", "suspended"} {
		t.Run(desired, func(t *testing.T) {
			ctx := context.Background()
			provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			input := applicationRuntimeInput("reserved-historical", applicationRevisionForTest())
			input.SchemaVersion, input.DataBindingID = 0, ""
			input.ConfigurationDigest = strings.Repeat("c", 64)
			readback, err := service.WorkspaceApplicationRuntimeReadback(ctx, input)
			if err != nil || readback.Status != "absent" || readback.RuntimeID != applicationRuntimeID(input) || len(readback.Components) != 2 {
				t.Fatalf("uncreated readback=%#v err=%v", readback, err)
			}
			if err := contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, readback); err != nil {
				t.Fatal(err)
			}
			if operations, err := service.ListOperations(ctx); err != nil || len(operations) != 0 {
				t.Fatalf("uncreated read wrote an adoption: operations=%v err=%v", operations, err)
			}
			if _, err := service.CreateWorkspaceApplicationRuntime(ctx, input); !errors.Is(err, ErrRuntimeIdempotencyConflict) {
				t.Fatalf("unclaimed historical create accepted: %v", err)
			}
			lifecycle := applicationLifecycleInput(input, desired, "cancel-reserved-historical")
			lifecycle.RuntimeID, lifecycle.HistoricalApplicationRuntime = readback.RuntimeID, true
			if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle); err != nil || result.State != "absent" {
				t.Fatalf("uncreated fence=%#v err=%v", result, err)
			}
			service = runtimeTestService(provider, store)
			if _, found, err := service.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", lifecycle.RuntimeID); err != nil || !found {
				t.Fatalf("fence missing after restart: found=%t err=%v", found, err)
			}
			if readback, err := service.WorkspaceApplicationRuntimeReadback(ctx, input); err != nil || readback.Status != "absent" {
				t.Fatalf("fenced readback=%#v err=%v", readback, err)
			}
			if _, err := service.CreateWorkspaceApplicationRuntime(ctx, input); !errors.Is(err, ErrRuntimeIdempotencyConflict) || runner.runCount() != 0 {
				t.Fatalf("fenced historical input recreated: err=%v runs=%d", err, runner.runCount())
			}
		})
	}
}

func TestWorkspaceApplicationHistoricalReadbackRejectsOtherSharedRuntimeOwner(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		for _, status := range []string{"started", "failed", "succeeded"} {
			name := status
			if foreign {
				name += "-foreign"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				provider := &recordingApplicationRuntimeProvider{}
				store := NewMemoryOperationStore()
				service := runtimeTestService(provider, store)
				input := applicationRuntimeInput("missing-historical-key", applicationRevisionForTest())
				input.SchemaVersion, input.DataBindingID = 0, ""
				input.ConfigurationDigest = strings.Repeat("c", 64)
				absent := applicationObservationForTest(input.Revision, "absent")
				provider.observation.Store(&absent)
				owner := input
				owner.RuntimeOperationID = "other-historical-key"
				if foreign {
					owner.AccountID = "another-account"
				}
				operation := newOperation("create_workspace_application_runtime", "workspace_application_runtime", applicationRuntimeID(owner), owner.AccountID, owner.WorkspaceID, owner.RuntimeOperationID, historicalApplicationRequestHash(owner), service.now())
				operation.ID, operation.Status, operation.CreatedAt = "other-historical-owner", status, service.now()
				if _, _, err := service.runtimeOperations.ClaimRuntime(ctx, operation); err != nil {
					t.Fatal(err)
				}
				if _, err := service.WorkspaceApplicationRuntimeReadback(ctx, input); !errors.Is(err, ErrRuntimeIdempotencyConflict) || provider.readCalls.Load() != 0 {
					t.Fatalf("shared RuntimeID owner ignored: err=%v reads=%d", err, provider.readCalls.Load())
				}
			})
		}
	}
}

func TestWorkspaceApplicationHistoricalReadbackUncreatedRequiresEveryComponentAbsent(t *testing.T) {
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(applicationRevisionForTest()) {
		t.Run(component.Name, func(t *testing.T) {
			ctx := context.Background()
			provider, _, paths := applicationRuntimeProviderFixture(t, "workspace-alpha")
			service := runtimeTestService(provider, NewMemoryOperationStore())
			input := applicationRuntimeInput("missing-owner", applicationRevisionForTest())
			input.SchemaVersion, input.DataBindingID = 0, ""
			input.ConfigurationDigest = strings.Repeat("c", 64)
			compute := service.computes[input.ComputeID]
			if _, err := provider.ensureWorkspaceApplicationComponent(ctx, input, compute, localDockerName("opl-compute", compute.ID), paths, component); err != nil {
				t.Fatal(err)
			}
			if _, err := service.WorkspaceApplicationRuntimeReadback(ctx, input); !errors.Is(err, ErrRuntimeIdempotencyConflict) {
				t.Fatalf("existing %s treated as absent: %v", component.Name, err)
			}
		})
	}
}

func TestWorkspaceApplicationHistoricalReadbackUncreatedPreservesProviderFailure(t *testing.T) {
	provider := &recordingApplicationRuntimeProvider{}
	service := runtimeTestService(provider, NewMemoryOperationStore())
	input := applicationRuntimeInput("missing-owner", applicationRevisionForTest())
	input.SchemaVersion, input.DataBindingID = 0, ""
	input.ConfigurationDigest = strings.Repeat("c", 64)
	readErr := errors.New("provider_read_failed")
	provider.readErr.Store(&readErr)
	if _, err := service.WorkspaceApplicationRuntimeReadback(context.Background(), input); !errors.Is(err, readErr) {
		t.Fatalf("provider failure treated as absence: %v", err)
	}
	provider.readErr.Store(nil)
	invalid := applicationObservationForTest(input.Revision, "absent")
	invalid.Components = invalid.Components[:1]
	provider.observation.Store(&invalid)
	if _, err := service.WorkspaceApplicationRuntimeReadback(context.Background(), input); err == nil {
		t.Fatal("incomplete provider absence accepted")
	}
}

func TestWorkspaceApplicationLifecycleResumeReadsCurrentEntryAfterPending(t *testing.T) {
	ctx := context.Background()
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	service := runtimeTestService(provider, NewMemoryOperationStore())
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID, volume.SizeGB = "", 10
	service.volumes["storage-alpha"] = volume
	input := applicationRuntimeInput("resume-current-entry", applicationRevisionForTest())
	initial, err := service.CreateWorkspaceApplicationRuntime(ctx, input)
	if err != nil || initial.Status != "ready" {
		t.Fatalf("initial=%#v err=%v", initial, err)
	}
	if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(input, "suspended", "suspend-current-entry")); err != nil || result.State != "suspended" {
		t.Fatalf("suspend=%#v err=%v", result, err)
	}
	name, _ := localDockerApplicationComponentNameForInput(input, "main")
	var containers []dockerContainerInspect
	if err := json.Unmarshal(runner.containers[name], &containers); err != nil {
		t.Fatal(err)
	}
	containers[0].NetworkSettings.Ports["8080/tcp"][0].HostPort = "32080"
	runner.containers[name], runner.containers["cid-"+name] = mustJSON(containers), mustJSON(containers)
	runner.probeReady = false
	resume := applicationLifecycleInput(input, "running", "resume-current-entry")
	pending, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, resume)
	if err != nil || pending.State != "pending" || pending.Observation.Status != "pending" || pending.Observation.EntryURL != "" {
		t.Fatalf("resume health pending=%#v err=%v", pending, err)
	}
	runner.probeReady = true
	live, err := service.ReadWorkspaceApplicationRuntimeLifecycle(ctx, resume)
	if err != nil || live.State != "running" || live.Observation.Status != "ready" || live.Observation.EntryURL != "http://127.0.0.1:32080/" || live.Observation.EntryURL == initial.EntryURL {
		t.Fatalf("resume current entry=%#v err=%v", live, err)
	}
	finished, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, resume)
	if err != nil || finished.State != "running" || finished.Observation.EntryURL != live.Observation.EntryURL || runner.runCount() != 2 {
		t.Fatalf("resume convergence recreated runtime: result=%#v err=%v runs=%d", finished, err, runner.runCount())
	}
}

func TestWorkspaceApplicationOPLCredentialsAreVersionedAndSecretsRetireAfterConsumers(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OPL_AIONUI_ADMIN_PASSWORD_SEED", "application-credential-fixture-seed")
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID = ""
	volume.SizeGB = 10
	service.volumes["storage-alpha"] = volume
	const gatewayKey = "synthetic-gateway-key-for-application-test"
	secret, err := provider.UpsertGatewaySecret(ctx, GatewaySecretInput{AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", WorkspaceAPIKeyID: 7, GatewayAPIKey: gatewayKey, Fingerprint: "sha256:" + stableSuffix(gatewayKey), IdempotencyKey: "secret-setup"})
	if err != nil {
		t.Fatal(err)
	}
	input := applicationRuntimeInput("opl-generation-1", applicationRevisionForTest())
	input.Revision.RuntimeProfile = "opl_app"
	input.Configuration.CredentialVersion = "explicit-credential-v1"
	input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "gateway", Target: "/run/secrets/opl_gateway_api_key"}}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: secret.SecretRef, Version: secret.Version, Key: "opl_gateway_api_key"}}
	input.Configuration.Environment = map[string]string{"OPL_WEBUI_USERNAME": webuiUsername, "OPL_WEBUI_PASSWORD_FILE": "/run/secrets/opl_webui_password", "OPL_WEBUI_SESSION_SECRET_FILE": "/run/secrets/webui_session_secret"}
	input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err := service.PreflightWorkspaceApplicationRuntime(ctx, input); err != nil || runner.runCount() != 0 {
		t.Fatalf("preflight err=%v runs=%d", err, runner.runCount())
	}
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, input); err != nil {
		t.Fatal(err)
	}
	credentials, err := service.ReadWorkspaceApplicationRuntimeCredentials(ctx, applicationLifecycleInput(input, "running", "credentials"))
	if err != nil || credentials.WebUIUsername != webuiUsername || credentials.WebUIPassword == "" {
		t.Fatalf("credential read err=%v", err)
	}
	cleanup := WorkspaceApplicationGatewaySecretCleanupInput{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, SecretRef: secret.SecretRef, IdempotencyKey: "secret-cleanup"}
	if err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, cleanup); err == nil {
		t.Fatal("bound Gateway Secret was removed")
	}
	next := input
	next.RuntimeOperationID = "opl-generation-2"
	next.IdempotencyKey = next.RuntimeOperationID
	if _, err := service.CreateWorkspaceApplicationRuntime(ctx, next); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.ReadWorkspaceApplicationRuntimeCredentials(ctx, applicationLifecycleInput(next, "running", "credentials-next"))
	if err != nil || rotated.WebUIPassword != credentials.WebUIPassword {
		t.Fatalf("ordinary image generation changed credentials: err=%v", err)
	}
	rotation := next
	rotation.Configuration.CredentialVersion = "explicit-credential-v2"
	changed, err := applicationWebUICredentials(rotation)
	if err != nil || string(changed.Password) == credentials.WebUIPassword {
		t.Fatalf("explicit credential rotation did not change password: %v", err)
	}
	operations, err := service.ListOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	journal := string(mustJSON(operations))
	if strings.Contains(journal, gatewayKey) || strings.Contains(journal, credentials.WebUIPassword) || strings.Contains(journal, rotated.WebUIPassword) {
		t.Fatal("credential value leaked into operation journal")
	}
	for _, generation := range []WorkspaceApplicationRuntimeInput{input, next} {
		result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(generation, "absent", generation.RuntimeOperationID+":retire"))
		if err != nil || result.State != "absent" {
			t.Fatalf("retire err=%v state=%s", err, result.State)
		}
	}
	if err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, cleanup); err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.readGatewaySecretFiles(secret.SecretRef); !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		t.Fatalf("Gateway secret still present: %v", err)
	}
}

func TestWorkspaceApplicationMigrationPreservesProvenRotatedCredentials(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OPL_AIONUI_ADMIN_PASSWORD_SEED", "migration-synthetic-seed")
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	input.Revision.RuntimeProfile = "opl_app"
	input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "gateway", Target: "/run/secrets/opl_gateway_api_key"}}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: gatewaySecretName(input.WorkspaceID), Version: "gateway-version", Key: "opl_gateway_api_key"}}
	fake.resources["Secret:"+gatewaySecretName(input.WorkspaceID)] = map[string]any{"kind": "Secret", "metadata": map[string]any{"name": gatewaySecretName(input.WorkspaceID), "annotations": map[string]any{"oplcloud.cn/account-id": input.AccountID, "oplcloud.cn/workspace-id": input.WorkspaceID, "oplcloud.cn/secret-version": "gateway-version"}}, "data": map[string]any{"opl_gateway_api_key": base64.StdEncoding.EncodeToString([]byte("synthetic-gateway"))}}
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	sourceID := "retained-full-runtime"
	originalToken := stableID(input.WorkspaceID, sourceID)[:24]
	original := WorkspaceRuntime{ID: "rt_old", WorkspaceID: input.WorkspaceID, OperationID: sourceID, ServiceName: "old-full-service", Access: RuntimeAccess{CredentialVersion: stableID("workspace-credential", input.WorkspaceID, originalToken)[:16]}}
	creation := newOperation("create_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, sourceID, "original-hash", time.Now().Add(-time.Hour))
	creation.ID, creation.Status = "original-create", "succeeded"
	fillOperationResource(&creation, original)
	if err := store.Append(ctx, creation); err != nil {
		t.Fatal(err)
	}
	rotationKey := "runtime-credential-rotate:" + input.WorkspaceID + ":explicit-reset:runtime"
	rotatedToken := stableID(input.WorkspaceID, rotationKey)[:24]
	rotated := original
	rotated.Access.CredentialVersion = stableID("workspace-credential", input.WorkspaceID, rotatedToken)[:16]
	update := newOperation("update_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, rotationKey, "rotation-hash", time.Now())
	update.ID, update.Status = "explicit-rotation", "succeeded"
	fillOperationResource(&update, rotated)
	if err := store.Append(ctx, update); err != nil {
		t.Fatal(err)
	}
	input.Configuration.CredentialVersion = rotated.Access.CredentialVersion
	input.Configuration.CredentialSourceRuntimeOperationID = sourceID
	input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	provenCtx, err := service.applicationCredentialContext(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.EnsureWorkspaceApplicationRuntime(provenCtx, input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	credentials, err := provider.ReadWorkspaceApplicationRuntimeCredentials(ctx, input)
	if err != nil || credentials.WebUIPassword != deriveAionUIAdminPassword("migration-synthetic-seed", input.WorkspaceID, rotatedToken) {
		t.Fatalf("migration did not preserve rotated password: %v", err)
	}
	secret := fake.resources["Secret:"+workspaceApplicationComponentResourceName(input, "secrets")]
	if stringValue(nested(secret, "data", "webui-session")) != base64.StdEncoding.EncodeToString([]byte(deriveWebUISessionSecret("migration-synthetic-seed", input.WorkspaceID, rotatedToken))) {
		t.Fatal("migration did not preserve rotated session secret")
	}
	input.Configuration.CredentialVersion = original.Access.CredentialVersion
	if _, err := service.applicationCredentialContext(ctx, input); err == nil {
		t.Fatal("migration accepted superseded credential version")
	}
	// Local Docker's proven version is itself the original derivation token.
	input.Configuration.CredentialVersion = "local-original-version"
	local, err := applicationWebUICredentials(input)
	existing, oldErr := localDockerWebUICredentialsFor(localDockerGatewayMetadata{WorkspaceID: input.WorkspaceID, Version: input.Configuration.CredentialVersion})
	if err != nil || oldErr != nil || string(local.Password) != string(existing.Password) || string(local.SessionSecret) != string(existing.SessionSecret) {
		t.Fatalf("Local migration changed credentials: %v %v", err, oldErr)
	}
}

func TestWorkspaceApplicationGatewaySecretCleanupFencesLateUpsert(t *testing.T) {
	ctx := context.Background()
	provider, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	cleanup := WorkspaceApplicationGatewaySecretCleanupInput{AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SecretRef: contracts.WorkspaceGatewaySecretRef("workspace-alpha"), IdempotencyKey: "delete-before-secret"}
	if err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, cleanup); err != nil {
		t.Fatal(err)
	}
	service = runtimeTestService(provider, store)
	key := "late-synthetic-key"
	input := GatewaySecretInput{AccountID: cleanup.AccountID, WorkspaceID: cleanup.WorkspaceID, WorkspaceAPIKeyID: 7, GatewayAPIKey: key, Fingerprint: "sha256:" + stableSuffix(key), IdempotencyKey: "late-secret-upsert"}
	if _, err := service.UpsertGatewaySecret(ctx, input); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("late Secret write escaped deletion fence: %v", err)
	}
	if _, _, err := provider.readGatewaySecretFiles(cleanup.SecretRef); !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		t.Fatalf("late Secret created: %v", err)
	}
}
