package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type RecalculateDiscountOnInvoiceSuite struct {
	testutil.BaseServiceTestSuite
	service  InvoiceService
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
	}
}

func TestRecalculateDiscountOnInvoice(t *testing.T) {
	suite.Run(t, new(RecalculateDiscountOnInvoiceSuite))
}

func (s *RecalculateDiscountOnInvoiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *RecalculateDiscountOnInvoiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *RecalculateDiscountOnInvoiceSuite) setupService() {
	s.service = NewInvoiceService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditNoteRepo:             s.GetStores().CreditNoteRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CreditNoteLineItemRepo:     s.GetStores().CreditNoteLineItemRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:   types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	})
}

func (s *RecalculateDiscountOnInvoiceSuite) setupTestData() {
	s.BaseServiceTestSuite.ClearStores()
	s.testData.customer = &customer.Customer{
		ID:         "cust_discount_test",
		ExternalID: "ext_cust_discount_test",
		Name:       "Discount Test Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_discount_test",
		Name:      "Discount Test Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))
}

// createOneOffDraftInvoice creates a subscription-less draft invoice with one line item.
func (s *RecalculateDiscountOnInvoiceSuite) createOneOffDraftInvoice(id string, amount decimal.Decimal) *invoice.Invoice {
	li := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        id,
		CustomerID:       s.testData.customer.ID,
		Amount:           amount,
		Quantity:         decimal.NewFromInt(1),
		Currency:         "usd",
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	inv := &invoice.Invoice{
		ID:            id,
		CustomerID:    s.testData.customer.ID,
		Currency:      "usd",
		Subtotal:      amount,
		Total:         amount,
		AmountDue:     amount,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
		LineItems:     []*invoice.InvoiceLineItem{li},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

// createSubscriptionDraftInvoice creates a subscription + a draft invoice for it, scoped to
// [periodStart, periodEnd), with PriceID set for line-item-level coupon matching.
func (s *RecalculateDiscountOnInvoiceSuite) createSubscriptionDraftInvoice(
	invID string, sub *subscription.Subscription, periodStart, periodEnd time.Time, amount decimal.Decimal, priceID string,
) *invoice.Invoice {
	li := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        invID,
		CustomerID:       s.testData.customer.ID,
		SubscriptionID:   &sub.ID,
		Amount:           amount,
		Quantity:         decimal.NewFromInt(1),
		Currency:         "usd",
		PriceID:          lo.ToPtr(priceID),
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	inv := &invoice.Invoice{
		ID:             invID,
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: &sub.ID,
		Currency:       "usd",
		Subtotal:       amount,
		Total:          amount,
		AmountDue:      amount,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusDraft,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		LineItems:      []*invoice.InvoiceLineItem{li},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

// createSubscriptionWithAssociation creates a subscription plus an invoice-level
// CouponAssociation for it, active from periodStart with no end date.
func (s *RecalculateDiscountOnInvoiceSuite) createSubscriptionWithAssociation(
	subID, assocID string, c *coupon.Coupon, periodStart, periodEnd time.Time,
) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID: subID, CustomerID: s.testData.customer.ID, PlanID: s.testData.plan.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd,
		BillingAnchor: periodStart, StartDate: periodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	assoc := &coupon_association.CouponAssociation{
		ID: assocID, CouponID: c.ID, SubscriptionID: sub.ID,
		StartDate: periodStart, EnvironmentID: types.GetEnvironmentID(s.GetContext()), Coupon: c,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(s.GetContext(), assoc))
	return sub
}

func (s *RecalculateDiscountOnInvoiceSuite) createPercentageCoupon(id string, percentOff int64) *coupon.Coupon {
	c := &coupon.Coupon{
		ID: id, Name: "Test Coupon " + id, Type: types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(percentOff)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(s.GetContext()), BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CouponRepo.Create(s.GetContext(), c))
	return c
}

func (s *RecalculateDiscountOnInvoiceSuite) createFixedAmountCoupon(id string, amountOff int64) *coupon.Coupon {
	c := &coupon.Coupon{
		ID: id, Name: "Test Coupon " + id, Type: types.CouponTypeFixed,
		AmountOff: lo.ToPtr(decimal.NewFromInt(amountOff)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(s.GetContext()), BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CouponRepo.Create(s.GetContext(), c))
	return c
}

// applyDiscount is a small call-site helper so every test reads the same way it will once
// wired through UpdateInvoice: PUT semantics, apply_discount:true.
func (s *RecalculateDiscountOnInvoiceSuite) applyDiscount(ctx context.Context, invoiceID string) (*dto.InvoiceResponse, error) {
	return s.service.UpdateInvoice(ctx, invoiceID, dto.UpdateInvoiceRequest{ApplyDiscount: true})
}

func (s *RecalculateDiscountOnInvoiceSuite) TestNoSubscriptionIsNoOp() {
	ctx := s.GetContext()
	inv := s.createOneOffDraftInvoice("inv_no_sub", decimal.NewFromInt(100))

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.TotalDiscount.IsZero())
	s.True(resp.Total.Equal(decimal.NewFromInt(100)))

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	apps, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.NoError(err)
	s.Empty(apps)
}

func (s *RecalculateDiscountOnInvoiceSuite) TestInvoiceNotFound() {
	_, err := s.applyDiscount(s.GetContext(), "inv_does_not_exist")
	s.Error(err)
}

// Finalized invoices are no longer rejected: applying a discount voids the invoice and
// lands the recalculation on a new draft copy (the void-and-recreate edit flow).
func (s *RecalculateDiscountOnInvoiceSuite) TestFinalizedInvoiceVoidsAndRecreatesOnApplyDiscount() {
	ctx := s.GetContext()
	inv := s.createOneOffDraftInvoice("inv_finalized", decimal.NewFromInt(100))
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.PaymentStatus = types.PaymentStatusPending
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.Require().NotNil(resp)
	s.NotEqual(inv.ID, resp.ID)
	s.Equal(types.InvoiceStatusDraft, resp.InvoiceStatus)

	original, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusVoided, original.InvoiceStatus)
	s.Equal(resp.ID, lo.FromPtr(original.RecalculatedInvoiceID))
}

// A finalized invoice that cannot be voided (payment status outside the voidable set)
// still rejects the discount recalculation.
func (s *RecalculateDiscountOnInvoiceSuite) TestUnvoidableFinalizedInvoiceRejectsApplyDiscount() {
	ctx := s.GetContext()
	inv := s.createOneOffDraftInvoice("inv_finalized_refunded", decimal.NewFromInt(100))
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.PaymentStatus = types.PaymentStatusRefunded
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.applyDiscount(ctx, inv.ID)
	s.Error(err)
	s.Contains(err.Error(), "payment status is not allowed")
}

func (s *RecalculateDiscountOnInvoiceSuite) TestAppliesInvoiceLevelCoupon() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_1", 10)
	sub := s.createSubscriptionWithAssociation("sub_discount_1", "assoc_discount_1", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_1", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_1")

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(10)), "want 10, got %s", resp.TotalDiscount)
	s.True(resp.Total.Equal(decimal.NewFromInt(90)), "want 90, got %s", resp.Total)

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	apps, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.NoError(err)
	s.Len(apps, 1)
	s.Equal(c.ID, apps[0].CouponID)
}

func (s *RecalculateDiscountOnInvoiceSuite) TestAppliesFixedAmountCoupon() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createFixedAmountCoupon("coupon_discount_fixed", 15)
	sub := s.createSubscriptionWithAssociation("sub_discount_fixed", "assoc_discount_fixed", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_fixed", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_fixed")

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(15)), "want 15, got %s", resp.TotalDiscount)
	s.True(resp.Total.Equal(decimal.NewFromInt(85)), "want 85, got %s", resp.Total)
}

func (s *RecalculateDiscountOnInvoiceSuite) TestCalledTwiceDoesNotDoubleDiscount() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_2", 10)
	sub := s.createSubscriptionWithAssociation("sub_discount_2", "assoc_discount_2", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_2", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_2")

	first, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	second, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)

	s.True(first.TotalDiscount.Equal(second.TotalDiscount), "first %s vs second %s", first.TotalDiscount, second.TotalDiscount)
	s.True(second.TotalDiscount.Equal(decimal.NewFromInt(10)))
	s.Require().Len(second.LineItems, 1)
	s.True(second.LineItems[0].LineItemDiscount.Equal(decimal.Zero), "invoice-level discount must not leak into line_item_discount")
	s.True(second.LineItems[0].InvoiceLevelDiscount.Equal(decimal.NewFromInt(10)))

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	apps, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.NoError(err)
	s.Len(apps, 1, "second call must not create a duplicate CouponApplication row")
}

// TestSubscriptionInvoiceWithNoAssociationsIsNoOp differs from TestNoSubscriptionIsNoOp: it
// exercises selectSubscriptionCoupons, not the early nil-subscription return.
func (s *RecalculateDiscountOnInvoiceSuite) TestSubscriptionInvoiceWithNoAssociationsIsNoOp() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_discount_6", CustomerID: s.testData.customer.ID, PlanID: s.testData.plan.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd,
		BillingAnchor: periodStart, StartDate: periodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	inv := s.createSubscriptionDraftInvoice("inv_discount_6", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_6")

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(resp.TotalDiscount.IsZero())
	s.True(resp.Total.Equal(decimal.NewFromInt(100)))
}

// TestAssociationRemovedResetsToZero covers the exact gap that let a prior version of this
// code ship a bug: it asserts on the PERSISTED line-item discount fields, not just totals.
func (s *RecalculateDiscountOnInvoiceSuite) TestAssociationRemovedResetsToZero() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_3", 10)
	sub := s.createSubscriptionWithAssociation("sub_discount_3", "assoc_discount_3", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_3", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_3")

	first, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(first.TotalDiscount.Equal(decimal.NewFromInt(10)))
	s.Require().Len(first.LineItems, 1)
	s.True(first.LineItems[0].InvoiceLevelDiscount.Equal(decimal.NewFromInt(10)))

	assoc, err := s.GetStores().CouponAssociationRepo.Get(ctx, "assoc_discount_3")
	s.NoError(err)
	expired := periodStart.Add(-time.Second)
	assoc.EndDate = &expired
	s.NoError(s.GetStores().CouponAssociationRepo.Update(ctx, assoc))

	second, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(second.TotalDiscount.IsZero(), "want 0, got %s", second.TotalDiscount)
	s.True(second.Total.Equal(decimal.NewFromInt(100)))
	s.Require().Len(second.LineItems, 1)
	s.True(second.LineItems[0].InvoiceLevelDiscount.IsZero(), "want 0, got %s", second.LineItems[0].InvoiceLevelDiscount)

	persistedLineItems, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(persistedLineItems, 1)
	s.True(persistedLineItems[0].InvoiceLevelDiscount.IsZero(), "persisted invoice_level_discount must be reset, got %s", persistedLineItems[0].InvoiceLevelDiscount)

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	apps, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.NoError(err)
	s.Empty(apps, "stale coupon application from the first call must be cleaned up")
}

func (s *RecalculateDiscountOnInvoiceSuite) TestPreservesAndRecalculatesExistingTax() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_4", 10)
	sub := s.createSubscriptionWithAssociation("sub_discount_4", "assoc_discount_4", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_4", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_4")

	// Simulate tax already computed on the pre-discount subtotal (8% of 100 = 8).
	inv.TotalTax = decimal.NewFromInt(8)
	inv.Total = inv.Subtotal.Add(inv.TotalTax)
	inv.AmountDue = inv.Total
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	// No tax rate association exists in this test, so applyTaxesToInvoice finds nothing to
	// recompute and TotalTax carries forward unchanged: Subtotal(100) - Discount(10) + Tax(8) = 98.
	s.True(resp.Total.Equal(decimal.NewFromInt(98)), "want 98, got %s", resp.Total)
	s.True(resp.TotalTax.Equal(decimal.NewFromInt(8)))
}

// TestRecalculatesTaxAgainstRealTaxRate proves tax genuinely recomputes (not just carries
// forward) against a real TaxRate/TaxAssociation when the discount later changes.
func (s *RecalculateDiscountOnInvoiceSuite) TestRecalculatesTaxAgainstRealTaxRate() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_discount_tax", CustomerID: s.testData.customer.ID, PlanID: s.testData.plan.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd,
		BillingAnchor: periodStart, StartDate: periodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	taxPct := decimal.NewFromInt(10)
	tr := &taxrate.TaxRate{
		ID: "taxrate_discount_1", Name: "Discount Test Tax", Code: "discount_test_tax",
		TaxRateStatus: types.TaxRateStatusActive, TaxRateType: types.TaxRateTypePercentage,
		PercentageValue: &taxPct, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
	assoc := &taxassociation.TaxAssociation{
		ID: "taxassoc_discount_1", TaxRateID: tr.ID, EntityType: types.TaxRateEntityTypeSubscription,
		EntityID: sub.ID, Priority: 100, AutoApply: true, Currency: "usd",
		StartDate: periodStart.Add(-24 * time.Hour), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))

	inv := s.createSubscriptionDraftInvoice("inv_discount_tax", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_tax")

	// Compute tax pre-discount: 10% of 100 = 10.
	afterTax, err := s.service.RecalculateTaxesOnInvoice(ctx, inv)
	s.NoError(err)
	s.True(afterTax.TotalTax.Equal(decimal.NewFromInt(10)), "want 10, got %s", afterTax.TotalTax)

	c := s.createPercentageCoupon("coupon_discount_tax", 20)
	couponAssoc := &coupon_association.CouponAssociation{
		ID: "assoc_discount_tax", CouponID: c.ID, SubscriptionID: sub.ID,
		StartDate: periodStart, EnvironmentID: types.GetEnvironmentID(ctx), Coupon: c,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, couponAssoc))

	// Discount(20) drops the taxable base to 80, so tax must recompute to 10% of 80 = 8, not
	// carry forward the stale pre-discount value of 10.
	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(20)), "want 20, got %s", resp.TotalDiscount)
	s.True(resp.TotalTax.Equal(decimal.NewFromInt(8)), "want 8, got %s", resp.TotalTax)
	s.True(resp.Total.Equal(decimal.NewFromInt(88)), "want 88, got %s", resp.Total)
}

// TestTaxAssociationRemovedResetsTax proves TotalTax is reset to zero (not left stale) when
// tax was previously computed but the current tax association no longer resolves any rate.
func (s *RecalculateDiscountOnInvoiceSuite) TestTaxAssociationRemovedResetsTax() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_discount_tax_removed", CustomerID: s.testData.customer.ID, PlanID: s.testData.plan.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd,
		BillingAnchor: periodStart, StartDate: periodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	taxPct := decimal.NewFromInt(10)
	tr := &taxrate.TaxRate{
		ID: "taxrate_discount_removed", Name: "Removed Tax", Code: "removed_tax",
		TaxRateStatus: types.TaxRateStatusActive, TaxRateType: types.TaxRateTypePercentage,
		PercentageValue: &taxPct, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
	assoc := &taxassociation.TaxAssociation{
		ID: "taxassoc_discount_removed", TaxRateID: tr.ID, EntityType: types.TaxRateEntityTypeSubscription,
		EntityID: sub.ID, Priority: 100, AutoApply: true, Currency: "usd",
		StartDate: periodStart.Add(-24 * time.Hour), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))

	inv := s.createSubscriptionDraftInvoice("inv_discount_tax_removed", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_tax_removed")

	afterTax, err := s.service.RecalculateTaxesOnInvoice(ctx, inv)
	s.NoError(err)
	s.True(afterTax.TotalTax.Equal(decimal.NewFromInt(10)), "want 10, got %s", afterTax.TotalTax)

	// Tax association no longer auto-applies -> PrepareTaxRatesForInvoice resolves zero rates.
	assoc.AutoApply = false
	s.NoError(s.GetStores().TaxAssociationRepo.Update(ctx, assoc))

	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(resp.TotalTax.IsZero(), "want 0, got %s", resp.TotalTax)
	s.True(resp.Total.Equal(decimal.NewFromInt(100)), "want 100, got %s", resp.Total)
}

// TestNegativeAmountRemainingClampedToZero proves an overpaid draft (AmountPaid exceeding the
// newly-discounted Total) doesn't end up with a negative AmountRemaining.
func (s *RecalculateDiscountOnInvoiceSuite) TestNegativeAmountRemainingClampedToZero() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_overpaid", 90)
	sub := s.createSubscriptionWithAssociation("sub_discount_overpaid", "assoc_discount_overpaid", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_overpaid", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_overpaid")

	inv.AmountPaid = decimal.NewFromInt(50)
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	// 90% off drops Total to 10, well below the 50 already paid.
	resp, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.True(resp.Total.Equal(decimal.NewFromInt(10)), "want 10, got %s", resp.Total)
	s.True(resp.AmountRemaining.IsZero(), "want 0, got %s", resp.AmountRemaining)
}

func (s *RecalculateDiscountOnInvoiceSuite) TestLineItemLevelCoupon() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_discount_5", CustomerID: s.testData.customer.ID, PlanID: s.testData.plan.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd,
		BillingAnchor: periodStart, StartDate: periodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	sli := &subscription.SubscriptionLineItem{
		ID: "sli_discount_5", SubscriptionID: sub.ID, CustomerID: s.testData.customer.ID,
		PriceID: "price_5", Quantity: decimal.NewFromInt(1),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, sli))

	c := s.createPercentageCoupon("coupon_discount_5", 20)
	assoc := &coupon_association.CouponAssociation{
		ID: "assoc_discount_5", CouponID: c.ID, SubscriptionID: sub.ID,
		SubscriptionLineItemID: &sli.ID,
		StartDate:              periodStart, EnvironmentID: types.GetEnvironmentID(ctx), Coupon: c,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))

	inv := s.createSubscriptionDraftInvoice("inv_discount_5", sub, periodStart, periodEnd, decimal.NewFromInt(50), "price_5")

	first, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(first.LineItems, 1)
	s.True(first.LineItems[0].LineItemDiscount.Equal(decimal.NewFromInt(10)), "want 10, got %s", first.LineItems[0].LineItemDiscount)
	s.True(first.TotalDiscount.Equal(decimal.NewFromInt(10)))

	second, err := s.applyDiscount(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(second.LineItems, 1)
	s.True(second.LineItems[0].LineItemDiscount.Equal(decimal.NewFromInt(10)), "must not double on repeat call, got %s", second.LineItems[0].LineItemDiscount)
}

// TestApplyDiscountAtomicWithOtherFields proves apply_discount:true and a plain field
// (due_date) in the same request both land, since they now share one transaction.
func (s *RecalculateDiscountOnInvoiceSuite) TestApplyDiscountAtomicWithOtherFields() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	c := s.createPercentageCoupon("coupon_discount_atomic", 10)
	sub := s.createSubscriptionWithAssociation("sub_discount_atomic", "assoc_discount_atomic", c, periodStart, periodEnd)
	inv := s.createSubscriptionDraftInvoice("inv_discount_atomic", sub, periodStart, periodEnd, decimal.NewFromInt(100), "price_atomic")

	newDueDate := time.Now().UTC().Add(30 * 24 * time.Hour)
	resp, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{
		DueDate:       &newDueDate,
		ApplyDiscount: true,
	})
	s.NoError(err)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(10)), "want 10, got %s", resp.TotalDiscount)
	s.Require().NotNil(resp.DueDate)
	s.True(resp.DueDate.Equal(newDueDate))

	persisted, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(persisted.TotalDiscount.Equal(decimal.NewFromInt(10)))
	s.Require().NotNil(persisted.DueDate)
	s.True(persisted.DueDate.Equal(newDueDate))
}

// TestApplyDiscountFalseIsNoOp is a regression guard: omitting apply_discount must leave
// discount/coupon state provably untouched, same as before this field existed. Uses a one-off
// invoice (no subscription) so it exercises UpdateInvoice's full existing GetInvoice-based
// return path.
func (s *RecalculateDiscountOnInvoiceSuite) TestApplyDiscountFalseIsNoOp() {
	ctx := s.GetContext()
	inv := s.createOneOffDraftInvoice("inv_discount_noop", decimal.NewFromInt(100))

	newDueDate := time.Now().UTC().Add(30 * 24 * time.Hour)
	resp, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{DueDate: &newDueDate})
	s.NoError(err)
	s.True(resp.TotalDiscount.IsZero(), "apply_discount omitted must not touch discount, got %s", resp.TotalDiscount)
	s.Require().NotNil(resp.DueDate)
	s.True(resp.DueDate.Equal(newDueDate))

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	apps, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.NoError(err)
	s.Empty(apps, "apply_discount omitted must not create any CouponApplication rows")
}
