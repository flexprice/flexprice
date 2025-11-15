package testutil

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryPriceUnitStore implements priceunit.Repository
type InMemoryPriceUnitStore struct {
	*InMemoryStore[*priceunit.PriceUnit]
}

// NewInMemoryPriceUnitStore creates a new in-memory price unit store
func NewInMemoryPriceUnitStore() *InMemoryPriceUnitStore {
	return &InMemoryPriceUnitStore{
		InMemoryStore: NewInMemoryStore[*priceunit.PriceUnit](),
	}
}

// Helper to copy price unit
func copyPriceUnit(pu *priceunit.PriceUnit) *priceunit.PriceUnit {
	if pu == nil {
		return nil
	}

	// Deep copy of price unit
	copied := &priceunit.PriceUnit{
		ID:             pu.ID,
		Name:           pu.Name,
		Code:           pu.Code,
		Symbol:         pu.Symbol,
		BaseCurrency:   pu.BaseCurrency,
		ConversionRate: pu.ConversionRate,
		Precision:      pu.Precision,
		EnvironmentID:  pu.EnvironmentID,
		Metadata:       lo.Assign(map[string]string{}, pu.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  pu.TenantID,
			Status:    pu.Status,
			CreatedAt: pu.CreatedAt,
			UpdatedAt: pu.UpdatedAt,
			CreatedBy: pu.CreatedBy,
			UpdatedBy: pu.UpdatedBy,
		},
	}

	return copied
}

func (s *InMemoryPriceUnitStore) Create(ctx context.Context, pu *priceunit.PriceUnit) (*priceunit.PriceUnit, error) {
	if pu == nil {
		return nil, ierr.NewError("price unit cannot be nil").
			WithHint("Price unit cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if pu.EnvironmentID == "" {
		pu.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, pu.ID, copyPriceUnit(pu))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create price unit").
			WithReportableDetails(map[string]interface{}{
				"id":   pu.ID,
				"code": pu.Code,
			}).
			Mark(ierr.ErrDatabase)
	}
	return copyPriceUnit(pu), nil
}

func (s *InMemoryPriceUnitStore) Get(ctx context.Context, id string) (*priceunit.PriceUnit, error) {
	pu, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("price unit not found").
			WithHint("Price unit not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyPriceUnit(pu), nil
}

func (s *InMemoryPriceUnitStore) List(ctx context.Context, filter *types.PriceUnitFilter) ([]*priceunit.PriceUnit, error) {
	if filter == nil {
		filter = types.NewPriceUnitFilter()
	}

	priceUnits, err := s.InMemoryStore.List(ctx, filter, priceUnitFilterFn, priceUnitSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list price units").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}
	return priceUnits, nil
}

func (s *InMemoryPriceUnitStore) Update(ctx context.Context, pu *priceunit.PriceUnit) (*priceunit.PriceUnit, error) {
	if pu == nil {
		return nil, ierr.NewError("price unit cannot be nil").
			WithHint("Price unit cannot be nil").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, pu.ID, copyPriceUnit(pu))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update price unit").
			WithReportableDetails(map[string]interface{}{
				"id": pu.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return copyPriceUnit(pu), nil
}

func (s *InMemoryPriceUnitStore) Delete(ctx context.Context, pu *priceunit.PriceUnit) error {
	if pu == nil {
		return ierr.NewError("price unit cannot be nil").
			WithHint("Price unit cannot be nil").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Delete(ctx, pu.ID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete price unit").
			WithReportableDetails(map[string]interface{}{
				"id": pu.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceUnitStore) GetByCode(ctx context.Context, code string) (*priceunit.PriceUnit, error) {
	if code == "" {
		return nil, ierr.NewError("code cannot be empty").
			WithHint("Code cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Create a filter to find by code
	filter := &types.PriceUnitFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("code"),
				Operator: lo.ToPtr(types.EQUAL),
				DataType: lo.ToPtr(types.DataTypeString),
				Value: &types.Value{
					String: &code,
				},
			},
		},
	}

	priceUnits, err := s.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to find price unit by code").
			WithReportableDetails(map[string]interface{}{
				"code": code,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(priceUnits) == 0 {
		return nil, ierr.NewError("price unit not found").
			WithHint("Price unit not found").
			WithReportableDetails(map[string]interface{}{
				"code": code,
			}).
			Mark(ierr.ErrNotFound)
	}

	return priceUnits[0], nil
}

// priceUnitFilterFn implements filtering logic for price units
func priceUnitFilterFn(ctx context.Context, pu *priceunit.PriceUnit, filter interface{}) bool {
	if pu == nil {
		return false
	}

	f, ok := filter.(*types.PriceUnitFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if pu.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, pu.EnvironmentID) {
		return false
	}

	// Filter by price unit IDs
	if len(f.PriceUnitIDs) > 0 {
		if !lo.Contains(f.PriceUnitIDs, pu.ID) {
			return false
		}
	}

	// Apply filter conditions
	for _, condition := range f.Filters {
		if !applyPriceUnitFilterCondition(pu, condition) {
			return false
		}
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && pu.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && pu.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

func applyPriceUnitFilterCondition(pu *priceunit.PriceUnit, condition *types.FilterCondition) bool {
	if condition.Field == nil {
		return true
	}

	switch *condition.Field {
	case "id":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(pu.ID), strings.ToLower(*condition.Value.String))
		}
	case "name":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(pu.Name), strings.ToLower(*condition.Value.String))
		}
	case "code":
		if condition.Value != nil && condition.Value.String != nil {
			return pu.Code == *condition.Value.String
		}
	case "symbol":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(pu.Symbol), strings.ToLower(*condition.Value.String))
		}
	case "base_currency":
		if condition.Value != nil && condition.Value.String != nil {
			return pu.BaseCurrency == *condition.Value.String
		}
	case "precision":
		if condition.Value != nil && condition.Value.Number != nil {
			return pu.Precision == int(*condition.Value.Number)
		}
	case "environment_id":
		if condition.Value != nil && condition.Value.String != nil {
			return pu.EnvironmentID == *condition.Value.String
		}
	case "status":
		if condition.Value != nil && condition.Value.String != nil {
			return string(pu.Status) == *condition.Value.String
		}
	default:
		return true
	}

	return true
}

// priceUnitSortFn implements sorting logic for price units
func priceUnitSortFn(i, j *priceunit.PriceUnit) bool {
	if i == nil || j == nil {
		return false
	}
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}

// Clear clears the price unit store
func (s *InMemoryPriceUnitStore) Clear() {
	s.InMemoryStore.Clear()
}
