package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryMeterUsageCheckpointStore implements meterusagecheckpoint.Repository in memory.
type InMemoryMeterUsageCheckpointStore struct {
	*InMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint]
}

func NewInMemoryMeterUsageCheckpointStore() *InMemoryMeterUsageCheckpointStore {
	return &InMemoryMeterUsageCheckpointStore{
		InMemoryStore: NewInMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint](),
	}
}

func (s *InMemoryMeterUsageCheckpointStore) GetLatestAtOrBefore(
	ctx context.Context,
	customerID, meterID string,
	at time.Time,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	rows, err := s.matching(ctx, customerID, meterID)
	if err != nil {
		return nil, err
	}

	var latest *meterusagecheckpoint.MeterUsageCheckpoint
	for _, c := range rows {
		if c.AggregatedUntil.After(at) {
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
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	rows, err := s.matching(ctx, c.CustomerID, c.MeterID)
	if err != nil {
		return nil, err
	}
	for _, existing := range rows {
		if !existing.AggregatedUntil.Equal(c.AggregatedUntil) {
			continue
		}
		updated := *existing
		updated.Total = c.Total
		updated.InvoiceID = c.InvoiceID
		updated.UpdatedAt = time.Now().UTC()
		if err := s.Update(ctx, updated.ID, &updated); err != nil {
			return nil, err
		}
		return &updated, nil
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
			CheckEnvironmentFilter(ctx, c.EnvironmentID) &&
			c.CustomerID == customerID &&
			c.MeterID == meterID
	}, nil)
}
