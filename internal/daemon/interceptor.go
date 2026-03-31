package daemon

import (
	"context"

	verrors "github.com/rotemtam/virtui/internal/errors"
	"google.golang.org/grpc"
)

// ErrorInterceptor wraps VirtuiErrors into gRPC statuses with structured error details.
func ErrorInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		if ve, ok := err.(*verrors.VirtuiError); ok {
			return nil, ve.ToGRPCStatus().Err()
		}
	}
	return resp, err
}
