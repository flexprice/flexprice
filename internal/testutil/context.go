package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

func SetupContext() context.Context {
	ctx := context.Background()
	ctx = types.WithWriterPinning(ctx)
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	ctx = context.WithValue(ctx, types.CtxRequestID, types.GenerateUUID())
	// Add default environment ID for testing
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_sandbox")
	return ctx
}
