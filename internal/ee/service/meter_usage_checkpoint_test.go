package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *MeterUsageServiceSuite) checkpointRequest(aggType types.AggregationType, until time.Time) CheckpointedUsageRequest {
	return CheckpointedUsageRequest{
		CustomerID:          s.customer.ID,
		ExternalCustomerIDs: []string{s.customer.ExternalID},
		MeterID:             s.meterAPI.ID,
		AggregationType:     aggType,
		Until:               until,
	}
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_RejectsUnsupportedType() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationMax, nov1))
	s.Error(err)
	_, err = s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationLatest, nov1), nil)
	s.Error(err)
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_Sum() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-48*time.Hour), 40)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(24*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(10*24*time.Hour), -4)

	total, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "no checkpoint aggregates all history, got %s", total)

	_, err = s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, oct1), nil)
	s.Require().NoError(err)

	total, err = s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "checkpoint plus changes since, got %s", total)

}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_SumGoesBelowZero() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-48*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-24*time.Hour), -15)

	total, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(-5).Equal(total), "stored totals stay exact, got %s", total)
}

func (s *MeterUsageServiceSuite) TestSaveUsageCheckpoint_RecomputeReplaces() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-time.Hour), 40)
	_, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, oct1), nil)
	s.Require().NoError(err)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(5*24*time.Hour), 6)
	first, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, nov1), nil)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(first.Total), "got %s", first.Total)

	// A change inside the period lands before the invoice is recomputed.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(20*24*time.Hour), -10)
	second, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, nov1), nil)
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "the same instant keeps one checkpoint")
	s.True(decimal.NewFromInt(36).Equal(second.Total), "recompute adds the period once, got %s", second.Total)
}

func (s *MeterUsageServiceSuite) TestCheckpointCutoffCountsBoundaryEventOnce() {
	ctx := s.GetContext()
	cutoff := s.periodStart
	end := cutoff.Add(time.Hour)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, cutoff.Add(-time.Minute), 40)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, cutoff, -10)
	checkpoint, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, cutoff), nil)
	s.Require().NoError(err)
	s.True(checkpoint.Total.Equal(decimal.NewFromInt(40)))
	got, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, end))
	s.Require().NoError(err)
	s.True(got.Equal(decimal.NewFromInt(30)), "boundary event belongs to the tail, got %s", got)
	atCutoff, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, cutoff))
	s.Require().NoError(err)
	s.True(atCutoff.Equal(decimal.NewFromInt(40)), "the event at cutoff is excluded")
}

func (s *MeterUsageServiceSuite) TestCheckpointNegativePrefixSurvivesLaterIncrease() {
	ctx := s.GetContext()
	cutoff := s.periodStart
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, cutoff.Add(-time.Hour), -20)
	_, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSum, cutoff), nil)
	s.Require().NoError(err)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, cutoff.Add(time.Minute), 30)
	got, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, cutoff.Add(time.Hour)))
	s.Require().NoError(err)
	s.True(got.Equal(decimal.NewFromInt(10)), "negative prefix must not be floored, got %s", got)
}

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
// subscription from November 1 to periodEnd, and five meters: SUM, COUNT and MAX (checkpointed),
// COUNT_UNIQUE and a never-reset SUM (not).
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
	require.NoError(t, meters.CreateMeter(ctx, &meter.Meter{
		ID: "m_never", EventName: "api_call", Aggregation: meter.Aggregation{Type: types.AggregationSum},
		ResetUsage: types.ResetUsageNever,
		BaseModel:  types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
	}))

	h := &checkpointHarness{
		usage:           &countingMeterUsageStore{InMemoryMeterUsageStore: testutil.NewInMemoryMeterUsageStore()},
		checkpoints:     testutil.NewInMemoryMeterUsageCheckpointStore(),
		aggregatedUsage: &fakeHourlyUsageRepository{active: map[time.Time][]string{}},
	}
	h.svc = &usageCheckpointService{ServiceParams: ServiceParams{
		Logger:                   logger.NewNoopLogger(),
		Config:                   &config.Configuration{AggregatedUsage: config.AggregatedUsageConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}}},
		CustomerRepo:             customers,
		SubRepo:                  subs,
		MeterRepo:                meters,
		MeterUsageRepo:           h.usage,
		MeterUsageCheckpointRepo: h.checkpoints,
		HourlyUsageRepo:          h.aggregatedUsage,
		RedisCache:               testutil.NewInMemoryRedis(),
	}}
	return h
}

// ingest adds one usage row for every meter at ts and marks the customer active in that hour.
func (h *checkpointHarness) ingest(t *testing.T, externalCustomerID, id string, ts time.Time, qty int64) {
	t.Helper()
	var records []*events.MeterUsage
	for _, meterID := range []string{"m_sum", "m_count", "m_max", "m_unique", "m_never"} {
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

// totals returns a customer's totals for the November period by meter and requires their watermark is at wantUntil.
func (h *checkpointHarness) totals(t *testing.T, customerID string, wantUntil time.Time) map[string]string {
	t.Helper()
	return h.periodTotals(t, customerID, checkpointPeriodStart, wantUntil)
}

func (h *checkpointHarness) periodTotals(t *testing.T, customerID string, periodStart, wantUntil time.Time) map[string]string {
	t.Helper()
	ctx := aggregatedUsageCtx()
	watermarks, err := h.checkpoints.ListMeterUsageCheckpointWatermarks(ctx, []string{customerID})
	require.NoError(t, err)
	require.Len(t, watermarks, 1)
	require.True(t, watermarks[0].AggregatedUntil.Equal(wantUntil), "watermark at %s, want %s", watermarks[0].AggregatedUntil, wantUntil)
	rows, err := h.checkpoints.ListPeriodTotals(ctx, customerID, periodStart)
	require.NoError(t, err)
	got := make(map[string]string, len(rows))
	for _, r := range rows {
		got[r.MeterID] = r.Total.String()
	}
	return got
}

func TestUpdateCustomerCheckpoints(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(5).Add(10*time.Minute), 4)
	h.ingest(t, "cust_1", "e2", checkpointHour(6).Add(20*time.Minute), 9)
	h.ingest(t, "cust_1", "e3", checkpointHour(7).Add(30*time.Minute), 2) // after the first cutoff

	result, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, result.Customers)
	require.Equal(t, 1, result.Outcomes["updated"])
	require.Equal(t, map[string]string{"m_sum": "13", "m_count": "2", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(7)),
		"first run counts from the period start; COUNT_UNIQUE and never-reset SUM are not checkpointed")

	result, err = h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["updated"])
	require.Equal(t, map[string]string{"m_sum": "15", "m_count": "3", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(8)),
		"second run adds only [07:00, 08:00); MAX keeps the higher stored value")

	result, err = h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["current"], "a re-run at the same cutoff changes nothing")
	require.Equal(t, map[string]string{"m_sum": "15", "m_count": "3", "m_max": "9"}, h.totals(t, "cus_1", checkpointHour(8)))
}

func TestUsageCheckpointReadsSharedWindowOnce(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(6), 4)
	h.ingest(t, "cust_2", "e2", checkpointHour(6).Add(time.Minute), 7)

	_, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, h.usage.totalsCalls, "both customers start from the period start, so one query covers them")
	require.Equal(t, 1, h.usage.finalCalls, "stored totals read with FINAL")
	require.Equal(t, map[string]string{"m_sum": "4", "m_count": "1", "m_max": "4"}, h.totals(t, "cus_1", checkpointHour(7)))
	require.Equal(t, map[string]string{"m_sum": "7", "m_count": "1", "m_max": "7"}, h.totals(t, "cus_2", checkpointHour(7)))
}

func TestUsageCheckpointGroupsCustomersByWindow(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(6), 4)
	_, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)

	h.usage.totalsCalls = 0
	h.ingest(t, "cust_1", "e2", checkpointHour(7), 1)
	h.ingest(t, "cust_2", "e3", checkpointHour(7), 3)
	_, err = h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, 2, h.usage.totalsCalls, "cust_1 adds [07:00, 08:00) while new cust_2 reads from the period start")
	require.Equal(t, map[string]string{"m_sum": "5", "m_count": "2", "m_max": "4"}, h.totals(t, "cus_1", checkpointHour(8)))
	require.Equal(t, map[string]string{"m_sum": "3", "m_count": "1", "m_max": "3"}, h.totals(t, "cus_2", checkpointHour(8)))
}

func TestUsageCheckpointClampsToPeriodEnd(t *testing.T) {
	periodEnd := checkpointHour(6)
	h := newCheckpointHarness(t, periodEnd)
	h.ingest(t, "cust_1", "e1", checkpointHour(5), 4)
	h.ingest(t, "cust_1", "e2", checkpointHour(7), 100) // next period's usage, in the hour the run reads

	_, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"m_sum": "4", "m_count": "1", "m_max": "4"}, h.totals(t, "cus_1", periodEnd))
}

func TestUsageCheckpointReadsOnlyTheFinalizedHour(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	h.ingest(t, "cust_1", "e1", checkpointHour(5), 4)

	result, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Zero(t, result.Customers, "cust_1 had no usage in [06:00, 07:00)")

	h.ingest(t, "cust_1", "e2", checkpointHour(7), 1)
	_, err = h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(8))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"m_sum": "5", "m_count": "2", "m_max": "4"}, h.totals(t, "cus_1", checkpointHour(8)),
		"the next active hour catches up from the period start")
}

func TestUsageCheckpointAddsUsageToEveryCurrentPeriod(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	require.NoError(t, h.svc.SubRepo.Create(aggregatedUsageCtx(), &subscription.Subscription{
		ID: "sub_1b", CustomerID: "cus_1", EnvironmentID: "env_1", SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: checkpointHour(3), CurrentPeriodEnd: checkpointPeriodStart.AddDate(0, 1, 0),
		BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
	}))
	h.ingest(t, "cust_1", "e1", checkpointHour(2), 4)
	h.ingest(t, "cust_1", "e2", checkpointHour(6), 5)

	result, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["updated"])
	require.Equal(t, map[string]string{"m_sum": "9", "m_count": "2", "m_max": "5"}, h.periodTotals(t, "cus_1", checkpointPeriodStart, checkpointHour(7)))
	require.Equal(t, map[string]string{"m_sum": "5", "m_count": "1", "m_max": "5"}, h.periodTotals(t, "cus_1", checkpointHour(3), checkpointHour(7)),
		"the second subscription's period counts only from its own start")
}

func TestUsageCheckpointCustomerWithoutSubscription(t *testing.T) {
	h := newCheckpointHarness(t, checkpointPeriodStart.AddDate(0, 1, 0))
	require.NoError(t, h.svc.CustomerRepo.Create(aggregatedUsageCtx(), &customer.Customer{
		ID: "cus_3", ExternalID: "cust_3", EnvironmentID: "env_1",
		BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
	}))
	h.ingest(t, "cust_3", "e1", checkpointHour(6), 4)

	result, err := h.svc.UpdateCustomerCheckpoints(aggregatedUsageCtx(), checkpointHour(7))
	require.NoError(t, err)
	require.Equal(t, 1, result.Outcomes["no_subscription"])
	require.Zero(t, h.usage.totalsCalls)
}

func TestUsageCheckpointActiveEnvironments(t *testing.T) {
	environments := testutil.NewInMemoryEnvironmentStore()
	// The in-memory store doesn't scope by tenant (the Postgres repository does), so only tenant_on is seeded.
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant_on")
	for _, id := range []string{"env_1", "env_2"} {
		require.NoError(t, environments.Create(ctx, &environment.Environment{
			ID: id, BaseModel: types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
		}))
	}
	svc := &usageCheckpointService{ServiceParams: ServiceParams{
		Config:          &config.Configuration{AggregatedUsage: config.AggregatedUsageConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}}},
		EnvironmentRepo: environments,
	}}

	got, err := svc.ActiveEnvironments(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []UsageCheckpointEnvironment{
		{TenantID: "tenant_on", EnvironmentID: "env_1"},
		{TenantID: "tenant_on", EnvironmentID: "env_2"},
	}, got, "every environment of each enabled tenant")

	svc.Config.AggregatedUsage.Enabled = false
	got, err = svc.ActiveEnvironments(context.Background())
	require.NoError(t, err)
	require.Empty(t, got, "the master switch off lists nothing")
}

func TestUsageCheckpointWithoutStore(t *testing.T) {
	svc := &usageCheckpointService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}}}
	_, err := svc.UpdateCustomerCheckpoints(context.Background(), checkpointHour(7))
	require.ErrorContains(t, err, "aggregated usage repository is not configured")
}
