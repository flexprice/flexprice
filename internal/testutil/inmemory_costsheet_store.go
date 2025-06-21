package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryCostSheetStore implements costsheet.Repository
type InMemoryCostSheetStore struct {
	*InMemoryStore[*costsheet.Costsheet]
}

// NewInMemoryCostSheetStore creates a new in-memory costsheet store
func NewInMemoryCostSheetStore() *InMemoryCostSheetStore {
	return &InMemoryCostSheetStore{
		InMemoryStore: NewInMemoryStore[*costsheet.Costsheet](),
	}
}

// costsheetFilterFn implements filtering logic for costsheets
func costsheetFilterFn(ctx context.Context, cs *costsheet.Costsheet, filter interface{}) bool {
	if cs == nil {
		return false
	}

	f, ok := filter.(*costsheet.Filter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if cs.BaseModel.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter - check if costsheet has environment ID field
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		// For now, we'll skip environment filtering since BaseModel doesn't have EnvironmentID
		// This would need to be added to the BaseModel or handled differently
	}

	// Filter by costsheet IDs
	if len(f.CostsheetIDs) > 0 {
		found := false
		for _, id := range f.CostsheetIDs {
			if cs.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by meter IDs
	if len(f.MeterIDs) > 0 {
		found := false
		for _, id := range f.MeterIDs {
			if cs.MeterID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by price IDs
	if len(f.PriceIDs) > 0 {
		found := false
		for _, id := range f.PriceIDs {
			if cs.PriceID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by status
	if f.Status != "" && cs.BaseModel.Status != types.Status(f.Status) {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil && cs.BaseModel.CreatedAt.Before(*f.TimeRangeFilter.StartTime) {
			return false
		}
		if f.TimeRangeFilter.EndTime != nil && cs.BaseModel.CreatedAt.After(*f.TimeRangeFilter.EndTime) {
			return false
		}
	}

	return true
}

// costsheetSortFn implements sorting logic for costsheets
func costsheetSortFn(i, j *costsheet.Costsheet) bool {
	if i == nil || j == nil {
		return false
	}
	return i.BaseModel.CreatedAt.After(j.BaseModel.CreatedAt)
}

func (s *InMemoryCostSheetStore) Create(ctx context.Context, cs *costsheet.Costsheet) error {
	if cs == nil {
		return ierr.NewError("costsheet cannot be nil").
			WithHint("Costsheet data is required").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Create(ctx, cs.ID, cs)
}

func (s *InMemoryCostSheetStore) Get(ctx context.Context, id string) (*costsheet.Costsheet, error) {
	cs, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Costsheet not found").
				WithReportableDetails(map[string]interface{}{
					"id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return cs, nil
}

func (s *InMemoryCostSheetStore) Update(ctx context.Context, cs *costsheet.Costsheet) error {
	if cs == nil {
		return ierr.NewError("costsheet cannot be nil").
			WithHint("Costsheet data is required").
			Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, cs.ID, cs)
}

func (s *InMemoryCostSheetStore) Delete(ctx context.Context, id string) error {
	// First get the current costsheet
	cs, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Check if the costsheet is published
	if cs.Status != types.StatusPublished {
		return ierr.NewError("costsheet must be published to archive").
			WithHint("Only published costsheets can be archived").
			WithReportableDetails(map[string]any{
				"costsheet_id": id,
				"status":       cs.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Perform soft delete by updating status to archived
	cs.Status = types.StatusArchived
	return s.Update(ctx, cs)
}

func (s *InMemoryCostSheetStore) List(ctx context.Context, filter *costsheet.Filter) ([]*costsheet.Costsheet, error) {
	return s.InMemoryStore.List(ctx, filter, costsheetFilterFn, costsheetSortFn)
}

func (s *InMemoryCostSheetStore) Count(ctx context.Context, filter *costsheet.Filter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, costsheetFilterFn)
}

func (s *InMemoryCostSheetStore) GetByMeterAndPrice(ctx context.Context, meterID, priceID string) (*costsheet.Costsheet, error) {
	filter := &costsheet.Filter{
		MeterIDs: []string{meterID},
		PriceIDs: []string{priceID},
		Status:   types.StatusPublished,
	}

	costsheets, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(costsheets) == 0 {
		return nil, ierr.NewError("costsheet not found").
			WithHint("No published costsheet found for the given meter and price").
			WithReportableDetails(map[string]interface{}{
				"meter_id": meterID,
				"price_id": priceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return costsheets[0], nil
}
