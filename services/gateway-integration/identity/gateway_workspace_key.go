package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const managedKeyExactNamePrefix = "opl-workspace-"

func workspaceKeyExactName(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return managedKeyExactNamePrefix + hex.EncodeToString(digest[:])[:12]
}

// GatewayWorkspaceKeyIssuer creates a Workspace key through the existing
// user-scoped Gateway API. Gateway remains the authority for the Key and Group;
// this adapter only maps the Cloud Workspace identity to that external API.
type GatewayWorkspaceKeyIssuer struct {
	gateway *Gateway
	group   string
}

// NewGatewayWorkspaceKeyIssuer builds the Cloud adapter for one existing,
// user-visible Gateway group. It does not require or accept a service token.
func NewGatewayWorkspaceKeyIssuer(gateway *Gateway, groupName string) (*GatewayWorkspaceKeyIssuer, error) {
	group := strings.TrimSpace(groupName)
	if gateway == nil {
		return nil, fmt.Errorf("Gateway client is required")
	}
	if group == "" {
		return nil, fmt.Errorf("the Workspace Gateway group is required")
	}
	return &GatewayWorkspaceKeyIssuer{gateway: gateway, group: group}, nil
}

func (*GatewayWorkspaceKeyIssuer) requiresDelegatedCredential() bool { return true }
func (*GatewayWorkspaceKeyIssuer) supportsIdempotentRetry() bool     { return true }

// IssueWorkspaceKey creates one external Key under the existing Codex Group.
// The Gateway's user idempotency store receives the Cloud operation identity, so
// a lost response is replayed by Gateway rather than creating another Key.
// ModelIDs intentionally remain empty: effective routing and model permission
// belong to the external Group, not to a second Cloud allowlist.
func (i *GatewayWorkspaceKeyIssuer) IssueWorkspaceKey(ctx context.Context, request ManagedKeyIssueRequest) (ManagedKeyIssueResult, error) {
	if i == nil || i.gateway == nil {
		return ManagedKeyIssueResult{}, status.Error(codes.FailedPrecondition, "no Gateway user key issuer is configured")
	}
	delegation := request.Delegation
	if delegation == nil || strings.TrimSpace(delegation.Bearer) == "" || delegation.Subject != request.Subject || !delegation.ExpiresAt.After(time.Now()) {
		return ManagedKeyIssueResult{}, reauthenticationRequired()
	}
	if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.WorkspaceID) == "" || strings.TrimSpace(request.ExactName) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return ManagedKeyIssueResult{}, status.Error(codes.InvalidArgument, "a Gateway subject, Workspace, key name and idempotency key are required")
	}
	subjectID, err := parseGatewaySubject(request.Subject)
	if err != nil {
		return ManagedKeyIssueResult{}, err
	}
	groupID, err := i.resolveGroup(ctx, delegation, request.GroupName)
	if err != nil {
		return ManagedKeyIssueResult{}, err
	}
	body := map[string]any{"name": request.ExactName, "group_id": groupID}
	payload, err := i.gateway.requestWithHeaders(ctx, "/api/v1/keys", body, delegation.Bearer, map[string]string{"Idempotency-Key": request.IdempotencyKey})
	if err != nil {
		return ManagedKeyIssueResult{}, mapGatewayUserWriteError(err)
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 || len(envelope.Data) == 0 {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "Gateway returned an invalid key creation response")
	}
	var key struct {
		ID     int64  `json:"id"`
		UserID int64  `json:"user_id"`
		Raw    string `json:"key"`
		Name   string `json:"name"`
		Group  *int64 `json:"group_id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(envelope.Data, &key) != nil || key.ID <= 0 || key.UserID != subjectID || strings.TrimSpace(key.Raw) == "" || key.Name != request.ExactName || key.Group == nil || *key.Group != groupID || key.Status != "active" {
		return ManagedKeyIssueResult{}, status.Error(codes.Unavailable, "Gateway returned an invalid created key identity")
	}
	return ManagedKeyIssueResult{Raw: key.Raw, ExternalKeyID: strconv.FormatInt(key.ID, 10), GroupID: groupID}, nil
}

func (i *GatewayWorkspaceKeyIssuer) resolveGroup(ctx context.Context, delegation *ManagedKeyDelegation, requested string) (int64, error) {
	groupName := strings.TrimSpace(requested)
	if groupName == "" {
		groupName = i.group
	}
	data, err := i.gateway.requestWithHeaders(ctx, "/api/v1/groups/available", nil, delegation.Bearer, nil)
	if err != nil {
		return 0, mapGatewayUserReadError(err)
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 || len(envelope.Data) == 0 {
		return 0, status.Error(codes.Unavailable, "Gateway returned an invalid Group response")
	}
	var groups []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if json.Unmarshal(envelope.Data, &groups) != nil {
		return 0, status.Error(codes.Unavailable, "Gateway returned an invalid Group response")
	}
	var found int64
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Name), groupName) {
			if group.ID <= 0 || (group.Status != "" && group.Status != "active") || found != 0 {
				return 0, status.Error(codes.FailedPrecondition, "Gateway returned an unusable or ambiguous Workspace Group")
			}
			found = group.ID
		}
	}
	if found == 0 {
		return 0, status.Errorf(codes.FailedPrecondition, "Gateway Group %q is not available to the logged-in user", groupName)
	}
	return found, nil
}

func mapGatewayUserReadError(err error) error {
	var upstream *gatewayHTTPError
	if err != nil && errors.As(err, &upstream) {
		switch upstream.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return reauthenticationRequired()
		case http.StatusNotFound:
			return status.Error(codes.NotFound, "Gateway Group endpoint not found")
		case http.StatusTooManyRequests:
			return status.Error(codes.ResourceExhausted, "Gateway Group read rate limited")
		}
	}
	return status.Error(codes.Unavailable, "Gateway Group read unavailable")
}

func mapGatewayUserWriteError(err error) error {
	var upstream *gatewayHTTPError
	if err != nil && errors.As(err, &upstream) {
		switch upstream.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return reauthenticationRequired()
		case http.StatusNotFound:
			return status.Error(codes.NotFound, "Gateway key endpoint not found")
		case http.StatusConflict:
			return status.Error(codes.AlreadyExists, "Gateway refused the Workspace key identity")
		case http.StatusTooManyRequests:
			return status.Error(codes.ResourceExhausted, "Gateway key creation rate limited")
		}
	}
	return status.Error(codes.Unavailable, "Gateway key creation outcome is unknown")
}

// RevokeWorkspaceKey remains unavailable because this closeout only needs the
// existing reversible user-key lifecycle. Permanent key revocation remains a
// separate owner decision and must not be inferred from Workspace deletion.
func (*GatewayWorkspaceKeyIssuer) RevokeWorkspaceKey(context.Context, string, string) error {
	return status.Error(codes.Unimplemented, "no approved Gateway user-key revocation operation is configured")
}

func (i *GatewayWorkspaceKeyIssuer) SetWorkspaceKeyState(ctx context.Context, subject, externalKeyID string, state ManagedKeyExternalState) error {
	keyID, delegation, err := i.lifecycleRequest(ctx, subject, externalKeyID)
	if err != nil {
		return err
	}
	statusValue := "inactive"
	if state == ManagedKeyExternalActive {
		statusValue = "active"
	} else if state != ManagedKeyExternalDisabled {
		return status.Error(codes.InvalidArgument, "unsupported Gateway key lifecycle state")
	}
	_, err = i.gateway.requestWithMethod(ctx, http.MethodPut, "/api/v1/keys/"+keyID, map[string]string{"status": statusValue}, delegation.Bearer, nil)
	if err != nil {
		return mapGatewayUserWriteError(err)
	}
	return nil
}

func (i *GatewayWorkspaceKeyIssuer) ReadWorkspaceKeyState(ctx context.Context, subject, externalKeyID string) (ManagedKeyExternalState, error) {
	keyID, delegation, err := i.lifecycleRequest(ctx, subject, externalKeyID)
	if err != nil {
		return "", err
	}
	key, err := i.readWorkspaceKey(ctx, delegation, subject, keyID)
	if status.Code(err) == codes.NotFound {
		return ManagedKeyExternalRevoked, nil
	}
	if err != nil {
		return "", err
	}
	switch key.Status {
	case "active":
		return ManagedKeyExternalActive, nil
	case "disabled", "inactive":
		return ManagedKeyExternalDisabled, nil
	default:
		return "", status.Error(codes.Unavailable, "Gateway returned an unsupported Workspace key state")
	}
}

func (i *GatewayWorkspaceKeyIssuer) lifecycleRequest(ctx context.Context, subject, externalKeyID string) (string, *ManagedKeyDelegation, error) {
	if i == nil || i.gateway == nil {
		return "", nil, status.Error(codes.FailedPrecondition, "no Gateway user key issuer is configured")
	}
	keyID, err := strconv.ParseInt(strings.TrimSpace(externalKeyID), 10, 64)
	if err != nil || keyID <= 0 || strconv.FormatInt(keyID, 10) != externalKeyID {
		return "", nil, status.Error(codes.InvalidArgument, "a positive Gateway key id is required")
	}
	if _, err = parseGatewaySubject(subject); err != nil {
		return "", nil, err
	}
	delegation, err := managedKeyDelegationFromContext(ctx, subject)
	if err != nil {
		return "", nil, err
	}
	return strconv.FormatInt(keyID, 10), delegation, nil
}

func (i *GatewayWorkspaceKeyIssuer) readWorkspaceKey(ctx context.Context, delegation *ManagedKeyDelegation, subject, keyID string) (struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Status string `json:"status"`
}, error) {
	var key struct {
		ID     int64  `json:"id"`
		UserID int64  `json:"user_id"`
		Status string `json:"status"`
	}
	payload, err := i.gateway.requestWithMethod(ctx, http.MethodGet, "/api/v1/keys/"+keyID, nil, delegation.Bearer, nil)
	if err != nil {
		var upstream *gatewayHTTPError
		if errors.As(err, &upstream) && upstream.StatusCode == http.StatusNotFound {
			return key, status.Error(codes.NotFound, "Gateway Workspace key not found")
		}
		return key, mapGatewayUserReadError(err)
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	subjectID, _ := parseGatewaySubject(subject)
	if json.Unmarshal(payload, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 || len(envelope.Data) == 0 ||
		json.Unmarshal(envelope.Data, &key) != nil || key.ID <= 0 || strconv.FormatInt(key.ID, 10) != keyID || key.UserID != subjectID {
		return key, status.Error(codes.Unavailable, "Gateway returned an invalid Workspace key identity")
	}
	return key, nil
}
