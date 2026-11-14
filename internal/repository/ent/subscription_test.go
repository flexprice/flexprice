package ent

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/migrate"
	"github.com/flexprice/flexprice/internal/cache"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// FLEXPRICE_TEST_POSTGRES_DSN must be a PostgreSQL URL; each run uses an isolated schema.
func newSubscriptionTestRepository(t *testing.T) (context.Context, postgres.IClient, *subscriptionRepository) {
	t.Helper()
	dsn := os.Getenv("FLEXPRICE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FLEXPRICE_TEST_POSTGRES_DSN to run PostgreSQL repository tests")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	schemaName := strings.ToLower(types.GenerateUUIDWithPrefix("trial_test"))
	_, err = db.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schemaName) + " CASCADE")
		require.NoError(t, err)
	})
	query := u.Query()
	query.Set("search_path", schemaName)
	u.RawQuery = query.Encode()
	client, err := ent.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "trial_test_tenant"), "trial_test_env")
	tables := make([]*schema.Table, 0, 2)
	for _, source := range []*schema.Table{migrate.SubscriptionsTable, migrate.CustomersTable} {
		table := schema.NewTable(source.Name)
		for _, column := range source.Columns {
			table.AddColumn(column)
		}
		table.PrimaryKey = source.PrimaryKey
		table.Indexes = source.Indexes
		table.Annotation = source.Annotation
		tables = append(tables, table)
	}
	require.NoError(t, migrate.Create(ctx, client.Schema, tables, migrate.WithForeignKeys(false)))
	log := logger.NewNoopLogger()
	database := postgres.NewClient(&postgres.EntClients{Writer: client, Reader: client}, log, nil)
	repo := NewSubscriptionRepository(database, log, newFakeRedisCache()).(*subscriptionRepository)
	return ctx, database, repo
}

func TestSubscriptionList_CustomerNameSort(t *testing.T) {
	ctx, database, repo := newSubscriptionTestRepository(t)
	client := database.Writer(ctx)
	otherTenant := types.SetTenantID(ctx, "other_tenant")
	otherEnvironment := types.SetEnvironmentID(ctx, "other_environment")
	for _, c := range []struct {
		id, name string
		ctx      context.Context
	}{
		{"alpha", "Alpha", ctx},
		{"alpha_upper", "ALPHA", ctx},
		{"beta", "beta", ctx},
		{"zulu", "Zulu", ctx},
		{"cross_tenant", "Aardvark", otherTenant},
		{"cross_env", "Aardvark", otherEnvironment},
	} {
		_, err := client.Customer.Create().SetID(c.id).SetExternalID(c.id).SetName(c.name).
			SetTenantID(types.GetTenantID(c.ctx)).SetEnvironmentID(types.GetEnvironmentID(c.ctx)).
			SetCreatedBy("test").SetUpdatedBy("test").Save(ctx)
		require.NoError(t, err)
	}
	for _, row := range []struct {
		id, customer string
		ctx          context.Context
	}{
		{"sub_a", "zulu", ctx},
		{"sub_b", "beta", ctx},
		{"sub_c", "alpha", ctx},
		{"sub_d", "alpha", ctx},
		{"sub_e", "alpha_upper", ctx},
		{"sub_missing", "missing", ctx},
		{"sub_cross_tenant", "cross_tenant", ctx},
		{"sub_cross_env", "cross_env", ctx},
		{"hidden_tenant", "zulu", otherTenant},
		{"hidden_env", "zulu", otherEnvironment},
	} {
		sub := subscriptionTestFixture(row.ctx, row.id, types.SubscriptionStatusActive, nil, nil)
		sub.CustomerID = row.customer
		require.NoError(t, repo.Create(row.ctx, sub))
	}
	for _, tc := range []struct {
		name      string
		direction types.SortDirection
		want      []string
	}{
		{"ascending", types.SortDirectionAsc, []string{"sub_cross_env", "sub_cross_tenant", "sub_missing", "sub_c", "sub_d", "sub_e", "sub_b", "sub_a"}},
		{"descending", types.SortDirectionDesc, []string{"sub_a", "sub_b", "sub_e", "sub_d", "sub_c", "sub_missing", "sub_cross_tenant", "sub_cross_env"}},
		{"default direction", "", []string{"sub_cross_env", "sub_cross_tenant", "sub_missing", "sub_c", "sub_d", "sub_e", "sub_b", "sub_a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idDirection := tc.direction
			if idDirection == "" {
				idDirection = types.SortDirectionAsc
			}
			filter := types.NewSubscriptionFilter()
			filter.Limit = lo.ToPtr(2)
			filter.Sort = []*types.SortCondition{
				{Field: "customer_name", Direction: tc.direction},
				{Field: "id", Direction: idDirection},
			}
			var ids []string
			for offset := 0; offset < len(tc.want); offset += 2 {
				filter.Offset = lo.ToPtr(offset)
				page, err := repo.List(ctx, filter)
				require.NoError(t, err)
				for _, sub := range page {
					ids = append(ids, sub.ID)
				}
			}
			require.Equal(t, tc.want, ids)
			count, err := repo.Count(ctx, filter)
			require.NoError(t, err)
			require.Equal(t, len(tc.want), count)
		})
	}
}

func TestSubscriptionUpdate_PersistsTrialBounds(t *testing.T) {
	ctx, database, repo := newSubscriptionTestRepository(t)
	start := time.Date(2026, 10, 15, 18, 41, 41, 5412000, time.UTC)

	for _, clear := range []bool{false, true} {
		name := "conversion"
		if clear {
			name = "clear bounds"
		}
		t.Run(name, func(t *testing.T) {
			original := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("subs"), types.SubscriptionStatusTrialing,
				lo.ToPtr(start.Add(-time.Hour)), lo.ToPtr(start.AddDate(0, 0, 30)))
			require.NoError(t, repo.Create(ctx, original))
			updated := subscriptionTestFixture(ctx, original.ID, types.SubscriptionStatusIncomplete, lo.ToPtr(start.Add(-2*time.Hour)), &start)
			if clear {
				updated = subscriptionTestFixture(ctx, original.ID, types.SubscriptionStatusIncomplete, nil, nil)
			}
			require.NoError(t, database.WithTx(ctx, func(txCtx context.Context) error {
				return repo.Update(txCtx, updated)
			}))
			require.NoError(t, database.WithTx(ctx, func(txCtx context.Context) error {
				stored, err := repo.GetForUpdate(txCtx, original.ID)
				if err != nil {
					return err
				}
				require.Equal(t, updated.TrialStart, stored.TrialStart)
				require.Equal(t, updated.TrialEnd, stored.TrialEnd)
				require.Equal(t, updated.SubscriptionStatus, stored.SubscriptionStatus)
				return nil
			}))
		})
	}
}

// Invalidation must wait for the commit; a delete before it lets a concurrent read re-cache the old row.
func TestSubscriptionRepository_InvalidateCacheAfterCommit(t *testing.T) {
	tests := []struct {
		name            string
		inTx            bool
		finish          func(context.Context)
		wantCachedAfter bool
	}{
		{name: "no transaction deletes immediately"},
		{name: "commit deletes after commit", inTx: true, finish: types.RunPostCommitHooks},
		{name: "rollback keeps the committed row cached", inTx: true, finish: types.DiscardPostCommitHooks, wantCachedAfter: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redis := newFakeRedisCache()
			repo := &subscriptionRepository{redisCache: redis}
			ctx := entitlementCacheTestContext()
			if tt.inTx {
				ctx = types.WithPostCommitHooks(ctx)
			}
			keys := []string{
				cache.GenerateKey(ctx, cache.PrefixSubscription, "subs-1"),
				cache.GenerateKey(ctx, cache.PrefixSubscription, "subs-2"),
			}
			for _, key := range keys {
				redis.Set(ctx, key, "cached", 0)
			}

			repo.invalidateCache(ctx, "subs-1", "subs-2")

			for _, key := range keys {
				_, cached := redis.Get(ctx, key)
				require.Equal(t, tt.inTx, cached, "cache state before the transaction ends")
			}
			if tt.finish != nil {
				tt.finish(ctx)
			}
			for _, key := range keys {
				_, cached := redis.Get(ctx, key)
				require.Equal(t, tt.wantCachedAfter, cached, "cache state after the transaction ends")
			}
		})
	}
}

func TestSubscriptionRepository_ConcurrentReadBeforeCommit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit evicts old row refilled by outside reader"
		if rollback {
			name = "rollback preserves committed state"
		}
		t.Run(name, func(t *testing.T) {
			ctx, database, repo := newSubscriptionTestRepository(t)
			sub := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("subs"), types.SubscriptionStatusIncomplete, nil, nil)
			require.NoError(t, repo.Create(ctx, sub))
			rollbackErr := errors.New("roll back activation")
			err := database.WithTx(ctx, func(txCtx context.Context) error {
				lockedSub, err := repo.GetForUpdate(txCtx, sub.ID)
				if err != nil {
					return err
				}
				lockedSub.SubscriptionStatus = types.SubscriptionStatusActive
				if err := repo.Update(txCtx, lockedSub); err != nil {
					return err
				}
				uncommitted, err := repo.Get(txCtx, sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusActive, uncommitted.SubscriptionStatus)
				require.Nil(t, repo.GetCache(ctx, sub.ID), "uncommitted state must not enter the shared cache")

				// A cache miss outside the open transaction reads the previously committed row.
				repo.DeleteCache(ctx, sub.ID)
				outside, err := repo.Get(ctx, sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusIncomplete, outside.SubscriptionStatus)
				require.Equal(t, types.SubscriptionStatusIncomplete, repo.GetCache(ctx, sub.ID).SubscriptionStatus)

				// Mutation paths explicitly lock instead of trusting that cached snapshot.
				current, err := repo.GetForUpdate(txCtx, sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusActive, current.SubscriptionStatus)
				if rollback {
					return rollbackErr
				}
				return nil
			})
			wantStatus := types.SubscriptionStatusActive
			if rollback {
				require.ErrorIs(t, err, rollbackErr)
				wantStatus = types.SubscriptionStatusIncomplete
			} else {
				require.NoError(t, err)
				require.Nil(t, repo.GetCache(ctx, sub.ID))
			}
			stored, err := repo.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Equal(t, wantStatus, stored.SubscriptionStatus)
		})
	}
}

func TestSubscriptionRepository_GetTimezone(t *testing.T) {
	ctx, database, repo := newSubscriptionTestRepository(t)
	sub := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("subs"), types.SubscriptionStatusIncomplete, nil, nil)
	sub.Timezone = "Asia/Kolkata"
	require.NoError(t, repo.Create(ctx, sub))
	timezone, err := repo.GetTimezone(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.Timezone, timezone)
	require.Nil(t, repo.GetCache(ctx, sub.ID), "timezone lookup must not fill the entity cache")

	cached := *sub
	cached.Timezone = "UTC"
	repo.SetCache(ctx, &cached)
	timezone, err = repo.GetTimezone(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.Timezone, timezone, "timezone lookup must not use the entity cache")
	require.Equal(t, "UTC", repo.GetCache(ctx, sub.ID).Timezone, "timezone lookup must not replace the entity cache")

	for _, otherCtx := range []context.Context{
		types.SetTenantID(ctx, "another_tenant"),
		types.SetEnvironmentID(ctx, "another_environment"),
	} {
		_, err := repo.GetTimezone(otherCtx, sub.ID)
		require.True(t, ierr.IsNotFound(err), "timezone lookup must remain tenant/environment scoped")
	}
	_, err = repo.GetTimezone(ctx, "missing")
	require.True(t, ierr.IsNotFound(err))
	require.NoError(t, database.WithTx(ctx, func(txCtx context.Context) error {
		return repo.Delete(txCtx, sub.ID)
	}))
	_, err = repo.GetTimezone(ctx, sub.ID)
	require.True(t, ierr.IsNotFound(err), "archived subscriptions must not be returned")
}

func TestSubscriptionRepository_SetCacheSkipsTransaction(t *testing.T) {
	ctx, database, repo := newSubscriptionTestRepository(t)
	sub := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("subs"), types.SubscriptionStatusActive, nil, nil)
	require.NoError(t, database.WithTx(ctx, func(txCtx context.Context) error {
		repo.SetCache(txCtx, sub)
		require.Nil(t, repo.GetCache(ctx, sub.ID))
		return nil
	}))
	require.Nil(t, repo.GetCache(ctx, sub.ID))
	repo.SetCache(ctx, sub)
	require.NotNil(t, repo.GetCache(ctx, sub.ID))
}

func subscriptionTestFixture(ctx context.Context, id string, status types.SubscriptionStatus, trialStart, trialEnd *time.Time) *domainSub.Subscription {
	start := time.Date(2026, 10, 15, 18, 41, 41, 5412000, time.UTC)
	return &domainSub.Subscription{
		ID: id, CustomerID: "customer", PlanID: "plan", Currency: "usd",
		SubscriptionStatus: status, SubscriptionType: types.SubscriptionTypeStandalone,
		StartDate: start.Add(-time.Hour), BillingAnchor: start,
		CurrentPeriodStart: start, CurrentPeriodEnd: start.AddDate(0, 1, 0),
		TrialStart: trialStart, TrialEnd: trialEnd,
		BillingCadence: types.BILLING_CADENCE_RECURRING,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingCycle: types.BillingCycleAnniversary, Timezone: "UTC",
		PaymentBehavior:  string(types.PaymentBehaviorDefaultActive),
		CollectionMethod: string(types.CollectionMethodChargeAutomatically),
		PauseStatus:      types.PauseStatusNone, ProrationBehavior: types.ProrationBehaviorNone,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
}
