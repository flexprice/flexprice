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

// end_date before current_period_end must clamp to [stub, terminal], not emit an inverted period.
func (s *SubscriptionServiceSuite) TestCalculateBillingPeriods_EndDateInsidePeriodClampsAndDoesNotInvert() {
	periodStart := s.testData.now.Add(-14 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(-2 * 24 * time.Hour)
	endDate := s.testData.now.Add(-4 * 24 * time.Hour) // before period end, after period start

	sub := &subscription.Subscription{
		ID:                 "sub_calc_periods_clamp",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		EndDate:            &endDate,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	periods, err := s.service.(*subscriptionService).CalculateBillingPeriods(s.GetContext(), sub.ID)
	s.NoError(err)

	// [clamped stub, terminal]; len>1 makes ShouldProcess true so the stub is invoiced.
	s.Require().Len(periods, 2)

	s.True(periods[0].Start.Equal(periodStart), "stub must start at the period start")
	s.True(periods[0].End.Equal(endDate), "stub must close at end_date (%s), got %s", endDate, periods[0].End)

	completed := periods[:len(periods)-1]
	for i, p := range completed {
		s.True(p.End.After(p.Start),
			"invoiced period %d [%s, %s] must not be inverted or zero-width", i, p.Start, p.End)
		s.False(p.End.After(endDate),
			"invoiced period %d end (%s) must not run past end_date (%s)", i, p.End, endDate)
	}

	last := periods[len(periods)-1]
	s.True(last.End.Equal(endDate),
		"terminal period must end at end_date (%s) to trigger cancellation, got %s", endDate, last.End)
}

// An already-inverted stored period (e.g. from a direct DB edit) must fail with a validation error.
func (s *SubscriptionServiceSuite) TestCalculateBillingPeriods_InvertedStoredPeriodFailsLoudly() {
	periodStart := s.testData.now.Add(-2 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(-5 * 24 * time.Hour) // before start = inverted stored period

	sub := &subscription.Subscription{
		ID:                 "sub_calc_periods_inverted",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.(*subscriptionService).CalculateBillingPeriods(s.GetContext(), sub.ID)
	s.Error(err)
	s.Contains(err.Error(), "inverted")
}

// PUT cancel_at inside the current period must shorten current_period_end to cancel_at.
func (s *SubscriptionServiceSuite) TestUpdateSubscription_CancelAtInsidePeriodShortensCurrentPeriodEnd() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)
	cancelAt := s.testData.now.Add(3 * 24 * time.Hour) // inside the current period

	sub := &subscription.Subscription{
		ID:                 "sub_update_cancel_at_shorten",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		CancelAt: &cancelAt,
	})
	s.NoError(err)

	updated, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Require().NotNil(updated.EndDate)
	s.True(updated.EndDate.Equal(cancelAt), "end_date must be set to cancel_at")
	s.True(updated.CurrentPeriodEnd.Equal(cancelAt),
		"current_period_end (%s) must be shortened to cancel_at (%s)", updated.CurrentPeriodEnd, cancelAt)
	s.True(updated.CurrentPeriodStart.Before(updated.CurrentPeriodEnd),
		"period must not invert")
}

// Moving cancel_at later on a second update must follow the new date, not stay stuck at the earlier one.
func (s *SubscriptionServiceSuite) TestUpdateSubscription_CancelAtMovedLaterRestoresBoundary() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)
	d1 := s.testData.now.Add(3 * 24 * time.Hour)  // first cancel: inside, earlier
	d2 := s.testData.now.Add(10 * 24 * time.Hour) // moved later, still inside

	sub := &subscription.Subscription{
		ID:                 "sub_update_cancel_moved_later",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{CancelAt: &d1})
	s.NoError(err)
	afterD1, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.True(afterD1.CurrentPeriodEnd.Equal(d1), "first update should shorten to d1")

	_, err = s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{CancelAt: &d2})
	s.NoError(err)
	afterD2, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.True(afterD2.CurrentPeriodEnd.Equal(d2),
		"current_period_end must follow the later cancel date (%s), not stay stuck at d1 (%s)", d2, d1)
	s.True(afterD2.EndDate.Equal(d2), "end_date must update to the later date")
}

// cancel_at_period_end=false with no cancel_at clears the pending cancellation and restores the natural boundary.
func (s *SubscriptionServiceSuite) TestUpdateSubscription_ClearCancellationRestoresNaturalBoundary() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	cancelAt := s.testData.now.Add(3 * 24 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub_update_clear_cancellation",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   cancelAt, // already shortened by a prior cancel
		EndDate:            &cancelAt,
		CancelAt:           &cancelAt,
		CancelAtPeriodEnd:  true,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	clear := false
	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{CancelAtPeriodEnd: &clear})
	s.NoError(err)

	updated, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Nil(updated.CancelAt, "cancel_at must be cleared")
	s.Nil(updated.EndDate, "end_date must be cleared")
	s.False(updated.CancelAtPeriodEnd, "flag must be false")

	expectedNatural, nerr := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: periodStart,
		BillingAnchor:      periodStart,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
		Timezone:           types.DefaultTimezone,
	})
	s.NoError(nerr)
	s.True(updated.CurrentPeriodEnd.Equal(expectedNatural),
		"current_period_end must restore to the natural boundary (%s), got %s", expectedNatural, updated.CurrentPeriodEnd)
}

// A false flag must not clear a fixed-term end_date (set at creation with no CancelAt).
func (s *SubscriptionServiceSuite) TestUpdateSubscription_ClearDoesNotWipeFixedTermEndDate() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)
	fixedTermEnd := s.testData.now.Add(90 * 24 * time.Hour) // contract end, NOT a cancellation

	sub := &subscription.Subscription{
		ID:                 "sub_update_fixed_term_end",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		EndDate:            &fixedTermEnd,
		CancelAt:           nil, // no pending cancellation
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	clear := false
	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{CancelAtPeriodEnd: &clear})
	s.NoError(err)

	updated, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Require().NotNil(updated.EndDate)
	s.True(updated.EndDate.Equal(fixedTermEnd), "fixed-term end_date must not be cleared by a false flag")
	s.True(updated.CurrentPeriodEnd.Equal(periodEnd), "current_period_end must be untouched")
}

// PUT cancel_at at/before the current period start must be rejected (would invert the stored period).
func (s *SubscriptionServiceSuite) TestUpdateSubscription_CancelAtBeforePeriodStartRejected() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)
	cancelAt := s.testData.now.Add(-10 * 24 * time.Hour) // before period start

	sub := &subscription.Subscription{
		ID:                 "sub_update_cancel_at_reject",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		CancelAt: &cancelAt,
	})
	s.Error(err)
}

// Omitting cancel_at_period_end must not reset a stored true flag.
func (s *SubscriptionServiceSuite) TestUpdateSubscription_OmittedCancelAtPeriodEndPreservesStoredValue() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub_update_preserve_cape",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		CancelAtPeriodEnd:  true,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	// Update only status, leaving cancel_at_period_end omitted.
	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		Status: types.SubscriptionStatusActive,
	})
	s.NoError(err)

	updated, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.True(updated.CancelAtPeriodEnd,
		"omitted cancel_at_period_end must leave the stored true value unchanged")
}

// scheduled_date cancel with cancel_at at/before the period start must be rejected, like immediate.
func (s *SubscriptionServiceSuite) TestCancelSubscription_ScheduledDateBeforePeriodStartRejected() {
	periodStart := s.testData.now.Add(-5 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(25 * 24 * time.Hour)
	cancelAt := s.testData.now.Add(-10 * 24 * time.Hour) // before period start

	sub := &subscription.Subscription{
		ID:                 "sub_scheduled_before_start_reject",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeScheduledDate,
		ProrationBehavior: types.ProrationBehaviorNone,
		CancelAt:          &cancelAt,
		Reason:            "test_scheduled_before_start",
	})
	s.Error(err)
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
