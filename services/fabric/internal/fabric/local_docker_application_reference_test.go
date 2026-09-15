//go:build application_reference

package fabric

// Opt-in runtime qualification only: go test -tags application_reference -run
// '^TestApplicationReferenceRuntime$'. OPL_APPLICATION_REFERENCE names an
// external JSON combining the publisher's typed revision/configuration with
// explicit local fixture sources below. Missing private material is a failure,
// never a skip or a synthetic replacement. Normal verification excludes this file.
//
// Source data is qualification pre-seeding, NOT a product Restore API. Local
// storage uses the existing integration quota fixture; this does not qualify
// host quota enforcement, Tencent execution/security capabilities, or business
// questions/SSE/citations. Publisher business verification remains external.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

type applicationReferenceSecret struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}
type applicationReferenceData struct {
	Name            string `json:"name"`
	SourceDirectory string `json:"sourceDirectory,omitempty"`
	SourceVolume    string `json:"sourceVolume,omitempty"`
}
type applicationReferenceSecurity struct {
	Digest string `json:"digest"`
	File   string `json:"file"`
}
type applicationReferenceFixture struct {
	Revision         contracts.WorkspaceApplicationRevision             `json:"revision"`
	Configuration    contracts.WorkspaceApplicationRuntimeConfiguration `json:"configuration"`
	SecretSources    []applicationReferenceSecret                       `json:"secretSources"`
	FixtureData      []applicationReferenceData                         `json:"fixtureData"`
	SecurityProfiles []applicationReferenceSecurity                     `json:"securityProfiles"`
	ProbeImage       string                                             `json:"probeImage"`
	CopyImage        string                                             `json:"copyImage"` // Publisher-selected image with /bin/sh and cp -a.
	SizeGB           int                                                `json:"sizeGB"`
	TimeoutSeconds   int                                                `json:"timeoutSeconds"`
}
type applicationReferenceMaterial struct {
	fixture  applicationReferenceFixture
	secrets  map[string][]byte
	security map[string][]byte
}

func readApplicationReferenceFile(path, digest string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("reference_source_path_invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("reference_source_file_missing")
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 {
		return nil, errors.New("reference_source_file_empty_or_unreadable")
	}
	if fmt.Sprintf("sha256:%x", sha256.Sum256(body)) != digest {
		return nil, errors.New("reference_source_digest_mismatch")
	}
	return body, nil
}
func loadApplicationReference(path string) (applicationReferenceMaterial, error) {
	result := applicationReferenceMaterial{secrets: map[string][]byte{}, security: map[string][]byte{}}
	if path == "" {
		return result, errors.New("OPL_APPLICATION_REFERENCE_required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return result, errors.New("reference_file_unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.fixture); err != nil {
		return result, errors.New("reference_json_invalid")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, errors.New("reference_json_trailing_value")
	}
	fixture := result.fixture
	if err := contracts.ValidateWorkspaceApplicationRevision(fixture.Revision); err != nil {
		return result, err
	}
	if fixture.Revision.RuntimeProfile != "" {
		return result, errors.New("reference_harness_requires_application_owned_credentials")
	}
	if !contracts.ValidWorkspaceImageReference(fixture.ProbeImage) || !contracts.ValidWorkspaceImageReference(fixture.CopyImage) || fixture.SizeGB < 10 || fixture.SizeGB%10 != 0 || fixture.TimeoutSeconds < 1 || fixture.TimeoutSeconds > 1800 {
		return result, errors.New("reference_fixture_bounds_invalid")
	}
	expectedSecrets, expectedData, expectedSecurity := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, secret := range fixture.Revision.SecretInputs {
		expectedSecrets[secret.Name] = true
	}
	for _, mount := range fixture.Revision.PersistentMounts {
		expectedData[mount.Name] = true
	}
	if fixture.Revision.Execution.SeccompProfile != "" {
		expectedSecurity[fixture.Revision.Execution.SeccompProfile] = true
	}
	for _, dependency := range fixture.Revision.Dependencies {
		for _, secret := range dependency.SecretInputs {
			expectedSecrets[secret.Name] = true
		}
		for _, mount := range dependency.PersistentMounts {
			expectedData[mount.Name] = true
		}
		if dependency.Execution.SeccompProfile != "" {
			expectedSecurity[dependency.Execution.SeccompProfile] = true
		}
	}
	for _, source := range fixture.SecretSources {
		if !expectedSecrets[source.Name] {
			return result, errors.New("reference_secret_undeclared_or_duplicate")
		}
		delete(expectedSecrets, source.Name)
		value, err := readApplicationReferenceFile(source.File, source.SHA256)
		if err != nil {
			return result, fmt.Errorf("reference_secret_%s: %w", source.Name, err)
		}
		result.secrets[source.Name] = value
	}
	if len(expectedSecrets) > 0 {
		return result, errors.New("reference_declared_secret_source_missing")
	}
	for _, source := range fixture.SecurityProfiles {
		if !expectedSecurity[source.Digest] {
			return result, errors.New("reference_security_undeclared_or_duplicate")
		}
		delete(expectedSecurity, source.Digest)
		value, err := readApplicationReferenceFile(source.File, source.Digest)
		if err != nil || !json.Valid(value) {
			return result, errors.New("reference_security_source_invalid")
		}
		result.security[source.Digest] = value
	}
	if len(expectedSecurity) > 0 {
		return result, errors.New("reference_security_source_missing")
	}
	for _, source := range fixture.FixtureData {
		if !expectedData[source.Name] || (source.SourceDirectory == "") == (source.SourceVolume == "") {
			return result, errors.New("reference_data_undeclared_duplicate_or_ambiguous")
		}
		delete(expectedData, source.Name)
		if source.SourceDirectory != "" {
			info, err := os.Lstat(source.SourceDirectory)
			if err != nil || !info.IsDir() || !filepath.IsAbs(source.SourceDirectory) || filepath.Clean(source.SourceDirectory) != source.SourceDirectory || strings.ContainsAny(source.SourceDirectory, ",\r\n") {
				return result, errors.New("reference_data_directory_invalid")
			}
		} else if !localDockerApplicationSecretName.MatchString(source.SourceVolume) {
			return result, errors.New("reference_data_volume_name_invalid")
		}
	}
	if len(expectedData) > 0 {
		return result, errors.New("reference_declared_data_source_missing")
	}
	// Validate all configuration/secret declarations before any temporary store
	// or Docker resources are written. These are admission-only identities.
	input := WorkspaceApplicationRuntimeInput{SchemaVersion: 2, DataBindingID: "reference-validation", Revision: fixture.Revision, Configuration: fixture.Configuration}
	for _, source := range fixture.SecretSources {
		input.SecretBindings = append(input.SecretBindings, contracts.WorkspaceApplicationRuntimeSecretBinding{Name: source.Name, SecretRef: source.Name, Version: source.SHA256, Key: "value"})
	}
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		return result, err
	}
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return result, err
	}
	// Secret values enter only the selected component; env-file sources must be
	// exactly one line, while declared file sources may contain structured data.
	checkEnv := func(inputs []contracts.WorkspaceApplicationSecretInput) error {
		for _, declared := range inputs {
			if declared.Env != "" && bytes.ContainsAny(result.secrets[declared.Name], "\x00\r\n") {
				return errors.New("reference_secret_environment_value_invalid")
			}
		}
		return nil
	}
	if err := checkEnv(fixture.Revision.SecretInputs); err != nil {
		return result, err
	}
	for _, dependency := range fixture.Revision.Dependencies {
		if err := checkEnv(dependency.SecretInputs); err != nil {
			return result, err
		}
	}
	return result, nil
}

// All daemon checks here are read-only and precede any resource/store writes.
func validateApplicationReferenceDocker(ctx context.Context, runner dockerRunner, fixture applicationReferenceFixture) error {
	images := []string{fixture.ProbeImage, fixture.CopyImage}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(fixture.Revision) {
		images = append(images, component.Image)
	}
	for _, image := range images {
		if _, err := runner.Run(ctx, nil, "image", "inspect", image); err != nil {
			return errors.New("reference_pinned_image_unavailable")
		}
	}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(fixture.Revision) {
		platform, err := runner.Run(ctx, nil, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", component.Image)
		if err != nil || strings.TrimSpace(string(platform)) != fixture.Revision.Platform {
			return errors.New("reference_component_image_platform_mismatch")
		}
	}
	for _, source := range fixture.FixtureData {
		if source.SourceVolume == "" {
			continue
		}
		if _, err := runner.Run(ctx, nil, "volume", "inspect", source.SourceVolume); err != nil {
			return errors.New("reference_source_volume_unavailable")
		}
		active, err := runner.Run(ctx, nil, "container", "ls", "--quiet", "--filter", "volume="+source.SourceVolume)
		if err != nil || strings.TrimSpace(string(active)) != "" {
			return errors.New("reference_source_volume_active_or_unknown")
		}
	}
	return nil
}

func TestApplicationReferenceRuntime(t *testing.T) {
	// This loader reads exact externally authorized files in memory but never
	// prints their values; copies below exist only in the temporary approved store.
	material, err := loadApplicationReference(os.Getenv("OPL_APPLICATION_REFERENCE"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := material.fixture
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(fixture.TimeoutSeconds)*time.Second)
	defer cancel()
	runner := execDockerRunner{binary: "docker"}
	if err := validateApplicationReferenceDocker(ctx, runner, fixture); err != nil {
		t.Fatal(err)
	}
	launchID := "reference-" + stableSuffix(t.Name(), time.Now().UTC().String())[:16]
	workspaceID, accountID := launchID, "application-reference"
	storageRoot := localDockerStorageTestRoot(t)
	secretRoot := t.TempDir()
	if err := os.Chmod(secretRoot, 0700); err != nil {
		t.Fatal(err)
	}
	profile, _ := json.Marshal(localDockerProviderProfile{SchemaVersion: 1, Packages: []localDockerPackageProfile{{ID: "reference", Name: "Reference", Available: true, Compute: ComputePlan{ID: "reference", Server: "reference", InstanceType: "local-reference", CPU: 2, MemoryGB: 4, DiskGB: fixture.SizeGB}, Storage: localDockerStoragePlan{SizeGB: fixture.SizeGB, QuotaPolicy: "linux-project"}}}})
	trusted := []string{fixture.ProbeImage, fixture.CopyImage}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(fixture.Revision) {
		trusted = append(trusted, component.Image)
	}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{GatewaySecretRoot: secretRoot, HostStorageRoot: storageRoot, PublishHost: "127.0.0.1", ApplicationProbeImage: fixture.ProbeImage, ProviderProfileJSON: profile, StorageQuotaBackend: localDockerStorageTestQuota(storageRoot), TrustedWorkspaceImageSources: trusted}, runner)
	input := WorkspaceApplicationRuntimeInput{SchemaVersion: 2, AccountID: accountID, WorkspaceID: workspaceID, RuntimeOperationID: launchID + ":application", IdempotencyKey: launchID + ":application", DataBindingID: launchID + "-data", Revision: fixture.Revision, Configuration: fixture.Configuration}
	for index, source := range fixture.SecretSources {
		metadata, _ := provisionApplicationSecretFixture(t, provider, input, fmt.Sprintf("%s-secret-%d", launchID, index), map[string]string{"value": string(material.secrets[source.Name])})
		input.SecretBindings = append(input.SecretBindings, contracts.WorkspaceApplicationRuntimeSecretBinding{Name: source.Name, SecretRef: metadata.SecretRef, Version: metadata.Version, Key: "value"})
	}
	for digest, value := range material.security {
		directory := filepath.Join(secretRoot, "application-security-profiles")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, strings.Replace(digest, ":", "-", 1)+".json"), value, 0400); err != nil {
			t.Fatal(err)
		}
	}
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal("reference configuration digest invalid")
	}
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		t.Fatal(err)
	}
	if err := provider.validateLocalDockerApplicationProbe(input.Revision); err != nil {
		t.Fatal(err)
	}
	if err := provider.validateApplicationExecutions(input); err != nil {
		t.Fatal(err)
	}
	// Resolve every declared private input before the first allocation.
	if _, _, err := provider.applicationSecretFiles(input); err != nil {
		t.Fatal("reference main secret validation failed")
	}
	for _, dependency := range input.Revision.Dependencies {
		if _, _, err := provider.applicationDeclaredSecrets(input, dependency.SecretInputs); err != nil {
			t.Fatal("reference dependency secret validation failed")
		}
	}
	store := NewMemoryOperationStore()
	service := NewServiceWithOperationStore(provider, store)
	requestHash := stableSuffix(launchID)
	preflight, err := service.PreflightWorkspaceLaunch(ctx, WorkspaceLaunchPreflightInput{SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID, PackageID: "reference", SizeGB: fixture.SizeGB, WorkspaceImageDigest: fixture.Revision.Image, RequestHash: requestHash})
	if err != nil || !preflight.Available {
		t.Fatal("reference resource preflight failed")
	}
	closeout := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID, ProviderProfileRef: "local-docker", ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, IdempotencyKey: launchID + ":closeout"}
	runtimeAttempted := false
	t.Logf("reference scope launch=%s workspace=%s dataBinding=%s configurationDigest=%s", launchID, workspaceID, input.DataBindingID, input.ConfigurationDigest)
	// Register before the first allocation so failed creation also has bounded,
	// exact-owner cleanup; no original image/volume enters a deletion call.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if runtimeAttempted {
			if result, err := provider.SetWorkspaceApplicationRuntimeLifecycle(cleanupCtx, input, "absent"); err != nil || result.State != "absent" {
				t.Errorf("reference runtime cleanup not confirmed workspace=%s", workspaceID)
				return
			}
		}
		for {
			result, err := service.CloseoutWorkspaceLaunch(cleanupCtx, closeout)
			if err == nil && result.State == "absent" {
				return
			}
			if err != nil || result.State == "blocked" {
				t.Errorf("reference resource closeout not confirmed workspace=%s", workspaceID)
				return
			}
			select {
			case <-cleanupCtx.Done():
				t.Errorf("reference resource closeout deadline workspace=%s", workspaceID)
				return
			case <-time.After(time.Second):
			}
		}
	})
	stage := func(name, action string, resources WorkspaceLaunchResources) WorkspaceLaunchStageInput {
		value := WorkspaceLaunchStageInput{ProviderProfileRef: "local-docker", ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, PackageID: "reference", SizeGB: fixture.SizeGB, WorkspaceImageDigest: fixture.Revision.Image, Resources: resources, Binding: WorkspaceLaunchStageBinding{SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID, Stage: name, Action: action, FabricOperationID: launchID + ":" + name, IdempotencyKey: launchID + ":" + name}}
		value.Binding.RequestHash = workspaceLaunchStageRequestHash(value, requestHash)
		return value
	}
	compute, err := service.EnsureWorkspaceLaunchStage(ctx, stage("ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{}))
	if err != nil || compute.State != "ready" {
		t.Fatal("reference compute allocation failed")
	}
	storage, err := service.EnsureWorkspaceLaunchStage(ctx, stage("storage", "ensure_storage", compute.Resources))
	if err != nil || storage.State != "ready" {
		t.Fatal("reference storage allocation failed")
	}
	attachment, err := service.EnsureWorkspaceLaunchStage(ctx, stage("attachment", "ensure_attachment", storage.Resources))
	if err != nil || attachment.State != "ready" {
		t.Fatal("reference attachment failed")
	}
	service = NewServiceWithOperationStore(provider, store)
	input.ComputeID, input.VolumeID = compute.Resources.ComputeAllocationID, storage.Resources.StorageID
	input.AttachmentID, input.AttachmentOperationID = attachment.Resources.AttachmentID, attachment.Resources.AttachmentBindingRef
	paths, err := provider.storagePaths(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(paths.Data, contracts.WorkspaceApplicationDataDirectory(input.DataBindingID))
	if err := os.Mkdir(dataRoot, 0755); err != nil {
		t.Fatal("reference data binding must be new")
	}
	for index, source := range fixture.FixtureData {
		target := filepath.Join(dataRoot, source.Name)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("reference target already exists")
		}
		copyName := fmt.Sprintf("%s-copy-%d", launchID, index)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _ = runner.Run(cleanupCtx, nil, "container", "rm", "--force", copyName)
		})
		sourceMount := "type=volume,source=" + source.SourceVolume + ",target=/source,readonly"
		if source.SourceDirectory != "" {
			sourceMount = "type=bind,source=" + source.SourceDirectory + ",target=/source,readonly"
		}
		if _, err := runner.Run(ctx, nil, "run", "--pull", "never", "--name", copyName, "--network", "none", "--label", "opl.reference.workspace="+workspaceID, "--mount", sourceMount, "--mount", "type=bind,source="+dataRoot+",target=/destination", "--entrypoint", "/bin/sh", fixture.CopyImage, "-c", `test ! -e "$1" && cp -a /source "$1"`, "reference-copy", "/destination/"+source.Name); err != nil {
			t.Fatal("reference data copy failed")
		}
		if _, err := runner.Run(ctx, nil, "container", "rm", copyName); err != nil {
			t.Fatal("reference copy cleanup failed")
		}
	}
	runtimeAttempted = true
	entryURL := ""
	for {
		result, err := service.CreateWorkspaceApplicationRuntime(ctx, input)
		if err == nil && result.Status == "ready" {
			entryURL = result.EntryURL
			break
		}
		if !errors.Is(err, ErrWorkspaceLaunchPending) {
			message := fmt.Sprint(err)
			for _, value := range material.secrets {
				message = strings.ReplaceAll(message, string(value), "[redacted]")
			}
			for _, component := range result.Components {
				t.Logf("reference component=%s state=%s", component.Name, component.State)
				if component.State == "failed" {
					name, _ := localDockerApplicationComponentNameForInput(input, component.Name)
					body, _ := runner.Run(ctx, nil, "logs", "--tail", "25", name)
					text := string(body)
					for _, value := range material.secrets {
						text = strings.ReplaceAll(text, string(value), "[redacted]")
					}
					t.Logf("reference failed component log: %s", text)
				}
			}
			t.Fatalf("reference runtime startup failed: %s", message)
		}
		select {
		case <-ctx.Done():
			t.Fatal("reference runtime readiness deadline")
		case <-time.After(2 * time.Second):
		}
	}
	// Optional publisher-owned business verifier runs before ephemeral state is
	// suspended. No application-specific protocol enters the Fabric adapter.
	if verifier := os.Getenv("OPL_APPLICATION_REFERENCE_VERIFIER"); verifier != "" {
		if !filepath.IsAbs(verifier) {
			t.Fatal("reference verifier must be an absolute executable path")
		}
		name, _ := localDockerApplicationComponentNameForInput(input, "main")
		command := exec.CommandContext(ctx, verifier, entryURL, name)
		if err := command.Run(); err != nil {
			t.Fatal("publisher business verification failed; see publisher evidence")
		}
		t.Log("publisher business verifier passed")
	}
	for _, desired := range []string{"suspended", "running", "absent"} {
		lifecycle := WorkspaceApplicationRuntimeLifecycleInput{AccountID: accountID, WorkspaceID: workspaceID, RuntimeID: applicationRuntimeID(input), RuntimeOperationID: input.RuntimeOperationID, DesiredState: desired, IdempotencyKey: launchID + ":" + desired}
		for {
			result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle)
			if err != nil {
				t.Fatal("reference lifecycle failed")
			}
			if result.State == desired {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("reference lifecycle deadline")
			case <-time.After(2 * time.Second):
			}
		}
	}
	t.Logf("runtime qualification passed workspace=%s runtime=%s configurationDigest=%s; seeded fixture data only; publisherVerifierInvoked=%t", workspaceID, applicationRuntimeID(input), input.ConfigurationDigest, os.Getenv("OPL_APPLICATION_REFERENCE_VERIFIER") != "")
}

func TestApplicationReferenceValidationRequiresPrivateMaterial(t *testing.T) {
	fixture := applicationReferenceFixture{Revision: applicationRevisionForTest(), ProbeImage: "example.test/probe@sha256:" + strings.Repeat("a", 64), CopyImage: "example.test/copy@sha256:" + strings.Repeat("b", 64), SizeGB: 10, TimeoutSeconds: 60}
	fixture.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "model-config", Target: "/run/secrets/model_config"}}
	source := filepath.Join(t.TempDir(), "reference.json")
	body, _ := json.Marshal(fixture)
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadApplicationReference(source); err == nil || err.Error() != "reference_declared_secret_source_missing" {
		t.Fatalf("missing private input not rejected before daemon access: %v", err)
	}
}

func TestApplicationReferenceValidationFreezesExternalSources(t *testing.T) {
	for _, mode := range []string{"valid", "missing-file", "digest-drift", "env-newline", "missing-data", "duplicate-secret"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			secretPath := filepath.Join(directory, "credential")
			value := []byte("unit-test-material")
			if mode == "env-newline" {
				value = append(value, '\n')
			}
			if err := os.WriteFile(secretPath, value, 0400); err != nil {
				t.Fatal(err)
			}
			fixture := applicationReferenceFixture{Revision: applicationRevisionForTest(), ProbeImage: "example.test/probe@sha256:" + strings.Repeat("a", 64), CopyImage: "example.test/copy@sha256:" + strings.Repeat("b", 64), SizeGB: 10, TimeoutSeconds: 60, FixtureData: []applicationReferenceData{{Name: "data", SourceDirectory: directory}}, SecretSources: []applicationReferenceSecret{{Name: "credential", File: secretPath, SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(value))}}}
			fixture.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "credential", Env: "PASSWORD"}}
			switch mode {
			case "missing-file":
				fixture.SecretSources[0].File = filepath.Join(directory, "absent")
			case "digest-drift":
				fixture.SecretSources[0].SHA256 = "sha256:" + strings.Repeat("0", 64)
			case "missing-data":
				fixture.FixtureData = nil
			case "duplicate-secret":
				fixture.SecretSources = append(fixture.SecretSources, fixture.SecretSources[0])
			}
			path := filepath.Join(directory, "reference.json")
			body, _ := json.Marshal(fixture)
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			_, err := loadApplicationReference(path)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("source validation mode=%s err=%v", mode, err)
			}
		})
	}
}

type applicationReferenceReadOnlyRunner struct {
	active   bool
	commands [][]string
}

func (r *applicationReferenceReadOnlyRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	r.commands = append(r.commands, args)
	if args[0] == "image" && args[1] == "inspect" && len(args) > 3 && args[2] == "--format" {
		return []byte("linux/amd64"), nil
	}
	if args[0] == "image" && args[1] == "inspect" || args[0] == "volume" && args[1] == "inspect" {
		return []byte("[]"), nil
	}
	if args[0] == "container" && args[1] == "ls" {
		if r.active {
			return []byte("active-original"), nil
		}
		return nil, nil
	}
	return nil, errors.New("unexpected_mutation")
}
func TestApplicationReferenceValidationRejectsLiveSourceVolumes(t *testing.T) {
	fixture := applicationReferenceFixture{Revision: applicationRevisionForTest(), ProbeImage: "probe", CopyImage: "copy", FixtureData: []applicationReferenceData{{Name: "data", SourceVolume: "cold-reference"}}}
	runner := &applicationReferenceReadOnlyRunner{active: true}
	if err := validateApplicationReferenceDocker(context.Background(), runner, fixture); err == nil || err.Error() != "reference_source_volume_active_or_unknown" {
		t.Fatalf("live source accepted: %v", err)
	}
	runner.active = false
	if err := validateApplicationReferenceDocker(context.Background(), runner, fixture); err != nil {
		t.Fatal(err)
	}
	for _, args := range runner.commands {
		if args[1] != "inspect" && args[1] != "ls" {
			t.Fatal("prevalidation mutated daemon")
		}
	}
}
