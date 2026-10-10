// Package proration provides functionality for handling subscription proration.
package proration

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// Service defines the operations for handling proration.
type Service interface {
	// CalculateProration calculates the proration credits and charges for a given change.
	// It does not persist anything or modify the subscription/invoice directly.
	CalculateProration(ctx context.Context, params ProrationParams) (*ProrationResult, error)

	// CalculateSubscriptionCancellationProration credits each item's unused time on its own windows.
	CalculateSubscriptionCancellationProration(
		ctx context.Context,
		subscription *subscription.Subscription,
		lineItems []*subscription.SubscriptionLineItem,
		cancellationType types.CancellationType,
		effectiveDate time.Time,
		reason string,
		behavior types.ProrationBehavior,
	) (*SubscriptionProrationResult, error)

	// CalculateEntitlementProration calculates prorated entitlement limits for a subscription
	CalculateEntitlementProration(
		ctx context.Context,
		planID string,
		periodStart time.Time,
		periodEnd time.Time,
		prorationDate time.Time,
		customerTimezone string,
		billingCycle types.BillingCycle,
		billingAnchor time.Time,
		billingPeriod types.BillingPeriod,
		billingPeriodCount int,
	) (*EntitlementProrationResult, error)

	// CalculateAdditiveEntitlementProration calculates combined entitlement limits
	// when changing plans mid-period. It adds the remaining entitlement from the old plan
	// to the prorated entitlement from the new plan.
	CalculateAdditiveEntitlementProration(
		ctx context.Context,
		oldPlanID string,
		newPlanID string,
		oldPeriodStart time.Time,
		oldPeriodEnd time.Time,
		changeDate time.Time,
		customerTimezone string,
		billingCycle types.BillingCycle,
		billingAnchor time.Time,
		billingPeriod types.BillingPeriod,
		billingPeriodCount int,
	) (*EntitlementProrationResult, error)

	// CreateProratedEntitlements creates subscription-scoped entitlement overrides
	CreateProratedEntitlements(
		ctx context.Context,
		subscriptionID string,
		prorationResult *EntitlementProrationResult,
		startDate time.Time,
		endDate time.Time,
	) error
}

// Calculator performs proration calculations.
// It's kept separate from the service to allow different calculation strategies or easier testing.
type Calculator interface {
	Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error)
}
