package delivery_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

// These exercise the real adapter HTTP contract against an isolated provider
// fixture. They are not a TKE qualification: no real cluster, ingress or
// certificate is involved, and the isolation is deliberate.
const (
	tkeToken = "serve-execution-test-token-distinct-0001"
	tkeKey   = "serve-execution-signature-0000000002"
)

// tkeProviderFixture answers exactly like the installation execution boundary and
// records the typed input Serve sent it.
type tkeProviderFixture struct {
	t              *testing.T
	entry          *contracts.WorkspaceApplicationEntry
	componentState string
	status         string
	foreignRuntime bool
	lastInput      contracts.WorkspaceApplicationRuntimeInput
	calls          int
}

func (f *tkeProviderFixture) handler(w http.ResponseWriter, r *http.Request) {
	f.calls++
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("Authorization") != "Bearer "+tkeToken {
		f.t.Error("transport token missing")
	}
	parts := strings.Split(r.Header.Get("X-OPL-Fabric-Capability"), ".")
	if len(parts) != 2 {
		f.t.Fatal("missing scoped capability")
	}
	mac := hmac.New(sha256.New, []byte(tkeKey))
	mac.Write([]byte(parts[0]))
	sig, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !hmac.Equal(sig, mac.Sum(nil)) {
		f.t.Fatal("invalid signature")
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var claims map[string]any
	json.Unmarshal(payload, &claims)
	sum := sha256.Sum256(body)
	if claims["caller"] != "serve" || claims["bodySha256"] != hex.EncodeToString(sum[:]) {
		f.t.Fatalf("capability identity drift=%v", claims)
	}
	if json.Unmarshal(body, &f.lastInput) != nil {
		f.t.Fatal("invalid typed input")
	}
	if f.lastInput.SecretBindings != nil {
		f.t.Fatal("secret material must never travel through Serve's execution input")
	}
	components := contracts.WorkspaceApplicationRuntimeComponents(f.lastInput.Revision)
	for i := range components {
		components[i].State = f.componentState
	}
	entry := f.entry
	if f.status != "ready" {
		// A provider publishes the entry only once the runtime is ready.
		entry = nil
	}
	observation := contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: f.lastInput.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(f.lastInput.RuntimeOperationID), Status: f.status, Entry: entry, Components: components}
	if f.foreignRuntime {
		observation.RuntimeID = "other-runtime"
	}
	json.NewEncoder(w).Encode(observation)
}

func tkeAdapterFixture(t *testing.T, entry *contracts.WorkspaceApplicationEntry, origin delivery.RouteOrigin) (*delivery.TKEApplicationAdapter, *tkeProviderFixture, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) {
	t.Helper()
	fixture := &tkeProviderFixture{t: t, entry: entry, componentState: "ready", status: "ready"}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	t.Cleanup(server.Close)
	cmd := deployCommand(t, "workspace-http", "deployment-http", 1)
	revision := cmd.DeploymentDescriptor.ApplicationRevision
	revision.EntryPort = proto.String("http")
	revision.Ports = []*api.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}}
	binding := &api.ResourceExecutionBinding{AccountId: "original-account", ComputeAllocationId: "original-compute", StorageVolumeId: "original-volume", DataAttachmentId: cmd.DataAttachmentId, DataAttachmentOperationId: "original-attachment-operation"}
	adapter := &delivery.TKEApplicationAdapter{BaseURL: server.URL, Token: tkeToken, CapabilityKey: tkeKey, Origin: origin}
	return adapter, fixture, cmd, binding
}

// TestServeTKEAdapterResolvesGatewayEntryToDeclaredOrigin is the decisive fix: a
// TKE provider reports one in-cluster service and port, and Serve publishes the
// installation's declared origin for it instead of treating the service name as a
// URL.
func TestServeTKEAdapterResolvesGatewayEntryToDeclaredOrigin(t *testing.T) {
	origin := delivery.RouteOrigin{Scheme: "https", WorkspaceDomain: "workspaces.example", ApplicationDomain: "apps.example"}
	adapter, _, cmd, binding := tkeAdapterFixture(t, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}, origin)

	ack, err := adapter.Start(context.Background(), cmd, binding)
	if err != nil || ack.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING || ack.ReadinessEvidenceRef != "" {
		t.Fatalf("start=%+v err=%v", ack, err)
	}
	observed, err := adapter.Observe(context.Background(), cmd, binding)
	if err != nil {
		t.Fatal(err)
	}
	host, ok := origin.ApplicationEntryHost(cmd.WorkspaceId, cmd.DeploymentDescriptor.ApplicationRevision.ApplicationId)
	if !ok {
		t.Fatal("fixture origin must compose a hostname")
	}
	if observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || observed.AccessURL != "https://"+host+"/" {
		t.Fatalf("observed=%+v want ready at https://%s/", observed, host)
	}
	if !strings.HasPrefix(observed.ReadinessEvidenceRef, "tke-application-readback:"+cmd.RuntimeInstanceId+":https://"+host+"/:sha256:") {
		t.Fatalf("readiness evidence=%q", observed.ReadinessEvidenceRef)
	}
	// The validated in-cluster upstream and the resolved origin are both recorded
	// in the evidence identity, so a later reader can replay routing exactly.
	if !strings.Contains(observed.ReadinessEvidenceRef, host) {
		t.Fatalf("readiness evidence must bind the resolved origin: %q", observed.ReadinessEvidenceRef)
	}
}

// TestServeTKEAdapterRefusesToGuessAnOrigin proves the other half: with no
// declared origin Serve never publishes the provider's service name as the
// customer address. The runtime is still reported running, so the delivery is
// recorded as not-open rather than with an unroutable URL.
func TestServeTKEAdapterRefusesToGuessAnOrigin(t *testing.T) {
	adapter, _, cmd, binding := tkeAdapterFixture(t, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}, delivery.RouteOrigin{})
	observed, err := adapter.Observe(context.Background(), cmd, binding)
	if err != nil {
		t.Fatal(err)
	}
	if observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || observed.AccessURL != "" || observed.ReadinessEvidenceRef != "" {
		t.Fatalf("observed=%+v want running with no publishable address", observed)
	}
}

// TestServeTKEAdapterRequiresWholeRuntimeReadiness proves a Pod that is merely
// running, or one unready component, is never recorded ready.
func TestServeTKEAdapterRequiresWholeRuntimeReadiness(t *testing.T) {
	origin := delivery.RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	adapter, fixture, cmd, binding := tkeAdapterFixture(t, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}, origin)
	fixture.componentState, fixture.status = "pending", "pending"
	observed, err := adapter.Observe(context.Background(), cmd, binding)
	if err != nil || observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING || observed.AccessURL != "" || observed.ReadinessEvidenceRef != "" {
		t.Fatalf("pending component observed=%+v err=%v", observed, err)
	}
	// A failed component is a failed runtime, never a ready one.
	fixture.componentState, fixture.status = "failed", "failed"
	if observed, err = adapter.Observe(context.Background(), cmd, binding); err != nil || observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED || observed.AccessURL != "" {
		t.Fatalf("failed runtime observed=%+v err=%v", observed, err)
	}
}

func TestServeTKEAdapterCarriesModelConfigurationAndNamesTheSecretGap(t *testing.T) {
	origin := delivery.RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	adapter, fixture, cmd, binding := tkeAdapterFixture(t, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}, origin)
	ctx := context.Background()

	// Model configuration is delivery intent and must reach the execution boundary
	// rather than being rejected.
	cmd.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	cmd.ModelConfigurationVersion = 2
	if _, err := adapter.Start(ctx, cmd, binding); err != nil {
		t.Fatalf("model configuration rejected: %v", err)
	}
	if fixture.lastInput.Revision.ApplicationID == "" {
		t.Fatal("fixture did not receive the typed runtime input")
	}

	// A referenced Secret has no field to travel in, so Serve refuses and names the
	// exact missing fact instead of executing an un-injected Agent.
	secretCommand := proto.Clone(cmd).(*api.RuntimeDeployCommand)
	secretCommand.SecretBindingId = "sbx-opaque-binding"
	before := fixture.calls
	_, err := adapter.Start(ctx, secretCommand, binding)
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonCredentialInjectionWireMissing) || !strings.Contains(err.Error(), "Secret binding") {
		t.Fatalf("secret gap err=%v", err)
	}
	if fixture.calls != before {
		t.Fatal("an unexpressible secret injection reached the provider")
	}
}

func TestServeTKEAdapterRejectsForeignObservation(t *testing.T) {
	origin := delivery.RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	adapter, fixture, cmd, binding := tkeAdapterFixture(t, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}, origin)
	fixture.foreignRuntime = true
	if _, err := adapter.Observe(context.Background(), cmd, binding); err == nil {
		t.Fatal("foreign runtime accepted")
	}
}
