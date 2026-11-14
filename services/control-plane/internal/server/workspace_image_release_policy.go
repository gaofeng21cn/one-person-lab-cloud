package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	workspaceImageReleasePolicyID     = "workspace-image-release-policy"
	workspaceImageReleasePolicyAction = "workspace.image_release_policy"
)

var (
	errWorkspaceImageReleasePolicyUnavailable = errors.New("workspace_image_release_policy_unavailable")
)

type workspaceImageReleasePolicy struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      int    `json:"revision"`
	ActiveVersion string `json:"activeVersion"`
	ActiveImage   string `json:"activeImage"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type workspaceImageReleaseDTO struct {
	Version string `json:"version"`
	Image   string `json:"image"`
	Digest  string `json:"digest"`
}

type workspaceImageReleasePolicyDTO struct {
	SchemaVersion    int                        `json:"schemaVersion"`
	Revision         int                        `json:"revision"`
	Active           workspaceImageReleaseDTO   `json:"active"`
	InstalledDefault workspaceImageReleaseDTO   `json:"installedDefault"`
	Releases         []workspaceImageReleaseDTO `json:"releases"`
	Source           string                     `json:"source"`
	UpdatedAt        string                     `json:"updatedAt,omitempty"`
	UpdatedBy        string                     `json:"updatedBy,omitempty"`
	Reason           string                     `json:"reason,omitempty"`
}

func configuredWorkspaceImageReleaseCatalog() (contracts.WorkspaceImageReleaseCatalog, contracts.WorkspaceImageRelease, error) {
	installedImage := currentWorkspaceImageDigest()
	catalog, err := contracts.DecodeWorkspaceImageReleaseCatalog(os.Getenv(contracts.WorkspaceImageReleasesEnv), installedImage)
	if err != nil {
		return contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, err
	}
	installed, found := catalog.ReleaseForImage(installedImage)
	if !found {
		return contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, errWorkspaceImageReleasePolicyUnavailable
	}
	return catalog, installed, nil
}

func defaultWorkspaceImageReleasePolicy(installed contracts.WorkspaceImageRelease) workspaceImageReleasePolicy {
	return workspaceImageReleasePolicy{
		SchemaVersion: 1,
		Revision:      1,
		ActiveVersion: installed.Version,
		ActiveImage:   installed.Image,
	}
}

func decodeWorkspaceImageReleasePolicyRow(row map[string]any) (workspaceImageReleasePolicy, error) {
	if stringValue(row["id"]) != workspaceImageReleasePolicyID || stringValue(row["operationId"]) != workspaceImageReleasePolicyID ||
		stringValue(row["resourceId"]) != workspaceImageReleasePolicyID || stringValue(row["resourceKind"]) != "workspace_image_release_policy" ||
		stringValue(row["action"]) != workspaceImageReleasePolicyAction || stringValue(row["status"]) != "active" {
		return workspaceImageReleasePolicy{}, errWorkspaceImageReleasePolicyUnavailable
	}
	var policy workspaceImageReleasePolicy
	if json.Unmarshal([]byte(stringValue(row["result"])), &policy) != nil || policy.SchemaVersion != 1 || policy.Revision < 1 ||
		strings.TrimSpace(policy.ActiveVersion) == "" || !contracts.ValidWorkspaceImageReference(policy.ActiveImage) {
		return workspaceImageReleasePolicy{}, errWorkspaceImageReleasePolicyUnavailable
	}
	return policy, nil
}

func validateWorkspaceImageReleasePolicy(policy workspaceImageReleasePolicy, catalog contracts.WorkspaceImageReleaseCatalog) error {
	release, found := catalog.ReleaseForImage(policy.ActiveImage)
	if !found || release.Version != policy.ActiveVersion || policy.SchemaVersion != 1 || policy.Revision < 1 {
		return errWorkspaceImageReleasePolicyUnavailable
	}
	return nil
}

func (app *controlPlaneServer) currentWorkspaceImageReleasePolicy(ctx context.Context) (workspaceImageReleasePolicy, contracts.WorkspaceImageReleaseCatalog, contracts.WorkspaceImageRelease, error) {
	catalog, installed, err := configuredWorkspaceImageReleaseCatalog()
	if err != nil {
		return workspaceImageReleasePolicy{}, contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, err
	}
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceImageReleasePolicyID)
	if err != nil {
		return workspaceImageReleasePolicy{}, contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, err
	}
	policy := defaultWorkspaceImageReleasePolicy(installed)
	if found {
		policy, err = decodeWorkspaceImageReleasePolicyRow(row)
		if err != nil {
			return workspaceImageReleasePolicy{}, contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, err
		}
	}
	if err := validateWorkspaceImageReleasePolicy(policy, catalog); err != nil {
		return workspaceImageReleasePolicy{}, contracts.WorkspaceImageReleaseCatalog{}, contracts.WorkspaceImageRelease{}, err
	}
	return policy, catalog, installed, nil
}

func workspaceImageReleasePolicyResponse(policy workspaceImageReleasePolicy, catalog contracts.WorkspaceImageReleaseCatalog, installed contracts.WorkspaceImageRelease) workspaceImageReleasePolicyDTO {
	releases := make([]workspaceImageReleaseDTO, 0, len(catalog.Releases))
	for _, release := range catalog.Releases {
		releases = append(releases, workspaceImageReleaseDTO{Version: release.Version, Image: release.Image, Digest: contracts.WorkspaceImageDigest(release.Image)})
	}
	return workspaceImageReleasePolicyDTO{
		SchemaVersion: 1,
		Revision:      policy.Revision,
		Active: workspaceImageReleaseDTO{
			Version: policy.ActiveVersion,
			Image:   policy.ActiveImage,
			Digest:  contracts.WorkspaceImageDigest(policy.ActiveImage),
		},
		InstalledDefault: workspaceImageReleaseDTO{Version: installed.Version, Image: installed.Image, Digest: contracts.WorkspaceImageDigest(installed.Image)},
		Releases:         releases,
		Source:           "control-plane-workspace-image-release-policy",
		UpdatedAt:        policy.UpdatedAt,
		UpdatedBy:        policy.UpdatedBy,
		Reason:           policy.Reason,
	}
}

func (app *controlPlaneServer) getWorkspaceImageReleasePolicy(w http.ResponseWriter, r *http.Request) {
	policy, catalog, installed, err := app.currentWorkspaceImageReleasePolicy(r.Context())
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", workspaceImageReleasePolicyResponse(policy, catalog, installed), policy.UpdatedAt)
}
