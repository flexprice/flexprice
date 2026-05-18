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
	CustomerID     string // subscription's customer_id, for reprocessing without listing subscriptions
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

	// CurrentPlanSequence returns max(prices.sequence) for the plan's
	// published, non-fixed prices. Used as the target sequence subscriptions
	// are stamped to after a successful sync pass. Returns 0 if the plan
	// has no qualifying prices.
	CurrentPlanSequence(ctx context.Context, planID string) (int64, error)

	// ListPlanLineItemsToCreateV2 returns missing (subscription_id, price_id)
	// pairs for a plan, narrowed to prices that changed since each
	// subscription's synced_price_sequence. Also returns the full set of
	// stale sub IDs in this page (so stamp can be scoped exactly to the
	// discovery window even when no pairs were produced). The page
	// advances implicitly via stamping — stamped subs fall out of the
	// `synced_price_sequence < TargetSeq` filter on the next call.
	ListPlanLineItemsToCreateV2(
		ctx context.Context,
		p ListPlanLineItemsToCreateV2Params,
	) (items []PlanLineItemCreationDelta, staleSubIDs []string, err error)

	// TerminatePlanPricesLineItemsV2 sets end_date on live plan-derived line
	// items belonging to the given subs whose price has been ended. Scoping
	// to a sub set bounds the UPDATE per page (no plan-wide locks). The
	// `li.end_date IS NULL` guard makes this idempotent — re-runs are no-ops.
	// Returns rows affected.
	TerminatePlanPricesLineItemsV2(
		ctx context.Context,
		p TerminatePlanPricesLineItemsV2Params,
	) (int, error)

	// StampSubsAsSynced sets synced_price_sequence on the given subs.
	// Always uses target as a forward-only update (idempotent).
	StampSubsAsSynced(
		ctx context.Context,
		p StampSubsAsSyncedParams,
	) (int, error)
}

// ListPlanLineItemsToCreateV2Params drives the V2 discovery query.
type ListPlanLineItemsToCreateV2Params struct {
	PlanID    string
	TargetSeq int64 // subs stale relative to this value are in scope
	Limit     int   // page size (defaults to implementation-defined)
}

// TerminatePlanPricesLineItemsV2Params drives the V2 termination UPDATE.
type TerminatePlanPricesLineItemsV2Params struct {
	PlanID string
	SubIDs []string
}

// StampSubsAsSyncedParams sets synced_price_sequence on a set of subs.
type StampSubsAsSyncedParams struct {
	TargetSeq int64
	SubIDs    []string
}
