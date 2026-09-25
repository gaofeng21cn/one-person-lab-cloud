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
	"time"

	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

// This exercises the real adapter HTTP contract, not a Docker or provider
// qualification. The Local-Docker chain supplies that external execution proof.
func TestServeFabricAdapterBindsOriginalInputsAndLiveObservation(t *testing.T) {
	const token = "serve-http-test-token-distinct-00001"
	const key = "serve-http-capability-signature-00002"
	cmd := deployCommand(t, "workspace-http", "deployment-http", 1)
	revision := cmd.DeploymentDescriptor.ApplicationRevision
	revision.EntryPort = proto.String("http")
	revision.Ports = []*api.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}}
	binding := &api.ResourceExecutionBinding{AccountId: "original-account", ComputeAllocationId: "original-compute", StorageVolumeId: "original-volume", DataAttachmentId: cmd.DataAttachmentId, DataAttachmentOperationId: "original-attachment-operation"}
	calls := 0
	foreign := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Idempotency-Key") != cmd.RuntimeInstanceId {
			t.Error("transport/original operation missing")
		}
		parts := strings.Split(r.Header.Get("X-OPL-Fabric-Capability"), ".")
		if len(parts) != 2 {
			t.Fatal("missing scoped capability")
		}
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(parts[0]))
		sig, _ := base64.RawURLEncoding.DecodeString(parts[1])
		if !hmac.Equal(sig, mac.Sum(nil)) {
			t.Fatal("invalid signature")
		}
		payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var claims map[string]any
		json.Unmarshal(payload, &claims)
		sum := sha256.Sum256(body)
		if claims["caller"] != "serve" || claims["accountId"] != binding.AccountId || claims["workspaceId"] != cmd.WorkspaceId || claims["operationId"] != cmd.RuntimeInstanceId || claims["bodySha256"] != hex.EncodeToString(sum[:]) {
			t.Fatalf("capability identity drift=%v", claims)
		}
		var input contracts.WorkspaceApplicationRuntimeInput
		if json.Unmarshal(body, &input) != nil {
			t.Fatal("invalid typed input")
		}
		if input.ComputeID != binding.ComputeAllocationId || input.VolumeID != binding.StorageVolumeId || input.AttachmentOperationID != binding.DataAttachmentOperationId || input.DataBindingID != binding.DataAttachmentId {
			t.Fatalf("binding drift=%+v", input)
		}
		components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
		for i := range components {
			components[i].State = "ready"
		}
		observation := contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), Status: "ready", Entry: &contracts.WorkspaceApplicationEntry{URL: "http://127.0.0.1:8080/"}, Components: components}
		if foreign {
			observation.RuntimeID = "other-runtime"
		}
		json.NewEncoder(w).Encode(observation)
	}))
	defer server.Close()
	adapter := &delivery.FabricApplicationAdapter{BaseURL: server.URL, Token: token, CapabilityKey: key}
	ack, err := adapter.Start(context.Background(), cmd, binding)
	if err != nil || ack.ReadinessEvidenceRef != "" {
		t.Fatalf("start=%+v %v", ack, err)
	}
	observed, err := adapter.Observe(context.Background(), cmd, binding)
	if err != nil || observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || observed.AccessURL != "http://127.0.0.1:8080/" || !strings.HasPrefix(observed.ReadinessEvidenceRef, "fabric-application-readback:") || time.Since(observed.ObservedAt) > time.Second {
		t.Fatalf("readback=%+v %v", observed, err)
	}
	foreign = true
	if _, err = adapter.Observe(context.Background(), cmd, binding); err == nil {
		t.Fatal("foreign runtime accepted")
	}
	cmd.SecretBindingId = "secret-requires-resolver"
	before := calls
	if _, err = adapter.Start(context.Background(), cmd, binding); err == nil || calls != before {
		t.Fatal("unresolved secret reached provider")
	}
}
