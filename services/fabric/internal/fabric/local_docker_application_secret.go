package fabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

var localDockerApplicationSecretName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,252}$`)

// Installation owns this immutable store; Fabric only consumes exact versions.
// Version hashes the identity and key digests, excluding the Version itself.
type localDockerApplicationSecretMetadata struct {
	AccountID   string            `json:"accountId"`
	WorkspaceID string            `json:"workspaceId"`
	SecretRef   string            `json:"secretRef"`
	Version     string            `json:"version"`
	Keys        map[string]string `json:"keys"`
}

func localDockerApplicationSecretVersion(metadata localDockerApplicationSecretMetadata) string {
	identity := struct {
		AccountID   string            `json:"accountId"`
		WorkspaceID string            `json:"workspaceId"`
		SecretRef   string            `json:"secretRef"`
		Keys        map[string]string `json:"keys"`
	}{metadata.AccountID, metadata.WorkspaceID, metadata.SecretRef, metadata.Keys}
	body, _ := json.Marshal(identity)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(body))
}

func localDockerApplicationSecretPath(root *os.Root, path string, directory bool) error {
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return ErrLaunchStageBindingConflict
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 || directory) && !info.IsDir() {
			return ErrLaunchStageBindingConflict
		}
		if i == len(parts)-1 && !directory && (!info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0) {
			return ErrLaunchStageBindingConflict
		}
	}
	return nil
}

func (p *LocalDockerProvider) applicationGenericSecret(input WorkspaceApplicationRuntimeInput, binding contracts.WorkspaceApplicationRuntimeSecretBinding) (string, []byte, error) {
	if !localDockerApplicationSecretName.MatchString(binding.SecretRef) || !localDockerApplicationSecretName.MatchString(binding.Key) || binding.Key == "metadata.json" || !strings.HasPrefix(binding.Version, "sha256:") {
		return "", nil, ErrLaunchStageBindingConflict
	}
	digest := strings.TrimPrefix(binding.Version, "sha256:")
	version, err := localDockerGatewayVersionDir(digest)
	if err != nil {
		return "", nil, ErrLaunchStageBindingConflict
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	directory := "application-secrets/" + binding.SecretRef + "/" + version
	if err := localDockerApplicationSecretPath(root, directory+"/metadata.json", false); err != nil {
		return "", nil, err
	}
	body, err := root.ReadFile(directory + "/metadata.json")
	if err != nil {
		return "", nil, ErrLaunchStageBindingConflict
	}
	var metadata localDockerApplicationSecretMetadata
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return "", nil, ErrLaunchStageBindingConflict
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", nil, ErrLaunchStageBindingConflict
	}
	if metadata.AccountID != input.AccountID || metadata.WorkspaceID != input.WorkspaceID || metadata.SecretRef != binding.SecretRef || metadata.Version != binding.Version || localDockerApplicationSecretVersion(metadata) != binding.Version || len(metadata.Keys) == 0 {
		return "", nil, ErrLaunchStageBindingConflict
	}
	for key, digest := range metadata.Keys {
		if !localDockerApplicationSecretName.MatchString(key) || !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 || !validDigest(strings.TrimPrefix(digest, "sha256:")) {
			return "", nil, ErrLaunchStageBindingConflict
		}
	}
	expected, ok := metadata.Keys[binding.Key]
	if !ok {
		return "", nil, ErrLaunchStageBindingConflict
	}
	path := directory + "/" + binding.Key
	if err := localDockerApplicationSecretPath(root, path, false); err != nil {
		return "", nil, err
	}
	value, err := root.ReadFile(path)
	if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(value)) != expected {
		return "", nil, ErrLaunchStageBindingConflict
	}
	return filepath.Join(p.gatewaySecretRoot, path), value, nil
}

// Only the component's declared inputs are resolved. A binding shared by name
// does not authorize exposing it to other components without a declaration.
func (p *LocalDockerProvider) applicationDeclaredSecrets(input WorkspaceApplicationRuntimeInput, declarations []contracts.WorkspaceApplicationSecretInput) (map[string]string, map[string]string, error) {
	files, env := map[string]string{}, map[string]string{}
	for _, declared := range declarations {
		var binding contracts.WorkspaceApplicationRuntimeSecretBinding
		count := 0
		for _, candidate := range input.SecretBindings {
			if candidate.Name == declared.Name {
				binding = candidate
				count++
			}
		}
		if count != 1 || (declared.Target == "") == (declared.Env == "") {
			return nil, nil, ErrLaunchStageBindingConflict
		}
		var source string
		var value []byte
		if gateway, hasGateway := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey); hasGateway && declared.Name == gateway.Name {
			expected, err := workspaceApplicationGatewayBinding(input)
			if err != nil || expected != binding {
				return nil, nil, ErrLaunchStageBindingConflict
			}
			directory, metadata, err := p.applicationGatewayVersion(binding.SecretRef, binding.Version)
			if err != nil {
				return nil, nil, err
			}
			if metadata.AccountID != input.AccountID || metadata.WorkspaceID != input.WorkspaceID {
				return nil, nil, ErrLaunchStageBindingConflict
			}
			source = filepath.Join(directory, localDockerGatewayKeyFile)
			if declared.Env != "" {
				value, err = os.ReadFile(source)
				if err != nil {
					return nil, nil, ErrLaunchStageBindingConflict
				}
			}
		} else {
			var err error
			source, value, err = p.applicationGenericSecret(input, binding)
			if err != nil {
				return nil, nil, err
			}
		}
		if declared.Env != "" {
			if !contracts.ValidWorkspaceApplicationEnvironmentName(declared.Env) || strings.ContainsAny(string(value), "\x00\r\n") {
				return nil, nil, ErrLaunchStageBindingConflict
			}
			if _, exists := env[declared.Env]; exists {
				return nil, nil, ErrLaunchStageBindingConflict
			}
			env[declared.Env] = string(value)
		} else {
			if _, exists := files[declared.Target]; exists {
				return nil, nil, ErrLaunchStageBindingConflict
			}
			files[declared.Target] = source
		}
	}
	return files, env, nil
}

func (p *LocalDockerProvider) applicationSecretEnvironmentFile(input WorkspaceApplicationRuntimeInput, component string, env map[string]string) (string, error) {
	if len(env) == 0 {
		return "", nil
	}
	if !localDockerApplicationComponentNamePattern.MatchString(component) {
		return "", ErrLaunchStageBindingConflict
	}
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	var body strings.Builder
	for _, name := range names {
		body.WriteString(name + "=" + env[name] + "\n")
	}
	value := []byte(body.String())
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	parent := "application-secret-env/" + applicationRuntimeID(input) + "/" + component
	parts := strings.Split(parent, "/")
	for i := range parts {
		if err := ensureLocalDockerSecretDirectory(root, strings.Join(parts[:i+1], "/"), 0700); err != nil {
			return "", err
		}
	}
	path := parent + fmt.Sprintf("/%x.env", sha256.Sum256(value))
	if _, err := root.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeLocalDockerSecretFile(root, path, value, 0400); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	if err := localDockerApplicationSecretPath(root, path, false); err != nil {
		return "", err
	}
	info, err := root.Lstat(path)
	if err != nil || info.Mode().Perm() != 0400 {
		return "", ErrLaunchStageBindingConflict
	}
	actual, err := root.ReadFile(path)
	if err != nil || !bytes.Equal(actual, value) {
		return "", ErrLaunchStageBindingConflict
	}
	return filepath.Join(p.gatewaySecretRoot, path), nil
}

func (p *LocalDockerProvider) applicationComponentSecretArgs(input WorkspaceApplicationRuntimeInput, component string, declarations []contracts.WorkspaceApplicationSecretInput) ([]string, error) {
	files, env, err := p.applicationDeclaredSecrets(input, declarations)
	if err != nil {
		return nil, err
	}
	if component == "main" {
		files, _, err = p.applicationSecretFiles(input)
		if err != nil {
			return nil, err
		}
	}
	targets := make([]string, 0, len(files))
	for target := range files {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	args := []string{}
	for _, target := range targets {
		args = append(args, "--mount", "type=bind,source="+files[target]+",target="+target+",readonly,bind-propagation=rprivate")
	}
	path, err := p.applicationSecretEnvironmentFile(input, component, env)
	if err != nil {
		return nil, err
	}
	if path != "" {
		args = append(args, "--env-file", path)
	}
	return args, nil
}

func (p *LocalDockerProvider) verifyApplicationDeclaredSecrets(input WorkspaceApplicationRuntimeInput, container dockerContainerInspect, declarations []contracts.WorkspaceApplicationSecretInput) error {
	files, env, err := p.applicationDeclaredSecrets(input, declarations)
	if err != nil {
		return err
	}
	for target, source := range files {
		found := false
		for _, mount := range container.Mounts {
			if mount.Destination == target {
				found = mount.Source == source && !mount.RW && mount.Type == "bind"
			}
		}
		if !found {
			return errors.New("local_docker_application_secret_mount_mismatch")
		}
	}
	actual := map[string]string{}
	for _, entry := range container.Config.Env {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			actual[name] = value
		}
	}
	for name, value := range env {
		actualValue, present := actual[name]
		if !present || actualValue != value {
			return errors.New("local_docker_application_secret_environment_mismatch")
		}
	}
	return nil
}

// Called only after runtime absence. Delete owned generation files individually;
// unexpected entries, symlinks or changed bytes are never recursively removed.
func (p *LocalDockerProvider) removeApplicationSecretEnvFiles(input WorkspaceApplicationRuntimeInput) error {
	hasEnvironment := false
	for _, secret := range input.Revision.SecretInputs {
		hasEnvironment = hasEnvironment || secret.Env != ""
	}
	for _, dependency := range input.Revision.Dependencies {
		for _, secret := range dependency.SecretInputs {
			hasEnvironment = hasEnvironment || secret.Env != ""
		}
	}
	if !hasEnvironment {
		return nil
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	parent := "application-secret-env/" + applicationRuntimeID(input)
	if _, err := root.Lstat(parent); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := localDockerApplicationSecretPath(root, parent, true); err != nil {
		return err
	}
	directory, err := root.Open(parent)
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return firstNonNil(readErr, closeErr)
	}
	allowed := map[string]bool{"main": true}
	for _, dependency := range input.Revision.Dependencies {
		allowed[dependency.Name] = true
	}
	files := []string{}
	directories := []string{}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return ErrLaunchStageBindingConflict
		}
		component := parent + "/" + entry.Name()
		if err := localDockerApplicationSecretPath(root, component, true); err != nil {
			return err
		}
		dir, err := root.Open(component)
		if err != nil {
			return err
		}
		leaves, readErr := dir.ReadDir(-1)
		closeErr := dir.Close()
		if readErr != nil || closeErr != nil {
			return firstNonNil(readErr, closeErr)
		}
		for _, leaf := range leaves {
			path := component + "/" + leaf.Name()
			if err := localDockerApplicationSecretPath(root, path, false); err != nil {
				return err
			}
			value, err := root.ReadFile(path)
			if err != nil {
				return err
			}
			if leaf.Name() != fmt.Sprintf("%x.env", sha256.Sum256(value)) {
				return ErrLaunchStageBindingConflict
			}
			files = append(files, path)
		}
		directories = append(directories, component)
	}
	for _, path := range files {
		if err := root.Remove(path); err != nil {
			return err
		}
	}
	for _, path := range directories {
		if err := root.Remove(path); err != nil {
			return err
		}
	}
	return root.Remove(parent)
}
