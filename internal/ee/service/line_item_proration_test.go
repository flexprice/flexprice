package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type LineItemProrationServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc LineItemProrationService
	td  lineItemProrationTestData
}

type lineItemProrationTestData struct {
	sub         *subscription.Subscription
	fixedPrice  *price.Price
	usagePrice  *price.Price
	lineItem    *subscription.SubscriptionLineItem
	periodStart time.Time // Apr 1 00:00:00 UTC
	periodEnd   time.Time // May 1 00:00:00 UTC (exclusive)
}

func TestLineItemProrationService(t *testing.T) {
	suite.Run(t, new(LineItemProrationServiceSuite))
}

func (s *LineItemProrationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *LineItemProrationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *LineItemProrationServiceSuite) setupService() {
	s.svc = NewLineItemProrationService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *LineItemProrationServiceSuite) setupTestData() {
	ctx := s.GetContext()

	s.td.periodStart = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	s.td.periodEnd = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	s.td.fixedPrice = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(20),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.td.fixedPrice))

	s.td.usagePrice = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.td.usagePrice))

	customerID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)
	cust := &customer.Customer{
		ID:        customerID,
		Name:      "Test Customer",
		Email:     "test@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	s.td.sub = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         customerID,
		StartDate:          s.td.periodStart,
		CurrentPeriodStart: s.td.periodStart,
		CurrentPeriodEnd:   s.td.periodEnd,
		BillingAnchor:      s.td.periodStart,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.td.sub))

	s.td.lineItem = &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.td.sub.ID,
		CustomerID:     s.td.sub.CustomerID,
		PriceID:        s.td.fixedPrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.td.periodStart,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, s.td.lineItem))
}

func (s *LineItemProrationServiceSuite) subCopyWithPeriod(start, end time.Time) *subscription.Subscription {
	cp := *s.td.sub
	cp.CurrentPeriodStart = start
	cp.CurrentPeriodEnd = end
	return &cp
}

func (s *LineItemProrationServiceSuite) recordBilled(lineItemID string, amount decimal.Decimal) {
	ctx := s.GetContext()
	periodStart := s.td.periodStart
	periodEnd := s.td.periodEnd

	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             s.td.sub.CustomerID,
		SubscriptionID:         &s.td.sub.ID,
		SubscriptionLineItemID: &lineItemID,
		Amount:                 amount,
		Quantity:               decimal.NewFromInt(1),
		Currency:               "usd",
		PeriodStart:            &periodStart,
		PeriodEnd:              &periodEnd,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}))
}

func (s *LineItemProrationServiceSuite) TestCompute_AddItem_FullPeriod() {
	ctx := s.GetContext()
	effectiveDate := s.td.periodStart // Apr 1 – entire period remaining

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_add_full",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)

	// Coefficient = (May1-1s - Apr1) / (May1-1s - Apr1) = 1.0 → $20.00
	s.True(summary.TotalChargeAmount.Equal(decimal.NewFromInt(20)),
		"full-period add should charge the full price; got %s", summary.TotalChargeAmount)
	s.True(summary.TotalCreditAmount.IsZero(), "no credit expected for AddItem")
	s.Len(summary.ChargeLineItems, 1)
	s.False(summary.IsPreview)
}

func (s *LineItemProrationServiceSuite) TestCompute_AddItem_MidPeriod() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC) // Apr 11

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_add_mid",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)

	// Expected: $20 × (1,727,999 / 2,591,999) = $13.33
	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalChargeAmount.Equal(expected),
		"mid-period add charge mismatch: want %s, got %s", expected, summary.TotalChargeAmount)
	s.True(summary.TotalCreditAmount.IsZero())
	s.Len(summary.ChargeLineItems, 1)
}

func (s *LineItemProrationServiceSuite) TestCompute_AddItem_LastSecond() {
	ctx := s.GetContext()
	effectiveDate := s.td.periodEnd.Add(-time.Second)

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_add_last",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)
	// 1 second remaining out of 2,591,999 → $0.00 after rounding
	s.True(summary.TotalChargeAmount.IsZero(),
		"last-second add should round to $0, got %s", summary.TotalChargeAmount)
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_MidPeriod() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_mid",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)

	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalCreditAmount.Equal(expected),
		"mid-period remove credit mismatch: want %s, got %s", expected, summary.TotalCreditAmount)
	s.True(summary.TotalChargeAmount.IsZero())
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_CreditCappedAtBilledAmount() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(5))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_capped",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().NotNil(summary)

	s.True(summary.TotalCreditAmount.Equal(decimal.NewFromInt(5)),
		"credit must be capped at the $5 actually billed, not the $13.33 of unused list price; got %s",
		summary.TotalCreditAmount)
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_CreditNetsPreviousCredits() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))
	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(-8))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_netted",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().NotNil(summary)

	s.True(summary.TotalCreditAmount.Equal(decimal.NewFromInt(12)),
		"credit must net the $8 already returned against the $20 billed; got %s",
		summary.TotalCreditAmount)
}

// No invoice row is absence of evidence, not evidence of a zero charge: advance
// charges are stamped with the period they fund, so the charge behind a line item
// is not always findable at the proration date. The cap falls back to list price
// rather than silently withholding the credit.
func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_NoBilledRow_FallsBackToListPrice() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_unbilled",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().NotNil(summary)

	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalCreditAmount.Equal(expected),
		"a line item with no billed row must credit the unused list price; got %s",
		summary.TotalCreditAmount)
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_IgnoresOtherPeriodBilling() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	marchStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	lineItemID := s.td.lineItem.ID
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             s.td.sub.CustomerID,
		SubscriptionID:         &s.td.sub.ID,
		SubscriptionLineItemID: &lineItemID,
		// Deliberately tiny: if March were counted it would cap the credit at $1.
		Amount:      decimal.NewFromInt(1),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		PeriodStart: &marchStart,
		PeriodEnd:   &s.td.periodStart,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_other_period",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().NotNil(summary)

	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalCreditAmount.Equal(expected),
		"a previous period's charge must not cap this period's credit; got %s",
		summary.TotalCreditAmount)
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_LongerCadenceCharge() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quarterStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	quarterEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	lineItemID := s.td.lineItem.ID
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             s.td.sub.CustomerID,
		SubscriptionID:         &s.td.sub.ID,
		SubscriptionLineItemID: &lineItemID,
		Amount:                 decimal.NewFromInt(60),
		Quantity:               decimal.NewFromInt(1),
		Currency:               "usd",
		PeriodStart:            &quarterStart,
		PeriodEnd:              &quarterEnd,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_quarterly",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().NotNil(summary)

	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalCreditAmount.Equal(expected),
		"a charge covering this date must fund the credit whatever its cadence; got %s",
		summary.TotalCreditAmount)
}

func (s *LineItemProrationServiceSuite) TestCompute_ProrationChargeLinksToLineItem() {
	ctx := s.GetContext()

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  s.td.periodStart,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_add_fk",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Require().Len(summary.ChargeLineItems, 1)

	s.Require().NotNil(summary.ChargeLineItems[0].SubscriptionLineItemID)
	s.Equal(s.td.lineItem.ID, *summary.ChargeLineItems[0].SubscriptionLineItemID)
}

func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_OnetimeAddon() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	onetimeItem := *s.td.lineItem
	onetimeItem.EndDate = s.td.periodEnd

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_rem_onetime",
		Entries: []LineItemProrationEntry{{
			LineItem: &onetimeItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)

	s.True(summary.TotalCreditAmount.IsZero(), "onetime addon remove must not produce a credit")
	s.True(summary.TotalChargeAmount.IsZero())
	s.Empty(summary.Results, "no proration result expected for onetime remove")
}

func (s *LineItemProrationServiceSuite) TestCompute_SkipsUsagePrice() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	usageItem := *s.td.lineItem
	usageItem.PriceType = types.PRICE_TYPE_USAGE
	usageItem.PriceID = s.td.usagePrice.ID

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorCreateProrations,
		Entries: []LineItemProrationEntry{{
			LineItem:    &usageItem,
			Price:       s.td.usagePrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: decimal.NewFromInt(1),
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)
	s.True(summary.TotalChargeAmount.IsZero(), "usage price must be skipped")
	s.Empty(summary.Results)
}

func (s *LineItemProrationServiceSuite) TestCompute_NoneProrationBehavior() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorNone,
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.NotNil(summary)
	s.True(summary.IsPreview, "behavior=none should produce a preview summary")
	s.True(summary.TotalChargeAmount.IsZero())
	s.True(summary.TotalCreditAmount.IsZero())
}

func (s *LineItemProrationServiceSuite) TestCompute_MultipleEntries_AddAndRemove() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	addItem := *s.td.lineItem
	addItem.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorCreateProrations,
		Entries: []LineItemProrationEntry{
			{
				LineItem: s.td.lineItem,
				Price:    s.td.fixedPrice,
				Action:   types.ProrationActionRemoveItem,
			},
			{
				LineItem:    &addItem,
				Price:       s.td.fixedPrice,
				Action:      types.ProrationActionAddItem,
				NewQuantity: addItem.Quantity,
			},
		},
	}

	summary, err := s.svc.Compute(ctx, req)
	s.NoError(err)
	s.Len(summary.Results, 2)

	expected, _ := decimal.NewFromString("13.33")
	s.True(summary.TotalCreditAmount.Equal(expected), "remove credit mismatch: %s", summary.TotalCreditAmount)
	s.True(summary.TotalChargeAmount.Equal(expected), "add charge mismatch: %s", summary.TotalChargeAmount)
}

func (s *LineItemProrationServiceSuite) TestApply_AddItem_CreatesOneOffInvoice() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_apply_add",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	_, err := s.svc.Apply(ctx, req)
	s.NoError(err)

	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Require().NotEmpty(invoices, "expected one proration invoice to be created")

	inv := invoices[0]
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.True(inv.AmountDue.GreaterThan(decimal.Zero),
		"invoice amount must be positive, got %s", inv.AmountDue)
}

func (s *LineItemProrationServiceSuite) TestApply_TwoChangesSameEffectiveDate_BillSeparately() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	secondPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(35),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, secondPrice))

	secondLineItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.td.sub.ID,
		CustomerID:     s.td.sub.CustomerID,
		PriceID:        secondPrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      effectiveDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, secondLineItem))

	applyAdd := func(lineItem *subscription.SubscriptionLineItem, p *price.Price, key string) error {
		_, applyErr := s.svc.Apply(ctx, LineItemProrationRequest{
			Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
			EffectiveDate:  effectiveDate,
			Behavior:       types.ProrationBehaviorCreateProrations,
			IdempotencyKey: key,
			Entries: []LineItemProrationEntry{{
				LineItem:    lineItem,
				Price:       p,
				Action:      types.ProrationActionAddItem,
				NewQuantity: lineItem.Quantity,
			}},
		})
		return applyErr
	}

	s.NoError(applyAdd(s.td.lineItem, s.td.fixedPrice, "addon_add_assoc_one"))
	s.NoError(applyAdd(secondLineItem, secondPrice, "addon_add_assoc_two"),
		"a second change with the same effective date must still be billed")

	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(err)
	s.Require().Len(invoices, 2, "each change must produce its own proration invoice")

	keys := make(map[string]struct{}, len(invoices))
	total := decimal.Zero
	for _, inv := range invoices {
		s.Require().NotNil(inv.IdempotencyKey, "proration invoice must carry an idempotency key")
		keys[*inv.IdempotencyKey] = struct{}{}
		s.True(inv.AmountDue.GreaterThan(decimal.Zero))
		total = total.Add(inv.AmountDue)
	}
	s.Len(keys, 2, "proration invoices must have distinct idempotency keys")

	// $20 and $35 prorated over the same remaining window (2/3 of the period).
	expected, _ := decimal.NewFromString("36.66")
	s.True(total.Equal(expected), "expected %s billed across both invoices, got %s", expected, total)
}

func (s *LineItemProrationServiceSuite) TestApply_SameChangeTwice_IsIdempotent() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "addon_add_assoc_replayed",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	_, applyErr := s.svc.Apply(ctx, req)
	s.NoError(applyErr)
	_, _ = s.svc.Apply(ctx, req)

	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1, "replaying the same change must not double-bill")
}

func (s *LineItemProrationServiceSuite) TestApply_RemoveItem_CreatesWalletCredit() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_apply_remove",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	_, err := s.svc.Apply(ctx, req)
	s.NoError(err)

	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Require().NotEmpty(wallets, "expected a wallet to be created for the proration credit")

	w := wallets[0]
	expectedCredit, _ := decimal.NewFromString("13.33")
	s.True(w.Balance.GreaterThanOrEqual(expectedCredit),
		"wallet balance %s should be >= expected credit %s", w.Balance, expectedCredit)
}

func (s *LineItemProrationServiceSuite) TestApply_NoneProrationBehavior_IsNoOp() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorNone,
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	_, err := s.svc.Apply(ctx, req)
	s.NoError(err)

	invoices, _ := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(invoices, "no invoice expected for behavior=none")

	wallets, _ := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(wallets, "no wallet expected for behavior=none")
}

func (s *LineItemProrationServiceSuite) TestApply_OnetimeRemove_IsNoOp() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	onetimeItem := *s.td.lineItem
	onetimeItem.EndDate = s.td.periodEnd

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "test_apply_onetime",
		Entries: []LineItemProrationEntry{{
			LineItem: &onetimeItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	_, err := s.svc.Apply(ctx, req)
	s.NoError(err)

	wallets, _ := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(wallets, "onetime remove must not create a wallet credit")
}

func (s *LineItemProrationServiceSuite) TestApply_RemoveItem_IdempotencyKeyUsed() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "idempotency_test_key",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	_, err := s.svc.Apply(ctx, req)
	s.NoError(err)

	_, err = s.svc.Apply(ctx, req)
	s.NoError(err, "duplicate Apply call with same idempotency key must not error")
}

func (s *LineItemProrationServiceSuite) TestCompute_AddItem_TableDriven() {
	ctx := s.GetContext()

	// All expected amounts are pre-calculated for Apr 1–May 1 (30-day month)
	// using SecondBased strategy: charge = $20 × (remainingSeconds / totalSeconds)
	// totalSeconds = 2,591,999  (Apr 1 00:00:00 → Apr 30 23:59:59)
	tests := []struct {
		name          string
		effectiveDate time.Time
		wantCharge    string // decimal string, rounded to 2dp
	}{
		{
			name:          "period_start_full_charge",
			effectiveDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantCharge:    "20.00",
		},
		{
			name: "ten_days_in",
			// Apr 11: remaining 1,727,999 / 2,591,999 → $13.33
			effectiveDate: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			wantCharge:    "13.33",
		},
		{
			name: "twenty_days_in",
			// Apr 21: remaining 863,999 / 2,591,999 → $6.67
			effectiveDate: time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
			wantCharge:    "6.67",
		},
		{
			name: "last_day",
			// Apr 30: remaining 86,399 / 2,591,999 → $0.67
			effectiveDate: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
			wantCharge:    "0.67",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := LineItemProrationRequest{
				Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
				EffectiveDate: tt.effectiveDate,
				Behavior:      types.ProrationBehaviorCreateProrations,
				Entries: []LineItemProrationEntry{{
					LineItem:    s.td.lineItem,
					Price:       s.td.fixedPrice,
					Action:      types.ProrationActionAddItem,
					NewQuantity: s.td.lineItem.Quantity,
				}},
			}

			summary, err := s.svc.Compute(ctx, req)
			s.NoError(err)
			s.NotNil(summary)

			want, _ := decimal.NewFromString(tt.wantCharge)
			s.True(summary.TotalChargeAmount.Equal(want),
				"[%s] charge: want %s, got %s", tt.name, want, summary.TotalChargeAmount)
		})
	}
}
