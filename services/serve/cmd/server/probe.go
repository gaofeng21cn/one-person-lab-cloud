package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// This result describes only a synthetic transport/readback probe. No customer
// identifier, address, token or upstream error is part of the output contract.
type workspaceProbeResult struct {
	SchemaVersion   int    `json:"schemaVersion"`
	OperationMode   string `json:"operationMode"`
	Status          string `json:"status"`
	ErrorCode       string `json:"errorCode"`
	Authenticated   bool   `json:"authenticated"`
	WorkspaceClosed bool   `json:"workspaceClosed"`
	MutationCounts  struct {
		Business int `json:"business"`
		Provider int `json:"provider"`
	} `json:"mutationCounts"`
}

func runWorkspaceAccessProbe(args []string) int {
	// The native result is the only diagnostic surface of this short-lived
	// command, even when the container enables verbose gRPC transport logging.
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, io.Discard))
	result := probeWorkspaceAccess(args)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return 1
	}
	if result.Status == "observed" {
		return 0
	}
	return 1
}

func probeWorkspaceAccess(args []string) workspaceProbeResult {
	result := workspaceProbeResult{SchemaVersion: 1, OperationMode: "serve_workspace_access_probe", Status: "failed", ErrorCode: "CONFIG_INVALID"}
	if len(args) != 0 {
		result.ErrorCode = "USAGE_INVALID"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	address, token := strings.TrimSpace(os.Getenv("OPL_WORKSPACE_ADDR")), os.Getenv("OPL_WORKSPACE_TOKEN")
	if address == "" || len(strings.TrimSpace(token)) < owneridentity.MinimumTokenLength {
		return result
	}
	// Read only the existing transport configuration. Loading ownerservice.Config
	// would also load database/startup requirements unrelated to this read-only call.
	transport := owneridentity.TLSFromEnv(os.Getenv)
	result.ErrorCode = "TLS_CONFIG_INVALID"
	if transport.AllowInsecureLocal {
		return result
	}
	options, err := transport.DialOptions(owneridentity.Serve.Service(), owneridentity.DeploymentUnitTransportPrincipal(owneridentity.Workspace), token)
	if err != nil {
		return result
	}
	conn, err := grpc.NewClient(address, append(options, grpc.WithDisableRetry())...)
	if err != nil {
		result.ErrorCode = "CONFIG_INVALID"
		return result
	}
	defer conn.Close()
	// Never accept a customer workspace or reuse a persistent synthetic identity.
	// This UUID is an opaque read target; it is never created, logged or returned.
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		result.ErrorCode = "RANDOM_FAILED"
		return result
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	workspaceID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	answer, err := api.NewWorkspaceAccessReadbackClient(conn).ReadWorkspaceAccess(ctx, &api.ReadWorkspaceAccessRequest{WorkspaceId: workspaceID})
	if err != nil {
		result.ErrorCode = "RPC_FAILED"
		switch status.Code(err) {
		case codes.PermissionDenied:
			result.ErrorCode = "RPC_PERMISSION_DENIED"
		case codes.Unauthenticated:
			result.ErrorCode = "RPC_UNAUTHENTICATED"
		case codes.DeadlineExceeded:
			result.ErrorCode = "RPC_DEADLINE_EXCEEDED"
		case codes.Unavailable:
			result.ErrorCode = "RPC_UNAVAILABLE"
		}
		return result
	}
	if answer.GetWorkspaceId() != workspaceID {
		result.ErrorCode = "RESPONSE_MISMATCH"
		return result
	}
	if answer.GetVerdict() == api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED {
		result.ErrorCode = "UNEXPECTED_PERMITTED"
		return result
	}
	if answer.GetVerdict() != api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED {
		result.ErrorCode = "UNEXPECTED_VERDICT"
		return result
	}
	result.Authenticated, result.WorkspaceClosed = true, true
	result.Status, result.ErrorCode = "observed", "none"
	return result
}
