package tkeapply

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	corev1 "k8s.io/api/core/v1"

	contracts "opl-cloud/packages/contracts/go"
)

var errImageCommand = errors.New("workspace_application_image_command_unverified")

// Keep a bounded stage as the durable error code, while preserving cancellation
// and transport causes for in-process callers. Raw registry responses are never
// surfaced in Error().
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

func extractImageCommand(cfg *v1.ConfigFile, platform string) ([]string, error) {
	if cfg == nil {
		return nil, errImageCommand
	}
	observed := cfg.OS + "/" + cfg.Architecture
	if cfg.Variant != "" {
		observed += "/" + cfg.Variant
	}
	if observed != platform {
		return nil, errImageCommand
	}
	if cfg.Config.Cmd == nil {
		return nil, nil
	}
	return append([]string(nil), cfg.Config.Cmd...), nil
}

type dockerConfigJSON struct {
	Auths map[string]authn.AuthConfig `json:"auths"`
}

// dockerConfigKeychain resolves registry credentials strictly in memory from a
// Kubernetes kubernetes.io/dockerconfigjson payload without touching disk.
type dockerConfigKeychain struct {
	auths map[string]authn.AuthConfig
}

func parseDockerConfigKeychain(raw []byte) (authn.Keychain, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errImageCommand
	}
	var parsed dockerConfigJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errImageCommand
	}
	return dockerConfigKeychain{auths: parsed.Auths}, nil
}

func (k dockerConfigKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if len(k.auths) == 0 || target == nil {
		return authn.Anonymous, nil
	}
	keys := []string{
		target.String(),
		target.RegistryStr(),
	}
	if target.RegistryStr() == name.DefaultRegistry {
		keys = append(keys, authn.DefaultAuthKey)
	}
	for _, candidate := range keys {
		if cfg, ok := k.auths[candidate]; ok {
			return authn.FromConfig(cfg), nil
		}
	}
	targetHost := normalizeRegistryAddress(target.RegistryStr())
	for rawAddr, cfg := range k.auths {
		if normalizeRegistryAddress(rawAddr) == targetHost {
			return authn.FromConfig(cfg), nil
		}
	}
	return authn.Anonymous, nil
}

func normalizeRegistryAddress(addr string) string {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			return strings.ToLower(parsed.Host)
		}
	}
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	if host, _, found := strings.Cut(trimmed, "/"); found {
		return strings.ToLower(host)
	}
	return strings.ToLower(trimmed)
}

func (p *Executor) readImageCommand(ctx context.Context, image, platform string) ([]string, error) {
	if !contracts.ValidWorkspaceImageReference(image) {
		return nil, errImageCommand
	}
	key := image + "\x00" + platform
	if cached, ok := imageCommandCache.Load(key); ok {
		return append([]string(nil), cached.([]string)...), nil
	}
	secret, err := p.getSecret(ctx, p.Installation.ImagePullSecretName)
	if err != nil {
		return nil, imageCommandReadError{"secret_read", err}
	}
	if secret == nil ||
		secret.Type != corev1.SecretTypeDockerConfigJson ||
		secret.Name != p.Installation.ImagePullSecretName ||
		secret.Namespace != p.Installation.Namespace {
		return nil, errImageCommand
	}
	keychain, err := parseDockerConfigKeychain(secret.Data[corev1.DockerConfigJsonKey])
	if err != nil {
		return nil, errImageCommand
	}
	ref, err := name.ParseReference(image, name.StrictValidation)
	if err != nil {
		return nil, errImageCommand
	}
	plat, err := v1.ParsePlatform(platform)
	if err != nil || plat == nil {
		return nil, errImageCommand
	}
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	opts := []remote.Option{
		remote.WithContext(readCtx),
		remote.WithAuthFromKeychain(keychain),
		remote.WithPlatform(*plat),
	}
	if p.RegistryTransport != nil {
		opts = append(opts, remote.WithTransport(p.RegistryTransport))
	}
	img, err := remote.Image(ref, opts...)
	if err != nil {
		return nil, imageCommandReadError{"registry_read", errors.Join(err, readCtx.Err())}
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, imageCommandReadError{"registry_read", errors.Join(err, readCtx.Err())}
	}
	args, err := extractImageCommand(cfg, platform)
	if err != nil {
		return nil, err
	}
	imageCommandCache.Store(key, append([]string(nil), args...))
	return args, nil
}
