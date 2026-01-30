package planpricesync

import (
	"context"
	"time"
)

// PlanLineItemTerminationDelta is a plan-sync delta row for setting a line item's end_date.
type PlanLineItemTerminationDelta struct {
	LineItemID     string
	SubscriptionID string
	PriceID        string
	TargetEndDate  time.Time // NOT NULL in this delta query
}

// PlanLineItemCreationDelta is a plan-sync delta row for creating a new line item.
type PlanLineItemCreationDelta struct {
	SubscriptionID string
	PriceID        string // plan price ID to use for the new line item
}

type ListPlanLineItemsToTerminateParams struct {
	PlanID string
	Limit  int
}

type ListPlanLineItemsToCreateParams struct {
	PlanID     string
	Limit      int
	AfterSubID string // Optional cursor: subscription_id from last row
}

type TerminateExpiredPlanPricesLineItemsParams struct {
	PlanID string
	Limit  int
}

// Repository defines the interface for plan price sync delta queries.
//
// This repo is intentionally scoped to two canonical DB-driven queries:
// 1) plan-derived line items whose end_date must be set to price.end_date
// 2) missing (subscription_id, price_id) pairs where a plan-derived line item must be created
type Repository interface {
	// TerminateExpiredPlanPricesLineItems terminates plan-derived line items whose end_date must be set to price.end_date.
	//
	// Batch:
	// - If limit <= 0, an implementation-defined default is used.
	TerminateExpiredPlanPricesLineItems(
		ctx context.Context,
		p TerminateExpiredPlanPricesLineItemsParams,
	) (numTerminated int, err error)

	// ListPlanLineItemsToTerminate returns plan-derived line items whose end_date must be set to price.end_date.
	//
	// Batch:
	// - If limit <= 0, an implementation-defined default is used.
	ListPlanLineItemsToTerminate(
		ctx context.Context,
		p ListPlanLineItemsToTerminateParams,
	) (items []PlanLineItemTerminationDelta, err error)

	// ListPlanLineItemsToCreate returns missing (subscription_id, price_id) pairs for a plan.
	// price_id is the plan price ID (prices.entity_type=PLAN), not parent_price_id.
	//
	// Batch:
	// - If limit <= 0, an implementation-defined default is used.
	ListPlanLineItemsToCreate(
		ctx context.Context,
		p ListPlanLineItemsToCreateParams,
	) (items []PlanLineItemCreationDelta, err error)

	// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch.
	// This is used to advance the cursor even when there are no missing pairs in the batch.
	//
	// Returns:
	// - nil when cursor can't advance: lastSubID == "" (no more subscriptions) OR lastSubID == afterSubID (cursor didn't advance)
	// - pointer to subscription ID when can advance: lastSubID != "" && lastSubID != afterSubID
	GetLastSubscriptionIDInBatch(
		ctx context.Context,
		p ListPlanLineItemsToCreateParams,
	) (lastSubID *string, err error)
}
