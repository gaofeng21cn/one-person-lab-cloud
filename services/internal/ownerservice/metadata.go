package ownerservice

import (
	"context"

	"google.golang.org/grpc/metadata"
)

func metadataAppend(ctx context.Context, key, value string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, key, value)
}
