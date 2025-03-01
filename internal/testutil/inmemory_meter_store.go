package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
		ID:            m.ID,
		Name:          m.Name,
		EventName:     m.EventName,
		EnvironmentID: m.EnvironmentID,
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
	// Set environment ID from context if not already set
	if m.EnvironmentID == "" {
		m.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, m.ID, copyMeter(m))
	if err != nil {
		if IsAlreadyExistsError(err) {
			return ierr.WithError(err).
				WithMessage("meter already exists").
				WithHint("Meter already exists").
				WithReportableDetails(map[string]any{
					"meter_id": m.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithMessage("failed to create meter").
			WithHint("Failed to create meter").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryMeterStore) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	m, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, ierr.WithError(err).
				WithMessage("meter not found").
				WithHint("Meter not found").
				WithReportableDetails(map[string]any{
					"meter_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to get meter").
			WithHint("Failed to retrieve meter").
			Mark(ierr.ErrDatabase)
	}
	return m, nil
}

func (s *InMemoryMeterStore) List(ctx context.Context, filter *types.MeterFilter) ([]*meter.Meter, error) {
	meters, err := s.InMemoryStore.List(ctx, filter, meterFilterFn, meterSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to list meters").
			WithHint("Failed to retrieve meters").
			Mark(ierr.ErrDatabase)
	}
	return meters, nil
}

func (s *InMemoryMeterStore) ListAll(ctx context.Context, filter *types.MeterFilter) ([]*meter.Meter, error) {
	f := *filter
	f.QueryFilter = types.NewNoLimitQueryFilter()
	return s.List(ctx, &f)
}

func (s *InMemoryMeterStore) Count(ctx context.Context, filter *types.MeterFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, meterFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithMessage("failed to count meters").
			WithHint("Failed to count meters").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryMeterStore) DisableMeter(ctx context.Context, id string) error {
	m, err := s.GetMeter(ctx, id)
	if err != nil {
		return err
	}

	m.Status = types.StatusDeleted
	err = s.InMemoryStore.Update(ctx, m.ID, m)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to disable meter").
			WithHint("Failed to disable meter").
			WithReportableDetails(map[string]any{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
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
	err = s.InMemoryStore.Update(ctx, m.ID, m)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to update meter").
			WithHint("Failed to update meter").
			WithReportableDetails(map[string]any{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
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

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, m.EnvironmentID) {
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
