package testutil

import (
	"context"

	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxAssociationStore implements taxassociation.Repository
type InMemoryTaxAssociationStore struct {
	*InMemoryStore[*taxassociation.TaxAssociation]
}

func NewInMemoryTaxAssociationStore() *InMemoryTaxAssociationStore {
	return &InMemoryTaxAssociationStore{
		InMemoryStore: NewInMemoryStore[*taxassociation.TaxAssociation](),
	}
}

// taxAssociationFilterFn implements filtering logic for tax associations
func taxAssociationFilterFn(ctx context.Context, ta *taxassociation.TaxAssociation, filter interface{}) bool {
	if ta == nil {
		return false
	}

	f, ok := filter.(*types.TaxAssociationFilter)
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

	// Filter by tax association IDs
	if len(f.TaxAssociationIDs) > 0 {
		if !lo.Contains(f.TaxAssociationIDs, ta.ID) {
			return false
		}
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

	// Filter by currency
	if f.Currency != "" && ta.Currency != f.Currency {
		return false
	}

	// Filter by auto apply
	if f.AutoApply != nil && ta.AutoApply != *f.AutoApply {
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

// taxAssociationSortFn implements sorting logic for tax associations
func taxAssociationSortFn(i, j *taxassociation.TaxAssociation) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryTaxAssociationStore) Create(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association data is required").
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
				WithHint("A tax association with this identifier already exists").
				WithReportableDetails(map[string]any{
					"tax_association_id": ta.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create tax association").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxAssociationStore) Get(ctx context.Context, id string) (*taxassociation.TaxAssociation, error) {
	ta, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Tax association with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax association").
			Mark(ierr.ErrDatabase)
	}
	return ta, nil
}

func (s *InMemoryTaxAssociationStore) List(ctx context.Context, filter *types.TaxAssociationFilter) ([]*taxassociation.TaxAssociation, error) {
	taxAssociations, err := s.InMemoryStore.List(ctx, filter, taxAssociationFilterFn, taxAssociationSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax associations").
			Mark(ierr.ErrDatabase)
	}
	return taxAssociations, nil
}

func (s *InMemoryTaxAssociationStore) Count(ctx context.Context, filter *types.TaxAssociationFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, taxAssociationFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax associations").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryTaxAssociationStore) Update(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, ta.ID, ta)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax association with ID %s was not found", ta.ID).
				WithReportableDetails(map[string]any{
					"tax_association_id": ta.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax association").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxAssociationStore) Delete(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Delete(ctx, ta.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax association with ID %s was not found", ta.ID).
				WithReportableDetails(map[string]any{
					"tax_association_id": ta.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tax association").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// Clear clears the tax association store
func (s *InMemoryTaxAssociationStore) Clear() {
	s.InMemoryStore.Clear()
}
