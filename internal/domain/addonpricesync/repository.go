package addonpricesync

import (
	"context"
	"time"
)

// AddonLineItemTerminationDelta is an addon-sync delta row for setting a line item's end_date.
type AddonLineItemTerminationDelta struct {
	LineItemID     string
	SubscriptionID string
	PriceID        string
	TargetEndDate  time.Time // NOT NULL in this delta query
}

// AddonLineItemCreationDelta is an addon-sync delta row for creating a new line item.
type AddonLineItemCreationDelta struct {
	SubscriptionID string
	PriceID        string // addon price ID (entity_type=ADDON)
	CustomerID     string // subscription's customer_id, for reprocessing without listing subscriptions
}

type ListAddonLineItemsToTerminateParams struct {
	AddonID string
	Limit   int
}

type ListAddonLineItemsToCreateParams struct {
	AddonID    string
	Limit      int
	AfterSubID string // Optional cursor: subscription_id from last row
}

type TerminateExpiredAddonPricesLineItemsParams struct {
	AddonID string
	Limit   int
}

// Repository defines the interface for addon price sync delta queries.
//
// This repo is intentionally scoped to two canonical DB-driven queries:
// 1) addon-derived line items whose end_date must be set to price.end_date
// 2) missing (subscription_id, price_id) pairs where an addon-derived line item must be created
type Repository interface {
	// TerminateExpiredAddonPricesLineItems sets end_date on addon-derived line items whose price has expired.
	// If limit <= 0, an implementation-defined default is used.
	TerminateExpiredAddonPricesLineItems(
		ctx context.Context,
		p TerminateExpiredAddonPricesLineItemsParams,
	) (numTerminated int, err error)

	// ListAddonLineItemsToTerminate returns addon-derived line items whose end_date must be set.
	// If limit <= 0, an implementation-defined default is used.
	ListAddonLineItemsToTerminate(
		ctx context.Context,
		p ListAddonLineItemsToTerminateParams,
	) (items []AddonLineItemTerminationDelta, err error)

	// ListAddonLineItemsToCreate returns missing (subscription_id, price_id) pairs for an addon.
	// price_id is the addon price ID (prices.entity_type=ADDON).
	// If limit <= 0, an implementation-defined default is used.
	ListAddonLineItemsToCreate(
		ctx context.Context,
		p ListAddonLineItemsToCreateParams,
	) (items []AddonLineItemCreationDelta, err error)

	// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch.
	// Returns nil when cursor can't advance.
	// Returns pointer to subscription ID when cursor can advance.
	GetLastSubscriptionIDInBatch(
		ctx context.Context,
		p ListAddonLineItemsToCreateParams,
	) (lastSubID *string, err error)
}
