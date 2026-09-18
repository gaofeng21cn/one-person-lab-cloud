package contracts

import (
	"strings"
	"testing"
)

func validWorkspaceApplicationRevision() WorkspaceApplicationRevision {
	return WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "chaokang-agent-ibd", Version: "20260909-verified",
		Platform: "linux/amd64", Image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:2fcfa6cd799ada43f6977621da9d7e2595a0b608c9207b9eaf06dd150f6fcf64",
		Ports:            []WorkspaceApplicationPort{{Name: "http", Port: 8082, Protocol: "TCP"}},
		EntryPort:        "http",
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

func TestValidateWorkspaceApplicationRevisionRejectsInvalidEntryAndProbe(t *testing.T) {
	for _, entry := range []string{"unknown", "dns"} {
		revision := validWorkspaceApplicationRevision()
		revision.Ports = append(revision.Ports, WorkspaceApplicationPort{Name: "dns", Port: 53, Protocol: "UDP"})
		revision.EntryPort = entry
		if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
			t.Fatalf("expected entry %q to be rejected", entry)
		}
	}
	revision := validWorkspaceApplicationRevision()
	revision.HealthChecks[0].InitialDelaySeconds = -1
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("expected negative probe delay to be rejected")
	}
}

func TestValidateWorkspaceApplicationRevisionRejectsConflictingComponentNames(t *testing.T) {
	for _, names := range [][]string{{"main"}, {"redis", "redis"}, {"Redis"}, {"redis-"}, {"../redis"}} {
		revision := validWorkspaceApplicationRevision()
		for _, name := range names {
			revision.Dependencies = append(revision.Dependencies, WorkspaceApplicationDependency{Name: name, Image: revision.Image})
		}
		if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
			t.Fatalf("expected invalid or conflicting component names %v to be rejected", names)
		}
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

func TestValidateWorkspaceApplicationComputeEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		compute WorkspaceApplicationCompute
		valid   bool
	}{
		{"undeclared", WorkspaceApplicationCompute{}, true},
		{"declared", WorkspaceApplicationCompute{CPURequestMilli: 500, CPULimitMilli: 2000, MemoryRequestBytes: 2 << 30, MemoryLimitBytes: 3 << 30}, true},
		{"request only", WorkspaceApplicationCompute{MemoryRequestBytes: 2 << 30}, true},
		{"negative", WorkspaceApplicationCompute{MemoryRequestBytes: -1}, false},
		{"cpu request above limit", WorkspaceApplicationCompute{CPURequestMilli: 2000, CPULimitMilli: 1000}, false},
		{"memory request above limit", WorkspaceApplicationCompute{MemoryRequestBytes: 3 << 30, MemoryLimitBytes: 2 << 30}, false},
		{"cpu above ceiling", WorkspaceApplicationCompute{CPULimitMilli: 128001}, false},
		{"memory above ceiling", WorkspaceApplicationCompute{MemoryLimitBytes: (1 << 40) + 1}, false},
	}
	for _, testCase := range cases {
		err := ValidateWorkspaceApplicationCompute(testCase.compute)
		if testCase.valid && err != nil {
			t.Fatalf("%s: expected valid envelope, got %v", testCase.name, err)
		}
		if !testCase.valid && err == nil {
			t.Fatalf("%s: expected rejection", testCase.name)
		}
	}
}

func TestValidateWorkspaceApplicationRejectsUnschedulableComponentEnvelope(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	revision.Compute = WorkspaceApplicationCompute{CPURequestMilli: 4000, CPULimitMilli: 1000}
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("expected an unschedulable main envelope to be rejected")
	}
	dependency := WorkspaceApplicationDependency{Name: "retrieval", Image: revision.Image, Compute: WorkspaceApplicationCompute{MemoryRequestBytes: -1}}
	if err := ValidateWorkspaceApplicationDependency(dependency); err == nil {
		t.Fatal("expected an unschedulable dependency envelope to be rejected")
	}
	if err := ValidateWorkspaceApplicationDependency(WorkspaceApplicationDependency{Name: "retrieval", Image: revision.Image, Compute: revision.Compute}); err == nil {
		t.Fatal("expected the dependency ceiling to apply as well")
	}
}

// A simple image declares only the facts it actually has. Nothing about ports,
// health checks, mounts or dependencies is required beyond what the exposure
// policy needs, and an image that needs none of them stays empty.
func TestValidateWorkspaceApplicationRevisionAcceptsMinimalSimpleImage(t *testing.T) {
	minimal := WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "simple-app", Version: "1.0.0", Platform: "linux/amd64",
		Image: "registry.example/simple@sha256:" + strings.Repeat("b", 64),
		Ports: []WorkspaceApplicationPort{{Name: "web", Port: 3000, Protocol: "TCP"}}, EntryPort: "web",
		ExposurePolicy: "application",
	}
	if err := ValidateWorkspaceApplicationRevision(minimal); err != nil {
		t.Fatalf("minimal simple image rejected: %v", err)
	}
	// An image with no published entry and no mounts is equally valid: the platform
	// must not require facts the image does not have.
	private := WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "batch-job", Version: "1", Platform: "linux/amd64",
		Image:          "registry.example/batch@sha256:" + strings.Repeat("c", 64),
		ExposurePolicy: "cloud_private",
	}
	if err := ValidateWorkspaceApplicationRevision(private); err != nil {
		t.Fatalf("private image with no entry or mounts rejected: %v", err)
	}
}

// A publishing exposure policy must name its entry port. Without it the platform
// proxy has no target, and guessing a default would publish a fabricated fact.
func TestValidateWorkspaceApplicationRevisionRequiresEntryPortForPublishedExposure(t *testing.T) {
	for _, policy := range []string{"anonymous", "application"} {
		t.Run(policy, func(t *testing.T) {
			revision := validWorkspaceApplicationRevision()
			revision.ExposurePolicy = policy
			revision.Ports, revision.EntryPort = nil, ""
			if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
				t.Fatal("published exposure without an entry port was accepted")
			}
			// Declaring a port the entry does not reference is equally unusable.
			revision.Ports = []WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}
			revision.EntryPort = ""
			if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
				t.Fatal("published exposure without a selected entry was accepted")
			}
			revision.EntryPort = "http"
			if err := ValidateWorkspaceApplicationRevision(revision); err != nil {
				t.Fatalf("declared entry rejected: %v", err)
			}
		})
	}
}
