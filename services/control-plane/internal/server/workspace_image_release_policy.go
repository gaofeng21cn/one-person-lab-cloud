package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	workspaceImageReleasePolicyID     = "workspace-image-release-policy"
	workspaceImageReleasePolicyAction = "workspace.image_release_policy"
)

var (
	errWorkspaceImageReleasePolicyUnavailable = errors.New("workspace_image_release_policy_unavailable")
	errWorkspaceImageReleaseRequestInvalid    = errors.New("workspace_image_release_request_invalid")
	errWorkspaceImageReleaseConflict          = errors.New("workspace_image_release_conflict")
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

type workspaceImageReleaseMutation struct {
	Base        workspaceImageReleasePolicy
	Catalog     contracts.WorkspaceImageReleaseCatalog
	Target      contracts.WorkspaceImageRelease
	Expected    int
	Reason      string
	UpdatedBy   string
	RequestHash string
	AuditEvent  map[string]any
}

type workspaceImageReleaseMutationStore interface {
	ApplyWorkspaceImageReleaseMutation(context.Context, workspaceImageReleaseMutation) (workspaceImageReleasePolicy, error)
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

func workspaceImageReleasePolicyRow(policy workspaceImageReleasePolicy, createdAt string) map[string]any {
	encoded, _ := json.Marshal(policy)
	row := map[string]any{
		"id": workspaceImageReleasePolicyID, "operationId": workspaceImageReleasePolicyID,
		"resourceId": workspaceImageReleasePolicyID, "resourceKind": "workspace_image_release_policy",
		"action": workspaceImageReleasePolicyAction, "provider": "control-plane", "status": "active", "result": string(encoded),
	}
	if createdAt != "" {
		row["createdAt"] = createdAt
	}
	return row
}

func prepareWorkspaceImageReleaseMutation(current workspaceImageReleasePolicy, mutation workspaceImageReleaseMutation, now time.Time) (workspaceImageReleasePolicy, error) {
	if err := validateWorkspaceImageReleasePolicy(current, mutation.Catalog); err != nil || mutation.Expected != current.Revision ||
		mutation.Target.Version == "" || !mutation.Catalog.ContainsImage(mutation.Target.Image) || mutation.Reason == "" || mutation.UpdatedBy == "" || mutation.RequestHash == "" {
		return workspaceImageReleasePolicy{}, errWorkspaceImageReleaseConflict
	}
	if current.ActiveImage == mutation.Target.Image && current.ActiveVersion == mutation.Target.Version {
		return current, nil
	}
	return workspaceImageReleasePolicy{
		SchemaVersion: 1,
		Revision:      current.Revision + 1,
		ActiveVersion: mutation.Target.Version,
		ActiveImage:   mutation.Target.Image,
		UpdatedAt:     now.UTC().Format(time.RFC3339Nano),
		UpdatedBy:     mutation.UpdatedBy,
		Reason:        mutation.Reason,
	}, nil
}

func workspaceImageReleaseAudit(mutation workspaceImageReleaseMutation, before, after workspaceImageReleasePolicy) map[string]any {
	audit := cloneMap(mutation.AuditEvent)
	audit["before"] = before
	audit["after"] = map[string]any{"requestHash": mutation.RequestHash, "policy": after}
	audit["result"] = "succeeded"
	return audit
}

func workspaceImageReleaseReplay(audit map[string]any, mutation workspaceImageReleaseMutation) (workspaceImageReleasePolicy, error) {
	if audit == nil || stringValue(audit["action"]) != "workspace.image_release.activate" || stringValue(audit["resourceId"]) != workspaceImageReleasePolicyID {
		return workspaceImageReleasePolicy{}, errWorkspaceImageReleaseConflict
	}
	after := mapField(audit, "after")
	if stringValue(after["requestHash"]) != mutation.RequestHash {
		return workspaceImageReleasePolicy{}, errIdempotencyConflict
	}
	encoded, _ := json.Marshal(after["policy"])
	var policy workspaceImageReleasePolicy
	if json.Unmarshal(encoded, &policy) != nil || validateWorkspaceImageReleasePolicy(policy, mutation.Catalog) != nil {
		return workspaceImageReleasePolicy{}, errWorkspaceImageReleasePolicyUnavailable
	}
	return policy, nil
}

func (app *controlPlaneServer) getWorkspaceImageReleasePolicy(w http.ResponseWriter, r *http.Request) {
	policy, catalog, installed, err := app.currentWorkspaceImageReleasePolicy(r.Context())
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", workspaceImageReleasePolicyResponse(policy, catalog, installed), policy.UpdatedAt)
}

func (app *controlPlaneServer) activateWorkspaceImageRelease(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok || !validBillingReviewOpaqueID(key) {
		if ok {
			writeError(w, http.StatusBadRequest, errWorkspaceImageReleaseRequestInvalid.Error())
		}
		return
	}
	input := decodeJSON(r)
	if !exactWorkspaceComputeClaimKeys(input, []string{"releaseVersion", "expectedRevision", "reason"}) {
		writeError(w, http.StatusBadRequest, errWorkspaceImageReleaseRequestInvalid.Error())
		return
	}
	expectedRevision := numberField(input, "expectedRevision", 0)
	request := contracts.WorkspaceImageReleaseActivationRequest{
		ReleaseVersion:   stringValue(input["releaseVersion"]),
		ExpectedRevision: int(expectedRevision),
		Reason:           stringValue(input["reason"]),
	}
	if request.ReleaseVersion != strings.TrimSpace(request.ReleaseVersion) || request.ReleaseVersion == "" || request.ExpectedRevision < 1 || math.Trunc(expectedRevision) != expectedRevision ||
		request.Reason != strings.TrimSpace(request.Reason) || request.Reason == "" || len(request.Reason) > 256 {
		writeError(w, http.StatusBadRequest, errWorkspaceImageReleaseRequestInvalid.Error())
		return
	}
	policy, catalog, installed, err := app.currentWorkspaceImageReleasePolicy(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errWorkspaceImageReleasePolicyUnavailable.Error())
		return
	}
	var target contracts.WorkspaceImageRelease
	for _, release := range catalog.Releases {
		if release.Version == request.ReleaseVersion {
			target = release
			break
		}
	}
	if target.Version == "" {
		writeError(w, http.StatusConflict, errWorkspaceImageReleaseConflict.Error())
		return
	}
	user, _ := app.sessionUserContext(r)
	requestHash := stableID("workspace-image-release-activation", request.ReleaseVersion, target.Image, strconv.Itoa(request.ExpectedRevision), request.Reason)
	audit := app.auditEvent(r, "workspace.image_release.activate", "workspace_image_release_policy", workspaceImageReleasePolicyID, "", policy, nil, "started")
	audit["id"] = "audit-" + stableID("workspace-image-release-activation", key)[:12]
	mutation := workspaceImageReleaseMutation{
		Base: policy, Catalog: catalog, Target: target, Expected: request.ExpectedRevision, Reason: request.Reason,
		UpdatedBy: stringValue(user["id"]), RequestHash: requestHash, AuditEvent: audit,
	}
	store, ok := app.tables.(workspaceImageReleaseMutationStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, errWorkspaceImageReleasePolicyUnavailable.Error())
		return
	}
	unlock, err := app.lockResourceContext(r.Context(), "workspace-image-release-policy", workspaceImageReleasePolicyID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errWorkspaceImageReleasePolicyUnavailable.Error())
		return
	}
	defer unlock()
	activated, err := store.ApplyWorkspaceImageReleaseMutation(r.Context(), mutation)
	if err != nil {
		switch {
		case errors.Is(err, errIdempotencyConflict), errors.Is(err, errWorkspaceImageReleaseConflict):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, errWorkspaceImageReleasePolicyUnavailable.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, workspaceImageReleasePolicyResponse(activated, catalog, installed))
}
