package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// TestEnvironmentID is the environment ID carried by every test context.
// All suite and plain-function tests share it so fixtures and queries are
// scoped to the same environment, exercising the multi-tenancy filters.
const TestEnvironmentID = "env_sandbox"

func SetupContext() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	ctx = context.WithValue(ctx, types.CtxRequestID, types.GenerateUUID())
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, TestEnvironmentID)
	return ctx
}
