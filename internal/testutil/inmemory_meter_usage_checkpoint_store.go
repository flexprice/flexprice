package testutil

import (
	"context"
	"strings"
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
	// periods holds period checkpoints by tenant|environment|customer|period start.
	periods map[string]*meterusagecheckpoint.PeriodCheckpoint
}

func NewInMemoryMeterUsageCheckpointStore() *InMemoryMeterUsageCheckpointStore {
	return &InMemoryMeterUsageCheckpointStore{
		InMemoryStore: NewInMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint](),
		periods:       make(map[string]*meterusagecheckpoint.PeriodCheckpoint),
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

func periodKey(ctx context.Context, customerID string, periodStart time.Time) string {
	return types.GetTenantID(ctx) + "|" + types.GetEnvironmentID(ctx) + "|" + customerID + "|" + periodStart.UTC().String()
}

func (s *InMemoryMeterUsageCheckpointStore) ListPeriodCheckpoints(
	ctx context.Context,
	customerIDs []string,
) ([]*meterusagecheckpoint.PeriodCheckpoint, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	prefix := types.GetTenantID(ctx) + "|" + types.GetEnvironmentID(ctx) + "|"
	wanted := lo.SliceToMap(customerIDs, func(id string) (string, bool) { return id, true })
	var result []*meterusagecheckpoint.PeriodCheckpoint
	for key, p := range s.periods {
		if strings.HasPrefix(key, prefix) && wanted[p.CustomerID] {
			copied := *p
			result = append(result, &copied)
		}
	}
	return result, nil
}

func (s *InMemoryMeterUsageCheckpointStore) AdvancePeriods(
	ctx context.Context,
	advances []*meterusagecheckpoint.PeriodAdvance,
) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	advanced := 0
	for _, a := range advances {
		key := periodKey(ctx, a.CustomerID, a.PeriodStart)
		p, ok := s.periods[key]
		if !ok {
			p = &meterusagecheckpoint.PeriodCheckpoint{CustomerID: a.CustomerID, PeriodStart: a.PeriodStart, AggregatedUntil: a.From}
			s.periods[key] = p
		}
		if !p.AggregatedUntil.Equal(a.From) {
			continue
		}
		p.AggregatedUntil = a.To
		advanced++
		if err := s.addPeriodTotals(ctx, a); err != nil {
			return advanced, err
		}
	}
	return advanced, nil
}

func (s *InMemoryMeterUsageCheckpointStore) addPeriodTotals(ctx context.Context, a *meterusagecheckpoint.PeriodAdvance) error {
	existing, err := s.ListPeriodTotals(ctx, a.CustomerID, a.PeriodStart)
	if err != nil {
		return err
	}
	byMeter := lo.KeyBy(existing, func(c *meterusagecheckpoint.MeterUsageCheckpoint) string { return c.MeterID })
	for _, t := range a.Totals {
		if current, ok := byMeter[t.MeterID]; ok {
			next := *current
			next.AggregatedUntil = a.To
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
		periodStart := a.PeriodStart
		row := &meterusagecheckpoint.MeterUsageCheckpoint{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT), CustomerID: a.CustomerID,
			MeterID: t.MeterID, AggregationType: t.AggregationType, Total: t.Quantity, AggregatedUntil: a.To,
			PeriodStart: &periodStart, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}
		if err := s.Create(ctx, row.ID, row); err != nil {
			return err
		}
	}
	return nil
}
