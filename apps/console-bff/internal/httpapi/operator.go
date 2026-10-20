package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// OperatorReader is the BFF's typed read surface for the operator console
// surfaces. The retained Control Plane owner answers them; the BFF decides only
// whether the caller is a CloudIdentity platform operator.
type OperatorReader interface {
	OperatorOverview(context.Context, string) ([]byte, error)
	OperatorAccounts(context.Context, string, int, int) ([]byte, error)
	OperatorWorkspaces(context.Context, string, int, int) ([]byte, error)
	OperatorWorkspace(context.Context, string, string) ([]byte, error)
	OperatorWorkspaceRuntimeImagePolicy(context.Context, string) ([]byte, error)
	OperatorAnnouncements(context.Context, string, int, int) ([]byte, error)
	OperatorWorkspaceRuntimeImageReplacementPreview(context.Context, string, string, string) ([]byte, error)
	OperatorHealth(context.Context, string) ([]byte, error)
	OperatorRuntimeObservations(context.Context, string) ([]byte, error)
	OperatorReconciliation(context.Context, string, int, int) ([]byte, error)
	OperatorWorkspaceLaunchRecovery(context.Context, string, string) ([]byte, error)
	OperatorWalletAdjustment(context.Context, string, string) ([]byte, error)
	OperatorWorkspaceRuntimeImageReplacement(context.Context, string, string, string) ([]byte, error)
	OperatorApplicationDeployment(context.Context, string, string) ([]byte, error)
	OperatorRegistryRepositories(context.Context, string, string) ([]byte, error)
	OperatorRegistryTags(context.Context, string, string, string) ([]byte, error)
}

const (
	operatorDefaultPageSize = 20
	operatorMaxPageSize     = 50
)

// operatorPrivilegeAction is the existing platform-admin-only action the
// console has always used to identify an operator. CloudIdentity derives it from
// the configured platform-admin subject list, so an ALLOWED decision is the
// operator fact; a tenant administrator is denied it.
const operatorPrivilegeAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPUBLISHERNAMESPACE

var (
	errInvalidOperatorPage = errors.New("invalid operator pagination")
	errInvalidOperatorRead = errors.New("invalid operator read")
)

func (s *Server) registerOperatorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/operator/overview", s.operatorReadHandler("overview", "control-plane"))
	mux.HandleFunc("GET /api/v2/operator/accounts", s.operatorReadHandler("accounts", "control-plane+sub2api"))
	mux.HandleFunc("GET /api/v2/operator/workspaces", s.operatorReadHandler("workspaces", "control-plane+fabric+sub2api"))
	mux.HandleFunc("GET /api/v2/operator/workspaces/{workspaceId}", s.operatorReadHandler("workspace", "control-plane+fabric+ledger"))
	mux.HandleFunc("GET /api/v2/operator/workspace-runtime-image-policy", s.operatorReadHandler("policy", "control-plane"))
	mux.HandleFunc("GET /api/v2/operator/announcements", s.operatorReadHandler("announcements", "control-plane"))
	mux.HandleFunc("GET /api/v2/operator/workspaces/{workspaceId}/runtime-image-replacements/preview", s.operatorReadHandler("preview", "control-plane+fabric"))
	mux.HandleFunc("GET /api/v2/operator/health", s.operatorReadHandler("Health", "control-plane"))
	mux.HandleFunc("GET /api/v2/operator/runtime-observations", s.operatorReadHandler("RuntimeObservations", "control-plane+fabric"))
	mux.HandleFunc("GET /api/v2/operator/reconciliation", s.operatorReadHandler("Reconciliation", "control-plane"))
	mux.HandleFunc("GET /api/v2/operator/workspace-launches/{operationId}/recovery", s.operatorReadHandler("WorkspaceLaunchRecovery", ""))
	mux.HandleFunc("GET /api/v2/operator/wallet-adjustments/{operationId}", s.operatorReadHandler("WalletAdjustment", ""))
	mux.HandleFunc("GET /api/v2/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}", s.operatorReadHandler("WorkspaceRuntimeImageReplacement", ""))
	mux.HandleFunc("GET /api/v2/operator/application-deployments/{operationId}", s.operatorReadHandler("ApplicationDeployment", ""))
	mux.HandleFunc("GET /api/v2/operator/registry/repositories", s.operatorReadHandler("RegistryRepositories", ""))
	mux.HandleFunc("GET /api/v2/operator/registry/tags/{namespace}/{repository}", s.operatorReadHandler("RegistryTags", ""))
}

// operatorReadHandler resolves the live CloudIdentity session, requires an
// explicit CloudIdentity ALLOWED decision for the platform-operator action, and
// only then reads the retained owner. The owner is reached with the browser's
// own session reference; the browser's actor, tenant or cookie never selects
// another identity.
func (s *Server) operatorReadHandler(kind, source string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "private, no-store")
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		page, pageSize := 1, operatorDefaultPageSize
		replacementImageDigest := ""
		switch kind {
		case "accounts", "workspaces", "announcements", "Reconciliation":
			if page, pageSize, err = operatorPage(r); err != nil {
				writePublisherError(w, r, 400, "invalid_pagination", "one page and pageSize are required")
				return
			}
		case "RegistryRepositories":
			query, parseErr := url.ParseQuery(r.URL.RawQuery)
			if parseErr != nil || r.ContentLength != 0 {
				writePublisherError(w, r, 400, "invalid_operator_read", "invalid registry selector")
				return
			}
			for key, values := range query {
				if key != "namespace" || len(values) != 1 {
					writePublisherError(w, r, 400, "invalid_operator_read", "invalid registry selector")
					return
				}
			}
		case "preview":
			if r.ContentLength != 0 {
				writePublisherError(w, r, 400, "invalid_operator_read", "only the declared operator read is accepted")
				return
			}
			if replacementImageDigest, err = operatorReplacementImageDigest(r); err != nil {
				writePublisherError(w, r, 400, "invalid_operator_read", "only the declared operator read is accepted")
				return
			}
		default:
			if r.URL.RawQuery != "" || r.ContentLength != 0 {
				writePublisherError(w, r, 400, "invalid_operator_read", "only the declared operator read is accepted")
				return
			}
		}
		workspaceID := ""
		if kind == "workspace" || kind == "preview" {
			workspaceID = strings.TrimSpace(r.PathValue("workspaceId"))
			if workspaceID == "" || workspaceID != r.PathValue("workspaceId") {
				writePublisherError(w, r, 400, "invalid_workspace_id", "Workspace id is required")
				return
			}
		}
		if s.operator == nil {
			writePublisherError(w, r, 503, "owner_unconfigured", "operator owner is not configured")
			return
		}
		// Operator surfaces are platform-wide. The authorization scope is the
		// session's own, with the tenant removed exactly as the admin publisher
		// routes do: a tenant administrator must not inherit platform scope.
		caller.Session = proto.Clone(caller.Session).(*api.Session)
		caller.Session.TenantId = nil
		ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
		if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Capability,
			operatorPrivilegeAction,
			&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
			clients.CallContext(ctx).GetRequestId()); err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		raw, err := s.operatorRead(ctx, kind, caller.SessionID, workspaceID, replacementImageDigest, page, pageSize, r)
		if err != nil {
			writeOwnerCallFailure(w, r, err)
			return
		}
		if (source != "" && !validOperatorEnvelope(raw, source)) || (source == "" && !validOperatorObject(raw)) {
			writePublisherError(w, r, 502, "invalid_owner_response", "invalid operator owner response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
}

func (s *Server) operatorRead(ctx context.Context, kind, session, workspaceID, replacementImageDigest string, page, pageSize int, r *http.Request) ([]byte, error) {
	switch kind {
	case "overview":
		return s.operator.OperatorOverview(ctx, session)
	case "accounts":
		return s.operator.OperatorAccounts(ctx, session, page, pageSize)
	case "workspaces":
		return s.operator.OperatorWorkspaces(ctx, session, page, pageSize)
	case "workspace":
		return s.operator.OperatorWorkspace(ctx, session, workspaceID)
	case "policy":
		return s.operator.OperatorWorkspaceRuntimeImagePolicy(ctx, session)
	case "announcements":
		return s.operator.OperatorAnnouncements(ctx, session, page, pageSize)
	case "preview":
		return s.operator.OperatorWorkspaceRuntimeImageReplacementPreview(ctx, session, workspaceID, replacementImageDigest)
	case "Health":
		return s.operator.OperatorHealth(ctx, session)
	case "RuntimeObservations":
		return s.operator.OperatorRuntimeObservations(ctx, session)
	case "Reconciliation":
		return s.operator.OperatorReconciliation(ctx, session, page, pageSize)
	case "WorkspaceLaunchRecovery":
		return s.operator.OperatorWorkspaceLaunchRecovery(ctx, session, r.PathValue("operationId"))
	case "WalletAdjustment":
		return s.operator.OperatorWalletAdjustment(ctx, session, r.PathValue("operationId"))
	case "WorkspaceRuntimeImageReplacement":
		return s.operator.OperatorWorkspaceRuntimeImageReplacement(ctx, session, r.PathValue("workspaceId"), r.PathValue("operationId"))
	case "ApplicationDeployment":
		return s.operator.OperatorApplicationDeployment(ctx, session, r.PathValue("operationId"))
	case "RegistryRepositories":
		return s.operator.OperatorRegistryRepositories(ctx, session, r.URL.Query().Get("namespace"))
	case "RegistryTags":
		return s.operator.OperatorRegistryTags(ctx, session, r.PathValue("namespace"), r.PathValue("repository"))
	default:
		return nil, status.Error(codes.Internal, "unknown operator read")
	}
}

// operatorReplacementImageDigest admits only the optional target the retained
// preview writer itself reads. The image catalog membership decision stays with
// the owner; the BFF only refuses undeclared selectors.
func operatorReplacementImageDigest(r *http.Request) (string, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", errInvalidOperatorRead
	}
	digest := ""
	for key, values := range query {
		if key != "replacementImageDigest" || len(values) != 1 {
			return "", errInvalidOperatorRead
		}
		digest = values[0]
		if digest == "" || digest != strings.TrimSpace(digest) || len(digest) > 512 {
			return "", errInvalidOperatorRead
		}
	}
	return digest, nil
}

func operatorPage(r *http.Request) (int, int, error) {
	page, pageSize := 1, operatorDefaultPageSize
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, 0, errInvalidOperatorPage
	}
	for key, values := range query {
		if (key != "page" && key != "pageSize") || len(values) != 1 {
			return 0, 0, errInvalidOperatorPage
		}
		switch key {
		case "page":
			page, err = strconv.Atoi(values[0])
		case "pageSize":
			pageSize, err = strconv.Atoi(values[0])
		}
		if err != nil || page < 1 || pageSize < 1 || pageSize > operatorMaxPageSize {
			return 0, 0, errInvalidOperatorPage
		}
	}
	return page, pageSize, nil
}

// validOperatorEnvelope is the minimum retained-owner proof the BFF forwards: a
// declared source, a non-unavailable status with data, or an explicitly
// unavailable source with a reason code and no data.
func validOperatorEnvelope(raw []byte, source string) bool {
	var envelope struct {
		Source     string          `json:"source"`
		Status     string          `json:"status"`
		Available  bool            `json:"available"`
		FetchedAt  string          `json:"fetchedAt"`
		ReasonCode string          `json:"reasonCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Source != source || envelope.FetchedAt == "" {
		return false
	}
	switch envelope.Status {
	case "available", "empty":
		return envelope.Available && envelope.ReasonCode == "" && len(envelope.Data) != 0
	case "unavailable":
		return !envelope.Available && envelope.ReasonCode != "" && envelope.Data == nil
	default:
		return false
	}
}

func validOperatorObject(raw []byte) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && len(value) > 0
}
