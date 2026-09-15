package fabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

func applicationComponentExecution(revision contracts.WorkspaceApplicationRevision, name string) contracts.WorkspaceApplicationExecution {
	if name == contracts.WorkspaceApplicationComponentMain {
		return revision.Execution
	}
	for _, dependency := range revision.Dependencies {
		if dependency.Name == name {
			return dependency.Execution
		}
	}
	return contracts.WorkspaceApplicationExecution{}
}

func (p *LocalDockerProvider) applicationExecutionArgs(execution contracts.WorkspaceApplicationExecution) ([]string, error) {
	if err := contracts.ValidateWorkspaceApplicationExecution(execution); err != nil {
		return nil, err
	}
	args := []string{}
	if execution.UserID != nil {
		user := strconv.FormatInt(*execution.UserID, 10)
		if execution.GroupID != nil {
			user += ":" + strconv.FormatInt(*execution.GroupID, 10)
		}
		args = append(args, "--user", user)
	}
	if execution.Init {
		args = append(args, "--init")
	}
	if execution.SeccompProfile != "" {
		root, err := p.openGatewaySecretRoot()
		if err != nil {
			return nil, err
		}
		defer root.Close()
		directory := "application-security-profiles"
		file := directory + "/" + strings.Replace(execution.SeccompProfile, ":", "-", 1) + ".json"
		info, err := root.Lstat(directory)
		if err != nil || !info.IsDir() {
			return nil, errors.New("local_docker_application_security_profile_unavailable")
		}
		info, err = root.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			return nil, errors.New("local_docker_application_security_profile_unavailable")
		}
		data, err := root.ReadFile(file)
		if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != execution.SeccompProfile || !json.Valid(data) {
			return nil, errors.New("local_docker_application_security_profile_invalid")
		}
		args = append(args, "--security-opt", "seccomp="+filepath.Join(p.gatewaySecretRoot, file))
	}
	return args, nil
}

func (p *LocalDockerProvider) validateApplicationExecutions(input WorkspaceApplicationRuntimeInput) error {
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if _, err := p.applicationExecutionArgs(applicationComponentExecution(input.Revision, component.Name)); err != nil {
			return err
		}
	}
	return nil
}

func (p *LocalDockerProvider) verifyApplicationExecution(input WorkspaceApplicationRuntimeInput, name string, container dockerContainerInspect) error {
	execution := applicationComponentExecution(input.Revision, name)
	args, err := p.applicationExecutionArgs(execution)
	if err != nil {
		return err
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--user":
			i++
			if container.Config.User != args[i] {
				return errors.New("local_docker_application_user_mismatch")
			}
		case "--init":
			if container.HostConfig.Init == nil || !*container.HostConfig.Init {
				return errors.New("local_docker_application_init_mismatch")
			}
		case "--security-opt":
			i++
			// Docker CLI sends the approved file's compact JSON to the daemon;
			// inspect returns that content, not the CLI's local filename.
			data, err := os.ReadFile(strings.TrimPrefix(args[i], "seccomp="))
			if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != execution.SeccompProfile {
				return errors.New("local_docker_application_security_profile_invalid")
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, data); err != nil {
				return errors.New("local_docker_application_security_profile_invalid")
			}
			expected := "seccomp=" + compact.String()
			found := false
			for _, value := range container.HostConfig.SecurityOpt {
				if value == expected {
					found = true
				}
			}
			if !found {
				return errors.New("local_docker_application_security_profile_mismatch")
			}
		}
	}
	return nil
}
