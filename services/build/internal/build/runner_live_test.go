//go:build livebuild

package build

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// This opt-in test owns disposable loopback-only containers. It never contacts
// an instance, publishes to a public registry or uses customer resources.
func TestLivePackageBuildAndRestartReadback(t *testing.T) {
	if os.Getenv("OPL_BUILD_TEST_HANDOFF_ONLY") == "1" && strings.TrimSpace(os.Getenv("OPL_BUILD_TEST_HANDOFF")) == "" {
		t.Fatal("OPL_BUILD_TEST_HANDOFF_ONLY=1 requires OPL_BUILD_TEST_HANDOFF")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	root := t.TempDir()
	docker := func(args ...string) string {
		t.Helper()
		c := exec.CommandContext(ctx, "docker", args...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	port := func() string {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
		l.Close()
		return p
	}
	rp, bp := port(), port()
	suffix := fmt.Sprint(time.Now().UnixNano())
	regName := "opl-build-test-reg-" + suffix
	bkName := "opl-build-test-kit-" + suffix
	registry := "127.0.0.1:" + rp
	var preserveRegistry atomic.Bool
	fixturePrefix := registry + "/fixtures"
	// Build resolves each User's destination from the authenticated owner. The
	// default harness explicitly binds Users to this disposable registry. An
	// operator may instead name a real registry so the same owner chain writes to
	// it; the fixtures below stay on the loopback registry either way.
	if os.Getenv("OPL_BUILD_TEST_REGISTRY_HOST") == "" {
		t.Setenv("OPL_BUILD_TEST_REGISTRY_HOST", registry)
	}
	docker("run", "-d", "--name", regName, "-p", registry+":"+rp, "-p", "127.0.0.1:"+bp+":"+bp, "-e", "REGISTRY_HTTP_ADDR=0.0.0.0:"+rp, "registry:2")
	t.Cleanup(func() {
		if !preserveRegistry.Load() {
			_ = exec.Command("docker", "rm", "-fv", regName).Run()
		}
	})
	cfg := filepath.Join(root, "buildkit.toml")
	os.WriteFile(cfg, []byte(fmt.Sprintf("[registry.%q]\n  http = true\n", registry)), 0600)
	docker("run", "-d", "--privileged", "--name", bkName, "--network", "container:"+regName, "--mount", "type=bind,src="+cfg+",dst=/etc/buildkit/buildkitd.toml,readonly", "moby/buildkit:buildx-stable-1", "--addr", "tcp://0.0.0.0:"+bp)
	t.Cleanup(func() { exec.Command("docker", "rm", "-fv", bkName).Run() })
	dc := filepath.Join(root, "docker")
	if err := os.Mkdir(dc, 0700); err != nil {
		t.Fatal(err)
	}
	pluginOutput, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{json .ClientInfo.Plugins}}").Output()
	if err != nil {
		t.Fatalf("discover Docker CLI plugins: %v", err)
	}
	var plugins []struct {
		Name string `json:"Name"`
		Path string `json:"Path"`
	}
	if err := json.Unmarshal(pluginOutput, &plugins); err != nil {
		t.Fatalf("decode Docker CLI plugins: %v", err)
	}
	var buildxDir string
	for _, plugin := range plugins {
		if plugin.Name == "buildx" && plugin.Path != "" {
			buildxDir = filepath.Dir(plugin.Path)
			break
		}
	}
	if buildxDir == "" {
		t.Fatal("Docker Buildx CLI plugin is required")
	}
	isolatedConfig := struct {
		CLIPluginsExtraDirs []string                   `json:"cliPluginsExtraDirs"`
		Auths               map[string]json.RawMessage `json:"auths,omitempty"`
	}{CLIPluginsExtraDirs: []string{buildxDir}, Auths: map[string]json.RawMessage{}}
	// The default run is loopback-only and needs no credential. An operator may
	// opt in to a real output registry, which needs credential material in both
	// the client config and the isolated BuildKit daemon.
	realRegistryAuth, realRegistryHost := os.Getenv("OPL_BUILD_TEST_DOCKER_AUTH"), os.Getenv("OPL_BUILD_TEST_DOCKER_REGISTRY")
	if realRegistryAuth != "" && realRegistryHost != "" {
		isolatedConfig.Auths[realRegistryHost] = json.RawMessage(fmt.Sprintf(`{"auth":%q}`, realRegistryAuth))
	}
	isolatedDockerConfig, err := json.Marshal(isolatedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dc, "config.json"), isolatedDockerConfig, 0600); err != nil {
		t.Fatal(err)
	}
	// BuildKit resolves docker-image:// contexts and pushes inside the daemon, so a
	// real-registry run needs the same approved config there; the client-side
	// DockerConfig alone cannot authenticate the remote worker. The loopback
	// default needs no credential and keeps its previous startup.
	if realRegistryAuth != "" && realRegistryHost != "" {
		docker("exec", bkName, "mkdir", "-p", "/root/.docker")
		docker("cp", filepath.Join(dc, "config.json"), bkName+":/root/.docker/config.json")
		docker("restart", bkName)
	}
	bx := func(args ...string) string {
		t.Helper()
		return docker(append([]string{"--config", dc, "buildx"}, args...)...)
	}
	bx("create", "--name", "isolated", "--driver", "remote", "tcp://127.0.0.1:"+bp)
	bx("inspect", "isolated", "--bootstrap")
	frontend := os.Getenv("OPL_BUILD_TEST_FRONTEND")
	if frontend == "" {
		// Keep the opt-in integration test loopback-only. The Dockerfile frontend
		// is an immutable local fixture pushed into the disposable registry before
		// the remote BuildKit worker starts consuming it.
		frontendRepo := fixturePrefix + "/frontend"
		frontendTag := "fixture"
		docker("tag", "docker/dockerfile:1", frontendRepo+":"+frontendTag)
		pushed := docker("push", frontendRepo+":"+frontendTag)
		match := regexp.MustCompile(`(?:digest: )(?P<digest>sha256:[a-f0-9]{64})`).FindStringSubmatch(pushed)
		if len(match) != 2 {
			t.Fatal("local Dockerfile frontend push did not return an immutable digest")
		}
		frontend = frontendRepo + "@" + match[1]
	}
	frontRepo, frontDigest, ok := strings.Cut(frontend, "@")
	if !ok || !digestPattern.MatchString(frontDigest) {
		t.Fatal("digest-pinned frontend required")
	}
	targetPlatform := os.Getenv("OPL_BUILD_TEST_PLATFORM")
	if targetPlatform == "" {
		targetPlatform = "linux/arm64"
	}
	if strings.TrimSpace(os.Getenv("OPL_BUILD_TEST_HANDOFF")) != "" && strings.TrimSpace(os.Getenv("OPL_RUNTIME_IMAGE")) == "" {
		t.Fatal("OPL_BUILD_TEST_HANDOFF requires a digest-pinned runnable OPL_RUNTIME_IMAGE; the default scratch fixture cannot serve an Agent response")
	}
	platformParts := strings.Split(targetPlatform, "/")
	if len(platformParts) != 2 || platformParts[0] != "linux" || (platformParts[1] != "amd64" && platformParts[1] != "arm64") {
		t.Fatal("OPL_BUILD_TEST_PLATFORM must be linux/amd64 or linux/arm64")
	}
	architecture := api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_ARM64
	if platformParts[1] == "amd64" {
		architecture = api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64
	}
	p := &api.ImagePlatform{Os: api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX, Architecture: architecture}
	// Fixtures live on the loopback registry, which is reachable only over http.
	// The immutable output may be written to a real https registry instead, so the
	// transport is decided per host rather than by one global switch.
	outputNamespace := os.Getenv("OPL_BUILD_TEST_REGISTRY_NAMESPACE")
	if outputNamespace == "" {
		outputNamespace = "result"
	}
	r := &Runner{Builder: "isolated", RegistryPrefix: os.Getenv("OPL_BUILD_TEST_REGISTRY_HOST") + "/" + outputNamespace, DockerConfig: dc, WorkDir: root, MaxPackageBytes: 1 << 20, MaxExpandedBytes: 2 << 20, MaxFiles: 128, Timeout: 3 * time.Minute, AllowHTTP: true, InsecureHTTPHosts: []string{registry}, RegistryToken: os.Getenv("OPL_BUILD_TEST_REGISTRY_TOKEN")}
	seed := func(name, filename, body string) *api.ArtifactReference {
		t.Helper()
		d := filepath.Join(root, name)
		os.Mkdir(d, 0755)
		os.WriteFile(filepath.Join(d, filename), []byte(body), 0644)
		os.WriteFile(filepath.Join(d, "Dockerfile"), []byte("FROM scratch\nCOPY "+filename+" /"+filename+"\n"), 0644)
		job := "build_" + strings.Repeat("1", 32)
		bx("build", "--builder", "isolated", "--platform", "linux/arm64", "--provenance=false", "--sbom=false", "--tag", fixturePrefix+"/"+name+":"+job, "--push", d)
		m, e := r.ReadManifest(ctx, fixturePrefix+"/"+name, job, p)
		if e != nil {
			t.Fatal(e)
		}
		return &api.ArtifactReference{Repository: fixturePrefix + "/" + name, Digest: m.Digest, Platform: p}
	}
	var runtime *api.ArtifactReference
	var upstreamRuntime *api.ArtifactReference
	if ref := os.Getenv("OPL_RUNTIME_IMAGE"); ref != "" {
		repo, d, ok := strings.Cut(ref, "@")
		if !ok || !digestPattern.MatchString(d) {
			t.Fatal("OPL_RUNTIME_IMAGE must be digest-pinned")
		}
		runtime = &api.ArtifactReference{Repository: repo, Digest: d, Platform: p}
		upstreamRuntime = runtime
		if os.Getenv("OPL_BUILD_TEST_RUNTIME_LOCAL") == "1" {
			mirrorRepo := fixturePrefix + "/runtime"
			docker("tag", ref, mirrorRepo+":source")
			pushed := docker("push", mirrorRepo+":source")
			match := regexp.MustCompile(`(?:digest: )(sha256:[a-f0-9]{64})`).FindStringSubmatch(pushed)
			if len(match) != 2 || match[1] != d {
				t.Fatal("local Runtime mirror did not preserve the exact upstream manifest digest")
			}
			runtime = &api.ArtifactReference{Repository: mirrorRepo, Digest: d, Platform: p}
			t.Setenv("OPL_RUNTIME_IMAGE", mirrorRepo+"@"+d)
			t.Logf("RUNTIME_LOCAL_MIRROR source=%s mirror=%s@%s", ref, mirrorRepo, d)
		}
	} else {
		runtime = seed("runtime", "runtime.txt", "approved runtime\n")
	}
	webui := buildCloudAgentWebUI(t, docker, fixturePrefix, targetPlatform, p)
	schemaBytes, err := os.ReadFile("../../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	runtimeContract := &api.RuntimePublisherContract{}
	webuiContract := &api.WebuiPublisherContract{}
	runtimeContractBytes := schema.Examples[0]
	runtimeContractPath := strings.TrimSpace(os.Getenv("OPL_BUILD_TEST_RUNTIME_CONTRACT"))
	if runtimeContractPath != "" {
		runtimeContractBytes, err = os.ReadFile(runtimeContractPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = publicjson.Unmarshal(runtimeContractBytes, runtimeContract); err != nil {
		t.Fatal(err)
	}
	if err = publicjson.Unmarshal(schema.Examples[1], webuiContract); err != nil {
		t.Fatal(err)
	}
	if runtimeContractPath != "" && !proto.Equal(runtimeContract.Image, upstreamRuntime) {
		t.Fatal("Runtime contract does not bind the selected OPL_RUNTIME_IMAGE and platform")
	}
	runtimeContract.Image = runtime
	webuiContract.Image = webui
	runtimeContract.ApplicationRevisionTemplate.Image = runtime.Repository + "@" + runtime.Digest
	runtimeContract.ApplicationRevisionTemplate.Platform = targetPlatform
	recipeContract := runtimeContract.BuildRecipe
	recipeContract.Frontend = &api.ArtifactReference{Repository: frontRepo, Digest: frontDigest, Platform: p}
	recipeContract.OutputPlatform = p
	if runtimeContractPath == "" {
		packageSourceRoot := "src"
		if packagePath := os.Getenv("OPL_BUILD_TEST_PACKAGE"); packagePath != "" {
			packageSourceRoot = "candidate"
		}
		recipeContract.PackageInput.SourceRoot = packageSourceRoot
		recipeContract.PackageInput.TargetPath = "/agent"
		recipeContract.WebuiInput.SourcePath = "/web"
		recipeContract.WebuiInput.TargetPath = "/web"
	}
	recipeContract.Recipe.Repository = fixturePrefix + "/recipe"
	input := &api.BuildInputSnapshot{RuntimeVersionId: "runtime-live", WebuiVersionId: "webui-live", RuntimeArtifact: runtime, WebuiArtifact: webui, RuntimeContract: runtimeContract, WebuiContract: webuiContract, RuntimeContractReference: &api.PublisherContractReference{Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME}, WebuiContractReference: &api.PublisherContractReference{Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI}}
	var pkg []byte
	if packagePath := os.Getenv("OPL_BUILD_TEST_PACKAGE"); packagePath != "" {
		pkg, err = os.ReadFile(packagePath)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		pkg = packageZIP(t, zipEntry{"manifest.json", `{"name":"live-package"}`, 0644}, zipEntry{"src/payload.txt", "immutable package payload\n", 0644})
	}
	dsn := startLivePostgres(t, ctx)
	object, storageURL, storageToken, packageID, packageVersionID, packageVersionStatus, capability, capabilityAddr, identity := uploadLivePackage(t, ctx, dsn, pkg)
	input.PackageObject = object
	r.StorageURL = storageURL
	r.StorageToken = storageToken
	input.PackageId = packageID
	input.PackageVersionId = packageVersionID
	recipe := archiveEntry(t, "Dockerfile", tar.TypeReg, string(FixedDockerfile(input)))
	putBlob := func(repo string, b []byte) {
		t.Helper()
		q, _ := http.NewRequestWithContext(ctx, "POST", "http://"+repo[:strings.Index(repo, "/")]+"/v2/"+strings.SplitN(repo, "/", 2)[1]+"/blobs/uploads/", nil)
		resp, e := http.DefaultClient.Do(q)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 202 {
			t.Fatalf("blob start: %d", resp.StatusCode)
		}
		u, e := url.Parse(resp.Header.Get("Location"))
		if e != nil {
			t.Fatal(e)
		}
		values := u.Query()
		values.Set("digest", digest(b))
		u.RawQuery = values.Encode()
		q, _ = http.NewRequestWithContext(ctx, "PUT", u.String(), bytes.NewReader(b))
		q.Header.Set("Content-Type", "application/octet-stream")
		resp, e = http.DefaultClient.Do(q)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("blob upload: %d", resp.StatusCode)
		}
	}
	putBlob(input.RuntimeContract.BuildRecipe.Recipe.Repository, recipe)
	input.RuntimeContract.BuildRecipe.Recipe.Digest = digest(recipe)
	// The frozen identity is the canonical three-input encoding the Capability
	// owner also derives, so this live fixture carries a digest both owners can
	// re-derive from the persisted snapshot bytes.
	input.SnapshotDigest = digest(BuildInputDigestBytes(input))
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Logf("BUILD_INPUT_SNAPSHOT package_object=%s/%s package_sha256=%s runtime=%s@%s webui=%s@%s recipe=%s@%s snapshot=%s", input.PackageObject.StorageObjectId, input.PackageObject.VersionId, input.PackageObject.Sha256, input.RuntimeArtifact.Repository, input.RuntimeArtifact.Digest, input.WebuiArtifact.Repository, input.WebuiArtifact.Digest, input.RuntimeContract.BuildRecipe.Recipe.Repository, input.RuntimeContract.BuildRecipe.Recipe.Digest, input.SnapshotDigest)
	handoff := verifyOwnerChain(t, ctx, dsn, capability, capabilityAddr, r, input, identity)
	if path := strings.TrimSpace(os.Getenv("OPL_BUILD_TEST_HANDOFF")); path != "" {
		handoff.PackageVersionStatus = packageVersionStatus
		writeLiveBuildHandoff(t, path, registry, regName, handoff)
		preserveRegistry.Store(true)
		t.Logf("BUILD_HANDOFF path=%s registry=%s container=%s capability=%s artifact=%s@%s descriptor=%s", path, registry, regName, handoff.CapabilityVersion.GetId(), handoff.CapabilityVersion.GetArtifact().GetRepository(), handoff.CapabilityVersion.GetArtifact().GetDigest(), handoff.CapabilityVersion.GetDeploymentDescriptorDigest())
		if os.Getenv("OPL_BUILD_TEST_HANDOFF_ONLY") == "1" {
			return
		}
	}
	// Owner admission populated publisher and catalog identities in this fixture.
	// The independent Runner case freezes those final inputs for its own Build.
	input.SnapshotDigest = digest(BuildInputDigestBytes(input))
	jobID := "build_" + strings.Repeat("2", 32)
	binding, err := api.NewGatewayCoordinationClient(identityConn(t, identity.address, owneridentity.Build.Service())).ReadUserExternalIdentity(ctx, &api.UserExternalBindingReadRequest{Context: identity.call("runner-destination", "tenant-live"), ActorId: "101"})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := r.DestinationRepository(binding)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("executing production Runner against isolated BuildKit and registry")
	result := r.Execute(ctx, jobID, repository, input, func(message string) { t.Log(message) })
	if result.Err != nil {
		t.Fatalf("real BuildKit execution: started=%v: %v", result.Started, result.Err)
	}
	t.Logf("artifact=%s@%s input=%s", repository, result.Manifest.Digest, input.SnapshotDigest)
	// Inspect the real output filesystem, not only the manifest returned by the builder.
	export := filepath.Join(root, "export")
	os.Mkdir(export, 0755)
	// Export the composed inputs we assert below. Exporting the entire upstream
	// Runtime also copies its dependency tree through the host filesystem and can
	// exhaust the test deadline without adding evidence about these inputs.
	os.WriteFile(filepath.Join(export, "Dockerfile"), []byte("FROM "+repository+"@"+result.Manifest.Digest+" AS built\nFROM scratch\nCOPY --from=built "+recipeContract.PackageInput.TargetPath+" /package\nCOPY --from=built "+recipeContract.WebuiInput.TargetPath+" /web\n"), 0644)
	outDir := filepath.Join(root, "output")
	bx("build", "--builder", "isolated", "--platform", targetPlatform, "--output", "type=local,dest="+outDir, export)
	archive, e := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if e != nil {
		t.Fatal(e)
	}
	prefix := recipeContract.PackageInput.SourceRoot + "/"
	verifiedFiles := 0
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !strings.HasPrefix(file.Name, prefix) {
			continue
		}
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		want, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := os.ReadFile(filepath.Join(outDir, "package", filepath.FromSlash(strings.TrimPrefix(file.Name, prefix))))
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatalf("output Package file %s differs from uploaded bytes: %v", file.Name, err)
		}
		verifiedFiles++
	}
	if verifiedFiles == 0 {
		t.Fatal("selected Package source root contains no verified file")
	}
	html, e := os.ReadFile(filepath.Join(outDir, "web/index.html"))
	if e != nil || !bytes.Contains(html, []byte(`data-opl-cloud-agent-webui="v1"`)) || bytes.Contains(html, []byte("<h1>Cloud WebUI</h1>")) {
		t.Fatalf("output WebUI: %q %v", html, e)
	}
	// Stop the actual builder: all subsequent confirmations must be registry reads.
	docker("stop", bkName)
	reopened := *r
	reopened.Builder = "must-not-be-invoked"
	again, e := reopened.ReadManifest(ctx, repository, result.Manifest.Digest, p)
	if e != nil || again.Digest != result.Manifest.Digest {
		t.Fatalf("digest read after restart: %v", e)
	}
	// Drop the client's first manifest acknowledgement after the server supplied it.
	var lost atomic.Bool
	reopened.HTTP = &http.Client{Transport: roundTripFunc(func(q *http.Request) (*http.Response, error) {
		resp, e := http.DefaultTransport.RoundTrip(q)
		if e == nil && strings.Contains(q.URL.Path, "/manifests/") && lost.CompareAndSwap(false, true) {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("injected acknowledgement loss")
		}
		return resp, e
	})}
	if _, e = reopened.ReadManifest(ctx, repository, jobID, p); e == nil {
		t.Fatal("lost acknowledgement was hidden")
	}
	recovered, e := reopened.ReadManifest(ctx, repository, jobID, p)
	if e != nil || recovered.Digest != result.Manifest.Digest {
		t.Fatalf("lost-ack recovery: %v", e)
	}
	verifyPersistedRecovery(t, ctx, &reopened, input, repository, jobID, result.Manifest)
}

func TestLiveBuildHandoffRoundTrip(t *testing.T) {
	artifact := &api.BuildArtifactReadback{
		BuildJobId: "build-handoff", ArtifactReceiptId: "artifact-receipt",
		Outcome:             api.Observation_OBSERVATION_CONFIRMED,
		RegistrationContext: &api.CallContext{AcceptedOperationGrantId: proto.String("test-grant"), SessionId: proto.String("test-session")},
	}
	version := &api.CapabilityVersion{Id: "capability-handoff", Provenance: api.CapabilityVersionProvenanceEnum_CAPABILITY_VERSION_PROVENANCE_ENUM_BUILD, Status: api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY}
	path := filepath.Join(t.TempDir(), "build-handoff.json")
	writeLiveBuildHandoff(t, path, "127.0.0.1:5000", "test-registry", liveBuildHandoff{CapabilityVersion: version, BuildArtifact: artifact, PackageVersionStatus: "uploaded"})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion     int             `json:"schemaVersion"`
		CapabilityVersion json.RawMessage `json:"capabilityVersion"`
		BuildArtifact     json.RawMessage `json:"buildArtifact"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	gotArtifact, gotVersion := &api.BuildArtifactReadback{}, &api.CapabilityVersion{}
	if err := protojson.Unmarshal(payload.BuildArtifact, gotArtifact); err != nil {
		t.Fatal(err)
	}
	if gotArtifact.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatalf("handoff lost typed observation: %s", gotArtifact.GetOutcome())
	}
	if payload.SchemaVersion != 2 {
		t.Fatalf("handoff schema version = %d, want 2", payload.SchemaVersion)
	}
	if err := publicjson.Unmarshal(payload.CapabilityVersion, gotVersion); err != nil || !proto.Equal(version, gotVersion) {
		t.Fatalf("public CapabilityVersion roundtrip failed: %v", err)
	}
	wantArtifact := proto.Clone(artifact).(*api.BuildArtifactReadback)
	wantArtifact.RegistrationContext = nil
	if !proto.Equal(wantArtifact, gotArtifact) || bytes.Contains(raw, []byte("test-grant")) || bytes.Contains(raw, []byte("test-session")) {
		t.Fatal("handoff changed artifact facts or leaked registration authorization")
	}
	if artifact.GetRegistrationContext().GetAcceptedOperationGrantId() != "test-grant" || artifact.GetRegistrationContext().GetSessionId() != "test-session" {
		t.Fatal("handoff serialization mutated the source readback")
	}
}

func writeLiveBuildHandoff(t *testing.T, path, registry, registryContainer string, handoff liveBuildHandoff) {
	t.Helper()
	capabilityVersion, err := publicjson.Marshal(handoff.CapabilityVersion)
	if err != nil {
		t.Fatal(err)
	}
	// Internal observation belongs to the protobuf wire format. Registration
	// authorization is transient and must never cross this handoff boundary.
	readback := proto.Clone(handoff.BuildArtifact).(*api.BuildArtifactReadback)
	readback.RegistrationContext = nil
	artifact, err := protojson.Marshal(readback)
	if err != nil {
		t.Fatal(err)
	}
	payload := struct {
		SchemaVersion        int             `json:"schemaVersion"`
		RegistryHost         string          `json:"registryHost"`
		RegistryContainer    string          `json:"registryContainer"`
		CapabilityVersion    json.RawMessage `json:"capabilityVersion"`
		BuildArtifact        json.RawMessage `json:"buildArtifact"`
		PackageVersionStatus string          `json:"packageVersionStatus"`
	}{2, registry, registryContainer, capabilityVersion, artifact, handoff.PackageVersionStatus}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func buildCloudAgentWebUI(t *testing.T, docker func(...string) string, registry, targetPlatform string, platform *api.ImagePlatform) *api.ArtifactReference {
	t.Helper()
	if ref := os.Getenv("OPL_WEBUI_IMAGE"); ref != "" {
		repo, d, ok := strings.Cut(ref, "@")
		if !ok || !digestPattern.MatchString(d) {
			t.Fatal("OPL_WEBUI_IMAGE must be digest-pinned")
		}
		return &api.ArtifactReference{Repository: repo, Digest: d, Platform: platform}
	}
	repo := registry + "/cloud-agent-webui"
	tag := repo + ":source"
	webuiDir := filepath.Join("..", "..", "..", "..", "webui", "cloud-agent")
	docker("build", "--platform", targetPlatform, "--provenance=false", "--sbom=false", "--tag", tag, "-f", filepath.Join(webuiDir, "Dockerfile"), webuiDir)
	pushed := docker("push", tag)
	digestValue := regexp.MustCompile(`(?:digest: )(?P<digest>sha256:[a-f0-9]{64})`).FindStringSubmatch(pushed)
	if len(digestValue) != 2 {
		t.Fatalf("Cloud WebUI image push did not return an immutable digest: %s", pushed)
	}
	return &api.ArtifactReference{Repository: repo, Digest: digestValue[1], Platform: platform}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func verifyPersistedRecovery(t *testing.T, ctx context.Context, r *Runner, in *api.BuildInputSnapshot, repo, job string, m Manifest) {
	dsn := startLivePostgres(t, ctx)
	h, e := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "build", Database: "opl_build", SchemaOwnerRole: "opl_build_owner", WriterRole: "opl_build_writer", RuntimeRole: "opl_build_runtime"})
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close(context.Background())
	source, e := migrations.Source()
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); e != nil {
		t.Fatal(e)
	}
	db, e := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if e != nil {
		t.Fatal(e)
	}
	store, e := ownerstore.New(db, "build")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	call := &api.CallContext{ActorId: "publisher", RequestId: "live-replay"}
	_, e = store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: "op-live", TenantID: "tenant-live", ActorID: "publisher", Kind: "build", ResourceID: job, Stage: "building", RequestID: "live-replay", AcceptedInput: wire(in)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO build.build_jobs(id,tenant_id,package_version_id,runtime_version_id,webui_version_id,input_digest,input_snapshot,status,stage,request_id,created_by,operation_id,catalog_policy_id,call_context,executor_ref) VALUES($1,'tenant-live',$2,$3,$4,$5,$6,'building','building','live-replay','publisher','op-live','policy-live',$7,$8)`, job, in.PackageVersionId, in.RuntimeVersionId, in.WebuiVersionId, in.SnapshotDigest, wire(in), wire(call), repo+":"+job)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	// A fresh connection and Service observe the persisted pre-ack state.
	db.Close()
	db, e = h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	store, e = ownerstore.New(db, "build")
	if e != nil {
		t.Fatal(e)
	}
	var unavailable atomic.Bool
	unavailable.Store(true)
	originalHTTP := r.HTTP
	r.HTTP = &http.Client{Transport: roundTripFunc(func(q *http.Request) (*http.Response, error) {
		if unavailable.Load() {
			return nil, fmt.Errorf("injected registry read outage")
		}
		return originalHTTP.Transport.RoundTrip(q)
	})}
	service := &Service{store: store, runner: r}
	if e = service.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	uncertain, e := service.read(ctx, job)
	if e != nil {
		t.Fatal(e)
	}
	if uncertain.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatal("unavailable registry reported a terminal result")
	}
	var premature int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM build.build_artifacts`).Scan(&premature); e != nil || premature != 0 {
		t.Fatalf("unconfirmed artifact registered: %d %v", premature, e)
	}
	unavailable.Store(false)
	if e = service.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	rec, e := service.read(ctx, job)
	if e != nil {
		t.Fatal(e)
	}
	if rec.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_REGISTERING || rec.Job.GetArtifactDigest() != m.Digest {
		t.Fatalf("restart state: %v", rec.Job)
	}
	if e = service.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var artifacts, events int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM build.build_artifacts WHERE build_job_id=$1`, job).Scan(&artifacts); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM build.outbox_events WHERE aggregate_id=$1`, job).Scan(&events); e != nil {
		t.Fatal(e)
	}
	if artifacts != 1 || events != 1 {
		t.Fatalf("duplicate result: artifacts=%d events=%d", artifacts, events)
	}
	var descriptorRaw []byte
	var descriptorDigest string
	if e = db.QueryRowContext(ctx, `SELECT descriptor_bytes,deployment_descriptor_digest FROM build.build_artifacts WHERE build_job_id=$1`, job).Scan(&descriptorRaw, &descriptorDigest); e != nil {
		t.Fatal(e)
	}
	if digest(descriptorRaw) != descriptorDigest {
		t.Fatal("descriptor integrity mismatch")
	}
	var desc map[string]json.RawMessage
	if e = json.Unmarshal(descriptorRaw, &desc); e != nil {
		t.Fatal(e)
	}
	got := &api.ArtifactReference{}
	if e = publicjson.Unmarshal(desc["artifact"], got); e != nil {
		t.Fatal(e)
	}
	if got.Digest != m.Digest {
		t.Fatal("descriptor artifact identity mismatch")
	}
	if !proto.Equal(in.RuntimeArtifact, in.RuntimeContract.Image) {
		t.Fatal("input identity mutated")
	}
	t.Logf("persisted restart recovery: one artifact, one event, descriptor=%s; builder stopped", descriptorDigest)
}

func startLivePostgres(t *testing.T, ctx context.Context) string {
	pgName := "opl-build-test-pg-" + fmt.Sprint(time.Now().UnixNano())
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--name", pgName, "-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD=isolated-test-only", "postgres:16")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("postgres: %v %s", e, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-fv", pgName).Run() })
	portBytes, e := exec.Command("docker", "port", pgName, "5432").Output()
	if e != nil {
		t.Fatal(e)
	}
	dsn := "postgres://postgres:isolated-test-only@" + strings.TrimSpace(string(portBytes)) + "/postgres?sslmode=disable"
	for i := 0; i < 50; i++ {
		if exec.Command("docker", "exec", pgName, "pg_isready", "-h", "127.0.0.1", "-U", "postgres").Run() == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return dsn
}
