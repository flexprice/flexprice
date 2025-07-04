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
	copy := &taxassociation.TaxAssociation{
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
		copy.Metadata = lo.Assign(map[string]string{}, ta.Metadata)
	}

	return copy
}

// taxAssociationFilterFn implements filtering logic for tax associations
func taxAssociationFilterFn(ctx context.Context, ta *taxassociation.TaxAssociation, filter interface{}) bool {
	if ta == nil {
		return false
	}

	// Always filter out deleted items unless specifically requested
	if ta.Status == types.StatusDeleted {
		// Only show deleted items if specifically requested via status filter
		filter_, ok := filter.(*types.TaxAssociationFilter)
		if !ok || filter_.GetStatus() != string(types.StatusDeleted) {
			return false
		}
	}

	filter_, ok := filter.(*types.TaxAssociationFilter)
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
	if filter_.GetStatus() != "" && string(ta.Status) != filter_.GetStatus() {
		return false
	}

	// Filter by tax association IDs
	if len(filter_.TaxAssociationIDs) > 0 {
		if !lo.Contains(filter_.TaxAssociationIDs, ta.ID) {
			return false
		}
	}

	// Filter by tax rate IDs
	if len(filter_.TaxRateIDs) > 0 {
		if !lo.Contains(filter_.TaxRateIDs, ta.TaxRateID) {
			return false
		}
	}

	// Filter by entity type
	if filter_.EntityType != "" && ta.EntityType != filter_.EntityType {
		return false
	}

	// Filter by entity ID
	if filter_.EntityID != "" && ta.EntityID != filter_.EntityID {
		return false
	}

	// Filter by currency
	if filter_.Currency != "" && ta.Currency != filter_.Currency {
		return false
	}

	// Filter by auto apply
	if filter_.AutoApply != nil && ta.AutoApply != *filter_.AutoApply {
		return false
	}

	// Filter by time range
	if filter_.TimeRangeFilter != nil {
		if filter_.StartTime != nil && ta.CreatedAt.Before(*filter_.StartTime) {
			return false
		}
		if filter_.EndTime != nil && ta.CreatedAt.After(*filter_.EndTime) {
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
	// Sort by priority first (lower number = higher priority), then by created date
	if i.Priority != j.Priority {
		return i.Priority < j.Priority
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// Create creates a new tax association
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

	// Check if the item is soft deleted
	if ta.Status == types.StatusDeleted {
		return nil, ierr.NewError("tax association not found").
			WithHintf("Tax association with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"tax_association_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyTaxAssociation(ta), nil
}

// List retrieves tax associations based on filter
func (s *InMemoryTaxAssociationStore) List(ctx context.Context, filter *types.TaxAssociationFilter) ([]*taxassociation.TaxAssociation, error) {
	items, err := s.InMemoryStore.List(ctx, filter, taxAssociationFilterFn, taxAssociationSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(ta *taxassociation.TaxAssociation, _ int) *taxassociation.TaxAssociation {
		return copyTaxAssociation(ta)
	}), nil
}

// Count returns the number of tax associations matching the filter
func (s *InMemoryTaxAssociationStore) Count(ctx context.Context, filter *types.TaxAssociationFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, taxAssociationFilterFn)
}

// Update updates a tax association
func (s *InMemoryTaxAssociationStore) Update(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association data is required").
			Mark(ierr.ErrValidation)
	}

	// Set updated timestamp
	ta.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, ta.ID, copyTaxAssociation(ta))
}

// Delete deletes a tax association (soft delete by setting status to deleted)
func (s *InMemoryTaxAssociationStore) Delete(ctx context.Context, ta *taxassociation.TaxAssociation) error {
	if ta == nil {
		return ierr.NewError("tax association cannot be nil").
			WithHint("Tax association data is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing tax association
	existing, err := s.Get(ctx, ta.ID)
	if err != nil {
		return err
	}

	// Mark as deleted
	existing.Status = types.StatusDeleted
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
