package service

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// razorpayCheckoutParams is the minimal checkout object the attach endpoint accepts.
func (s *SubscriptionServiceSuite) razorpayCheckoutParams() *dto.CheckoutParams {
	return &dto.CheckoutParams{
		PaymentParams: dto.PaymentParams{
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		},
	}
}

// seedPendingAddonAssociation writes a pending association straight through the store, the
// state the payment-gated attach flow will produce once it exists. The in-memory store does
// no status normalization, so the row lands exactly as written.
func (s *SubscriptionServiceSuite) seedPendingAddonAssociation(
	associationID, addonID, subscriptionID string,
	endDate *time.Time,
) *addonassociation.AddonAssociation {
	ctx := s.GetContext()
	start := s.testData.now

	assoc := &addonassociation.AddonAssociation{
		ID:          associationID,
		EntityID:    subscriptionID,
		EntityType:  types.AddonAssociationEntityTypeSubscription,
		AddonID:     addonID,
		StartDate:   &start,
		EndDate:     endDate,
		AddonStatus: types.AddonStatusPending,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().AddonAssociationRepo.Create(ctx, assoc))

	return assoc
}

// seedFixedPriceAddon registers a published addon with a single FIXED price, the shape that
// actually produces a proration charge when attached mid-period.
func (s *SubscriptionServiceSuite) seedFixedPriceAddon(
	addonID string,
	amount decimal.Decimal,
	cadence types.InvoiceCadence,
) {
	ctx := s.GetContext()

	s.NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        addonID,
		LookupKey: addonID,
		Name:      "Fixed Price Addon",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + addonID,
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     cadence,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
}

// oneOffInvoicesFor returns the subscription's ONE_OFF invoices — the shape addon proration
// charges take.
func (s *SubscriptionServiceSuite) oneOffInvoicesFor(subscriptionID string) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.NoError(err)

	oneOff := make([]*invoice.Invoice, 0, len(invoices))
	for _, inv := range invoices {
		if inv.InvoiceType == types.InvoiceTypeOneOff {
			oneOff = append(oneOff, inv)
		}
	}
	return oneOff
}

func (s *SubscriptionServiceSuite) addonLineItemsFor(subscriptionID, addonID string) []*subscription.SubscriptionLineItem {
	filter := types.NewNoLimitSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.EntityIDs = []string{addonID}
	filter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)

	items, err := s.GetStores().SubscriptionLineItemRepo.List(s.GetContext(), filter)
	s.NoError(err)
	return items
}

func (s *SubscriptionServiceSuite) checkoutSessionCount() int {
	sessions, err := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
	})
	s.NoError(err)
	return len(sessions)
}

// seedMeteredAddon registers a published addon with one usage price and one metered
// entitlement on featureID at the given reset period.
func (s *SubscriptionServiceSuite) seedMeteredAddon(
	addonID, featureID string,
	resetPeriod types.EntitlementUsageResetPeriod,
) {
	ctx := s.GetContext()

	s.NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        addonID,
		LookupKey: addonID,
		Name:      "Metered Addon",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + addonID,
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meters.apiCalls.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))

	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID:               "ent_" + addonID,
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_ADDON,
		EntityID:         addonID,
		FeatureID:        featureID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(int64(1000)),
		UsageResetPeriod: resetPeriod,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)
}

// A pending association is gated behind an unpaid checkout: it owns no line items, credit
// grants or entitlements, so removing it through the addon API would strand the session that
// is meant to activate it. RemoveAddonFromSubscription reads via GetByID, which applies no
// addon_status filter, so the association is reachable and the guard has to be explicit.
func (s *SubscriptionServiceSuite) TestRemoveAddon_RejectsPendingAssociation() {
	ctx := s.GetContext()
	sub := s.testData.subscription

	assoc := s.seedPendingAddonAssociation("assoc_pending_remove", "addon_pending_remove", sub.ID, nil)

	err := s.service.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: assoc.ID,
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), "pending payment")

	stored, getErr := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.NoError(getErr)
	s.Equal(types.AddonStatusPending, stored.AddonStatus, "rejected removal must leave the association untouched")
	s.Nil(stored.CancelledAt)
}

// A onetime pending association carries the cadence boundary as its EndDate. The pending
// guard must be reached before the "already scheduled to be removed" EndDate check, or the
// caller is told the wrong reason and never learns a checkout is outstanding.
func (s *SubscriptionServiceSuite) TestRemoveAddon_RejectsPendingOnetimeAssociationWithEndDate() {
	ctx := s.GetContext()
	sub := s.testData.subscription

	periodEnd := sub.CurrentPeriodEnd
	assoc := s.seedPendingAddonAssociation("assoc_pending_onetime", "addon_pending_onetime", sub.ID, &periodEnd)

	err := s.service.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: assoc.ID,
	})

	s.Error(err)
	s.Contains(err.Error(), "pending payment")
	s.NotContains(err.Error(), "already scheduled to be removed")
}

// Cancelling a subscription must take its pending associations with it. They are invisible to
// the active-window read cancelAddonsForSubscription uses, so without the explicit pending
// read they outlive the subscription as orphans that a late checkout could still activate.
func (s *SubscriptionServiceSuite) TestCancelSubscription_CancelsPendingAssociations() {
	ctx := s.GetContext()

	sub := &subscription.Subscription{
		ID:                 "sub_cancel_pending_addon",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))

	assoc := s.seedPendingAddonAssociation("assoc_pending_cancel", "addon_pending_cancel", sub.ID, nil)

	_, err := s.service.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		Reason:           "test cancellation",
	})
	s.NoError(err)

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.NoError(err)
	s.Equal(types.AddonStatusCancelled, stored.AddonStatus,
		"pending association must be cancelled alongside the subscription")
	s.NotNil(stored.CancelledAt)
	s.NotNil(stored.EndDate)
}

// A onetime pending association's EndDate is the cadence boundary, not a removal schedule, so
// the "already scheduled for removal, skip" branch must not swallow it during cancellation.
func (s *SubscriptionServiceSuite) TestCancelSubscription_CancelsPendingOnetimeAssociationWithEndDate() {
	ctx := s.GetContext()

	sub := &subscription.Subscription{
		ID:                 "sub_cancel_pending_onetime",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))

	periodEnd := sub.CurrentPeriodEnd
	assoc := s.seedPendingAddonAssociation("assoc_pending_cancel_onetime", "addon_pending_cancel_onetime", sub.ID, &periodEnd)

	_, err := s.service.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
	})
	s.NoError(err)

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.NoError(err)
	s.Equal(types.AddonStatusCancelled, stored.AddonStatus)
}

// The concurrent guard only runs inside the payment-gated path, so a plain pay-later add slips
// past it. Pending addon A grants a metered feature at MONTHLY; adding addon B granting the
// same feature at ANNUAL must be rejected, otherwise A's checkout completing leaves two
// conflicting reset periods live on one feature — the state this validation exists to prevent.
func (s *SubscriptionServiceSuite) TestValidateEntitlementCompatibility_SeesPendingAssociation() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription

	featureID := "feat_pending_conflict"
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, &feature.Feature{
		ID:        featureID,
		Name:      "Shared Metered Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.testData.meters.apiCalls.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	pendingAddonID := "addon_pending_monthly"
	incomingAddonID := "addon_incoming_annual"
	s.seedMeteredAddon(pendingAddonID, featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon(incomingAddonID, featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL)

	// Without the pending association the conflicting add is accepted: the pending addon's
	// entitlements are invisible to GetSubscriptionEntitlements by design.
	s.NoError(subService.validateEntitlementCompatibility(ctx, sub.ID, incomingAddonID),
		"baseline: nothing to conflict with before the pending association exists")

	s.seedPendingAddonAssociation("assoc_pending_entitlement", pendingAddonID, sub.ID, nil)

	err := subService.validateEntitlementCompatibility(ctx, sub.ID, incomingAddonID)
	s.Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), "reset period")

	// The same addon's own reset period must still pass — the guard rejects conflicts, not
	// every feature a pending addon happens to touch.
	matchingAddonID := "addon_incoming_monthly"
	s.seedMeteredAddon(matchingAddonID, featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.NoError(subService.validateEntitlementCompatibility(ctx, sub.ID, matchingAddonID))
}

// Baseline for everything the checkout path must not disturb. Attaching at the period start
// makes the proration a full period, so the charge is exactly the price and the money assertion
// is deterministic rather than a tolerance.
func (s *SubscriptionServiceSuite) TestAddAddon_NoCheckout_PayLaterUnchanged() {
	ctx := s.GetContext()
	sub := s.testData.subscription
	addonID := "addon_paylater_fixed"

	amount := decimal.NewFromInt(30)
	s.seedFixedPriceAddon(addonID, amount, types.InvoiceCadenceAdvance)

	// The shared fixture declares a monthly cadence but only a 7-day current period. A monthly
	// addon is priced against a whole month, so give the subscription the full period this test
	// is about before attaching.
	sub.CurrentPeriodEnd = sub.CurrentPeriodStart.AddDate(0, 1, 0)
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	periodStart := sub.CurrentPeriodStart
	resp, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
		SubscriptionID: sub.ID,
		AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
			AddonID:           addonID,
			Cadence:           types.AddonCadenceRecurring,
			StartDate:         &periodStart,
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		},
	})
	s.Require().NoError(err)

	s.Require().NotNil(resp.AddonAssociation)
	s.Equal(types.AddonStatusActive, resp.AddonAssociation.AddonStatus)
	s.Nil(resp.CheckoutSession, "pay-later must not produce a checkout session")
	s.Nil(resp.Invoice)
	s.Zero(s.checkoutSessionCount())

	s.Len(s.addonLineItemsFor(sub.ID, addonID), 1)

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1, "pay-later must raise exactly one ONE_OFF proration invoice")
	s.Equal(string(types.InvoiceBillingReasonSubscriptionUpdate), invoices[0].BillingReason)
	s.True(amount.Equal(invoices[0].AmountDue),
		"full-period attach must charge the full price, got %s", invoices[0].AmountDue)
}

// Every route to a zero net: the pay-later path raises no charge for these, so gating them
// behind payment would ask the customer to pay nothing. They attach immediately and the
// checkout is ignored.
func (s *SubscriptionServiceSuite) TestAddAddon_CheckoutZeroNet_AttachesImmediately() {
	tests := []struct {
		name              string
		usagePrice        bool
		prorationBehavior types.ProrationBehavior
	}{
		{
			// calculator.go short-circuits on `none` before computing anything.
			name:              "proration behavior none",
			prorationBehavior: types.ProrationBehaviorNone,
		},
		{
			// D2: Compute would price this, but Apply is a no-op for anything other than
			// create_prorations — so previewing a charge here would diverge from pay-later.
			name:              "proration behavior unset",
			prorationBehavior: "",
		},
		{
			// Future consumption is unknown at change time, so usage prices are skipped.
			name:              "usage only addon prices",
			usagePrice:        true,
			prorationBehavior: types.ProrationBehaviorCreateProrations,
		},
	}

	for i, tc := range tests {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			sub := s.testData.subscription
			addonID := fmt.Sprintf("addon_zero_net_%d", i)

			if tc.usagePrice {
				s.seedMeteredAddon(addonID, "", types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
			} else {
				s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
			}

			now := s.testData.now
			resp, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
				SubscriptionID: sub.ID,
				Checkout:       s.razorpayCheckoutParams(),
				AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
					AddonID:           addonID,
					Cadence:           types.AddonCadenceRecurring,
					StartDate:         &now,
					ProrationBehavior: tc.prorationBehavior,
				},
			})
			s.Require().NoError(err)

			s.Require().NotNil(resp.AddonAssociation)
			s.Equal(types.AddonStatusActive, resp.AddonAssociation.AddonStatus,
				"a zero-net attach takes effect immediately")
			s.Nil(resp.CheckoutSession, "checkout must be ignored when there is nothing to charge")
			s.Len(s.addonLineItemsFor(sub.ID, addonID), 1)
			s.Zero(s.checkoutSessionCount())
		})
	}
}

// The v1 checkout path cannot honour overrides, commitments or draft subscriptions. Each must
// be refused before anything is written.
func (s *SubscriptionServiceSuite) TestAddAddon_CheckoutRejectsUnsupportedCombinations() {
	ctx := s.GetContext()
	sub := s.testData.subscription

	s.Run("override_line_items", func() {
		addonID := "addon_reject_override"
		s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

		now := s.testData.now
		_, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
			SubscriptionID: sub.ID,
			Checkout:       s.razorpayCheckoutParams(),
			AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
				AddonID:           addonID,
				Cadence:           types.AddonCadenceRecurring,
				StartDate:         &now,
				ProrationBehavior: types.ProrationBehaviorCreateProrations,
				OverrideLineItems: []dto.OverrideLineItemRequest{{
					PriceID:  "price_" + addonID,
					Quantity: lo.ToPtr(decimal.NewFromInt(2)),
				}},
			},
		})

		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
		s.Empty(s.addonLineItemsFor(sub.ID, addonID))
		s.Zero(s.checkoutSessionCount())
	})

	s.Run("line_item_commitments", func() {
		addonID := "addon_reject_commitment"
		s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

		now := s.testData.now
		_, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
			SubscriptionID: sub.ID,
			Checkout:       s.razorpayCheckoutParams(),
			AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
				AddonID:           addonID,
				Cadence:           types.AddonCadenceRecurring,
				StartDate:         &now,
				ProrationBehavior: types.ProrationBehaviorCreateProrations,
				LineItemCommitments: map[string]*dto.LineItemCommitmentConfig{
					"price_" + addonID: {CommitmentAmount: lo.ToPtr(decimal.NewFromInt(10))},
				},
			},
		})

		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
		s.Empty(s.addonLineItemsFor(sub.ID, addonID))
		s.Zero(s.checkoutSessionCount())
	})

	s.Run("draft subscription", func() {
		addonID := "addon_reject_draft"
		s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

		draft := &subscription.Subscription{
			ID:                 "sub_draft_checkout_addon",
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			SubscriptionStatus: types.SubscriptionStatusDraft,
			StartDate:          s.testData.now,
			CurrentPeriodStart: s.testData.now,
			CurrentPeriodEnd:   s.testData.now.AddDate(0, 1, 0),
			BillingAnchor:      s.testData.now,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			Currency:           "usd",
			BaseModel:          types.GetDefaultBaseModel(ctx),
			LineItems:          []*subscription.SubscriptionLineItem{},
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, draft, draft.LineItems))

		now := s.testData.now
		_, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
			SubscriptionID: draft.ID,
			Checkout:       s.razorpayCheckoutParams(),
			AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
				AddonID:           addonID,
				Cadence:           types.AddonCadenceRecurring,
				StartDate:         &now,
				ProrationBehavior: types.ProrationBehaviorCreateProrations,
			},
		})

		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
		s.Contains(err.Error(), "status does not allow")
		s.Empty(s.addonLineItemsFor(draft.ID, addonID))
		s.Zero(s.checkoutSessionCount())

		// Draft attach without checkout stays supported.
		_, err = s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
			SubscriptionID: draft.ID,
			AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
				AddonID:   addonID,
				Cadence:   types.AddonCadenceRecurring,
				StartDate: &now,
			},
		})
		s.NoError(err, "pay-later attach to a draft subscription must be unaffected")
	})
}

// Pending associations must stay out of the reads that grant real access and drive the public
// addon list — they are only visible to the compatibility check and the cancellation sweep.
func (s *SubscriptionServiceSuite) TestPendingAssociation_InvisibleToActiveReads() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription

	featureID := "feat_pending_invisible"
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, &feature.Feature{
		ID:        featureID,
		Name:      "Pending Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.testData.meters.apiCalls.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	addonID := "addon_pending_invisible"
	s.seedMeteredAddon(addonID, featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedPendingAddonAssociation("assoc_pending_invisible", addonID, sub.ID, nil)

	associations, err := subService.GetActiveAddonAssociations(ctx, sub.ID)
	s.NoError(err)
	for _, item := range associations.Items {
		s.NotEqual(addonID, item.AddonAssociation.AddonID,
			"pending association must not surface in the public addon associations list")
	}

	entitlements, err := subService.GetSubscriptionEntitlements(ctx, sub.ID)
	s.NoError(err)
	for _, ent := range entitlements {
		s.NotEqual(featureID, ent.FeatureID,
			"pending association must not grant entitlements before its checkout completes")
	}
}

// ─────────────────────────────────────────────
// Pay-first execute
// ─────────────────────────────────────────────

// The invariant the whole design rests on: at execute time the pay-first path writes the
// association and the DRAFT invoice and NOTHING else. Line items are what billing reads, so a
// published addon line item here would be billed at the next rollover — including by the
// unattended daily draft-recompute cron — for an addon the customer has not paid for.
//
// Driven through the pieces rather than the entry point because completing a checkout session
// requires a live Razorpay call, which the test suite has no connection for.
func (s *SubscriptionServiceSuite) TestAddAddon_CheckoutNetCharge_PersistsOnlyPendingAssociation() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	addonID := "addon_payfirst_only_pending"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	now := s.testData.now
	req := &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         &now,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	config, err := NewAddonChangeService(subService.ServiceParams).Resolve(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{{Request: req}},
	})
	s.Require().NoError(err)

	plan := config.getAttaches()[0]
	summary := config.getQuote()
	s.True(summary.TotalChargeAmount.GreaterThan(decimal.Zero),
		"a mid-period fixed ADVANCE addon must produce a charge to gate on")

	// Planning alone must not have written anything.
	s.Empty(s.addonLineItemsFor(sub.ID, addonID), "planning must not persist line items")

	pending := plan.getAssociation()
	pending.AddonStatus = types.AddonStatusPending
	s.Require().NoError(s.GetStores().AddonAssociationRepo.Create(ctx, pending))

	drafted, err := NewLineItemProrationService(subService.ServiceParams).Settle(ctx, NewSettleProrationRequest(
		sub, summary, plan.getEffectiveDate(), sub.CurrentPeriodEnd,
		"Subscription update", plan.prorationIdempotencyKey(), SettleModeDraft,
	))
	s.Require().NoError(err)
	draft := drafted.Draft

	s.Equal(types.InvoiceStatusDraft, draft.InvoiceStatus)
	s.Equal(types.InvoiceTypeOneOff, draft.InvoiceType)
	s.True(summary.TotalChargeAmount.Equal(draft.AmountDue),
		"the draft must lock exactly the computed proration, got %s want %s",
		draft.AmountDue, summary.TotalChargeAmount)

	// Everything that must NOT exist yet.
	s.Empty(s.addonLineItemsFor(sub.ID, addonID), "pay-first must defer line items to completion")

	grantFilter := types.NewNoLimitCreditGrantFilter()
	grantFilter.SubscriptionIDs = []string{sub.ID}
	grants, err := subService.CreditGrantRepo.List(ctx, grantFilter)
	s.Require().NoError(err)
	for _, g := range grants {
		s.NotEqual(addonID, lo.FromPtr(g.AddonID), "pay-first must not materialize addon credit grants")
	}

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusPending, stored.AddonStatus)
}

// The highest-value assertion in the plan: a pending association must be invisible to the
// billing engine. Billing reads subscription_line_items and never addon_associations, so this
// guards the chokepoint at billing.go's line-item load — if pay-first ever started creating
// line items, this is what would catch it.
func (s *SubscriptionServiceSuite) TestAddAddon_PendingIsInvisibleToBilling() {
	ctx := s.GetContext()
	sub := s.testData.subscription
	addonID := "addon_pending_billing"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedPendingAddonAssociation("assoc_pending_billing", addonID, sub.ID, nil)

	billingSvc := NewBillingService(s.service.(*subscriptionService).ServiceParams)
	invoiceReq, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   sub,
		PeriodStart:    sub.CurrentPeriodStart,
		PeriodEnd:      sub.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.Require().NoError(err)
	s.Require().NotNil(invoiceReq)

	addonPriceID := "price_" + addonID
	for _, li := range invoiceReq.LineItems {
		s.NotEqual(addonPriceID, lo.FromPtr(li.PriceID),
			"a pending addon must not be billed before its checkout completes")
	}
}

// Both payment-gated subscription flows mutually exclude, in both directions: either can
// invalidate the amount the other has locked on its draft invoice.
func (s *SubscriptionServiceSuite) TestAddAddon_CheckoutConcurrentGuard() {
	seedSession := func(action types.CheckoutAction, subscriptionID string) {
		ctx := s.GetContext()
		cfg := types.CheckoutConfiguration{}
		switch action {
		case types.CheckoutActionAddAddon:
			cfg.AddAddonParams = &types.AddAddonParams{
				SubscriptionID: subscriptionID,
				Addons: []types.AddAddonRef{{
					AssociationID: "addon_assoc_outstanding",
					AddonID:       "addon_outstanding",
					Cadence:       types.AddonCadenceRecurring,
					StartDate:     s.testData.now,
				}},
			}
		case types.CheckoutActionModifySubscription:
			cfg.ModifySubscriptionParams = &types.ModifySubscriptionParams{
				SubscriptionID: subscriptionID,
				LineItemModifications: []types.ModifySubscriptionLineItem{
					{LineItemID: "subs_li_outstanding", Quantity: decimal.NewFromInt(2)},
				},
			}
		}

		s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, &domainCheckout.CheckoutSession{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
			EnvironmentID:   types.GetEnvironmentID(ctx),
			CustomerID:      s.testData.customer.ID,
			Action:          action,
			CheckoutStatus:  types.CheckoutStatusPending,
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
			Configuration:   domainCheckout.ToJSONBCheckoutConfiguration(cfg),
			ExpiresAt:       time.Now().UTC().Add(time.Hour),
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}))
	}

	tests := []struct {
		name           string
		outstanding    types.CheckoutAction
		onSubscription bool
		wantBlocked    bool
	}{
		{name: "pending add_addon blocks", outstanding: types.CheckoutActionAddAddon, onSubscription: true, wantBlocked: true},
		{name: "pending modify_subscription blocks", outstanding: types.CheckoutActionModifySubscription, onSubscription: true, wantBlocked: true},
		{name: "session on another subscription does not block", outstanding: types.CheckoutActionAddAddon, onSubscription: false, wantBlocked: false},
	}

	for i, tc := range tests {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			// s.Run does not re-run SetupTest, so sessions seeded by earlier cases would
			// otherwise linger on this subscription and trip the guard for every case.
			s.GetStores().CheckoutSessionRepo.(*testutil.InMemoryCheckoutSessionStore).Clear()

			sub := s.testData.subscription
			addonID := fmt.Sprintf("addon_guard_%d", i)
			s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

			target := sub.ID
			if !tc.onSubscription {
				target = "subs_unrelated_guard"
			}
			seedSession(tc.outstanding, target)

			now := s.testData.now
			_, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
				SubscriptionID: sub.ID,
				Checkout:       s.razorpayCheckoutParams(),
				AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
					AddonID:           addonID,
					Cadence:           types.AddonCadenceRecurring,
					StartDate:         &now,
					ProrationBehavior: types.ProrationBehaviorCreateProrations,
				},
			})
			s.Require().Error(err, "the provider is unreachable in tests, so this always errors")

			if tc.wantBlocked {
				s.True(ierr.IsAlreadyExists(err), "expected the concurrent guard, got %v", err)
				// The guard runs before any write.
				s.Empty(s.addonLineItemsFor(sub.ID, addonID))
				s.Empty(s.oneOffInvoicesFor(sub.ID), "guard must reject before creating a draft")
				return
			}

			s.False(ierr.IsAlreadyExists(err),
				"a session on another subscription must not trip the guard, got %v", err)
		})
	}
}

// When session creation fails after the pending association is written, the association must
// not survive as an orphan a later webhook could activate.
//
// Triggered the same way wallet top-up tests trigger it: a session already holds this
// idempotency key. Its configuration names a DIFFERENT subscription, so the concurrent guard
// does not fire and the failure lands in CheckoutSessionRepo.Create — after the pending
// association and the draft invoice exist, which is precisely the window the rollback covers.
func (s *SubscriptionServiceSuite) TestAddAddon_CheckoutSessionCreateFailure_ArchivesPendingAssociation() {
	ctx := s.GetContext()
	sub := s.testData.subscription
	addonID := "addon_payfirst_rollback"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	idempKey := "addon-attach-orphan-idemp-key"
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionAddAddon,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			AddAddonParams: &types.AddAddonParams{
				SubscriptionID: "subs_some_other_subscription",
				Addons: []types.AddAddonRef{{
					AssociationID: "addon_assoc_other",
					AddonID:       "addon_other",
					Cadence:       types.AddonCadenceRecurring,
					StartDate:     s.testData.now,
				}},
			},
		}),
		IdempotencyKey: &idempKey,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}))

	checkout := s.razorpayCheckoutParams()
	checkout.IdempotencyKey = &idempKey

	now := s.testData.now
	_, err := s.service.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
		SubscriptionID: sub.ID,
		Checkout:       checkout,
		AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
			AddonID:           addonID,
			Cadence:           types.AddonCadenceRecurring,
			StartDate:         &now,
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		},
	})
	s.Require().Error(err)

	// Asserted on the row itself rather than through a status filter: the in-memory
	// association store only applies the published/archived filter inside its time-window
	// branch, so a status-scoped List would not distinguish the two here.
	assocFilter := types.NewNoLimitAddonAssociationFilter()
	assocFilter.EntityIDs = []string{sub.ID}
	assocFilter.AddonIDs = []string{addonID}
	associations, listErr := s.GetStores().AddonAssociationRepo.List(ctx, assocFilter)
	s.Require().NoError(listErr)
	s.Require().Len(associations, 1, "the pending association must have been written before the failure")
	s.Equal(types.StatusArchived, associations[0].Status,
		"the pending association must be archived when pay-first setup fails")
	s.Equal(types.AddonStatusPending, associations[0].AddonStatus,
		"the soft delete leaves addon_status untouched, matching the ent repository")

	s.Empty(s.addonLineItemsFor(sub.ID, addonID), "a failed pay-first must leave no line items")
}

// ─────────────────────────────────────────────
// Completion & cleanup
// ─────────────────────────────────────────────

// seedPayFirstAddonCheckout reproduces the state a single-addon ExecutePayFirst leaves behind —
// pending association, DRAFT proration invoice, INITIATED payment, pending session — without
// going through the provider, which needs a live Razorpay connection.
func (s *SubscriptionServiceSuite) seedPayFirstAddonCheckout(
	addonID string,
) (*domainCheckout.CheckoutSession, *addonassociation.AddonAssociation, *dto.InvoiceResponse) {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	params := subService.ServiceParams

	now := s.testData.now
	req := &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         &now,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	config, err := NewAddonChangeService(subService.ServiceParams).Resolve(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{{Request: req}},
	})
	s.Require().NoError(err)

	attach := config.getAttaches()[0]
	summary := config.getQuote()
	s.Require().True(summary.TotalChargeAmount.GreaterThan(decimal.Zero))

	pending := attach.getAssociation()
	pending.AddonStatus = types.AddonStatusPending
	s.Require().NoError(s.GetStores().AddonAssociationRepo.Create(ctx, pending))

	drafted, err := NewLineItemProrationService(subService.ServiceParams).Settle(ctx, NewSettleProrationRequest(
		sub, summary, attach.getEffectiveDate(), sub.CurrentPeriodEnd,
		"Subscription update", attach.prorationIdempotencyKey(), SettleModeDraft,
	))
	s.Require().NoError(err)
	draft := drafted.Draft

	checkoutSvc := &checkoutSessionService{ServiceParams: params}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draft.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      sub.CustomerID,
		Action:          types.CheckoutActionAddAddon,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			AddAddonParams: &types.AddAddonParams{
				SubscriptionID: sub.ID,
				Addons: []types.AddAddonRef{{
					AssociationID:     pending.ID,
					AddonID:           addonID,
					Cadence:           types.AddonCadenceRecurring,
					ProrationBehavior: types.ProrationBehaviorCreateProrations,
					StartDate:         attach.getRequestedStart(),
				}},
			},
		}),
		CheckoutInvoiceID: &draft.ID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	return session, pending, draft
}

// Payment succeeded: the addon takes effect, the SAME draft is finalized, and no second charge
// appears — the charge exists exactly once, on the invoice the customer paid.
func (s *SubscriptionServiceSuite) TestCompleteAddAddonCheckout_ActivatesAndFinalizes() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	addonID := "addon_complete_activates"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	session, pending, draft := s.seedPayFirstAddonCheckout(addonID)

	oneOffBefore := len(s.oneOffInvoicesFor(sub.ID))
	s.Empty(s.addonLineItemsFor(sub.ID, addonID), "line items must not exist before payment")

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_addon_complete_001",
	}))

	activated, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, activated.AddonStatus, "payment must activate the association")

	s.Len(s.addonLineItemsFor(sub.ID, addonID), 1, "completion must create the addon's line items")

	finalized, err := NewInvoiceService(subService.ServiceParams).GetInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)

	s.Equal(oneOffBefore, len(s.oneOffInvoicesFor(sub.ID)),
		"completion must finalize the existing draft, never raise a second proration charge")

	completed, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, completed.CheckoutStatus)
}

// CompleteCheckoutSession runs the action BEFORE the atomic MarkCompleted claim, so two
// concurrent webhooks can both enter completion. The association's own status is the replay
// fingerprint that makes the second one a no-op.
func (s *SubscriptionServiceSuite) TestCompleteAddAddonCheckout_ReplayIsIdempotent() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	addonID := "addon_complete_replay"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	session, pending, _ := s.seedPayFirstAddonCheckout(addonID)

	cfg := session.Configuration.ToCheckoutConfiguration()

	// Applied directly, twice, so the second call is not short-circuited by the session's own
	// terminal-status guard — this isolates the association-level replay guard.
	s.Require().NoError(subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams))
	s.Require().NoError(subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams))

	s.Len(s.addonLineItemsFor(sub.ID, addonID), 1, "a replay must not duplicate line items")

	assocFilter := types.NewNoLimitAddonAssociationFilter()
	assocFilter.EntityIDs = []string{sub.ID}
	assocFilter.AddonIDs = []string{addonID}
	associations, err := s.GetStores().AddonAssociationRepo.List(ctx, assocFilter)
	s.Require().NoError(err)
	s.Len(associations, 1, "a replay must not create a second association")
	s.Equal(pending.ID, associations[0].ID)
	s.Equal(types.AddonStatusActive, associations[0].AddonStatus)

	s.Equal(1, len(s.oneOffInvoicesFor(sub.ID)), "a replay must not raise another charge")
}

// Paid-but-unactivatable: the subscription was cancelled while the checkout was outstanding.
// The attach must fail loudly and leave a clean, resumable state rather than half-applying.
func (s *SubscriptionServiceSuite) TestCompleteAddAddonCheckout_SubscriptionCancelled() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	addonID := "addon_complete_cancelled"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	session, pending, draft := s.seedPayFirstAddonCheckout(addonID)

	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	cfg := session.Configuration.ToCheckoutConfiguration()
	err := subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusPending, stored.AddonStatus, "a failed activation must leave the association pending")

	s.Empty(s.addonLineItemsFor(sub.ID, addonID))

	stillDraft, err := NewInvoiceService(subService.ServiceParams).GetInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, stillDraft.InvoiceStatus, "the invoice must not be finalized when the attach fails")
}

// An expired or failed session must take its pending association with it, or the association
// outlives the session as an orphan that RemoveAddonFromSubscription refuses to touch.
func (s *SubscriptionServiceSuite) TestAddAddonCheckout_CleanupArchivesPendingAssociation() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription
	addonID := "addon_cleanup_pending"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	session, pending, draft := s.seedPayFirstAddonCheckout(addonID)

	lineItemsBefore := len(sub.LineItems)

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, nil))

	archived, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusArchived, archived.Status, "cleanup must archive the pending association")

	// The generic cleanup block archives the draft. Asserted on status, not retrievability: the
	// repository soft deletes, so the row stays readable.
	archivedDraft, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusDeleted, archivedDraft.Status,
		"the draft proration invoice must be archived by cleanup")

	cleaned, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, cleaned.CheckoutStatus)

	// The subscription and its own line items are untouched.
	storedSub, storedItems, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, storedSub.SubscriptionStatus)
	s.Equal(lineItemsBefore, len(storedItems))
}

// A session that completed and only later expired must not archive the addon it activated.
func (s *SubscriptionServiceSuite) TestAddAddonCheckout_CleanupLeavesActivatedAssociationAlone() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	addonID := "addon_cleanup_active"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	session, pending, _ := s.seedPayFirstAddonCheckout(addonID)

	cfg := session.Configuration.ToCheckoutConfiguration()
	s.Require().NoError(subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams))

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, nil))

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, stored.AddonStatus)
	s.Equal(types.StatusPublished, stored.Status, "a live addon must survive a late session expiry")
}

// =============================================================================
// Characterisation: today's addon attach/detach entity footprint
// =============================================================================

// Characterisation baseline for addon attach/detach (plan Group A / PR A1).
//
// Each test drives ONE change through the real path and then asserts the whole entity
// footprint it leaves behind — association, line items, credit grants, entitlement grants
// and the money — rather than the one entity the change is nominally about. The batch
// refactor moves all five of these across service boundaries at once, so a test that pins
// only one of them cannot tell a clean move from a dropped write.

// -----------------------------------------------------------------------------
// fixtures
// -----------------------------------------------------------------------------

// fullFeaturedAddon carries everything one attach can touch: a fixed ADVANCE price (so it
// prorates), a metered entitlement on featureID (entitlement grants) and a recurring
// credit grant template (credit grants).
func (s *SubscriptionServiceSuite) seedFullFeaturedAddon(addonID, ecID, featureID string, amount int64, quota int64) {
	ctx := s.GetContext()

	s.NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        addonID,
		LookupKey: addonID,
		Name:      addonID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + addonID,
		Amount:             decimal.NewFromInt(amount),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))

	s.seedGrantEC(ecID, featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, addonID, quota,
		types.EntitlementAggregationModeAdditive)

	_, err := NewCreditGrantService(s.service.(*subscriptionService).ServiceParams).
		CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
			Name:           addonID + " credits",
			Scope:          types.CreditGrantScopeAddon,
			AddonID:        lo.ToPtr(addonID),
			Credits:        decimal.NewFromInt(100),
			Cadence:        types.CreditGrantCadenceRecurring,
			Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
			PeriodCount:    lo.ToPtr(1),
			ExpirationType: types.CreditGrantExpiryTypeBillingCycle,
			Priority:       lo.ToPtr(1),
		})
	s.Require().NoError(err)
}

// grantsFromAddon returns the SUBSCRIPTION-scoped credit grants materialised from addonID.
func (s *SubscriptionServiceSuite) grantsFromAddon(addonID string) []*creditgrant.CreditGrant {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	grants, err := s.GetStores().CreditGrantRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)

	return lo.Filter(grants, func(g *creditgrant.CreditGrant, _ int) bool {
		return lo.FromPtr(g.AddonID) == addonID
	})
}

// prorationCredits returns only the wallet transactions settlement raises. Credit-grant
// applications top up under the same transaction reason, so the proration source tag on the
// metadata is what separates the two.
func (s *SubscriptionServiceSuite) prorationCredits() []*wallet.Transaction {
	ctx := s.GetContext()
	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, s.testData.customer.ID)
	s.Require().NoError(err)

	credits := make([]*wallet.Transaction, 0)
	for _, w := range wallets {
		filter := types.NewNoLimitWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(w.ID)
		txns, err := s.GetStores().WalletRepo.ListWalletTransactions(ctx, filter)
		s.Require().NoError(err)

		credits = append(credits, lo.Filter(txns, func(t *wallet.Transaction, _ int) bool {
			return t.TransactionReason == types.TransactionReasonSubscriptionCredit &&
				t.Metadata["source"] == "subscription_change_proration"
		})...)
	}
	return credits
}

// monthlyPeriodSubscription widens the shared 7-day fixture window to a whole month, so a
// monthly addon price is prorated against a period it fits in and the numbers are readable.
func (s *SubscriptionServiceSuite) monthlyPeriodSubscription() *subscription.Subscription {
	sub := s.testData.subscription
	sub.CurrentPeriodEnd = sub.CurrentPeriodStart.AddDate(0, 1, 0)
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	return sub
}

// -----------------------------------------------------------------------------
// attach
// -----------------------------------------------------------------------------

// One mid-period attach, every entity it writes. Anything this test stops seeing after the
// refactor is a write the new orchestrator dropped.
func (s *SubscriptionServiceSuite) TestCharacteriseAddon_Attach_FullEntityFootprint() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	addonID := "addon_char_attach"
	featureID := s.seedGrantFeature("feat_char_attach")
	s.seedFullFeaturedAddon(addonID, "ent_char_attach", featureID, 30, 400)

	// A live window the evaluator has already opened, so the attach has a predecessor to close.
	planEC := s.seedGrantEC("ent_char_attach_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		s.testData.plan.ID, 500, types.EntitlementAggregationModeAdditive)
	existing := s.seedCycleGrant(planEC.ID, featureID, 500)

	attachAt := sub.CurrentPeriodStart.Add(24 * time.Hour)
	result, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(attachAt),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}, nil)
	s.Require().NoError(err)

	s.Run("association", func() {
		s.Require().NotNil(result.Association)
		stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, result.Association.ID)
		s.Require().NoError(err)

		s.Equal(types.AddonStatusActive, stored.AddonStatus)
		s.Equal(addonID, stored.AddonID)
		s.Equal(sub.ID, stored.EntityID)
		s.Equal(types.AddonAssociationEntityTypeSubscription, stored.EntityType)
		s.Require().NotNil(stored.StartDate)
		s.True(stored.StartDate.Equal(attachAt), "the association starts when the caller asked")
		s.Nil(stored.EndDate, "a recurring attach is open-ended")
	})

	s.Run("line_items", func() {
		items := s.addonLineItemsFor(sub.ID, addonID)
		s.Require().Len(items, 1, "one price on the addon, one line item")

		item := items[0]
		s.Equal("price_"+addonID, item.PriceID)
		s.Equal(types.SubscriptionLineItemEntityTypeAddon, item.EntityType)
		s.Equal(addonID, item.EntityID)
		s.Require().NotNil(item.AddonAssociationID)
		s.Equal(result.Association.ID, *item.AddonAssociationID,
			"the line item is keyed to its association — the batch path must keep that link")
		s.True(item.StartDate.Equal(attachAt))
		s.True(item.EndDate.IsZero(), "a recurring attach leaves the line open")
	})

	s.Run("credit_grants", func() {
		grants := s.grantsFromAddon(addonID)
		s.Require().Len(grants, 1, "the addon's ADDON-scoped template is cloned onto the subscription")

		g := grants[0]
		s.Equal(types.CreditGrantScopeSubscription, g.Scope, "materialised grants are subscription-scoped")
		s.Equal(sub.ID, lo.FromPtr(g.SubscriptionID))
		s.Equal(addonID, lo.FromPtr(g.AddonID), "addon provenance is what detach targets")
		s.True(g.Credits.Equal(decimal.NewFromInt(100)))
	})

	s.Run("entitlement_grants", func() {
		rows := s.sortedGrantsForFeature(featureID)
		s.Require().Len(rows, 2, "one close, one open — exactly one segment boundary for one change")

		closed := rows[0]
		s.Equal(existing.ID, closed.ID, "the live window is closed in place, not replaced")
		// Stores round timestamps, so the boundary is pinned to the second; what must be
		// exact is the tiling below.
		s.WithinDuration(attachAt, closed.ValidTo, time.Second,
			"the predecessor closes at the attach: got %s want %s", closed.ValidTo, attachAt)

		successor := rows[1]
		s.True(successor.ValidFrom.Equal(closed.ValidTo),
			"successor must tile onto the predecessor: %s vs %s", successor.ValidFrom, closed.ValidTo)
		s.True(successor.ValidTo.Equal(sub.CurrentPeriodEnd))
		s.True(successor.Quota.GreaterThan(decimal.Zero))
		s.True(successor.Quota.LessThan(decimal.NewFromInt(900)),
			"the successor's pooled quota is prorated for the remaining cycle, got %s", successor.Quota)
	})

	s.Run("money", func() {
		invoices := s.oneOffInvoicesFor(sub.ID)
		s.Require().Len(invoices, 1, "one attach, one ONE_OFF proration invoice")
		s.Equal(string(types.InvoiceBillingReasonSubscriptionUpdate), invoices[0].BillingReason)
		s.True(invoices[0].AmountDue.GreaterThan(decimal.Zero))
		s.True(invoices[0].AmountDue.LessThan(decimal.NewFromInt(30)),
			"a mid-period attach bills less than a whole period, got %s", invoices[0].AmountDue)

		s.Empty(s.prorationCredits(), "an attach charges; it must not credit the wallet")
	})
}

// With proration off the attach still writes every entity, but moves no money and grants
// the full quota. This is the shape create-subscription's Addons[] loop uses.
func (s *SubscriptionServiceSuite) TestCharacteriseAddon_Attach_ProrationNone_WritesEntitiesWithoutMoney() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	addonID := "addon_char_attach_none"
	featureID := s.seedGrantFeature("feat_char_attach_none")
	s.seedFullFeaturedAddon(addonID, "ent_char_attach_none", featureID, 30, 400)

	result, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart.Add(24 * time.Hour)),
		ProrationBehavior: types.ProrationBehaviorNone,
	}, nil)
	s.Require().NoError(err)

	s.Equal(types.AddonStatusActive, result.Association.AddonStatus)
	s.Len(s.addonLineItemsFor(sub.ID, addonID), 1)
	s.Len(s.grantsFromAddon(addonID), 1, "credit grants are not gated on proration behaviour")
	s.Empty(s.oneOffInvoicesFor(sub.ID), "no proration, no invoice")
	s.Empty(s.prorationCredits())

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "no predecessor to close, so one fresh window")
	s.True(rows[0].Quota.Equal(decimal.NewFromInt(400)), "behaviour none grants the full quota, got %s", rows[0].Quota)
}

// -----------------------------------------------------------------------------
// detach
// -----------------------------------------------------------------------------

// The mirror image: one detach, every entity it touches. This is the footprint the shared
// removal seam must reproduce for plan change's dropped addons.
func (s *SubscriptionServiceSuite) TestCharacteriseAddon_Detach_FullEntityFootprint() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	addonID := "addon_char_detach"
	featureID := s.seedGrantFeature("feat_char_detach")
	s.seedFullFeaturedAddon(addonID, "ent_char_detach", featureID, 30, 400)

	attached, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}, nil)
	s.Require().NoError(err)

	addonLine := s.addonLineItemsFor(sub.ID, addonID)
	s.Require().Len(addonLine, 1)

	// Give the removal a billed basis, or the credit caps at zero.
	s.recordBilledForLineItem(addonLine[0].ID, decimal.NewFromInt(30))
	s.Require().NotEmpty(s.sortedGrantsForFeature(featureID))

	detachAt := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)

	// The credit is quoted before the removal is executed. The in-memory line-item store
	// hands out the stored pointer, so persisting the removal stamps EndDate on the very
	// object the settlement quote reads, and Compute then skips it as a non-refundable
	// onetime item. Against a real DB the params carry their own snapshot and the credit
	// is issued, so the quote — not the executed top-up — is what this suite can pin.
	quoted, err := subSvc.detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: attached.Association.ID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		EffectiveDate:      lo.ToPtr(detachAt),
		PreviewOnly:        true,
	}, sub.ID)
	s.Require().NoError(err)

	result, err := subSvc.detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: attached.Association.ID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		EffectiveDate:      lo.ToPtr(detachAt),
		Reason:             "characterisation",
	}, sub.ID)
	s.Require().NoError(err)

	s.Run("association", func() {
		stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, attached.Association.ID)
		s.Require().NoError(err)

		s.Equal(types.AddonStatusCancelled, stored.AddonStatus)
		s.Require().NotNil(stored.EndDate)
		s.True(stored.EndDate.Equal(detachAt), "the association ends on the effective date")
		s.Equal("characterisation", stored.CancellationReason)
	})

	s.Run("line_items", func() {
		stored, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, addonLine[0].ID)
		s.Require().NoError(err)
		s.False(stored.EndDate.IsZero(), "the line item closes with its association")
		s.True(stored.EndDate.Equal(detachAt), "and closes on the same date")

		s.Require().Len(result.EndedLineItems, 1)
		s.Equal(addonLine[0].ID, result.EndedLineItems[0].ID)
	})

	s.Run("credit_grants", func() {
		grants := s.grantsFromAddon(addonID)
		s.Require().Len(grants, 1, "the grant row survives; only its future applications stop")

		filter := types.NewNoLimitCreditGrantApplicationFilter()
		filter.CreditGrantIDs = []string{grants[0].ID}
		filter.SubscriptionIDs = []string{sub.ID}
		applications, err := s.GetStores().CreditGrantApplicationRepo.List(ctx, filter)
		s.Require().NoError(err)

		for _, app := range applications {
			if app.ScheduledFor.After(detachAt) {
				s.Equal(types.ApplicationStatusCancelled, app.ApplicationStatus,
					"applications scheduled after the removal must be cancelled, not left pending")
			}
		}
	})

	// The attach's window was opened but never evaluated, so the removal DELETEs it rather
	// than closing it — a live window would otherwise keep granting quota the customer no
	// longer pays for. This is the behaviour the merged close/open pass has to preserve.
	s.Run("entitlement_grants", func() {
		s.Empty(s.sortedGrantsForFeature(featureID),
			"a never-evaluated window is removed outright, not left open")
	})

	s.Run("money", func() {
		s.Require().Len(quoted.ChangedInvoices, 1, "the removal quotes exactly one document")
		s.Equal(dto.ChangedInvoiceStatusPreview, quoted.ChangedInvoices[0].Status)
		s.Require().NotNil(quoted.ChangedInvoices[0].WalletTransaction)
		s.True(quoted.ChangedInvoices[0].WalletTransaction.Amount.GreaterThan(decimal.Zero),
			"half a period of a prepaid addon comes back as a credit")

		s.Len(s.oneOffInvoicesFor(sub.ID), 1,
			"only the attach's invoice: a removal never raises a second document")
	})
}

// A detach with proration off still removes everything; it just issues no credit. The
// removal seam has to keep these two halves independently switchable.
func (s *SubscriptionServiceSuite) TestCharacteriseAddon_Detach_ProrationNone_RemovesWithoutCredit() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	addonID := "addon_char_detach_none"
	featureID := s.seedGrantFeature("feat_char_detach_none")
	s.seedFullFeaturedAddon(addonID, "ent_char_detach_none", featureID, 30, 400)

	attached, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart),
		ProrationBehavior: types.ProrationBehaviorNone,
	}, nil)
	s.Require().NoError(err)

	detachAt := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	_, err = subSvc.detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: attached.Association.ID,
		ProrationBehavior:  types.ProrationBehaviorNone,
		EffectiveDate:      lo.ToPtr(detachAt),
	}, sub.ID)
	s.Require().NoError(err)

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, attached.Association.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusCancelled, stored.AddonStatus, "the addon still goes away")

	s.Empty(s.prorationCredits(), "but no credit is issued")
	s.Empty(s.oneOffInvoicesFor(sub.ID))
}

// recordBilledForLineItem writes the invoice line the credit cap reads, so a removal's
// credit has a basis other than list price.
func (s *SubscriptionServiceSuite) recordBilledForLineItem(lineItemID string, amount decimal.Decimal) {
	ctx := s.GetContext()
	sub := s.testData.subscription
	periodStart := sub.CurrentPeriodStart
	periodEnd := sub.CurrentPeriodEnd

	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             sub.CustomerID,
		SubscriptionID:         &sub.ID,
		SubscriptionLineItemID: &lineItemID,
		Amount:                 amount,
		Quantity:               decimal.NewFromInt(1),
		Currency:               "usd",
		PeriodStart:            &periodStart,
		PeriodEnd:              &periodEnd,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}))
}

// -----------------------------------------------------------------------------
// transaction boundary
// -----------------------------------------------------------------------------

// Settlement runs inside the change's transaction, so failing to raise the proration invoice now
// fails the attach instead of returning success with the addon attached and unbilled. The rollback
// itself is the transaction's job and is not observable here: testutil runs WithTx without one.
func (s *SubscriptionServiceSuite) TestAddonChange_SettlementFailure_FailsTheAttach() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	addonID := "addon_settle_failure"
	featureID := s.seedGrantFeature("feat_settle_failure")
	s.seedFullFeaturedAddon(addonID, "ent_settle_failure", featureID, 30, 400)

	// Bill a customer that does not exist: everything resolves and persists, and only the
	// settlement at the end of the transaction fails.
	sub.InvoicingCustomerID = lo.ToPtr("cust_missing_settle_failure")
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart.Add(24 * time.Hour)),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}, nil)
	s.Require().Error(err, "an attach whose settlement cannot raise its invoice must fail")
}

// =============================================================================
// Characterisation: addon preview twins and the pay-first draft key
// =============================================================================

// The attach preview is a second implementation of settlement (previewAddonSettlement),
// separate from Apply. It must quote what execute then bills, and write nothing.
func (s *SubscriptionServiceSuite) TestCharacteriseSettlement_AttachPreviewTwinMatchesExecute() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()
	addonID := "addon_characterise_preview"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(24 * time.Hour)
	req := func() *dto.AddAddonToSubscriptionRequest {
		return &dto.AddAddonToSubscriptionRequest{
			AddonID:           addonID,
			Cadence:           types.AddonCadenceRecurring,
			StartDate:         lo.ToPtr(at),
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		}
	}

	previewReq := req()
	previewReq.PreviewOnly = true
	preview, err := subSvc.attachAddon(ctx, sub, previewReq, nil)
	s.Require().NoError(err)
	s.Require().Len(preview.ChangedInvoices, 1, "the preview twin quotes one document")
	s.Equal(dto.ChangedInvoiceStatusPreview, preview.ChangedInvoices[0].Status)
	quoted := preview.ChangedInvoices[0].Invoice.AmountDue

	// Preview writes nothing: no association, no line item, no invoice.
	s.Empty(s.addonLineItemsFor(sub.ID, addonID))
	s.Empty(s.oneOffInvoicesFor(sub.ID))

	executed, err := subSvc.attachAddon(ctx, sub, req(), nil)
	s.Require().NoError(err)
	s.Require().Len(executed.ChangedInvoices, 1)
	charged := executed.ChangedInvoices[0].Invoice.AmountDue

	s.True(quoted.Equal(charged), "the preview twin quoted %s but execute billed %s", quoted, charged)
	s.Require().Len(s.oneOffInvoicesFor(sub.ID), 1, "execute raises exactly one invoice")
}

// The detach preview quotes a wallet credit as a synthetic transaction rather than a
// document — a second shape the twin has to keep in step with Apply's real top-up.
func (s *SubscriptionServiceSuite) TestCharacteriseSettlement_DetachPreviewTwinQuotesWalletCredit() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()
	addonID := "addon_characterise_detach_preview"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	attached, err := subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}, nil)
	s.Require().NoError(err)

	preview, err := subSvc.detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: attached.Association.ID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		EffectiveDate:      lo.ToPtr(sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)),
		PreviewOnly:        true,
	}, sub.ID)
	s.Require().NoError(err)

	s.Require().Len(preview.ChangedInvoices, 1)
	s.Equal(dto.ChangedInvoiceStatusPreview, preview.ChangedInvoices[0].Status)
	s.Require().NotNil(preview.ChangedInvoices[0].WalletTransaction)
	s.True(preview.ChangedInvoices[0].WalletTransaction.Amount.GreaterThan(decimal.Zero),
		"a mid-period removal of a prepaid addon quotes a positive credit")

	// The quoted cancellation is built but not stored.
	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, attached.Association.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, stored.AddonStatus, "preview must not cancel the association")
	s.Nil(stored.EndDate)
}

// Pay-first and pay-later must stamp the SAME key on the same economic event, or the two never
// dedupe against each other. Pay-first used to pass the association's raw key straight through
// while pay-later hashed it; hoisting pay-first onto the shared spine closed that.
func (s *SubscriptionServiceSuite) TestCharacteriseSettlement_PayFirstDraft_SharesThePayLaterKey() {
	ctx := s.GetContext()
	subSvc := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()
	addonID := "addon_characterise_draft_key"

	s.seedFixedPriceAddon(addonID, decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	config, err := NewAddonChangeService(subSvc.ServiceParams).Resolve(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{{Request: &dto.AddAddonToSubscriptionRequest{
			AddonID:           addonID,
			Cadence:           types.AddonCadenceRecurring,
			StartDate:         lo.ToPtr(sub.CurrentPeriodStart.Add(24 * time.Hour)),
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		}}},
	})
	s.Require().NoError(err)

	params := config.getAttaches()[0]
	summary := config.getQuote()
	s.Require().True(summary.TotalChargeAmount.GreaterThan(decimal.Zero))

	rawKey := params.prorationIdempotencyKey()
	hashedKey := prorationChargeInvoiceKey(LineItemProrationRequest{
		Subscription:   sub,
		EffectiveDate:  config.getPeriodStart(),
		IdempotencyKey: rawKey,
	})

	// Both modes settle off config.getIdempotencyKey(), so the draft and the pay-later invoice
	// cannot drift apart.
	s.Equal(hashedKey, config.getIdempotencyKey(),
		"a single attach hashes its key, whether it is drafted or issued")
	s.NotEqual(rawKey, config.getIdempotencyKey(),
		"the raw association key is no longer stamped on the document")

	drafted, err := NewLineItemProrationService(subSvc.ServiceParams).Settle(ctx, NewSettleProrationRequest(
		sub, summary, config.getPeriodStart(), sub.CurrentPeriodEnd,
		"Subscription update", config.getIdempotencyKey(), SettleModeDraft,
	))
	s.Require().NoError(err)
	draft := drafted.Draft

	s.Equal(hashedKey, lo.FromPtr(draft.IdempotencyKey))

	// What the draft locks must still be exactly the pay-later amount.
	s.Equal(types.InvoiceStatusDraft, draft.InvoiceStatus)
	s.Equal(types.InvoiceSourceTypeCheckout, draft.SourceType)
	s.True(summary.TotalChargeAmount.Equal(draft.AmountDue))
}
