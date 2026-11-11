package connection

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestInScope(t *testing.T) {
	conn := func(id string, provider types.SecretProvider, entityID string) *Connection {
		return &Connection{ID: id, ProviderType: provider, BillingEntityID: lo.EmptyableToPtr(entityID)}
	}
	s3 := conn("conn_s3", types.SecretProviderS3, "")
	stripeA := conn("conn_stripe_a", types.SecretProviderStripe, "be_a")
	stripeB := conn("conn_stripe_b", types.SecretProviderStripe, "be_b")
	hubspotA := conn("conn_hubspot_a", types.SecretProviderHubSpot, "be_a")
	unowned := []*Connection{
		s3,
		conn("conn_stripe", types.SecretProviderStripe, ""),
		conn("conn_hubspot", types.SecretProviderHubSpot, ""),
	}
	owned := []*Connection{s3, stripeA, stripeB, hubspotA}

	withEntity := func(id string) context.Context {
		return types.SetBillingEntityID(context.Background(), id)
	}

	tests := []struct {
		name  string
		ctx   context.Context
		conns []*Connection
		want  []string
	}{
		{
			name:  "off_tenant_no_owners",
			ctx:   context.Background(),
			conns: unowned,
			want:  []string{"conn_s3", "conn_stripe", "conn_hubspot"},
		},
		{
			name:  "entity_in_ctx",
			ctx:   withEntity("be_a"),
			conns: owned,
			want:  []string{"conn_s3", "conn_stripe_a", "conn_hubspot_a"},
		},
		{
			name:  "entity_in_ctx_no_owners",
			ctx:   withEntity("be_a"),
			conns: unowned,
			want:  []string{"conn_s3"},
		},
		{
			name:  "no_entity_owned_exist",
			ctx:   context.Background(),
			conns: owned,
			want:  []string{"conn_s3"},
		},
		{
			name:  "entity_without_provider",
			ctx:   withEntity("be_b"),
			conns: []*Connection{s3, stripeA, hubspotA},
			want:  []string{"conn_s3"},
		},
	}

	ids := func(conns []*Connection) []string {
		return lo.Map(conns, func(c *Connection, _ int) string { return c.ID })
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ids(InScope(tt.ctx, tt.conns)))
		})
	}

	t.Run("storage_always_in_scope", func(t *testing.T) {
		for _, ctx := range []context.Context{context.Background(), withEntity("be_a"), withEntity("be_missing")} {
			for _, conns := range [][]*Connection{unowned, owned} {
				require.Contains(t, ids(InScope(ctx, conns)), "conn_s3")
			}
		}
	})
}
