// Package requestcontext carries the typed caller context across in-process
// adapters without changing the public method signatures of owner clients.
package requestcontext

import (
	"context"

	api "opl-cloud/packages/contracts/go/api"
)

type key struct{}

func WithCallContext(ctx context.Context, call *api.CallContext) context.Context {
	return context.WithValue(ctx, key{}, call)
}

func CallContext(ctx context.Context) *api.CallContext {
	call, _ := ctx.Value(key{}).(*api.CallContext)
	return call
}
