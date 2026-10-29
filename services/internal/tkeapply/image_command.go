package tkeapply

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

var errImageCommand = errors.New("workspace_application_image_command_unverified")

// Keep a bounded stage as the durable error code, while preserving cancellation
// and transport causes for in-process callers. Raw registry stderr is not read.
type imageCommandReadError struct {
	stage string
	cause error
}

func (e imageCommandReadError) Error() string {
	return "workspace_application_image_command_" + e.stage + "_failed"
}
func (e imageCommandReadError) Unwrap() []error { return []error{errImageCommand, e.cause} }

// OCI configuration is immutable under a verified image digest. Cache successful
// content reads only; a registry or credential failure is never an empty CMD.
var imageCommandCache sync.Map

func (p *Executor) mainImageArgs(ctx context.Context, input WorkspaceApplicationRuntimeInput) ([]string, error) {
	if len(input.Revision.Entrypoint) == 0 {
		return nil, nil
	}
	var args []string
	var err error
	if p.ImageCommand != nil {
		args, err = p.ImageCommand(ctx, input.Revision.Image, input.Revision.Platform)
	} else {
		args, err = p.readImageCommand(ctx, input.Revision.Image, input.Revision.Platform)
	}
	if err != nil {
		return nil, err
	}
	// Kubernetes expands supplied args, unlike OCI's image-default CMD. Escape
	// dollar signs so the same image bytes produce the same argv in both cases.
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = strings.ReplaceAll(arg, "$", "$$")
	}
	return result, nil
}

func decodeImageCommand(raw []byte, platform string) ([]string, error) {
	if len(raw) > 8<<20 {
		return nil, errImageCommand
	}
	var images map[string]json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		return nil, errImageCommand
	}
	selected := raw
	if _, single := images["config"]; !single {
		selected = images[platform]
	}
	var image struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
		Config       *struct {
			Cmd []*string `json:"Cmd"`
		} `json:"config"`
	}
	if json.Unmarshal(selected, &image) != nil || image.Config == nil {
		return nil, errImageCommand
	}
	observed := image.OS + "/" + image.Architecture
	if image.Variant != "" {
		observed += "/" + image.Variant
	}
	if observed != platform {
		return nil, errImageCommand
	}
	if image.Config.Cmd == nil {
		return nil, nil
	}
	args := make([]string, len(image.Config.Cmd))
	for i, arg := range image.Config.Cmd {
		if arg == nil {
			return nil, errImageCommand
		}
		args[i] = *arg
	}
	return args, nil
}

func (p *Executor) readImageCommand(ctx context.Context, image, platform string) ([]string, error) {
	if !contracts.ValidWorkspaceImageReference(image) {
		return nil, errImageCommand
	}
	key := image + "\x00" + platform
	if cached, ok := imageCommandCache.Load(key); ok {
		return append([]string(nil), cached.([]string)...), nil
	}
	raw, err := p.callKubectl(ctx, []string{"get", "secret/" + p.Installation.ImagePullSecretName, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, imageCommandReadError{"secret_read", err}
	}
	var secret struct {
		Type     string `json:"type"`
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Data map[string]string `json:"data"`
	}
	if json.Unmarshal(raw, &secret) != nil || secret.Type != "kubernetes.io/dockerconfigjson" || secret.Metadata.Name != p.Installation.ImagePullSecretName || secret.Metadata.Namespace != p.Installation.Namespace {
		return nil, errImageCommand
	}
	config, err := base64.StdEncoding.DecodeString(secret.Data[".dockerconfigjson"])
	if err != nil || !json.Valid(config) {
		return nil, errImageCommand
	}
	dir, err := os.MkdirTemp("", "opl-runtime-image-command-")
	if err != nil {
		return nil, imageCommandReadError{"config_create", err}
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(dir+"/config.json", config, 0600); err != nil {
		return nil, imageCommandReadError{"config_write", err}
	}
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(readCtx, "docker", "--config", dir, "buildx", "imagetools", "inspect", image, "--format", "{{json .Image}}")
	var output limitedImageOutput
	command.Stdout = &output
	// Registry stderr may include signed redirect URLs; retain only a stable code.
	if err := command.Run(); err != nil {
		return nil, imageCommandReadError{"registry_read", errors.Join(err, readCtx.Err())}
	}
	args, err := decodeImageCommand(output.Bytes(), platform)
	if err != nil {
		return nil, err
	}
	imageCommandCache.Store(key, append([]string(nil), args...))
	return args, nil
}

type limitedImageOutput struct{ bytes.Buffer }

func (b *limitedImageOutput) Write(value []byte) (int, error) {
	if b.Len()+len(value) > 8<<20 {
		return 0, fmt.Errorf("image command output exceeds limit")
	}
	return b.Buffer.Write(value)
}
