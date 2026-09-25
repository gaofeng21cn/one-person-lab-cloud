package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// FabricApplicationAdapter reuses Fabric's existing provider application engine.
// Its dedicated Serve transport and signature are admitted only on application
// start/readback paths; they cannot buy, delete, or mutate resource allocations.
type FabricApplicationAdapter struct {
	BaseURL, Token, CapabilityKey string
	Client                        *http.Client
}

func (a *FabricApplicationAdapter) Start(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding) (RuntimeObservation, error) {
	return a.call(ctx, c, b, false)
}
func (a *FabricApplicationAdapter) Observe(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding) (RuntimeObservation, error) {
	return a.call(ctx, c, b, true)
}
func (a *FabricApplicationAdapter) call(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding, read bool) (RuntimeObservation, error) {
	var zero RuntimeObservation
	if a == nil || a.BaseURL == "" || a.Token == "" || len(a.CapabilityKey) < 32 {
		return zero, status.Error(codes.Unavailable, "Serve Fabric application credentials are not configured")
	}
	if c.GetSecretBindingId() != "" || len(c.GetModelSelections()) > 0 || c.GetModelConfigurationVersion() != 0 {
		return zero, status.Error(codes.FailedPrecondition, "configured model and secret injection requires the owning adapter binding")
	}
	raw, err := publicjson.Marshal(c.GetDeploymentDescriptor().GetApplicationRevision())
	if err != nil {
		return zero, err
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil || contracts.ValidateWorkspaceApplicationRevision(revision) != nil {
		return zero, status.Error(codes.InvalidArgument, "invalid application revision")
	}
	input := contracts.WorkspaceApplicationRuntimeInput{SchemaVersion: 2, AccountID: b.GetAccountId(), WorkspaceID: c.GetWorkspaceId(), ComputeID: b.GetComputeAllocationId(), VolumeID: b.GetStorageVolumeId(), AttachmentID: b.GetDataAttachmentId(), AttachmentOperationID: b.GetDataAttachmentOperationId(), RuntimeOperationID: c.GetRuntimeInstanceId(), Revision: revision, DataBindingID: c.GetDataAttachmentId()}
	// The first-delivery adapter admits a descriptor requiring no undisclosed
	// configuration or credentials. Required inputs fail validation, never default.
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, nil, input.DataBindingID)
	if err != nil {
		return zero, err
	}
	if err = contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		return zero, status.Error(codes.FailedPrecondition, "application configuration or protected secret binding is required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	path, action := "/fabric/workspace-application-runtimes", "create_workspace_application_runtime"
	if read {
		path += "/" + url.PathEscape(input.WorkspaceID) + "/readback"
		action = "read_workspace_application_runtime"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return zero, err
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
	}{1, "serve", input.AccountID, input.WorkspaceID, "workspace_application_runtime", input.WorkspaceID, action, input.RuntimeOperationID, time.Now().Add(time.Minute).Unix(), hex.EncodeToString(sum[:])}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(a.CapabilityKey))
	mac.Write([]byte(encoded))
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Idempotency-Key", input.RuntimeOperationID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OPL-Fabric-Capability", encoded+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return zero, status.Error(codes.Unavailable, "Fabric application adapter transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return zero, status.Errorf(codes.Unavailable, "Fabric application adapter returned HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, 2<<20)
	var observation contracts.WorkspaceApplicationRuntimeObservation
	if json.NewDecoder(limited).Decode(&observation) != nil || observation.WorkspaceID != input.WorkspaceID || observation.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) || contracts.ValidateWorkspaceApplicationRuntimeObservation(revision, observation) != nil {
		return zero, status.Error(codes.FailedPrecondition, "Fabric runtime observation does not match the exact original input")
	}
	out := RuntimeObservation{ObservedAt: time.Now().UTC(), ApplicationEntry: observation.Entry}
	switch observation.Status {
	case "ready":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
	case "pending":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
	case "failed":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
	case "absent":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED
	default:
		return zero, status.Error(codes.FailedPrecondition, "Fabric runtime state is unknown")
	}
	if observation.Entry != nil {
		out.AccessURL = observation.Entry.URL
	}
	// The readiness reference identifies the exact live observation bytes, rather
	// than inventing a provider receipt. A gateway service reference alone has no
	// publishable URL; it remains unavailable until its access owner resolves it.
	if read && observation.Status == "ready" {
		raw, _ := json.Marshal(observation)
		h := sha256.Sum256(raw)
		out.ReadinessEvidenceRef = fmt.Sprintf("fabric-application-readback:%s:sha256:%x", input.RuntimeOperationID, h)
	}
	return out, nil
}
