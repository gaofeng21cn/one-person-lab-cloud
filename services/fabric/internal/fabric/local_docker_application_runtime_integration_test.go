package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// TestLocalDockerApplicationRuntimeEndToEndNonOPLApplication exercises the OPL
// credential ABI and replacement by an unrelated application using qualification
// fixtures on real Docker. It does not qualify either upstream product image.
func TestLocalDockerApplicationRuntimeEndToEndNonOPLApplication(t *testing.T) {
	if os.Getenv("OPL_FABRIC_LOCAL_DOCKER_INTEGRATION") != "1" {
		t.Skip("set OPL_FABRIC_LOCAL_DOCKER_INTEGRATION=1 to run against the local Docker daemon")
	}
	t.Setenv("OPL_AIONUI_ADMIN_PASSWORD_SEED", "application-integration-synthetic-credential-seed")
	// The full three-generation lifecycle performs about sixteen real probes.
	// On Docker Desktop a measured successful probe takes ~35 seconds including
	// container startup/teardown. Budget the whole scenario accordingly; the
	// five-second HTTP, thirty-second execution and one-minute cleanup deadlines
	// stay independently bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
	dockerfile := "FROM " + base + "\nARG APP_FLAVOR=counter\nENV APP_FLAVOR=$APP_FLAVOR\nWORKDIR /app\nCOPY server.js .\nCMD [\"node\", \"server.js\"]\n"
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}
	serverSource := `const fs = require('fs');
const crypto = require('crypto');
const profile = process.env.OPL_WEBUI_DEPLOYMENT_MODE === 'cloud';
let password = '', session = '';
if (profile) {
  if (process.env.OPL_WEBUI_USERNAME !== 'opl' || process.env.OPL_WEBUI_AUTH_MODE !== 'password') throw new Error('OPL auth ABI mismatch');
  password = fs.readFileSync(process.env.OPL_WEBUI_PASSWORD_FILE, 'utf8');
  session = fs.readFileSync(process.env.OPL_WEBUI_SESSION_SECRET_FILE, 'utf8');
  const gateway = fs.readFileSync(process.env.OPL_GATEWAY_API_KEY_FILE, 'utf8');
  if (!password || !session || crypto.createHash('sha256').update(gateway).digest('hex') !== process.env.FIXTURE_GATEWAY_DIGEST) throw new Error('OPL Secret ABI mismatch');
}
const loginSession = profile ? crypto.createHmac('sha256', session).update('fixture-user:opl').digest('hex') : '';
const file = '/data/visits';
let visits = 0;
try { visits = parseInt(fs.readFileSync(file, 'utf8'), 10) || 0; } catch {}
visits += 1;
fs.writeFileSync(file, String(visits));
require('node:http').createServer((request, response) => {
  if (request.url === '/healthz') {
    response.writeHead(fs.existsSync('/data/ready') ? 200 : 503);
    response.end();
    return;
  }
  if (profile && request.url === '/login' && request.method === 'POST') {
    let body = ''; request.on('data', chunk => body += chunk); request.on('end', () => {
      const credentials = JSON.parse(body);
      if (credentials.username !== 'opl' || credentials.password !== password) { response.writeHead(401); response.end(); return; }
      response.writeHead(200, { 'set-cookie': 'fixture_session=' + loginSession + '; HttpOnly; SameSite=Strict; Path=/' }); response.end();
    }); return;
  }
  if (profile && request.headers.cookie !== 'fixture_session=' + loginSession) { response.writeHead(401); response.end(); return; }
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
	fixtureSuffix := stableSuffix(tag)[:12]
	registryName := "opl-fabric-application-registry-" + fixtureSuffix
	// This integration suite is serialized. A daemon-network loopback listener
	// makes both image push and image pull use the same actual Registry. Docker
	// Desktop's bridge host-port publishing exposes only the desktop-side socket
	// and is unreachable from its configured daemon image resolver/proxy.
	const registryContainer = "127.0.0.1:25557"
	registryRun := exec.CommandContext(ctx, "docker", "run", "-d", "--name", registryName, "--network", "host", "-e", "REGISTRY_HTTP_ADDR="+registryContainer, "-e", "REGISTRY_LOG_FORMATTER=json", "registry:2")
	if output, err := registryRun.CombinedOutput(); err != nil {
		t.Fatalf("start local registry: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "container", "rm", "-f", registryName).Run() })
	registryReadyBy := time.Now().Add(30 * time.Second)
	for {
		state, stateErr := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", registryName).Output()
		logs, logErr := exec.CommandContext(ctx, "docker", "logs", registryName).CombinedOutput()
		if stateErr != nil || strings.TrimSpace(string(state)) != "running" {
			t.Fatalf("test Registry listener %s failed: state=%s err=%v logs=%s", registryContainer, state, stateErr, logs)
		}
		if logErr != nil {
			t.Fatalf("read own Registry startup: %v", logErr)
		}
		ownListener := false
		decoder := json.NewDecoder(bytes.NewReader(logs))
		for {
			var record struct{ Level, Msg string }
			if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("decode own Registry startup: %v logs=%s", err, logs)
			}
			if record.Level == "fatal" || record.Level == "panic" {
				t.Fatalf("own Registry failed to bind %s: %s", registryContainer, record.Msg)
			}
			ownListener = ownListener || record.Msg == "listening on "+registryContainer
		}
		if ownListener && exec.CommandContext(ctx, "docker", "exec", registryName, "wget", "-q", "-O", "-", "http://"+registryContainer+"/v2/").Run() == nil {
			break
		}
		if time.Now().After(registryReadyBy) {
			t.Fatal("local registry never became ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
	repo := registryContainer + "/visit-counter-" + fixtureSuffix
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", repo+":e2e").Run() })
	if output, err := exec.CommandContext(ctx, "docker", "tag", tag, repo+":e2e").CombinedOutput(); err != nil {
		t.Fatalf("tag application image: %v: %s", err, output)
	}
	push := exec.CommandContext(ctx, "docker", "push", repo+":e2e")
	if output, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push application image: %v: %s", err, output)
	}
	digestOutput, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", repo+":e2e").Output()
	if err != nil {
		t.Fatal(err)
	}
	var repoDigests []string
	if err := json.Unmarshal(digestOutput, &repoDigests); err != nil {
		t.Fatal(err)
	}
	imageID := ""
	for _, digest := range repoDigests {
		if strings.HasPrefix(digest, repo+"@sha256:") {
			if imageID != "" {
				t.Fatalf("multiple digests for pushed repository %s", repo)
			}
			imageID = digest
		}
	}
	if imageID == "" {
		t.Fatalf("pushed repository %s has no digest: %s", repo, digestOutput)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", imageID).Run() })
	platformOutput, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", repo+":e2e").Output()
	if err != nil {
		t.Fatal(err)
	}
	applicationPlatform := strings.TrimSpace(string(platformOutput))

	alternateTag := tag + "-isolated"
	alternateRepo := registryContainer + "/isolated-counter-" + fixtureSuffix
	if output, err := exec.CommandContext(ctx, "docker", "build", "--quiet", "--build-arg", "APP_FLAVOR=isolated", "--file", filepath.Join(buildDir, "Dockerfile"), "--tag", alternateTag, buildDir).CombinedOutput(); err != nil {
		t.Fatalf("build isolated fixture: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", "-f", alternateTag, alternateRepo+":e2e").Run() })
	if output, err := exec.CommandContext(ctx, "docker", "tag", alternateTag, alternateRepo+":e2e").CombinedOutput(); err != nil {
		t.Fatalf("tag isolated fixture: %v: %s", err, output)
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", alternateRepo+":e2e").CombinedOutput(); err != nil {
		t.Fatalf("push isolated fixture: %v: %s", err, output)
	}
	alternateOutput, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", alternateRepo+":e2e").Output()
	if err != nil {
		t.Fatal(err)
	}
	var alternateDigests []string
	if err := json.Unmarshal(alternateOutput, &alternateDigests); err != nil {
		t.Fatal(err)
	}
	alternateImage := ""
	for _, digest := range alternateDigests {
		if strings.HasPrefix(digest, alternateRepo+"@sha256:") {
			if alternateImage != "" {
				t.Fatal("ambiguous alternate digest")
			}
			alternateImage = digest
		}
	}
	if alternateImage == "" || alternateImage == imageID {
		t.Fatal("two fixtures must have distinct immutable images")
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", alternateImage).Run() })
	// Drop build/publishing tags before execution. Runtime pulls only the exact
	// digest; there are no test-created aliases preventing retirement later.
	if output, err := exec.CommandContext(ctx, "docker", "image", "rm", tag, repo+":e2e", alternateTag, alternateRepo+":e2e").CombinedOutput(); err != nil {
		t.Fatalf("remove publication aliases: %v: %s", err, output)
	}
	launchID := "local-app-" + stableSuffix(t.Name(), time.Now().String())[:12]
	accountID, workspaceID := "acct-local", "ws-"+stableSuffix(launchID)[:10]
	runner := &execDockerRunner{binary: "docker"}
	storageRoot := localDockerStorageTestRoot(t)
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: storageRoot, RuntimeHost: "127.0.0.1",
		PublishHost:                  "127.0.0.1",
		ApplicationProbeImage:        base,
		StorageQuotaBackend:          localDockerStorageTestQuota(storageRoot),
		TrustedWorkspaceImageSources: []string{repo},
	}, runner)
	store := NewMemoryOperationStore()
	service := NewServiceWithOperationStore(provider, store)
	computeNetworkName := ""
	t.Cleanup(func() {
		if computeNetworkName != "" {
			_ = exec.Command("docker", "network", "rm", computeNetworkName).Run()
		}
	})
	t.Cleanup(func() {
		for _, key := range []string{launchID + ":app-1", launchID + ":app-2", launchID + ":app-3"} {
			for _, componentName := range []string{"main", "retrieval"} {
				name, nameErr := localDockerApplicationComponentName(key, componentName)
				if nameErr != nil {
					continue
				}
				_ = exec.Command("docker", "container", "rm", "-f", name).Run()
			}
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
	computeNetworkName = localDockerName("opl-compute", compute.Resources.ComputeAllocationID)
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
		SchemaVersion: 1, ApplicationID: "fixture-opl-app", Version: "1.0.0", Platform: applicationPlatform, RuntimeProfile: "opl_app", SecretInputs: []contracts.WorkspaceApplicationSecretInput{{Name: "gateway", Target: "/run/secrets/opl_gateway_api_key"}},
		Image:            imageID,
		Ports:            []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		ExposurePolicy:   "application", EntryPort: "http",
		HealthChecks: []contracts.WorkspaceApplicationHealthCheck{{Port: 8080, Path: "/healthz"}},
	}
	runtimeInput := WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, DataBindingID: "counter-data",
		AccountID: accountID, WorkspaceID: workspaceID, ComputeID: compute.Resources.ComputeAllocationID, VolumeID: storage.Resources.StorageID,
		AttachmentID: attachment.Resources.AttachmentID, AttachmentOperationID: attachment.Resources.AttachmentBindingRef,
		Revision:            revision,
		ConfigurationDigest: strings.Repeat("c", 64),
	}

	const fixtureGatewayKey = "integration-synthetic-workspace-gateway-key"
	gateway, err := service.UpsertGatewaySecret(ctx, GatewaySecretInput{AccountID: accountID, WorkspaceID: workspaceID, WorkspaceAPIKeyID: 7, GatewayAPIKey: fixtureGatewayKey, Fingerprint: "sha256:" + stableSuffix(fixtureGatewayKey), IdempotencyKey: launchID + ":gateway"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeInput.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: gateway.SecretRef, Version: gateway.Version, Key: "opl_gateway_api_key"}}
	runtimeInput.Configuration.Environment = map[string]string{"OPL_WEBUI_DEPLOYMENT_MODE": "cloud", "OPL_WEBUI_AUTH_MODE": "password", "OPL_WEBUI_USERNAME": "opl", "OPL_WEBUI_PASSWORD_FILE": "/run/secrets/opl_webui_password", "OPL_WEBUI_SESSION_SECRET_FILE": "/run/secrets/webui_session_secret", "OPL_GATEWAY_API_KEY_FILE": "/run/secrets/opl_gateway_api_key", "FIXTURE_GATEWAY_DIGEST": stableSuffix(fixtureGatewayKey)}
	runtimeInput.Configuration.CredentialVersion = "explicit-integration-credential-v1"
	runtimeInput.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(runtimeInput.Configuration, runtimeInput.SecretBindings, runtimeInput.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	ensure := func(key string) contracts.WorkspaceApplicationRuntimeObservation {
		t.Helper()
		copied := runtimeInput
		copied.IdempotencyKey, copied.RuntimeOperationID = key, key
		for {
			observation, ensureErr := service.CreateWorkspaceApplicationRuntime(ctx, copied)
			if ensureErr == nil && observation.Status == "ready" {
				return observation
			}
			if !errors.Is(ensureErr, ErrWorkspaceLaunchPending) {
				t.Fatalf("ensure %q: observation=%#v err=%v test_context=%v", key, observation, ensureErr, ctx.Err())
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	var establishedSession *http.Cookie
	authenticatedBody := func(input WorkspaceApplicationRuntimeInput, observation contracts.WorkspaceApplicationRuntimeObservation) string {
		t.Helper()
		credentials, err := service.ReadWorkspaceApplicationRuntimeCredentials(ctx, applicationLifecycleInput(input, "running", "credential-read"))
		if err != nil || credentials.WebUIUsername != "opl" {
			t.Fatalf("OPL credential ABI: %v", err)
		}
		anonymous, err := http.Get(observation.EntryURL)
		if err != nil {
			t.Fatal(err)
		}
		anonymous.Body.Close()
		if anonymous.StatusCode != http.StatusUnauthorized {
			t.Fatal("OPL profile entry must require its own login")
		}
		body, _ := json.Marshal(map[string]string{"username": credentials.WebUIUsername, "password": credentials.WebUIPassword})
		login, err := http.Post(observation.EntryURL+"login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		login.Body.Close()
		if login.StatusCode != http.StatusOK || len(login.Cookies()) != 1 {
			t.Fatal("OPL profile credential did not authenticate")
		}
		if establishedSession == nil {
			establishedSession = login.Cookies()[0]
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, observation.EntryURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Ordinary image replacement must retain the original session signing
		// secret: the cookie issued by the first generation still authorizes it.
		request.AddCookie(establishedSession)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatal("OPL profile session did not authorize entry")
		}
		return string(data)
	}
	firstInput := runtimeInput
	firstInput.IdempotencyKey = launchID + ":app-1"
	firstInput.RuntimeOperationID = firstInput.IdempotencyKey
	pending, pendingErr := service.CreateWorkspaceApplicationRuntime(ctx, firstInput)
	if !errors.Is(pendingErr, ErrWorkspaceLaunchPending) || pending.Status != "pending" || pending.EntryURL != "" {
		t.Fatalf("running HTTP server with failing declared health check must stay pending: observation=%#v err=%v", pending, pendingErr)
	}
	containerName, nameErr := localDockerApplicationComponentName(firstInput.RuntimeOperationID, "main")
	if nameErr != nil {
		t.Fatal(nameErr)
	}
	beforeID, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", containerName).Output()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(ctx, "docker", "exec", containerName, "node", "-e", "require('fs').writeFileSync('/data/ready', 'ready')").CombinedOutput(); err != nil {
		t.Fatalf("make declared health check ready: %v: %s", err, output)
	}
	first := ensure(firstInput.IdempotencyKey)
	afterID, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", containerName).Output()
	if err != nil || string(beforeID) != string(afterID) {
		t.Fatalf("pending replay recreated main: before=%s after=%s err=%v", beforeID, afterID, err)
	}

	if first.Status != "ready" || first.EntryURL == "" {
		t.Fatalf("first observation=%#v", first)
	}
	if err := waitForLocalRuntime(ctx, first.EntryURL+"healthz"); err != nil {
		t.Fatalf("entry not reachable: %v", err)
	}
	body := authenticatedBody(firstInput, first)
	if !strings.Contains(body, "visits: 1") {
		t.Fatalf("first visit body=%q", body)
	}
	t.Log("first OPL fixture is ready; protected entry login and session succeeded")

	// Suspend the first writer before the same application reuses its data.
	if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(firstInput, "suspended", launchID+":pause-1")); err != nil || result.State != "suspended" {
		t.Fatalf("suspend first writer: %v", err)
	}
	secondInput := firstInput
	secondInput.RuntimeOperationID = launchID + ":app-2"
	secondInput.IdempotencyKey = secondInput.RuntimeOperationID
	second := ensure(secondInput.RuntimeOperationID)
	if err := waitForLocalRuntime(ctx, second.EntryURL+"healthz"); err != nil {
		t.Fatal(err)
	}
	body = authenticatedBody(secondInput, second)
	if !strings.Contains(body, "visits: 2") {
		t.Fatalf("same-application data was not retained: %q", body)
	}
	firstCredentials, err := provider.ReadWorkspaceApplicationRuntimeCredentials(ctx, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	secondCredentials, err := provider.ReadWorkspaceApplicationRuntimeCredentials(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if firstCredentials.WebUIPassword != secondCredentials.WebUIPassword {
		t.Fatal("ordinary OPL image generation changed credentials")
	}
	t.Log("same-application replacement retained data, password and the original session")
	if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(firstInput, "absent", launchID+":retire-1")); err != nil || result.State != "absent" {
		t.Fatalf("retire predecessor: %v", err)
	}
	if _, exists, err := provider.inspectContainer(ctx, containerName); err != nil || exists {
		t.Fatalf("old OPL component remains: %v", err)
	}
	if result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(secondInput, "suspended", launchID+":pause-2")); err != nil || result.State != "suspended" {
		t.Fatalf("suspend second: %v", err)
	}
	// A different application receives neither OPL credentials nor OPL data.
	runtimeInput = secondInput
	runtimeInput.Revision = revision
	runtimeInput.Revision.ApplicationID = "isolated-counter"
	runtimeInput.Revision.RuntimeProfile = ""
	runtimeInput.Revision.SecretInputs = nil
	runtimeInput.Revision.Image = alternateImage
	runtimeInput.Configuration = contracts.WorkspaceApplicationRuntimeConfiguration{}
	runtimeInput.SecretBindings = nil
	runtimeInput.DataBindingID = "isolated-counter-data"
	runtimeInput.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(runtimeInput.Configuration, nil, runtimeInput.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	thirdInput := runtimeInput
	thirdInput.RuntimeOperationID = launchID + ":app-3"
	thirdInput.IdempotencyKey = thirdInput.RuntimeOperationID
	pending, pendingErr = service.CreateWorkspaceApplicationRuntime(ctx, thirdInput)
	if !errors.Is(pendingErr, ErrWorkspaceLaunchPending) || pending.Status != "pending" {
		t.Fatalf("fresh isolated data must not contain predecessor ready marker: %v", pendingErr)
	}
	thirdName, err := localDockerApplicationComponentName(thirdInput.RuntimeOperationID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(ctx, "docker", "exec", thirdName, "node", "-e", "const fs=require('fs');if(fs.existsSync('/run/secrets/opl_webui_password')||fs.existsSync('/run/secrets/opl_gateway_api_key')||fs.readFileSync('/data/visits','utf8')!=='1')process.exit(1);fs.writeFileSync('/data/ready','ready')").CombinedOutput(); err != nil {
		t.Fatalf("isolated app inherited data or credentials: %v: %s", err, output)
	}
	third := ensure(thirdInput.RuntimeOperationID)
	body = httpGetBody(t, third.EntryURL)
	if !strings.Contains(body, "visits: 1") {
		t.Fatalf("different-application data not isolated: %q", body)
	}
	t.Log("unrelated application is ready with isolated data and no OPL secrets")
	retired, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, applicationLifecycleInput(secondInput, "absent", launchID+":retire-2"))
	if err != nil || retired.State != "absent" {
		t.Fatalf("retire old OPL app: %v", err)
	}
	if len(retired.ImageRetirement) != 1 || (retired.ImageRetirement[0].State != "removed" && retired.ImageRetirement[0].State != "absent") {
		t.Fatalf("unreferenced image not retired: %#v", retired.ImageRetirement)
	}
	if output, err := exec.CommandContext(ctx, "docker", "image", "inspect", imageID).CombinedOutput(); err == nil || !strings.Contains(string(output), "No such image: "+imageID) {
		t.Fatalf("old image absence was not confirmed by exact inspect: %v: %s", err, output)
	}
	t.Log("old OPL component and its unreferenced image are absent")
	if output, err := exec.CommandContext(ctx, "docker", "exec", thirdName, "node", "-e", "require('fs').unlinkSync('/data/ready')").CombinedOutput(); err != nil {
		t.Fatalf("prepare declared health check for resume: %v: %s", err, output)
	}
	for _, state := range []string{"suspended", "running", "absent"} {
		lifecycle := applicationLifecycleInput(thirdInput, state, launchID+":third-"+state)
		result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle)
		if err != nil {
			t.Fatalf("third app %s: %v", state, err)
		}
		if state != "running" && result.State != state {
			t.Fatalf("third app state=%s want=%s", result.State, state)
		}
		if state == "running" {
			if result.State != "pending" || result.Observation.Status != "pending" || result.Observation.EntryURL != "" {
				t.Fatalf("resume published an entry before declared health passed: %#v", result)
			}
			if output, err := exec.CommandContext(ctx, "docker", "exec", thirdName, "node", "-e", "require('fs').writeFileSync('/data/ready','ready')").CombinedOutput(); err != nil {
				t.Fatalf("complete resumed application health: %v: %s", err, output)
			}
			for {
				result, err = service.ReadWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle)
				if err != nil {
					t.Fatalf("read resumed application: %v", err)
				}
				if result.State == "running" {
					break
				}
				if result.State != "pending" || result.Observation.Status != "pending" || result.Observation.EntryURL != "" {
					t.Fatalf("unexpected resumed application state: %#v", result)
				}
				select {
				case <-ctx.Done():
					t.Fatalf("resumed application never became ready: %v", ctx.Err())
				case <-time.After(200 * time.Millisecond):
				}
			}
			if result.Observation.Status != "ready" || result.Observation.EntryURL == "" {
				t.Fatalf("resumed application has no ready entry: %#v", result)
			}
			// Docker may allocate a new ephemeral host port when it starts the
			// container again. Only the current owner readback names its entry.
			httpGetBody(t, result.Observation.EntryURL+"healthz")
			finished, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, lifecycle)
			if err != nil || finished.State != "running" || finished.Observation.EntryURL != result.Observation.EntryURL {
				t.Fatalf("resume command did not converge to its current entry: result=%#v err=%v", finished, err)
			}
			t.Log("resumed application passed pending-to-ready readback and current-entry HTTP health")
		}
	}
	if err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, WorkspaceApplicationGatewaySecretCleanupInput{AccountID: accountID, WorkspaceID: workspaceID, SecretRef: gateway.SecretRef, IdempotencyKey: launchID + ":secret-cleanup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.CommandContext(ctx, "docker", "network", "inspect", localDockerName("opl-compute", firstInput.ComputeID)).Output(); err != nil {
		t.Fatalf("application cleanup removed compute: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(storageRoot, localDockerName("opl-workspace", workspaceID), "data", contracts.WorkspaceApplicationDataDirectory(firstInput.DataBindingID), "data", "visits")); err != nil || string(data) != "2" {
		t.Fatalf("old OPL data not preserved: err=%v", err)
	}
	if output, err := exec.CommandContext(ctx, "docker", "container", "ls", "-a", "--filter", "label=opl.fabric.kind=application_probe", "--filter", "label=opl.workspace.id="+workspaceID, "--format", "{{.ID}}").Output(); err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("application health probes leaked: %s err=%v", output, err)
	}
	t.Log("real Docker verified OPL profile credential/session/Gateway file ABI, same-application data retention, unrelated-application data/Secret isolation, stop/resume/delete and precise predecessor image retirement; workload is a qualification fixture, not upstream OPL App or IBD qualification")
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
