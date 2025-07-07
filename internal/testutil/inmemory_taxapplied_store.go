package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxAppliedStore implements taxapplied.Repository
type InMemoryTaxAppliedStore struct {
	*InMemoryStore[*taxapplied.TaxApplied]
}

// NewInMemoryTaxAppliedStore creates a new in-memory tax applied store
func NewInMemoryTaxAppliedStore() *InMemoryTaxAppliedStore {
	return &InMemoryTaxAppliedStore{
		InMemoryStore: NewInMemoryStore[*taxapplied.TaxApplied](),
	}
}

// Helper to copy tax applied
func copyTaxApplied(ta *taxapplied.TaxApplied) *taxapplied.TaxApplied {
	if ta == nil {
		return nil
	}

	// Deep copy of tax applied
	copied := &taxapplied.TaxApplied{
		ID:            ta.ID,
		TaxRateID:     ta.TaxRateID,
		EntityType:    ta.EntityType,
		EntityID:      ta.EntityID,
		TaxableAmount: ta.TaxableAmount,
		TaxAmount:     ta.TaxAmount,
		Currency:      ta.Currency,
		AppliedAt:     ta.AppliedAt,
		EnvironmentID: ta.EnvironmentID,
		BaseModel:     ta.BaseModel,
	}

	// Deep copy pointers
	if ta.TaxAssociationID != nil {
		taxAssociationID := *ta.TaxAssociationID
		copied.TaxAssociationID = &taxAssociationID
	}
	if ta.IdempotencyKey != nil {
		idempotencyKey := *ta.IdempotencyKey
		copied.IdempotencyKey = &idempotencyKey
	}

	// Deep copy metadata
	if ta.Metadata != nil {
		copied.Metadata = lo.Assign(map[string]string{}, ta.Metadata)
	}

	return copied
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

	// Filter by status
	if f.GetStatus() != "" && string(ta.Status) != f.GetStatus() {
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

	// Filter by tax association ID
	if f.TaxAssociationID != "" && (ta.TaxAssociationID == nil || *ta.TaxAssociationID != f.TaxAssociationID) {
		return false
	}

	// Apply custom filters
	if f.Filters != nil {
		// This would need to be implemented based on the DSL filter system
		// For now, we'll skip custom filters in the in-memory implementation
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
			WithHint("Tax applied cannot be nil").
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

	return s.InMemoryStore.Create(ctx, ta.ID, copyTaxApplied(ta))
}

func (s *InMemoryTaxAppliedStore) Get(ctx context.Context, id string) (*taxapplied.TaxApplied, error) {
	ta, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyTaxApplied(ta), nil
}

func (s *InMemoryTaxAppliedStore) List(ctx context.Context, filter *types.TaxAppliedFilter) ([]*taxapplied.TaxApplied, error) {
	return s.InMemoryStore.List(ctx, filter, taxAppliedFilterFn, taxAppliedSortFn)
}

func (s *InMemoryTaxAppliedStore) Count(ctx context.Context, filter *types.TaxAppliedFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, taxAppliedFilterFn)
}

func (s *InMemoryTaxAppliedStore) Update(ctx context.Context, ta *taxapplied.TaxApplied) error {
	if ta == nil {
		return ierr.NewError("tax applied cannot be nil").
			WithHint("Tax applied cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set updated timestamp
	ta.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, ta.ID, copyTaxApplied(ta))
}

func (s *InMemoryTaxAppliedStore) Delete(ctx context.Context, id string) error {
	// Get the existing tax applied record
	existing, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}

	// Soft delete by setting status to archived
	existing.Status = types.StatusArchived
	existing.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, existing.ID, existing)
}

// GetByIdempotencyKey retrieves a tax applied record by idempotency key
func (s *InMemoryTaxAppliedStore) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*taxapplied.TaxApplied, error) {
	// Create a filter function that matches by idempotency key
	filterFn := func(ctx context.Context, ta *taxapplied.TaxApplied, _ interface{}) bool {
		return ta.IdempotencyKey != nil &&
			*ta.IdempotencyKey == idempotencyKey &&
			ta.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ta.EnvironmentID) &&
			ta.Status != types.StatusDeleted
	}

	// List all tax applied records with our filter
	taxAppliedRecords, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax applied records").
			Mark(ierr.ErrDatabase)
	}

	if len(taxAppliedRecords) == 0 {
		return nil, ierr.NewError("tax applied record not found").
			WithHintf("Tax applied record with idempotency key %s was not found", idempotencyKey).
			WithReportableDetails(map[string]any{
				"idempotency_key": idempotencyKey,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyTaxApplied(taxAppliedRecords[0]), nil
}

// Clear removes all tax applied records from the store
func (s *InMemoryTaxAppliedStore) Clear() {
	s.InMemoryStore.Clear()
}
