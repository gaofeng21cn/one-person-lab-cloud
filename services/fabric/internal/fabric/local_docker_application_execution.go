package fabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

type localDockerExecutionSpec struct {
	User        string
	Init        *bool
	SecurityOpt []string
}

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

func (p *LocalDockerProvider) applicationExecutionSpec(execution contracts.WorkspaceApplicationExecution, defaultIdentity *localDockerWorkloadIdentity) (localDockerExecutionSpec, error) {
	if err := contracts.ValidateWorkspaceApplicationExecution(execution); err != nil {
		return localDockerExecutionSpec{}, err
	}
	var spec localDockerExecutionSpec
	uid, gid := execution.UserID, execution.GroupID
	if defaultIdentity != nil {
		if uid == nil {
			uid = &defaultIdentity.uid
		}
		if gid == nil {
			gid = &defaultIdentity.gid
		}
	}
	if uid != nil {
		user := strconv.FormatInt(*uid, 10)
		if gid != nil {
			user += ":" + strconv.FormatInt(*gid, 10)
		}
		spec.User = user
	}
	if execution.Init {
		enabled := true
		spec.Init = &enabled
	}
	if execution.SeccompProfile != "" {
		root, err := p.openGatewaySecretRoot()
		if err != nil {
			return localDockerExecutionSpec{}, err
		}
		defer root.Close()
		directory := "application-security-profiles"
		file := directory + "/" + strings.Replace(execution.SeccompProfile, ":", "-", 1) + ".json"
		info, err := root.Lstat(directory)
		if err != nil || !info.IsDir() {
			return localDockerExecutionSpec{}, errors.New("local_docker_application_security_profile_unavailable")
		}
		info, err = root.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			return localDockerExecutionSpec{}, errors.New("local_docker_application_security_profile_unavailable")
		}
		data, err := root.ReadFile(file)
		if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != execution.SeccompProfile || !json.Valid(data) {
			return localDockerExecutionSpec{}, errors.New("local_docker_application_security_profile_invalid")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			return localDockerExecutionSpec{}, errors.New("local_docker_application_security_profile_invalid")
		}
		// Docker Engine API takes the approved seccomp policy JSON inline in
		// HostConfig.SecurityOpt (identical to what docker inspect returns).
		spec.SecurityOpt = append(spec.SecurityOpt, "seccomp="+compact.String())
	}
	return spec, nil
}

func (p *LocalDockerProvider) validateApplicationExecutions(input WorkspaceApplicationRuntimeInput) error {
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if _, err := p.applicationExecutionSpec(applicationComponentExecution(input.Revision, component.Name), applicationComponentWorkloadIdentity(input.Revision, component.Name)); err != nil {
			return err
		}
	}
	return nil
}

func (p *LocalDockerProvider) verifyApplicationExecution(input WorkspaceApplicationRuntimeInput, name string, container dockerContainerInspect) error {
	execution := applicationComponentExecution(input.Revision, name)
	spec, err := p.applicationExecutionSpec(execution, applicationComponentWorkloadIdentity(input.Revision, name))
	if err != nil {
		return err
	}
	if spec.User != "" && container.Config.User != spec.User {
		return errors.New("local_docker_application_user_mismatch")
	}
	if spec.Init != nil && *spec.Init && (container.HostConfig.Init == nil || !*container.HostConfig.Init) {
		return errors.New("local_docker_application_init_mismatch")
	}
	for _, expected := range spec.SecurityOpt {
		if !slices.Contains(container.HostConfig.SecurityOpt, expected) {
			return errors.New("local_docker_application_security_profile_mismatch")
		}
	}
	return nil
}
