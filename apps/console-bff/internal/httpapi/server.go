// Package httpapi serves the BFF's same-origin REST surface.
//
// The BFF owns no business data. Every value it serves comes from the Cloud owner
// that wrote it, reached over the typed gRPC contract, so a missing or failing
// owner is reported as an upstream failure rather than replaced by a default.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// Server serves the Console BFF REST surface over typed owner reads.
type Server struct {
	reader OwnerReader
}

// OwnerReader is the typed read surface the BFF needs. It is satisfied by the
// gRPC client set, and by a test double in unit tests.
type OwnerReader interface {
	DeliveryReader
	Operation(ctx context.Context, owner owneridentity.Owner, operationID string) (*api.Operation, error)
}

// NewServer returns the BFF REST server over the supplied owner reader.
func NewServer(reader OwnerReader) *Server { return &Server{reader: reader} }

// Handler returns the BFF's same-origin REST handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v2/delivery/{workspaceId}", s.handleDelivery)
	mux.HandleFunc("GET /api/v2/operations/{owner}/{operationId}", s.handleOperation)
	return mux
}

func (s *Server) handleDelivery(w http.ResponseWriter, r *http.Request) {
	if s.reader == nil {
		writeError(w, http.StatusInternalServerError, "bff_unconfigured", "owner reader is not configured")
		return
	}
	view, err := s.deliveryView(r.Context(), r.PathValue("workspaceId"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "owner_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleOperation routes to the finite owner enum the contract defines. An owner
// outside that enum is a client error; the BFF never scans services for the id.
func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	if s.reader == nil {
		writeError(w, http.StatusInternalServerError, "bff_unconfigured", "owner reader is not configured")
		return
	}
	owner, ok := ownerFromPath(r.PathValue("owner"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_owner", fmt.Sprintf("%q is not a Cloud operation owner", r.PathValue("owner")))
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	if operationID == "" {
		writeError(w, http.StatusBadRequest, "operation_id_required", "operation id is required")
		return
	}
	operation, err := s.reader.Operation(r.Context(), owner, operationID)
	if err != nil {
		if errors.Is(err, errOwnerUnconfigured) {
			writeError(w, http.StatusServiceUnavailable, "owner_unconfigured", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "owner_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

// ownerFromPath resolves the path owner token against the contract's finite
// OperationOwner set. An owner outside that enum is a client error; the BFF never
// scans services for the id, and it does not invent an enum member the contract
// does not define.
func ownerFromPath(value string) (owneridentity.Owner, bool) {
	owner, ok := owneridentity.Parse(value)
	if !ok {
		return "", false
	}
	switch owner {
	case owneridentity.Tenant, owneridentity.Capability, owneridentity.Build,
		owneridentity.Workspace, owneridentity.RuntimeControl, owneridentity.Fabric,
		owneridentity.Gateway, owneridentity.ResourceCatalog:
		return owner, true
	default:
		return "", false
	}
}

// errOwnerUnconfigured reports that a required owner is not reachable in this
// deployment. It maps to 503 without inventing the missing owner's facts.
var errOwnerUnconfigured = errors.New("domain owner is not configured")

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": code, "safeMessage": message})
}
