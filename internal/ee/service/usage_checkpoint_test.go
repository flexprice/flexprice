package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var checkpointPeriodStart = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func checkpointHour(h int) time.Time { return checkpointPeriodStart.Add(time.Duration(h) * time.Hour) }

// countingMeterUsageStore counts GetMeterTotals calls to prove customers sharing a window share a query.
type countingMeterUsageStore struct {
	*testutil.InMemoryMeterUsageStore
	totalsCalls int
	finalCalls  int
}

func (s *countingMeterUsageStore) GetMeterTotals(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterTotals, error) {
	s.totalsCalls++
	if params.UseFinal {
		s.finalCalls++
	}
	return s.InMemoryMeterUsageStore.GetMeterTotals(ctx, params)
}

type checkpointHarness struct {
	svc             *usageCheckpointService
	usage           *countingMeterUsageStore
	checkpoints     *testutil.InMemoryMeterUsageCheckpointStore
	aggregatedUsage *fakeHourlyUsageRepository
}

// newCheckpointHarness seeds customers cust_1 and cust_2 (internal cus_1 and cus_2), each with one
// subscription from November 1 to periodEnd, and four meters: SUM, COUNT and MAX (checkpointed) and
// COUNT_UNIQUE (not).
func newCheckpointHarness(t *testing.T, periodEnd time.Time) *checkpointHarness {
	t.Helper()
	ctx := aggregatedUsageCtx()
	customers := testutil.NewInMemoryCustomerStore()
	subs := testutil.NewInMemorySubscriptionStore()
	for _, n := range []string{"1", "2"} {
		require.NoError(t, customers.Create(ctx, &customer.Customer{
			ID: "cus_" + n, ExternalID: "cust_" + n, EnvironmentID: "env_1",
			BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
		}))
		require.NoError(t, subs.Create(ctx, &subscription.Subscription{
			ID: "sub_" + n, CustomerID: "cus_" + n, EnvironmentID: "env_1",
			SubscriptionStatus: types.SubscriptionStatusActive,
			CurrentPeriodStart: checkpointPeriodStart, CurrentPeriodEnd: periodEnd,
			BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
		}))
	}
	meters := testutil.NewInMemoryMeterStore()
	for id, aggregation := range map[string]types.AggregationType{
		"m_sum": types.AggregationSum, "m_count": types.AggregationCount,
		"m_max": types.AggregationMax, "m_unique": types.AggregationCountUnique,
	} {
		require.NoError(t, meters.CreateMeter(ctx, &meter.Meter{
			ID: id, EventName: "api_call", Aggregation: meter.Aggregation{Type: aggregation},
			BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
		}))
	}

	h := &checkpointHarness{
		usage:           &countingMeterUsageStore{InMemoryMeterUsageStore: testutil.NewInMemoryMeterUsageStore()},
		checkpoints:     testutil.NewInMemoryMeterUsageCheckpointStore(),
		aggregatedUsage: &fakeHourlyUsageRepository{active: map[time.Time][]string{}},
	}
	h.svc = &usageCheckpointService{ServiceParams: ServiceParams{
		Logger:                   logger.NewNoopLogger(),
		Config:                   &config.Configuration{AggregatedUsage: config.AggregatedUsageConfig{Enabled: true}},
		CustomerRepo:             customers,
		SubRepo:                  subs,
		MeterRepo:                meters,
		MeterUsageRepo:           h.usage,
		MeterUsageCheckpointRepo: h.checkpoints,
		HourlyUsageRepo:          h.aggregatedUsage,
	}}
	return h
}

// ingest adds one usage row for every meter at ts and marks the customer active in that hour.
func (h *checkpointHarness) ingest(t *testing.T, externalCustomerID, id string, ts time.Time, qty int64) {
	t.Helper()
	var records []*events.MeterUsage
	for _, meterID := range []string{"m_sum", "m_count", "m_max", "m_unique"} {
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: "tenant_on", EnvironmentID: "env_1", ExternalCustomerID: externalCustomerID, Timestamp: ts,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromInt(qty),
		})
	}
	require.NoError(t, h.usage.BulkInsertMeterUsage(aggregatedUsageCtx(), records))
	hour := ts.UTC().Truncate(time.Hour)
	h.aggregatedUsage.active[hour] = append(h.aggregatedUsage.active[hour], externalCustomerID)
}

// totals returns a customer's per-period totals by meter and requires they share wantUntil.
func (h *checkpointHarness) totals(t *testing.T, customerID string, wantUntil time.Time) map[string]string {
	t.Helper()
	rows, err := h.checkpoints.ListPeriodTotals(aggregatedUsageCtx(), customerID, checkpointPeriodStart)
	require.NoError(t, err)
	got := make(map[string]string, len(rows))
	for _, r := range rows {
		require.True(t, r.AggregatedUntil.Equal(wantUntil), "meter %s at %s, want %s", r.MeterID, r.AggregatedUntil, wantUntil)
		got[r.MeterID] = r.Total.String()
	}
	return got
}

func TestUsageCheckpointEnvironment(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(5).Add(10*time.Minute), 4)
	h.ingest(t, "cust_1", "e2", checkpointHour(6).Add(20*time.Minute), 9)
	h.ingest(t, "cust_1", "e3", checkpointHour(7).Add(30*time.Minute), 2) // after the first cutoff

	result, err := h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, result.Customers)
	require.Equal(t, 1, result.Outcomes["updated"])
	require.Equal(t, map[string]string{"m_sum": "13", "m_count": "2", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(7)),
		"first run counts from the period start; COUNT_UNIQUE is not checkpointed")

	result, err = h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["updated"])
	require.Equal(t, map[string]string{"m_sum": "15", "m_count": "3", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(8)),
		"second run adds only [07:00, 08:00); MAX keeps the higher stored value")

	result, err = h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["current"], "a re-run at the same cutoff changes nothing")
	require.Equal(t, map[string]string{"m_sum": "15", "m_count": "3", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(8)))
}

func TestUsageCheckpointReadsSharedWindowOnce(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(5), 4)
	h.ingest(t, "cust_2", "e2", checkpointHour(6), 7)

	_, err := h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, h.usage.totalsCalls, "both customers start from the period start, so one query covers them")
	require.Equal(t, 1, h.usage.finalCalls, "stored totals read with FINAL")
	require.Equal(t, map[string]string{"m_sum": "4", "m_count": "1", "m_max": "4"}, h.totals(t, "cus_1", checkpointHour(7)))
	require.Equal(t, map[string]string{"m_sum": "7", "m_count": "1", "m_max": "7"}, h.totals(t, "cus_2", checkpointHour(7)))
}

func TestUsageCheckpointGroupsCustomersByWindow(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(5), 4)
	_, err := h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)

	h.usage.totalsCalls = 0
	h.ingest(t, "cust_1", "e2", checkpointHour(7), 1)
	h.ingest(t, "cust_2", "e3", checkpointHour(7), 3)
	_, err = h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 2, h.usage.totalsCalls, "cust_1 adds [07:00, 08:00) while new cust_2 reads from the period start")
	require.Equal(t, map[string]string{"m_sum": "5", "m_count": "2", "m_max": "4"}, h.totals(t, "cus_1", checkpointHour(8)))
	require.Equal(t, map[string]string{"m_sum": "3", "m_count": "1", "m_max": "3"}, h.totals(t, "cus_2", checkpointHour(8)))
}

func TestUsageCheckpointClampsToPeriodEnd(t *testing.T) {
	periodEnd := checkpointHour(6)
	h := newCheckpointHarness(t, periodEnd)
	h.ingest(t, "cust_1", "e1", checkpointHour(5), 4)
	h.ingest(t, "cust_1", "e2", checkpointHour(6).Add(time.Minute), 100) // next period's usage

	_, err := h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"m_sum": "4", "m_count": "1", "m_max": "4"}, h.totals(t, "cus_1", periodEnd))
}

func TestUsageCheckpointUnknownCustomer(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.aggregatedUsage.active[checkpointHour(5)] = []string{"cust_missing"}

	result, err := h.svc.CheckpointEnvironment(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["unknown_customer"])
}

func TestMergePeriodTotals(t *testing.T) {
	stored := []*meterusagecheckpoint.MeterUsageCheckpoint{
		{ID: "r_sum", MeterID: "m_sum", AggregationType: types.AggregationSum, Total: decimal.NewFromInt(5), AggregatedUntil: checkpointHour(3)},
		{ID: "r_idle", MeterID: "m_idle", AggregationType: types.AggregationMax, Total: decimal.NewFromInt(9), AggregatedUntil: checkpointHour(3)},
	}
	window := []*events.MeterTotals{
		{MeterID: "m_sum", Sum: decimal.NewFromInt(2), Count: 1, Max: decimal.NewFromInt(2)},
		{MeterID: "m_new", Sum: decimal.NewFromInt(-3), Count: 1, Max: decimal.NewFromInt(-3)},
		{MeterID: "m_unique", Sum: decimal.NewFromInt(50), Count: 1, Max: decimal.NewFromInt(50)},
	}
	meters := map[string]*meter.Meter{
		"m_sum": {ID: "m_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}},
		"m_new": {ID: "m_new", Aggregation: meter.Aggregation{Type: types.AggregationMax}},
	}

	next, err := mergePeriodTotals(aggregatedUsageCtx(), stored, window, meters, checkpointHour(4))
	require.NoError(t, err)
	byMeter := map[string]*meterusagecheckpoint.MeterUsageCheckpoint{}
	for _, row := range next {
		require.True(t, row.AggregatedUntil.Equal(checkpointHour(4)), "every row of the period moves together")
		byMeter[row.MeterID] = row
	}
	require.Len(t, byMeter, 3, "usage of a meter missing from meters starts no row")
	require.Equal(t, "7", byMeter["m_sum"].Total.String())
	require.Equal(t, "9", byMeter["m_idle"].Total.String(), "a row without usage keeps its total")
	require.Equal(t, "-3", byMeter["m_new"].Total.String(), "a new MAX row is not clamped to zero")
}

func TestMergePeriodTotalsRejectsChangedAggregation(t *testing.T) {
	stored := []*meterusagecheckpoint.MeterUsageCheckpoint{
		{ID: "r1", MeterID: "m1", AggregationType: types.AggregationSum, Total: decimal.NewFromInt(5), AggregatedUntil: checkpointHour(3)},
	}
	window := []*events.MeterTotals{{MeterID: "m1", Sum: decimal.NewFromInt(1), Count: 1, Max: decimal.NewFromInt(1)}}
	meters := map[string]*meter.Meter{"m1": {ID: "m1", Aggregation: meter.Aggregation{Type: types.AggregationMax}}}
	_, err := mergePeriodTotals(aggregatedUsageCtx(), stored, window, meters, checkpointHour(4))
	require.ErrorContains(t, err, "aggregation changed")
}

func TestCurrentPeriods(t *testing.T) {
	nov, dec := checkpointPeriodStart, checkpointPeriodStart.AddDate(0, 1, 0)
	sub := func(start, end time.Time) *subscription.Subscription {
		return &subscription.Subscription{CurrentPeriodStart: start, CurrentPeriodEnd: end}
	}
	tests := []struct {
		name string
		subs []*subscription.Subscription
		want []billingPeriod
	}{
		{"subscriptions sharing a start run to the latest end", []*subscription.Subscription{sub(nov, dec), sub(nov, nov.AddDate(1, 0, 0))}, []billingPeriod{{nov, nov.AddDate(1, 0, 0)}}},
		{"different starts are separate periods", []*subscription.Subscription{sub(nov, dec), sub(nov.AddDate(0, 0, -15), dec.AddDate(0, 0, -15))}, []billingPeriod{{nov, dec}, {nov.AddDate(0, 0, -15), dec.AddDate(0, 0, -15)}}},
		{"a period starting at or after the cutoff is skipped", []*subscription.Subscription{sub(checkpointHour(7), dec)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := currentPeriods(tt.subs, checkpointHour(7))
			require.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestUsageCheckpointWithoutStore(t *testing.T) {
	svc := &usageCheckpointService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}}}
	_, err := svc.CheckpointEnvironment(context.Background(), checkpointHour(7))
	require.ErrorContains(t, err, "aggregated usage store is not configured")
}
