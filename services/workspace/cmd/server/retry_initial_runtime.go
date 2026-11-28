package main

import (
	"context"
	"io"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/workspace/module"
)

const initialRuntimeRetryInputLimit = module.InitialRuntimeRetryInputLimit

type initialRuntimeRetryResult = module.InitialRuntimeRetryResult

type initialRuntimeRetryExecutor = module.InitialRuntimeRetryExecutor

func runInitialRuntimeRetry(args []string, input io.Reader, output io.Writer, execute initialRuntimeRetryExecutor) int {
	return module.RunInitialRuntimeRetry(args, input, output, execute)
}

func executeInitialRuntimeRetry(ctx context.Context, request *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
	return module.ExecuteInitialRuntimeRetry(ctx, request)
}

func decodeInitialRuntimeRetry(input io.Reader) (*api.ReconcileOperationRpcRequest, error) {
	return module.DecodeInitialRuntimeRetry(input)
}
