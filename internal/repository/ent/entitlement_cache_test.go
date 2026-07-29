package ent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	domainEntitlement "github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeRedisCache mimics redisCacheImpl's storage contract: Set JSON-marshals
// non-string values and Get hands back the marshalled string. Reproducing that
// here is the point of the test — an in-memory map holding the original pointers
// would pass even if a field failed to survive JSON.
type fakeRedisCache struct {
	noopRedisCache
	store map[string]string
}

func newFakeRedisCache() *fakeRedisCache {
	return &fakeRedisCache{store: map[string]string{}}
}

func (f *fakeRedisCache) IsEnabled() bool { return true }

func (f *fakeRedisCache) Set(_ context.Context, key string, value interface{}, _ time.Duration) {
	if s, ok := value.(string); ok {
		f.store[key] = s
		return
	}
	b, err := json.Marshal(value)
	if err != nil {
		return
	}
	f.store[key] = string(b)
}

func (f *fakeRedisCache) Get(_ context.Context, key string) (interface{}, bool) {
	v, ok := f.store[key]
	if !ok {
		return nil, false
	}
	return v, true
}

func (f *fakeRedisCache) Delete(_ context.Context, key string) {
	delete(f.store, key)
}

func newEntitlementCacheTestRepo() (*entitlementRepository, *fakeRedisCache) {
	redis := newFakeRedisCache()
	return &entitlementRepository{redisCache: redis}, redis
}

func entitlementCacheTestContext() context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant-1")
	return context.WithValue(ctx, types.CtxEnvironmentID, "env-1")
}

// The cached payload crosses a JSON boundary, so every field the billing paths
// read has to survive the round trip — a silently dropped usage limit or grant
// quota would under- or over-entitle a customer.
func TestEntitlementRepository_EntityCacheRoundTripsThroughJSON(t *testing.T) {
	repo, _ := newEntitlementCacheTestRepo()
	ctx := entitlementCacheTestContext()

	now := time.Now().UTC().Truncate(time.Second)
	quota := decimal.RequireFromString("250.5")
	want := []*domainEntitlement.Entitlement{
		{
			ID:                 "ent-1",
			EntityType:         types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			EntityID:           "plan-1",
			FeatureID:          "feat-1",
			FeatureType:        types.FeatureTypeMetered,
			IsEnabled:          true,
			UsageLimit:         lo.ToPtr(int64(1000)),
			UsageResetPeriod:   types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:        true,
			DisplayOrder:       3,
			ConfigValue:        map[string]interface{}{"tier": "gold"},
			StartDate:          lo.ToPtr(now),
			GrantMeasure:       types.EntitlementGrantMeasureQuantity,
			GrantDurationValue: lo.ToPtr(24),
			GrantDurationUnit:  types.EntitlementGrantDurationUnitHour,
			GrantQuota:         &quota,
			AggregationMode:    types.EntitlementAggregationModeAdditive,
			BaseModel: types.BaseModel{
				TenantID:  "tenant-1",
				Status:    types.StatusPublished,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			ID:          "ent-2",
			EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			EntityID:    "plan-1",
			FeatureID:   "feat-2",
			FeatureType: types.FeatureTypeStatic,
			StaticValue: "blue",
			BaseModel: types.BaseModel{
				TenantID: "tenant-1",
				Status:   types.StatusPublished,
			},
		},
	}

	repo.setEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1", want)

	got := repo.getEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1")
	require.Len(t, got, 2)
	require.Equal(t, want[0], got[0])
	require.Equal(t, want[1], got[1])
	require.NotNil(t, got[0].GrantQuota)
	require.True(t, quota.Equal(*got[0].GrantQuota))
}

func TestEntitlementRepository_EntityCacheIsInvalidated(t *testing.T) {
	repo, _ := newEntitlementCacheTestRepo()
	ctx := entitlementCacheTestContext()

	ents := []*domainEntitlement.Entitlement{{
		ID:         "ent-1",
		EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
	}}

	repo.setEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1", ents)
	require.Len(t, repo.getEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1"), 1)

	repo.invalidateEntityCaches(ctx, ents)
	require.Nil(t, repo.getEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1"))
}

// Tenant and environment are part of the key, so one tenant must never observe
// another's cached entitlements.
func TestEntitlementRepository_EntityCacheIsScopedByTenantAndEnvironment(t *testing.T) {
	repo, _ := newEntitlementCacheTestRepo()
	ctx := entitlementCacheTestContext()

	repo.setEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1",
		[]*domainEntitlement.Entitlement{{ID: "ent-1", EntityID: "plan-1"}})

	otherTenant := context.WithValue(context.Background(), types.CtxTenantID, "tenant-2")
	otherTenant = context.WithValue(otherTenant, types.CtxEnvironmentID, "env-1")
	require.Nil(t, repo.getEntityCache(otherTenant, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1"))

	otherEnv := context.WithValue(context.Background(), types.CtxTenantID, "tenant-1")
	otherEnv = context.WithValue(otherEnv, types.CtxEnvironmentID, "env-2")
	require.Nil(t, repo.getEntityCache(otherEnv, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1"))
}

// Only entity types in cachedEntityTypes may be cached: everything else has no
// key, so it can never be written and therefore never go stale.
func TestEntitlementRepository_UncacheableEntityTypesAreNotCached(t *testing.T) {
	repo, redis := newEntitlementCacheTestRepo()
	ctx := entitlementCacheTestContext()

	for _, entityType := range []types.EntitlementEntityType{
		types.ENTITLEMENT_ENTITY_TYPE_ADDON,
		types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
	} {
		repo.setEntityCache(ctx, entityType, "entity-1",
			[]*domainEntitlement.Entitlement{{ID: "ent-1", EntityType: entityType, EntityID: "entity-1"}})

		require.Empty(t, redis.store, "entity type %s must not be cached", entityType)
		require.Nil(t, repo.getEntityCache(ctx, entityType, "entity-1"))
	}
}

// A repository built without Redis (scripts, tests) must degrade to no caching
// rather than panicking.
func TestEntitlementRepository_EntityCacheNoopsWithoutRedis(t *testing.T) {
	repo := &entitlementRepository{}
	ctx := entitlementCacheTestContext()

	ents := []*domainEntitlement.Entitlement{{
		ID:         "ent-1",
		EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
	}}

	require.NotPanics(t, func() {
		repo.setEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1", ents)
		repo.invalidateEntityCaches(ctx, ents)
	})
	require.Nil(t, repo.getEntityCache(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, "plan-1"))
}

var _ cache.RedisCache = (*fakeRedisCache)(nil)
