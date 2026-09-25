//go:build livebuild

package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	capabilitycatalog "opl-cloud/services/capability/catalog"
)

func verifyPublisherBrowser(t *testing.T, ctx context.Context, base string, service *Service, capability *capabilitycatalog.Service, runner *Runner, input *api.BuildInputSnapshot) {
	t.Helper()
	data, err := runner.packageBytes(ctx, input.PackageObject)
	if err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), "package.zip")
	if err := os.WriteFile(zip, data, 0600); err != nil {
		t.Fatal(err)
	}
	workerCtx := ctx
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := service.RunOnce(workerCtx); err != nil {
				done <- err
				return
			}
			if err := capability.DeliverRegistrations(workerCtx); err != nil {
				done <- err
				return
			}
			select {
			case <-stop:
				done <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	compile := exec.CommandContext(ctx, "npm", "run", "build")
	compile.Dir = "../../../.."
	compile.Env = append(os.Environ(), "VITE_CONSOLE_IDENTITY=cloud")
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("Console build: %v\n%s", err, out)
	}
	command := exec.CommandContext(ctx, "node", "tests/live/publisher-browser.ts")
	command.Dir = "../../../.."
	command.Env = append(os.Environ(), "OPL_PUBLISHER_BFF_URL="+base, "OPL_PUBLISHER_TEST_ZIP="+zip)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("publisher browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
