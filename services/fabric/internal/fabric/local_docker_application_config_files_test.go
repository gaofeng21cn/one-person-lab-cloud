package fabric

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestLocalDockerApplicationConfigFilesAreBoundAndComponentScoped(t *testing.T) {
	p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("configs", applicationRevisionForTest())
	input.Revision.ConfigInputs = []contracts.WorkspaceApplicationConfigInput{{Name: "main-config", Target: "/etc/main/config"}}
	input.Revision.Dependencies[0].ConfigInputs = []contracts.WorkspaceApplicationConfigInput{{Name: "dependency-config", Target: "/etc/dependency/config"}}
	input.Configuration.Files = map[string]string{"main-config": "main-value", "dependency-config": "dependency-value"}
	var err error
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.applicationConfigFiles(input, true); err != nil {
		t.Fatal(err)
	}
	if err = p.applicationConfigFiles(input, true); err != nil {
		t.Fatal("exact replay", err)
	}
	args := strings.Join(p.applicationConfigMountArgs(input, "main"), " ")
	if !strings.Contains(args, "target=/etc/main/config,readonly") || strings.Contains(args, "/etc/dependency/config") {
		t.Fatal("component config leak")
	}
	root := filepath.Join(p.gatewaySecretRoot, "application-configurations", applicationRuntimeID(input))
	body, err := os.ReadFile(filepath.Join(root, applicationConfigFilename("dependency-config")))
	if err != nil || string(body) != "dependency-value" {
		t.Fatal("file mismatch", err)
	}
	input.Configuration.Files["main-config"] = "changed"
	input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err = p.applicationConfigFiles(input, true); err == nil {
		t.Fatal("generation config drift accepted")
	}
	if err = p.removeApplicationConfigFiles(input); err == nil {
		t.Fatal("unowned generation removed")
	}
	input.Configuration.Files["main-config"] = "main-value"
	input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err = p.removeApplicationConfigFiles(input); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("configuration directory remains")
	}
}

func TestLocalDockerApplicationConfigFilesRejectTamper(t *testing.T) {
	for _, attack := range []string{"content", "symlink", "unexpected-file"} {
		t.Run(attack, func(t *testing.T) {
			p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			input := applicationRuntimeInput("config-tamper", applicationRevisionForTest())
			input.Revision.ConfigInputs = []contracts.WorkspaceApplicationConfigInput{{Name: "config", Target: "/etc/config"}}
			input.Configuration.Files = map[string]string{"config": "expected"}
			input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
			if err := p.applicationConfigFiles(input, true); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(p.gatewaySecretRoot, "application-configurations", applicationRuntimeID(input))
			file := filepath.Join(dir, applicationConfigFilename("config"))
			switch attack {
			case "content":
				if err := os.Chmod(file, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("changed"), 0444); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "owner.json"), file); err != nil {
					t.Fatal(err)
				}
			case "unexpected-file":
				if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("x"), 0444); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.applicationConfigFiles(input, false); err == nil {
				t.Fatal("tamper accepted")
			}
			if err := p.removeApplicationConfigFiles(input); err == nil {
				t.Fatal("tampered tree removed")
			}
		})
	}
}
