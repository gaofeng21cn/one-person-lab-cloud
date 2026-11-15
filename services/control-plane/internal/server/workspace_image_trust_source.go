package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// The workspace image trust source is the admission decision for images: which
// repositories may start a workspace. It is not the published release catalog —
// the catalog stays the version directory, and this is a separate fact with its
// own revision and audit trail.
//
// The source lives in the Control Plane's own store, so registering a
// repository needs no environment edit and no allowlist file edit anywhere.

const (
	workspaceImageTrustSourceID     = "workspace-image-trust-source"
	workspaceImageTrustSourceAction = "workspace.image_trust_source"
	workspaceImageTrustSourceKind   = "workspace_image_trust_source"
)

var errWorkspaceImageTrustSourceUnavailable = errors.New("workspace_image_trust_source_unavailable")

type workspaceImageTrustSourceDTO struct {
	SchemaVersion int      `json:"schemaVersion"`
	Revision      int      `json:"revision"`
	Repositories  []string `json:"repositories"`
	References    []string `json:"references"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	UpdatedBy     string   `json:"updatedBy,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// defaultWorkspaceImageTrustSource is an installation that trusts nothing
// beyond the images it was installed with.
func defaultWorkspaceImageTrustSource() contracts.WorkspaceImageTrustSource {
	return contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 1}
}

func decodeWorkspaceImageTrustSourceRow(row map[string]any) (contracts.WorkspaceImageTrustSource, error) {
	if stringValue(row["id"]) != workspaceImageTrustSourceID || stringValue(row["operationId"]) != workspaceImageTrustSourceID ||
		stringValue(row["resourceId"]) != workspaceImageTrustSourceID || stringValue(row["resourceKind"]) != workspaceImageTrustSourceKind ||
		stringValue(row["action"]) != workspaceImageTrustSourceAction || stringValue(row["status"]) != "active" {
		return contracts.WorkspaceImageTrustSource{}, errWorkspaceImageTrustSourceUnavailable
	}
	source, err := contracts.DecodeWorkspaceImageTrustSource(stringValue(row["result"]))
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, errWorkspaceImageTrustSourceUnavailable
	}
	return source, nil
}

func (app *controlPlaneServer) currentWorkspaceImageTrustSource(ctx context.Context) (contracts.WorkspaceImageTrustSource, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceImageTrustSourceID)
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, err
	}
	if !found {
		return defaultWorkspaceImageTrustSource(), nil
	}
	return decodeWorkspaceImageTrustSourceRow(row)
}

func workspaceImageTrustSourceResponse(source contracts.WorkspaceImageTrustSource) workspaceImageTrustSourceDTO {
	repositories := source.Repositories
	if repositories == nil {
		repositories = []string{}
	}
	references := source.References
	if references == nil {
		references = []string{}
	}
	return workspaceImageTrustSourceDTO{
		SchemaVersion: 1,
		Revision:      source.Revision,
		Repositories:  repositories,
		References:    references,
		UpdatedAt:     source.UpdatedAt,
		UpdatedBy:     source.UpdatedBy,
		Reason:        source.Reason,
	}
}

// requireInternalServiceToken authenticates a service-to-service call. The
// credential is the one the Control Plane and the Fabric owner already share
// as their capability key, so neither side is handed a new secret for this
// route. Every registration it accepts is audited and revisioned.
func (app *controlPlaneServer) requireInternalServiceToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(os.Getenv(contracts.WorkspaceImageTrustSourceTokenEnv))
		provided := strings.TrimSpace(r.Header.Get(contracts.WorkspaceImageTrustSourceTokenHeader))
		if len(expected) < owneridentity.MinimumTokenLength || !bearerCredentialMatches(provided, expected) {
			writeError(w, http.StatusUnauthorized, "service_token_invalid")
			return
		}
		next(w, r)
	}
}

// bearerCredentialMatches compares a presented bearer credential with the
// expected one without leaking either through the comparison's timing.
func bearerCredentialMatches(provided, expected string) bool {
	want := sha256.Sum256([]byte("Bearer " + expected))
	got := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// registerWorkspaceImageTrustSourceRoutes installs the trust source surface.
// The registration is a service call, not an operator action: the publisher
// side registers a repository once its build succeeded, and no human edits an
// environment file or an allowlist to make that image startable.
//
// The route patterns are written out literally because the route ownership
// check reads the registrations with Go's parser: a pattern it cannot resolve
// is a route it cannot see, and an invisible route is an unowned surface. The
// shared contract constant is pinned against these literals by a guard test.
func registerWorkspaceImageTrustSourceRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	mux.HandleFunc("GET /internal/workspace-image-trust-source", app.requireInternalServiceToken(app.getWorkspaceImageTrustSource))
	mux.HandleFunc("PUT /internal/workspace-image-trust-source", app.requireInternalServiceToken(app.putWorkspaceImageTrustSource))
}

func (app *controlPlaneServer) getWorkspaceImageTrustSource(w http.ResponseWriter, r *http.Request) {
	source, err := app.currentWorkspaceImageTrustSource(r.Context())
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", workspaceImageTrustSourceResponse(source), source.UpdatedAt)
}

// putWorkspaceImageTrustSource registers a repository as trusted. The
// registration is idempotent: registering a repository that is already trusted
// returns the current source without moving its revision.
func (app *controlPlaneServer) putWorkspaceImageTrustSource(w http.ResponseWriter, r *http.Request) {
	var request contracts.WorkspaceImageReleaseActivationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "workspace_image_trust_source_request_invalid")
		return
	}
	repository := strings.TrimSpace(request.ReleaseRepository)
	version := strings.TrimSpace(request.ReleaseVersion)
	reason := strings.TrimSpace(request.Reason)
	if !contracts.ValidWorkspaceImageRepository(repository) || version == "" || reason == "" {
		writeError(w, http.StatusBadRequest, "workspace_image_trust_source_request_invalid")
		return
	}

	current, err := app.currentWorkspaceImageTrustSource(r.Context())
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	if request.ExpectedRevision != current.Revision {
		writeError(w, http.StatusConflict, "workspace_image_trust_source_revision_conflict")
		return
	}
	if current.TrustsRepository(repository) {
		// Already trusted: the same registration twice is the same fact.
		w.Header().Set("Cache-Control", "private, no-store")
		writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", workspaceImageTrustSourceResponse(current), current.UpdatedAt)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated := current
	updated.Repositories = append(append([]string{}, current.Repositories...), repository)
	updated.Revision = current.Revision + 1
	updated.UpdatedAt = now
	updated.UpdatedBy = "workspace_image_trust_source"
	updated.Reason = reason
	encoded, err := json.Marshal(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_image_trust_source_write_failed")
		return
	}
	if err := app.tables.SaveRuntimeOperation(r.Context(), map[string]any{
		"id": workspaceImageTrustSourceID, "operationId": workspaceImageTrustSourceID,
		"resourceId": workspaceImageTrustSourceID, "resourceKind": workspaceImageTrustSourceKind,
		"action": workspaceImageTrustSourceAction, "provider": "control-plane", "status": "active",
		"result": string(encoded), "createdAt": now,
	}); err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	event := app.auditEvent(r, workspaceImageTrustSourceAction, workspaceImageTrustSourceKind, workspaceImageTrustSourceID, "", nil,
		map[string]any{"repository": repository, "releaseVersion": version, "reason": reason, "revision": updated.Revision}, "succeeded")
	// The audit row is keyed by its id, and the default id is derived from a
	// second-resolution timestamp. Two registrations inside one second would
	// therefore overwrite each other and the trail would lose a decision. The
	// revision is the identity of a registration, so it belongs in the id.
	event["id"] = "audit-" + stableID(workspaceImageTrustSourceAction, workspaceImageTrustSourceKind, workspaceImageTrustSourceID, strconv.Itoa(updated.Revision))[:12]
	event["createdAt"] = now
	if err := app.tables.SaveAuditEvent(r.Context(), event); err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", workspaceImageTrustSourceResponse(updated), updated.UpdatedAt)
}
