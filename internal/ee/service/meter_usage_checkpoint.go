package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CheckpointedUsageRequest names one customer's running aggregate of a meter: every
// change before Until, aggregated by AggregationType.
type CheckpointedUsageRequest struct {
	CustomerID          string
	ExternalCustomerIDs []string
	MeterID             string
	AggregationType     types.AggregationType
	Until               time.Time
	// IgnoreCheckpoints sums from the first event instead of starting at a saved checkpoint.
	IgnoreCheckpoints bool
}

func (s *meterUsageService) GetCheckpointedUsage(ctx context.Context, req CheckpointedUsageRequest) (decimal.Decimal, error) {
	return s.checkpointedUsage(ctx, req)
}

func (s *meterUsageService) SaveUsageCheckpoint(
	ctx context.Context,
	req CheckpointedUsageRequest,
	invoiceID *string,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	total, err := s.checkpointedUsage(ctx, req)
	if err != nil {
		return nil, err
	}

	return s.MeterUsageCheckpointRepo.Upsert(ctx, &meterusagecheckpoint.MeterUsageCheckpoint{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT),
		CustomerID:      req.CustomerID,
		MeterID:         req.MeterID,
		AggregationType: req.AggregationType,
		Total:           total,
		AggregatedUntil: req.Until,
		InvoiceID:       invoiceID,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	})
}

// checkpointedUsage adds the usage in [checkpoint.aggregated_until, req.Until) to the
// latest checkpoint strictly before req.Until, so saving the same instant again rebuilds
// it from the one before. With no checkpoint, the usage is summed from the start.
func (s *meterUsageService) checkpointedUsage(ctx context.Context, req CheckpointedUsageRequest) (decimal.Decimal, error) {
	if req.CustomerID == "" || req.MeterID == "" || req.Until.IsZero() || len(req.ExternalCustomerIDs) == 0 {
		return decimal.Zero, ierr.NewError("customer, meter, external customer IDs and cutoff are required").Mark(ierr.ErrValidation)
	}
	if types.GetTenantID(ctx) == "" {
		return decimal.Zero, ierr.NewError("tenant is required for checkpointed usage").Mark(ierr.ErrValidation)
	}
	if req.AggregationType != types.AggregationSum {
		return decimal.Zero, ierr.NewError("aggregation type does not support checkpoints").
			WithHint("Only SUM meters can be checkpointed.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         req.MeterID,
				"aggregation_type": req.AggregationType,
			}).
			Mark(ierr.ErrValidation)
	}
	if s.MeterUsageCheckpointRepo == nil {
		return decimal.Zero, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	total, from := decimal.Zero, time.Time{}
	if !req.IgnoreCheckpoints {
		checkpoint, err := s.MeterUsageCheckpointRepo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
			CustomerID:      req.CustomerID,
			MeterID:         req.MeterID,
			AggregationType: req.AggregationType,
			Before:          req.Until,
		})
		if err != nil {
			return decimal.Zero, err
		}
		if checkpoint != nil {
			total, from = checkpoint.Total, checkpoint.AggregatedUntil
		}
	}

	changes, err := s.GetUsageTotal(ctx, &dto.UsageTotalRequest{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: req.ExternalCustomerIDs,
		MeterID:             req.MeterID,
		AggregationType:     req.AggregationType,
		StartTime:           from,
		EndTime:             req.Until,
	})
	if err != nil {
		return decimal.Zero, err
	}
	return total.Add(changes), nil
}

// keepsRunningAggregate reports whether a meter retains usage across billing periods.
func keepsRunningAggregate(m *meter.Meter) bool {
	return m != nil && m.Aggregation.Type == types.AggregationSum && m.ResetUsage == types.ResetUsageNever
}

// usesCheckpoints reports whether a subscription's running totals are checkpointed. A parent's
// total spans its current children, so one row keyed by the parent cannot hold it.
func usesCheckpoints(sub *subscription.Subscription) bool {
	return sub != nil && sub.SubscriptionType != types.SubscriptionTypeParent
}

func (s *meterUsageService) SaveUsageCheckpointsForSubscription(
	ctx context.Context,
	sub *subscription.Subscription,
	until time.Time,
	invoiceID *string,
) error {
	if !usesCheckpoints(sub) || s.MeterRepo == nil {
		return nil
	}

	meterIDs := make([]string, 0, len(sub.LineItems))
	for _, item := range sub.LineItems {
		if item != nil && item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" && item.StartDate.Before(until) {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	if len(meterIDs) == 0 {
		return nil
	}

	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(meterIDs))
	if err != nil {
		return err
	}
	meters = lo.Filter(meters, func(m *meter.Meter, _ int) bool {
		return keepsRunningAggregate(m)
	})
	if len(meters) == 0 {
		return nil
	}

	externalCustomerIDs, err := s.resolveExternalCustomerIDs(ctx, sub, true)
	if err != nil {
		return err
	}
	for _, m := range meters {
		req := CheckpointedUsageRequest{
			CustomerID:          sub.CustomerID,
			ExternalCustomerIDs: externalCustomerIDs,
			MeterID:             m.ID,
			AggregationType:     m.Aggregation.Type,
			Until:               until,
		}
		if _, err := s.SaveUsageCheckpoint(ctx, req, invoiceID); err != nil {
			return err
		}
	}
	return nil
}

// UsageCheckpointService adds finalized ClickHouse usage to per-period meter totals for customers
// whose usage is in the Redis hourly totals.
type UsageCheckpointService interface {
	// ActiveEnvironments returns every environment of the tenants with usage rollup on.
	ActiveEnvironments(ctx context.Context) ([]UsageCheckpointEnvironment, error)

	// UpdateCustomerCheckpoints brings the period totals of the customers active in the hour before
	// cutoff, in the tenant and environment in ctx, up to cutoff.
	UpdateCustomerCheckpoints(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error)
}

// UsageCheckpointEnvironment names one tenant environment to checkpoint.
type UsageCheckpointEnvironment struct {
	TenantID      string
	EnvironmentID string
}

// UsageCheckpointResult counts the customers read and what happened to each.
type UsageCheckpointResult struct {
	Customers int
	Outcomes  map[string]int
}

func (r *UsageCheckpointResult) record(ctx context.Context, outcome usageCheckpointOutcome, count int) {
	if count == 0 {
		return
	}
	r.Outcomes[string(outcome)] += count
	metrics.RecordCounter(ctx, metrics.UsageCheckpointCustomers, int64(count), metrics.L(metrics.KeyOutcome, string(outcome)))
}

type usageCheckpointOutcome string

const (
	usageCheckpointUpdated        usageCheckpointOutcome = "updated"
	usageCheckpointCurrent        usageCheckpointOutcome = "current"
	usageCheckpointConflict       usageCheckpointOutcome = "conflict"
	usageCheckpointNoSubscription usageCheckpointOutcome = "no_subscription"
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

// usageWindow is the [start, end) range of usage a run reads. Times are UTC so equal windows are
// equal map keys.
type usageWindow struct {
	start, end time.Time
}

// customerWindow is one customer's usage in one window.
type customerWindow struct {
	externalCustomerID string
	window             usageWindow
}

func (s *usageCheckpointService) ActiveEnvironments(ctx context.Context) ([]UsageCheckpointEnvironment, error) {
	usageRollup := s.Config.UsageRollup
	if !usageRollup.Enabled {
		return nil, nil
	}
	var environments []UsageCheckpointEnvironment
	for _, tenantID := range usageRollup.Tenants() {
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenantID)
		filter := types.GetDefaultFilter()
		for {
			page, err := s.EnvironmentRepo.List(tenantCtx, filter)
			if err != nil {
				return nil, err
			}
			for _, e := range page {
				environments = append(environments, UsageCheckpointEnvironment{TenantID: tenantID, EnvironmentID: e.ID})
			}
			if len(page) < filter.Limit {
				break
			}
			filter.Offset += filter.Limit
		}
	}
	return environments, nil
}

func (s *usageCheckpointService) UpdateCustomerCheckpoints(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error) {
	if s.HourlyUsageRepo == nil {
		return nil, ierr.NewError("usage rollup repository is not configured").
			WithHint("Usage checkpoints need Redis; fix the redis config or disable usage_rollup.").
			Mark(ierr.ErrSystem)
	}
	// Only the hour this run finalizes: a customer missed by a failed run is caught up the next time
	// they are active, from their watermark.
	externalCustomerIDs, err := s.HourlyUsageRepo.ListActiveCustomers(ctx, cutoff.Add(-time.Hour))
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read active customers from the usage rollup").Mark(ierr.ErrSystem)
	}

	result := &UsageCheckpointResult{Customers: len(externalCustomerIDs), Outcomes: make(map[string]int)}
	for _, batch := range lo.Chunk(externalCustomerIDs, checkpointBatchSize) {
		if err := s.updateCustomerCheckpoints(ctx, batch, cutoff, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// updateCustomerCheckpoints brings up to checkpointBatchSize customers up to cutoff: three Postgres reads,
// one ClickHouse read per distinct window and one Postgres transaction.
func (s *usageCheckpointService) updateCustomerCheckpoints(ctx context.Context, externalCustomerIDs []string, cutoff time.Time, result *UsageCheckpointResult) error {
	customerFilter := types.NewNoLimitCustomerFilter()
	customerFilter.ExternalIDs = externalCustomerIDs
	customers, err := s.CustomerRepo.List(ctx, customerFilter)
	if err != nil || len(customers) == 0 {
		return err
	}
	customerIDs := lo.Map(customers, func(c *customer.Customer, _ int) string { return c.ID })

	subs, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CustomerIDs:        customerIDs,
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing},
	})
	if err != nil {
		return err
	}
	subsByCustomer := lo.GroupBy(subs, func(sub *subscription.Subscription) string { return sub.CustomerID })

	watermarks, err := s.MeterUsageCheckpointRepo.ListMeterUsageCheckpointWatermarks(ctx, customerIDs)
	if err != nil {
		return err
	}
	watermarkByCustomer := lo.SliceToMap(watermarks, func(w *meterusagecheckpoint.MeterUsageCheckpointWatermark) (string, time.Time) {
		return w.CustomerID, w.AggregatedUntil.UTC()
	})

	updates := make(map[string]*meterusagecheckpoint.CustomerCheckpointUpdate)
	// customersByWindow is who to read from ClickHouse per window; periodStarts is which billing periods
	// each customer's window usage is added to.
	customersByWindow := make(map[usageWindow][]string)
	periodStarts := make(map[customerWindow][]time.Time)
	for _, cust := range customers {
		customerSubs := subsByCustomer[cust.ID]
		if len(customerSubs) == 0 {
			result.record(ctx, usageCheckpointNoSubscription, 1)
			continue
		}
		watermark := watermarkByCustomer[cust.ID]
		// Stop at the earliest period end, so usage after it waits for the next period instead of being skipped.
		to := cutoff.UTC()
		for _, sub := range customerSubs {
			to = types.EarliestOf(to, sub.CurrentPeriodEnd.UTC())
		}
		starts := lo.Uniq(lo.Map(customerSubs, func(sub *subscription.Subscription, _ int) time.Time { return sub.CurrentPeriodStart.UTC() }))
		windowed := false
		for _, start := range starts {
			window := usageWindow{start: types.LatestOf(watermark, start), end: to}
			if !window.end.After(window.start) {
				continue
			}
			key := customerWindow{externalCustomerID: cust.ExternalID, window: window}
			if len(periodStarts[key]) == 0 {
				customersByWindow[window] = append(customersByWindow[window], cust.ExternalID)
			}
			periodStarts[key] = append(periodStarts[key], start)
			windowed = true
		}
		if !windowed {
			result.record(ctx, usageCheckpointCurrent, 1)
			continue
		}
		updates[cust.ExternalID] = &meterusagecheckpoint.CustomerCheckpointUpdate{CustomerID: cust.ID, From: watermark, To: to}
	}
	if len(updates) == 0 {
		return nil
	}

	if err := s.addWindowUsage(ctx, customersByWindow, periodStarts, updates); err != nil {
		return err
	}
	applied, err := s.MeterUsageCheckpointRepo.UpdateCustomerCheckpoints(ctx, lo.Values(updates))
	if err != nil {
		return err
	}
	// Reads cache each period's totals and watermark; drop them so the next read picks up the new ones.
	if s.RedisCache != nil {
		for _, cust := range customers {
			if _, ok := updates[cust.ExternalID]; !ok {
				continue
			}
			for _, sub := range subsByCustomer[cust.ID] {
				s.RedisCache.ForceCacheDelete(ctx, usageSnapshotKey(ctx, cust.ID, sub.CurrentPeriodStart.UTC()))
			}
		}
	}
	result.record(ctx, usageCheckpointUpdated, applied)
	result.record(ctx, usageCheckpointConflict, len(updates)-applied)
	return nil
}

// addWindowUsage reads each window's usage from ClickHouse, once for all customers sharing it, and adds it
// to their updates under every billing period it belongs to. Meters a period total does not hold (deleted,
// unsupported or never reset) are left out.
func (s *usageCheckpointService) addWindowUsage(
	ctx context.Context,
	customersByWindow map[usageWindow][]string,
	periodStarts map[customerWindow][]time.Time,
	updates map[string]*meterusagecheckpoint.CustomerCheckpointUpdate,
) error {
	type windowRow struct {
		window usageWindow
		total  *events.MeterTotals
	}
	var rows []windowRow
	for window, externalCustomerIDs := range customersByWindow {
		totals, err := s.MeterUsageRepo.GetMeterTotals(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: externalCustomerIDs,
			StartTime:           window.start,
			EndTime:             window.end,
			// Stored totals, so FINAL merges duplicate rows.
			UseFinal: true,
		})
		if err != nil {
			return err
		}
		for _, t := range totals {
			rows = append(rows, windowRow{window: window, total: t})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(lo.Map(rows, func(r windowRow, _ int) string { return r.total.MeterID })))
	if err != nil {
		return err
	}
	aggregationByMeter := make(map[string]types.AggregationType, len(meters))
	for _, m := range meters {
		// Never-reset SUM meters keep running totals instead, and the read path doesn't use them.
		if usagerollup.SupportsAggregation(m.Aggregation.Type) && !keepsRunningAggregate(m) {
			aggregationByMeter[m.ID] = m.Aggregation.Type
		}
	}
	for _, r := range rows {
		aggregation, ok := aggregationByMeter[r.total.MeterID]
		if !ok {
			continue
		}
		update := updates[r.total.ExternalCustomerID]
		for _, start := range periodStarts[customerWindow{externalCustomerID: r.total.ExternalCustomerID, window: r.window}] {
			update.Totals = append(update.Totals, meterusagecheckpoint.PeriodTotal{
				PeriodStart:     start,
				MeterID:         r.total.MeterID,
				AggregationType: aggregation,
				Quantity:        addWindowTotal(aggregation, decimal.Zero, false, r.total),
			})
		}
	}
	return nil
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
