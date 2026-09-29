package delivery

// TKEApplicationAdapter is Serve's own adapter to the installation's Agent
// execution boundary.
//
// The installation executes Agent workloads on TKE. The execution engine is
// reached over the Fabric-owned application-runtime port, which is the only
// component that holds the cluster credentials and the provider naming; Serve
// calls that port through the same scoped, signed capability the port admits, and
// it never buys, deletes or mutates a resource allocation.
//
// The adapter exists because three facts have exactly one owner and Serve must
// respect each of them:
//
//   - The provider reports the *in-cluster destination* it created, not the
//     address a customer opens. Serve therefore resolves the publishable URL from
//     the installation's declared route origin and never treats the provider's
//     service name or port as an Entry URL. A gateway entry with no declared
//     origin is recorded as not-open rather than published at a guessed host.
//   - Readiness is the provider's whole-runtime observation. A Pod that is merely
//     Running, or a persisted pointer, is not readiness: every declared component
//     must be observed ready *and* a publishable entry must resolve before Serve
//     records the Agent as ready.
//   - Credential and model configuration belong to the deployment intent. The
//     adapter carries the model configuration into the observed runtime record and
//     refuses only the one case the current wire cannot express: a referenced
//     Secret whose delivery reference has no field to travel in. That refusal
//     names the missing owner field instead of silently dropping the injection.

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
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// ReasonCredentialInjectionWireMissing names the one injection the current
// contract cannot carry. Serve holds an opaque Secret binding identity and the
// model configuration; the runtime input needs the Secret delivery reference, and
// no field on the runtime command or the runtime input transports it. Recording a
// delivery without injection would claim an Agent that cannot reach its model, so
// the adapter refuses and names the exact owner fact it needs.
const ReasonCredentialInjectionWireMissing = "credential_injection_wire_missing"

// TKEApplicationAdapter executes one already-admitted descriptor against the
// installation's Agent execution boundary. Resource bindings are Fabric readbacks,
// never caller input, and the browser-facing origin comes from the installation's
// declared RouteOrigin.
type TKEApplicationAdapter struct {
	BaseURL, Token, CapabilityKey string
	Origin                        RouteOrigin
	Client                        *http.Client
}

func (a *TKEApplicationAdapter) Start(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding) (RuntimeObservation, error) {
	return a.call(ctx, c, b, false)
}

func (a *TKEApplicationAdapter) Observe(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding) (RuntimeObservation, error) {
	return a.call(ctx, c, b, true)
}

func (a *TKEApplicationAdapter) call(ctx context.Context, c *api.RuntimeDeployCommand, b *api.ResourceExecutionBinding, read bool) (RuntimeObservation, error) {
	var zero RuntimeObservation
	if a == nil || a.BaseURL == "" || a.Token == "" || len(a.CapabilityKey) < 32 {
		return zero, status.Error(codes.Unavailable, "Serve Agent execution credentials are not configured")
	}
	if c == nil || c.GetDeploymentDescriptor() == nil || b == nil {
		return zero, status.Error(codes.InvalidArgument, "deployment descriptor and resource binding are required")
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
	// An application that needs an injected Secret or platform-issued credential
	// cannot be executed with a proven injection: the current runtime command and
	// runtime input carry an opaque Secret binding identity, but no field
	// transports the Secret delivery reference the execution boundary injects.
	// Serve refuses and names the missing owner fact rather than starting an Agent
	// that can never reach its model.
	if strings.TrimSpace(c.GetSecretBindingId()) != "" || referencedSecretRequired(revision) {
		return zero, owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: Serve holds Secret binding %q and model configuration version %d, but no field on RuntimeDeployCommand or WorkspaceApplicationRuntimeInput carries the Secret delivery reference the execution boundary injects", ReasonCredentialInjectionWireMissing, strings.TrimSpace(c.GetSecretBindingId()), c.GetModelConfigurationVersion()), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
	}
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
		return zero, status.Error(codes.Unavailable, "Agent execution adapter transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return zero, status.Errorf(codes.Unavailable, "Agent execution adapter returned HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, 2<<20)
	var observation contracts.WorkspaceApplicationRuntimeObservation
	if json.NewDecoder(limited).Decode(&observation) != nil || observation.WorkspaceID != input.WorkspaceID || observation.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) || contracts.ValidateWorkspaceApplicationRuntimeObservation(revision, observation) != nil {
		return zero, status.Error(codes.FailedPrecondition, "Agent runtime observation does not match the exact original input")
	}
	out := RuntimeObservation{ObservedAt: time.Now().UTC(), Components: observation.Components, ApplicationEntry: observation.Entry}
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
		return zero, status.Error(codes.FailedPrecondition, "Agent runtime state is unknown")
	}
	// Readiness is the whole-runtime fact, not one component and not a Pod phase.
	// Only an observation that reports every declared component ready may even be
	// considered; a running Pod whose component is not ready stays not-ready.
	allComponentsReady := deploymentComponentsReady(revision, observation.Components)
	if out.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || !allComponentsReady {
		if out.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
			out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
		}
		return out, nil
	}
	accessURL, upstream, resolveErr := resolveApplicationEntry(a.Origin, c.GetWorkspaceId(), revision.ApplicationID, *observation.Entry)
	if resolveErr != nil {
		// The application runs, but no publishable address is provable. Serve never
		// publishes a guessed host; only a readback reports the running state, and a
		// start stays starting because it cannot prove readiness at all.
		out.AccessURL = ""
		if !read {
			out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
		}
		return out, nil
	}
	out.AccessURL = accessURL
	// The readiness reference binds the exact live observation bytes with the
	// resolved route and the validated in-cluster upstream, so a later reader can
	// tell which execution and which route produced the readiness fact.
	if read {
		out.ReadinessEvidenceRef = tkeReadinessEvidence(input.RuntimeOperationID, accessURL, upstream, observation, c.GetModelConfigurationVersion())
	} else {
		// Start acknowledges the request; only the separate read may prove readiness.
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
	}
	return out, nil
}

// deploymentComponentsReady reports whether the observation accounts for every
// declared component (the main component plus one per dependency) and every one
// of them is ready. A missing component is never treated as ready.
func deploymentComponentsReady(revision contracts.WorkspaceApplicationRevision, observed []contracts.WorkspaceApplicationRuntimeComponentState) bool {
	declared := contracts.WorkspaceApplicationRuntimeComponents(revision)
	if len(observed) != len(declared) {
		return false
	}
	state := make(map[string]string, len(observed))
	for _, component := range observed {
		state[component.Name] = component.State
	}
	for _, component := range declared {
		if state[component.Name] != "ready" {
			return false
		}
	}
	return true
}

// tkeReadinessEvidence is the provider evidence identity for a ready report. It
// binds the runtime operation, the resolved route, the validated in-cluster
// upstream, the whole-runtime observation and the applied model configuration
// version, so the readiness fact names exactly what was observed.
func tkeReadinessEvidence(runtimeOperationID, accessURL, upstream string, observation contracts.WorkspaceApplicationRuntimeObservation, modelConfigurationVersion int64) string {
	raw, _ := json.Marshal(observation)
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("tke-application-readback:%s:%s:%d ", upstream, accessURL, modelConfigurationVersion)), raw...))
	return fmt.Sprintf("tke-application-readback:%s:%s:sha256:%x", runtimeOperationID, accessURL, sum)
}

// referencedSecretRequired reports whether the revision declares a Secret or
// credential the execution boundary must inject. An application that declares
// none executes with no injection and needs no wire.
func referencedSecretRequired(revision contracts.WorkspaceApplicationRevision) bool {
	if len(revision.SecretInputs) > 0 {
		return true
	}
	for _, dependency := range revision.Dependencies {
		if len(dependency.SecretInputs) > 0 {
			return true
		}
	}
	return contracts.WorkspaceApplicationRequiresPlatformCredentials(revision)
}

// lifecycleRuntimeInput builds the Fabric lifecycle input for the exact original
// deployment. Only opaque runtime identity travels; explicit application input is
// replaced by the lifecycle handle the Fabric runtime already owns.
func (a *TKEApplicationAdapter) lifecycleInput(command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding, desired string) contracts.WorkspaceApplicationRuntimeLifecycleInput {
	return contracts.WorkspaceApplicationRuntimeLifecycleInput{
		AccountID: binding.GetAccountId(), WorkspaceID: command.GetWorkspaceId(),
		RuntimeID: contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId()),
		RuntimeOperationID: command.GetRuntimeInstanceId(), DesiredState: desired,
	}
}

// Lifecycle applies a desired state to the exact reserved runtime through the
// installation's lifecycle boundary and confirms the provider applied it.
func (a *TKEApplicationAdapter) Lifecycle(ctx context.Context, command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding, desired string) error {
	_, err := a.callLifecycle(ctx, command, binding, desired, "lifecycle")
	return err
}

// Reload applies the command's model configuration through the lifecycle boundary's
// running state, which re-reads the frozen configuration the runtime already holds.
func (a *TKEApplicationAdapter) Reload(ctx context.Context, command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding) error {
	_, err := a.callLifecycle(ctx, command, binding, "running", "lifecycle")
	return err
}

// Credentials reads the platform-issued WebUI credential for the exact runtime. The
// value travels only in the response and is never persisted.
func (a *TKEApplicationAdapter) Credentials(ctx context.Context, command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding) (*api.WorkspaceApplicationCredentials, error) {
	if a == nil || a.BaseURL == "" || a.Token == "" || len(a.CapabilityKey) < 32 {
		return nil, status.Error(codes.Unavailable, "Serve Agent execution credentials are not configured")
	}
	body, err := json.Marshal(a.lifecycleInput(command, binding, "running"))
	if err != nil {
		return nil, err
	}
	response, err := a.post(ctx, command, binding, "/fabric/workspace-application-runtimes/"+url.PathEscape(command.GetWorkspaceId())+"/credentials", "read_workspace_application_runtime_credentials", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, status.Errorf(codes.Unavailable, "Agent credential boundary returned HTTP %d", response.StatusCode)
	}
	var credentials contracts.WorkspaceApplicationRuntimeCredentials
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&credentials) != nil || credentials.RuntimeID != contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId()) || credentials.WorkspaceID != command.GetWorkspaceId() {
		return nil, status.Error(codes.FailedPrecondition, "Agent credential readback does not match the exact runtime")
	}
	return &api.WorkspaceApplicationCredentials{WorkspaceId: credentials.WorkspaceID, RuntimeInstanceId: credentials.RuntimeID, Username: credentials.WebUIUsername, Password: credentials.WebUIPassword}, nil
}

// callLifecycle posts one lifecycle request and confirms the provider's readback of
// the exact runtime.
func (a *TKEApplicationAdapter) callLifecycle(ctx context.Context, command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding, desired, endpoint string) (*contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	if a == nil || a.BaseURL == "" || a.Token == "" || len(a.CapabilityKey) < 32 {
		return nil, status.Error(codes.Unavailable, "Serve Agent execution credentials are not configured")
	}
	body, err := json.Marshal(a.lifecycleInput(command, binding, desired))
	if err != nil {
		return nil, err
	}
	response, err := a.post(ctx, command, binding, "/fabric/workspace-application-runtimes/"+url.PathEscape(command.GetWorkspaceId())+"/"+endpoint, "set_workspace_application_runtime_lifecycle", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, status.Errorf(codes.Unavailable, "Agent lifecycle boundary returned HTTP %d", response.StatusCode)
	}
	var result contracts.WorkspaceApplicationRuntimeLifecycleResult
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
		return nil, status.Error(codes.FailedPrecondition, "Agent lifecycle readback is not decodable")
	}
	if result.RuntimeID != contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId()) || result.WorkspaceID != command.GetWorkspaceId() {
		return nil, status.Error(codes.FailedPrecondition, "Agent lifecycle readback does not match the exact runtime")
	}
	return &result, nil
}

// post signs and sends one Fabric capability request. It is the shared frame the
// adapter's own execution calls use, factored so lifecycle and credential calls
// carry the identical scoped, signed identity.
func (a *TKEApplicationAdapter) post(ctx context.Context, command *api.RuntimeDeployCommand, binding *api.ResourceExecutionBinding, path, action string, body []byte) (*http.Response, error) {
	if binding == nil {
		return nil, status.Error(codes.InvalidArgument, "resource binding is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+path, bytes.NewReader(body))
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
	}{1, "serve", binding.GetAccountId(), command.GetWorkspaceId(), "workspace_application_runtime", command.GetWorkspaceId(), action, command.GetRuntimeInstanceId(), time.Now().Add(time.Minute).Unix(), hex.EncodeToString(sum[:])}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(a.CapabilityKey))
	mac.Write([]byte(encoded))
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Idempotency-Key", command.GetRuntimeInstanceId())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OPL-Fabric-Capability", encoded+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return client.Do(req)
}
