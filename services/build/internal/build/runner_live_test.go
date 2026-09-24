//go:build livebuild

package build

import (
	"archive/tar"
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publisherjson"
	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// This opt-in test owns disposable loopback-only containers. It never contacts
// an instance, publishes to a public registry or uses customer resources.
func TestLivePackageBuildAndRestartReadback(t *testing.T) {
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
	docker("run", "-d", "--name", regName, "-p", registry+":"+rp, "-p", "127.0.0.1:"+bp+":"+bp, "-e", "REGISTRY_HTTP_ADDR=0.0.0.0:"+rp, "registry:2")
	t.Cleanup(func() { exec.Command("docker", "rm", "-fv", regName).Run() })
	cfg := filepath.Join(root, "buildkit.toml")
	os.WriteFile(cfg, []byte(fmt.Sprintf("[registry.%q]\n  http = true\n", registry)), 0600)
	docker("run", "-d", "--privileged", "--name", bkName, "--network", "container:"+regName, "--mount", "type=bind,src="+cfg+",dst=/etc/buildkit/buildkitd.toml,readonly", "moby/buildkit:buildx-stable-1", "--addr", "tcp://0.0.0.0:"+bp)
	t.Cleanup(func() { exec.Command("docker", "rm", "-fv", bkName).Run() })
	dc := filepath.Join(root, "docker")
	os.Mkdir(dc, 0700)
	os.WriteFile(filepath.Join(dc, "config.json"), []byte(`{}`), 0600)
	bx := func(args ...string) string {
		t.Helper()
		return docker(append([]string{"--config", dc, "buildx"}, args...)...)
	}
	bx("create", "--name", "isolated", "--driver", "remote", "tcp://127.0.0.1:"+bp)
	bx("inspect", "isolated", "--bootstrap")
	frontend := os.Getenv("OPL_BUILD_TEST_FRONTEND")
	if frontend == "" {
		frontend = "docker.io/docker/dockerfile@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32"
	}
	frontRepo, frontDigest, ok := strings.Cut(frontend, "@")
	if !ok || !digestPattern.MatchString(frontDigest) {
		t.Fatal("digest-pinned frontend required")
	}
	p := &api.ImagePlatform{Os: api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX, Architecture: api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_ARM64}
	r := &Runner{Builder: "isolated", RegistryPrefix: registry + "/result", DockerConfig: dc, WorkDir: root, MaxPackageBytes: 1 << 20, MaxExpandedBytes: 2 << 20, MaxFiles: 20, Timeout: 3 * time.Minute, AllowHTTP: true}
	seed := func(name, filename, body string) *api.ArtifactReference {
		t.Helper()
		d := filepath.Join(root, name)
		os.Mkdir(d, 0755)
		os.WriteFile(filepath.Join(d, filename), []byte(body), 0644)
		os.WriteFile(filepath.Join(d, "Dockerfile"), []byte("FROM scratch\nCOPY "+filename+" /"+filename+"\n"), 0644)
		job := "build_" + strings.Repeat("1", 32)
		bx("build", "--builder", "isolated", "--platform", "linux/arm64", "--provenance=false", "--sbom=false", "--tag", registry+"/"+name+":"+job, "--push", d)
		m, e := r.ReadManifest(ctx, registry+"/"+name, job, p)
		if e != nil {
			t.Fatal(e)
		}
		return &api.ArtifactReference{Repository: registry + "/" + name, Digest: m.Digest, Platform: p}
	}
	runtime := seed("runtime", "runtime.txt", "approved runtime\n")
	webui := seed("webui", "index.html", "<h1>WebUI</h1>\n")
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
	if err = publisherjson.Unmarshal(schema.Examples[0], runtimeContract); err != nil {
		t.Fatal(err)
	}
	if err = publisherjson.Unmarshal(schema.Examples[1], webuiContract); err != nil {
		t.Fatal(err)
	}
	runtimeContract.Image = runtime
	webuiContract.Image = webui
	runtimeContract.ApplicationRevisionTemplate.Image = runtime.Repository + "@" + runtime.Digest
	runtimeContract.ApplicationRevisionTemplate.Platform = "linux/arm64"
	recipeContract := runtimeContract.BuildRecipe
	recipeContract.Frontend = &api.ArtifactReference{Repository: frontRepo, Digest: frontDigest, Platform: p}
	recipeContract.OutputPlatform = p
	recipeContract.PackageInput.SourceRoot = "src"
	recipeContract.PackageInput.TargetPath = "/agent"
	recipeContract.WebuiInput.SourcePath = "/index.html"
	recipeContract.WebuiInput.TargetPath = "/web/index.html"
	recipeContract.Recipe.Repository = registry + "/recipe"
	input := &api.BuildInputSnapshot{RuntimeVersionId: "runtime-live", WebuiVersionId: "webui-live", RuntimeArtifact: runtime, WebuiArtifact: webui, RuntimeContract: runtimeContract, WebuiContract: webuiContract, RuntimeContractReference: &api.PublisherContractReference{Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME}, WebuiContractReference: &api.PublisherContractReference{Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI}}
	pkg := packageZIP(t, zipEntry{"manifest.json", `{"name":"live-package"}`, 0644}, zipEntry{"src/payload.txt", "immutable package payload\n", 0644})
	dsn := startLivePostgres(t, ctx)
	object, storageURL, storageToken, packageID, packageVersionID, capability, capabilityAddr := uploadLivePackage(t, ctx, dsn, pkg)
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
	input.SnapshotDigest = digest(wire(input))
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	verifyOwnerChain(t, ctx, dsn, capability, capabilityAddr, r, input)
	jobID := "build_" + strings.Repeat("2", 32)
	repository := r.Repository("tenant-live", input.PackageId)
	t.Log("executing production Runner against isolated BuildKit and registry")
	result := r.Execute(ctx, jobID, repository, input, func(message string) { t.Log(message) })
	if result.Err != nil {
		t.Fatalf("real BuildKit execution: started=%v: %v", result.Started, result.Err)
	}
	t.Logf("artifact=%s@%s input=%s", repository, result.Manifest.Digest, input.SnapshotDigest)
	// Inspect the real output filesystem, not only the manifest returned by the builder.
	export := filepath.Join(root, "export")
	os.Mkdir(export, 0755)
	os.WriteFile(filepath.Join(export, "Dockerfile"), []byte("FROM "+repository+"@"+result.Manifest.Digest+"\n"), 0644)
	outDir := filepath.Join(root, "output")
	bx("build", "--builder", "isolated", "--platform", "linux/arm64", "--output", "type=local,dest="+outDir, export)
	payload, e := os.ReadFile(filepath.Join(outDir, "agent/payload.txt"))
	if e != nil || string(payload) != "immutable package payload\n" {
		t.Fatalf("output package: %q %v", payload, e)
	}
	html, e := os.ReadFile(filepath.Join(outDir, "web/index.html"))
	if e != nil || string(html) != "<h1>WebUI</h1>\n" {
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
	if e = publisherjson.Unmarshal(desc["artifact"], got); e != nil {
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
