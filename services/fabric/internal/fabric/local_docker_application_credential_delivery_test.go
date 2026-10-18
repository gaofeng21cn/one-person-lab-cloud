package fabric

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// A Workspace application image may declare its own default user (the
// published OPL WebUI image runs as an unprivileged build user). The Local
// provider therefore has to hand the platform-issued credentials to the
// identity the component is actually scheduled as, and it must fail closed
// instead of starting an application that cannot read them: an application
// that cannot read its credentials exits, Fabric reports the component failed,
// and the deployment parks in manual review.
func TestLocalDockerApplicationCredentialDeliveryUsesWorkloadIdentity(t *testing.T) {
	// The fail-closed branch below is only reachable while the delivered files
	// cannot already carry the workload identity's group, which is the case for
	// every host user outside that group. A test process that already runs as
	// the workload identity would verify its own delivery and prove nothing, so
	// the case is skipped rather than forced onto another identity: the default
	// identity of an undeclared main component is exactly what this pins.
	if int64(os.Getegid()) == workspaceApplicationWorkloadIdentity {
		t.Skip("test process already runs as the OPL workload identity")
	}
	provider, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	revision := applicationRevisionForTest()
	revision.Credentials = []contracts.WorkspaceApplicationCredential{
		{Name: contracts.WorkspaceApplicationCredentialGatewayKey, Kind: contracts.WorkspaceApplicationCredentialGatewayKey, Target: "/run/secrets/opl_gateway_api_key"},
		{Name: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Kind: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Target: "/run/secrets/opl_webui_password", Username: "opl"},
		{Name: contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret, Kind: contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret, Target: "/run/secrets/webui_session_secret"},
	}
	input := applicationRuntimeInput("workload-identity-credentials", revision)
	// The revision declares no execution identity, exactly like the default OPL
	// application: the provider schedules the main component as the OPL workload
	// identity rather than as the image's own default user.
	gateway, err := provider.UpsertGatewaySecret(context.Background(), GatewaySecretInput{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, WorkspaceAPIKeyID: 7,
		GatewayAPIKey: "synthetic-workload-identity-key", Fingerprint: "sha256:" + stableSuffix("synthetic-workload-identity-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{
		Name: contracts.WorkspaceApplicationCredentialGatewayKey, SecretRef: gateway.SecretRef,
		Version: gateway.Version, Key: localDockerGatewayKeyFile,
	}}
	input.Configuration.CredentialVersion = "workload-identity-credential-version"

	identity := applicationComponentWorkloadIdentity(input.Revision, contracts.WorkspaceApplicationComponentMain)
	if identity == nil || identity.uid != workspaceApplicationWorkloadIdentity || identity.gid != workspaceApplicationWorkloadIdentity {
		t.Fatalf("undisclosed main component must run as the OPL workload identity: %#v", identity)
	}
	args, err := provider.applicationExecutionArgs(applicationComponentExecution(input.Revision, contracts.WorkspaceApplicationComponentMain), identity)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("--user %d:%d", workspaceApplicationWorkloadIdentity, workspaceApplicationWorkloadIdentity); strings.Join(args, " ") != want {
		t.Fatalf("main component execution args=%q, want %q", strings.Join(args, " "), want)
	}

	ownership, ok := provider.workloadOwnership.(*fakeLocalDockerWorkloadOwnership)
	if !ok {
		t.Fatalf("test provider ownership backend is %T", provider.workloadOwnership)
	}
	deliveryErr := provider.applicationCredentialFiles(input, true)

	// The delivery shape is asserted before the sentinel below, so a regression
	// that only changes the file mode reports the mode instead of hiding behind
	// the shared binding-conflict error.
	for name, info := range deliveredApplicationCredentialFiles(t, provider) {
		if info.Mode().Perm() != 0440 {
			t.Fatalf("credential %s delivered with mode %04o: only the group-readable 0440 form is readable by the workload identity", name, info.Mode().Perm())
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("credential %s has no stat owner", name)
		}
		if int64(stat.Gid) == identity.gid {
			t.Fatalf("credential %s already carries the workload identity group; the no-op ownership backend cannot exercise the fail-closed branch", name)
		}
	}

	// Ownership is requested for exactly the identity the component runs as.
	delivered := map[string]bool{}
	for _, call := range ownership.recorded() {
		if call.gid != workspaceApplicationWorkloadIdentity || call.uid != -1 {
			t.Fatalf("credential delivery assigned %+v", call)
		}
		for _, name := range []string{localDockerWebUIPasswordFile, localDockerWebUISessionSecretFile} {
			if strings.HasSuffix(call.path, "/"+name) {
				delivered[name] = true
			}
		}
	}
	for _, name := range []string{localDockerWebUIPasswordFile, localDockerWebUISessionSecretFile} {
		if !delivered[name] {
			t.Fatalf("credential %s was not handed to the workload identity's group", name)
		}
	}

	// A delivery the identity cannot verify must be refused instead of handed to
	// the application; the fixture backend reports success without adopting
	// ownership, which is exactly the environment this must fail closed in.
	if !errors.Is(deliveryErr, ErrLaunchStageBindingConflict) {
		t.Fatalf("unreadable credential delivery must fail closed, got %v", deliveryErr)
	}
}

// deliveredApplicationCredentialFiles returns the credential files the provider
// wrote for the current launch, staged or already published.
func deliveredApplicationCredentialFiles(t *testing.T, provider *LocalDockerProvider) map[string]os.FileInfo {
	t.Helper()
	files := map[string]os.FileInfo{}
	for _, name := range []string{localDockerWebUIPasswordFile, localDockerWebUISessionSecretFile} {
		matches, err := filepath.Glob(filepath.Join(provider.gatewaySecretRoot, "application-credentials", "*", name))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 {
			t.Fatalf("credential %s has %d delivered copies", name, len(matches))
		}
		info, err := os.Lstat(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		files[name] = info
	}
	return files
}
