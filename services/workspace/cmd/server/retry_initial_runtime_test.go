package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

const retryCLIInput = `{"context":{"actorId":"admin-operator","sessionId":"session_abc123","requestId":"request-retry","idempotencyKey":"retry-key","scope":{"platform":{}}},"owner":"OPERATION_OWNER_ENUM_WORKSPACE","operationId":"original-operation","body":{"reason":"repair acknowledged"}}`

func TestInitialRuntimeRetryCLIReconcileSchema(t *testing.T) {
	var output bytes.Buffer
	calls := 0
	exit := runInitialRuntimeRetry(nil, strings.NewReader(retryCLIInput), &output, func(_ context.Context, r *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
		calls++
		if r.GetContext().GetSessionId() != "session_abc123" || r.GetOperationId() != "original-operation" || r.GetContext().GetScope().GetPlatform() == nil || r.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE {
			t.Fatal("request changed")
		}
		return &api.Operation{OperationId: "secret-op", ResourceId: "secret-workspace", Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME}, nil
	})
	if exit != 0 || calls != 1 {
		t.Fatalf("exit=%d calls=%d", exit, calls)
	}
	want := `{"schemaVersion":1,"status":"replayed","errorCode":"OK","operationStatus":"awaiting_confirmation","operationStage":"runtime"}` + "\n"
	if output.String() != want {
		t.Fatalf("output=%s", output.String())
	}
}

func TestInitialRuntimeRetryCLIRejectsUntrustedJSON(t *testing.T) {
	cases := map[string]string{
		"unknown":          strings.Replace(retryCLIInput, `"body":`, `"descriptor":{},"body":`, 1),
		"duplicate root":   strings.Replace(retryCLIInput, `"operationId":`, `"operationId":"other","operationId":`, 1),
		"duplicate nested": strings.Replace(retryCLIInput, `"sessionId":`, `"sessionId":"other","sessionId":`, 1),
		"trailing":         retryCLIInput + "{}",
		"null":             strings.Replace(retryCLIInput, `"session_abc123"`, `null`, 1),
		"missing":          strings.Replace(retryCLIInput, `"sessionId":"session_abc123",`, "", 1),
		"tenant":           strings.Replace(retryCLIInput, `"platform":{}`, `"tenant":{"tenantId":"other"}`, 1),
		"grant":            strings.Replace(retryCLIInput, `"sessionId":`, `"acceptedOperationGrantId":"fake","sessionId":`, 1),
		"utf8":             retryCLIInput + string([]byte{255}),
		"oversize":         retryCLIInput + strings.Repeat(" ", initialRuntimeRetryInputLimit),
		"array":            "[]",
		"wrong type":       strings.Replace(retryCLIInput, `"session_abc123"`, `123`, 1),
		"empty":            strings.Replace(retryCLIInput, `"session_abc123"`, `""`, 1),
		"null platform":    strings.Replace(retryCLIInput, `"platform":{}`, `"platform":null`, 1),
		"owner":            strings.Replace(retryCLIInput, "OPERATION_OWNER_ENUM_WORKSPACE", "OPERATION_OWNER_ENUM_SERVE", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			exit := runInitialRuntimeRetry(nil, strings.NewReader(input), &output, func(context.Context, *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
				t.Fatal("invalid input executed")
				return nil, nil
			})
			if exit != 1 {
				t.Fatalf("exit=%d", exit)
			}
			var result initialRuntimeRetryResult
			if json.Unmarshal(output.Bytes(), &result) != nil || result.Status != "blocked" || result.ErrorCode != "InvalidArgument" {
				t.Fatalf("output=%s", output.String())
			}
			if strings.Contains(output.String(), "session_abc123") {
				t.Fatal("session leaked")
			}
		})
	}
}

func TestInitialRuntimeRetryCLIRedactsFailureAndRefusesArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"session_abc123"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			exit := runInitialRuntimeRetry(args, strings.NewReader(retryCLIInput), &output, func(context.Context, *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
				return nil, status.Error(codes.PermissionDenied, "session_abc123 private diagnostic")
			})
			if exit != 1 || strings.Contains(output.String(), "session_abc123") || strings.Contains(output.String(), "private") {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			var result map[string]any
			if json.Unmarshal(output.Bytes(), &result) != nil || len(result) != 5 || result["status"] != "blocked" || result["operationStatus"] != nil || result["operationStage"] != nil {
				t.Fatalf("output=%s", output.String())
			}
			want := "PermissionDenied"
			if len(args) > 0 {
				want = "InvalidArgument"
			}
			if result["errorCode"] != want {
				t.Fatalf("output=%s", output.String())
			}
		})
	}
}

func TestInitialRuntimeRetryCLIReportsKnownFailureState(t *testing.T) {
	var output bytes.Buffer
	exit := runInitialRuntimeRetry(nil, strings.NewReader(retryCLIInput), &output, func(context.Context, *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
		return &api.Operation{Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME, ObservationResult: nil}, status.Error(codes.FailedPrecondition, "private")
	})
	if exit != 1 || !strings.Contains(output.String(), `"operationStatus":"needs_attention"`) {
		t.Fatalf("exit=%d output=%s", exit, output.String())
	}
}

func TestInitialRuntimeRetryCLIProcessEntry(t *testing.T) {
	if os.Getenv("OPL_RETRY_INITIAL_PROCESS_TEST") == "1" {
		os.Args = []string{"opl-workspace", "retry-initial-runtime"}
		main()
		return
	}
	// Invalid input must take the protected CLI branch before server startup,
	// migrations or configuration diagnostics could write anything to stderr.
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitialRuntimeRetryCLIProcessEntry$")
	cmd.Env = append(os.Environ(), "OPL_RETRY_INITIAL_PROCESS_TEST=1", "DATABASE_URL=not-a-database")
	cmd.Stdin = strings.NewReader(`{"context":{"sessionId":"private-session"}}`)
	var output, diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostics
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("CLI exit=%v", err)
	}
	want := `{"schemaVersion":1,"status":"blocked","errorCode":"InvalidArgument","operationStatus":null,"operationStage":null}` + "\n"
	if output.String() != want || diagnostics.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", output.String(), diagnostics.String())
	}
}

func TestInitialRuntimeRetryCLIAcceptsProtobufJSONFieldNames(t *testing.T) {
	input := strings.NewReplacer(
		"actorId", "actor_id", "sessionId", "session_id", "requestId", "request_id",
		"idempotencyKey", "idempotency_key", "operationId", "operation_id",
	).Replace(retryCLIInput)
	request, err := decodeInitialRuntimeRetry(strings.NewReader(input))
	if err != nil || request.GetContext().GetActorId() != "admin-operator" || request.GetContext().GetSessionId() != "session_abc123" || request.GetOperationId() != "original-operation" {
		t.Fatalf("valid protobuf field names refused: %v", err)
	}
}
