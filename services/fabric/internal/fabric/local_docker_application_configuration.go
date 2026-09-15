package fabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	contracts "opl-cloud/packages/contracts/go"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func applicationEnvironmentArgs(input WorkspaceApplicationRuntimeInput) []string {
	names := make([]string, 0, len(input.Configuration.Environment))
	for name := range input.Configuration.Environment {
		names = append(names, name)
	}
	sort.Strings(names)
	args := make([]string, 0, len(names)*2)
	for _, name := range names {
		args = append(args, "--env", name+"="+input.Configuration.Environment[name])
	}
	return args
}

func (p *LocalDockerProvider) applicationSecretFiles(input WorkspaceApplicationRuntimeInput) (map[string]string, localDockerGatewayMetadata, error) {
	files, _, err := p.applicationDeclaredSecrets(input, input.Revision.SecretInputs)
	if err != nil {
		return nil, localDockerGatewayMetadata{}, err
	}
	if input.Revision.RuntimeProfile != "opl_app" {
		return files, localDockerGatewayMetadata{}, nil
	}
	binding, err := workspaceApplicationGatewayBinding(input)
	if err != nil {
		return nil, localDockerGatewayMetadata{}, err
	}
	_, metadata, err := p.applicationGatewayVersion(binding.SecretRef, binding.Version)
	if err != nil {
		return nil, metadata, err
	}
	if metadata.AccountID != input.AccountID || metadata.WorkspaceID != input.WorkspaceID || metadata.Version != binding.Version {
		return nil, metadata, ErrLaunchStageBindingConflict
	}

	credentialPath := filepath.Join(p.gatewaySecretRoot, "application-credentials", applicationRuntimeID(input))
	files["/run/secrets/opl_webui_password"] = filepath.Join(credentialPath, localDockerWebUIPasswordFile)
	files["/run/secrets/webui_session_secret"] = filepath.Join(credentialPath, localDockerWebUISessionSecretFile)
	return files, metadata, nil
}

func (p *LocalDockerProvider) applicationSecretMountArgs(input WorkspaceApplicationRuntimeInput) ([]string, error) {
	return p.applicationComponentSecretArgs(input, "main", input.Revision.SecretInputs)
}

func (p *LocalDockerProvider) verifyApplicationConfiguration(input WorkspaceApplicationRuntimeInput, container dockerContainerInspect) error {
	if input.SchemaVersion == 0 {
		return nil
	}
	if err := p.applicationCredentialFiles(input, false); err != nil {
		return err
	}
	paths, err := p.storagePaths(input.WorkspaceID)
	if err != nil {
		return err
	}
	for _, mount := range input.Revision.PersistentMounts {
		source := localDockerApplicationPersistentSource(input, paths, mount)
		found := false
		for _, actual := range container.Mounts {
			if actual.Destination == mount.MountPath {
				found = actual.Type == "bind" && actual.Source == source && actual.RW != mount.ReadOnly
			}
		}
		if !found {
			return errors.New("local_docker_application_data_mount_mismatch")
		}
	}
	actual := map[string]string{}
	for _, entry := range container.Config.Env {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			actual[name] = value
		}
	}
	for name, value := range input.Configuration.Environment {
		if actual[name] != value {
			return errors.New("local_docker_application_environment_mismatch")
		}
	}
	files, _, err := p.applicationSecretFiles(input)
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
	return p.verifyApplicationDeclaredSecrets(input, container, input.Revision.SecretInputs)
}
func (p *LocalDockerProvider) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	_, _, err := p.applicationSecretFiles(input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	credentials, err := applicationWebUICredentials(input)
	if err == nil {
		err = p.applicationCredentialFiles(input, false)
	}
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: applicationRuntimeID(input), WorkspaceID: input.WorkspaceID, WebUIUsername: webuiUsername, WebUIPassword: string(credentials.Password)}, nil
}

// Resolve a retained immutable version inside the approved store. Rotating the
// current pointer must not change an already-deployed generation's files.
func (p *LocalDockerProvider) applicationGatewayVersion(secretRef, version string) (string, localDockerGatewayMetadata, error) {
	if !validLocalDockerGatewaySecretRef(secretRef) || len(version) != 16 {
		return "", localDockerGatewayMetadata{}, ErrLaunchStageBindingConflict
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return "", localDockerGatewayMetadata{}, err
	}
	defer root.Close()
	directory, err := root.Open(secretRef + "/" + localDockerGatewayVersionsDir)
	if err != nil {
		return "", localDockerGatewayMetadata{}, err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil || closeErr != nil {
		return "", localDockerGatewayMetadata{}, firstNonNil(err, closeErr)
	}
	selected := ""
	var metadata localDockerGatewayMetadata
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "sha256-"+version) {
			continue
		}
		digest := strings.TrimPrefix(entry.Name(), "sha256-")
		if _, err := localDockerGatewayVersionDir(digest); err != nil {
			return "", metadata, err
		}
		key, current, err := readLocalDockerGatewayVersion(root, secretRef, entry.Name())
		if err != nil {
			return "", metadata, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(key)) != digest || current.Version != version || current.SecretRef != secretRef || current.Fingerprint != "sha256:"+digest || selected != "" {
			return "", metadata, ErrLaunchStageBindingConflict
		}
		selected = entry.Name()
		metadata = current
	}
	if selected == "" {
		return "", metadata, ErrWorkspaceLaunchResourceAbsent
	}
	return filepath.Join(p.gatewaySecretRoot, secretRef, localDockerGatewayVersionsDir, selected), metadata, nil
}

func applicationWebUICredentials(input WorkspaceApplicationRuntimeInput) (localDockerWebUICredentials, error) {
	if _, err := workspaceApplicationGatewayBinding(input); err != nil {
		return localDockerWebUICredentials{}, err
	}
	seed := strings.TrimSpace(os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED"))
	if seed == "" {
		return localDockerWebUICredentials{}, errors.New("workspace_application_webui_credential_seed_required")
	}
	token := input.Configuration.CredentialVersion
	if token == "" {
		return localDockerWebUICredentials{}, errors.New("workspace_application_credential_version_required")
	}
	return localDockerWebUICredentials{Password: []byte(deriveAionUIAdminPassword(seed, input.WorkspaceID, token)), SessionSecret: []byte(deriveWebUISessionSecret(seed, input.WorkspaceID, token))}, nil
}
func (p *LocalDockerProvider) applicationCredentialFiles(input WorkspaceApplicationRuntimeInput, prepare bool) error {
	if input.Revision.RuntimeProfile != "opl_app" {
		return nil
	}
	credentials, err := applicationWebUICredentials(input)
	if err != nil {
		return err
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	parent := "application-credentials"
	directory := parent + "/" + applicationRuntimeID(input)
	expected := map[string][]byte{localDockerWebUIPasswordFile: credentials.Password, localDockerWebUISessionSecretFile: credentials.SessionSecret}
	if _, err := root.Lstat(directory); errors.Is(err, os.ErrNotExist) && prepare {
		if err := ensureLocalDockerSecretDirectory(root, parent, 0711); err != nil {
			return err
		}
		staging, err := localDockerSecretStagingName()
		if err != nil {
			return err
		}
		temporary := parent + "/.pending-" + staging
		if err := ensureLocalDockerSecretDirectory(root, temporary, 0711); err != nil {
			return err
		}
		for name, value := range expected {
			if err := writeLocalDockerSecretFile(root, temporary+"/"+name, value, 0400); err != nil {
				return err
			}
		}
		if err := root.Rename(temporary, directory); err != nil {
			return err
		}
		if err := syncLocalDockerSecretPath(root, parent); err != nil {
			return err
		}
	}
	for name, value := range expected {
		info, err := root.Lstat(directory + "/" + name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0400 {
			return ErrLaunchStageBindingConflict
		}
		actual, err := root.ReadFile(directory + "/" + name)
		if err != nil || !bytes.Equal(actual, value) {
			return ErrLaunchStageBindingConflict
		}
	}
	return nil
}

func localDockerApplicationPersistentSource(input WorkspaceApplicationRuntimeInput, paths localDockerStoragePaths, mount contracts.WorkspaceApplicationMount) string {
	if input.SchemaVersion == 0 || input.DataLayout == "legacy_application" {
		return filepath.Join(paths.Data, mount.Name)
	}
	if input.DataLayout == "legacy_opl" {
		if mount.Name == "projects" {
			return paths.Projects
		}
		return paths.Data
	}
	return filepath.Join(paths.Data, contracts.WorkspaceApplicationDataDirectory(input.DataBindingID), mount.Name)
}

func (p *LocalDockerProvider) removeApplicationCredentialFiles(input WorkspaceApplicationRuntimeInput) error {
	if input.Revision.RuntimeProfile != "opl_app" {
		return nil
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	directory := "application-credentials/" + applicationRuntimeID(input)
	if _, err := root.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := p.applicationCredentialFiles(input, false); err != nil {
		return err
	}
	for _, name := range []string{localDockerWebUIPasswordFile, localDockerWebUISessionSecretFile} {
		if err := root.Remove(directory + "/" + name); err != nil {
			return err
		}
	}
	return root.Remove(directory)
}
