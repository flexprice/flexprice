package testutil

import (
	"context"
	"sync"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryMeterUsageCheckpointStore implements meterusagecheckpoint.Repository in memory.
type InMemoryMeterUsageCheckpointStore struct {
	writeMu sync.Mutex
	*InMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint]
	// watermarks holds usage watermarks by tenant|environment|customer.
	watermarks map[string]time.Time
}

func NewInMemoryMeterUsageCheckpointStore() *InMemoryMeterUsageCheckpointStore {
	return &InMemoryMeterUsageCheckpointStore{
		InMemoryStore: NewInMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint](),
		watermarks:    make(map[string]time.Time),
	}
}

func (s *InMemoryMeterUsageCheckpointStore) GetLatestBefore(
	ctx context.Context,
	params meterusagecheckpoint.GetLatestBeforeParams,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	rows, err := s.matching(ctx, params.CustomerID, params.MeterID)
	if err != nil {
		return nil, err
	}

	var latest *meterusagecheckpoint.MeterUsageCheckpoint
	for _, c := range rows {
		if c.PeriodStart != nil || c.AggregationType != params.AggregationType || !c.AggregatedUntil.Before(params.Before) {
			continue
		}
		if latest == nil || c.AggregatedUntil.After(latest.AggregatedUntil) {
			latest = c
		}
	}
	return latest, nil
}

func (s *InMemoryMeterUsageCheckpointStore) Upsert(
	ctx context.Context,
	c *meterusagecheckpoint.MeterUsageCheckpoint,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	if c == nil || c.ID == "" || c.CustomerID == "" || c.MeterID == "" || c.AggregatedUntil.IsZero() || !c.AggregationType.Validate() {
		return nil, ierr.NewError("invalid meter usage checkpoint").Mark(ierr.ErrValidation)
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || c.TenantID != tenantID || (c.EnvironmentID != "" && c.EnvironmentID != environmentID) {
		return nil, ierr.NewError("checkpoint scope does not match request context").Mark(ierr.ErrValidation)
	}
	copied := *c
	copied.EnvironmentID = environmentID
	copied.Status = types.StatusPublished
	c = &copied
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	rows, err := s.matching(ctx, c.CustomerID, c.MeterID)
	if err != nil {
		return nil, err
	}
	for _, existing := range rows {
		if existing.PeriodStart != nil || !existing.AggregatedUntil.Equal(c.AggregatedUntil) {
			continue
		}
		updated := meterusagecheckpoint.NewMeterUsageCheckpointBuilder(existing).
			WithAggregationType(c.AggregationType).
			WithTotal(c.Total).
			WithInvoiceID(c.InvoiceID).
			WithUpdatedAt(time.Now().UTC()).
			Build()
		if err := s.Update(ctx, updated.ID, updated); err != nil {
			return nil, err
		}
		return updated, nil
	}

	if err := s.Create(ctx, c.ID, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *InMemoryMeterUsageCheckpointStore) matching(
	ctx context.Context,
	customerID, meterID string,
) ([]*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	return s.List(ctx, nil, func(ctx context.Context, c *meterusagecheckpoint.MeterUsageCheckpoint, _ interface{}) bool {
		return c.TenantID == types.GetTenantID(ctx) &&
			c.EnvironmentID == types.GetEnvironmentID(ctx) && c.Status == types.StatusPublished &&
			c.CustomerID == customerID &&
			c.MeterID == meterID
	}, nil)
}

func (s *InMemoryMeterUsageCheckpointStore) ListPeriodTotals(
	ctx context.Context,
	customerID string,
	periodStart time.Time,
) ([]*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	return s.List(ctx, nil, func(ctx context.Context, c *meterusagecheckpoint.MeterUsageCheckpoint, _ interface{}) bool {
		return c.TenantID == types.GetTenantID(ctx) &&
			c.EnvironmentID == types.GetEnvironmentID(ctx) && c.Status == types.StatusPublished &&
			c.CustomerID == customerID &&
			c.PeriodStart != nil && c.PeriodStart.Equal(periodStart)
	}, nil)
}

func watermarkKey(ctx context.Context, customerID string) string {
	return types.GetTenantID(ctx) + "|" + types.GetEnvironmentID(ctx) + "|" + customerID
}

func (s *InMemoryMeterUsageCheckpointStore) ListMeterUsageCheckpointWatermarks(
	ctx context.Context,
	customerIDs []string,
) ([]*meterusagecheckpoint.MeterUsageCheckpointWatermark, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result []*meterusagecheckpoint.MeterUsageCheckpointWatermark
	for _, customerID := range customerIDs {
		if at, ok := s.watermarks[watermarkKey(ctx, customerID)]; ok {
			result = append(result, &meterusagecheckpoint.MeterUsageCheckpointWatermark{CustomerID: customerID, AggregatedUntil: at})
		}
	}
	return result, nil
}

func (s *InMemoryMeterUsageCheckpointStore) UpdateCustomerCheckpoints(
	ctx context.Context,
	updates []*meterusagecheckpoint.CustomerCheckpointUpdate,
) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	applied := 0
	for _, u := range updates {
		key := watermarkKey(ctx, u.CustomerID)
		at, ok := s.watermarks[key]
		if ok != !u.From.IsZero() || (ok && !at.Equal(u.From)) {
			continue
		}
		s.watermarks[key] = u.To
		applied++
		if err := s.addPeriodTotals(ctx, u); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func (s *InMemoryMeterUsageCheckpointStore) addPeriodTotals(ctx context.Context, u *meterusagecheckpoint.CustomerCheckpointUpdate) error {
	for _, t := range u.Totals {
		existing, err := s.ListPeriodTotals(ctx, u.CustomerID, t.PeriodStart)
		if err != nil {
			return err
		}
		current, found := lo.Find(existing, func(c *meterusagecheckpoint.MeterUsageCheckpoint) bool { return c.MeterID == t.MeterID })
		if found {
			next := *current
			next.AggregatedUntil = u.To
			if current.AggregationType == types.AggregationMax {
				next.Total = decimal.Max(current.Total, t.Quantity)
			} else {
				next.Total = current.Total.Add(t.Quantity)
			}
			if err := s.Update(ctx, next.ID, &next); err != nil {
				return err
			}
			continue
		}
		periodStart := t.PeriodStart
		row := &meterusagecheckpoint.MeterUsageCheckpoint{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT), CustomerID: u.CustomerID,
			MeterID: t.MeterID, AggregationType: t.AggregationType, Total: t.Quantity, AggregatedUntil: u.To,
			PeriodStart: &periodStart, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}
		if err := s.Create(ctx, row.ID, row); err != nil {
			return err
		}
	}
	return nil
}
