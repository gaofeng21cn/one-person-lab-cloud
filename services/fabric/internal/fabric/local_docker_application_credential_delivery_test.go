package fabric

import (
	"context"
	"errors"
	"strings"
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
	if strings.Join(args, " ") != "--user 10001:10001" {
		t.Fatalf("main component execution args=%q", args)
	}

	ownership, ok := provider.workloadOwnership.(*fakeLocalDockerWorkloadOwnership)
	if !ok {
		t.Fatalf("test provider ownership backend is %T", provider.workloadOwnership)
	}
	// The fixture backend cannot adopt ownership, so delivery must fail closed:
	// handing the application files its identity cannot read is the defect this
	// guards, not an acceptable degraded mode.
	if err := provider.applicationCredentialFiles(input, true); !errors.Is(err, ErrLaunchStageBindingConflict) {
		t.Fatalf("unreadable credential delivery must fail closed, got %v", err)
	}
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
}
