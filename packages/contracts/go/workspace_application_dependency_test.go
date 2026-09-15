package contracts

import (
	"strings"
	"testing"
)

func validDependency() WorkspaceApplicationDependency {
	return WorkspaceApplicationDependency{
		Name:             "mysql",
		Image:            "registry.example/oplcloud/ibd-mysql@sha256:" + strings.Repeat("a", 64),
		Ports:            []WorkspaceApplicationDependencyPort{{Name: "mysql", Port: 3306, Protocol: "TCP"}},
		HealthChecks:     []WorkspaceApplicationDependencyHealthCheck{{Type: "tcp", Port: 3306, InitialDelaySeconds: 10}},
		PersistentMounts: []WorkspaceApplicationDependencyMount{{Name: "data", MountPath: "/var/lib/mysql"}},
		Command: WorkspaceApplicationDependencyCommand{
			Args: []string{"--max_connections=1000", "--character-set-server=utf8mb4"},
			Env:  map[string]string{"MYSQL_ROOT_PASSWORD": "managed-by-secret"},
		},
		SecretInputs: []WorkspaceApplicationSecretInput{{Name: "mysql", Target: "/run/secrets/mysql_root_password"}},
	}
}

func TestValidateWorkspaceApplicationDependencyAcceptsFullSpec(t *testing.T) {
	if err := ValidateWorkspaceApplicationDependency(validDependency()); err != nil {
		t.Fatalf("full dependency spec rejected: %v", err)
	}
}

func TestValidateWorkspaceApplicationDependencyRejections(t *testing.T) {
	cases := map[string]func(*WorkspaceApplicationDependency){
		"bad name":      func(d *WorkspaceApplicationDependency) { d.Name = "MySQL" },
		"bad image":     func(d *WorkspaceApplicationDependency) { d.Image = "registry.example/mysql:8" },
		"bad port name": func(d *WorkspaceApplicationDependency) { d.Ports[0].Name = "MySQL" },
		"port conflict": func(d *WorkspaceApplicationDependency) {
			d.Ports = append(d.Ports, WorkspaceApplicationDependencyPort{Name: "x", Port: 3306, Protocol: "TCP"})
		},
		"bad protocol":   func(d *WorkspaceApplicationDependency) { d.Ports[0].Protocol = "HTTP" },
		"bad check type": func(d *WorkspaceApplicationDependency) { d.HealthChecks[0].Type = "icmp" },
		"tcp with path":  func(d *WorkspaceApplicationDependency) { d.HealthChecks[0].Path = "/health" },
		"http no path": func(d *WorkspaceApplicationDependency) {
			d.HealthChecks[0].Type, d.HealthChecks[0].Path = "http", ""
		},
		"relative mount": func(d *WorkspaceApplicationDependency) { d.PersistentMounts[0].MountPath = "var/lib" },
		"dup mount": func(d *WorkspaceApplicationDependency) {
			d.PersistentMounts = append(d.PersistentMounts, WorkspaceApplicationDependencyMount{Name: "data", MountPath: "/other"})
		},
		"bad env name": func(d *WorkspaceApplicationDependency) { d.Command.Env["1BAD"] = "x" },
		"dup secret": func(d *WorkspaceApplicationDependency) {
			d.SecretInputs = append(d.SecretInputs, WorkspaceApplicationSecretInput{Name: "mysql", Target: "/x"})
		},
	}
	for name, mutate := range cases {
		dependency := validDependency()
		mutate(&dependency)
		if err := ValidateWorkspaceApplicationDependency(dependency); err == nil {
			t.Fatalf("%s must not validate", name)
		}
	}
}

func TestRevisionValidationCoversDependencySpec(t *testing.T) {
	revision := WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "chaokang-agent-ibd", Version: "20260909",
		Platform:  "linux/amd64",
		Image:     "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:" + strings.Repeat("2", 64),
		Ports:     []WorkspaceApplicationPort{{Name: "webui", Port: 8082, Protocol: "TCP"}},
		EntryPort: "webui", ExposurePolicy: "application",
	}
	dependency := validDependency()
	dependency.Image = "uswccr.ccs.tencentyun.com/oplcloud/ibd-mysql@sha256:" + strings.Repeat("e", 64)
	revision.Dependencies = []WorkspaceApplicationDependency{dependency}
	if err := ValidateWorkspaceApplicationRevision(revision); err != nil {
		t.Fatalf("revision with full dependency spec rejected: %v", err)
	}
	broken := validDependency()
	broken.HealthChecks[0].Type = "icmp"
	revision.Dependencies = []WorkspaceApplicationDependency{broken}
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("broken dependency health check must fail the whole revision")
	}
}
