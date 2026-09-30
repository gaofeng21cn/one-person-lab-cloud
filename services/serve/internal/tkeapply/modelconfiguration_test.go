package tkeapply

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// applicationStub is the application's own declared HTTP interface, recorded: it
// answers the declared apply and readback paths with the exact bytes an
// application answers with, and it keeps every request the executor issued, so a
// test proves the wire the boundary sends rather than what a fixture assumed.
type applicationStub struct {
	requests []recordedRequest
	apply    stubResponse
	readback stubResponse
}

type recordedRequest struct {
	method string
	url    string
	header http.Header
	body   string
}

type stubResponse struct {
	status int
	body   string
}

func (s *applicationStub) RoundTrip(request *http.Request) (*http.Response, error) {
	var raw []byte
	if request.Body != nil {
		var err error
		raw, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
	}
	s.requests = append(s.requests, recordedRequest{method: request.Method, url: request.URL.String(), header: request.Header.Clone(), body: string(raw)})
	response := s.readback
	if request.Method == http.MethodPut {
		response = s.apply
	}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return &http.Response{
		StatusCode: response.status,
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    request,
	}, nil
}

// controlSecretInput is the one secret input a publisher declares for the model
// configuration interface: the credential the application itself verifies.
const (
	controlSecretInput = "control_token"
	controlSecretRef   = "installation-control-token"
	controlSecretValue = "control-token-value"
)

// modelConfigurationFixture is the frozen default application: the declared port the
// interface is reached at, the declared secret input it authorizes with, and the
// confirmed Secret binding that delivered that input's value to the runtime.
func modelConfigurationFixture(t *testing.T) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
	t.Helper()
	input := testInput(t)
	input.Revision.Ports = append(input.Revision.Ports, contracts.WorkspaceApplicationPort{Name: "control", Port: 8091, Protocol: "TCP"})
	input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: controlSecretInput, Target: "/run/secrets/control-token"}}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: controlSecretInput, SecretRef: controlSecretRef, Version: "control-v1", Key: "token"}}
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	input.ConfigurationDigest = digest
	if err := contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		t.Fatalf("fixture input is not a valid runtime configuration: %v", err)
	}
	contract := ModelConfigurationContract{
		Protocol: ModelConfigurationProtocol, PortName: "control",
		ApplyPath: "/control/models", ReadbackPath: "/control/models",
		RequestFields: []string{"version", "selections"}, ReadbackFields: []string{"appliedVersion", "selections"},
		AuthorizationSecretInputName: controlSecretInput,
	}
	return input, contract
}

// modelConfigurationCluster is the installation namespace the fixture's confirmed
// binding points into: the Secret the application was injected from.
func modelConfigurationCluster(t *testing.T, input WorkspaceApplicationRuntimeInput) (*fakeCluster, *Executor) {
	t.Helper()
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	cluster.store(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{
		"name": controlSecretRef,
		"annotations": map[string]any{
			"oplcloud.cn/account-id": input.AccountID, "oplcloud.cn/workspace-id": input.WorkspaceID, "oplcloud.cn/secret-version": "control-v1",
		},
	}, "data": map[string]any{"token": base64String(controlSecretValue)}})
	return cluster, testExecutor(cluster, testInstallation(map[string]string{"app.kubernetes.io/name": "opl-cloud"}))
}

func modelConfigurationExecutor(t *testing.T, input WorkspaceApplicationRuntimeInput, stub *applicationStub) *Executor {
	t.Helper()
	_, executor := modelConfigurationCluster(t, input)
	executor.HTTPClient = &http.Client{Transport: stub}
	return executor
}

// TestModelConfigurationApplyReadsTheDeclaredVersionBack is the decisive test of
// this interface: the boundary addresses the port the revision declares, presents
// the value of the declared authorization secret input, sends the exact declared
// payload, and reports only the version the application answered with.
func TestModelConfigurationApplyReadsTheDeclaredVersionBack(t *testing.T) {
	input, contract := modelConfigurationFixture(t)
	stub := &applicationStub{
		apply:    stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2"}`},
		readback: stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2","selections":[{"slot":"chat","modelId":"model-b"},{"slot":"embed","modelId":"model-a"}]}`},
	}
	executor := modelConfigurationExecutor(t, input, stub)
	request := ModelConfigurationRequest{Version: 2, Selections: []ModelSelection{{Slot: "embed", ModelID: "model-a"}, {Slot: "chat", ModelID: "model-b"}}}
	readback, err := executor.ApplyModelConfiguration(context.Background(), input, contract, request)
	if err != nil {
		t.Fatal(err)
	}
	if readback.AppliedVersion != 2 || len(readback.Selections) != 2 {
		t.Fatalf("readback=%+v want the applied version the application reported", readback)
	}
	service := workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	if len(stub.requests) != 2 {
		t.Fatalf("requests=%+v want one apply and one readback", stub.requests)
	}
	for index, want := range []struct{ method, path string }{{http.MethodPut, contract.ApplyPath}, {http.MethodGet, contract.ReadbackPath}} {
		issued := stub.requests[index]
		if issued.method != want.method {
			t.Fatalf("request %d method=%s want %s", index, issued.method, want.method)
		}
		if issued.url != "http://"+service+":8091"+want.path {
			t.Fatalf("request %d url=%s want the declared port's destination", index, issued.url)
		}
		if issued.header.Get("Authorization") != "Bearer "+controlSecretValue {
			t.Fatalf("request %d authorization=%q want the declared secret input's own value", index, issued.header.Get("Authorization"))
		}
	}
	if stub.requests[0].header.Get("Content-Type") != "application/json" {
		t.Fatalf("apply content-type=%q", stub.requests[0].header.Get("Content-Type"))
	}
	var body struct {
		Version    string `json:"version"`
		Selections []struct {
			Slot    string `json:"slot"`
			ModelID string `json:"modelId"`
		} `json:"selections"`
	}
	if json.Unmarshal([]byte(stub.requests[0].body), &body) != nil {
		t.Fatalf("apply body=%s is not the declared payload", stub.requests[0].body)
	}
	if body.Version != "2" || len(body.Selections) != 2 || body.Selections[0].Slot != "chat" || body.Selections[0].ModelID != "model-b" {
		t.Fatalf("apply body=%s want the requested version and the declared selection wires", stub.requests[0].body)
	}
	if stub.requests[1].body != "" {
		t.Fatalf("readback body=%s want no body", stub.requests[1].body)
	}
}

// TestModelConfigurationApplyRefusesAnUnreportedVersion proves the returned version
// is the application's answer and never the requested one.
func TestModelConfigurationApplyRefusesAnUnreportedVersion(t *testing.T) {
	input, contract := modelConfigurationFixture(t)
	for _, body := range []string{`{"appliedVersion":"1"}`, `{"appliedVersion":"3"}`, `{"appliedVersion":""}`, `not-json`} {
		stub := &applicationStub{
			apply:    stubResponse{status: http.StatusOK, body: `{}`},
			readback: stubResponse{status: http.StatusOK, body: body},
		}
		executor := modelConfigurationExecutor(t, input, stub)
		readback, err := executor.ApplyModelConfiguration(context.Background(), input, contract, ModelConfigurationRequest{Version: 2})
		if !errors.Is(err, ErrModelConfigurationUnconfirmed) {
			t.Fatalf("readback=%+v err=%v for %s want the unconfirmed refusal", readback, err, body)
		}
	}
}

// TestModelConfigurationApplyRefusesAConflictingSelectionPayload proves one version
// names one payload: an application that answers the requested version with other
// selections has not applied the configuration that was requested.
func TestModelConfigurationApplyRefusesAConflictingSelectionPayload(t *testing.T) {
	input, contract := modelConfigurationFixture(t)
	stub := &applicationStub{
		apply:    stubResponse{status: http.StatusOK, body: `{}`},
		readback: stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2","selections":[{"slot":"chat","modelId":"model-c"}]}`},
	}
	executor := modelConfigurationExecutor(t, input, stub)
	request := ModelConfigurationRequest{Version: 2, Selections: []ModelSelection{{Slot: "chat", ModelID: "model-b"}}}
	if _, err := executor.ApplyModelConfiguration(context.Background(), input, contract, request); !errors.Is(err, ErrModelConfigurationConflict) {
		t.Fatalf("err=%v want the conflicting-payload refusal", err)
	}
	// A declared selections field the application omits is not an applied
	// configuration either.
	stub.readback = stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2"}`}
	if _, err := executor.ApplyModelConfiguration(context.Background(), input, contract, request); !errors.Is(err, ErrModelConfigurationUnconfirmed) {
		t.Fatalf("err=%v want the unconfirmed refusal for an absent declared field", err)
	}
}

// TestModelConfigurationApplyRefusesAnApplicationThatRefusedTheApply proves a
// refused apply never advances to a readback that could report an older version as
// applied.
func TestModelConfigurationApplyRefusesAnApplicationThatRefusedTheApply(t *testing.T) {
	input, contract := modelConfigurationFixture(t)
	stub := &applicationStub{
		apply:    stubResponse{status: http.StatusInternalServerError, body: `{"error":"busy"}`},
		readback: stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2"}`},
	}
	executor := modelConfigurationExecutor(t, input, stub)
	if _, err := executor.ApplyModelConfiguration(context.Background(), input, contract, ModelConfigurationRequest{Version: 2}); !errors.Is(err, ErrModelConfigurationUnconfirmed) {
		t.Fatalf("err=%v want the unconfirmed refusal", err)
	}
	if len(stub.requests) != 1 {
		t.Fatalf("requests=%+v want no readback after a refused apply", stub.requests)
	}
}

// TestModelConfigurationApplyRefusesAnUnexecutableContract proves the boundary
// refuses a declaration it cannot execute instead of guessing a wire.
func TestModelConfigurationApplyRefusesAnUnexecutableContract(t *testing.T) {
	base, contract := modelConfigurationFixture(t)
	cases := map[string]func(input WorkspaceApplicationRuntimeInput, contract ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract){
		"protocol": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.Protocol = "opl-model-config/v2"
			return in, c
		},
		"port": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.PortName = "metrics"
			return in, c
		},
		"apply path": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.ApplyPath = "control/models"
			return in, c
		},
		"escaping path": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.ReadbackPath = "/control/../models"
			return in, c
		},
		"request fields": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.RequestFields = []string{"version", "selections", "extra"}
			return in, c
		},
		"readback fields": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.ReadbackFields = []string{"appliedVersion"}
			return in, c
		},
		"authorization name": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			c.AuthorizationSecretInputName = "absent"
			return in, c
		},
		"undeclared revision input": func(in WorkspaceApplicationRuntimeInput, c ModelConfigurationContract) (WorkspaceApplicationRuntimeInput, ModelConfigurationContract) {
			in.Revision.SecretInputs = nil
			return in, c
		},
	}
	for name, mutate := range cases {
		input, mutated := mutate(base, contract)
		stub := &applicationStub{apply: stubResponse{status: http.StatusOK, body: `{}`}, readback: stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2"}`}}
		executor := modelConfigurationExecutor(t, input, stub)
		if _, err := executor.ApplyModelConfiguration(context.Background(), input, mutated, ModelConfigurationRequest{Version: 2}); !errors.Is(err, ErrModelConfigurationUnsupported) {
			t.Fatalf("%s err=%v want the unsupported refusal", name, err)
		}
		if len(stub.requests) != 0 {
			t.Fatalf("%s issued %+v, want nothing addressed", name, stub.requests)
		}
	}
	// A non-positive version is not a version the application can have applied.
	stub := &applicationStub{}
	executor := modelConfigurationExecutor(t, base, stub)
	if _, err := executor.ApplyModelConfiguration(context.Background(), base, contract, ModelConfigurationRequest{}); !errors.Is(err, ErrModelConfigurationUnsupported) {
		t.Fatalf("err=%v want the unsupported refusal for a non-positive version", err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("requests=%+v want nothing addressed", stub.requests)
	}
}

// TestModelConfigurationApplyRefusesAnUndeliveredAuthorization proves the boundary
// authorizes with the declared secret input's own delivered value or not at all: a
// runtime whose input carries no confirmed delivery for that input, or whose
// delivered Secret is absent from the namespace, is refused rather than authorized
// with a value this boundary invented.
func TestModelConfigurationApplyRefusesAnUndeliveredAuthorization(t *testing.T) {
	input, contract := modelConfigurationFixture(t)
	stub := &applicationStub{apply: stubResponse{status: http.StatusOK, body: `{}`}, readback: stubResponse{status: http.StatusOK, body: `{"appliedVersion":"2"}`}}

	unbound := input
	unbound.SecretBindings = nil
	executor := modelConfigurationExecutor(t, unbound, stub)
	if _, err := executor.ApplyModelConfiguration(context.Background(), unbound, contract, ModelConfigurationRequest{Version: 2}); !errors.Is(err, ErrModelConfigurationUnsupported) {
		t.Fatalf("unbound err=%v want the unsupported refusal", err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("requests=%+v want nothing addressed", stub.requests)
	}

	// The binding is confirmed but the Secret it names is not in the namespace, so
	// the value the application holds cannot be read and nothing is addressed.
	cluster, executor := modelConfigurationCluster(t, input)
	delete(cluster.objects, "Secret/"+controlSecretRef)
	executor.HTTPClient = &http.Client{Transport: stub}
	if _, err := executor.ApplyModelConfiguration(context.Background(), input, contract, ModelConfigurationRequest{Version: 2}); !errors.Is(err, ErrModelConfigurationUnsupported) {
		t.Fatalf("absent Secret err=%v want the unsupported refusal", err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("requests=%+v want nothing addressed", stub.requests)
	}
}
