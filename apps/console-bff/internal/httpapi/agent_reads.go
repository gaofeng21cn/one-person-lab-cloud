package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent reads share the Console's authenticated routes and their owner checks.
// The MCP server is request-scoped: initialization never retains an actor,
// permission, session cookie or owner result for a later HTTP request.
func (s *Server) registerAgentReadRoutes(mux *http.ServeMux) {
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return cloudReadMCP(mux, r)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux.Handle("/api/v2/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !sameOrigin(r) {
			writePublisherError(w, r, 403, "origin_rejected", "same-origin request required")
			return
		}
		if _, err := RequireSession(r.Context(), s.identity, r); err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		// This endpoint contains only reads. SDK POST messages do not reach any
		// command route; quote creation and purchase keep the existing CSRF and
		// idempotency guards in Console.
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		transport.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /api/v2/agent/quotes/{quoteId}/review", func(w http.ResponseWriter, r *http.Request) {
		serveQuoteReview(mux, w, r)
	})
}

type agentPageInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"Owner pagination cursor; omit for the first page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Page size from 1 to 100; defaults to 25"`
}

type agentObjectInput struct {
	ID               string `json:"id" jsonschema:"Exact owner object ID"`
	ExpectedRevision string `json:"expectedRevision,omitempty" jsonschema:"Revision from a previous read; rejects changed owner bytes"`
}

// Fixed tools deliberately accept neither a URL/path nor actor/auth headers.
// Authoritative JSON remains useful in hosts with no UI support.
func cloudReadMCP(routes http.Handler, request *http.Request) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "opl-cloud", Version: "1.0.0"}, nil)
	closed, nonDestructive := false, false
	annotation := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &nonDestructive, OpenWorldHint: &closed}
	for _, tool := range []struct{ name, description, path string }{
		{"cloud_list_compute_plans", "Read compute plans authorized for the current Cloud session. Does not quote or purchase.", "/api/v2/catalog/compute-plans"},
		{"cloud_list_storage_plans", "Read storage plans authorized for the current Cloud session. Does not quote or purchase.", "/api/v2/catalog/storage-plans"},
		{"cloud_list_workspaces", "Read one page of the current session's Workspaces.", "/api/v2/workspaces"},
	} {
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description, Annotations: annotation},
			func(ctx context.Context, _ *mcp.CallToolRequest, input agentPageInput) (*mcp.CallToolResult, any, error) {
				if input.Limit == 0 {
					input.Limit = 25
				}
				if input.Limit < 1 || input.Limit > 100 || len(input.Cursor) > 4096 {
					return nil, nil, fmt.Errorf("invalid_pagination")
				}
				query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "cursor": {input.Cursor}}
				return agentReadResult(routes, request.WithContext(ctx), tool.path+"?"+query.Encode(), "", "", "")
			})
	}
	for _, tool := range []struct{ name, description, path, kind string }{
		{"cloud_get_quote", "Read an existing exact quote and its authenticated browser review path. Quote creation, approval and purchase are not performed.", "/api/v2/quotes/", "quote"},
		{"cloud_get_workspace", "Read the original Workspace and its owner-composed application state.", "/api/v2/workspaces/", "workspace"},
		{"cloud_get_workspace_operation", "Read the original Workspace operation after a response loss. Never starts or retries an operation; unknown remains unknown.", "/api/v2/operations/workspace/", "operation"},
	} {
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description, Annotations: annotation},
			func(ctx context.Context, _ *mcp.CallToolRequest, input agentObjectInput) (*mcp.CallToolResult, any, error) {
				if strings.TrimSpace(input.ID) == "" || len(input.ID) > 256 {
					return nil, nil, fmt.Errorf("invalid_object_id")
				}
				return agentReadResult(routes, request.WithContext(ctx), tool.path+url.PathEscape(input.ID), input.ExpectedRevision, tool.kind, input.ID)
			})
	}
	return server
}

type agentRead struct {
	Source       string          `json:"source"`
	Revision     string          `json:"revision"`
	ObservedAt   string          `json:"observedAt"`
	Data         json.RawMessage `json:"data"`
	ReviewPath   string          `json:"reviewPath,omitempty"`
	ReviewStatus string          `json:"reviewStatus,omitempty"`
}

func agentReadResult(routes http.Handler, request *http.Request, path, expected, kind, expectedID string) (*mcp.CallToolResult, any, error) {
	read, code := readAgentOwner(routes, request, path)
	if code != http.StatusOK {
		return nil, nil, fmt.Errorf("owner_read_failed: HTTP %d; re-read the original object after restoring access", code)
	}
	if expectedID != "" {
		var identity struct {
			ID          string `json:"id"`
			OperationID string `json:"operationId"`
			Owner       string `json:"owner"`
		}
		if json.Unmarshal(read.Data, &identity) != nil || (kind == "operation" && (identity.OperationID != expectedID || identity.Owner != "workspace")) || (kind != "operation" && identity.ID != expectedID) {
			return nil, nil, fmt.Errorf("owner_identity_mismatch")
		}
	}
	if expected != "" && expected != read.Revision {
		return nil, nil, fmt.Errorf("stale_revision: read the original object again")
	}
	if kind == "quote" {
		var quote quoteReview
		if json.Unmarshal(read.Data, &quote) != nil || quote.ID == "" {
			return nil, nil, fmt.Errorf("invalid_quote_readback")
		}
		read.ReviewPath = "/api/v2/agent/quotes/" + url.PathEscape(quote.ID) + "/review?revision=" + url.QueryEscape(read.Revision)
		read.ReviewStatus = quoteReviewStatus(quote, time.Now())
	}
	return nil, read, nil
}

// Dispatch a fixed GET in-process, preserving the caller's request context and
// cookie. The existing route resolves the session and owner authorization anew.
// No network proxy, alternative service token, state cache or domain DTO exists.
func readAgentOwner(routes http.Handler, request *http.Request, path string) (agentRead, int) {
	r := request.Clone(request.Context())
	r.Method = http.MethodGet
	r.URL, _ = url.Parse(path)
	r.RequestURI = path
	r.Body = nil
	r.ContentLength = 0
	r.Header = request.Header.Clone()
	r.Header.Del("Content-Type")
	r.Header.Del("Content-Length")
	response := &agentReadWriter{header: make(http.Header)}
	routes.ServeHTTP(response, r)
	if response.status != http.StatusOK {
		return agentRead{}, response.status
	}
	// Stable object-key ordering, without floating-point conversion of owner
	// numbers, binds browser review and subsequent Agent reads to the same bytes.
	decoder := json.NewDecoder(bytes.NewReader(response.body.Bytes()))
	decoder.UseNumber()
	var data any
	if decoder.Decode(&data) != nil {
		return agentRead{}, http.StatusBadGateway
	}
	canonical, err := json.Marshal(data)
	if err != nil {
		return agentRead{}, http.StatusBadGateway
	}
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256(canonical))
	return agentRead{Source: path, Revision: revision, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: canonical}, http.StatusOK
}

type agentReadWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *agentReadWriter) Header() http.Header { return w.header }
func (w *agentReadWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *agentReadWriter) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(value)
}
