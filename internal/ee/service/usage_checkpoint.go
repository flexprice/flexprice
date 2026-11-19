package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/aggregatedusage"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// UsageCheckpointService adds finalized ClickHouse usage to per-period meter totals for customers
// whose usage is in the Redis hourly totals.
type UsageCheckpointService interface {
	// CheckpointEnvironment brings every recently active customer of the tenant and environment
	// in ctx up to cutoff: their current periods' totals then cover all usage before it.
	CheckpointEnvironment(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error)
}

// UsageCheckpointResult counts the customers read and their billing periods by outcome.
type UsageCheckpointResult struct {
	Customers int
	Outcomes  map[string]int
}

// record counts billing periods (or customers, before their periods are known) under outcome.
func (r *UsageCheckpointResult) record(ctx context.Context, outcome usageCheckpointOutcome, count int) {
	if count == 0 {
		return
	}
	r.Outcomes[string(outcome)] += count
	metrics.RecordCounter(ctx, metrics.UsageCheckpointCustomers, int64(count), metrics.L(metrics.KeyOutcome, string(outcome)))
}

type usageCheckpointOutcome string

const (
	usageCheckpointUpdated         usageCheckpointOutcome = "updated"
	usageCheckpointCurrent         usageCheckpointOutcome = "current"
	usageCheckpointUnknownCustomer usageCheckpointOutcome = "unknown_customer"
	usageCheckpointNoSubscription  usageCheckpointOutcome = "no_subscription"
	usageCheckpointConflict        usageCheckpointOutcome = "conflict"
	usageCheckpointFailed          usageCheckpointOutcome = "failed"
)

// checkpointBatchSize caps the customers in one Postgres or ClickHouse IN list. At 500, even
// 255-character IDs keep a ClickHouse query well under its 256 KiB max_query_size.
const checkpointBatchSize = 500

type usageCheckpointService struct {
	ServiceParams
}

// NewUsageCheckpointService returns the service behind the hourly usage checkpoint job.
func NewUsageCheckpointService(params ServiceParams) UsageCheckpointService {
	return &usageCheckpointService{ServiceParams: params}
}

// usageWindow is the [start, end) range of usage a run adds to a period. Times are UTC so equal
// windows are equal map keys.
type usageWindow struct {
	start, end time.Time
}

// periodUpdate is one customer's billing period, its stored rows and the window the run adds to them.
type periodUpdate struct {
	customerID         string
	externalCustomerID string
	periodStart        time.Time
	rows               []*meterusagecheckpoint.MeterUsageCheckpoint
	window             usageWindow
}

// windowTotals holds usage by window, then by external customer.
type windowTotals map[usageWindow]map[string][]*events.MeterTotals

func (s *usageCheckpointService) CheckpointEnvironment(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error) {
	if s.HourlyUsageRepo == nil {
		return nil, ierr.NewError("aggregated usage store is not configured").
			WithHint("Usage checkpoints need Redis; fix the redis config or disable aggregated_usage.").
			Mark(ierr.ErrSystem)
	}
	externalCustomerIDs, err := s.recentlyActiveCustomers(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	result := &UsageCheckpointResult{Customers: len(externalCustomerIDs), Outcomes: make(map[string]int)}
	updates, err := s.pendingPeriodUpdates(ctx, externalCustomerIDs, cutoff, result)
	if err != nil {
		return nil, err
	}
	totals, err := s.readWindowTotals(ctx, updates)
	if err != nil {
		return nil, err
	}
	meters, err := s.supportedMeters(ctx, totals)
	if err != nil {
		return nil, err
	}
	for _, update := range updates {
		result.record(ctx, s.writePeriodUpdate(ctx, update, totals[update.window][update.externalCustomerID], meters), 1)
	}
	return result, nil
}

// recentlyActiveCustomers reads the active sets of the late window's hours before cutoff, so a
// customer missed by up to two failed runs is still picked up.
func (s *usageCheckpointService) recentlyActiveCustomers(ctx context.Context, cutoff time.Time) ([]string, error) {
	var customers []string
	for hour := cutoff.Add(-aggregatedusage.LateWindow); hour.Before(cutoff); hour = hour.Add(time.Hour) {
		active, err := s.HourlyUsageRepo.ListActiveCustomers(ctx, hour)
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to read active customers from the aggregated usage").Mark(ierr.ErrSystem)
		}
		customers = append(customers, active...)
	}
	return lo.Uniq(customers), nil
}

// pendingPeriodUpdates loads the customers, their current periods and stored rows, and returns the
// periods with usage left to add. The rest are recorded in result straight away.
func (s *usageCheckpointService) pendingPeriodUpdates(
	ctx context.Context,
	externalCustomerIDs []string,
	cutoff time.Time,
	result *UsageCheckpointResult,
) ([]*periodUpdate, error) {
	var updates []*periodUpdate
	for _, batch := range lo.Chunk(externalCustomerIDs, checkpointBatchSize) {
		customerFilter := types.NewNoLimitCustomerFilter()
		customerFilter.ExternalIDs = batch
		customers, err := s.CustomerRepo.List(ctx, customerFilter)
		if err != nil {
			return nil, err
		}
		result.record(ctx, usageCheckpointUnknownCustomer, len(batch)-len(customers))
		if len(customers) == 0 {
			continue
		}

		subs, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
			QueryFilter:        types.NewNoLimitQueryFilter(),
			CustomerIDs:        lo.Map(customers, func(c *customer.Customer, _ int) string { return c.ID }),
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing},
		})
		if err != nil {
			return nil, err
		}
		subsByCustomer := lo.GroupBy(subs, func(sub *subscription.Subscription) string { return sub.CustomerID })

		for _, cust := range customers {
			periods := currentPeriods(subsByCustomer[cust.ID], cutoff)
			if len(periods) == 0 {
				result.record(ctx, usageCheckpointNoSubscription, 1)
				continue
			}
			for _, period := range periods {
				rows, err := s.MeterUsageCheckpointRepo.ListPeriodTotals(ctx, cust.ID, period.start)
				if err != nil {
					return nil, err
				}
				window := usageWindow{start: period.start, end: cutoff.UTC()}
				if len(rows) > 0 {
					window.start = rows[0].AggregatedUntil.UTC()
				}
				if period.end.Before(window.end) {
					window.end = period.end
				}
				if !window.end.After(window.start) {
					result.record(ctx, usageCheckpointCurrent, 1)
					continue
				}
				updates = append(updates, &periodUpdate{
					customerID:         cust.ID,
					externalCustomerID: cust.ExternalID,
					periodStart:        period.start,
					rows:               rows,
					window:             window,
				})
			}
		}
	}
	return updates, nil
}

type billingPeriod struct {
	start, end time.Time
}

// currentPeriods returns one period per distinct current period start that has begun. Subscriptions
// sharing a start share rows, which run to the latest end among them.
func currentPeriods(subs []*subscription.Subscription, cutoff time.Time) []billingPeriod {
	ends := make(map[time.Time]time.Time)
	for _, sub := range subs {
		start := sub.CurrentPeriodStart.UTC()
		if !start.Before(cutoff) {
			continue
		}
		if end, ok := ends[start]; !ok || sub.CurrentPeriodEnd.After(end) {
			ends[start] = sub.CurrentPeriodEnd.UTC()
		}
	}
	periods := make([]billingPeriod, 0, len(ends))
	for start, end := range ends {
		periods = append(periods, billingPeriod{start: start, end: end})
	}
	return periods
}

// readWindowTotals reads each window once for all the customers that share it. In steady state every
// customer shares [previous cutoff, cutoff), so a run is one query per batch of customers.
func (s *usageCheckpointService) readWindowTotals(ctx context.Context, updates []*periodUpdate) (windowTotals, error) {
	customersByWindow := make(map[usageWindow][]string)
	for _, update := range updates {
		customersByWindow[update.window] = append(customersByWindow[update.window], update.externalCustomerID)
	}

	totals := make(windowTotals, len(customersByWindow))
	for window, customers := range customersByWindow {
		byCustomer := make(map[string][]*events.MeterTotals)
		for _, batch := range lo.Chunk(lo.Uniq(customers), checkpointBatchSize) {
			rows, err := s.MeterUsageRepo.GetMeterTotals(ctx, &events.MeterUsageQueryParams{
				TenantID:            types.GetTenantID(ctx),
				EnvironmentID:       types.GetEnvironmentID(ctx),
				ExternalCustomerIDs: batch,
				StartTime:           window.start,
				EndTime:             window.end,
				// Stored totals, so FINAL merges duplicate rows.
				UseFinal: true,
			})
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				byCustomer[row.ExternalCustomerID] = append(byCustomer[row.ExternalCustomerID], row)
			}
		}
		totals[window] = byCustomer
	}
	return totals, nil
}

// supportedMeters loads every meter with usage in totals, keeping those a period total can hold.
func (s *usageCheckpointService) supportedMeters(ctx context.Context, totals windowTotals) (map[string]*meter.Meter, error) {
	var ids []string
	for _, byCustomer := range totals {
		for _, rows := range byCustomer {
			for _, row := range rows {
				ids = append(ids, row.MeterID)
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(ids))
	if err != nil {
		return nil, err
	}
	supported := make(map[string]*meter.Meter, len(meters))
	for _, m := range meters {
		if aggregatedusage.SupportsAggregation(m.Aggregation.Type) {
			supported[m.ID] = m
		}
	}
	return supported, nil
}

// writePeriodUpdate merges a window's usage into one period's rows and saves them in one transaction.
func (s *usageCheckpointService) writePeriodUpdate(
	ctx context.Context,
	update *periodUpdate,
	window []*events.MeterTotals,
	meters map[string]*meter.Meter,
) usageCheckpointOutcome {
	next, err := mergePeriodTotals(ctx, update.rows, window, meters, update.window.end)
	if err == nil {
		err = s.MeterUsageCheckpointRepo.UpdatePeriodTotals(ctx, meterusagecheckpoint.UpdatePeriodTotalsParams{
			CustomerID:              update.customerID,
			PeriodStart:             update.periodStart,
			ExpectedAggregatedUntil: update.window.start,
			Rows:                    next,
		})
	}
	if ierr.IsVersionConflict(err) {
		return usageCheckpointConflict
	}
	if err != nil {
		s.Logger.Error(ctx, "usage checkpoint: failed to update billing period", "error", err,
			"customer_id", update.customerID, "period_start", update.periodStart)
		return usageCheckpointFailed
	}
	return usageCheckpointUpdated
}

// mergePeriodTotals adds a window's usage to a period's rows and stamps every row with windowEnd, so
// the period's rows stay together. SUM and COUNT add, MAX keeps the higher value. Usage of a meter
// with no row starts one, unless the meter is missing from meters (deleted or unsupported).
func mergePeriodTotals(
	ctx context.Context,
	rows []*meterusagecheckpoint.MeterUsageCheckpoint,
	window []*events.MeterTotals,
	meters map[string]*meter.Meter,
	windowEnd time.Time,
) ([]*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	windowByMeter := lo.KeyBy(window, func(t *events.MeterTotals) string { return t.MeterID })
	next := make([]*meterusagecheckpoint.MeterUsageCheckpoint, 0, len(rows)+len(window))

	for _, row := range rows {
		updated := *row
		updated.AggregatedUntil = windowEnd
		if total, ok := windowByMeter[row.MeterID]; ok {
			if m, ok := meters[row.MeterID]; ok && m.Aggregation.Type != row.AggregationType {
				return nil, ierr.NewError("meter aggregation changed during the billing period").
					WithHint("Delete this customer's rows for the billing period; the next run rebuilds them from ClickHouse.").
					WithReportableDetails(map[string]any{"meter_id": m.ID, "stored": row.AggregationType, "current": m.Aggregation.Type}).
					Mark(ierr.ErrInternal)
			}
			updated.Total = addWindowTotal(row.AggregationType, row.Total, true, total)
			delete(windowByMeter, row.MeterID)
		}
		next = append(next, &updated)
	}

	for meterID, total := range windowByMeter {
		m, ok := meters[meterID]
		if !ok {
			continue
		}
		next = append(next, &meterusagecheckpoint.MeterUsageCheckpoint{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT),
			MeterID:         m.ID,
			AggregationType: m.Aggregation.Type,
			Total:           addWindowTotal(m.Aggregation.Type, decimal.Zero, false, total),
			AggregatedUntil: windowEnd,
			EnvironmentID:   types.GetEnvironmentID(ctx),
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})
	}
	return next, nil
}

// addWindowTotal merges one window's total into a stored total. hasStored is false for a new row,
// so a MAX of negative values is not clamped to zero.
func addWindowTotal(aggregation types.AggregationType, stored decimal.Decimal, hasStored bool, window *events.MeterTotals) decimal.Decimal {
	switch aggregation {
	case types.AggregationCount:
		return stored.Add(decimal.NewFromInt(int64(window.Count)))
	case types.AggregationMax:
		if !hasStored || window.Max.GreaterThan(stored) {
			return window.Max
		}
		return stored
	default:
		return stored.Add(window.Sum)
	}
}
