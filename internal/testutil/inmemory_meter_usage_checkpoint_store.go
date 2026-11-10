package testutil

import (
	"context"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryMeterUsageCheckpointStore implements meterusagecheckpoint.Repository in memory.
type InMemoryMeterUsageCheckpointStore struct {
	writeMu sync.Mutex
	*InMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint]
}

func NewInMemoryMeterUsageCheckpointStore() *InMemoryMeterUsageCheckpointStore {
	return &InMemoryMeterUsageCheckpointStore{
		InMemoryStore: NewInMemoryStore[*meterusagecheckpoint.MeterUsageCheckpoint](),
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
		if c.AggregationType != params.AggregationType || !c.AggregatedUntil.Before(params.Before) {
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
		if !existing.AggregatedUntil.Equal(c.AggregatedUntil) {
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
