package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEnvironmentID = "env_topup_test"

func topUpTestContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = types.SetEnvironmentID(ctx, testEnvironmentID)
	return ctx
}

func seedGrant(t *testing.T, store *InMemoryEntitlementGrantStore, mutate func(*entitlementgrant.EntitlementGrant)) *entitlementgrant.EntitlementGrant {
	t.Helper()
	ctx := topUpTestContext()
	g := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUID(),
		EntitlementConfigID: "ent_1",
		CustomerID:          "cust_1",
		SubscriptionID:      "sub_1",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_1",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(1000),
		Usage:               decimal.Zero,
		ValidFrom:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:             time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       testEnvironmentID,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	if mutate != nil {
		mutate(g)
	}
	created, err := store.Create(ctx, g)
	require.NoError(t, err)
	return created
}

func TestInMemoryTopUpQuota_IncrementsAndMergesMetadata(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := topUpTestContext()

	g := seedGrant(t, store, func(g *entitlementgrant.EntitlementGrant) {
		g.Metadata = types.Metadata{"seeded": "yes"}
	})

	at := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	got, err := store.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), at,
		types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)

	assert.True(t, got.Quota.Equal(decimal.NewFromInt(1300)), "quota should be 1300, got %s", got.Quota)
	assert.Equal(t, "yes", got.Metadata["seeded"], "top up must merge, not replace, metadata")
	assert.Equal(t, "0.5", got.Metadata["proration_addon_assoc_a1"])
	assert.Equal(t, types.EntitlementGrantStatusActive, got.GrantStatus,
		"top up must not touch grant_status — the evaluator derives it")
}

// Several addons on one pooled bucket each add their own delta under their own
// marker; the accumulated quota is the sum.
func TestInMemoryTopUpQuota_Accumulates(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := topUpTestContext()
	g := seedGrant(t, store, nil)
	at := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	_, err := store.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), at, types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)
	got, err := store.TopUpQuota(ctx, g.ID, decimal.NewFromInt(150), at, types.Metadata{"proration_addon_assoc_a2": "0.25"})
	require.NoError(t, err)

	assert.True(t, got.Quota.Equal(decimal.NewFromInt(1450)), "quota should be 1450, got %s", got.Quota)
	assert.Equal(t, "0.5", got.Metadata["proration_addon_assoc_a1"], "first marker must survive the second top up")
	assert.Equal(t, "0.25", got.Metadata["proration_addon_assoc_a2"])
}

func TestInMemoryTopUpQuota_QuotaCrossedAt(t *testing.T) {
	early := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	// Seed quota is 1000 and every case tops up by 300, so the new quota is 1300:
	// usage 1500 leaves the row exhausted, usage 1200 restores headroom.
	tests := []struct {
		name     string
		crossed  *time.Time
		usage    int64
		at       time.Time
		wantNil  bool
		wantTime time.Time
	}{
		{
			name:    "unset stays unset",
			crossed: nil,
			at:      late,
			wantNil: true,
		},
		{
			name:    "cleared when the top up restores headroom",
			crossed: &early,
			usage:   1200,
			at:      late,
			wantNil: true,
		},
		{
			name:     "advances to the top up time when still over the new quota",
			crossed:  &early,
			usage:    1500,
			at:       late,
			wantTime: late,
		},
		{
			name:     "never moves backwards",
			crossed:  &late,
			usage:    1500,
			at:       early,
			wantTime: late,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryEntitlementGrantStore()
			ctx := topUpTestContext()
			g := seedGrant(t, store, func(g *entitlementgrant.EntitlementGrant) {
				g.QuotaCrossedAt = tt.crossed
				g.Usage = decimal.NewFromInt(tt.usage)
				if tt.crossed != nil {
					g.GrantStatus = types.EntitlementGrantStatusExhausted
				}
			})

			got, err := store.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), tt.at, nil)
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got.QuotaCrossedAt)
				return
			}
			require.NotNil(t, got.QuotaCrossedAt)
			assert.True(t, got.QuotaCrossedAt.Equal(tt.wantTime),
				"expected quota_crossed_at %s, got %s", tt.wantTime, got.QuotaCrossedAt)
		})
	}
}

func TestInMemoryTopUpQuota_MissingGrant(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	_, err := store.TopUpQuota(topUpTestContext(), "eg_missing", decimal.NewFromInt(1), time.Now().UTC(), nil)
	assert.Error(t, err)
}

// The top up writes quota/metadata/quota_crossed_at only; a subsequent snapshot
// write must not resurrect the pre-top-up quota.
func TestInMemoryTopUpQuota_SurvivesSnapshotWrite(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := topUpTestContext()
	g := seedGrant(t, store, nil)

	toppedUp, err := store.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300),
		time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC), nil)
	require.NoError(t, err)

	// The evaluator holds a grant read before the top up and writes its snapshot.
	stale := entitlementgrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(42)).
		Build()
	require.NoError(t, store.UpdateSnapshot(ctx, stale))

	after, err := store.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.Quota.Equal(toppedUp.Quota),
		"snapshot write clobbered the topped-up quota: %s", after.Quota)
	assert.True(t, after.Usage.Equal(decimal.NewFromInt(42)))
}
