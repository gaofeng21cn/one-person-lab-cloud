package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// Sub2API's service issuance endpoint is the one approved way for this owner to
// mint a Workspace-managed Gateway key without holding a customer credential. The
// route is authenticated with the deployment's own service token, is bounded to
// the caller-supplied Tenant wallet subject, Workspace, launch identity and group,
// and is idempotent by (subject, exact name): a replay returns the original key
// rather than a second one.
const sub2apiWorkspaceKeyPath = "/api/v1/service/workspace-keys"

// managedKeyExactNamePrefix is the legacy Workspace reserved key-name convention:
// one deterministic name per Workspace, so a replayed issuance names the same key.
const managedKeyExactNamePrefix = "opl-workspace-"

// managedKeyModelIDPattern is the bounded model-id shape this owner accepts from
// the issuance authority. A resolved scope is never empty, never a wildcard and
// never an unbounded string.
var managedKeyModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:-]{0,79}$`)

const managedKeyResolvedModelLimit = 64

// Sub2APIWorkspaceKeyIssuer issues Workspace-managed Gateway keys through the
// Sub2API owner's service endpoint.
type Sub2APIWorkspaceKeyIssuer struct {
	base       string
	token      string
	group      string
	httpClient *http.Client
}

// NewSub2APIWorkspaceKeyIssuer builds the issuance client. All three facts are
// required together: without them the Gateway managed-key surface must fail
// closed rather than fall back to another issuance path.
func NewSub2APIWorkspaceKeyIssuer(rawURL, serviceToken, groupName string) (*Sub2APIWorkspaceKeyIssuer, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, fmt.Errorf("explicit HTTPS Sub2API URL required; loopback HTTP allowed for isolated tests")
	}
	token := strings.TrimSpace(serviceToken)
	if len(token) < owneridentity.MinimumTokenLength {
		return nil, fmt.Errorf("the Sub2API service token must contain at least %d characters", owneridentity.MinimumTokenLength)
	}
	group := strings.TrimSpace(groupName)
	if group == "" {
		return nil, fmt.Errorf("the approved Workspace key group is required")
	}
	return &Sub2APIWorkspaceKeyIssuer{base: strings.TrimRight(strings.TrimSpace(rawURL), "/"), token: token, group: group,
		httpClient: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// workspaceKeyExactName is the deterministic reserved name of one Workspace's
// managed key: "opl-workspace-" plus the first 12 hex of the Workspace id digest.
// The name is stable for the Workspace, so the issuance authority can recognise a
// replay of the same launch and return the original key.
func workspaceKeyExactName(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return managedKeyExactNamePrefix + hex.EncodeToString(digest[:])[:12]
}

// IssueWorkspaceKey mints (or reads back) one Workspace key for the declared
// intent. An empty declared model list is sent as the declared default scope; the
// authority resolves the approved concrete scope and returns it. A transport
// failure or a malformed success body is an unresolved outcome (Unavailable) that
// must never be repeated as a new issuance; a listed refusal is definite.
func (i *Sub2APIWorkspaceKeyIssuer) IssueWorkspaceKey(ctx context.Context, request ManagedKeyIssueRequest) (ManagedKeyIssueResult, error) {
	if i == nil || i.httpClient == nil {
		return ManagedKeyIssueResult{}, status.Error(codes.FailedPrecondition, "no approved Gateway key issuer is configured")
	}
	if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.WorkspaceID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return ManagedKeyIssueResult{}, status.Error(codes.InvalidArgument, "a wallet subject, workspace and idempotency key are required to issue a managed key")
	}
	subjectID, err := parseGatewaySubject(request.Subject)
	if err != nil {
		return ManagedKeyIssueResult{}, err
	}
	body := map[string]any{
		"user_id":             subjectID,
		"workspace_id":        request.WorkspaceID,
		"launch_operation_id": request.LaunchOperationID,
		"exact_name":          request.ExactName,
		"group_name":          request.GroupName,
		"model_ids":           append([]string{}, request.ModelIDs...),
		"idempotency_key":     request.IdempotencyKey,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ManagedKeyIssueResult{}, status.Error(codes.InvalidArgument, "the managed key issuance request cannot be encoded")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, i.base+sub2apiWorkspaceKeyPath, bytes.NewReader(raw))
	if err != nil {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "the managed key issuance request cannot be built")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+i.token)
	response, err := i.httpClient.Do(httpRequest)
	if err != nil {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "the managed key issuance authority is unavailable")
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "the managed key issuance response is unreadable")
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
		Data    struct {
			KeyID    int64    `json:"key_id"`
			APIKey   string   `json:"api_key"`
			GroupID  int64    `json:"group_id"`
			ModelIDs []string `json:"model_ids"`
			Replayed bool     `json:"replayed"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "the managed key issuance authority returned an unreadable response")
	}
	if response.StatusCode != http.StatusOK || envelope.Code != 0 {
		return ManagedKeyIssueResult{}, sub2apiIssuanceRefusal(response.StatusCode, envelope.Reason, envelope.Message)
	}
	resolved, err := validateIssuedWorkspaceKey(envelope.Data.KeyID, envelope.Data.APIKey, envelope.Data.GroupID, envelope.Data.ModelIDs)
	if err != nil {
		// The response is a success envelope that violates the approved contract.
		// The key may exist, so the outcome stays unresolved: the original command
		// is never re-dispatched and the identity must be reconciled by its
		// original issue identity rather than being re-issued.
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "the managed key issuance authority returned an invalid key identity")
	}
	return ManagedKeyIssueResult{Raw: envelope.Data.APIKey, ExternalKeyID: fmt.Sprintf("%d", envelope.Data.KeyID), GroupID: envelope.Data.GroupID,
		ModelIDs: resolved, Replayed: envelope.Data.Replayed}, nil
}

// RevokeWorkspaceKey retires one externally issued key. The approved service
// route is issuance-only: this issuer has no approved revocation path yet and
// reports the missing capability instead of inventing an administrative call.
func (i *Sub2APIWorkspaceKeyIssuer) RevokeWorkspaceKey(context.Context, string, string) error {
	return status.Error(codes.Unimplemented, "no approved Sub2API service revocation endpoint is configured")
}

// sub2apiIssuanceRefusal maps one refusal envelope to the status the owner
// records. The listed reasons are definite: they were answered before any key
// effect. An unknown status or reason stays Unavailable so it is never mistaken
// for a refusal and never triggers a second issuance.
func sub2apiIssuanceRefusal(httpStatus int, reason, message string) error {
	switch reason {
	case "workspace_key_issuance_binding_conflict":
		return status.Error(codes.AlreadyExists, "the Workspace key identity is bound to a different launch: "+reason)
	case "workspace_key_issuance_key_unavailable":
		return status.Error(codes.FailedPrecondition, "the originally issued Workspace key can no longer be read back: "+reason)
	case "workspace_key_model_scope_unresolved", "workspace_key_model_not_allowed", "workspace_key_group_unresolved":
		return status.Error(codes.FailedPrecondition, "the approved Workspace key model scope was refused: "+reason)
	case "service_token_not_configured":
		// The service is not configured to authenticate this caller, so no key
		// effect happened. It is reported as a definite refusal of this command
		// rather than an ambiguous success that could be re-issued.
		return status.Error(codes.FailedPrecondition, "the managed key issuance authority is not configured for service issuance: "+reason)
	}
	switch httpStatus {
	case http.StatusUnauthorized:
		return status.Error(codes.Unauthenticated, "the managed key issuance authority rejected the service identity")
	case http.StatusForbidden:
		return status.Error(codes.PermissionDenied, "the managed key issuance authority denied the service identity")
	}
	if reason != "" {
		return status.Errorf(codes.Unavailable, "the managed key issuance authority refused the request (%d %s)", httpStatus, reason)
	}
	if message != "" {
		return status.Errorf(codes.Unavailable, "the managed key issuance authority refused the request (%d %s)", httpStatus, message)
	}
	return status.Errorf(codes.Unavailable, "the managed key issuance authority refused the request (%d)", httpStatus)
}

// validateIssuedWorkspaceKey enforces the issuance contract on a success envelope:
// a positive key id, a non-empty raw value, a positive group and a resolved,
// bounded, non-wildcard model list. A contract violation fails closed.
func validateIssuedWorkspaceKey(keyID int64, apiKey string, groupID int64, modelIDs []string) ([]string, error) {
	if keyID <= 0 || strings.TrimSpace(apiKey) == "" || groupID <= 0 {
		return nil, fmt.Errorf("issued key identity is incomplete")
	}
	return validateResolvedWorkspaceKeyModels(modelIDs)
}

// validateResolvedWorkspaceKeyModels enforces the resolved-scope contract at every
// boundary that consumes an issuance result: the scope is present, bounded,
// deduplicated, and each id is a concrete non-wildcard identifier.
func validateResolvedWorkspaceKeyModels(modelIDs []string) ([]string, error) {
	if len(modelIDs) == 0 || len(modelIDs) > managedKeyResolvedModelLimit {
		return nil, fmt.Errorf("a resolved model scope is required")
	}
	resolved := make([]string, 0, len(modelIDs))
	seen := map[string]bool{}
	for _, id := range modelIDs {
		trimmed := strings.TrimSpace(id)
		if !managedKeyModelIDPattern.MatchString(trimmed) || strings.Contains(trimmed, "*") || seen[trimmed] {
			return nil, fmt.Errorf("a resolved model id violates the approved scope shape")
		}
		seen[trimmed] = true
		resolved = append(resolved, trimmed)
	}
	return resolved, nil
}
