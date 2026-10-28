package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestForEachConnection(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	newConn := func(id string, provider types.SecretProvider, owner string) *connection.Connection {
		return &connection.Connection{
			ID:              id,
			ProviderType:    provider,
			EnvironmentID:   "env_1",
			BillingEntityID: lo.ToPtr(owner),
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}
	}

	tests := []struct {
		name      string
		conns     []*connection.Connection
		failing   string
		wantCalls map[string]string // connection id -> owner seen in ctx
		wantErr   func(error) bool
	}{
		{
			name:    "no connection of the provider",
			conns:   []*connection.Connection{newConn("conn_stripe", types.SecretProviderStripe, "be_a")},
			wantErr: ierr.IsNotFound,
		},
		{
			name: "every connection runs with its pin and owner; failures join",
			conns: []*connection.Connection{
				newConn("conn_qb_a", types.SecretProviderQuickBooks, "be_a"),
				newConn("conn_qb_b", types.SecretProviderQuickBooks, "be_b"),
				newConn("conn_stripe", types.SecretProviderStripe, "be_a"),
			},
			failing:   "conn_qb_a",
			wantCalls: map[string]string{"conn_qb_a": "be_a", "conn_qb_b": "be_b"},
			wantErr:   func(err error) bool { return err != nil && !ierr.IsNotFound(err) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := testutil.NewInMemoryConnectionStore()
			for _, c := range tt.conns {
				require.NoError(t, repo.Create(ctx, c))
			}

			calls := map[string]string{}
			err := ForEachConnection(ctx, repo, types.SecretProviderQuickBooks, func(ctx context.Context, c *connection.Connection) error {
				require.Equal(t, c.ID, types.GetConnectionID(ctx))
				calls[c.ID] = types.GetBillingEntityID(ctx)
				if c.ID == tt.failing {
					return errors.New("sync failed")
				}
				return nil
			})

			require.True(t, tt.wantErr(err), "unexpected error: %v", err)
			require.Equal(t, lo.Ternary(tt.wantCalls == nil, map[string]string{}, tt.wantCalls), calls)
		})
	}
}
