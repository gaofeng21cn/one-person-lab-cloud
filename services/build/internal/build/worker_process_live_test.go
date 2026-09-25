//go:build livebuild

package build

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	capabilitycatalog "opl-cloud/services/capability/catalog"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

type workerProcessConfig struct {
	DSN, CapabilityAddress, IdentityAddress string
	Runner                                  *Runner
}

// This entrypoint runs the actual worker in a different OS process. Its config
// contains only disposable local test credentials and is kept in a private temp
// directory. No production test hook or manually inserted Job state is used.
func TestLiveWorkerProcess(t *testing.T) {
	path := os.Getenv("OPL_LIVE_WORKER_CONFIG")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config workerProcessConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", config.DSN)
	if err != nil {
		t.Fatal("open isolated worker database")
	}
	defer db.Close()
	store, err := ownerstore.New(db, "build")
	if err != nil {
		t.Fatal(err)
	}
	conn := liveConn(t, config.CapabilityAddress, owneridentity.Build.Service())
	service, err := New(store, ownerservice.NewAuthorizer(ownerservice.OwnerBuild, api.NewCloudIdentityAuthorizationClient(identityConn(t, config.IdentityAddress, owneridentity.Build.Service()))), api.NewCapabilityCoordinationClient(conn), api.NewCapabilityProductServiceClient(conn), nil, api.NewCloudIdentityAuthorizationClient(identityConn(t, config.IdentityAddress, owneridentity.Build.Service())), config.Runner)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
}

func verifyInterruptedWorker(t *testing.T, ctx context.Context, dsn, capAddr string, client *publisherBuildClient, service *Service, capability *capabilitycatalog.Service, runner *Runner, original *api.CreateBuildRpcRequest) {
	t.Helper()
	req := proto.Clone(original).(*api.CreateBuildRpcRequest)
	req.Context.IdempotencyKey = "interrupt-real-worker"
	req.Context.RequestId = "interrupt-real-worker"
	job, err := client.CreateBuild(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	marker := filepath.Join(root, "export-committed")
	realDocker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	// The real exporter pushes all bytes, then this test-only command wrapper
	// withholds its exit acknowledgement. The worker stays inside cmd.Wait until
	// SIGKILL. This precisely exercises loss of the worker's push acknowledgement;
	// it does not pretend to interrupt a layer upload or the Registry process.
	shim := fmt.Sprintf("#!/bin/sh\n%q \"$@\"\nresult=$?\nif [ \"$result\" -eq 0 ]; then\n  printf committed > %q\n  /bin/sleep 120\nfi\nexit \"$result\"\n", realDocker, marker)
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "worker.json")
	writeConfig := func(r *Runner) {
		t.Helper()
		b, err := json.Marshal(workerProcessConfig{DSN: dsn, CapabilityAddress: capAddr, IdentityAddress: client.identity.address, Runner: r})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(runner)
	start := func(shim bool) (*exec.Cmd, <-chan error) {
		t.Helper()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLiveWorkerProcess$", "-test.v")
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		command.Env = append(os.Environ(), "OPL_LIVE_WORKER_CONFIG="+configPath)
		if shim {
			command.Env = append(command.Env, "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		log, err := os.Create(filepath.Join(root, fmt.Sprintf("worker-%t.log", shim)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { log.Close() })
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		t.Cleanup(func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
		return command, done
	}
	worker, done := start(true)
	for deadline := time.Now().Add(time.Minute); ; {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-done:
			b, _ := os.ReadFile(filepath.Join(root, "worker-true.log"))
			t.Fatalf("worker exited before exporter boundary: %v\n%s", err, b)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("real exporter did not reach acknowledgement boundary")
		}
		time.Sleep(50 * time.Millisecond)
	}
	rec, err := service.read(ctx, job.Id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_BUILDING {
		t.Fatalf("not awaiting exporter acknowledgement: %s", rec.Job.Status)
	}
	repository := runner.Repository("tenant-live", rec.Input.PackageId)
	manifest, err := runner.ReadManifest(ctx, repository, job.Id, rec.Input.RuntimeArtifact.Platform)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-worker.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("worker was not killed")
	}
	var premature int
	if err := service.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM build.build_artifacts WHERE build_job_id=$1`, job.Id).Scan(&premature); err != nil || premature != 0 {
		t.Fatalf("premature artifact: %d %v", premature, err)
	}
	recoveryRunner := *runner
	recoveryRunner.Builder = "must-not-start-another-exporter"
	writeConfig(&recoveryRunner)
	_, recovered := start(false)
	if err := <-recovered; err != nil {
		t.Fatal("restarted worker could not reconcile committed image")
	}
	for n := 0; n < 3; n++ {
		if err := service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if err := capability.DeliverRegistrations(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rec, err = service.read(ctx, job.Id)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts, versions, events int
	if err := service.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM build.build_artifacts WHERE build_job_id=$1`, job.Id).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if err := service.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM build.outbox_events WHERE aggregate_id=$1 AND event_type='build.artifact_confirmed.v1'`, job.Id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := capability.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.capability_versions WHERE build_job_id=$1`, job.Id).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if rec.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_SUCCEEDED || rec.Job.GetArtifactDigest() != manifest.Digest || artifacts != 1 || versions != 1 || events != 1 {
		t.Fatalf("process recovery did not converge: %s, artifacts=%d versions=%d events=%d", rec.Job.Status, artifacts, versions, events)
	}
	t.Logf("actual worker SIGKILL after Registry commit and before exporter acknowledgement: fresh worker recovered %s, one artifact/event/version, no exporter rerun", manifest.Digest)
}
