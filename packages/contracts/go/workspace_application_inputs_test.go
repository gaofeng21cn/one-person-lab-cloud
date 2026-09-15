package contracts

import (
	"strings"
	"testing"
)

func TestApplicationInputBindingsCoverDeclaredComponents(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	revision.SecretInputs = []WorkspaceApplicationSecretInput{{Name: "shared", Env: "MAIN_TOKEN"}}
	revision.ConfigInputs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/etc/app/settings"}}
	revision.Dependencies = []WorkspaceApplicationDependency{{Name: "database", Image: revision.Image, SecretInputs: []WorkspaceApplicationSecretInput{{Name: "shared", Target: "/run/secrets/token"}, {Name: "database", Env: "DATABASE_PASSWORD"}}, ConfigInputs: []WorkspaceApplicationConfigInput{{Name: "bootstrap", Target: "/etc/database/bootstrap"}}}}
	config := WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"MODE": "test"}, Files: map[string]string{"settings": "mode=test\n", "bootstrap": "bootstrap\n"}}
	bindings := []WorkspaceApplicationRuntimeSecretBinding{{Name: "shared", SecretRef: "shared-ref", Version: "1", Key: "token"}, {Name: "database", SecretRef: "database-ref", Version: "2", Key: "password"}}
	input := WorkspaceApplicationRuntimeInput{SchemaVersion: 2, Revision: revision, Configuration: config, SecretBindings: bindings, DataBindingID: "data"}
	digest, err := WorkspaceApplicationConfigurationDigest(config, bindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	input.ConfigurationDigest = digest
	if err := ValidateWorkspaceApplicationRevision(revision); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		t.Fatal(err)
	}
	input.SecretBindings = input.SecretBindings[:1]
	input.ConfigurationDigest, _ = WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err == nil {
		t.Fatal("missing dependency Secret accepted")
	}
}

func TestApplicationFilesParticipateInConfigurationIdentity(t *testing.T) {
	config := WorkspaceApplicationRuntimeConfiguration{Files: map[string]string{"settings": "original"}}
	first, err := WorkspaceApplicationConfigurationDigest(config, nil, "data")
	if err != nil {
		t.Fatal(err)
	}
	config.Files["settings"] = "changed"
	second, err := WorkspaceApplicationConfigurationDigest(config, nil, "data")
	if err != nil || first == second {
		t.Fatal("configuration content must change identity")
	}
	for _, name := range []string{"../escape", "/absolute", ".", "bad/name"} {
		config.Files = map[string]string{name: "value"}
		if _, err := WorkspaceApplicationConfigurationDigest(config, nil, "data"); err == nil {
			t.Fatalf("unsafe file name %q accepted", name)
		}
	}
	config.Files = map[string]string{"settings": strings.Repeat("x", 256*1024+1)}
	if _, err := WorkspaceApplicationConfigurationDigest(config, nil, "data"); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestApplicationInputTargetsRejectAmbiguity(t *testing.T) {
	for _, secret := range []WorkspaceApplicationSecretInput{{Name: "s"}, {Name: "s", Env: "TOKEN", Target: "/run/secrets/s"}, {Name: "s", Target: "../s"}, {Name: "s", Target: "/run/../s"}, {Name: "s", Env: "INVALID=ENV"}} {
		if err := validateWorkspaceApplicationInputs([]WorkspaceApplicationSecretInput{secret}, nil, nil, nil); err == nil {
			t.Fatalf("ambiguous input accepted: %#v", secret)
		}
	}
	if err := validateWorkspaceApplicationInputs([]WorkspaceApplicationSecretInput{{Name: "s", Env: "TOKEN"}}, nil, map[string]string{"TOKEN": "plain"}, nil); err == nil {
		t.Fatal("plain env shadows secret")
	}
	if err := validateWorkspaceApplicationInputs([]WorkspaceApplicationSecretInput{{Name: "s", Target: "/etc/config"}}, []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/etc/config"}}, nil, nil); err == nil {
		t.Fatal("config shadows secret")
	}
}

func TestApplicationEnvironmentPreservesContainerSettingsNames(t *testing.T) {
	for _, name := range []string{"node.name", "xpack.security.enabled", "cluster.routing.allocation.disk.watermark.low", "ELASTIC_PASSWORD"} {
		if !ValidWorkspaceApplicationEnvironmentName(name) {
			t.Fatalf("valid container environment key %q rejected", name)
		}
	}
	for _, name := range []string{"", "1NAME", "NAME=value", "NAME\nINJECTED"} {
		if ValidWorkspaceApplicationEnvironmentName(name) {
			t.Fatalf("invalid name %q accepted", name)
		}
	}
	dependency := validDependency()
	dependency.Command.Env = map[string]string{"node.name": "search", "xpack.security.enabled": "true"}
	if err := ValidateWorkspaceApplicationDependency(dependency); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceApplicationConfigurationDigest(WorkspaceApplicationRuntimeConfiguration{Environment: dependency.Command.Env}, nil, "data"); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationInputMountNamespaceRejectsConflicts(t *testing.T) {
	for _, component := range []string{"main", "dependency"} {
		for _, kind := range []string{"config_exact", "secret_exact", "file_parent_of_mount", "file_parent_of_file", "file_child_of_file", "duplicate_mount", "nested_file_allowed"} {
			t.Run(component+"/"+kind, func(t *testing.T) {
				r := validWorkspaceApplicationRevision()
				persistent := []WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}}
				var scratch []WorkspaceApplicationMount
				var configs []WorkspaceApplicationConfigInput
				var secrets []WorkspaceApplicationSecretInput
				switch kind {
				case "config_exact":
					configs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/data"}}
				case "secret_exact":
					secrets = []WorkspaceApplicationSecretInput{{Name: "token", Target: "/data"}}
				case "file_parent_of_mount":
					persistent[0].MountPath = "/data/nested"
					configs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/data"}}
				case "file_parent_of_file":
					secrets = []WorkspaceApplicationSecretInput{{Name: "token", Target: "/etc/settings/token"}}
					configs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/etc/settings"}}
				case "file_child_of_file":
					secrets = []WorkspaceApplicationSecretInput{{Name: "token", Target: "/etc/settings"}}
					configs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/etc/settings/child"}}
				case "duplicate_mount":
					scratch = []WorkspaceApplicationMount{{Name: "scratch", MountPath: "/data"}}
				case "nested_file_allowed":
					configs = []WorkspaceApplicationConfigInput{{Name: "settings", Target: "/data/settings"}}
				}
				if component == "main" {
					r.PersistentMounts, r.ScratchMounts, r.ConfigInputs, r.SecretInputs = persistent, scratch, configs, secrets
				} else {
					r.Dependencies = []WorkspaceApplicationDependency{{Name: "database", Image: r.Image, PersistentMounts: persistent, ScratchMounts: scratch, ConfigInputs: configs, SecretInputs: secrets}}
				}
				err := ValidateWorkspaceApplicationRevision(r)
				if (err == nil) != (kind == "nested_file_allowed") {
					t.Fatalf("admission error=%v", err)
				}
			})
		}
	}
}
