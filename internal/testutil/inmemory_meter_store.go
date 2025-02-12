package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryMeterStore implements meter.Repository
type InMemoryMeterStore struct {
	*InMemoryStore[*meter.Meter]
}

// NewInMemoryMeterStore creates a new in-memory meter store
func NewInMemoryMeterStore() *InMemoryMeterStore {
	return &InMemoryMeterStore{
		InMemoryStore: NewInMemoryStore[*meter.Meter](),
	}
}

// Helper to copy meter
func copyMeter(m *meter.Meter) *meter.Meter {
	if m == nil {
		return nil
	}

	// Deep copy of meter
	meter := &meter.Meter{
		ID:        m.ID,
		Name:      m.Name,
		EventName: m.EventName,
		BaseModel: types.BaseModel{
			TenantID:  m.TenantID,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
			CreatedBy: m.CreatedBy,
			UpdatedBy: m.UpdatedBy,
		},
		Aggregation: m.Aggregation,
		Filters:     make([]meter.Filter, len(m.Filters)),
		ResetUsage:  m.ResetUsage,
	}

	// Deep copy filters
	copy(meter.Filters, m.Filters)

	return meter
}

func (s *InMemoryMeterStore) CreateMeter(ctx context.Context, m *meter.Meter) error {
	return s.InMemoryStore.Create(ctx, m.ID, copyMeter(m))
}

func (s *InMemoryMeterStore) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryMeterStore) List(ctx context.Context, filter *types.MeterFilter) ([]*meter.Meter, error) {
	return s.InMemoryStore.List(ctx, filter, meterFilterFn, meterSortFn)
}

func (s *InMemoryMeterStore) ListAll(ctx context.Context, filter *types.MeterFilter) ([]*meter.Meter, error) {
	f := *filter
	f.QueryFilter = types.NewNoLimitQueryFilter()
	return s.List(ctx, &f)
}

func (s *InMemoryMeterStore) Count(ctx context.Context, filter *types.MeterFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, meterFilterFn)
}

func (s *InMemoryMeterStore) DisableMeter(ctx context.Context, id string) error {
	m, err := s.GetMeter(ctx, id)
	if err != nil {
		return err
	}

	m.Status = types.StatusDeleted
	return s.InMemoryStore.Update(ctx, m.ID, m)
}

func (s *InMemoryMeterStore) UpdateMeter(ctx context.Context, id string, filters []meter.Filter) error {
	m, err := s.GetMeter(ctx, id)
	if err != nil {
		return err
	}

	if m.Filters == nil {
		m.Filters = []meter.Filter{}
	}

	// Merge new filters into the existing filters
	existingFilters := map[string][]string{}
	for _, f := range m.Filters {
		existingFilters[f.Key] = f.Values
	}

	for _, newFilter := range filters {
		if _, exists := existingFilters[newFilter.Key]; !exists {
			// If the key doesn't exist, add the entire filter
			existingFilters[newFilter.Key] = newFilter.Values
		} else {
			// Append new values for an existing key, avoiding duplicates
			for _, newValue := range newFilter.Values {
				if !lo.Contains(existingFilters[newFilter.Key], newValue) {
					existingFilters[newFilter.Key] = append(existingFilters[newFilter.Key], newValue)
				}
			}
		}
	}

	// Update the meter's filters
	updatedFilters := []meter.Filter{}
	for key, values := range existingFilters {
		updatedFilters = append(updatedFilters, meter.Filter{
			Key:    key,
			Values: values,
		})
	}

	m.Filters = updatedFilters
	return s.InMemoryStore.Update(ctx, m.ID, m)
}

// meterFilterFn implements filtering logic for meters
func meterFilterFn(ctx context.Context, m *meter.Meter, filter interface{}) bool {
	f, ok := filter.(*types.MeterFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && m.TenantID != tenantID {
		return false
	}

	// Apply status filter
	if f.Status != nil && m.Status != *f.Status {
		return false
	}

	// Apply event name filter
	if f.EventName != "" && m.EventName != f.EventName {
		return false
	}

	// Apply meter ids filter
	if len(f.MeterIDs) > 0 && !lo.Contains(f.MeterIDs, m.ID) {
		return false
	}

	// Apply time range filter
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && m.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && m.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// meterSortFn implements sorting logic for meters
func meterSortFn(i, j *meter.Meter) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}
