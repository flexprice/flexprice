package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// InvoiceLifecycleSuite tests the invoice lifecycle: one-off creation, draft
// creation for subscriptions, compute (incl. idempotency), finalization,
// payment status reconciliation, void metadata, and small helper methods.
type InvoiceLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	service  InvoiceService
	testData struct {
		now time.Time

		customer *customer.Customer
		plan     *plan.Plan
		price    *price.Price // fixed advance $50/mo
		sub      *subscription.Subscription

		// subscription with zero charges (plan without prices, no line items)
		emptyPlan *plan.Plan
		emptySub  *subscription.Subscription
	}
}

func TestInvoiceLifecycle(t *testing.T) {
	suite.Run(t, new(InvoiceLifecycleSuite))
}

func (s *InvoiceLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *InvoiceLifecycleSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()
	now := s.testData.now

	s.testData.customer = &customer.Customer{
		ID:         "cust_lc",
		ExternalID: "ext_cust_lc",
		Name:       "Lifecycle Customer",
		Email:      "lc@test.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_lc",
		Name:      "Lifecycle Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.price = &price.Price{
		ID:                 "price_lc_fixed",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))

	periodStart := now.Add(-48 * time.Hour)
	periodEnd := now.Add(6 * 24 * time.Hour)
	s.testData.sub = &subscription.Subscription{
		ID:                 "sub_lc",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      periodEnd,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := []*subscription.SubscriptionLineItem{
		{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.testData.sub.ID,
			CustomerID:      s.testData.customer.ID,
			EntityID:        s.testData.plan.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         s.testData.price.ID,
			PriceType:       s.testData.price.Type,
			DisplayName:     "Fixed Plan Fee",
			Quantity:        decimal.NewFromInt(1),
			Currency:        "usd",
			BillingPeriod:   types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       s.testData.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.sub, lineItems))

	// Zero-charge subscription: plan without prices and no subscription line items.
	s.testData.emptyPlan = &plan.Plan{
		ID:        "plan_lc_empty",
		Name:      "Empty Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.emptyPlan))

	s.testData.emptySub = &subscription.Subscription{
		ID:                 "sub_lc_empty",
		PlanID:             s.testData.emptyPlan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      periodEnd,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.emptySub, nil))
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) oneOffRequest(mutators ...func(*dto.CreateInvoiceRequest)) dto.CreateInvoiceRequest {
	req := dto.CreateInvoiceRequest{
		CustomerID:  s.testData.customer.ID,
		InvoiceType: types.InvoiceTypeOneOff,
		Currency:    "usd",
		Subtotal:    decimal.NewFromInt(100),
		Total:       decimal.NewFromInt(100),
		AmountDue:   decimal.NewFromInt(100),
		LineItems: []dto.CreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Item A"),
				Amount:      decimal.NewFromInt(60),
				Quantity:    decimal.NewFromInt(1),
			},
			{
				DisplayName: lo.ToPtr("Item B"),
				Amount:      decimal.NewFromInt(40),
				Quantity:    decimal.NewFromInt(2),
			},
		},
	}
	for _, m := range mutators {
		m(&req)
	}
	return req
}

func (s *InvoiceLifecycleSuite) createCoupon(id string, amountOff decimal.Decimal, published bool) *coupon.Coupon {
	c := &coupon.Coupon{
		ID:        id,
		Name:      "Coupon " + id,
		Type:      types.CouponTypeFixed,
		AmountOff: &amountOff,
		Currency:  "USD",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	if !published {
		c.Status = types.StatusArchived
	}
	s.NoError(s.GetStores().CouponRepo.Create(s.GetContext(), c))
	return c
}

func (s *InvoiceLifecycleSuite) createPercentTaxRate(id string, pct decimal.Decimal) *taxrate.TaxRate {
	tr := &taxrate.TaxRate{
		ID:              id,
		Name:            "Tax " + id,
		Code:            id,
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pct,
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaxRateRepo.Create(s.GetContext(), tr))
	return tr
}

// buildStoredInvoice persists an invoice built from the given template.
func (s *InvoiceLifecycleSuite) buildStoredInvoice(inv *invoice.Invoice) *invoice.Invoice {
	if inv.BaseModel.TenantID == "" {
		inv.BaseModel = types.GetDefaultBaseModel(s.GetContext())
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

// buildFinalizedInvoiceFor persists a finalized subscription invoice with the given amounts.
func (s *InvoiceLifecycleSuite) buildFinalizedInvoiceFor(id string, amountDue decimal.Decimal, billingReason string, subID *string) *invoice.Invoice {
	now := s.testData.now
	bp := string(types.BILLING_PERIOD_MONTHLY)
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	return s.buildStoredInvoice(&invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  subID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        amountDue,
		Total:           amountDue,
		AmountDue:       amountDue,
		AmountPaid:      decimal.Zero,
		AmountRemaining: amountDue,
		BillingReason:   billingReason,
		BillingPeriod:   &bp,
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		FinalizedAt:     &now,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateOneOffInvoice
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestCreateOneOffInvoice() {
	s.Run("happy_path_finalizes_and_assigns_invoice_number", func() {
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), s.oneOffRequest())
		s.NoError(err)
		s.Require().NotNil(resp)

		s.Equal(types.InvoiceStatusFinalized, resp.InvoiceStatus)
		s.Require().NotNil(resp.InvoiceNumber)
		s.NotEmpty(*resp.InvoiceNumber)
		s.Equal(types.PaymentStatusPending, resp.PaymentStatus)
		s.True(resp.Subtotal.Equal(decimal.NewFromInt(100)), "subtotal, got %s", resp.Subtotal)
		s.True(resp.Total.Equal(decimal.NewFromInt(100)), "total, got %s", resp.Total)
		s.True(resp.AmountRemaining.Equal(decimal.NewFromInt(100)), "amount_remaining, got %s", resp.AmountRemaining)

		// Read back through the repo.
		stored, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
		s.NotNil(stored.FinalizedAt)
		s.NotNil(stored.LastComputedAt)

		items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Len(items, 2)
	})

	s.Run("valid_coupon_applies_discount", func() {
		c := s.createCoupon("coupon_lc_valid", decimal.NewFromInt(10), true)
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.Coupons = []string{c.ID}
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(10)), "total_discount, got %s", resp.TotalDiscount)
		s.True(resp.Total.Equal(decimal.NewFromInt(90)), "total after discount, got %s", resp.Total)
	})

	s.Run("nonexistent_coupon_is_skipped", func() {
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.Coupons = []string{"coupon_does_not_exist"}
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.True(resp.TotalDiscount.IsZero(), "no discount expected, got %s", resp.TotalDiscount)
		s.True(resp.Total.Equal(decimal.NewFromInt(100)))
	})

	s.Run("unpublished_coupon_is_skipped", func() {
		c := s.createCoupon("coupon_lc_archived", decimal.NewFromInt(10), false)
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.Coupons = []string{c.ID}
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.True(resp.TotalDiscount.IsZero(), "archived coupon must not apply, got %s", resp.TotalDiscount)
	})

	s.Run("tax_rate_id_applies_tax", func() {
		tr := s.createPercentTaxRate("tax_lc_10pct", decimal.NewFromInt(10))
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.TaxRates = []string{tr.ID}
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.True(resp.Total.Equal(decimal.NewFromInt(110)), "total incl tax, got %s", resp.Total)
		s.True(resp.AmountDue.Equal(decimal.NewFromInt(110)))

		// Read back the tax application record (10% of the 100 subtotal).
		// NOTE: stored TotalTax is not asserted here because the in-memory
		// invoice store's copyInvoice drops the TotalTax field (test-infra gap).
		taxFilter := types.NewNoLimitTaxAppliedFilter()
		taxFilter.EntityType = types.TaxRateEntityTypeInvoice
		taxFilter.EntityID = resp.ID
		applied, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), taxFilter)
		s.NoError(err)
		s.Require().Len(applied, 1)
		s.True(applied[0].TaxAmount.Equal(decimal.NewFromInt(10)), "tax amount, got %s", applied[0].TaxAmount)
		s.True(applied[0].TaxableAmount.Equal(decimal.NewFromInt(100)), "taxable amount, got %s", applied[0].TaxableAmount)
	})

	s.Run("nonexistent_tax_rate_returns_error", func() {
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.TaxRates = []string{"tax_does_not_exist"}
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("missing_customer_id_returns_validation_error", func() {
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.CustomerID = ""
		})
		resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("duplicate_idempotency_key_returns_already_exists", func() {
		key := "idem_lc_dup"
		req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
			r.IdempotencyKey = &key
		})
		first, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(first)
		s.Equal(types.InvoiceStatusFinalized, first.InvoiceStatus)

		// Same key again: the first invoice is finalized, so creation must fail.
		second, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
		s.Error(err)
		s.True(ierr.IsAlreadyExists(err), "expected already-exists error, got %v", err)
		s.Nil(second)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateEmptyDraftInvoice idempotency
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestCreateEmptyDraftInvoiceIdempotency() {
	s.Run("same_idempotency_key_returns_existing_draft", func() {
		key := "idem_lc_draft"
		req := dto.CreateDraftInvoiceRequest{
			CustomerID:     s.testData.customer.ID,
			InvoiceType:    types.InvoiceTypeOneOff,
			Currency:       "usd",
			IdempotencyKey: &key,
		}
		first, err := s.service.CreateEmptyDraftInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(first)
		s.Equal(types.InvoiceStatusDraft, first.InvoiceStatus)
		s.True(first.Total.IsZero())
		s.Nil(first.InvoiceNumber)

		second, err := s.service.CreateEmptyDraftInvoice(s.GetContext(), req)
		s.NoError(err)
		s.Require().NotNil(second)
		s.Equal(first.ID, second.ID, "same idempotency key must return the same draft")
	})

	s.Run("validation_error_for_subscription_without_period", func() {
		req := dto.CreateDraftInvoiceRequest{
			CustomerID:     s.testData.customer.ID,
			SubscriptionID: lo.ToPtr(s.testData.sub.ID),
			InvoiceType:    types.InvoiceTypeSubscription,
			Currency:       "usd",
		}
		resp, err := s.service.CreateEmptyDraftInvoice(s.GetContext(), req)
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateDraftInvoiceForSubscription
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestCreateDraftInvoiceForSubscription() {
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd

	s.Run("creates_zero_dollar_draft_without_invoice_number", func() {
		resp, err := s.service.CreateDraftInvoiceForSubscription(
			s.GetContext(), s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(types.InvoiceStatusDraft, resp.InvoiceStatus)
		s.Equal(types.InvoiceTypeSubscription, resp.InvoiceType)
		s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), resp.BillingReason)
		s.True(resp.Total.IsZero())
		s.True(resp.Subtotal.IsZero())
		s.Nil(resp.InvoiceNumber)
		s.Require().NotNil(resp.SubscriptionID)
		s.Equal(s.testData.sub.ID, *resp.SubscriptionID)
		s.Require().NotNil(resp.PeriodStart)
		s.True(resp.PeriodStart.Equal(periodStart))

		// Read back and confirm the draft was persisted with no line items.
		stored, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
		items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Empty(items)
	})

	s.Run("second_call_for_same_period_returns_same_draft", func() {
		first, err := s.service.CreateDraftInvoiceForSubscription(
			s.GetContext(), s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.NoError(err)
		second, err := s.service.CreateDraftInvoiceForSubscription(
			s.GetContext(), s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.NoError(err)
		s.Equal(first.ID, second.ID, "draft creation must be idempotent per period")
	})

	s.Run("cancel_reference_point_uses_proration_billing_reason", func() {
		// Use a distinct period so we do not collide with the cycle draft above.
		cancelStart := periodStart.Add(1 * time.Hour)
		resp, err := s.service.CreateDraftInvoiceForSubscription(
			s.GetContext(), s.testData.sub.ID, cancelStart, periodEnd, types.ReferencePointCancel)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(string(types.InvoiceBillingReasonProration), resp.BillingReason)
	})

	s.Run("subscription_not_found_returns_error", func() {
		resp, err := s.service.CreateDraftInvoiceForSubscription(
			s.GetContext(), "sub_does_not_exist", periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.Error(err)
		s.Nil(resp)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeInvoice — including the idempotency invariant
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestComputeInvoiceIdempotency() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd

	draft, err := s.service.CreateDraftInvoiceForSubscription(
		ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
	s.Require().NoError(err)

	// First compute populates line items and totals.
	skipped, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)

	afterFirst, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Require().NotNil(afterFirst.LastComputedAt, "compute must set last_computed_at")
	s.True(afterFirst.Subtotal.IsPositive(), "fixed advance charge must produce a positive subtotal, got %s", afterFirst.Subtotal)
	itemsFirst, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, draft.ID)
	s.Require().NoError(err)
	s.Require().NotEmpty(itemsFirst)

	// Second compute must NOT double-bill: same totals, same line item count.
	skipped, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)

	afterSecond, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.True(afterSecond.Subtotal.Equal(afterFirst.Subtotal),
		"recompute must not change subtotal: first=%s second=%s", afterFirst.Subtotal, afterSecond.Subtotal)
	s.True(afterSecond.Total.Equal(afterFirst.Total),
		"recompute must not change total: first=%s second=%s", afterFirst.Total, afterSecond.Total)
	s.True(afterSecond.AmountDue.Equal(afterFirst.AmountDue),
		"recompute must not change amount_due: first=%s second=%s", afterFirst.AmountDue, afterSecond.AmountDue)

	itemsSecond, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, draft.ID)
	s.Require().NoError(err)
	s.Len(itemsSecond, len(itemsFirst), "recompute must not duplicate line items")
}

func (s *InvoiceLifecycleSuite) TestComputeInvoiceStatusHandling() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd

	s.Run("finalized_invoice_is_not_recomputed", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_final_compute", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), lo.ToPtr(s.testData.sub.ID))

		skipped, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
		s.NoError(err)
		s.False(skipped)

		// Totals untouched — the finalized invoice is immutable.
		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AmountDue.Equal(decimal.NewFromInt(100)))
		s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
		s.Nil(stored.LastComputedAt, "compute must not touch a finalized invoice")
	})

	s.Run("zero_charge_subscription_invoice_is_marked_skipped", func() {
		draft, err := s.service.CreateDraftInvoiceForSubscription(
			ctx, s.testData.emptySub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.Require().NoError(err)

		skipped, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
		s.NoError(err)
		s.True(skipped, "zero-dollar subscription invoice must be skipped")

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusSkipped, stored.InvoiceStatus)
		s.NotNil(stored.LastComputedAt)

		// Recompute of a SKIPPED invoice is allowed and, with still-zero usage,
		// results in SKIPPED again (not an error, no state corruption).
		skippedAgain, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
		s.NoError(err)
		s.True(skippedAgain)
		stored, err = s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusSkipped, stored.InvoiceStatus)
	})

	s.Run("subscription_invoice_missing_period_dates_returns_error", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:             "inv_lc_no_period",
			CustomerID:     s.testData.customer.ID,
			SubscriptionID: lo.ToPtr(s.testData.sub.ID),
			InvoiceType:    types.InvoiceTypeSubscription,
			InvoiceStatus:  types.InvoiceStatusDraft,
			PaymentStatus:  types.PaymentStatusPending,
			Currency:       "usd",
			BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		})

		_, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("invoice_not_found_returns_error", func() {
		_, err := s.service.ComputeInvoice(ctx, "inv_does_not_exist", nil)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// FinalizeInvoice + IsFinalizationDue
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestFinalizeInvoiceLifecycle() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd

	s.Run("finalize_computed_draft_assigns_invoice_number", func() {
		draft, err := s.service.CreateDraftInvoiceForSubscription(
			ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
		s.Require().NoError(err)
		_, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
		s.Require().NoError(err)

		s.NoError(s.service.FinalizeInvoice(ctx, draft.ID))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
		s.Require().NotNil(stored.InvoiceNumber)
		s.Contains(*stored.InvoiceNumber, "INV")
		s.NotNil(stored.FinalizedAt)

		// Finalizing again must fail — the invoice is no longer a draft.
		err = s.service.FinalizeInvoice(ctx, draft.ID)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("finalize_skipped_invoice_is_noop", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:            "inv_lc_skipped_fin",
			CustomerID:    s.testData.customer.ID,
			InvoiceType:   types.InvoiceTypeSubscription,
			InvoiceStatus: types.InvoiceStatusSkipped,
			PaymentStatus: types.PaymentStatusPending,
			Currency:      "usd",
		})

		s.NoError(s.service.FinalizeInvoice(ctx, inv.ID))
		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusSkipped, stored.InvoiceStatus, "skipped invoices are never finalized")
		s.Nil(stored.InvoiceNumber)
	})

	s.Run("auto_completed_metadata_marks_invoice_paid_on_finalize", func() {
		amount := decimal.NewFromInt(25)
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:              "inv_lc_autocomplete",
			CustomerID:      s.testData.customer.ID,
			InvoiceType:     types.InvoiceTypeOneOff,
			InvoiceStatus:   types.InvoiceStatusDraft,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			Subtotal:        amount,
			Total:           amount,
			AmountDue:       amount,
			AmountRemaining: amount,
			Metadata:        types.Metadata{"auto_completed": "true"},
		})

		s.NoError(s.service.FinalizeInvoice(ctx, inv.ID))
		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(amount))
		s.True(stored.AmountRemaining.IsZero())
	})

	s.Run("finalize_not_found_returns_error", func() {
		err := s.service.FinalizeInvoice(ctx, "inv_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *InvoiceLifecycleSuite) TestIsFinalizationDue() {
	ctx := s.GetContext()
	now := s.testData.now
	pastPeriodStart := now.Add(-10 * 24 * time.Hour)
	pastPeriodEnd := now.Add(-4 * time.Hour)

	buildDraft := func(id string, lastComputedAt *time.Time, periodEnd time.Time, billingReason string) *invoice.Invoice {
		return s.buildStoredInvoice(&invoice.Invoice{
			ID:             id,
			CustomerID:     s.testData.customer.ID,
			SubscriptionID: lo.ToPtr(s.testData.sub.ID),
			InvoiceType:    types.InvoiceTypeSubscription,
			InvoiceStatus:  types.InvoiceStatusDraft,
			PaymentStatus:  types.PaymentStatusPending,
			Currency:       "usd",
			BillingReason:  billingReason,
			PeriodStart:    &pastPeriodStart,
			PeriodEnd:      &periodEnd,
			LastComputedAt: lastComputedAt,
		})
	}

	s.Run("false_for_uncomputed_draft", func() {
		inv := buildDraft("inv_lc_due_uncomputed", nil, pastPeriodEnd, string(types.InvoiceBillingReasonSubscriptionCycle))
		due, err := s.service.IsFinalizationDue(ctx, inv.ID)
		s.NoError(err)
		s.False(due)
	})

	s.Run("false_for_non_draft_invoice", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_due_finalized", decimal.NewFromInt(10),
			string(types.InvoiceBillingReasonSubscriptionCycle), lo.ToPtr(s.testData.sub.ID))
		due, err := s.service.IsFinalizationDue(ctx, inv.ID)
		s.NoError(err)
		s.False(due)
	})

	s.Run("false_when_computed_before_period_end_for_cycle_invoice", func() {
		lastComputed := now.Add(-5 * time.Hour) // before period end (-4h)
		inv := buildDraft("inv_lc_due_early", &lastComputed, pastPeriodEnd, string(types.InvoiceBillingReasonSubscriptionCycle))
		due, err := s.service.IsFinalizationDue(ctx, inv.ID)
		s.NoError(err)
		s.False(due, "cycle invoices computed before period end are not due")
	})

	s.Run("false_within_finalization_delay", func() {
		lastComputed := now.Add(-1 * time.Minute) // default delay is 2h
		inv := buildDraft("inv_lc_due_within_delay", &lastComputed, pastPeriodEnd, string(types.InvoiceBillingReasonSubscriptionCycle))
		due, err := s.service.IsFinalizationDue(ctx, inv.ID)
		s.NoError(err)
		s.False(due, "delay (default 2h) has not elapsed yet")
	})

	s.Run("true_after_finalization_delay_elapsed", func() {
		lastComputed := now.Add(-3 * time.Hour) // after period end, delay 2h elapsed
		inv := buildDraft("inv_lc_due_elapsed", &lastComputed, pastPeriodEnd, string(types.InvoiceBillingReasonSubscriptionCycle))
		due, err := s.service.IsFinalizationDue(ctx, inv.ID)
		s.NoError(err)
		s.True(due, "computed 3h ago with a 2h delay must be due")
	})

	s.Run("not_found_returns_error", func() {
		_, err := s.service.IsFinalizationDue(ctx, "inv_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *InvoiceLifecycleSuite) TestListAllTenantDraftInvoices() {
	ctx := s.GetContext()

	buildInvoice := func(id string, status types.InvoiceStatus) {
		s.buildStoredInvoice(&invoice.Invoice{
			ID:            id,
			CustomerID:    s.testData.customer.ID,
			InvoiceType:   types.InvoiceTypeOneOff,
			InvoiceStatus: status,
			PaymentStatus: types.PaymentStatusPending,
			Currency:      "usd",
		})
	}
	buildInvoice("inv_lc_alltenant_d1", types.InvoiceStatusDraft)
	buildInvoice("inv_lc_alltenant_d2", types.InvoiceStatusDraft)
	buildInvoice("inv_lc_alltenant_fin", types.InvoiceStatusFinalized)

	drafts, err := s.service.ListAllTenantDraftInvoices(ctx, 100, 0)
	s.NoError(err)
	s.Len(drafts, 2)
	for _, d := range drafts {
		s.Equal(types.InvoiceStatusDraft, d.InvoiceStatus)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// VoidInvoice — metadata merge and wallet auto-creation on refund
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestVoidInvoiceMetadataMerge() {
	ctx := s.GetContext()
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:              "inv_lc_void_meta",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        decimal.NewFromInt(10),
		Total:           decimal.NewFromInt(10),
		AmountDue:       decimal.NewFromInt(10),
		AmountRemaining: decimal.NewFromInt(10),
		Metadata:        types.Metadata{"existing": "keep", "override": "old"},
	})

	err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{
		Metadata: types.Metadata{"override": "new", "added": "yes"},
	})
	s.NoError(err)

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusVoided, stored.InvoiceStatus)
	s.NotNil(stored.VoidedAt)
	s.Equal("keep", stored.Metadata["existing"], "unmentioned keys must be preserved")
	s.Equal("new", stored.Metadata["override"], "request values must override existing keys")
	s.Equal("yes", stored.Metadata["added"], "new keys must be added")
}

func (s *InvoiceLifecycleSuite) TestVoidInvoicePaidRefundCreatesWallet() {
	ctx := s.GetContext()
	paid := decimal.NewFromInt(40)
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:              "inv_lc_void_refund",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusSucceeded,
		Currency:        "usd",
		Subtotal:        paid,
		Total:           paid,
		AmountDue:       paid,
		AmountPaid:      paid,
		AmountRemaining: decimal.Zero,
	})

	// No wallet exists for the customer yet — void must create one and refund into it.
	err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.NoError(err)

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusVoided, stored.InvoiceStatus)
	s.Equal(types.PaymentStatusRefunded, stored.PaymentStatus)
	s.True(stored.RefundedAmount.Equal(paid), "refunded_amount, got %s", stored.RefundedAmount)

	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, s.testData.customer.ID)
	s.NoError(err)
	s.Require().Len(wallets, 1, "a prepaid wallet must be auto-created for the refund")
	s.Equal(types.WalletTypePrePaid, wallets[0].WalletType)
	s.True(wallets[0].Balance.Equal(paid), "wallet balance must hold the refund, got %s", wallets[0].Balance)
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdatePaymentStatus — guard rails
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestUpdatePaymentStatusGuards() {
	ctx := s.GetContext()

	s.Run("rejected_when_succeeded_payment_records_exist", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_ups_payment", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)

		p := &payment.Payment{
			ID:                "pay_lc_1",
			IdempotencyKey:    "pay_lc_1_idem",
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypeOffline,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			PaymentStatus:     types.PaymentStatusSucceeded,
			BaseModel:         types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PaymentRepo.Create(ctx, p))

		err := s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err), "manual updates are blocked when payments exist, got %v", err)
	})

	s.Run("negative_amount_is_rejected", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_ups_negative", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		neg := decimal.NewFromInt(-5)
		err := s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &neg)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("pending_with_amount_updates_amount_paid", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_ups_pending", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		amount := decimal.NewFromInt(30)
		s.NoError(s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &amount))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusPending, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(amount))
		s.True(stored.AmountRemaining.Equal(decimal.NewFromInt(70)))
	})

	s.Run("voided_invoice_is_rejected", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:            "inv_lc_ups_voided",
			CustomerID:    s.testData.customer.ID,
			InvoiceType:   types.InvoiceTypeOneOff,
			InvoiceStatus: types.InvoiceStatusVoided,
			PaymentStatus: types.PaymentStatusPending,
			Currency:      "usd",
		})
		err := s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconcilePaymentStatus
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestReconcilePaymentStatus() {
	ctx := s.GetContext()
	hundred := decimal.NewFromInt(100)

	s.Run("partial_pending_payment_accumulates_amount_paid", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_partial", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)

		thirty := decimal.NewFromInt(30)
		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &thirty))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AmountPaid.Equal(thirty))
		s.True(stored.AmountRemaining.Equal(decimal.NewFromInt(70)))
		s.Equal(types.PaymentStatusPending, stored.PaymentStatus)

		// A second pending payment accumulates (reconcile adds, not replaces).
		twenty := decimal.NewFromInt(20)
		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &twenty))
		stored, err = s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AmountPaid.Equal(decimal.NewFromInt(50)), "amount_paid must accumulate, got %s", stored.AmountPaid)
		s.True(stored.AmountRemaining.Equal(decimal.NewFromInt(50)))
	})

	s.Run("succeeded_without_amount_pays_in_full", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_full", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)

		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(hundred))
		s.True(stored.AmountRemaining.IsZero())
		s.NotNil(stored.PaidAt)
	})

	s.Run("overpayment_marks_invoice_overpaid_with_zero_remaining", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_overpaid", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)

		amount := decimal.NewFromInt(150)
		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, &amount))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusOverpaid, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(amount))
		s.True(stored.AmountRemaining.IsZero(), "overpaid invoices must have zero remaining")

		// Additional payment to an already-overpaid invoice keeps accumulating.
		ten := decimal.NewFromInt(10)
		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusOverpaid, &ten))
		stored, err = s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusOverpaid, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(decimal.NewFromInt(160)))
		s.True(stored.AmountRemaining.IsZero())
	})

	s.Run("failed_payment_clears_paid_at_and_keeps_amount_paid", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_failed", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)

		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusFailed, nil))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusFailed, stored.PaymentStatus)
		s.Nil(stored.PaidAt)
		s.True(stored.AmountPaid.IsZero())
	})

	s.Run("invalid_transition_succeeded_to_failed_is_rejected", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_invalid", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil))

		err := s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusFailed, nil)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("voided_invoice_is_rejected", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:            "inv_lc_rec_voided",
			CustomerID:    s.testData.customer.ID,
			InvoiceType:   types.InvoiceTypeOneOff,
			InvoiceStatus: types.InvoiceStatusVoided,
			PaymentStatus: types.PaymentStatusPending,
			Currency:      "usd",
		})
		err := s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("negative_amount_is_rejected", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_negative", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		neg := decimal.NewFromInt(-1)
		err := s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &neg)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_returns_error", func() {
		err := s.service.ReconcilePaymentStatus(ctx, "inv_does_not_exist", types.PaymentStatusSucceeded, nil)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("full_payment_of_subscription_create_invoice_activates_subscription", func() {
		ctx := s.GetContext()
		incompleteSub := &subscription.Subscription{
			ID:                 "sub_lc_incomplete",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(29 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
			Currency:           "usd",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusIncomplete,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, incompleteSub, nil))

		inv := s.buildFinalizedInvoiceFor("inv_lc_rec_activate", hundred,
			string(types.InvoiceBillingReasonSubscriptionCreate), lo.ToPtr(incompleteSub.ID))

		s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, &hundred))

		storedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, incompleteSub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, storedSub.SubscriptionStatus,
			"paying the activating invoice in full must activate the subscription")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// HandleIncompleteSubscriptionPayment
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestHandleIncompleteSubscriptionPayment() {
	ctx := s.GetContext()

	s.Run("noop_without_subscription_id", func() {
		inv := &invoice.Invoice{
			ID:              "inv_lc_hisp_nosub",
			CustomerID:      s.testData.customer.ID,
			BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
			AmountRemaining: decimal.Zero,
		}
		s.NoError(s.service.HandleIncompleteSubscriptionPayment(ctx, inv))
	})

	s.Run("noop_when_amount_remaining_is_nonzero", func() {
		inv := &invoice.Invoice{
			ID:              "inv_lc_hisp_remaining",
			CustomerID:      s.testData.customer.ID,
			SubscriptionID:  lo.ToPtr(s.testData.sub.ID),
			BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
			AmountRemaining: decimal.NewFromInt(5),
		}
		s.NoError(s.service.HandleIncompleteSubscriptionPayment(ctx, inv))
		// Subscription untouched.
		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})

	s.Run("noop_for_non_qualifying_billing_reason", func() {
		inv := &invoice.Invoice{
			ID:              "inv_lc_hisp_cycle",
			CustomerID:      s.testData.customer.ID,
			SubscriptionID:  lo.ToPtr(s.testData.sub.ID),
			BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
			AmountRemaining: decimal.Zero,
		}
		s.NoError(s.service.HandleIncompleteSubscriptionPayment(ctx, inv))
	})

	s.Run("activates_incomplete_subscription_for_create_reason", func() {
		incompleteSub := &subscription.Subscription{
			ID:                 "sub_lc_hisp_incomplete",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(29 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
			Currency:           "usd",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusIncomplete,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, incompleteSub, nil))

		inv := &invoice.Invoice{
			ID:              "inv_lc_hisp_activate",
			CustomerID:      s.testData.customer.ID,
			SubscriptionID:  lo.ToPtr(incompleteSub.ID),
			BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
			AmountRemaining: decimal.Zero,
		}
		s.NoError(s.service.HandleIncompleteSubscriptionPayment(ctx, inv))

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, incompleteSub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ProcessDraftInvoice guard rails
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestProcessDraftInvoiceGuards() {
	ctx := s.GetContext()

	s.Run("non_draft_invoice_is_rejected", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_pdi_final", decimal.NewFromInt(10),
			string(types.InvoiceBillingReasonSubscriptionCycle), lo.ToPtr(s.testData.sub.ID))
		err := s.service.ProcessDraftInvoice(ctx, inv.ID, nil, nil, types.InvoiceFlowManual)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("uncomputed_draft_is_rejected", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:            "inv_lc_pdi_uncomputed",
			CustomerID:    s.testData.customer.ID,
			InvoiceType:   types.InvoiceTypeSubscription,
			InvoiceStatus: types.InvoiceStatusDraft,
			PaymentStatus: types.PaymentStatusPending,
			Currency:      "usd",
		})
		err := s.service.ProcessDraftInvoice(ctx, inv.ID, nil, nil, types.InvoiceFlowManual)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Trigger helpers, deletion stub, proration description
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestTriggerCommunication() {
	ctx := s.GetContext()

	s.Run("not_found_returns_error", func() {
		err := s.service.TriggerCommunication(ctx, "inv_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("publishes_for_existing_invoice", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_comm", decimal.NewFromInt(10),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		s.NoError(s.service.TriggerCommunication(ctx, inv.ID))
	})
}

func (s *InvoiceLifecycleSuite) TestTriggerWebhook() {
	ctx := s.GetContext()
	inv := s.buildFinalizedInvoiceFor("inv_lc_webhook", decimal.NewFromInt(10),
		string(types.InvoiceBillingReasonSubscriptionCycle), nil)

	testCases := []struct {
		name        string
		invoiceID   string
		eventName   types.WebhookEventName
		expectError bool
	}{
		{
			name:      "valid_event_finalized",
			invoiceID: inv.ID,
			eventName: types.WebhookEventInvoiceUpdateFinalized,
		},
		{
			name:      "valid_event_payment",
			invoiceID: inv.ID,
			eventName: types.WebhookEventInvoiceUpdatePayment,
		},
		{
			name:        "invalid_event_name_is_rejected",
			invoiceID:   inv.ID,
			eventName:   types.WebhookEventName("invoice.not.a.real.event"),
			expectError: true,
		},
		{
			name:        "invoice_not_found",
			invoiceID:   "inv_does_not_exist",
			eventName:   types.WebhookEventInvoiceUpdateFinalized,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.TriggerWebhook(ctx, tc.invoiceID, tc.eventName)
			if tc.expectError {
				s.Error(err)
				return
			}
			s.NoError(err)
		})
	}
}

func (s *InvoiceLifecycleSuite) TestDeleteInvoiceNotImplemented() {
	err := s.service.DeleteInvoice(s.GetContext(), "inv_anything")
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *InvoiceLifecycleSuite) TestGenerateProrationInvoiceDescription() {
	svc, ok := s.service.(*invoiceService)
	s.Require().True(ok)

	testCases := []struct {
		name             string
		cancellationType string
		reason           string
		amount           decimal.Decimal
		expected         string
	}{
		{
			name:             "credit_immediate_cancellation",
			cancellationType: "immediate",
			reason:           "user_requested",
			amount:           decimal.NewFromInt(-10),
			expected:         "Credit for unused time - immediate cancellation (user_requested)",
		},
		{
			name:             "credit_scheduled_cancellation",
			cancellationType: "specific_date",
			reason:           "downgrade",
			amount:           decimal.NewFromInt(-10),
			expected:         "Credit for unused time - scheduled cancellation (downgrade)",
		},
		{
			name:             "credit_default_cancellation_type",
			cancellationType: "other",
			reason:           "misc",
			amount:           decimal.NewFromInt(-1),
			expected:         "Cancellation credit (misc)",
		},
		{
			name:             "positive_amount_is_a_charge",
			cancellationType: "immediate",
			reason:           "usage_overage",
			amount:           decimal.NewFromInt(5),
			expected:         "Proration charges - cancellation (usage_overage)",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			got := svc.generateProrationInvoiceDescription(tc.cancellationType, tc.reason, tc.amount)
			s.Equal(tc.expected, got)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeInvoice — inherited subscriptions and trial-start invoices
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestComputeInvoiceInheritedSubscriptionSkips() {
	ctx := s.GetContext()

	inheritedSub := &subscription.Subscription{
		ID:                 "sub_lc_inherited",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      s.testData.sub.CurrentPeriodEnd,
		CurrentPeriodStart: s.testData.sub.CurrentPeriodStart,
		CurrentPeriodEnd:   s.testData.sub.CurrentPeriodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeInherited,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, inheritedSub, nil))

	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:             "inv_lc_inherited",
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(inheritedSub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusDraft,
		PaymentStatus:  types.PaymentStatusPending,
		Currency:       "usd",
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	})

	skipped, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
	s.NoError(err)
	s.True(skipped, "inherited subscriptions must not be computed on their own invoice")

	// Invoice must be untouched: still draft, still zero, never computed.
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
	s.Nil(stored.LastComputedAt)
}

func (s *InvoiceLifecycleSuite) TestTrialStartInvoiceIsZeroedAndFinalizedPending() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)

	draft, err := s.service.CreateEmptyDraftInvoice(ctx, dto.CreateDraftInvoiceRequest{
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(s.testData.sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionTrialStart,
	})
	s.Require().NoError(err)

	// Trial start invoices preview the charges at $0 — computed but NOT skipped.
	skipped, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.False(skipped, "trial-start invoices must not be marked skipped despite the zero subtotal")

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
	s.True(stored.Subtotal.IsZero(), "trial-start amounts must be zeroed out, got %s", stored.Subtotal)
	s.NotNil(stored.LastComputedAt)

	// Line item structure is preserved at $0 so the customer sees upcoming charges.
	items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, draft.ID)
	s.Require().NoError(err)
	s.Require().NotEmpty(items)
	for _, li := range items {
		s.True(li.Amount.IsZero(), "trial-start line items must be zeroed, got %s", li.Amount)
	}

	// Finalizing must NOT auto-succeed the payment (collection method decides later).
	s.NoError(s.service.FinalizeInvoice(ctx, draft.ID))
	stored, err = s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.Equal(types.PaymentStatusPending, stored.PaymentStatus,
		"zero-total trial-start invoice must stay pending on finalize")
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateInvoice / CreateSubscriptionInvoice
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestCreateInvoiceSubscriptionZeroChargesIsSkipped() {
	ctx := s.GetContext()
	periodStart := s.testData.emptySub.CurrentPeriodStart
	periodEnd := s.testData.emptySub.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.CreateInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(s.testData.emptySub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.InvoiceStatusSkipped, resp.InvoiceStatus, "zero-charge subscription invoice must be returned as SKIPPED")
	s.True(resp.Total.IsZero())
	s.Nil(resp.InvoiceNumber)
}

func (s *InvoiceLifecycleSuite) TestCreateSubscriptionInvoice() {
	ctx := s.GetContext()

	s.Run("validation_error_for_reversed_period", func() {
		resp, sub, err := s.service.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
			PeriodStart:    s.testData.now,
			PeriodEnd:      s.testData.now.Add(-time.Hour),
			ReferencePoint: types.ReferencePointPeriodEnd,
		}, nil, types.InvoiceFlowManual, false)
		s.Error(err)
		s.Nil(resp)
		s.Nil(sub)
	})

	s.Run("draft_subscription_is_rejected", func() {
		draftSub := &subscription.Subscription{
			ID:                 "sub_lc_draft",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(29 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
			Currency:           "usd",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusDraft,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, draftSub, nil))

		resp, _, err := s.service.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: draftSub.ID,
			PeriodStart:    draftSub.CurrentPeriodStart,
			PeriodEnd:      draftSub.CurrentPeriodEnd,
			ReferencePoint: types.ReferencePointPeriodEnd,
		}, nil, types.InvoiceFlowManual, false)
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("zero_charge_subscription_returns_nil_invoice", func() {
		resp, sub, err := s.service.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: s.testData.emptySub.ID,
			PeriodStart:    s.testData.emptySub.CurrentPeriodStart,
			PeriodEnd:      s.testData.emptySub.CurrentPeriodEnd,
			ReferencePoint: types.ReferencePointPeriodEnd,
		}, nil, types.InvoiceFlowManual, false)
		s.NoError(err)
		s.Nil(resp, "skipped invoices are not returned")
		s.Require().NotNil(sub)
		s.Equal(s.testData.emptySub.ID, sub.ID)
	})
}

func (s *InvoiceLifecycleSuite) TestCreateEmptyDraftInvoicePeriodConflict() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)

	// Create, compute, and finalize an invoice for the period.
	draft, err := s.service.CreateDraftInvoiceForSubscription(
		ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
	s.Require().NoError(err)
	_, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.service.FinalizeInvoice(ctx, draft.ID))

	// A new draft request for the same period (different idempotency key)
	// must be rejected because a finalized invoice already covers the period.
	key := "idem_lc_conflict"
	resp, err := s.service.CreateEmptyDraftInvoice(ctx, dto.CreateDraftInvoiceRequest{
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(s.testData.sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		IdempotencyKey: &key,
	})
	s.Error(err)
	s.True(ierr.IsAlreadyExists(err), "finalized invoice for the same period must block a new draft, got %v", err)
	s.Nil(resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// RecalculateInvoiceAmounts
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestRecalculateInvoiceAmounts() {
	ctx := s.GetContext()

	newCreditNote := func(id, invoiceID string, cnType types.CreditNoteType, amount decimal.Decimal) {
		cn := &creditnote.CreditNote{
			ID:               id,
			CreditNoteNumber: "CN-" + id,
			InvoiceID:        invoiceID,
			CustomerID:       s.testData.customer.ID,
			CreditNoteStatus: types.CreditNoteStatusFinalized,
			CreditNoteType:   cnType,
			Reason:           types.CreditNoteReasonBillingError,
			Currency:         "usd",
			TotalAmount:      amount,
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CreditNoteRepo.Create(ctx, cn))
	}

	s.Run("adjustment_and_refund_credit_notes_reduce_amount_due", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_recalc_amounts", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		newCreditNote("cn_lc_adj", inv.ID, types.CreditNoteTypeAdjustment, decimal.NewFromInt(20))
		newCreditNote("cn_lc_refund", inv.ID, types.CreditNoteTypeRefund, decimal.NewFromInt(10))

		s.NoError(s.service.RecalculateInvoiceAmounts(ctx, inv.ID))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AdjustmentAmount.Equal(decimal.NewFromInt(20)), "adjustment, got %s", stored.AdjustmentAmount)
		s.True(stored.RefundedAmount.Equal(decimal.NewFromInt(10)), "refunded, got %s", stored.RefundedAmount)
		// amount_due = total - adjustment = 100 - 20 = 80
		s.True(stored.AmountDue.Equal(decimal.NewFromInt(80)), "amount_due, got %s", stored.AmountDue)
		s.True(stored.AmountRemaining.Equal(decimal.NewFromInt(80)), "amount_remaining, got %s", stored.AmountRemaining)
	})

	s.Run("fully_adjusted_invoice_becomes_succeeded", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_recalc_full", decimal.NewFromInt(100),
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		newCreditNote("cn_lc_adj_full", inv.ID, types.CreditNoteTypeAdjustment, decimal.NewFromInt(100))

		s.NoError(s.service.RecalculateInvoiceAmounts(ctx, inv.ID))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AmountDue.IsZero())
		s.True(stored.AmountRemaining.IsZero())
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus,
			"a fully adjusted invoice must be marked paid")
	})

	s.Run("non_finalized_invoice_is_skipped", func() {
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:              "inv_lc_recalc_draft",
			CustomerID:      s.testData.customer.ID,
			InvoiceType:     types.InvoiceTypeOneOff,
			InvoiceStatus:   types.InvoiceStatusDraft,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			Subtotal:        decimal.NewFromInt(50),
			Total:           decimal.NewFromInt(50),
			AmountDue:       decimal.NewFromInt(50),
			AmountRemaining: decimal.NewFromInt(50),
		})
		newCreditNote("cn_lc_adj_draft", inv.ID, types.CreditNoteTypeAdjustment, decimal.NewFromInt(20))

		s.NoError(s.service.RecalculateInvoiceAmounts(ctx, inv.ID))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.True(stored.AmountDue.Equal(decimal.NewFromInt(50)), "draft invoices must not be recalculated")
		s.True(stored.AdjustmentAmount.IsZero())
	})

	s.Run("not_found_returns_error", func() {
		err := s.service.RecalculateInvoiceAmounts(ctx, "inv_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdatePaymentStatus / ReconcilePaymentStatus — wallet transaction metadata
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestUpdatePaymentStatusTransitions() {
	ctx := s.GetContext()
	hundred := decimal.NewFromInt(100)

	s.Run("succeeded_pays_in_full_and_sets_paid_at", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_ups_success", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		s.NoError(s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil))

		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
		s.True(stored.AmountPaid.Equal(hundred))
		s.True(stored.AmountRemaining.IsZero())
		s.NotNil(stored.PaidAt)
	})

	s.Run("failed_resets_amounts_and_paid_at", func() {
		inv := s.buildFinalizedInvoiceFor("inv_lc_ups_failed", hundred,
			string(types.InvoiceBillingReasonSubscriptionCycle), nil)
		thirty := decimal.NewFromInt(30)
		s.NoError(s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusPending, &thirty))

		s.NoError(s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusFailed, nil))
		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusFailed, stored.PaymentStatus)
		s.True(stored.AmountPaid.IsZero())
		s.True(stored.AmountRemaining.Equal(hundred))
		s.Nil(stored.PaidAt)
	})

	s.Run("succeeded_with_wallet_transaction_metadata_does_not_fail", func() {
		bp := string(types.BILLING_PERIOD_MONTHLY)
		inv := s.buildStoredInvoice(&invoice.Invoice{
			ID:              "inv_lc_ups_wallet_txn",
			CustomerID:      s.testData.customer.ID,
			InvoiceType:     types.InvoiceTypeOneOff,
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			Subtotal:        hundred,
			Total:           hundred,
			AmountDue:       hundred,
			AmountRemaining: hundred,
			BillingPeriod:   &bp,
			// Non-existent wallet transaction: completion failure must be
			// logged, not returned — the payment update itself succeeds.
			Metadata: types.Metadata{"wallet_transaction_id": "wtxn_missing"},
		})

		s.NoError(s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil))
		stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
	})
}

func (s *InvoiceLifecycleSuite) TestReconcilePaymentStatusWalletTransactionMetadata() {
	ctx := s.GetContext()
	hundred := decimal.NewFromInt(100)
	bp := string(types.BILLING_PERIOD_MONTHLY)
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:              "inv_lc_rec_wallet_txn",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        hundred,
		Total:           hundred,
		AmountDue:       hundred,
		AmountRemaining: hundred,
		BillingPeriod:   &bp,
		Metadata:        types.Metadata{"wallet_transaction_id": "wtxn_missing"},
	})

	// Wallet transaction completion failure is logged, not fatal.
	s.NoError(s.service.ReconcilePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, &hundred))
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
	s.True(stored.AmountPaid.Equal(hundred))
}

// ─────────────────────────────────────────────────────────────────────────────
// AttemptPayment / VoidInvoice guards
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestAttemptPaymentSkippedInvoiceIsNoop() {
	ctx := s.GetContext()
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:            "inv_lc_attempt_skipped",
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusSkipped,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "usd",
	})

	s.NoError(s.service.AttemptPayment(ctx, inv.ID))

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusSkipped, stored.InvoiceStatus)
	s.Equal(types.PaymentStatusPending, stored.PaymentStatus, "skipped invoices are never paid")
}

func (s *InvoiceLifecycleSuite) TestVoidInvoiceDisallowedPaymentStatus() {
	ctx := s.GetContext()
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:            "inv_lc_void_refunded",
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusRefunded,
		Currency:      "usd",
	})

	err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.Error(err)
	s.True(ierr.IsValidation(err), "already-refunded invoices cannot be voided")
}

// ─────────────────────────────────────────────────────────────────────────────
// DistributeInvoiceLevelDiscount
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestDistributeInvoiceLevelDiscount() {
	ctx := s.GetContext()

	newLineItem := func(amount, lineItemDiscount decimal.Decimal) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
			Amount:           amount,
			LineItemDiscount: lineItemDiscount,
			Currency:         "usd",
		}
	}

	s.Run("zero_discount_is_a_noop", func() {
		items := []*invoice.InvoiceLineItem{newLineItem(decimal.NewFromInt(60), decimal.Zero)}
		s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, items, decimal.Zero))
		s.True(items[0].InvoiceLevelDiscount.IsZero())
	})

	s.Run("discount_is_distributed_proportionally_and_exactly", func() {
		items := []*invoice.InvoiceLineItem{
			newLineItem(decimal.NewFromInt(60), decimal.Zero),
			newLineItem(decimal.NewFromInt(40), decimal.Zero),
		}
		discount := decimal.NewFromInt(10)
		s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, items, discount))

		total := decimal.Zero
		for _, li := range items {
			total = total.Add(li.InvoiceLevelDiscount)
		}
		s.True(total.Equal(discount), "distribution must be exact: got %s of %s", total, discount)
		// 60/100 * 10 = 6, 40/100 * 10 = 4 (items are sorted desc by amount).
		s.True(items[0].InvoiceLevelDiscount.Equal(decimal.NewFromInt(6)), "got %s", items[0].InvoiceLevelDiscount)
		s.True(items[1].InvoiceLevelDiscount.Equal(decimal.NewFromInt(4)), "got %s", items[1].InvoiceLevelDiscount)
	})

	s.Run("fully_discounted_line_items_are_ineligible", func() {
		fullDiscounted := newLineItem(decimal.NewFromInt(50), decimal.NewFromInt(50))
		eligible := newLineItem(decimal.NewFromInt(50), decimal.Zero)
		items := []*invoice.InvoiceLineItem{fullDiscounted, eligible}

		s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, items, decimal.NewFromInt(10)))
		s.True(fullDiscounted.InvoiceLevelDiscount.IsZero(), "fully discounted item must receive nothing")
		s.True(eligible.InvoiceLevelDiscount.Equal(decimal.NewFromInt(10)))
	})

	s.Run("all_items_fully_discounted_distributes_nothing", func() {
		items := []*invoice.InvoiceLineItem{
			newLineItem(decimal.NewFromInt(50), decimal.NewFromInt(50)),
		}
		s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, items, decimal.NewFromInt(10)))
		s.True(items[0].InvoiceLevelDiscount.IsZero())
	})

	s.Run("last_item_share_is_capped_at_its_amount", func() {
		big := newLineItem(decimal.NewFromInt(99), decimal.Zero)
		tiny := newLineItem(decimal.NewFromInt(1), decimal.Zero)
		items := []*invoice.InvoiceLineItem{big, tiny}

		// Discount equals total eligible amount: last item's remainder is capped.
		s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, items, decimal.NewFromInt(100)))
		s.True(big.InvoiceLevelDiscount.Equal(decimal.NewFromInt(99)), "got %s", big.InvoiceLevelDiscount)
		s.True(tiny.InvoiceLevelDiscount.Equal(decimal.NewFromInt(1)), "got %s", tiny.InvoiceLevelDiscount)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Additional edge branches
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoiceLifecycleSuite) TestCreateOneOffInvoiceTaxRateOverrides() {
	tr := s.createPercentTaxRate("tax_lc_override", decimal.NewFromInt(5))
	req := s.oneOffRequest(func(r *dto.CreateInvoiceRequest) {
		r.TaxRateOverrides = []*dto.TaxRateOverride{
			{TaxRateCode: tr.Code, Currency: "usd"},
		}
	})

	resp, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
	s.NoError(err)
	s.Require().NotNil(resp)
	// 5% of 100 = 5 tax → total 105.
	s.True(resp.Total.Equal(decimal.NewFromInt(105)), "total incl override tax, got %s", resp.Total)

	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeInvoice
	taxFilter.EntityID = resp.ID
	applied, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), taxFilter)
	s.NoError(err)
	s.Require().Len(applied, 1)
	s.True(applied[0].TaxAmount.Equal(decimal.NewFromInt(5)))
}

func (s *InvoiceLifecycleSuite) TestCreateInvoiceExplicitFinalizedStatusFinalizesSubscriptionInvoice() {
	ctx := s.GetContext()
	// Distinct period so the fixture does not collide with other tests.
	periodStart := s.testData.sub.CurrentPeriodStart.Add(30 * time.Minute)
	periodEnd := s.testData.sub.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.CreateInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(s.testData.sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		InvoiceStatus:  lo.ToPtr(types.InvoiceStatusFinalized),
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.InvoiceStatusFinalized, resp.InvoiceStatus,
		"explicit finalized status must finalize even subscription invoices")
	s.NotNil(resp.InvoiceNumber)
	s.True(resp.Total.IsPositive())
}

func (s *InvoiceLifecycleSuite) TestComputeInvoiceMissingSubscriptionReturnsError() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:             "inv_lc_ghost_sub",
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr("sub_ghost_lc"),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusDraft,
		PaymentStatus:  types.PaymentStatusPending,
		Currency:       "usd",
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	})

	_, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *InvoiceLifecycleSuite) TestCreateEmptyDraftInvoiceReturnsExistingDraftForPeriod() {
	ctx := s.GetContext()
	periodStart := s.testData.sub.CurrentPeriodStart
	periodEnd := s.testData.sub.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)

	first, err := s.service.CreateDraftInvoiceForSubscription(
		ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
	s.Require().NoError(err)

	// Different idempotency key, same subscription period: the period-uniqueness
	// check must return the existing draft instead of creating a duplicate.
	key := "idem_lc_period_reuse"
	second, err := s.service.CreateEmptyDraftInvoice(ctx, dto.CreateDraftInvoiceRequest{
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(s.testData.sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		IdempotencyKey: &key,
	})
	s.NoError(err)
	s.Require().NotNil(second)
	s.Equal(first.ID, second.ID, "existing draft for the period must be reused")
}

func (s *InvoiceLifecycleSuite) TestUpdateInvoiceMetadataAndDueDate() {
	ctx := s.GetContext()
	// NOTE: a real subscription ID is required — GetInvoice dereferences
	// SubscriptionID for subscription-type invoices without a nil check.
	inv := s.buildFinalizedInvoiceFor("inv_lc_update_meta", decimal.NewFromInt(10),
		string(types.InvoiceBillingReasonSubscriptionCycle), lo.ToPtr(s.testData.sub.ID))

	newDue := s.testData.now.Add(14 * 24 * time.Hour)
	resp, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{
		InvoicePDFURL: lo.ToPtr("https://cdn.example.com/updated.pdf"),
		DueDate:       &newDue,
		Metadata:      &types.Metadata{"purchase_order": "PO-42"},
	})
	s.NoError(err)
	s.Require().NotNil(resp)

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal("https://cdn.example.com/updated.pdf", lo.FromPtr(stored.InvoicePDFURL))
	s.Require().NotNil(stored.DueDate)
	s.WithinDuration(newDue, *stored.DueDate, time.Second)
	s.Equal("PO-42", stored.Metadata["purchase_order"])
}

func (s *InvoiceLifecycleSuite) TestAttemptPaymentNotFound() {
	err := s.service.AttemptPayment(s.GetContext(), "inv_does_not_exist")
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *InvoiceLifecycleSuite) TestGetInvoiceSubscriptionMissingReturnsError() {
	ctx := s.GetContext()
	inv := s.buildStoredInvoice(&invoice.Invoice{
		ID:             "inv_lc_getinv_ghost",
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr("sub_ghost_getinv"),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusFinalized,
		PaymentStatus:  types.PaymentStatusPending,
		Currency:       "usd",
	})

	resp, err := s.service.GetInvoice(ctx, inv.ID)
	s.Error(err, "subscription expansion must fail for a missing subscription")
	s.Nil(resp)
}

func (s *InvoiceLifecycleSuite) TestDistributeInvoiceLevelDiscountCapsFirstPassShares() {
	ctx := s.GetContext()
	big := &invoice.InvoiceLineItem{
		ID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		Amount:   decimal.NewFromInt(99),
		Currency: "usd",
	}
	tiny := &invoice.InvoiceLineItem{
		ID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		Amount:   decimal.NewFromInt(1),
		Currency: "usd",
	}

	// Discount exceeds the total eligible amount: every share must be capped
	// at its line item amount, never above.
	s.NoError(s.service.DistributeInvoiceLevelDiscount(ctx, []*invoice.InvoiceLineItem{big, tiny}, decimal.NewFromInt(200)))
	s.True(big.InvoiceLevelDiscount.Equal(decimal.NewFromInt(99)), "big share capped, got %s", big.InvoiceLevelDiscount)
	s.True(tiny.InvoiceLevelDiscount.Equal(decimal.NewFromInt(1)), "tiny share capped, got %s", tiny.InvoiceLevelDiscount)
}
