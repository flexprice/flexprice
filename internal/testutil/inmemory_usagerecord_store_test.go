package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInMemoryUsageRecordStore(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	store := NewInMemoryUsageRecordStore()
	periodStart := time.Now().UTC().Add(-10 * time.Hour)
	periodEnd := time.Now().UTC().Add(-4 * time.Hour)

	rec := &usagerecord.UsageRecord{
		ID:             "ur_1",
		CustomerID:     "cust_1",
		SubscriptionID: "sub_1",
		PlanID:         "plan_1",
		Amount:         decimal.NewFromInt(10),
		Currency:       "usd",
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Synced:         false,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	// Create + duplicate-period guard
	require.NoError(t, store.Create(ctx, rec))
	dup := *rec
	dup.ID = "ur_2"
	require.Error(t, store.Create(ctx, &dup), "same subscription+period should be rejected")

	// ExistsForPeriod
	exists, err := store.ExistsForPeriod(ctx, "sub_1", periodStart, periodEnd)
	require.NoError(t, err)
	require.True(t, exists)

	notExists, err := store.ExistsForPeriod(ctx, "sub_1", periodStart, periodEnd.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, notExists)

	// ListUnsynced — record has no syncs entries yet, so it's provider-agnostic and unsynced
	unsynced, err := store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 1)
	require.Equal(t, "ur_1", unsynced[0].ID)

	// Reported to one of two marketplaces the subscription has an agreement on — synced stays false,
	// so the record is still in the unsynced list
	awsKey := string(types.SecretProviderAWSMarketplace)
	gcpKey := string(types.SecretProviderGCPMarketplace)
	err = store.MarkSynced(ctx, "ur_1", map[string]types.UsageRecordSyncEntry{
		awsKey: {AgreementID: "arn:aws:license-manager::l-abc", ReportingID: "aws-report-1", SyncedAt: time.Now().UTC(), ConnectionID: "conn_aws"},
	}, false)
	require.NoError(t, err)

	unsynced, err = store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 1, "a second marketplace is still outstanding, so still unsynced")
	require.Contains(t, unsynced[0].Syncs, awsKey)

	// Reported to the second marketplace too — now fully synced, drops out of the unsynced list
	err = store.MarkSynced(ctx, "ur_1", map[string]types.UsageRecordSyncEntry{
		awsKey: {AgreementID: "arn:aws:license-manager::l-abc", ReportingID: "aws-report-1", SyncedAt: time.Now().UTC(), ConnectionID: "conn_aws"},
		gcpKey: {AgreementID: "gcp-usage-reporting-id", ReportingID: "gcp-report-1", SyncedAt: time.Now().UTC(), ConnectionID: "conn_gcp"},
	}, true)
	require.NoError(t, err)

	unsynced, err = store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 0, "record should no longer be unsynced")

	stored, err := store.store.Get(ctx, "ur_1")
	require.NoError(t, err)
	require.Contains(t, stored.Syncs, awsKey)
	require.Contains(t, stored.Syncs, gcpKey)

	store.Clear()
	unsynced, err = store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 0)
}

// usageRecordAt builds a published record covering [end-1h, end) for the given subscription.
func usageRecordAt(ctx context.Context, id, subscriptionID string, end time.Time) *usagerecord.UsageRecord {
	return &usagerecord.UsageRecord{
		ID:             id,
		CustomerID:     "cust_1",
		SubscriptionID: subscriptionID,
		PlanID:         "plan_1",
		Amount:         decimal.NewFromInt(10),
		Currency:       "usd",
		PeriodStart:    end.Add(-time.Hour),
		PeriodEnd:      end,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
}

func periodEndCondition(op types.FilterOperatorType, at time.Time) *types.FilterCondition {
	return &types.FilterCondition{
		Field:    lo.ToPtr("period_end"),
		Operator: lo.ToPtr(op),
		DataType: lo.ToPtr(types.DataTypeDate),
		Value:    &types.Value{Date: lo.ToPtr(at)},
	}
}

// Covers the List query shapes the cancellation flush depends on: the exact-window lookup for an
// existing final record, and the period_end bounds used for the backlog and the last computed point.
func TestInMemoryUsageRecordStore_ListFilters(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	store := NewInMemoryUsageRecordStore()
	now := time.Now().UTC().Truncate(time.Second)

	ends := []time.Time{
		now.Add(-5 * time.Hour),
		now.Add(-4 * time.Hour),
		now.Add(-3 * time.Hour),
		now.Add(-2 * time.Hour),
		now.Add(-1 * time.Hour),
	}
	ids := []string{"ur_5h", "ur_4h", "ur_3h", "ur_2h", "ur_1h"}
	for i, end := range ends {
		require.NoError(t, store.Create(ctx, usageRecordAt(ctx, ids[i], "sub_1", end)))
	}
	// A second subscription must never leak into a subscription-scoped query.
	require.NoError(t, store.Create(ctx, usageRecordAt(ctx, "ur_other", "sub_2", ends[0])))

	t.Run("exact window identifies a single row", func(t *testing.T) {
		got, err := store.List(ctx, &types.UsageRecordFilter{
			QueryFilter:    types.NewNoLimitQueryFilter(),
			SubscriptionID: "sub_1",
			PeriodStart:    lo.ToPtr(ends[2].Add(-time.Hour)),
			PeriodEnd:      &ends[2],
		})
		require.NoError(t, err)
		require.Len(t, got, 1, "the unique key over the window admits at most one row")
		require.Equal(t, "ur_3h", got[0].ID)
	})

	t.Run("exact window returns nothing when the row is absent", func(t *testing.T) {
		unwritten := now.Add(30 * time.Minute)
		got, err := store.List(ctx, &types.UsageRecordFilter{
			QueryFilter:    types.NewNoLimitQueryFilter(),
			SubscriptionID: "sub_1",
			PeriodStart:    lo.ToPtr(unwritten.Add(-time.Hour)),
			PeriodEnd:      &unwritten,
		})
		require.NoError(t, err)
		require.Empty(t, got, "an empty result is how the flush decides to compute the record")
	})

	t.Run("period_end bounds, sorted ascending", func(t *testing.T) {
		got, err := store.List(ctx, &types.UsageRecordFilter{
			// No Limit, exactly as the flush's backlog query builds it: an unset limit is unbounded.
			QueryFilter: &types.QueryFilter{
				Sort:  lo.ToPtr("period_end"),
				Order: lo.ToPtr(types.OrderAsc),
			},
			SubscriptionID: "sub_1",
			Filters: []*types.FilterCondition{
				periodEndCondition(types.GREATER_THAN_EQUAL, ends[1]),
				periodEndCondition(types.LESS_THAN, ends[4]),
			},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"ur_4h", "ur_3h", "ur_2h"}, recordIDs(got),
			"lower bound inclusive, upper bound exclusive")
	})

	t.Run("last computed point excludes the boundary row", func(t *testing.T) {
		got, err := store.List(ctx, &types.UsageRecordFilter{
			QueryFilter: &types.QueryFilter{
				Sort:  lo.ToPtr("period_end"),
				Order: lo.ToPtr(types.OrderDesc),
				Limit: lo.ToPtr(1),
			},
			SubscriptionID: "sub_1",
			Filters:        []*types.FilterCondition{periodEndCondition(types.LESS_THAN, ends[4])},
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "ur_2h", got[0].ID, "newest row strictly before the cutoff")
	})

	t.Run("pagination applies offset as well as limit", func(t *testing.T) {
		got, err := store.List(ctx, &types.UsageRecordFilter{
			QueryFilter: &types.QueryFilter{
				Sort:   lo.ToPtr("period_end"),
				Order:  lo.ToPtr(types.OrderAsc),
				Limit:  lo.ToPtr(2),
				Offset: lo.ToPtr(2),
			},
			SubscriptionID: "sub_1",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"ur_3h", "ur_2h"}, recordIDs(got),
			"a non-zero offset must not silently return the first page")
	})
}

// Count backs the API's pagination total, which must reflect every matching row — not just the
// current page — so a caller paging with a small limit can still compute the right number of pages.
func TestInMemoryUsageRecordStore_Count(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	store := NewInMemoryUsageRecordStore()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, store.Create(ctx, usageRecordAt(ctx, "ur_count_1", "sub_1", now.Add(-3*time.Hour))))
	require.NoError(t, store.Create(ctx, usageRecordAt(ctx, "ur_count_2", "sub_1", now.Add(-2*time.Hour))))
	require.NoError(t, store.Create(ctx, usageRecordAt(ctx, "ur_count_3", "sub_2", now.Add(-1*time.Hour))))

	t.Run("counts every matching row, ignoring the filter's own limit", func(t *testing.T) {
		count, err := store.Count(ctx, &types.UsageRecordFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(1)}})
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("applies the same named filters as List", func(t *testing.T) {
		count, err := store.Count(ctx, &types.UsageRecordFilter{SubscriptionID: "sub_1"})
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("Count and a limited List agree on the true total", func(t *testing.T) {
		filter := &types.UsageRecordFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(1), Offset: lo.ToPtr(0)}}

		page, err := store.List(ctx, filter)
		require.NoError(t, err)
		require.Len(t, page, 1, "the page itself is limited")

		count, err := store.Count(ctx, filter)
		require.NoError(t, err)
		require.Equal(t, 3, count, "Count must report the total independent of the page's own limit")
	})
}

// The invariant the cancellation flush rests on: every attempt computes the same window, so a retry
// collides with the unique key and reuses the existing row instead of writing a second one. If the
// window could shift between attempts the key would differ and nothing would catch the duplicate.
func TestUsageRecordFlushRetryReusesTheSameRecord(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	store := NewInMemoryUsageRecordStore()
	cancelAt := time.Now().UTC().Truncate(time.Second)
	snapshotEnd := cancelAt.Add(-4 * time.Hour)
	require.NoError(t, store.Create(ctx, usageRecordAt(ctx, "ur_snapshot", "sub_1", snapshotEnd)))

	// lastComputedPeriodEnd: newest period_end strictly before cancelAt, so the final record can
	// never move the start of its own window.
	windowStart := func() time.Time {
		rows, err := store.List(ctx, &types.UsageRecordFilter{
			QueryFilter: &types.QueryFilter{
				Sort: lo.ToPtr("period_end"), Order: lo.ToPtr(types.OrderDesc), Limit: lo.ToPtr(1),
			},
			SubscriptionID: "sub_1",
			Filters:        []*types.FilterCondition{periodEndCondition(types.LESS_THAN, cancelAt)},
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0].PeriodEnd
	}

	// Attempt 1: nothing covers the window yet, so the record is computed and written.
	first := windowStart()
	require.Equal(t, snapshotEnd, first)
	final := &usagerecord.UsageRecord{
		ID: "ur_final", CustomerID: "cust_1", SubscriptionID: "sub_1", PlanID: "plan_1",
		Amount: decimal.NewFromInt(42), Currency: "usd",
		PeriodStart: first, PeriodEnd: cancelAt, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, store.Create(ctx, final))

	// Attempt 2: the window is unchanged, the lookup finds the row, and no second one is written.
	require.Equal(t, first, windowStart(), "the final record must not move the window it starts from")

	existing, err := store.List(ctx, &types.UsageRecordFilter{
		QueryFilter:    types.NewNoLimitQueryFilter(),
		SubscriptionID: "sub_1",
		PeriodStart:    &first,
		PeriodEnd:      &cancelAt,
	})
	require.NoError(t, err)
	require.Len(t, existing, 1)
	require.Equal(t, "ur_final", existing[0].ID, "the record keeps one identity across attempts")

	duplicate := *final
	duplicate.ID = "ur_final_retry"
	require.Error(t, store.Create(ctx, &duplicate), "the unique key must reject a second final record")

	all, err := store.List(ctx, &types.UsageRecordFilter{
		QueryFilter: types.NewNoLimitQueryFilter(), SubscriptionID: "sub_1",
	})
	require.NoError(t, err)
	require.Len(t, all, 2, "one snapshot row and one final record")

	// The stored window ends at the true cancellation instant; the reporting margin is applied only
	// to the value sent to a provider.
	require.Equal(t, cancelAt, existing[0].PeriodEnd)
}

func recordIDs(records []*usagerecord.UsageRecord) []string {
	ids := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.ID
	}
	return ids
}

// The sync map is keyed by marketplace, not by the connection that reported it, so replacing a
// connection must not make an already reported record look unreported. Keyed by connection id this
// would report the same usage to the same marketplace twice.
func TestUsageRecordSyncSurvivesConnectionReplacement(t *testing.T) {
	awsKey := string(types.SecretProviderAWSMarketplace)

	entry := types.UsageRecordSyncEntry{
		AgreementID:  "arn:aws:license-manager::l-abc",
		ReportingID:  "aws-report-1",
		SyncedAt:     time.Now().UTC(),
		ConnectionID: "conn_old",
	}
	syncs := map[string]types.UsageRecordSyncEntry{awsKey: entry}

	// The tenant deletes conn_old and connects conn_new for the same marketplace. The agreement is
	// unchanged, so the record must still read as reported.
	got, ok := syncs[awsKey]
	require.True(t, ok, "the entry is found by marketplace regardless of which connection wrote it")
	require.True(t, got.IsSynced())
	require.Equal(t, "conn_old", got.ConnectionID, "the reporting connection stays recorded for tracing")
}

// A skip resolves the marketplace so it is not retried, but it is not a delivery: nothing was sent,
// so it must not be counted as synced and must carry no SyncedAt.
func TestUsageRecordSyncEntryIsSynced(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name  string
		entry types.UsageRecordSyncEntry
		want  bool
	}{
		{"accepted", types.UsageRecordSyncEntry{AgreementID: "a", ReportingID: "r", SyncedAt: now}, true},
		{"skipped", types.UsageRecordSyncEntry{AgreementID: "a", Skipped: true, SkipReason: types.SkipReasonZeroAmountNotSupported}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.entry.IsSynced())
		})
	}
}

// A skip records no SyncedAt, since synced_at means the marketplace accepted the usage and a skip
// never sent it.
func TestUsageRecordSkipCarriesNoSyncedAt(t *testing.T) {
	skip := types.UsageRecordSyncEntry{
		AgreementID:  "resource-id",
		ConnectionID: "conn_azure",
		Skipped:      true,
		SkipReason:   types.SkipReasonZeroAmountNotSupported,
	}
	require.True(t, skip.SyncedAt.IsZero(), "a skip sent nothing, so it was never synced at any time")
	require.False(t, skip.IsSynced())
	require.Empty(t, skip.ReportingID, "a skip has no marketplace receipt")
}
