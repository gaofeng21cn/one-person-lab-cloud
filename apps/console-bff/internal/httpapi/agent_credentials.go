package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	api "opl-cloud/packages/contracts/go/api"
)

const agentCredentialAudience = "opl-cloud-mcp"

// AgentCredentialReader is the existing CloudIdentity owner's typed credential
// surface. The BFF holds no token persistence or authorization policy.
type AgentCredentialReader interface {
	IssueAgentCredential(context.Context, *api.IssueAgentCredentialRpcRequest) (*api.IssuedAgentCredential, error)
	ResolveAgentCredential(context.Context, *api.ResolveAgentCredentialRequest) (*api.ResolvedAgentCredential, error)
	ListAgentCredentials(context.Context, *api.ListAgentCredentialsRpcRequest) (*api.AgentCredentialPage, error)
	RevokeAgentCredential(context.Context, *api.RevokeAgentCredentialRpcRequest) error
}

// Only the MCP HTTP boundary can create this request-local context. Neither a
// public header nor an MCP tool argument can supply the resolved owner reference.
type agentCredentialContextKey struct{}

func agentBearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 512 || r.Header.Get("Cookie") != "" {
		return "", ErrSessionRequired
	}
	parts := strings.Split(values[0], " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" || strings.ContainsAny(parts[1], "\t\r\n,") {
		return "", ErrSessionRequired
	}
	return parts[1], nil
}

func resolveAgentCaller(ctx context.Context, identity IdentityReader, credential string) (Caller, error) {
	reader, ok := identity.(AgentCredentialReader)
	if !ok {
		return Caller{}, ErrSessionRequired
	}
	resolved, err := reader.ResolveAgentCredential(ctx, &api.ResolveAgentCredentialRequest{Credential: credential, Audience: agentCredentialAudience})
	if err != nil {
		return Caller{}, err
	}
	if resolved.GetSession().GetActorId() == "" || !strings.HasPrefix(resolved.GetSessionReference(), "agent:") ||
		resolved.GetMetadata().GetId() == "" || resolved.GetMetadata().GetAudience() != agentCredentialAudience ||
		resolved.GetSession().GetCsrfToken() != "" || resolved.GetMetadata().GetExpiresAt() == nil ||
		!resolved.GetMetadata().GetExpiresAt().AsTime().After(time.Now()) ||
		resolved.GetSession().GetTenantId() != resolved.GetMetadata().GetTenantId() {
		return Caller{}, ErrSessionRequired
	}
	return Caller{Session: resolved.Session, SessionID: resolved.SessionReference}, nil
}

type agentCredentialMetadata struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	TenantID  string `json:"tenantId,omitempty"`
	Audience  string `json:"audience"`
	IssuedAt  string `json:"issuedAt"`
	ExpiresAt string `json:"expiresAt"`
	RevokedAt string `json:"revokedAt,omitempty"`
}

func credentialMetadata(value *api.AgentCredential) agentCredentialMetadata {
	out := agentCredentialMetadata{ID: value.GetId(), Label: value.GetLabel(), TenantID: value.GetTenantId(), Audience: value.GetAudience(),
		IssuedAt: value.GetIssuedAt().AsTime().UTC().Format(time.RFC3339Nano), ExpiresAt: value.GetExpiresAt().AsTime().UTC().Format(time.RFC3339Nano)}
	if value.GetRevokedAt() != nil {
		out.RevokedAt = value.GetRevokedAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	return out
}

func (s *Server) registerAgentCredentialRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/agent/connections", s.agentConnectionsPage)
	for _, pattern := range []string{"GET /api/v2/auth/agent-credentials", "POST /api/v2/auth/agent-credentials", "DELETE /api/v2/auth/agent-credentials/{credentialId}"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			publisherRequestID(r)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			reader, ok := s.identity.(AgentCredentialReader)
			if !ok {
				writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity agent credentials unavailable")
				return
			}
			caller, err := RequireSession(r.Context(), s.identity, r)
			if err != nil {
				writePublisherIdentityError(w, r, err)
				return
			}
			if r.Method != http.MethodGet && (!sameOrigin(r) || caller.Session.GetCsrfToken() == "" || subtle.ConstantTimeCompare([]byte(caller.Session.GetCsrfToken()), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
				writePublisherError(w, r, 403, "csrf_required", "same-origin session CSRF required")
				return
			}
			call := &api.CallContext{SessionId: &caller.SessionID, ActorId: caller.Session.GetActorId(), Scope: sessionScope(caller.Session), RequestId: r.Header.Get(requestIDHeader)}
			switch r.Method {
			case http.MethodGet:
				page, err := reader.ListAgentCredentials(r.Context(), &api.ListAgentCredentialsRpcRequest{Context: call})
				if err != nil {
					writePublisherIdentityError(w, r, err)
					return
				}
				items := make([]agentCredentialMetadata, 0, len(page.GetItems()))
				for _, item := range page.GetItems() {
					items = append(items, credentialMetadata(item))
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items})
			case http.MethodPost:
				content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || content != "application/json" {
					writePublisherError(w, r, 415, "json_required", "application/json required")
					return
				}
				var input struct {
					Label string `json:"label"`
				}
				decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
					writePublisherError(w, r, 400, "invalid_request", "valid credential label required")
					return
				}
				out, err := reader.IssueAgentCredential(r.Context(), &api.IssueAgentCredentialRpcRequest{Context: call, Label: input.Label})
				if err != nil {
					writePublisherIdentityError(w, r, err)
					return
				}
				if out.GetCredential() == "" || out.GetMetadata().GetId() == "" {
					writePublisherError(w, r, 502, "invalid_owner_response", "CloudIdentity credential response invalid")
					return
				}
				// Secret bytes appear only in this explicit authenticated download.
				// Metadata reads, logs, MCP tool output and the page DOM never include it.
				w.Header().Set("Content-Disposition", `attachment; filename="opl-cloud-credential.json"`)
				writeJSON(w, http.StatusCreated, struct {
					agentCredentialMetadata
					Credential string `json:"credential"`
				}{credentialMetadata(out.Metadata), out.Credential})
			case http.MethodDelete:
				if err := reader.RevokeAgentCredential(r.Context(), &api.RevokeAgentCredentialRpcRequest{Context: call, CredentialId: r.PathValue("credentialId")}); err != nil {
					writePublisherIdentityError(w, r, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}
		})
	}
}
