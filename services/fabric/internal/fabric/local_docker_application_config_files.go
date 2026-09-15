package fabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	contracts "opl-cloud/packages/contracts/go"
)

func applicationConfigFilename(name string) string {
	return fmt.Sprintf("file-%x", sha256.Sum256([]byte(name)))
}

func applicationComponentConfigs(input WorkspaceApplicationRuntimeInput, name string) []contracts.WorkspaceApplicationConfigInput {
	if name == contracts.WorkspaceApplicationComponentMain {
		return input.Revision.ConfigInputs
	}
	for _, dependency := range input.Revision.Dependencies {
		if dependency.Name == name {
			return dependency.ConfigInputs
		}
	}
	return nil
}

func applicationConfigContents(input WorkspaceApplicationRuntimeInput) (map[string][]byte, error) {
	if len(contracts.WorkspaceApplicationConfigInputs(input.Revision)) == 0 && len(input.Configuration.Files) == 0 {
		return nil, nil
	}
	if err := contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		return nil, err
	}
	if len(input.Configuration.Files) == 0 {
		return nil, nil
	}
	owner, err := json.Marshal(struct{ AccountID, WorkspaceID, RuntimeID, ConfigurationDigest string }{input.AccountID, input.WorkspaceID, applicationRuntimeID(input), input.ConfigurationDigest})
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"owner.json": owner}
	for name, value := range input.Configuration.Files {
		files[applicationConfigFilename(name)] = []byte(value)
	}
	return files, nil
}

func readApplicationConfigDirectory(root *os.Root, directory string, files map[string][]byte) error {
	info, err := root.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0711 {
		return errors.New("local_docker_application_config_owner_mismatch")
	}
	dir, err := root.Open(directory)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if len(entries) != len(files) {
		return errors.New("local_docker_application_config_files_mismatch")
	}
	for name, expected := range files {
		actual, err := root.Lstat(directory + "/" + name)
		if err != nil || !actual.Mode().IsRegular() || actual.Mode().Perm() != 0444 {
			return errors.New("local_docker_application_config_file_invalid")
		}
		body, err := root.ReadFile(directory + "/" + name)
		if err != nil || !bytes.Equal(body, expected) {
			return errors.New("local_docker_application_config_content_mismatch")
		}
	}
	return nil
}

// Configuration is immutable for a runtime generation. Files are prepared only
// inside the provider root, never from an application-supplied host path.
func (p *LocalDockerProvider) applicationConfigFiles(input WorkspaceApplicationRuntimeInput, prepare bool) error {
	if len(contracts.WorkspaceApplicationConfigInputs(input.Revision)) == 0 && len(input.Configuration.Files) == 0 {
		return nil
	}
	files, err := applicationConfigContents(input)
	if err != nil {
		return err
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	parent := "application-configurations"
	directory := parent + "/" + applicationRuntimeID(input)
	if err = readApplicationConfigDirectory(root, directory, files); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) || !prepare {
		return err
	}
	if err = ensureLocalDockerSecretDirectory(root, parent, 0711); err != nil {
		return err
	}
	staging, err := localDockerSecretStagingName()
	if err != nil {
		return err
	}
	staging = parent + "/" + staging
	if err = ensureLocalDockerSecretDirectory(root, staging, 0711); err != nil {
		return err
	}
	defer func() {
		for name := range files {
			_ = root.Remove(staging + "/" + name)
		}
		_ = root.Remove(staging)
	}()
	for name, content := range files {
		if err = writeLocalDockerSecretFile(root, staging+"/"+name, content, 0444); err != nil {
			return err
		}
	}
	if err = syncLocalDockerSecretPath(root, staging); err != nil {
		return err
	}
	if err = root.Rename(staging, directory); err != nil {
		// An identical concurrent preparation may own this generation already.
		if readErr := readApplicationConfigDirectory(root, directory, files); readErr != nil {
			return errors.Join(err, readErr)
		}
	}
	return readApplicationConfigDirectory(root, directory, files)
}

func (p *LocalDockerProvider) applicationConfigMountArgs(input WorkspaceApplicationRuntimeInput, component string) []string {
	args := []string{}
	for _, config := range applicationComponentConfigs(input, component) {
		source := filepath.Join(p.gatewaySecretRoot, "application-configurations", applicationRuntimeID(input), applicationConfigFilename(config.Name))
		args = append(args, "--mount", "type=bind,source="+source+",target="+config.Target+",readonly,bind-propagation=rprivate")
	}
	return args
}

func (p *LocalDockerProvider) verifyApplicationConfigMounts(input WorkspaceApplicationRuntimeInput, component string, container dockerContainerInspect) error {
	if len(applicationComponentConfigs(input, component)) == 0 {
		return nil
	}
	if err := p.applicationConfigFiles(input, false); err != nil {
		return err
	}
	for _, config := range applicationComponentConfigs(input, component) {
		source := filepath.Join(p.gatewaySecretRoot, "application-configurations", applicationRuntimeID(input), applicationConfigFilename(config.Name))
		found := false
		for _, mount := range container.Mounts {
			if mount.Destination == config.Target {
				found = mount.Type == "bind" && mount.Source == source && !mount.RW
			}
		}
		if !found {
			return errors.New("local_docker_application_config_mount_mismatch")
		}
	}
	return nil
}

func (p *LocalDockerProvider) removeApplicationConfigFiles(input WorkspaceApplicationRuntimeInput) error {
	files, err := applicationConfigContents(input)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	directory := "application-configurations/" + applicationRuntimeID(input)
	if err = readApplicationConfigDirectory(root, directory, files); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err = root.Remove(directory + "/" + name); err != nil {
			return err
		}
	}
	return root.Remove(directory)
}
