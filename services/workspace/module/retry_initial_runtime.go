package module

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/workspace/internal/launch"
)

// InitialRuntimeRetryInputLimit bounds untrusted JSON stdin payloads.
const InitialRuntimeRetryInputLimit = 16 * 1024

// InitialRuntimeRetryResult is the machine-readable CLI output.
type InitialRuntimeRetryResult struct {
	SchemaVersion   int     `json:"schemaVersion"`
	Status          string  `json:"status"`
	ErrorCode       string  `json:"errorCode"`
	OperationStatus *string `json:"operationStatus"`
	OperationStage  *string `json:"operationStage"`
}

// InitialRuntimeRetryExecutor reconciles one accepted retry request.
type InitialRuntimeRetryExecutor func(context.Context, *api.ReconcileOperationRpcRequest) (*api.Operation, error)

// RunInitialRuntimeRetryCLI executes the CLI command with live owner endpoints.
func RunInitialRuntimeRetryCLI(args []string, input io.Reader, output io.Writer) int {
	return RunInitialRuntimeRetry(args, input, output, ExecuteInitialRuntimeRetry)
}

// RunInitialRuntimeRetry validates input, calls execute, and emits bounded JSON output.
func RunInitialRuntimeRetry(args []string, input io.Reader, output io.Writer, execute InitialRuntimeRetryExecutor) int {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, io.Discard))
	result := InitialRuntimeRetryResult{SchemaVersion: 1, Status: "blocked", ErrorCode: codes.InvalidArgument.String()}
	var op *api.Operation
	var err error
	if len(args) == 0 {
		var request *api.ReconcileOperationRpcRequest
		request, err = DecodeInitialRuntimeRetry(input)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
			defer cancel()
			op, err = execute(ctx, request)
			result.ErrorCode = status.Code(err).String()
			if err == nil && op == nil {
				result.ErrorCode = codes.DataLoss.String()
			} else if err == nil {
				result.Status = "replayed"
			}
		}
	}
	if op != nil {
		result.OperationStatus = initialRuntimeRetryEnum(op.GetStatus().String(), "OPERATION_STATUS_ENUM_")
		result.OperationStage = initialRuntimeRetryEnum(op.GetStage().String(), "OPERATION_STAGE_ENUM_")
	}
	if json.NewEncoder(output).Encode(result) != nil {
		return 1
	}
	if result.Status == "replayed" {
		return 0
	}
	return 1
}

func initialRuntimeRetryEnum(value, prefix string) *string {
	if !strings.HasPrefix(value, prefix) || strings.HasSuffix(value, "_UNSPECIFIED") {
		return nil
	}
	short := strings.ToLower(strings.TrimPrefix(value, prefix))
	return &short
}

// DecodeInitialRuntimeRetry validates and unmarshals untrusted CLI stdin JSON.
func DecodeInitialRuntimeRetry(input io.Reader) (*api.ReconcileOperationRpcRequest, error) {
	invalid := status.Error(codes.InvalidArgument, "invalid initial runtime retry request")
	raw, err := io.ReadAll(io.LimitReader(input, InitialRuntimeRetryInputLimit+1))
	if err != nil || len(raw) > InitialRuntimeRetryInputLimit || !utf8.Valid(raw) {
		return nil, invalid
	}
	request := &api.ReconcileOperationRpcRequest{}
	if protojson.Unmarshal(raw, request) != nil {
		return nil, invalid
	}
	if err = launch.ValidateInitialRuntimeRetry(request); err != nil {
		return nil, err
	}
	return request, nil
}

// ExecuteInitialRuntimeRetry runs the retry-initial-runtime reconcile RPC against OS env.
func ExecuteInitialRuntimeRetry(ctx context.Context, request *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerWorkspace, ":8184")
	if err != nil || config.TLS.AllowInsecureLocal {
		return nil, status.Error(codes.FailedPrecondition, "owner mTLS configuration required")
	}
	serveAddress := strings.TrimSpace(os.Getenv("OPL_SERVE_ADDR"))
	if config.CloudIdentityAddr == "" || serveAddress == "" {
		return nil, status.Error(codes.FailedPrecondition, "owner endpoints required")
	}
	dial := func(address string, target owneridentity.Service, token string) (*grpc.ClientConn, error) {
		options, err := config.TLS.DialOptions(config.Owner.Service(), target, token)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, "owner mTLS configuration invalid")
		}
		return grpc.NewClient(address, append(options, grpc.WithDisableRetry())...)
	}
	identity, err := dial(config.CloudIdentityAddr, owneridentity.DeploymentUnitTransportPrincipal(owneridentity.Tenant), config.CloudIdentityToken)
	if err != nil {
		return nil, err
	}
	defer identity.Close()
	serve, err := dial(serveAddress, owneridentity.DeploymentUnitTransportPrincipal(owneridentity.Serve), os.Getenv("OPL_SERVE_TOKEN"))
	if err != nil {
		return nil, err
	}
	defer serve.Close()
	database, err := ownerservice.OpenDatabase(ctx, config.Owner, config.DatabaseURL)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "owner database unavailable")
	}
	defer database.Close()
	service := &launch.Service{Store: database.Store(), Identity: api.NewCloudIdentityAuthorizationClient(identity), Serve: api.NewServeAgentCoordinationClient(serve)}
	return service.RetryInitialRuntime(ctx, request)
}
