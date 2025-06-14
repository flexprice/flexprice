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
	Status types.CostsheetStatus

	// TenantID filters by specific tenant ID
	TenantID string

	// EnvironmentID filters by specific environment ID
	EnvironmentID string
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
	return nil
}

func GetTenantAndEnvFromContext(ctx context.Context) (string, string) {
	return types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
}
