package auth

import (
	"context"

	"google.golang.org/grpc"
)

// Interceptor examples:
// https://github.com/grpc/grpc-go/blob/master/examples/features/interceptor/server/main.go

// UnaryInterceptor extracts the caller's identity from the TLS certificate and
// stores it in the context.
func UnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id, err := identityFromTLS(ctx)
	if err != nil {
		return nil, err
	}
	return handler(NewContext(ctx, id), req)
}

// StreamInterceptor extracts the caller's identity from the TLS certificate and
// stores it in the context.
func StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	id, err := identityFromTLS(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, &wrappedStream{ServerStream: ss, ctx: NewContext(ss.Context(), id)})
}

// wrappedStream overrides Context() to return the context with the identity.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
