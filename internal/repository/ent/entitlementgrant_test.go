package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainGrant "github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TopUpQuota is raw SQL (ent generates no Add mutator for field.Other), so the
// jsonb merge, the numeric cast and the quota_crossed_at CASE can only be
// verified against a real Postgres. Skips when none is reachable — see
// newRealPostgresTestClient in coupon_test.go.
func newTestEntitlementGrantRepository(t *testing.T) domainGrant.Repository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	return NewEntitlementGrantRepository(client, log)
}

func testGrantContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = types.SetEnvironmentID(ctx, "env_eg_topup_test")
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	return ctx
}

func newTestGrant(ctx context.Context, mutate func(*domainGrant.EntitlementGrant)) *domainGrant.EntitlementGrant {
	// valid_from is unique per grant so repeat runs never collide on the
	// (slot, valid_from) index.
	validFrom := time.Now().UTC().Truncate(time.Microsecond)
	g := &domainGrant.EntitlementGrant{
		ID:                  types.GenerateUUID(),
		EntitlementConfigID: "ent_topup_test",
		CustomerID:          "cust_topup_test",
		SubscriptionID:      "sub_topup_test",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_topup_test",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(1000),
		Usage:               decimal.Zero,
		ValidFrom:           validFrom,
		ValidTo:             validFrom.Add(30 * 24 * time.Hour),
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	g.Status = types.StatusPublished
	if mutate != nil {
		mutate(g)
	}
	return g
}

func createTestGrant(t *testing.T, repo domainGrant.Repository, ctx context.Context, mutate func(*domainGrant.EntitlementGrant)) *domainGrant.EntitlementGrant {
	t.Helper()
	g, err := repo.Create(ctx, newTestGrant(ctx, mutate))
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Delete(context.WithValue(ctx, types.CtxUserID, types.DefaultUserID), g.ID) })
	return g
}

func TestTopUpQuota_IncrementsQuotaAndMergesMetadata(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()

	g := createTestGrant(t, repo, ctx, func(g *domainGrant.EntitlementGrant) {
		g.Metadata = types.Metadata{"seeded": "yes"}
	})

	at := time.Now().UTC()
	got, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), at,
		types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)

	assert.True(t, got.Quota.Equal(decimal.NewFromInt(1300)), "expected quota 1300, got %s", got.Quota)
	assert.Equal(t, "yes", got.Metadata["seeded"], "jsonb merge must preserve existing keys")
	assert.Equal(t, "0.5", got.Metadata["proration_addon_assoc_a1"])
	assert.Equal(t, types.EntitlementGrantStatusActive, got.GrantStatus,
		"top up must leave grant_status to the evaluator")
}

// The coefficient can produce a long fraction; numeric(25,15) must keep it.
func TestTopUpQuota_FractionalDelta(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)

	delta := decimal.RequireFromString("232.258064516129032")
	got, err := repo.TopUpQuota(ctx, g.ID, delta, time.Now().UTC(), nil)
	require.NoError(t, err)

	want := decimal.NewFromInt(1000).Add(delta)
	assert.True(t, got.Quota.Equal(want), "expected %s, got %s", want, got.Quota)
}

func TestTopUpQuota_AccumulatesAcrossAttaches(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)
	at := time.Now().UTC()

	_, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), at, types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)
	got, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(150), at, types.Metadata{"proration_addon_assoc_a2": "0.25"})
	require.NoError(t, err)

	assert.True(t, got.Quota.Equal(decimal.NewFromInt(1450)), "expected quota 1450, got %s", got.Quota)
	assert.Equal(t, "0.5", got.Metadata["proration_addon_assoc_a1"], "first marker must survive")
	assert.Equal(t, "0.25", got.Metadata["proration_addon_assoc_a2"])
}

// A row written before the metadata column existed carries SQL NULL; the
// COALESCE in the merge has to cope.
func TestTopUpQuota_NullMetadataMerges(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)

	client := newRealPostgresTestClient(t)
	_, err := client.Writer(ctx).ExecContext(ctx,
		`UPDATE entitlement_grants SET metadata = NULL WHERE id = $1`, g.ID)
	require.NoError(t, err)

	got, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), time.Now().UTC(),
		types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)
	assert.Equal(t, "0.5", got.Metadata["proration_addon_assoc_a1"])
	assert.True(t, got.Quota.Equal(decimal.NewFromInt(1300)))
}

func TestTopUpQuota_QuotaCrossedAt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	early := now.Add(-10 * time.Hour)
	late := now

	// Seed quota is 1000 and every case tops up by 300, so the new quota is 1300:
	// usage 1500 leaves the row exhausted, usage 1200 restores headroom.
	tests := []struct {
		name    string
		crossed *time.Time
		usage   int64
		at      time.Time
		wantNil bool
		want    time.Time
	}{
		{name: "unset stays unset", crossed: nil, at: late, wantNil: true},
		{name: "cleared when the top up restores headroom", crossed: &early, usage: 1200, at: late, wantNil: true},
		{name: "advances to the top up time when still over the new quota", crossed: &early, usage: 1500, at: late, want: late},
		{name: "never moves backwards", crossed: &late, usage: 1500, at: early, want: late},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newTestEntitlementGrantRepository(t)
			ctx := testGrantContext()
			g := createTestGrant(t, repo, ctx, func(g *domainGrant.EntitlementGrant) {
				g.QuotaCrossedAt = tt.crossed
				g.Usage = decimal.NewFromInt(tt.usage)
				if tt.crossed != nil {
					g.GrantStatus = types.EntitlementGrantStatusExhausted
				}
			})

			got, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), tt.at, nil)
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got.QuotaCrossedAt)
				return
			}
			require.NotNil(t, got.QuotaCrossedAt)
			assert.True(t, got.QuotaCrossedAt.UTC().Equal(tt.want),
				"expected quota_crossed_at %s, got %s", tt.want, got.QuotaCrossedAt.UTC())
		})
	}
}

// The UPDATE is tenant/environment scoped; a top up aimed at another tenant's
// row must find nothing rather than silently succeed.
func TestTopUpQuota_IsTenantScoped(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)

	otherTenant := types.SetTenantID(ctx, "tenant_someone_else")
	_, err := repo.TopUpQuota(otherTenant, g.ID, decimal.NewFromInt(300), time.Now().UTC(), nil)
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err), "expected not-found, got %v", err)

	unchanged, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, unchanged.Quota.Equal(decimal.NewFromInt(1000)),
		"cross-tenant top up leaked into the row: %s", unchanged.Quota)
}

func TestTopUpQuota_MissingGrant(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	_, err := repo.TopUpQuota(testGrantContext(), "eg_does_not_exist",
		decimal.NewFromInt(1), time.Now().UTC(), nil)
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err), "expected not-found, got %v", err)
}

// The whole point of doing the increment in SQL: an evaluator snapshot written
// from a pre-top-up read must not roll the quota back.
func TestTopUpQuota_SurvivesConcurrentSnapshot(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)

	// The evaluator's in-flight copy, read before the top up.
	stale := domainGrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(42)).
		Build()

	_, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), time.Now().UTC(), nil)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateSnapshot(ctx, stale))

	after, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.Quota.Equal(decimal.NewFromInt(1300)),
		"snapshot write clobbered the topped-up quota: %s", after.Quota)
	assert.True(t, after.Usage.Equal(decimal.NewFromInt(42)))
}

// Phase 3 derives exhaustion each tick, so the snapshot must be able to CLEAR
// quota_crossed_at. SetNillable silently skips a nil, which would strand a
// stale overage window on a grant that is back under quota.
func TestUpdateSnapshot_ClearsQuotaCrossedAt(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()

	crossed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	g := createTestGrant(t, repo, ctx, func(g *domainGrant.EntitlementGrant) {
		g.QuotaCrossedAt = &crossed
		g.GrantStatus = types.EntitlementGrantStatusExhausted
		g.Usage = decimal.NewFromInt(1500)
	})
	require.NotNil(t, g.QuotaCrossedAt)

	cleared := domainGrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(900)).
		WithGrantStatus(types.EntitlementGrantStatusActive).
		WithQuotaCrossedAt(nil).
		Build()
	require.NoError(t, repo.UpdateSnapshot(ctx, cleared))

	after, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.Nil(t, after.QuotaCrossedAt, "snapshot must clear quota_crossed_at, not skip the nil")
	assert.Equal(t, types.EntitlementGrantStatusActive, after.GrantStatus)
	assert.True(t, after.Usage.Equal(decimal.NewFromInt(900)))
}

func TestUpdateSnapshot_SetsQuotaCrossedAt(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)
	require.Nil(t, g.QuotaCrossedAt)

	at := time.Now().UTC().Truncate(time.Microsecond)
	crossed := domainGrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(1500)).
		WithGrantStatus(types.EntitlementGrantStatusExhausted).
		WithQuotaCrossedAt(&at).
		Build()
	require.NoError(t, repo.UpdateSnapshot(ctx, crossed))

	after, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, after.QuotaCrossedAt)
	assert.True(t, after.QuotaCrossedAt.UTC().Equal(at))
	assert.Equal(t, types.EntitlementGrantStatusExhausted, after.GrantStatus)
}

// The snapshot writes usage/status/crossing only — a tick must not roll back a
// quota top-up or wipe its metadata.
func TestUpdateSnapshot_LeavesQuotaAndMetadataAlone(t *testing.T) {
	repo := newTestEntitlementGrantRepository(t)
	ctx := testGrantContext()
	g := createTestGrant(t, repo, ctx, nil)

	_, err := repo.TopUpQuota(ctx, g.ID, decimal.NewFromInt(300), time.Now().UTC(),
		types.Metadata{"proration_addon_assoc_a1": "0.5"})
	require.NoError(t, err)

	require.NoError(t, repo.UpdateSnapshot(ctx, domainGrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(900)).
		WithGrantStatus(types.EntitlementGrantStatusActive).
		Build()))

	after, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.Quota.Equal(decimal.NewFromInt(1300)), "quota rolled back to %s", after.Quota)
	assert.Equal(t, "0.5", after.Metadata["proration_addon_assoc_a1"])
}
