package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// TestCancelSubscription_ImmediateTruncatesCurrentPeriodEnd pins the invariant that a
// cancelled subscription never reports a period extending past its end date.
//
// Leaving CurrentPeriodEnd in the future is what produced the production shape where
// end_date sat inside the still-open period; any later write that flipped the subscription
// back to active then handed the billing cron a subscription it could not generate valid
// periods for. scheduled_date already truncates — immediate did not.
func (s *SubscriptionServiceSuite) TestCancelSubscription_ImmediateTruncatesCurrentPeriodEnd() {
	originalPeriodEnd := s.testData.now.Add(25 * 24 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub_immediate_truncate",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-5 * 24 * time.Hour),
		CurrentPeriodEnd:   originalPeriodEnd,
		BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "test_immediate_truncation",
	})
	s.NoError(err)

	cancelled, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)

	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
	s.Require().NotNil(cancelled.EndDate)
	s.True(cancelled.CurrentPeriodEnd.Equal(*cancelled.EndDate),
		"current_period_end (%s) must be closed at end_date (%s)",
		cancelled.CurrentPeriodEnd, *cancelled.EndDate)
	s.True(cancelled.CurrentPeriodEnd.Before(originalPeriodEnd),
		"current_period_end must no longer extend past the cancellation")
	s.True(cancelled.CurrentPeriodStart.Before(cancelled.CurrentPeriodEnd),
		"period must not collapse or invert")
}

// TestCancelSubscription_ImmediateProrationKeyUsesPreCancellationPeriod guards the
// idempotency hazard created by the truncation above.
//
// buildCancellationProrationKey keys immediate cancellations on the subscription's period
// precisely because effectiveDate is time.Now() and would differ on every retry. If the key
// were derived after CurrentPeriodEnd is truncated to effectiveDate, it would inherit that
// instability and a retry could credit the wallet twice.
func (s *SubscriptionServiceSuite) TestCancelSubscription_ImmediateProrationKeyUsesPreCancellationPeriod() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub_immediate_proration_key",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	// Advance fixed charge: cancelling mid-period leaves unused time, which prorates to a
	// wallet credit and therefore exercises the idempotency key.
	advancePrice := &price.Price{
		ID:                 "price_fixed_advance_cancel_key",
		Amount:             decimal.NewFromFloat(60.00),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), advancePrice))

	lineItem := &subscription.SubscriptionLineItem{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:  sub.ID,
		CustomerID:      sub.CustomerID,
		EntityID:        s.testData.plan.ID,
		EntityType:      types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.testData.plan.Name,
		PriceID:         advancePrice.ID,
		PriceType:       advancePrice.Type,
		DisplayName:     "Monthly Fee (Advance)",
		Quantity:        decimal.NewFromInt(1),
		Currency:        sub.Currency,
		BillingPeriod:   sub.BillingPeriod,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(
		s.GetContext(), sub, []*subscription.SubscriptionLineItem{lineItem}))

	// Snapshot the pre-cancellation period, which is what the key must be built from.
	preCancellation := *sub

	req := &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		Reason:            "test_proration_key_stability",
	}
	_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, req)
	s.NoError(err)

	cancelled, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Require().NotNil(cancelled.CancelAt)

	subService := s.service.(*subscriptionService)
	expectedKey := subService.buildCancellationProrationKey(&preCancellation, req, *cancelled.CancelAt)

	txn, err := subService.WalletRepo.GetTransactionByIdempotencyKey(s.GetContext(), expectedKey)
	s.NoError(err, "proration credit must be keyed on the pre-cancellation period")
	s.NotNil(txn)
}

// TestCancelSubscription_ImmediateOnNotYetStartedSubscription covers a subscription whose
// period starts in the future and is cancelled immediately with cancel_at omitted. The
// derived effective date is time.Now(), which precedes the period; left unclamped it
// persisted an end_date before current_period_start while current_period_end kept running
// to the original boundary. An explicit cancel_at before the period start is rejected
// outright, so only the derived date could reach this shape.
func (s *SubscriptionServiceSuite) TestCancelSubscription_ImmediateOnNotYetStartedSubscription() {
	futureStart := s.testData.now.Add(10 * 24 * time.Hour)
	futureEnd := s.testData.now.Add(40 * 24 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub_immediate_not_yet_started",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          futureStart,
		CurrentPeriodStart: futureStart,
		CurrentPeriodEnd:   futureEnd,
		BillingAnchor:      futureStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "test_not_yet_started",
	})
	s.NoError(err)

	cancelled, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)

	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
	s.Require().NotNil(cancelled.EndDate)
	s.True(cancelled.EndDate.Equal(futureStart),
		"end_date (%s) must be clamped to the period start (%s), not time.Now()",
		*cancelled.EndDate, futureStart)
	s.False(cancelled.EndDate.Before(cancelled.CurrentPeriodStart),
		"end_date must never precede current_period_start")
	s.True(cancelled.CurrentPeriodEnd.Equal(*cancelled.EndDate),
		"current_period_end (%s) must close at end_date (%s)",
		cancelled.CurrentPeriodEnd, *cancelled.EndDate)
}
