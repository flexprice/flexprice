package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	invoice_domain "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// CouponApplicationServiceSuite covers the VAPT-driven redemption enforcement
// for ApplyCouponsToInvoice: one-off invoice applications must count against
// MaxRedemptions, subscription-attached applications must not (already counted
// at association time), and repeated compute of the same draft must not
// double-count.
type CouponApplicationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CouponApplicationService
	testData struct {
		customer *customer.Customer
	}
}

func TestCouponApplicationService(t *testing.T) {
	suite.Run(t, new(CouponApplicationServiceSuite))
}

func (s *CouponApplicationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewCouponApplicationService(s.newServiceParams())
	s.setupTestData()
}

func (s *CouponApplicationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CouponApplicationServiceSuite) newServiceParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		PriceRepo:                stores.PriceRepo,
		MeterRepo:                stores.MeterRepo,
		CustomerRepo:             stores.CustomerRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		InvoiceLineItemRepo:      stores.InvoiceLineItemRepo,
		CouponRepo:               stores.CouponRepo,
		CouponAssociationRepo:    stores.CouponAssociationRepo,
		CouponApplicationRepo:    stores.CouponApplicationRepo,
		SettingsRepo:             stores.SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
	}
}

func (s *CouponApplicationServiceSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_coupon_app",
		Name:       "Coupon Application Customer",
		Email:      "coupon-app@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))
}

func (s *CouponApplicationServiceSuite) createPublishedCoupon(name string, maxRedemptions *int) *coupon_domain.Coupon {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(10)
	c := &coupon_domain.Coupon{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:           name,
		Type:           types.CouponTypePercentage,
		Cadence:        types.CouponCadenceOnce,
		PercentageOff:  &pct,
		MaxRedemptions: maxRedemptions,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

func (s *CouponApplicationServiceSuite) createOneOffInvoiceWithLineItem(id string, priceID string, amount decimal.Decimal) *invoice_domain.Invoice {
	ctx := s.GetContext()
	li := &invoice_domain.InvoiceLineItem{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		CustomerID:            s.testData.customer.ID,
		InvoiceID:             id,
		PriceID:               lo.ToPtr(priceID),
		Amount:                amount,
		Currency:              "usd",
		Quantity:              decimal.NewFromInt(1),
		LineItemDiscount:      decimal.Zero,
		InvoiceLevelDiscount:  decimal.Zero,
		PrepaidCreditsApplied: decimal.Zero,
		BaseModel:             types.GetDefaultBaseModel(ctx),
	}
	inv := &invoice_domain.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		Currency:        "usd",
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		Subtotal:        amount,
		Total:           amount,
		AmountDue:       amount,
		AmountRemaining: amount,
		LineItems:       []*invoice_domain.InvoiceLineItem{li},
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

// TestApplyCouponsToInvoice_OneOff_IncrementsRedemption is the core VAPT fix:
// applying a coupon to a one-off invoice must increment TotalRedemptions.
// Before this fix, one-off invoices could apply a coupon unlimited times
// without ever counting against MaxRedemptions (the counter was only bumped
// during subscription association).
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_OneOff_IncrementsRedemption() {
	ctx := s.GetContext()

	maxRedemptions := 2
	c := s.createPublishedCoupon("one-off limited", &maxRedemptions)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)

	// First one-off apply: MUST increment TotalRedemptions from 0 to 1.
	inv1 := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))
	res, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv1,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)
	s.True(res.TotalDiscountAmount.IsPositive(), "discount should apply on first one-off use")

	updated, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, updated.TotalRedemptions, "TotalRedemptions must be 1 after first one-off application")

	// Second one-off apply: also increments. TotalRedemptions -> 2.
	inv2 := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))
	_, err = s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv2,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)
	updated, err = s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(2, updated.TotalRedemptions, "TotalRedemptions must be 2 after second one-off application")

	// Third one-off apply: MaxRedemptions=2 reached. couponService.ApplyDiscount
	// rejects via coupon.IsValid() (existing check), so the coupon is silently
	// skipped and no discount is applied. TotalRedemptions must stay at 2.
	inv3 := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))
	res, err = s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv3,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)
	s.True(res.TotalDiscountAmount.IsZero(), "exhausted coupon must not discount the invoice")

	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv3.ID}
	rows, err := s.GetStores().CouponApplicationRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Empty(rows, "exhausted coupon must not persist a CouponApplication row on inv3")

	updated, err = s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(2, updated.TotalRedemptions, "TotalRedemptions must stay at 2 after exhausted apply")
}

// TestApplyCouponsToInvoice_SameCouponOnLineItemAndInvoice_CountsOnce confirms
// that a coupon appearing in both line-item and invoice-level lists on the same
// invoice counts as one redemption, not two.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_SameCouponOnLineItemAndInvoice_CountsOnce() {
	ctx := s.GetContext()

	max := 5
	c := s.createPublishedCoupon("dedup coupon", &max)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(200))

	_, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
		InvoiceCoupons: []dto.InvoiceCoupon{
			{CouponID: c.ID},
		},
	})
	s.Require().NoError(err)

	updated, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, updated.TotalRedemptions, "same coupon on line-item + invoice-level = one redemption per invoice")
}

// TestApplyCouponsToInvoice_SubscriptionAssociation_NoIncrement confirms the
// subscription path is unaffected: coupons carrying a CouponAssociationID were
// already counted at association time and must not be re-counted per invoice.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_SubscriptionAssociation_NoIncrement() {
	ctx := s.GetContext()

	c := s.createPublishedCoupon("sub-attached", nil)

	// Simulate the association-time increment that ApplyCouponsToSubscription
	// would have performed: bump TotalRedemptions to 1 before the invoice apply.
	s.NoError(s.GetStores().CouponRepo.IncrementRedemptions(ctx, c.ID, nil))
	before, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, before.TotalRedemptions)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))

	assocID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION)
	_, err = s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID, CouponAssociationID: &assocID},
		},
	})
	s.Require().NoError(err)

	after, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, after.TotalRedemptions, "subscription-attached apply must NOT re-increment redemptions")
}

// TestApplyCouponsToInvoice_RepeatedCompute_NoDoubleIncrement confirms
// idempotency: if ComputeInvoice is retried on the same one-off Draft (e.g.
// after a prior successful compute but failed finalize), the second apply
// must not double-count the redemption OR persist duplicate CouponApplication
// rows. This is the persistence-level guard requested by review: the earlier
// fix protected TotalRedemptions but the persistence loop was still inserting
// new rows on every retry.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_RepeatedCompute_NoDoubleIncrement() {
	ctx := s.GetContext()

	max := 2
	c := s.createPublishedCoupon("idempotent coupon", &max)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))

	// First apply.
	_, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)

	afterFirst, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, afterFirst.TotalRedemptions)

	appFilter := types.NewNoLimitCouponApplicationFilter()
	appFilter.InvoiceIDs = []string{inv.ID}
	appFilter.CouponIDs = []string{c.ID}
	firstRows, err := s.GetStores().CouponApplicationRepo.List(ctx, appFilter)
	s.Require().NoError(err)
	s.Len(firstRows, 1, "first apply should persist exactly one CouponApplication")

	// Second apply on same invoice (recompute retry). Reset the line-item
	// discount as reconcileLineItems would; the CouponApplication row from
	// the first apply persists.
	inv.LineItems[0].LineItemDiscount = decimal.Zero
	recomputeRes, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)
	s.True(recomputeRes.TotalDiscountAmount.IsPositive(), "recompute must still apply the discount so the invoice total is correct after reconcileLineItems reset it")
	s.True(inv.LineItems[0].LineItemDiscount.IsPositive(), "recompute must restore the line-item discount reset by reconcileLineItems")

	afterSecond, err := s.GetStores().CouponRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Equal(1, afterSecond.TotalRedemptions, "recompute of same invoice must not double-count redemption")

	secondRows, err := s.GetStores().CouponApplicationRepo.List(ctx, appFilter)
	s.Require().NoError(err)
	s.Len(secondRows, 1, "recompute must not persist duplicate CouponApplication rows for the same (invoice, coupon)")
}

// createInvoiceWithTwoLineItemsSharingPrice builds an invoice whose two line items carry the
// SAME price ID but distinct subscription line item IDs — the shape produced by attaching one
// addon twice, or by grouped invoicing merging children that share a plan.
func (s *CouponApplicationServiceSuite) createInvoiceWithTwoLineItemsSharingPrice(
	priceID string,
	amountA, amountB decimal.Decimal,
) (*invoice_domain.Invoice, string, string) {
	ctx := s.GetContext()
	invID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE)
	sliA := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
	sliB := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)

	newLI := func(sliID string, amount decimal.Decimal) *invoice_domain.InvoiceLineItem {
		return &invoice_domain.InvoiceLineItem{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
			CustomerID:             s.testData.customer.ID,
			InvoiceID:              invID,
			PriceID:                lo.ToPtr(priceID),
			SubscriptionLineItemID: lo.ToPtr(sliID),
			Amount:                 amount,
			Currency:               "usd",
			Quantity:               decimal.NewFromInt(1),
			LineItemDiscount:       decimal.Zero,
			InvoiceLevelDiscount:   decimal.Zero,
			PrepaidCreditsApplied:  decimal.Zero,
			BaseModel:              types.GetDefaultBaseModel(ctx),
		}
	}

	total := amountA.Add(amountB)
	inv := &invoice_domain.Invoice{
		ID:              invID,
		CustomerID:      s.testData.customer.ID,
		Currency:        "usd",
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		Subtotal:        total,
		Total:           total,
		AmountDue:       total,
		AmountRemaining: total,
		LineItems:       []*invoice_domain.InvoiceLineItem{newLI(sliA, amountA), newLI(sliB, amountB)},
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv, sliA, sliB
}

// TestApplyCouponsToInvoice_SharedPriceAcrossLineItems_EachDiscountedIndependently pins the fix
// for price-ID-keyed matching: two line items sharing one price must each receive their own
// line-item coupon. Keying by price ID collapsed them (last-write-wins), so both coupons landed
// on a single line item — double-discounting it while the other went untouched.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_SharedPriceAcrossLineItems_EachDiscountedIndependently() {
	ctx := s.GetContext()

	c := s.createPublishedCoupon("shared price coupon", nil)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	amountA, amountB := decimal.NewFromInt(200), decimal.NewFromInt(300)
	inv, sliA, sliB := s.createInvoiceWithTwoLineItemsSharingPrice(priceID, amountA, amountB)

	_, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, SubscriptionLineItemID: lo.ToPtr(sliA), CouponID: c.ID},
			{LineItemID: priceID, SubscriptionLineItemID: lo.ToPtr(sliB), CouponID: c.ID},
		},
	})
	s.Require().NoError(err)

	// 10% coupon: distinct amounts prove each discount was computed against its own line item.
	bySLI := map[string]decimal.Decimal{}
	for _, li := range inv.LineItems {
		bySLI[lo.FromPtr(li.SubscriptionLineItemID)] = li.LineItemDiscount
	}
	s.True(decimal.NewFromInt(20).Equal(bySLI[sliA]), "line item A must be discounted against its own amount, got %s", bySLI[sliA])
	s.True(decimal.NewFromInt(30).Equal(bySLI[sliB]), "line item B must be discounted against its own amount, got %s", bySLI[sliB])
}

// TestApplyCouponsToInvoice_NoSubscriptionLineItemID_FallsBackToPriceID guards the one-off path:
// coupons carrying no subscription line item must still match on price ID.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_NoSubscriptionLineItemID_FallsBackToPriceID() {
	ctx := s.GetContext()

	c := s.createPublishedCoupon("fallback coupon", nil)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(400))

	_, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID},
		},
	})
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(inv.LineItems[0].LineItemDiscount), "price-ID fallback must still discount one-off line items")
}

// TestApplyCouponsToInvoice_UnmatchedSubLineItemID_KeepsLegacyPriceBehaviour guards against a
// regression on invoices predating subscription_line_item_id: when a coupon names a subscription
// line item the invoice does not carry, resolution must fall through to price matching and keep
// its historical last-wins result. Refusing to resolve here would strip a discount that used to
// apply. Ambiguity is fixed by stamping subscription_line_item_id, not by dropping the discount.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_UnmatchedSubLineItemID_KeepsLegacyPriceBehaviour() {
	ctx := s.GetContext()

	c := s.createPublishedCoupon("legacy fallback", nil)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv, _, sliB := s.createInvoiceWithTwoLineItemsSharingPrice(priceID, decimal.NewFromInt(200), decimal.NewFromInt(300))

	unknownSLI := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
	res, err := s.service.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, SubscriptionLineItemID: lo.ToPtr(unknownSLI), CouponID: c.ID},
		},
	})
	s.Require().NoError(err)

	// 10% of the last line item sharing the price — exactly what price-only matching produced.
	s.True(decimal.NewFromInt(30).Equal(res.TotalDiscountAmount),
		"unmatched subscription line item must fall back to price matching, got %s", res.TotalDiscountAmount)
	for _, li := range inv.LineItems {
		if lo.FromPtr(li.SubscriptionLineItemID) == sliB {
			s.True(decimal.NewFromInt(30).Equal(li.LineItemDiscount),
				"last line item sharing the price takes the discount, got %s", li.LineItemDiscount)
			continue
		}
		s.True(li.LineItemDiscount.IsZero(), "earlier line items sharing the price stay undiscounted")
	}
}

// TestApplyCouponsToInvoice_SubscriptionAssociation_RepeatedCompute_NoDuplicateRows
// covers a subscription-attached coupon (non-empty CouponAssociationID) recomputed
// twice on the same invoice — e.g. an initial ComputeInvoice followed by a
// finalization recompute. The row-existence idempotency check previously only ran
// for one-off applications (nil CouponAssociationID), so association-based coupons
// skipped it entirely and every recompute inserted another CouponApplication row,
// which surfaced as the same discount listed twice on the invoice PDF.
func (s *CouponApplicationServiceSuite) TestApplyCouponsToInvoice_SubscriptionAssociation_RepeatedCompute_NoDuplicateRows() {
	ctx := s.GetContext()

	c := s.createPublishedCoupon("sub-attached repeated", nil)

	priceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	inv := s.createOneOffInvoiceWithLineItem(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), priceID, decimal.NewFromInt(100))
	assocID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION)

	req := dto.ApplyCouponsToInvoiceRequest{
		Invoice: inv,
		LineItemCoupons: []dto.InvoiceLineItemCoupon{
			{LineItemID: priceID, CouponID: c.ID, CouponAssociationID: &assocID},
		},
	}

	// First compute pass (e.g. invoice creation).
	_, err := s.service.ApplyCouponsToInvoice(ctx, req)
	s.Require().NoError(err)

	appFilter := types.NewNoLimitCouponApplicationFilter()
	appFilter.InvoiceIDs = []string{inv.ID}
	appFilter.CouponIDs = []string{c.ID}
	firstRows, err := s.GetStores().CouponApplicationRepo.List(ctx, appFilter)
	s.Require().NoError(err)
	s.Len(firstRows, 1, "first apply should persist exactly one CouponApplication")

	// Second compute pass on the same invoice (e.g. finalization recompute).
	inv.LineItems[0].LineItemDiscount = decimal.Zero
	_, err = s.service.ApplyCouponsToInvoice(ctx, req)
	s.Require().NoError(err)

	secondRows, err := s.GetStores().CouponApplicationRepo.List(ctx, appFilter)
	s.Require().NoError(err)
	s.Len(secondRows, 1, "recompute of a subscription-attached coupon must not persist duplicate CouponApplication rows")
}

// A fixed-amount coupon carries a currency and is matched against the subscription's,
// so an unsupported one would silently never apply. Percentage coupons carry none and
// are unrestricted.
func (s *CouponApplicationServiceSuite) TestCreateCoupon_CustomCurrencyEnforcement() {
	cfg := types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			"mac": {
				Name:                  "MoEngage AI Credits",
				Symbol:                "MAC",
				FiatConversionFactors: map[string]decimal.Decimal{"usd": decimal.NewFromFloat(0.1)},
			},
		},
		DefaultFiatCurrency: "usd",
	}
	s.NoError(cfg.Validate())
	value, err := utils.ToMap(cfg)
	s.NoError(err)
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyCustomCurrencyConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}))

	couponSvc := NewCouponService(s.newServiceParams())
	amountOff := decimal.NewFromInt(5)

	tests := []struct {
		name     string
		currency *string
		wantErr  bool
	}{
		{name: "custom currency is allowed", currency: lo.ToPtr("mac")},
		{name: "default fiat currency is allowed", currency: lo.ToPtr("usd")},
		{name: "unconfigured currency is rejected", currency: lo.ToPtr("eur"), wantErr: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := couponSvc.CreateCoupon(s.GetContext(), dto.CreateCouponRequest{
				Name:      "Coupon " + tt.name,
				Type:      types.CouponTypeFixed,
				AmountOff: &amountOff,
				Currency:  tt.currency,
				Cadence:   types.CouponCadenceOnce,
			})
			if tt.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Equal(*tt.currency, resp.Currency)
		})
	}
}

// A percentage coupon has no currency, so enforcement does not apply to it.
func (s *CouponApplicationServiceSuite) TestCreateCoupon_PercentageCouponUnrestricted() {
	percentageOff := decimal.NewFromInt(10)
	couponSvc := NewCouponService(s.newServiceParams())

	resp, err := couponSvc.CreateCoupon(s.GetContext(), dto.CreateCouponRequest{
		Name:          "Ten percent",
		Type:          types.CouponTypePercentage,
		PercentageOff: &percentageOff,
		Cadence:       types.CouponCadenceOnce,
	})
	s.NoError(err)
	s.Empty(resp.Currency)
}
