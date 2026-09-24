package build

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// Runner invokes one instance-approved, isolated BuildKit builder. It receives
// immutable inputs, never a database handle or Cloud credentials. The builder's
// credential file may access only the approved registries and output namespace.
type Runner struct {
	Builder                           string
	RegistryPrefix                    string
	StorageURL                        string
	StorageToken                      string
	RegistryToken                     string
	DockerConfig                      string
	WorkDir                           string
	MaxPackageBytes, MaxExpandedBytes int64
	MaxFiles                          int
	Timeout                           time.Duration
	HTTP                              *http.Client
	AllowHTTP                         bool
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.:-]*(/[a-z0-9][a-z0-9._-]*)+$`)
var pathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

func digest(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func (r *Runner) Validate() error {
	if r.Builder == "" || !repositoryPattern.MatchString(r.RegistryPrefix) || r.StorageURL == "" || r.WorkDir == "" || r.DockerConfig == "" || r.MaxPackageBytes <= 0 || r.MaxExpandedBytes <= 0 || r.MaxFiles <= 0 || r.Timeout <= 0 {
		return errors.New("isolated builder, registry, storage, credential directory and explicit worker limits are required")
	}
	u, err := url.Parse(r.StorageURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(r.AllowHTTP && u.Scheme == "http")) {
		return errors.New("invalid approved storage endpoint")
	}
	return nil
}
func (r *Runner) Repository(tenantID, packageID string) string {
	t := sha256.Sum256([]byte(tenantID))
	p := sha256.Sum256([]byte(packageID))
	return r.RegistryPrefix + "/" + hex.EncodeToString(t[:12]) + "/" + hex.EncodeToString(p[:12])
}
func platform(p *api.ImagePlatform) (string, error) {
	if p.GetOs() != api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX {
		return "", errors.New("Linux platform required")
	}
	arch := ""
	switch p.GetArchitecture() {
	case api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64:
		arch = "amd64"
	case api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_ARM64:
		arch = "arm64"
	default:
		return "", errors.New("unsupported output architecture")
	}
	if p.GetVariant() != "" && !regexp.MustCompile(`^v[0-9]+$`).MatchString(p.GetVariant()) {
		return "", errors.New("invalid platform variant")
	}
	if p.GetVariant() != "" {
		arch += "/" + p.GetVariant()
	}
	return "linux/" + arch, nil
}
func artifact(a *api.ArtifactReference) error {
	if !repositoryPattern.MatchString(a.GetRepository()) || !digestPattern.MatchString(a.GetDigest()) {
		return errors.New("artifact repository and immutable digest are required")
	}
	_, err := platform(a.Platform)
	return err
}
func safePath(p string) bool {
	return pathPattern.MatchString(p) && path.Clean(p) == p && !strings.Contains(p, "/../") && p != "/"
}
func (r *Runner) ValidateInput(in *api.BuildInputSnapshot) error {
	if in == nil || in.PackageId == "" || in.PackageVersionId == "" || in.RuntimeVersionId == "" || in.WebuiVersionId == "" || !digestPattern.MatchString(in.SnapshotDigest) {
		return errors.New("complete immutable Build input is required")
	}
	o := in.GetPackageObject()
	if o.GetStorageObjectId() == "" || o.GetVersionId() == "" || !digestPattern.MatchString(o.GetSha256()) || o.GetSizeBytes() <= 0 || o.GetSizeBytes() > r.MaxPackageBytes {
		return errors.New("invalid immutable source object")
	}
	for _, a := range []*api.ArtifactReference{in.RuntimeArtifact, in.WebuiArtifact, in.GetRuntimeContract().GetBuildRecipe().GetFrontend()} {
		if err := artifact(a); err != nil {
			return err
		}
	}
	if !proto.Equal(in.RuntimeArtifact, in.GetRuntimeContract().GetImage()) || !proto.Equal(in.WebuiArtifact, in.GetWebuiContract().GetImage()) {
		return errors.New("publisher image identity mismatch")
	}
	recipe := in.GetRuntimeContract().GetBuildRecipe()
	if recipe.GetVersion() != api.BuildRecipeContractVersionEnum_BUILD_RECIPE_CONTRACT_VERSION_ENUM_OPL_BUILD_RECIPE_V1 || recipe.GetNetworkPolicy() != api.BuildRecipeContractNetworkPolicyEnum_BUILD_RECIPE_CONTRACT_NETWORK_POLICY_ENUM_NONE || recipe.GetRuntimeContextName() != api.BuildRecipeContractRuntimeContextNameEnum_BUILD_RECIPE_CONTRACT_RUNTIME_CONTEXT_NAME_ENUM_RUNTIME {
		return errors.New("unsupported Build recipe")
	}
	if recipe.GetPackageInput().GetContextName() != api.PackageBuildInputContextNameEnum_PACKAGE_BUILD_INPUT_CONTEXT_NAME_ENUM_AGENT_PACKAGE || recipe.GetWebuiInput().GetContextName() != api.WebuiBuildInputContextNameEnum_WEBUI_BUILD_INPUT_CONTEXT_NAME_ENUM_WEBUI {
		return errors.New("invalid recipe contexts")
	}
	if !proto.Equal(recipe.OutputPlatform, in.RuntimeArtifact.Platform) || !proto.Equal(recipe.OutputPlatform, in.WebuiArtifact.Platform) {
		return errors.New("input platform mismatch")
	}
	if !repositoryPattern.MatchString(recipe.GetRecipe().GetRepository()) || !digestPattern.MatchString(recipe.GetRecipe().GetDigest()) || recipe.GetRecipe().GetMediaType() != api.RecipeArtifactMediaTypeEnum_RECIPE_ARTIFACT_MEDIA_TYPE_ENUM_APPLICATION_VND_OPL_BUILD_RECIPE_V1_TAR {
		return errors.New("immutable recipe artifact required")
	}
	if recipe.GetRecipe().GetDockerfilePath() != "Dockerfile" {
		return errors.New("recipe Dockerfile must be the artifact root Dockerfile")
	}
	p, w := recipe.PackageInput, recipe.WebuiInput
	for _, v := range []string{p.SourceRoot, p.TargetPath, w.SourcePath, w.TargetPath} {
		if !safePath(v) {
			return errors.New("unsafe recipe input path")
		}
	}
	if p.Uid < 0 || p.Gid < 0 || w.Uid < 0 || w.Gid < 0 || recipe.OutputImageCommand == nil || len(recipe.OutputImageCommand.Entrypoint) == 0 {
		return errors.New("recipe ownership and output entrypoint are required")
	}
	if in.RuntimeContract.ApplicationRevisionTemplate == nil || in.RuntimeContractReference == nil || in.WebuiContractReference == nil {
		return errors.New("publisher descriptor and references are required")
	}
	return nil
}

// FixedDockerfile is the only accepted recipe in v1. The approved recipe tar
// must contain these exact bytes; Package Dockerfiles and scripts are never read.
func FixedDockerfile(in *api.BuildInputSnapshot) []byte {
	r := in.RuntimeContract.BuildRecipe
	p, w := r.PackageInput, r.WebuiInput
	entry, _ := json.Marshal(r.OutputImageCommand.Entrypoint)
	cmd, _ := json.Marshal(r.OutputImageCommand.Cmd)
	return []byte(fmt.Sprintf("# syntax=%s@%s\nFROM runtime\nCOPY --from=agent_package --chown=%d:%d %s %s\nCOPY --from=webui --chown=%d:%d %s %s\nENTRYPOINT %s\nCMD %s\n", r.Frontend.Repository, r.Frontend.Digest, p.Uid, p.Gid, p.SourceRoot, p.TargetPath, w.Uid, w.Gid, w.SourcePath, w.TargetPath, entry, cmd))
}
func (r *Runner) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errors.New("storage and registry redirects are not approved")
	}}
}
func (r *Runner) request(ctx context.Context, endpoint, token, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return r.client().Do(req)
}
func (r *Runner) registryURL(repository, kind, reference string) string {
	parts := strings.SplitN(repository, "/", 2)
	scheme := "https"
	if r.AllowHTTP {
		scheme = "http"
	}
	return scheme + "://" + parts[0] + "/v2/" + parts[1] + "/" + kind + "/" + reference
}

var ErrAbsent = errors.New("registry manifest is absent")

type Manifest struct {
	Digest string
	Size   int64
	Bytes  []byte
}

func (r *Runner) ReadManifest(ctx context.Context, repository, reference string, p *api.ImagePlatform) (Manifest, error) {
	if !repositoryPattern.MatchString(repository) || (!digestPattern.MatchString(reference) && !regexp.MustCompile(`^build_[a-f0-9]{32}$`).MatchString(reference)) {
		return Manifest{}, errors.New("unsafe manifest identity")
	}
	resp, err := r.request(ctx, r.registryURL(repository, "manifests", reference), r.RegistryToken, "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	if err != nil {
		return Manifest{}, errors.New("registry manifest read unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return Manifest{}, ErrAbsent
	}
	if resp.StatusCode != 200 {
		return Manifest{}, fmt.Errorf("registry manifest read status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Manifest{}, errors.New("registry manifest body unavailable")
	}
	d := digest(b)
	if strings.HasPrefix(reference, "sha256:") && reference != d {
		return Manifest{}, errors.New("registry manifest digest mismatch")
	}
	if h := resp.Header.Get("Docker-Content-Digest"); h != "" && h != d {
		return Manifest{}, errors.New("registry digest header mismatch")
	}
	var m struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
		Config        struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"layers"`
	}
	if json.Unmarshal(b, &m) != nil || m.SchemaVersion != 2 || !digestPattern.MatchString(m.Config.Digest) || len(m.Layers) == 0 {
		return Manifest{}, errors.New("single-platform image manifest required")
	}
	config, err := r.readBlob(ctx, repository, m.Config.Digest, 8<<20)
	if err != nil {
		return Manifest{}, err
	}
	var c struct{ OS, Architecture, Variant string }
	if json.Unmarshal(config, &c) != nil {
		return Manifest{}, errors.New("invalid image config")
	}
	want, err := platform(p)
	if err != nil {
		return Manifest{}, err
	}
	actual := c.OS + "/" + c.Architecture
	if c.Variant != "" {
		actual += "/" + c.Variant
	}
	if actual != want {
		return Manifest{}, errors.New("output image platform mismatch")
	}
	size := int64(len(b)) + int64(len(config))
	for _, l := range m.Layers {
		if !digestPattern.MatchString(l.Digest) || l.Size < 0 {
			return Manifest{}, errors.New("invalid image layer")
		}
		size += l.Size
	}
	return Manifest{Digest: d, Size: size, Bytes: b}, nil
}
func (r *Runner) readBlob(ctx context.Context, repository, d string, max int64) ([]byte, error) {
	resp, err := r.request(ctx, r.registryURL(repository, "blobs", d), r.RegistryToken, "")
	if err != nil {
		return nil, errors.New("registry blob unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry blob read status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || int64(len(b)) > max || digest(b) != d {
		return nil, errors.New("registry blob digest or size mismatch")
	}
	return b, nil
}
func (r *Runner) packageBytes(ctx context.Context, o *api.SourceObjectReference) ([]byte, error) {
	// Capability's object authority keys immutable objects by the SHA-256 hex
	// digest. Both typed IDs must carry that same digest; there is no mutable
	// version URL or caller-controlled path segment.
	if o.GetStorageObjectId() != o.GetVersionId() || !digestPattern.MatchString(o.GetStorageObjectId()) {
		return nil, errors.New("source object identity is not immutable")
	}
	endpoint := strings.TrimRight(r.StorageURL, "/") + "/objects/" + url.PathEscape(strings.TrimPrefix(o.GetStorageObjectId(), "sha256:"))
	resp, err := r.request(ctx, endpoint, r.StorageToken, "")
	if err != nil {
		return nil, errors.New("source object read unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("source object read status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, r.MaxPackageBytes+1))
	if err != nil || int64(len(b)) != o.SizeBytes || digest(b) != o.Sha256 {
		return nil, errors.New("source object digest or size mismatch")
	}
	return b, nil
}

// extractPackage consumes the ZIP bytes admitted by Capability. The approved
// build recipe remains a separate tar artifact; formats are never auto-detected.
func extractPackage(data []byte, directory string, maxBytes int64, maxFiles int) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("package must be a valid ZIP archive")
	}
	if len(zr.File) > maxFiles {
		return errors.New("package entry limit exceeded")
	}
	seen := map[string]bool{}
	var total int64
	for _, entry := range zr.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || name == "." || path.IsAbs(name) || strings.ContainsAny(name, "\\:") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || seen[name] {
			return errors.New("unsafe or duplicate package path")
		}
		seen[name] = true
		if entry.UncompressedSize64 > uint64(maxBytes-total) {
			return errors.New("package expansion limit exceeded")
		}
		total += int64(entry.UncompressedSize64)
		target := filepath.Join(directory, filepath.FromSlash(name))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return errors.New("package links and special files are forbidden")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		// Preserve only the publisher's executable bit, never special permissions.
		mode := os.FileMode(0644)
		if entry.Mode()&0111 != 0 {
			mode = 0755
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			reader.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
		readErr, closeErr := reader.Close(), out.Close()
		if copyErr != nil || readErr != nil || closeErr != nil || uint64(n) != entry.UncompressedSize64 {
			return errors.New("package entry integrity failed")
		}
	}
	return nil
}

// extract admits only regular files and directories. No links, devices, duplicate
// path overwrite, absolute paths, or expansion outside the explicit resource cap.
func extract(data []byte, directory string, maxBytes int64, maxFiles int) error {
	var source io.Reader = strings.NewReader(string(data))
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, err := gzip.NewReader(source)
		if err != nil {
			return err
		}
		defer gz.Close()
		source = gz
	}
	tr := tar.NewReader(source)
	seen := map[string]bool{}
	var total int64
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid package tar")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || name == "." {
			continue
		}
		if path.IsAbs(name) || strings.Contains(name, "\\") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || seen[name] {
			return errors.New("unsafe or duplicate archive path")
		}
		seen[name] = true
		count++
		total += h.Size
		if count > maxFiles || h.Size < 0 || total > maxBytes {
			return errors.New("archive expansion limit exceeded")
		}
		target := filepath.Join(directory, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("archive links and special files are forbidden")
		}
	}
}

type ExecuteResult struct {
	Manifest Manifest
	Started  bool
	Err      error
}

func (r *Runner) Execute(ctx context.Context, jobID, repository string, in *api.BuildInputSnapshot, log func(string)) ExecuteResult {
	if err := r.ValidateInput(in); err != nil {
		return ExecuteResult{Err: err}
	}
	dir, err := os.MkdirTemp(r.WorkDir, "build-")
	if err != nil {
		return ExecuteResult{Err: errors.New("worker disk unavailable")}
	}
	defer os.RemoveAll(dir)
	b, err := r.packageBytes(ctx, in.PackageObject)
	if err != nil {
		return ExecuteResult{Err: err}
	}
	packageDir := filepath.Join(dir, "package")
	if err = os.Mkdir(packageDir, 0700); err != nil {
		return ExecuteResult{Err: err}
	}
	if err = extractPackage(b, packageDir, r.MaxExpandedBytes, r.MaxFiles); err != nil {
		return ExecuteResult{Err: err}
	}
	log("verified immutable package bytes and bounded archive")
	recipe := in.RuntimeContract.BuildRecipe.Recipe
	rb, err := r.readBlob(ctx, recipe.Repository, recipe.Digest, 4<<20)
	if err != nil {
		return ExecuteResult{Err: err}
	}
	recipeDir := filepath.Join(dir, "recipe")
	if err = os.Mkdir(recipeDir, 0700); err != nil {
		return ExecuteResult{Err: err}
	}
	if err = extract(rb, recipeDir, 4<<20, 10); err != nil {
		return ExecuteResult{Err: err}
	}
	df, err := os.ReadFile(filepath.Join(recipeDir, "Dockerfile"))
	if err != nil || string(df) != string(FixedDockerfile(in)) {
		return ExecuteResult{Err: errors.New("approved recipe is not the fixed Package/WebUI composition recipe")}
	}
	p, _ := platform(in.RuntimeArtifact.Platform)
	args := []string{"buildx", "build", "--builder", r.Builder, "--network=none", "--provenance=false", "--sbom=false", "--progress=rawjson", "--platform", p, "--build-context", "runtime=docker-image://" + in.RuntimeArtifact.Repository + "@" + in.RuntimeArtifact.Digest, "--build-context", "webui=docker-image://" + in.WebuiArtifact.Repository + "@" + in.WebuiArtifact.Digest, "--build-context", "agent_package=" + packageDir, "--tag", repository + ":" + jobID, "--push", recipeDir}
	bounded, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(bounded, "docker", args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "DOCKER_CONFIG=" + r.DockerConfig, "BUILDX_NO_DEFAULT_ATTESTATIONS=1"}
	writer := &progressWriter{log: log}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err = cmd.Start(); err != nil {
		return ExecuteResult{Err: errors.New("BuildKit command could not start")}
	}
	log("isolated BuildKit exporter started")
	runErr := cmd.Wait()
	readCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer done()
	manifest, readErr := r.ReadManifest(readCtx, repository, jobID, in.RuntimeArtifact.Platform)
	if readErr == nil {
		manifest, readErr = r.ReadManifest(readCtx, repository, manifest.Digest, in.RuntimeArtifact.Platform)
	}
	if readErr == nil {
		return ExecuteResult{Started: true, Manifest: manifest}
	}
	if bounded.Err() != nil {
		return ExecuteResult{Started: true, Err: errors.New("BuildKit result unknown; recover the original registry identity")}
	}
	if runErr != nil && errors.Is(readErr, ErrAbsent) {
		return ExecuteResult{Started: true, Err: ErrBuildRejected}
	}
	return ExecuteResult{Started: true, Err: errors.New("registry exporter result unknown")}
}

var ErrBuildRejected = errors.New("BuildKit rejected the build and registry confirms no output")

// Raw BuildKit messages can include URLs, command arguments and secrets. Persist
// only typed vertex/status facts; never forward raw output, error text or logs.
type progressWriter struct {
	pending []byte
	log     func(string)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		idx := strings.IndexByte(string(w.pending), '\n')
		if idx < 0 {
			break
		}
		line := w.pending[:idx]
		w.pending = w.pending[idx+1:]
		var v struct {
			ID             string     `json:"id"`
			Completed      *time.Time `json:"completed"`
			Error          string     `json:"error"`
			Vertex         string     `json:"vertex"`
			Current, Total int64
		}
		if json.Unmarshal(line, &v) == nil {
			if digestPattern.MatchString(v.ID) {
				state := "running"
				if v.Completed != nil {
					state = "completed"
				}
				if v.Error != "" {
					state = "rejected"
				}
				w.log("BuildKit vertex " + v.ID + " " + state)
			} else if digestPattern.MatchString(v.Vertex) && v.Total > 0 {
				w.log(fmt.Sprintf("BuildKit transfer %s %d/%d", v.Vertex, v.Current, v.Total))
			}
		}
	}
	if len(w.pending) > 65536 {
		w.pending = nil
	}
	return len(p), nil
}
