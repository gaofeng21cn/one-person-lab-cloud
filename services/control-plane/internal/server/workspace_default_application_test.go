package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

type applicationReplacementFabric struct {
	fakeFabricClient
	preflightErr error
	inputs       map[string]clients.WorkspaceApplicationRuntimeInput
	lifecycle    []contracts.WorkspaceApplicationRuntimeLifecycleInput
	states       map[string]string
}

func (f *applicationReplacementFabric) PreflightWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) error {
	if f.preflightErr != nil {
		return f.preflightErr
	}
	return f.fakeFabricClient.PreflightWorkspaceApplicationRuntime(ctx, input)
}

func (f *applicationReplacementFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, key string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if f.inputs == nil {
		f.inputs = map[string]clients.WorkspaceApplicationRuntimeInput{}
		f.states = map[string]string{}
	}
	result, err := f.fakeFabricClient.EnsureWorkspaceApplicationRuntime(ctx, input, key)
	if err == nil {
		f.inputs[input.RuntimeOperationID] = input
		f.states[input.RuntimeOperationID] = "ready"
	}
	return result, err
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntime(_ context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	result := contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), Status: f.states[input.RuntimeOperationID], Entry: &contracts.WorkspaceApplicationEntry{URL: "https://" + input.Revision.ApplicationID + ".example.test/"}, Components: contracts.WorkspaceApplicationRuntimeComponents(input.Revision)}
	for i := range result.Components {
		result.Components[i].State = result.Status
	}
	return result, nil
}
func (f *applicationReplacementFabric) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput, _ string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	original, exists := f.inputs[input.RuntimeOperationID]
	if !exists || original.AccountID != input.AccountID || original.WorkspaceID != input.WorkspaceID || contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) != input.RuntimeID {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("test_runtime_identity_mismatch")
	}
	f.lifecycle = append(f.lifecycle, input)
	f.states[input.RuntimeOperationID] = input.DesiredState
	if input.DesiredState == "running" {
		f.states[input.RuntimeOperationID] = "ready"
	}
	return f.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	observation, err := f.ReadWorkspaceApplicationRuntime(ctx, f.inputs[input.RuntimeOperationID])
	state := observation.Status
	if state == "ready" {
		state = "running"
	}
	return contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: state, Observation: observation}, err
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntimeCredentials(context.Context, contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return contracts.WorkspaceApplicationRuntimeCredentials{}, errors.New("not_requested")
}

func TestAcceptedApplicationDeploymentSuspensionFence(t *testing.T) {
	store := newMemoryTableStore()
	ctx := context.Background()
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	paidThrough, _ := time.Parse(time.RFC3339Nano, stringValue(workspace["paidThrough"]))
	if workspaceApplicationEntitlementOpen(workspace, paidThrough.Add(time.Second)) {
		t.Fatal("expired entitlement was admitted")
	}
	if !workspaceApplicationEntitlementOpen(workspace, paidThrough.Add(-time.Second)) {
		t.Fatal("paid entitlement was refused")
	}
}

// Frozen historical data/credential fields must still reach the real Runtime
// input reader after the general reinstall admission path has been removed.
func TestAcceptedHistoricalApplicationRuntimeInputRetainsDataSource(t *testing.T) {
	ctx := context.Background()
	for _, layout := range []string{"legacy_opl", "legacy_application"} {
		t.Run(layout, func(t *testing.T) {
			fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			purchase, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			revision := knowledgeApplicationRevisionForTest()
			configuration := contracts.WorkspaceApplicationRuntimeConfiguration{}
			var bindings []contracts.WorkspaceApplicationRuntimeSecretBinding
			source := "workspace-launch-alpha:runtime"
			if layout == "legacy_application" {
				original := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, configuration, nil, 0, true)
				row, _, _ := fixture.store.GetRuntimeOperation(ctx, original.OperationID)
				original.Version = 1
				original.Configuration, original.SecretBindings = contracts.WorkspaceApplicationRuntimeConfiguration{}, nil
				original.DataBindingID, original.DataLayout, original.DataSourceRuntimeOperationID = "", "", ""
				original.RequestHash = workspaceApplicationDeploymentRequestHash(original)
				payload, err := json.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				row["result"] = string(payload)
				mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
				source = original.OperationID + ":runtime"
			}
			if layout == "legacy_opl" {
				revision = defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64))
				source = "workspace-launch-alpha:runtime"
				configuration = contracts.WorkspaceApplicationRuntimeConfiguration{CredentialVersion: "full-runtime-explicitly-rotated", CredentialSourceRuntimeOperationID: source}
				bindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: "opl-gateway-ws-alpha", Version: "v1", Key: "opl_gateway_api_key"}}
			}
			seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "another-app", true)
			intent := applicationDeploymentFixtureForTest(t, app, "ws-alpha", "accepted-historical-return", revision)
			intent.Configuration, intent.SecretBindings = configuration, bindings
			intent.DataLayout, intent.DataSourceRuntimeOperationID = layout, source
			intent = seedAcceptedApplicationDeploymentForTest(t, app, intent)
			restarted, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			row, found, err := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
			if err != nil || !found {
				t.Fatalf("accepted history found=%v err=%v", found, err)
			}
			retained, err := decodeWorkspaceApplicationDeploymentIntent(row)
			if err != nil {
				t.Fatal(err)
			}
			input, err := restarted.(*controlPlaneHTTPHandler).app.workspaceApplicationRuntimeInput(ctx, retained)
			if err != nil || input.DataLayout != layout || input.DataSourceRuntimeOperationID != source || input.DataBindingID != intent.DataBindingID || input.Configuration.CredentialVersion != configuration.CredentialVersion || input.Configuration.CredentialSourceRuntimeOperationID != configuration.CredentialSourceRuntimeOperationID {
				t.Fatalf("historical runtime input lost accepted lineage: %+v err=%v", input, err)
			}
			after, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			if after["result"] != purchase["result"] {
				t.Fatal("historical runtime read rewrote purchase")
			}
		})
	}
}
