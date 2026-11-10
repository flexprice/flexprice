package cache

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGenerateKey(t *testing.T) {
	withIDs := func(tenant, env string) context.Context {
		ctx := context.Background()
		if tenant != "" {
			ctx = context.WithValue(ctx, types.CtxTenantID, tenant)
		}
		if env != "" {
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, env)
		}
		return ctx
	}

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"nil ctx is global", nil, "p:x:1"},
		{"tenant and env", withIDs("t1", "e1"), "t1:e1:p:x:1"},
		{"tenant only", withIDs("t1", ""), "t1:p:x:1"},
		{"other tenant only", withIDs("t2", ""), "t2:p:x:1"},
		{"no tenant", withIDs("", "e1"), "p:x:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, GenerateKey(tt.ctx, "p", "x", 1))
		})
	}

	require.NotEqual(t,
		GenerateKey(withIDs("t1", ""), "p", "x"),
		GenerateKey(withIDs("t2", ""), "p", "x"))
}
