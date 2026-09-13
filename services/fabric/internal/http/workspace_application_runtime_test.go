package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/fabric"
)

type applicationRuntimeHTTPProvider struct {
	testProvider
	state       string
	readErr     error
	ensureCalls int
	readCalls   int
	lastInput   fabric.WorkspaceApplicationRuntimeInput
}

func (p *applicationRuntimeHTTPProvider) observation(input fabric.WorkspaceApplicationRuntimeInput) contracts.WorkspaceApplicationRuntimeObservation {
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	for index := range components {
		components[index].State = p.state
	}
	return contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, Status: p.state, Components: components,
		EntryURL: "https://application.example/",
	}
}

func (p *applicationRuntimeHTTPProvider) EnsureWorkspaceApplicationRuntime(_ context.Context, input fabric.WorkspaceApplicationRuntimeInput, _ fabric.ComputeAllocation, _ fabric.StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	p.ensureCalls++
	p.lastInput = input
	return p.observation(input), nil
}

func (p *applicationRuntimeHTTPProvider) ReadWorkspaceApplicationRuntime(_ context.Context, input fabric.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	p.readCalls++
	if p.readErr != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, p.readErr
	}
	return p.observation(input), nil
}

func applicationRuntimeHTTPFixture(t *testing.T) (http.Handler, *applicationRuntimeHTTPProvider, fabric.WorkspaceApplicationRuntimeInput) {
	t.Helper()
	provider := &applicationRuntimeHTTPProvider{state: "pending"}
	service := fabric.NewService(provider)
	ctx := context.Background()
	compute, err := service.CreateComputeAllocation(ctx, fabric.ComputeAllocationInput{
		ID: "compute-application", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", PackageID: "basic", NodePoolID: "np-basic", IdempotencyKey: "launch-alpha:compute",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for compute.Status != "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		compute, _ = service.GetComputeAllocation(ctx, compute.ID)
	}
	if compute.Status != "running" {
		t.Fatalf("compute did not become ready: %s", compute.Status)
	}
	volume, err := service.CreateStorageVolume(ctx, fabric.StorageVolumeInput{
		ID: "volume-application", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ComputeID: compute.ID,
		SizeGB: 10, Zone: compute.Zone, IdempotencyKey: "launch-alpha:storage",
	})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := service.CreateStorageAttachment(ctx, fabric.StorageAttachmentInput{
		WorkspaceID: "ws-alpha", ComputeID: compute.ID, VolumeID: volume.ID, IdempotencyKey: "launch-alpha:attachment",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := fabric.WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, DataBindingID: "data-knowledge",
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ComputeID: compute.ID, VolumeID: volume.ID,
		AttachmentID: attachment.ID, AttachmentOperationID: attachment.OperationID, RuntimeOperationID: "deploy-alpha:runtime", ConfigurationDigest: strings.Repeat("c", 64),
		Revision: contracts.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
			Image: "registry.example/app@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application", EntryPort: "http",
			Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		},
	}
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	return NewServerWithAuth(service, ServerAuthConfig{ControlPlaneToken: "internal-secret", RunnerToken: "runner-secret", CapabilityKey: testFabricCapabilityKey}), provider, input
}

func applicationRuntimeHTTPRequest(t *testing.T, server http.Handler, input fabric.WorkspaceApplicationRuntimeInput, readback bool, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path, action := "/fabric/workspace-application-runtimes", "create_workspace_application_runtime"
	if readback {
		path, action = path+"/"+input.WorkspaceID+"/readback", "read_workspace_application_runtime"
	}
	req := testRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", input.RuntimeOperationID)
	if authorization != "missing" {
		claims := fabricCapabilityClaimsForTest{
			Version: 1, Caller: "control-plane", AccountID: input.AccountID, WorkspaceID: input.WorkspaceID,
			ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: action,
			OperationID: input.RuntimeOperationID, ExpiresAt: time.Now().Add(time.Minute).Unix(),
		}
		if authorization == "wrong-scope" {
			claims.WorkspaceID = "ws-other"
		}
		req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, claims, body))
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	return response
}

func TestWorkspaceApplicationRuntimeHTTPRequiresCapabilityAndOriginalAttachment(t *testing.T) {
	server, provider, input := applicationRuntimeHTTPFixture(t)
	for _, readback := range []bool{false, true} {
		for _, authorization := range []string{"missing", "wrong-scope"} {
			response := applicationRuntimeHTTPRequest(t, server, input, readback, authorization)
			if response.Code != http.StatusForbidden {
				t.Fatalf("authorization=%s readback=%v status=%d body=%s", authorization, readback, response.Code, response.Body.String())
			}
		}
	}
	wrongAttachment := input
	wrongAttachment.AttachmentOperationID = input.RuntimeOperationID + ":attachment"
	response := applicationRuntimeHTTPRequest(t, server, wrongAttachment, false, "valid")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "workspace_application_runtime_attachment_mismatch") {
		t.Fatalf("wrong attachment status=%d body=%s", response.Code, response.Body.String())
	}
	wrongEntry := input
	wrongEntry.Revision.EntryPort = "unknown"
	response = applicationRuntimeHTTPRequest(t, server, wrongEntry, false, "valid")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "workspace_application_entry_port_invalid") {
		t.Fatalf("wrong entry status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.ensureCalls != 0 || provider.readCalls != 0 {
		t.Fatal("rejected requests reached application provider")
	}
}

func TestWorkspaceApplicationRuntimeHTTPPendingLiveReadbackAndFailure(t *testing.T) {
	server, provider, input := applicationRuntimeHTTPFixture(t)
	var runtimeID string
	for _, step := range []struct {
		name, state string
		readback    bool
	}{
		{"create pending", "pending", false},
		{"read pending", "pending", true},
		{"read ready", "ready", true},
		{"ensure ready replay", "ready", false},
		{"read failed", "failed", true},
	} {
		provider.state = step.state
		response := applicationRuntimeHTTPRequest(t, server, input, step.readback, "valid")
		var observation contracts.WorkspaceApplicationRuntimeObservation
		if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &observation) != nil || observation.Status != step.state || observation.RuntimeID == "" {
			t.Fatalf("%s status=%d body=%s", step.name, response.Code, response.Body.String())
		}
		if runtimeID != "" && observation.RuntimeID != runtimeID {
			t.Fatalf("%s changed runtime identity", step.name)
		}
		runtimeID = observation.RuntimeID
	}
	if provider.ensureCalls != 1 || provider.readCalls != 4 || provider.lastInput.Revision.EntryPort != "http" || provider.lastInput.AttachmentOperationID != input.AttachmentOperationID {
		t.Fatalf("wire replay changed execution: ensure=%d read=%d input=%#v", provider.ensureCalls, provider.readCalls, provider.lastInput)
	}
	provider.readErr = errors.New("live application read unavailable")
	response := applicationRuntimeHTTPRequest(t, server, input, true, "valid")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"status":"ready"`) || !strings.Contains(response.Body.String(), "live application read unavailable") {
		t.Fatalf("failed live read returned history: status=%d body=%s", response.Code, response.Body.String())
	}
}

func (p *applicationRuntimeHTTPProvider) PreflightWorkspaceApplicationRuntime(context.Context, fabric.WorkspaceApplicationRuntimeInput, fabric.ComputeAllocation, fabric.StorageVolume) error {
	return nil
}
func (p *applicationRuntimeHTTPProvider) ReadWorkspaceApplicationRuntimeLifecycle(_ context.Context, input fabric.WorkspaceApplicationRuntimeInput) (fabric.WorkspaceApplicationRuntimeLifecycleResult, error) {
	observation := p.observation(input)
	observation.RuntimeID = contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID)
	state := observation.Status
	if state == "ready" {
		state = "running"
	}
	if state != "running" {
		observation.EntryURL = ""
	}
	return fabric.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: observation.RuntimeID, WorkspaceID: input.WorkspaceID, State: state, Observation: observation}, nil
}
func (p *applicationRuntimeHTTPProvider) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input fabric.WorkspaceApplicationRuntimeInput, state string) (fabric.WorkspaceApplicationRuntimeLifecycleResult, error) {
	p.state = state
	if state == "running" {
		p.state = "ready"
	}
	return p.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
}
func (p *applicationRuntimeHTTPProvider) ReadWorkspaceApplicationRuntimeCredentials(_ context.Context, input fabric.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), WorkspaceID: input.WorkspaceID, WebUIUsername: "opl", WebUIPassword: "synthetic-protected-password"}, nil
}

func TestWorkspaceApplicationRuntimeHTTPNewPortsBindExactCapability(t *testing.T) {
	server, provider, input := applicationRuntimeHTTPFixture(t)
	provider.state = "ready"
	if response := applicationRuntimeHTTPRequest(t, server, input, false, "valid"); response.Code != http.StatusAccepted {
		t.Fatalf("create status=%d", response.Code)
	}
	lifecycle := fabric.WorkspaceApplicationRuntimeLifecycleInput{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), RuntimeOperationID: input.RuntimeOperationID, DesiredState: "running"}
	for _, test := range []struct {
		endpoint, action, resourceID, key string
		body                              any
	}{
		{"preflight", "preflight_workspace_application_runtime", input.WorkspaceID, input.RuntimeOperationID, input},
		{"lifecycle-readback", "read_workspace_application_runtime_lifecycle", lifecycle.RuntimeID, input.RuntimeOperationID, lifecycle},
		{"credentials", "read_workspace_application_runtime_credentials", lifecycle.RuntimeID, input.RuntimeOperationID, lifecycle},
		{"lifecycle", "set_workspace_application_runtime_lifecycle", lifecycle.RuntimeID, "new-port-resume", lifecycle},
	} {
		for _, authorized := range []bool{false, true} {
			body, err := json.Marshal(test.body)
			if err != nil {
				t.Fatal(err)
			}
			req := testRequest(http.MethodPost, "/fabric/workspace-application-runtimes/"+input.WorkspaceID+"/"+test.endpoint, bytes.NewReader(body))
			req.Header.Set("Idempotency-Key", test.key)
			if authorized {
				req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, fabricCapabilityClaimsForTest{Version: 1, Caller: "control-plane", AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: test.resourceID, Action: test.action, OperationID: test.key, ExpiresAt: time.Now().Add(time.Minute).Unix()}, body))
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			expected := http.StatusForbidden
			if authorized {
				expected = http.StatusAccepted
				if test.endpoint == "preflight" {
					expected = http.StatusOK
				}
			}
			if response.Code != expected {
				t.Fatalf("endpoint=%s authorized=%t status=%d", test.endpoint, authorized, response.Code)
			}
			if authorized && test.endpoint == "credentials" && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("credential response cacheable")
			}
		}
	}
}
