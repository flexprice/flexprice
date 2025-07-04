package testutil

import (
	"context"
	"strings"
	"time"

	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxRateStore implements taxrate.Repository
type InMemoryTaxRateStore struct {
	*InMemoryStore[*taxrate.TaxRate]
}

// NewInMemoryTaxRateStore creates a new in-memory tax rate store
func NewInMemoryTaxRateStore() *InMemoryTaxRateStore {
	return &InMemoryTaxRateStore{
		InMemoryStore: NewInMemoryStore[*taxrate.TaxRate](),
	}
}

// Helper to copy tax rate
func copyTaxRate(tr *taxrate.TaxRate) *taxrate.TaxRate {
	if tr == nil {
		return nil
	}

	// Deep copy of tax rate
	copy := &taxrate.TaxRate{
		ID:            tr.ID,
		Name:          tr.Name,
		Description:   tr.Description,
		Code:          tr.Code,
		TaxRateStatus: tr.TaxRateStatus,
		TaxRateType:   tr.TaxRateType,
		Scope:         tr.Scope,
		Currency:      tr.Currency,
		ValidFrom:     tr.ValidFrom,
		ValidTo:       tr.ValidTo,
		EnvironmentID: tr.EnvironmentID,
		BaseModel:     tr.BaseModel,
	}

	// Deep copy pointers
	if tr.PercentageValue != nil {
		copy.PercentageValue = &(*tr.PercentageValue)
	}
	if tr.FixedValue != nil {
		copy.FixedValue = &(*tr.FixedValue)
	}
	if tr.ValidFrom != nil {
		copy.ValidFrom = &(*tr.ValidFrom)
	}
	if tr.ValidTo != nil {
		copy.ValidTo = &(*tr.ValidTo)
	}

	// Deep copy metadata
	if tr.Metadata != nil {
		copy.Metadata = lo.Assign(map[string]string{}, tr.Metadata)
	}

	return copy
}

// taxRateFilterFn implements filtering logic for tax rates
func taxRateFilterFn(ctx context.Context, tr *taxrate.TaxRate, filter interface{}) bool {
	if tr == nil {
		return false
	}

	// Always filter out deleted items unless specifically requested
	if tr.Status == types.StatusDeleted {
		// Only show deleted items if specifically requested via status filter
		filter_, ok := filter.(*types.TaxRateFilter)
		if !ok || filter_.GetStatus() != string(types.StatusDeleted) {
			return false
		}
	}

	filter_, ok := filter.(*types.TaxRateFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if tr.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, tr.EnvironmentID) {
		return false
	}

	// Filter by status
	if filter_.GetStatus() != "" && string(tr.Status) != filter_.GetStatus() {
		return false
	}

	// Filter by tax rate IDs
	if len(filter_.TaxRateIDs) > 0 {
		if !lo.Contains(filter_.TaxRateIDs, tr.ID) {
			return false
		}
	}

	// Filter by code
	if filter_.Code != "" {
		if !strings.Contains(strings.ToLower(tr.Code), strings.ToLower(filter_.Code)) {
			return false
		}
	}

	// Filter by scope
	if filter_.Scope != "" && tr.Scope != filter_.Scope {
		return false
	}

	// Filter by time range
	if filter_.TimeRangeFilter != nil {
		if filter_.StartTime != nil && tr.CreatedAt.Before(*filter_.StartTime) {
			return false
		}
		if filter_.EndTime != nil && tr.CreatedAt.After(*filter_.EndTime) {
			return false
		}
	}

	return true
}

// taxRateSortFn implements sorting logic for tax rates
func taxRateSortFn(i, j *taxrate.TaxRate) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// Create creates a new tax rate
func (s *InMemoryTaxRateStore) Create(ctx context.Context, tr *taxrate.TaxRate) error {
	if tr == nil {
		return ierr.NewError("tax rate cannot be nil").
			WithHint("Tax rate data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if tr.EnvironmentID == "" {
		tr.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set timestamps
	if tr.CreatedAt.IsZero() {
		tr.CreatedAt = time.Now()
	}
	if tr.UpdatedAt.IsZero() {
		tr.UpdatedAt = time.Now()
	}

	return s.InMemoryStore.Create(ctx, tr.ID, copyTaxRate(tr))
}

// Get retrieves a tax rate by ID
func (s *InMemoryTaxRateStore) Get(ctx context.Context, id string) (*taxrate.TaxRate, error) {
	tr, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyTaxRate(tr), nil
}

// GetByCode retrieves a tax rate by code
func (s *InMemoryTaxRateStore) GetByCode(ctx context.Context, code string) (*taxrate.TaxRate, error) {
	// Create a filter function that matches by code, tenant_id, and environment_id
	filterFn := func(ctx context.Context, tr *taxrate.TaxRate, _ interface{}) bool {
		return tr.Code == code &&
			tr.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, tr.EnvironmentID) &&
			tr.Status != types.StatusDeleted
	}

	// List all tax rates with our filter
	taxRates, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax rates").
			Mark(ierr.ErrDatabase)
	}

	if len(taxRates) == 0 {
		return nil, ierr.NewError("tax rate not found").
			WithHintf("Tax rate with code %s was not found", code).
			WithReportableDetails(map[string]any{
				"code": code,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyTaxRate(taxRates[0]), nil
}

// List retrieves tax rates based on filter
func (s *InMemoryTaxRateStore) List(ctx context.Context, filter *types.TaxRateFilter) ([]*taxrate.TaxRate, error) {
	items, err := s.InMemoryStore.List(ctx, filter, taxRateFilterFn, taxRateSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(tr *taxrate.TaxRate, _ int) *taxrate.TaxRate {
		return copyTaxRate(tr)
	}), nil
}

// ListAll retrieves all tax rates based on filter without pagination
func (s *InMemoryTaxRateStore) ListAll(ctx context.Context, filter *types.TaxRateFilter) ([]*taxrate.TaxRate, error) {
	if filter == nil {
		filter = types.NewNoLimitTaxRateFilter()
	} else {
		f := *filter
		f.QueryFilter = types.NewNoLimitQueryFilter()
		filter = &f
	}

	return s.List(ctx, filter)
}

// Count returns the number of tax rates matching the filter
func (s *InMemoryTaxRateStore) Count(ctx context.Context, filter *types.TaxRateFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, taxRateFilterFn)
}

// Update updates a tax rate
func (s *InMemoryTaxRateStore) Update(ctx context.Context, tr *taxrate.TaxRate) error {
	if tr == nil {
		return ierr.NewError("tax rate cannot be nil").
			WithHint("Tax rate data is required").
			Mark(ierr.ErrValidation)
	}

	// Set updated timestamp
	tr.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, tr.ID, copyTaxRate(tr))
}

// Delete deletes a tax rate (soft delete by setting status to deleted)
func (s *InMemoryTaxRateStore) Delete(ctx context.Context, tr *taxrate.TaxRate) error {
	if tr == nil {
		return ierr.NewError("tax rate cannot be nil").
			WithHint("Tax rate data is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing tax rate
	existing, err := s.Get(ctx, tr.ID)
	if err != nil {
		return err
	}

	// Mark as deleted
	existing.Status = types.StatusDeleted
	existing.UpdatedAt = time.Now()

	return s.InMemoryStore.Update(ctx, existing.ID, existing)
}

// Clear clears the tax rate store
func (s *InMemoryTaxRateStore) Clear() {
	s.InMemoryStore.Clear()
}
