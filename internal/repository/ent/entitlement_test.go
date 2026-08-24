package ent

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	domainEntitlement "github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func newTestEntitlementRepository(t *testing.T) domainEntitlement.Repository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	return NewEntitlementRepository(client, log, cache.NewInMemoryCache(), noopRedisCache{})
}

func newTestEntitlement(t *testing.T) *domainEntitlement.Entitlement {
	t.Helper()
	ctx := testCouponContext()
	e := &domainEntitlement.Entitlement{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		FeatureID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		FeatureType:      types.FeatureTypeStatic,
		StaticValue:      "2000",
		IsEnabled:        true,
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	e.Status = types.StatusPublished
	return e
}

// end_date is what stops a subscription-scoped override from stacking with the plan
// entitlement that replaced it, and the plan-change close is the only writer. Update
// silently ignored the field for a while, so the close reported a drop it never made
// and the customer kept both limits.
func TestEntitlementUpdate_PersistsTheClosingWindow(t *testing.T) {
	repo := newTestEntitlementRepository(t)
	ctx := testCouponContext()

	created, err := repo.Create(ctx, newTestEntitlement(t))
	require.NoError(t, err)
	require.Nil(t, created.EndDate, "a fresh entitlement has no end date")

	closedAt := time.Now().UTC().Truncate(time.Second)
	_, err = repo.Update(ctx, domainEntitlement.NewEntitlementBuilder(created).
		WithEndDate(closedAt).
		Build())
	require.NoError(t, err)

	reloaded, err := repo.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.EndDate, "the close must survive the write")
	require.True(t, closedAt.Equal(reloaded.EndDate.UTC()),
		"closed at %s but stored %s", closedAt, reloaded.EndDate.UTC())
}

// The field is Nillable rather than set-or-clear, so an update that carries no end
// date narrows nothing. Reopening a closed entitlement is a deliberate act and must
// not fall out of an unrelated field update.
func TestEntitlementUpdate_DoesNotReopenAClosedEntitlement(t *testing.T) {
	repo := newTestEntitlementRepository(t)
	ctx := testCouponContext()

	closedAt := time.Now().UTC().Truncate(time.Second)
	seed := newTestEntitlement(t)
	seed.EndDate = lo.ToPtr(closedAt)
	created, err := repo.Create(ctx, seed)
	require.NoError(t, err)
	require.NotNil(t, created.EndDate)

	stale := *created
	stale.EndDate = nil
	stale.StaticValue = "3000"
	_, err = repo.Update(ctx, &stale)
	require.NoError(t, err)

	reloaded, err := repo.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "3000", reloaded.StaticValue, "the update still applied")
	require.NotNil(t, reloaded.EndDate, "but it did not reopen the window")
	require.True(t, closedAt.Equal(reloaded.EndDate.UTC()))
}
