package clients

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Sub2APIWorkspaceKeyRevokeInput struct {
	UserID            int64  `json:"user_id"`
	KeyID             int64  `json:"key_id"`
	ExactName         string `json:"exact_name"`
	LaunchOperationID string `json:"launch_operation_id"`
}

type Sub2APIWorkspaceKeyRevokeClient interface {
	WorkspaceKeysForRevocation(context.Context, int64, string) ([]Sub2APIWorkspaceKey, error)
	RevokeWorkspaceKey(context.Context, Sub2APIWorkspaceKeyRevokeInput) error
}

// WorkspaceKeysForRevocation obtains the original owner/key/name identity even
// when the key is disabled. It never returns credentials or asserts usability.
func (c *Sub2APIHTTPClient) WorkspaceKeysForRevocation(ctx context.Context, userID int64, name string) ([]Sub2APIWorkspaceKey, error) {
	if !strings.HasPrefix(name, "opl-workspace-") {
		return nil, errors.New("invalid sub2api workspace key revocation lookup")
	}
	return c.workspaceKeyIdentityRefs(ctx, userID, name)
}

type sub2APIWorkspaceKeyRevocation struct {
	Sub2APIWorkspaceKeyRevokeInput
	Capability string     `json:"capability"`
	Absent     *bool      `json:"absent"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// RevokeWorkspaceKey uses service authorization and confirms the same original
// launch identity with a separate owner read. HTTP 404 is never absence evidence.
func (c *Sub2APIHTTPClient) RevokeWorkspaceKey(ctx context.Context, input Sub2APIWorkspaceKeyRevokeInput) error {
	if input.UserID <= 0 || input.KeyID < 0 || !validWorkspaceKeyLookupName(input.ExactName) ||
		!strings.HasPrefix(input.ExactName, "opl-workspace-") || len(input.ExactName) > 100 ||
		!strings.HasPrefix(input.LaunchOperationID, "workspace-launch-") || len(input.LaunchOperationID) > 128 ||
		strings.TrimSpace(input.LaunchOperationID) != input.LaunchOperationID {
		return errors.New("invalid sub2api workspace key revocation input")
	}
	operationCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	path := "/api/v1/admin/api-keys/" + strconv.FormatInt(input.KeyID, 10) + "/workspace-revocation"
	body, err := c.doAuthenticated(operationCtx, http.MethodPost, path, input, input.LaunchOperationID+":revoke-key:"+strconv.FormatInt(input.KeyID, 10))
	if err != nil {
		return err
	}
	if err := validateSub2APIWorkspaceRevocation(body, input); err != nil {
		return err
	}
	query := url.Values{
		"user_id": {strconv.FormatInt(input.UserID, 10)}, "exact_name": {input.ExactName}, "launch_operation_id": {input.LaunchOperationID},
	}
	body, err = c.doAuthenticated(operationCtx, http.MethodGet, path+"?"+query.Encode(), nil, "")
	if err != nil {
		return err
	}
	return validateSub2APIWorkspaceRevocation(body, input)
}

func validateSub2APIWorkspaceRevocation(body []byte, input Sub2APIWorkspaceKeyRevokeInput) error {
	var result sub2APIWorkspaceKeyRevocation
	if err := decodeSub2APIEnvelope(body, &result); err != nil {
		return err
	}
	if result.Capability != "workspace_key_revocation_v1" || result.Sub2APIWorkspaceKeyRevokeInput != input ||
		result.Absent == nil || !*result.Absent || result.RevokedAt == nil || result.RevokedAt.IsZero() {
		return errors.New("unverified sub2api workspace key revocation")
	}
	return nil
}
