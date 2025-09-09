// Package proration provides functionality for handling subscription proration.
package proration

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// Service defines the operations for handling proration.
type Service interface {
	// CalculateProration calculates the proration credits and charges for a given change.
	// It does not persist anything or modify the subscription/invoice directly.
	CalculateProration(ctx context.Context, params ProrationParams) (*ProrationResult, error)

	// CreateProrationParamsForLineItem creates the proration parameters for a given line item.
	CreateProrationParamsForLineItem(
		subscription *subscription.Subscription,
		item *subscription.SubscriptionLineItem,
		price *price.Price,
		action types.ProrationAction,
		behavior types.ProrationBehavior,
	) (ProrationParams, error)

	// CreateProrationParamsForLineItemCancellation creates proration parameters for cancellation scenarios
	CreateProrationParamsForLineItemCancellation(
		ctx context.Context,
		subscription *subscription.Subscription,
		item *subscription.SubscriptionLineItem,
		price *price.Price,
		cancellationDate time.Time,
		cancellationType types.CancellationType,
		cancellationReason string,
		behavior types.ProrationBehavior,
	) (ProrationParams, error)

	// CalculateSubscriptionProration handles proration for an entire subscription.
	// This is used when creating or modifying a subscription that needs proration
	// (e.g., calendar billing with proration enabled).
	// It will calculate and apply proration for all applicable line items in a single transaction.
	CalculateSubscriptionProration(ctx context.Context, params SubscriptionProrationParams) (*SubscriptionProrationResult, error)

	// CalculateSubscriptionCancellationProration handles proration calculation for subscription cancellation.
	// This provides a single, unified function for calculating all proration changes during cancellation.
	CalculateSubscriptionCancellationProration(
		ctx context.Context,
		subscription *subscription.Subscription,
		lineItems []*subscription.SubscriptionLineItem,
		cancellationType types.CancellationType,
		effectiveDate time.Time,
		reason string,
		behavior types.ProrationBehavior,
	) (*SubscriptionProrationResult, error)
}

// Calculator performs proration calculations.
// It's kept separate from the service to allow different calculation strategies or easier testing.
type Calculator interface {
	Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error)
}
