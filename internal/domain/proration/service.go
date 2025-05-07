package proration

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Service defines the operations for handling proration.
type Service interface {
	// CalculateProration calculates the proration credits and charges for a given change.
	// It does not persist anything or modify the subscription/invoice directly.
	CalculateProration(ctx context.Context, params ProrationParams) (*ProrationResult, error)

	// ApplyProration takes a ProrationResult and applies it based on the ProrationBehavior.
	// For ProrationBehaviorCreateInvoiceItems, this typically means creating invoice line items
	// (or potentially credit notes) via the Invoice service/repository.
	ApplyProration(ctx context.Context,
		result *ProrationResult,
		behavior types.ProrationBehavior,
		tenantID string,
		environmentID string,
		subscriptionID string,
	) error
}

// Calculator performs proration calculations.
// It's kept separate from the service to allow different calculation strategies or easier testing.
type Calculator interface {
	Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error)
}
