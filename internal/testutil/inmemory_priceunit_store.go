package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InMemoryPriceUnitStore implements priceunit.Repository
type InMemoryPriceUnitStore struct {
	*InMemoryStore[*priceunit.PriceUnit]
}

func NewInMemoryPriceUnitStore() *InMemoryPriceUnitStore {
	return &InMemoryPriceUnitStore{
		InMemoryStore: NewInMemoryStore[*priceunit.PriceUnit](),
	}
}

// priceUnitFilterFn implements filtering logic for price units
func priceUnitFilterFn(ctx context.Context, p *priceunit.PriceUnit, filter interface{}) bool {
	if p == nil {
		return false
	}

	f, ok := filter.(*priceunit.PriceUnitFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if p.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, p.EnvironmentID) {
		return false
	}

	// Apply status filter if provided
	if f.Status != "" && p.Status != f.Status {
		return false
	}

	// Apply tenant ID filter if provided
	if f.TenantID != "" && p.TenantID != f.TenantID {
		return false
	}

	// Apply environment ID filter if provided
	if f.EnvironmentID != "" && p.EnvironmentID != f.EnvironmentID {
		return false
	}

	return true
}

// priceSortFn implements sorting logic for price units
func priceUnitSortFn(i, j *priceunit.PriceUnit) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryPriceUnitStore) Create(ctx context.Context, p *priceunit.PriceUnit) error {
	if p == nil {
		return ierr.NewError("price unit cannot be nil").
			WithHint("Price unit data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, p.ID, p)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A price unit with this identifier already exists").
				WithReportableDetails(map[string]any{
					"price_unit_id": p.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create price unit").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceUnitStore) GetByID(ctx context.Context, id string) (*priceunit.PriceUnit, error) {
	p, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_unit_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get price unit").
			Mark(ierr.ErrDatabase)
	}
	return p, nil
}

func (s *InMemoryPriceUnitStore) GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*priceunit.PriceUnit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, unit := range s.items {
		if unit.Code == code && unit.TenantID == tenantID && unit.EnvironmentID == environmentID {
			if status != "" && string(unit.Status) != status {
				continue
			}
			return unit, nil
		}
	}

	return nil, ierr.NewError("price unit not found").
		WithHintf("Price unit with code %s was not found", code).
		WithReportableDetails(map[string]any{
			"code":           code,
			"tenant_id":      tenantID,
			"environment_id": environmentID,
		}).
		Mark(ierr.ErrNotFound)
}

func (s *InMemoryPriceUnitStore) List(ctx context.Context, filter *priceunit.PriceUnitFilter) ([]*priceunit.PriceUnit, error) {
	priceUnits, err := s.InMemoryStore.List(ctx, filter, priceUnitFilterFn, priceUnitSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list price units").
			Mark(ierr.ErrDatabase)
	}
	return priceUnits, nil
}

func (s *InMemoryPriceUnitStore) Count(ctx context.Context, filter *priceunit.PriceUnitFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, priceUnitFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count price units").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryPriceUnitStore) Update(ctx context.Context, p *priceunit.PriceUnit) error {
	if p == nil {
		return ierr.NewError("price unit cannot be nil").
			WithHint("Price unit data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, p.ID, p)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"price_unit_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update price unit").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceUnitStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_unit_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete price unit").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceUnitStore) ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := s.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	return unit.ConvertToBaseCurrency(priceUnitAmount), nil
}

func (s *InMemoryPriceUnitStore) ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := s.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	return unit.ConvertFromBaseCurrency(fiatAmount), nil
}
