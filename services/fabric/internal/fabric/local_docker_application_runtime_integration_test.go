package fabric

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// TestLocalDockerApplicationRuntimeEndToEndNonOPLApplication deploys a
// genuinely non-OPL application (a node HTTP visit counter) through the
// application runtime engine onto real Docker resources, then proves over
// HTTP that it serves traffic and that its data on the workspace storage
// survives full container replacement.
func TestLocalDockerApplicationRuntimeEndToEndNonOPLApplication(t *testing.T) {
	if os.Getenv("OPL_FABRIC_LOCAL_DOCKER_INTEGRATION") != "1" {
		t.Skip("set OPL_FABRIC_LOCAL_DOCKER_INTEGRATION=1 to run against the local Docker daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Fatalf("docker daemon unavailable: %v: %s", err, output)
	}

	// The application image is deliberately NOT the OPL workspace image: a
	// minimal visit-counter service whose data lives on the workspace mount.
	// The base image is the qualification workspace's pinned base so the build
	// needs no network pull.
	qualificationDockerfilePath := qualificationWorkspaceDockerfile(t)
	qualificationBody, readErr := os.ReadFile(qualificationDockerfilePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	base := ""
	for _, line := range strings.Split(string(qualificationBody), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && strings.EqualFold(fields[0], "FROM") {
			base = fields[1]
			break
		}
	}
	if base == "" {
		t.Fatal("qualification workspace dockerfile has no FROM line")
	}
	buildDir := t.TempDir()
	dockerfile := "FROM " + base + "\nWORKDIR /app\nCOPY server.js .\nCMD [\"node\", \"server.js\"]\n"
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}
	serverSource := `const fs = require('fs');
const file = '/data/visits';
let visits = 0;
try { visits = parseInt(fs.readFileSync(file, 'utf8'), 10) || 0; } catch {}
visits += 1;
fs.writeFileSync(file, String(visits));
require('node:http').createServer((request, response) => {
  response.writeHead(200, { 'content-type': 'text/plain' });
  response.end('visits: ' + visits + '\n');
}).listen(8080, '0.0.0.0');
`
	if err := os.WriteFile(filepath.Join(buildDir, "server.js"), []byte(serverSource), 0644); err != nil {
		t.Fatal(err)
	}
	tag := "opl-fabric-application-test:" + stableSuffix(t.Name(), time.Now().String())[:12]
	imageBuild := exec.CommandContext(ctx, "docker", "build", "--quiet", "--file", filepath.Join(buildDir, "Dockerfile"), "--tag", tag, buildDir)
	if output, err := imageBuild.CombinedOutput(); err != nil {
		t.Fatalf("build application image: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", "-f", tag).Run() })

	// Push the image into a throwaway local registry so the revision carries a
	// genuine repo@sha256 reference — the same shape production uses via TCR.
	registryName := "opl-fabric-application-registry-test"
	registryPort := 5500 + int(time.Now().UnixNano()%500)
	registryContainer := fmt.Sprintf("localhost:%d", registryPort)
	registryRun := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "--name", registryName, "-p", fmt.Sprintf("127.0.0.1:%d:5000", registryPort), "registry:2")
	if output, err := registryRun.CombinedOutput(); err != nil {
		t.Fatalf("start local registry: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "container", "rm", "-f", registryName).Run() })
	registryReadyBy := time.Now().Add(30 * time.Second)
	for {
		check := exec.CommandContext(ctx, "docker", "exec", registryName, "wget", "-q", "-O", "-", "http://localhost:5000/v2/").Run()
		if check == nil {
			break
		}
		if time.Now().After(registryReadyBy) {
			t.Fatal("local registry never became ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
	repo := registryContainer + "/visit-counter"
	if output, err := exec.CommandContext(ctx, "docker", "tag", tag, repo+":e2e").CombinedOutput(); err != nil {
		t.Fatalf("tag application image: %v: %s", err, output)
	}
	push := exec.CommandContext(ctx, "docker", "push", repo+":e2e")
	if output, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push application image: %v: %s", err, output)
	}
	digestOutput, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", repo+":e2e").Output()
	if err != nil {
		t.Fatal(err)
	}
	imageID := strings.TrimSpace(string(digestOutput))
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", "-f", repo+":e2e").Run() })

	launchID := "local-app-" + stableSuffix(t.Name(), time.Now().String())[:12]
	accountID, workspaceID := "acct-local", "ws-"+stableSuffix(launchID)[:10]
	runner := &execDockerRunner{binary: "docker"}
	storageRoot := localDockerStorageTestRoot(t)
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: storageRoot, RuntimeHost: "127.0.0.1",
		PublishHost:                  "127.0.0.1",
		StorageQuotaBackend:          localDockerStorageTestQuota(storageRoot),
		TrustedWorkspaceImageSources: []string{imageID},
	}, runner)
	store := NewMemoryOperationStore()
	service := NewServiceWithOperationStore(provider, store)
	t.Cleanup(func() {
		for _, componentName := range []string{"main", "retrieval"} {
			name, nameErr := localDockerApplicationComponentName(workspaceID, componentName)
			if nameErr != nil {
				continue
			}
			_ = exec.Command("docker", "container", "rm", "-f", name).Run()
		}
	})

	// Provision the workspace's compute, storage and attachment through the
	// real launch stages: the application deploys into delivered resources.
	preflight, err := service.PreflightWorkspaceLaunch(ctx, WorkspaceLaunchPreflightInput{
		SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID,
		PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: imageID, RequestHash: strings.Repeat("b", 64),
	})
	if err != nil || !preflight.Available {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	stage := func(stage, action string) WorkspaceLaunchStageInput {
		input := WorkspaceLaunchStageInput{
			ProviderProfileRef: "local-docker", ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest,
			PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: imageID,
			Binding: WorkspaceLaunchStageBinding{
				SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID,
				Stage: stage, Action: action, FabricOperationID: launchID + ":" + stage, IdempotencyKey: launchID + ":" + stage,
			},
		}
		input.Binding.RequestHash = workspaceLaunchStageRequestHash(input, strings.Repeat("b", 64))
		return input
	}
	computeInput := stage("ensure_compute_allocation", "ensure_compute_allocation")
	compute, err := service.EnsureWorkspaceLaunchStage(ctx, computeInput)
	if err != nil || compute.State != "ready" {
		t.Fatalf("compute=%#v err=%v", compute, err)
	}
	storageInput := stage("storage", "ensure_storage")
	storageInput.Resources = compute.Resources
	storageInput.Binding.RequestHash = workspaceLaunchStageRequestHash(storageInput, strings.Repeat("b", 64))
	storage, err := service.EnsureWorkspaceLaunchStage(ctx, storageInput)
	if err != nil || storage.State != "ready" {
		t.Fatalf("storage=%#v err=%v", storage, err)
	}
	attachmentInput := stage("attachment", "ensure_attachment")
	attachmentInput.Resources = storage.Resources
	attachmentInput.Binding.RequestHash = workspaceLaunchStageRequestHash(attachmentInput, strings.Repeat("b", 64))
	attachment, err := service.EnsureWorkspaceLaunchStage(ctx, attachmentInput)
	if err != nil || attachment.State != "ready" {
		t.Fatalf("attachment=%#v err=%v", attachment, err)
	}

	// Recreate the service from the same store so the engine's resource maps
	// replay the provisioned compute, storage and attachment.
	service = NewServiceWithOperationStore(provider, store)
	revision := contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "visit-counter", Version: "1.0.0", Platform: "linux/amd64",
		Image:            imageID,
		Ports:            []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		ExposurePolicy:   "application",
	}
	runtimeInput := WorkspaceApplicationRuntimeInput{
		WorkspaceID: workspaceID, ComputeID: compute.Resources.ComputeAllocationID, VolumeID: storage.Resources.StorageID,
		AttachmentID: attachment.Resources.AttachmentID, AttachmentOperationID: attachment.Resources.AttachmentBindingRef,
		RuntimeOperationID: launchID + ":application-runtime", Revision: revision,
		ConfigurationDigest: strings.Repeat("c", 64),
	}
	ensure := func(key string) contracts.WorkspaceApplicationRuntimeObservation {
		t.Helper()
		observation, ensureErr := service.CreateWorkspaceApplicationRuntime(ctx, func() WorkspaceApplicationRuntimeInput {
			copied := runtimeInput
			copied.IdempotencyKey = key
			return copied
		}())
		if ensureErr != nil {
			t.Fatalf("ensure %q: %v", key, ensureErr)
		}
		return observation
	}

	first := ensure(launchID + ":app-1")
	if first.Status != "ready" || first.EntryURL == "" {
		t.Fatalf("first observation=%#v", first)
	}
	if err := waitForLocalRuntime(ctx, first.EntryURL); err != nil {
		t.Fatalf("entry not reachable: %v", err)
	}
	body := httpGetBody(t, first.EntryURL)
	if !strings.Contains(body, "visits: 1") {
		t.Fatalf("first visit body=%q", body)
	}

	// Replace the whole main container: the local workspace mount must survive.
	containerName, nameErr := localDockerApplicationComponentName(workspaceID, "main")
	if nameErr != nil {
		t.Fatal(nameErr)
	}
	if output, err := exec.CommandContext(ctx, "docker", "container", "rm", "-f", containerName).CombinedOutput(); err != nil {
		t.Fatalf("remove container: %v: %s", err, output)
	}
	second := ensure(launchID + ":app-2")
	if second.Status != "ready" || second.EntryURL == "" {
		t.Fatalf("second observation=%#v", second)
	}
	if err := waitForLocalRuntime(ctx, second.EntryURL); err != nil {
		t.Fatalf("recreated entry not reachable: %v", err)
	}
	body = httpGetBody(t, second.EntryURL)
	if !strings.Contains(body, "visits: 2") {
		t.Fatalf("visit counter did not persist across replacement: %q", body)
	}
	t.Log("non-OPL application deployed, served over HTTP, and its local workspace data survived full container replacement")
}

func httpGetBody(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", url, response.StatusCode, body)
	}
	return string(body)
}
