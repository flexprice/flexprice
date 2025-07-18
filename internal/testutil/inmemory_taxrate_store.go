package testutil

import (
	"context"

	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryTaxRateStore implements taxrate.Repository
type InMemoryTaxRateStore struct {
	*InMemoryStore[*taxrate.TaxRate]
}

func NewInMemoryTaxRateStore() *InMemoryTaxRateStore {
	return &InMemoryTaxRateStore{
		InMemoryStore: NewInMemoryStore[*taxrate.TaxRate](),
	}
}

// taxRateFilterFn implements filtering logic for tax rates
func taxRateFilterFn(ctx context.Context, tr *taxrate.TaxRate, filter interface{}) bool {
	if tr == nil {
		return false
	}

	f, ok := filter.(*types.TaxRateFilter)
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

	// Filter by tax rate IDs
	if len(f.TaxRateIDs) > 0 {
		if !lo.Contains(f.TaxRateIDs, tr.ID) {
			return false
		}
	}

	// Filter by tax rate codes
	if len(f.TaxRateCodes) > 0 {
		if !lo.Contains(f.TaxRateCodes, tr.Code) {
			return false
		}
	}

	// Filter by scope
	if f.Scope != "" && tr.Scope != f.Scope {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && tr.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && tr.CreatedAt.After(*f.EndTime) {
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

	err := s.InMemoryStore.Create(ctx, tr.ID, tr)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A tax rate with this identifier already exists").
				WithReportableDetails(map[string]any{
					"tax_rate_id": tr.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create tax rate").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxRateStore) Get(ctx context.Context, id string) (*taxrate.TaxRate, error) {
	tr, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Tax rate with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_rate_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax rate").
			Mark(ierr.ErrDatabase)
	}
	return tr, nil
}

func (s *InMemoryTaxRateStore) List(ctx context.Context, filter *types.TaxRateFilter) ([]*taxrate.TaxRate, error) {
	taxRates, err := s.InMemoryStore.List(ctx, filter, taxRateFilterFn, taxRateSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax rates").
			Mark(ierr.ErrDatabase)
	}
	return taxRates, nil
}

func (s *InMemoryTaxRateStore) ListAll(ctx context.Context, filter *types.TaxRateFilter) ([]*taxrate.TaxRate, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.TaxRateFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
		TaxRateIDs:      filter.TaxRateIDs,
		TaxRateCodes:    filter.TaxRateCodes,
		Scope:           filter.Scope,
	}

	return s.List(ctx, unlimitedFilter)
}

func (s *InMemoryTaxRateStore) Count(ctx context.Context, filter *types.TaxRateFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, taxRateFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax rates").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryTaxRateStore) Update(ctx context.Context, tr *taxrate.TaxRate) error {
	if tr == nil {
		return ierr.NewError("tax rate cannot be nil").
			WithHint("Tax rate data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, tr.ID, tr)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax rate with ID %s was not found", tr.ID).
				WithReportableDetails(map[string]any{
					"tax_rate_id": tr.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax rate").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxRateStore) Delete(ctx context.Context, tr *taxrate.TaxRate) error {
	if tr == nil {
		return ierr.NewError("tax rate cannot be nil").
			WithHint("Tax rate data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Delete(ctx, tr.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax rate with ID %s was not found", tr.ID).
				WithReportableDetails(map[string]any{
					"tax_rate_id": tr.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tax rate").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryTaxRateStore) GetByCode(ctx context.Context, code string) (*taxrate.TaxRate, error) {
	// Create a filter to find by code
	filter := &types.TaxRateFilter{
		QueryFilter:  types.NewNoLimitQueryFilter(),
		TaxRateCodes: []string{code},
	}

	// Apply status filter
	filter.Status = lo.ToPtr(types.StatusPublished)

	taxRates, err := s.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax rate by code").
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

	return taxRates[0], nil
}

// Clear clears the tax rate store
func (s *InMemoryTaxRateStore) Clear() {
	s.InMemoryStore.Clear()
}
