package contracts

import "testing"

func validWorkspaceApplicationRevision() WorkspaceApplicationRevision {
	return WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "chaokang-agent-ibd", Version: "20260909-verified",
		Platform: "linux/amd64", Image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:2fcfa6cd799ada43f6977621da9d7e2595a0b608c9207b9eaf06dd150f6fcf64",
		Ports:            []WorkspaceApplicationPort{{Name: "http", Port: 8082, Protocol: "TCP"}},
		HealthChecks:     []WorkspaceApplicationHealthCheck{{Port: 8082, Path: "/api/health"}},
		PersistentMounts: []WorkspaceApplicationMount{{Name: "knowledge", MountPath: "/data/knowledge"}},
		ExposurePolicy:   "anonymous",
	}
}

func TestValidateWorkspaceApplicationRevisionAcceptsIBDShape(t *testing.T) {
	if err := ValidateWorkspaceApplicationRevision(validWorkspaceApplicationRevision()); err != nil {
		t.Fatalf("expected valid revision, got %v", err)
	}
}

func TestValidateWorkspaceApplicationRevisionRejectsTagAndInvalidMount(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	revision.Image = "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd:latest"
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("expected tag reference to be rejected")
	}
	revision = validWorkspaceApplicationRevision()
	revision.PersistentMounts[0].MountPath = "/data/../knowledge"
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("expected traversal mount to be rejected")
	}
}

func TestValidateWorkspaceApplicationDeploymentPinsWorkspaceIntent(t *testing.T) {
	deployment := WorkspaceApplicationDeployment{
		SchemaVersion: 1, OperationID: "deploy-ws-a-1", WorkspaceID: "ws-a",
		ApplicationID: "chaokang-agent-ibd", TargetRevision: "20260909-verified",
		PreviousApplicationID: "opl-app", PreviousRevision: "v1",
		ConfigurationDigest: "sha256:config", SecretBindingVersions: []string{"secret:model:v1"},
		DataBindingIDs: []string{"data:knowledge:ws-a"}, ExpectedWorkspaceVersion: 4,
		IdempotencyKey: "deploy-intent-1",
	}
	if err := ValidateWorkspaceApplicationDeployment(deployment); err != nil {
		t.Fatalf("expected valid deployment, got %v", err)
	}
}

func TestValidateWorkspaceApplicationDeploymentRejectsDuplicateBindings(t *testing.T) {
	deployment := WorkspaceApplicationDeployment{
		SchemaVersion: 1, OperationID: "op", WorkspaceID: "ws-a", ApplicationID: "app",
		TargetRevision: "v1", ConfigurationDigest: "sha256:config",
		SecretBindingVersions: []string{"same"}, DataBindingIDs: []string{"same"},
		IdempotencyKey: "key",
	}
	if err := ValidateWorkspaceApplicationDeployment(deployment); err == nil {
		t.Fatal("expected duplicate binding to be rejected")
	}
}
