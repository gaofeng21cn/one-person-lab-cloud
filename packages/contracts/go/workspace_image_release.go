package contracts

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const WorkspaceImageReleasesEnv = "OPL_WORKSPACE_IMAGE_RELEASES_JSON"

const (
	// WorkspaceImageTrustSourcePath is the Control Plane route that serves the
	// registered workspace image trust source. Both sides of the call read it
	// from here, so the path cannot drift apart between them.
	WorkspaceImageTrustSourcePath = "/internal/workspace-image-trust-source"
	// WorkspaceImageTrustSourceTokenHeader carries the shared internal service
	// credential on that call.
	WorkspaceImageTrustSourceTokenHeader = "x-opl-service-token"
)

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
	// ReleaseVersion identifies a published release. It is a version identity,
	// not a repository identity.
	ReleaseVersion string `json:"releaseVersion"`
	// ReleaseRepository carries the repository the release was published to.
	// Trusting a release means trusting its repository, so the repository needs
	// its own field rather than being encoded into the version.
	ReleaseRepository string `json:"releaseRepository"`
	ExpectedRevision  int    `json:"expectedRevision"`
	Reason            string `json:"reason"`
}

// WorkspaceImageTrustSource names the repositories whose images may start a
// workspace. It is deliberately separate from the published release catalog:
// the catalog is the version directory, the trust source is the admission
// decision. A repository that is trusted admits every image published to it,
// so a trust source is an admission decision, not a release list.
type WorkspaceImageTrustSource struct {
	SchemaVersion int      `json:"schemaVersion"`
	Revision      int      `json:"revision"`
	Repositories  []string `json:"repositories"`
	References    []string `json:"references"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	UpdatedBy     string   `json:"updatedBy,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

var workspaceImageRepositoryPattern = regexp.MustCompile(`^[a-z0-9.-]+(?::[1-9][0-9]*)?/[a-z0-9._/-]+$`)

// ValidWorkspaceImageRepository reports whether a value is a repository
// reference a trust source may name.
func ValidWorkspaceImageRepository(value string) bool {
	return workspaceImageRepositoryPattern.MatchString(strings.TrimSpace(value))
}

// WorkspaceImageRepository returns the repository part of an image reference.
func WorkspaceImageRepository(value string) string {
	repository, _, ok := strings.Cut(strings.TrimSpace(value), "@")
	if !ok {
		return ""
	}
	return repository
}

// DecodeWorkspaceImageTrustSource reads a stored trust source. An empty input
// is an installation with nothing trusted beyond the installed image.
func DecodeWorkspaceImageTrustSource(raw string) (WorkspaceImageTrustSource, error) {
	source := WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1}
	if strings.TrimSpace(raw) == "" {
		return source, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
	}
	if source.SchemaVersion != 1 || source.Revision < 1 {
		return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
	}
	repositories := map[string]struct{}{}
	for index := range source.Repositories {
		repository := strings.TrimSpace(source.Repositories[index])
		if !ValidWorkspaceImageRepository(repository) {
			return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
		}
		if _, exists := repositories[repository]; exists {
			return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
		}
		repositories[repository] = struct{}{}
		source.Repositories[index] = repository
	}
	references := map[string]struct{}{}
	for index := range source.References {
		reference := strings.TrimSpace(source.References[index])
		if !ValidWorkspaceImageReference(reference) {
			return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
		}
		if _, exists := references[reference]; exists {
			return WorkspaceImageTrustSource{}, errors.New("workspace_image_trust_source_invalid")
		}
		references[reference] = struct{}{}
		source.References[index] = reference
	}
	return source, nil
}

// TrustsRepository reports whether the trust source admits every image
// published to a repository.
func (source WorkspaceImageTrustSource) TrustsRepository(repository string) bool {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return false
	}
	for _, trusted := range source.Repositories {
		if trusted == repository {
			return true
		}
	}
	return false
}

// TrustsReference reports whether the trust source admits one exact image.
func (source WorkspaceImageTrustSource) TrustsReference(reference string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return false
	}
	for _, trusted := range source.References {
		if trusted == reference {
			return true
		}
	}
	return false
}

// TrustsImage reports whether the trust source admits an image reference,
// either as an exact reference or through the repository it was published to.
func (source WorkspaceImageTrustSource) TrustsImage(reference string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return false
	}
	if source.TrustsReference(reference) {
		return true
	}
	return source.TrustsRepository(WorkspaceImageRepository(reference))
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

// ContainsRepository reports whether the catalog names any release published
// to a repository. It is the repository-granular companion of ContainsImage,
// which stays an exact image match: a repository-level question must never
// loosen the exact question the catalog already answers.
func (catalog WorkspaceImageReleaseCatalog) ContainsRepository(repository string) bool {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return false
	}
	for _, release := range catalog.Releases {
		if WorkspaceImageRepository(release.Image) == repository {
			return true
		}
	}
	return false
}
