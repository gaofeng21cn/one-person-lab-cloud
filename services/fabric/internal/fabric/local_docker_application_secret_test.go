package fabric

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// Fixture emulates installation's pre-provisioned, immutable secret material.
func provisionApplicationSecretFixture(t *testing.T, p *LocalDockerProvider, input WorkspaceApplicationRuntimeInput, ref string, values map[string]string) (localDockerApplicationSecretMetadata, string) {
	t.Helper()
	metadata := localDockerApplicationSecretMetadata{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, SecretRef: ref, Keys: map[string]string{}}
	for key, value := range values {
		metadata.Keys[key] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
	}
	metadata.Version = localDockerApplicationSecretVersion(metadata)
	directory := filepath.Join(p.gatewaySecretRoot, "application-secrets", ref, "sha256-"+strings.TrimPrefix(metadata.Version, "sha256:"))
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if err := os.WriteFile(filepath.Join(directory, key), []byte(value), 0444); err != nil {
			t.Fatal(err)
		}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "metadata.json"), body, 0400); err != nil {
		t.Fatal(err)
	}
	return metadata, directory
}

func TestLocalDockerApplicationSecretsComponentScope(t *testing.T) {
	p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("scoped-secret", applicationRevisionForTest())
	first, firstPath := provisionApplicationSecretFixture(t, p, input, "database", map[string]string{"password": "database-secret"})
	second, secondPath := provisionApplicationSecretFixture(t, p, input, "search", map[string]string{"password": "search-secret"})
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{
		{Name: "db-password", SecretRef: first.SecretRef, Version: first.Version, Key: "password"},
		{Name: "search-password", SecretRef: second.SecretRef, Version: second.Version, Key: "password"},
	}
	input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "db-password", Target: "/run/secrets/database"}}
	input.Revision.Dependencies[0].SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "search-password", Env: "SEARCH_PASSWORD"}}
	files, metadata, err := p.applicationSecretFiles(input)
	if err != nil || len(files) != 1 || files["/run/secrets/database"] != filepath.Join(firstPath, "password") || metadata.SecretRef != "" {
		t.Fatalf("main files mismatch; err=%v", err)
	}
	args, err := p.applicationComponentSecretArgs(input, "main", input.Revision.SecretInputs)
	if err != nil || len(args) != 2 || strings.Contains(strings.Join(args, " "), secondPath) || strings.Contains(strings.Join(args, " "), "opl_webui") {
		t.Fatalf("main args leaked undeclared secret; err=%v", err)
	}
	args, err = p.applicationComponentSecretArgs(input, "retrieval", input.Revision.Dependencies[0].SecretInputs)
	if err != nil || len(args) != 2 || args[0] != "--env-file" {
		t.Fatalf("dependency args invalid; err=%v", err)
	}
	if strings.Contains(strings.Join(args, " "), "search-secret") || strings.Contains(strings.Join(args, " "), "database-secret") {
		t.Fatal("secret exposed in argv")
	}
	body, err := os.ReadFile(args[1])
	if err != nil || string(body) != "SEARCH_PASSWORD=search-secret\n" {
		t.Fatalf("envfile mismatch; err=%v", err)
	}
	info, err := os.Lstat(args[1])
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatalf("envfile mode mismatch; err=%v", err)
	}
	// Same binding may be explicitly declared by another component, as a file.
	declarations := []contracts.WorkspaceApplicationSecretInput{{Name: "db-password", Target: "/run/secrets/shared"}}
	args, err = p.applicationComponentSecretArgs(input, "retrieval", declarations)
	if err != nil || len(args) != 2 || !strings.Contains(args[1], firstPath) {
		t.Fatalf("declared shared input missing; err=%v", err)
	}
	// Main environment inputs use the same bounded consumer without OPL injection.
	input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "db-password", Env: "DB_PASSWORD"}}
	args, err = p.applicationSecretMountArgs(input)
	if err != nil || len(args) != 2 || args[0] != "--env-file" {
		t.Fatalf("main env input failed; err=%v", err)
	}
	var container dockerContainerInspect
	container.Config.Env = []string{"DB_PASSWORD=database-secret"}
	if err := p.verifyApplicationDeclaredSecrets(input, container, input.Revision.SecretInputs); err != nil {
		t.Fatal(err)
	}
	container.Config.Env = []string{"DB_PASSWORD=wrong"}
	if err := p.verifyApplicationDeclaredSecrets(input, container, input.Revision.SecretInputs); err == nil {
		t.Fatal("changed runtime secret accepted")
	}
}

func TestLocalDockerApplicationSecretsRejectUnboundMaterial(t *testing.T) {
	for _, mode := range []string{"account", "workspace", "version", "key", "ref-path", "key-path", "tamper", "unknown-metadata", "trailing-json", "symlink-file", "symlink-directory", "writable-file", "metadata-version", "missing-binding", "duplicate-binding", "env-newline"} {
		t.Run(mode, func(t *testing.T) {
			p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			input := applicationRuntimeInput("secret-rejection", applicationRevisionForTest())
			value := "fixture-secret"
			if mode == "env-newline" {
				value = "fixture\nsecret"
			}
			metadata, directory := provisionApplicationSecretFixture(t, p, input, "private-source", map[string]string{"password": value})
			binding := contracts.WorkspaceApplicationRuntimeSecretBinding{Name: "password", SecretRef: metadata.SecretRef, Version: metadata.Version, Key: "password"}
			declaration := contracts.WorkspaceApplicationSecretInput{Name: "password", Target: "/run/secrets/password"}
			switch mode {
			case "account":
				input.AccountID = "other-account"
			case "workspace":
				input.WorkspaceID = "other-workspace"
			case "version":
				binding.Version = "sha256:" + strings.Repeat("0", 64)
			case "key":
				binding.Key = "missing"
			case "ref-path":
				binding.SecretRef = "../private-source"
			case "key-path":
				binding.Key = "../password"
			case "tamper":
				if err := os.Chmod(filepath.Join(directory, "password"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "password"), []byte("tampered"), 0400); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(directory, "password"), 0400); err != nil {
					t.Fatal(err)
				}
			case "unknown-metadata", "trailing-json", "metadata-version":
				body, _ := json.Marshal(metadata)
				if mode == "unknown-metadata" {
					body = append(body[:len(body)-1], []byte(`,"unexpected":true}`)...)
				}
				if mode == "trailing-json" {
					body = append(body, []byte(` {}`)...)
				}
				if mode == "metadata-version" {
					metadata.Version = "sha256:" + strings.Repeat("1", 64)
					body, _ = json.Marshal(metadata)
				}
				path := filepath.Join(directory, "metadata.json")
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0400); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0400); err != nil {
					t.Fatal(err)
				}
			case "symlink-file":
				source := filepath.Join(directory, "password")
				moved := filepath.Join(directory, "retained")
				if err := os.Rename(source, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, source); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				moved := directory + "-retained"
				if err := os.Rename(directory, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, directory); err != nil {
					t.Fatal(err)
				}
			case "writable-file":
				if err := os.Chmod(filepath.Join(directory, "password"), 0600); err != nil {
					t.Fatal(err)
				}
			case "env-newline":
				declaration.Target = ""
				declaration.Env = "PASSWORD"
			}
			input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{binding}
			if mode == "missing-binding" {
				input.SecretBindings = nil
			}
			if mode == "duplicate-binding" {
				input.SecretBindings = append(input.SecretBindings, binding)
			}
			if _, _, err := p.applicationDeclaredSecrets(input, []contracts.WorkspaceApplicationSecretInput{declaration}); err == nil {
				t.Fatal("unbound secret accepted")
			}
		})
	}
}

func TestLocalDockerApplicationSecretsDependencyFileRuntime(t *testing.T) {
	p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("dependency-secret-runtime", applicationRevisionForTest())
	metadata, path := provisionApplicationSecretFixture(t, p, input, "retrieval-config", map[string]string{"config": "retrieval-private-config"})
	input.Revision.Dependencies[0].SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "retrieval-config", Target: "/run/secrets/config"}}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "retrieval-config", SecretRef: metadata.SecretRef, Version: metadata.Version, Key: "config"}}
	input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	observation, err := p.EnsureWorkspaceApplicationRuntime(context.Background(), input,
		ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
		StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil || observation.Status != "ready" {
		t.Fatalf("ensure failed; err=%v status=%s", err, observation.Status)
	}
	if strings.Contains(strings.Join(runner.runArgsForComponent("main"), " "), path) || !strings.Contains(strings.Join(runner.runArgsForComponent("retrieval"), " "), "source="+filepath.Join(path, "config")) {
		t.Fatal("component file scope violated")
	}
}

func TestLocalDockerApplicationSecretEnvCleanup(t *testing.T) {
	for _, tampered := range []bool{false, true} {
		t.Run(fmt.Sprint(tampered), func(t *testing.T) {
			p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			input := applicationRuntimeInput("secret-env-cleanup", applicationRevisionForTest())
			input.Revision.Dependencies[0].SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "password", Env: "PASSWORD"}}
			path, err := p.applicationSecretEnvironmentFile(input, "retrieval", map[string]string{"PASSWORD": "private"})
			if err != nil {
				t.Fatal(err)
			}
			if tampered {
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("modified"), 0400); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0400); err != nil {
					t.Fatal(err)
				}
			}
			err = p.removeApplicationSecretEnvFiles(input)
			if tampered {
				if err == nil {
					t.Fatal("changed generation removed")
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatal("changed file not retained")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("secret env retained; err=%v", err)
			}
			if err := p.removeApplicationSecretEnvFiles(input); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalDockerApplicationCredentialsMountOnlyDeclaredFiles(t *testing.T) {
	for _, kinds := range [][]string{
		{contracts.WorkspaceApplicationCredentialGatewayKey},
		{contracts.WorkspaceApplicationCredentialGatewayKey, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword},
		{contracts.WorkspaceApplicationCredentialGatewayKey, contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret},
		{contracts.WorkspaceApplicationCredentialGatewayKey, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret},
	} {
		t.Run(strings.Join(kinds, "+"), func(t *testing.T) {
			provider, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			input := applicationRuntimeInput("declared-credentials", applicationRevisionForTest())
			for _, kind := range kinds {
				credential := contracts.WorkspaceApplicationCredential{Name: kind, Kind: kind, Target: "/run/declared/" + kind}
				if kind == contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword {
					credential.Username = "opl"
				}
				input.Revision.Credentials = append(input.Revision.Credentials, credential)
			}
			const key = "synthetic-declared-credential-key"
			gateway, err := provider.UpsertGatewaySecret(context.Background(), GatewaySecretInput{
				AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, WorkspaceAPIKeyID: 7,
				GatewayAPIKey: key, Fingerprint: "sha256:" + stableSuffix(key),
			})
			if err != nil {
				t.Fatal(err)
			}
			input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{
				Name: contracts.WorkspaceApplicationCredentialGatewayKey, SecretRef: gateway.SecretRef,
				Version: gateway.Version, Key: localDockerGatewayKeyFile,
			}}
			input.Configuration.CredentialVersion = "declared-credential-version"
			if err := provider.applicationCredentialFiles(input, true); err != nil {
				t.Fatal(err)
			}
			files, _, err := provider.applicationSecretFiles(input)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != len(kinds) {
				t.Fatalf("mounted %d files for %d declared credentials", len(files), len(kinds))
			}
			for _, credential := range input.Revision.Credentials {
				source, ok := files[credential.Target]
				if !ok {
					t.Fatalf("declared credential target missing: %s", credential.Target)
				}
				if _, err := os.Stat(source); err != nil {
					t.Fatalf("declared credential file missing: %v", err)
				}
			}
		})
	}
}
