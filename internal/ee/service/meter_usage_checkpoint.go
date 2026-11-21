package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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
	// ActiveEnvironments returns the environments, of tenants with aggregated usage on, that had
	// customers with usage in the hour before cutoff.
	ActiveEnvironments(ctx context.Context, cutoff time.Time) ([]UsageCheckpointEnvironment, error)

	// CheckpointEnvironment brings the period totals of the customers active in the hour before cutoff,
	// in the tenant and environment in ctx, up to cutoff.
	CheckpointEnvironment(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error)
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
	usageCheckpointUpdated               usageCheckpointOutcome = "updated"
	usageCheckpointCurrent               usageCheckpointOutcome = "current"
	usageCheckpointConflict              usageCheckpointOutcome = "conflict"
	usageCheckpointUnknownCustomer       usageCheckpointOutcome = "unknown_customer"
	usageCheckpointNoSubscription        usageCheckpointOutcome = "no_subscription"
	usageCheckpointMultipleSubscriptions usageCheckpointOutcome = "multiple_subscriptions"
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

func (s *usageCheckpointService) ActiveEnvironments(ctx context.Context, cutoff time.Time) ([]UsageCheckpointEnvironment, error) {
	active, err := s.activeCustomers(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	environments := lo.Uniq(lo.Map(active, func(c aggregatedusage.ActiveCustomer, _ int) UsageCheckpointEnvironment {
		return UsageCheckpointEnvironment{TenantID: c.TenantID, EnvironmentID: c.EnvironmentID}
	}))
	return lo.Filter(environments, func(e UsageCheckpointEnvironment, _ int) bool {
		return s.Config.AggregatedUsage.IsEnabledForTenant(e.TenantID)
	}), nil
}

func (s *usageCheckpointService) CheckpointEnvironment(ctx context.Context, cutoff time.Time) (*UsageCheckpointResult, error) {
	active, err := s.activeCustomers(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	var externalCustomerIDs []string
	for _, c := range active {
		if c.TenantID == tenantID && c.EnvironmentID == environmentID {
			externalCustomerIDs = append(externalCustomerIDs, c.ExternalCustomerID)
		}
	}

	result := &UsageCheckpointResult{Customers: len(externalCustomerIDs), Outcomes: make(map[string]int)}
	for _, batch := range lo.Chunk(externalCustomerIDs, checkpointBatchSize) {
		if err := s.checkpointBatch(ctx, batch, cutoff, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// activeCustomers reads the customers with usage in the hour before cutoff: the hour this run finalizes.
// A customer missed by a failed run is caught up the next time they are active, from their checkpoint.
func (s *usageCheckpointService) activeCustomers(ctx context.Context, cutoff time.Time) ([]aggregatedusage.ActiveCustomer, error) {
	if s.HourlyUsageRepo == nil {
		return nil, ierr.NewError("aggregated usage repository is not configured").
			WithHint("Usage checkpoints need Redis; fix the redis config or disable aggregated_usage.").
			Mark(ierr.ErrSystem)
	}
	active, err := s.HourlyUsageRepo.ListActiveCustomers(ctx, cutoff.Add(-time.Hour))
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read active customers from the aggregated usage").Mark(ierr.ErrSystem)
	}
	return active, nil
}

// checkpointBatch brings up to checkpointBatchSize customers up to cutoff: two Postgres reads, one
// ClickHouse read per distinct window and one Postgres transaction.
func (s *usageCheckpointService) checkpointBatch(ctx context.Context, externalCustomerIDs []string, cutoff time.Time, result *UsageCheckpointResult) error {
	customerFilter := types.NewNoLimitCustomerFilter()
	customerFilter.ExternalIDs = externalCustomerIDs
	customers, err := s.CustomerRepo.List(ctx, customerFilter)
	if err != nil {
		return err
	}
	result.record(ctx, usageCheckpointUnknownCustomer, len(externalCustomerIDs)-len(customers))
	if len(customers) == 0 {
		return nil
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

	checkpoints, err := s.MeterUsageCheckpointRepo.ListPeriodCheckpoints(ctx, customerIDs)
	if err != nil {
		return err
	}
	aggregatedUntil := make(map[string]time.Time, len(checkpoints))
	for _, c := range checkpoints {
		aggregatedUntil[c.CustomerID+"|"+c.PeriodStart.UTC().String()] = c.AggregatedUntil.UTC()
	}

	advances := make(map[string]*meterusagecheckpoint.PeriodAdvance)
	customersByWindow := make(map[usageWindow][]string)
	for _, cust := range customers {
		switch len(subsByCustomer[cust.ID]) {
		case 0:
			result.record(ctx, usageCheckpointNoSubscription, 1)
			continue
		case 1:
		default:
			result.record(ctx, usageCheckpointMultipleSubscriptions, 1)
			continue
		}
		sub := subsByCustomer[cust.ID][0]
		periodStart := sub.CurrentPeriodStart.UTC()
		window := usageWindow{start: periodStart, end: types.EarliestOf(cutoff.UTC(), sub.CurrentPeriodEnd.UTC())}
		if at, ok := aggregatedUntil[cust.ID+"|"+periodStart.String()]; ok {
			window.start = at
		}
		if !window.end.After(window.start) {
			result.record(ctx, usageCheckpointCurrent, 1)
			continue
		}
		advances[cust.ExternalID] = &meterusagecheckpoint.PeriodAdvance{
			CustomerID: cust.ID, PeriodStart: periodStart, From: window.start, To: window.end,
		}
		customersByWindow[window] = append(customersByWindow[window], cust.ExternalID)
	}
	if len(advances) == 0 {
		return nil
	}

	if err := s.addWindowUsage(ctx, customersByWindow, advances); err != nil {
		return err
	}
	advanced, err := s.MeterUsageCheckpointRepo.AdvancePeriods(ctx, lo.Values(advances))
	if err != nil {
		return err
	}
	result.record(ctx, usageCheckpointUpdated, advanced)
	result.record(ctx, usageCheckpointConflict, len(advances)-advanced)
	return nil
}

// addWindowUsage reads each window's usage from ClickHouse, once for all customers sharing it, and adds
// it to their advances. Meters a period total does not hold (deleted, unsupported or never reset) are left out.
func (s *usageCheckpointService) addWindowUsage(
	ctx context.Context,
	customersByWindow map[usageWindow][]string,
	advances map[string]*meterusagecheckpoint.PeriodAdvance,
) error {
	var rows []*events.MeterTotals
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
		rows = append(rows, totals...)
	}
	if len(rows) == 0 {
		return nil
	}

	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(lo.Map(rows, func(t *events.MeterTotals, _ int) string { return t.MeterID })))
	if err != nil {
		return err
	}
	aggregationByMeter := make(map[string]types.AggregationType, len(meters))
	for _, m := range meters {
		// Never-reset SUM meters keep running totals instead, and the read path doesn't use them.
		if aggregatedusage.SupportsAggregation(m.Aggregation.Type) && !keepsRunningAggregate(m) {
			aggregationByMeter[m.ID] = m.Aggregation.Type
		}
	}
	for _, row := range rows {
		aggregation, ok := aggregationByMeter[row.MeterID]
		if !ok {
			continue
		}
		advance := advances[row.ExternalCustomerID]
		advance.Totals = append(advance.Totals, meterusagecheckpoint.PeriodTotal{
			MeterID:         row.MeterID,
			AggregationType: aggregation,
			Quantity:        addWindowTotal(aggregation, decimal.Zero, false, row),
		})
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
