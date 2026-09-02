package contracts

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const WorkspaceImageReleasesEnv = "OPL_WORKSPACE_IMAGE_RELEASES_JSON"

var (
	workspaceImageDigestPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	workspaceImageVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
)

type WorkspaceImageRelease struct {
	Version string `json:"version"`
	Image   string `json:"image"`
}

type WorkspaceImageReleaseCatalog struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Releases      []WorkspaceImageRelease `json:"releases"`
}

type WorkspaceImageReleaseActivationRequest struct {
	ReleaseVersion   string `json:"releaseVersion"`
	ExpectedRevision int    `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type WorkspaceRuntimeImageReplacementPreview struct {
	WorkspaceID        string `json:"workspaceId"`
	WorkspaceStatus    string `json:"workspaceStatus"`
	RuntimeID          string `json:"runtimeId"`
	RuntimeStatus      string `json:"runtimeStatus"`
	CurrentImageDigest string `json:"currentImageDigest"`
	TargetImageDigest  string `json:"targetImageDigest"`
	CanReplace         bool   `json:"canReplace"`
}

type WorkspaceComputeRuntimeBindingStatus string

const (
	WorkspaceComputeRuntimeBindingMatched    WorkspaceComputeRuntimeBindingStatus = "matched"
	WorkspaceComputeRuntimeBindingMismatched WorkspaceComputeRuntimeBindingStatus = "mismatched"
)

type WorkspaceComputeRuntimeBinding struct {
	Status WorkspaceComputeRuntimeBindingStatus `json:"status"`
}

func ValidWorkspaceImageReference(value string) bool {
	value = strings.TrimSpace(value)
	repository, digest, ok := strings.Cut(value, "@")
	return ok && repository != "" && !strings.Contains(repository, "@") && workspaceImageDigestPattern.MatchString(digest)
}

func WorkspaceImageDigest(value string) string {
	_, digest, ok := strings.Cut(strings.TrimSpace(value), "@")
	if !ok || !workspaceImageDigestPattern.MatchString(digest) {
		return ""
	}
	return digest
}

func DecodeWorkspaceImageReleaseCatalog(raw, installedImage string) (WorkspaceImageReleaseCatalog, error) {
	installedImage = strings.TrimSpace(installedImage)
	if !ValidWorkspaceImageReference(installedImage) {
		return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_installed_image_invalid")
	}
	if strings.TrimSpace(raw) == "" {
		return WorkspaceImageReleaseCatalog{
			SchemaVersion: 1,
			Releases:      []WorkspaceImageRelease{{Version: "installed", Image: installedImage}},
		}, nil
	}

	var catalog WorkspaceImageReleaseCatalog
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
	}
	if catalog.SchemaVersion != 1 || len(catalog.Releases) == 0 || len(catalog.Releases) > 32 {
		return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
	}
	versions, images := map[string]struct{}{}, map[string]struct{}{}
	installedFound := false
	for index := range catalog.Releases {
		release := &catalog.Releases[index]
		release.Version = strings.TrimSpace(release.Version)
		release.Image = strings.TrimSpace(release.Image)
		if !workspaceImageVersionPattern.MatchString(release.Version) || !ValidWorkspaceImageReference(release.Image) {
			return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
		}
		if _, exists := versions[release.Version]; exists {
			return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
		}
		if _, exists := images[release.Image]; exists {
			return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_catalog_invalid")
		}
		versions[release.Version], images[release.Image] = struct{}{}, struct{}{}
		installedFound = installedFound || release.Image == installedImage
	}
	if !installedFound {
		return WorkspaceImageReleaseCatalog{}, errors.New("workspace_image_release_installed_image_missing")
	}
	return catalog, nil
}

func (catalog WorkspaceImageReleaseCatalog) ReleaseForImage(image string) (WorkspaceImageRelease, bool) {
	for _, release := range catalog.Releases {
		if release.Image == image {
			return release, true
		}
	}
	return WorkspaceImageRelease{}, false
}

func (catalog WorkspaceImageReleaseCatalog) ContainsImage(image string) bool {
	_, found := catalog.ReleaseForImage(image)
	return found
}
