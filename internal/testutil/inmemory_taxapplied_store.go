package testutil

import (
	"context"

	taxapplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxAppliedStore implements taxapplied.Repository
type InMemoryTaxAppliedStore struct {
	*InMemoryStore[*taxapplied.TaxApplied]
}

func NewInMemoryTaxAppliedStore() *InMemoryTaxAppliedStore {
	return &InMemoryTaxAppliedStore{
		InMemoryStore: NewInMemoryStore[*taxapplied.TaxApplied](),
	}
}

// taxAppliedFilterFn implements filtering logic for tax applied records
func taxAppliedFilterFn(ctx context.Context, ta *taxapplied.TaxApplied, filter interface{}) bool {
	if ta == nil {
		return false
	}

	f, ok := filter.(*types.TaxAppliedFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if ta.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, ta.EnvironmentID) {
		return false
	}

	// Filter by tax rate IDs
	if len(f.TaxRateIDs) > 0 {
		if !lo.Contains(f.TaxRateIDs, ta.TaxRateID) {
			return false
		}
	}

	// Filter by entity type
	if f.EntityType != "" && ta.EntityType != f.EntityType {
		return false
	}

	// Filter by entity ID
	if f.EntityID != "" && ta.EntityID != f.EntityID {
		return false
	}

	// Filter by tax association ID
	if f.TaxAssociationID != "" && (ta.TaxAssociationID == nil || *ta.TaxAssociationID != f.TaxAssociationID) {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && ta.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && ta.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// taxAppliedSortFn implements sorting logic for tax applied records
func taxAppliedSortFn(i, j *taxapplied.TaxApplied) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryTaxAppliedStore) Create(ctx context.Context, ta *taxapplied.TaxApplied) error {
	if ta == nil {
		return ierr.NewError("tax applied cannot be nil").
			WithHint("Tax applied data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if ta.EnvironmentID == "" {
		ta.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, ta.ID, ta)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A tax applied record with this identifier already exists").
				WithReportableDetails(map[string]any{
					"tax_applied_id": ta.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create tax applied record").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxAppliedStore) Get(ctx context.Context, id string) (*taxapplied.TaxApplied, error) {
	ta, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Tax applied record with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_applied_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax applied record").
			Mark(ierr.ErrDatabase)
	}
	return ta, nil
}

func (s *InMemoryTaxAppliedStore) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*taxapplied.TaxApplied, error) {
	// Create a filter to find by idempotency key
	filter := &types.TaxAppliedFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	taxAppliedRecords, err := s.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax applied record by idempotency key").
			Mark(ierr.ErrDatabase)
	}

	// Find the record with matching idempotency key
	for _, record := range taxAppliedRecords {
		if record.IdempotencyKey != nil && *record.IdempotencyKey == idempotencyKey {
			return record, nil
		}
	}

	return nil, ierr.NewError("tax applied record not found").
		WithHintf("Tax applied record with idempotency key %s was not found", idempotencyKey).
		WithReportableDetails(map[string]any{
			"idempotency_key": idempotencyKey,
		}).
		Mark(ierr.ErrNotFound)
}

func (s *InMemoryTaxAppliedStore) List(ctx context.Context, filter *types.TaxAppliedFilter) ([]*taxapplied.TaxApplied, error) {
	taxAppliedRecords, err := s.InMemoryStore.List(ctx, filter, taxAppliedFilterFn, taxAppliedSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax applied records").
			Mark(ierr.ErrDatabase)
	}
	return taxAppliedRecords, nil
}

func (s *InMemoryTaxAppliedStore) Count(ctx context.Context, filter *types.TaxAppliedFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, taxAppliedFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax applied records").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryTaxAppliedStore) Update(ctx context.Context, ta *taxapplied.TaxApplied) error {
	if ta == nil {
		return ierr.NewError("tax applied cannot be nil").
			WithHint("Tax applied data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, ta.ID, ta)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax applied record with ID %s was not found", ta.ID).
				WithReportableDetails(map[string]any{
					"tax_applied_id": ta.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax applied record").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxAppliedStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax applied record with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_applied_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tax applied record").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// Clear clears the tax applied store
func (s *InMemoryTaxAppliedStore) Clear() {
	s.InMemoryStore.Clear()
}
