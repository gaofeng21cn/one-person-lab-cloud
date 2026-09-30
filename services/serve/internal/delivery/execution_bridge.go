package delivery

// fabricApplicationBridge is the declared migration source for the one execution
// provider Serve does not yet run in-process: the Local-Docker installation. It
// reaches the Fabric signed application-runtime surface with the exact scoped
// capability that surface admits and carries no delivery state of its own.
//
// It exists only while that provider's executor is still Fabric's. No installation
// may declare this boundary together with its own cluster facts, because that would
// be two writers for one workload; configureAgentExecution refuses it.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/serve/internal/tkeapply"
)

type fabricApplicationBridge struct {
	BaseURL       string
	Token         string
	CapabilityKey string
	Client        *http.Client
}

func (b *fabricApplicationBridge) Configured() error {
	if b == nil || strings.TrimSpace(b.BaseURL) == "" || strings.TrimSpace(b.Token) == "" || len(b.CapabilityKey) < 32 {
		return status.Error(codes.Unavailable, "Serve Agent execution credentials are not configured")
	}
	return nil
}

func (b *fabricApplicationBridge) EnsureWorkspaceApplicationRuntime(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput, _ tkeapply.Placement) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var observation contracts.WorkspaceApplicationRuntimeObservation
	response, err := b.post(ctx, "/fabric/workspace-application-runtimes", "create_workspace_application_runtime", input, input.RuntimeOperationID, input.AccountID, input.WorkspaceID)
	if err != nil {
		return observation, err
	}
	defer response.Body.Close()
	if err := decodeBridgeResponse(response, &observation); err != nil {
		return observation, err
	}
	return observation, nil
}

func (b *fabricApplicationBridge) ReadWorkspaceApplicationRuntime(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var observation contracts.WorkspaceApplicationRuntimeObservation
	response, err := b.post(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/readback", "read_workspace_application_runtime", input, input.RuntimeOperationID, input.AccountID, input.WorkspaceID)
	if err != nil {
		return observation, err
	}
	defer response.Body.Close()
	if err := decodeBridgeResponse(response, &observation); err != nil {
		return observation, err
	}
	return observation, nil
}

func (b *fabricApplicationBridge) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput, desired string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	return b.lifecycle(ctx, input, desired, "lifecycle", "set_workspace_application_runtime_lifecycle")
}

func (b *fabricApplicationBridge) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	return b.lifecycle(ctx, input, "", "lifecycle-readback", "read_workspace_application_runtime_lifecycle")
}

func (b *fabricApplicationBridge) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	var credentials contracts.WorkspaceApplicationRuntimeCredentials
	response, err := b.post(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/credentials", "read_workspace_application_runtime_credentials", lifecycleWire(input, "running"), input.RuntimeOperationID, input.AccountID, input.WorkspaceID)
	if err != nil {
		return credentials, err
	}
	defer response.Body.Close()
	if err := decodeBridgeResponse(response, &credentials); err != nil {
		return credentials, err
	}
	return credentials, nil
}

func (b *fabricApplicationBridge) lifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeInput, desired, endpoint, action string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	var result contracts.WorkspaceApplicationRuntimeLifecycleResult
	response, err := b.post(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/"+endpoint, action, lifecycleWire(input, desired), input.RuntimeOperationID, input.AccountID, input.WorkspaceID)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if err := decodeBridgeResponse(response, &result); err != nil {
		return result, err
	}
	return result, nil
}

// lifecycleWire restates the migration-source lifecycle handle. Only opaque runtime
// identity travels: Fabric resolves the exact creation input from its own records,
// so the bridge never re-states application input.
func lifecycleWire(input contracts.WorkspaceApplicationRuntimeInput, desired string) contracts.WorkspaceApplicationRuntimeLifecycleInput {
	return contracts.WorkspaceApplicationRuntimeLifecycleInput{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID,
		RuntimeID:          contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID),
		RuntimeOperationID: input.RuntimeOperationID, DesiredState: desired,
	}
}

// post signs and sends one Fabric capability request for the exact typed input. The
// account and workspace scope the capability to the exact Workspace, so the bridge
// cannot address another owner's runtime.
func (b *fabricApplicationBridge) post(ctx context.Context, path, action string, payload any, operationID, accountID, workspaceID string) (*http.Response, error) {
	if err := b.Configured(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	claims := struct {
		Version      int    `json:"version"`
		Caller       string `json:"caller"`
		AccountID    string `json:"accountId"`
		WorkspaceID  string `json:"workspaceId"`
		ResourceKind string `json:"resourceKind"`
		ResourceID   string `json:"resourceId"`
		Action       string `json:"action"`
		OperationID  string `json:"operationId"`
		ExpiresAt    int64  `json:"expiresAt"`
		BodySHA256   string `json:"bodySha256"`
	}{1, "serve", accountID, workspaceID, "workspace_application_runtime", workspaceID, action, operationID, time.Now().Add(time.Minute).Unix(), hex.EncodeToString(sum[:])}
	payloadBytes, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(b.CapabilityKey))
	mac.Write([]byte(encoded))
	req.Header.Set("Authorization", "Bearer "+b.Token)
	req.Header.Set("Idempotency-Key", operationID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OPL-Fabric-Capability", encoded+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return client.Do(req)
}

// decodeBridgeResponse refuses a boundary response that is not the exact typed
// observation. Pending is a 2xx with the live observation, so only a transport or
// decode failure is an error here.
func decodeBridgeResponse(response *http.Response, target any) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return status.Errorf(codes.Unavailable, "Agent execution boundary returned HTTP %d", response.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target) != nil {
		return status.Error(codes.FailedPrecondition, "Agent execution boundary readback is not decodable")
	}
	return nil
}
