package testutil

import (
	"context"
	"time"

	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxAssociationStore implements taxassociation.Repository
type InMemoryTaxAssociationStore struct {
	*InMemoryStore[*taxassociation.TaxAssociation]
}

// NewInMemoryTaxAssociationStore creates a new in-memory tax association store
func NewInMemoryTaxAssociationStore() *InMemoryTaxAssociationStore {
	return &InMemoryTaxAssociationStore{
		InMemoryStore: NewInMemoryStore[*taxassociation.TaxAssociation](),
	}
}

// Helper to copy tax association
func copyTaxAssociation(ta *taxassociation.TaxAssociation) *taxassociation.TaxAssociation {
	if ta == nil {
		return nil
	}

	// Deep copy of tax association
	copied := &taxassociation.TaxAssociation{
		ID:            ta.ID,
		TaxRateID:     ta.TaxRateID,
		EntityType:    ta.EntityType,
		EntityID:      ta.EntityID,
		Priority:      ta.Priority,
		AutoApply:     ta.AutoApply,
		Currency:      ta.Currency,
		EnvironmentID: ta.EnvironmentID,
		BaseModel:     ta.BaseModel,
	}

	// Deep copy metadata
	if ta.Metadata != nil {
		copied.Metadata = lo.Assign(map[string]string{}, ta.Metadata)
	}

	return copied
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

	// Filter by status
	if f.GetStatus() != "" && string(ta.Status) != f.GetStatus() {
		return false
	}

	// Filter by tax association IDs
	if len(f.TaxAssociationIDs) > 0 && !lo.Contains(f.TaxAssociationIDs, ta.ID) {
		return false
	}

	// Filter by tax rate IDs
	if len(f.TaxRateIDs) > 0 && !lo.Contains(f.TaxRateIDs, ta.TaxRateID) {
		return false
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
		if f.TimeRangeFilter.StartTime != nil && ta.CreatedAt.Before(*f.TimeRangeFilter.StartTime) {
			return false
		}
		if f.TimeRangeFilter.EndTime != nil && ta.CreatedAt.After(*f.TimeRangeFilter.EndTime) {
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

// Create creates a new tax association
func (s *InMemoryTaxAssociationStore) Create(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if ta.EnvironmentID == "" {
		ta.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set timestamps
	if ta.CreatedAt.IsZero() {
		ta.CreatedAt = time.Now()
	}
	if ta.UpdatedAt.IsZero() {
		ta.UpdatedAt = time.Now()
	}

	return s.InMemoryStore.Create(ctx, ta.ID, copyTaxAssociation(ta))
}

// Get retrieves a tax association by ID
func (s *InMemoryTaxAssociationStore) Get(ctx context.Context, id string) (*taxassociation.TaxAssociation, error) {
	ta, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyTaxAssociation(ta), nil
}

// List retrieves tax associations based on filter
func (s *InMemoryTaxAssociationStore) List(ctx context.Context, filter *types.TaxAssociationFilter) ([]*taxassociation.TaxAssociation, error) {
	return s.InMemoryStore.List(ctx, filter, taxAssociationFilterFn, taxAssociationSortFn)
}

// Count returns the number of tax associations matching the filter
func (s *InMemoryTaxAssociationStore) Count(ctx context.Context, filter *types.TaxAssociationFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, taxAssociationFilterFn)
}

// Update updates a tax association
func (s *InMemoryTaxAssociationStore) Update(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set updated timestamp
	ta.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, ta.ID, copyTaxAssociation(ta))
}

// Delete deletes a tax association (soft delete by setting status to archived)
func (s *InMemoryTaxAssociationStore) Delete(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Soft delete by setting status to archived
	existing, err := s.InMemoryStore.Get(ctx, ta.ID)
	if err != nil {
		return err
	}

	existing.Status = types.StatusArchived
	existing.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, existing.ID, existing)
}

// Clear clears the tax association store
func (s *InMemoryTaxAssociationStore) Clear() {
	s.InMemoryStore.Clear()
}

// GetByEntityAndTaxRate retrieves tax associations by entity and tax rate
func (s *InMemoryTaxAssociationStore) GetByEntityAndTaxRate(ctx context.Context, entityType types.TaxrateEntityType, entityID string, taxRateID string) (*taxassociation.TaxAssociation, error) {
	// Create a filter function that matches by entity type, entity ID, and tax rate ID
	filterFn := func(ctx context.Context, ta *taxassociation.TaxAssociation, _ interface{}) bool {
		return ta.EntityType == entityType &&
			ta.EntityID == entityID &&
			ta.TaxRateID == taxRateID &&
			ta.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ta.EnvironmentID) &&
			ta.Status != types.StatusDeleted
	}

	// List all tax associations with our filter
	taxAssociations, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax associations").
			Mark(ierr.ErrDatabase)
	}

	if len(taxAssociations) == 0 {
		return nil, ierr.NewError("tax association not found").
			WithHintf("Tax association for entity %s:%s and tax rate %s was not found", entityType, entityID, taxRateID).
			WithReportableDetails(map[string]any{
				"entity_type": entityType,
				"entity_id":   entityID,
				"tax_rate_id": taxRateID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyTaxAssociation(taxAssociations[0]), nil
}

// GetByEntity retrieves tax associations by entity (with optional auto-apply filter)
func (s *InMemoryTaxAssociationStore) GetByEntity(ctx context.Context, entityType types.TaxrateEntityType, entityID string, autoApplyOnly bool) ([]*taxassociation.TaxAssociation, error) {
	// Create a filter function that matches by entity type and entity ID
	filterFn := func(ctx context.Context, ta *taxassociation.TaxAssociation, _ interface{}) bool {
		if ta.EntityType != entityType ||
			ta.EntityID != entityID ||
			ta.TenantID != types.GetTenantID(ctx) ||
			!CheckEnvironmentFilter(ctx, ta.EnvironmentID) ||
			ta.Status == types.StatusDeleted {
			return false
		}

		// Filter by auto-apply if specified
		if autoApplyOnly && !ta.AutoApply {
			return false
		}

		return true
	}

	// List all tax associations with our filter
	taxAssociations, err := s.InMemoryStore.List(ctx, nil, filterFn, taxAssociationSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax associations").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(taxAssociations, func(ta *taxassociation.TaxAssociation, _ int) *taxassociation.TaxAssociation {
		return copyTaxAssociation(ta)
	}), nil
}
