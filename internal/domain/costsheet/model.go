/*
Package costsheet provides domain models and operations for managing costsheets in the FlexPrice system.
Costsheets are used to track the relationship between meters (usage tracking) and prices (cost calculation)
for different tenants and environments.
*/
package costsheet

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Costsheet represents the domain model for tracking meter-price relationships.
// It maps usage metrics (meters) to their associated costs (prices) and includes
// metadata for tenant and environment context.
type Costsheet struct {
	// ID uniquely identifies this costsheet record
	ID string `json:"id"`

	// MeterID references the meter used to track usage
	MeterID string `json:"meter_id"`

	// PriceID references the price configuration for cost calculation
	PriceID string `json:"price_id"`

	// Embed BaseModel for common fields (tenant_id, status, timestamps, etc.)
	types.BaseModel
}

// Filter defines comprehensive query parameters for searching and filtering costsheets.
// It leverages common filter types from the project for consistency and reusability.
type Filter struct {
	// QueryFilter contains pagination and basic query parameters
	QueryFilter *types.QueryFilter

	// TimeRangeFilter allows filtering by time periods
	TimeRangeFilter *types.TimeRangeFilter

	// Filters contains custom filtering conditions
	Filters []*types.FilterCondition

	// Sort specifies result ordering preferences
	Sort []*types.SortCondition

	// CostsheetIDs allows filtering by specific costsheet IDs
	CostsheetIDs []string

	// MeterIDs filters by specific meter IDs
	MeterIDs []string

	// PriceIDs filters by specific price IDs
	PriceIDs []string

	// Status filters by costsheet status
	Status types.Status

	// TenantID filters by specific tenant ID
	TenantID string

	// EnvironmentID filters by specific environment ID
	EnvironmentID string
}

// GetLimit implements BaseFilter interface
func (f *Filter) GetLimit() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *Filter) GetOffset() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *Filter) GetSort() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *Filter) GetOrder() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *Filter) GetStatus() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *Filter) GetExpand() types.Expand {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query
func (f *Filter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// Validate validates the filter
func (f *Filter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// New creates a new Costsheet instance with the provided meter and price IDs.
// It automatically sets up the base model fields using context information.
func New(ctx context.Context, meterID, priceID string) *Costsheet {
	return &Costsheet{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COSTSHEET),
		MeterID:   meterID,
		PriceID:   priceID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

// Validate checks if the costsheet data is valid.
// This includes checking required fields and valid status values.
func (c *Costsheet) Validate() error {
	if c.MeterID == "" {
		return ierr.NewError("meter_id is required").
			WithHint("Meter ID is required").
			Mark(ierr.ErrValidation)
	}
	if c.PriceID == "" {
		return ierr.NewError("price_id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation)
	}

	// Validate status
	validStatuses := []types.Status{
		types.StatusPublished,
		types.StatusArchived,
		types.StatusDeleted,
	}
	isValidStatus := false
	for _, status := range validStatuses {
		if c.Status == status {
			isValidStatus = true
			break
		}
	}
	if !isValidStatus {
		return ierr.NewError("invalid status").
			WithHint("Status must be one of: published, archived, deleted").
			WithReportableDetails(map[string]any{
				"status":         c.Status,
				"valid_statuses": validStatuses,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}
