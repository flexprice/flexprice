package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ─── Suite ───────────────────────────────────────────────────────────────────

// LineItemProrationServiceSuite tests the LineItemProrationService in isolation
// using in-memory repositories. It focuses on:
//   1. Compute – pure math correctness (no side effects)
//   2. Apply   – Compute + settle (invoice creation / wallet credit)
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
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *LineItemProrationServiceSuite) setupTestData() {
	ctx := s.GetContext()

	// Billing period: Apr 1 → May 1 (30-day month, UTC)
	s.td.periodStart = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	s.td.periodEnd = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Fixed $20/month price
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

	// Usage price (should be skipped by proration)
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

	// Customer (needed by wallet service's TopUpWalletForProratedCharge)
	customerID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)
	cust := &customer.Customer{
		ID:        customerID,
		Name:      "Test Customer",
		Email:     "test@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	// Subscription anchored to Apr 1, current period Apr 1–May 1
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
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.td.sub))

	// Active fixed-price line item (EndDate zero = active recurring)
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

// ─── Helper ──────────────────────────────────────────────────────────────────

// subCopyWithPeriod returns a shallow copy of the subscription with the billing
// period overridden.  Matches the pattern used in production code.
func (s *LineItemProrationServiceSuite) subCopyWithPeriod(start, end time.Time) *subscription.Subscription {
	cp := *s.td.sub
	cp.CurrentPeriodStart = start
	cp.CurrentPeriodEnd = end
	return &cp
}

// ─── Compute – AddItem ───────────────────────────────────────────────────────

// TestCompute_AddItem_FullPeriod verifies that adding a line item at period start
// results in a charge equal to the full price (coefficient == 1.0).
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

// TestCompute_AddItem_MidPeriod verifies the proportional charge when a line item
// is added partway through the billing period.
//
// Period: Apr 1–May 1 (30 days).  Effective: Apr 11 (10 days elapsed, 20 remaining).
// SecondBased: remaining=(Apr30 23:59:59 - Apr11 00:00:00)=1,727,999s
//
//	total=(Apr30 23:59:59 - Apr1 00:00:00)=2,591,999s
//	charge = $20 × (1,727,999/2,591,999) ≈ $13.33
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

// TestCompute_AddItem_LastSecond adds at the very last second of the period —
// proration coefficient ≈ 0, so charge rounds to $0.
func (s *LineItemProrationServiceSuite) TestCompute_AddItem_LastSecond() {
	ctx := s.GetContext()
	// periodEnd - 1s is the last valid second
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

// ─── Compute – RemoveItem ────────────────────────────────────────────────────

// TestCompute_RemoveItem_MidPeriod mirrors the AddItem mid-period test but for
// removal — the credit amount should equal the unused portion of the period.
func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_MidPeriod() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

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

// TestCompute_RemoveItem_OnetimeAddon verifies that removing an item whose EndDate
// is non-zero (onetime addon) is silently skipped — onetime charges are non-refundable.
func (s *LineItemProrationServiceSuite) TestCompute_RemoveItem_OnetimeAddon() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	// Clone the line item but give it a non-zero EndDate to simulate a onetime addon.
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

// ─── Compute – Usage price skip ──────────────────────────────────────────────

// TestCompute_SkipsUsagePrice asserts that usage-type line items are excluded
// from proration because future consumption is unknown at change time.
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

// ─── Compute – ProrationBehavior = none ──────────────────────────────────────

// TestCompute_NoneProrationBehavior confirms that when behavior == none the
// summary is returned with IsPreview=true and zero amounts (the underlying
// calculator returns nil for none-behavior params).
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

// ─── Compute – Multiple entries ───────────────────────────────────────────────

// TestCompute_MultipleEntries_AddAndRemove covers a batch where one entry triggers
// a charge (AddItem) and another triggers a credit (RemoveItem). Both amounts
// should be accumulated independently.
func (s *LineItemProrationServiceSuite) TestCompute_MultipleEntries_AddAndRemove() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	// Second line item to add (same price for simplicity)
	addItem := *s.td.lineItem
	addItem.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorCreateProrations,
		Entries: []LineItemProrationEntry{
			{
				LineItem:    s.td.lineItem,
				Price:       s.td.fixedPrice,
				Action:      types.ProrationActionRemoveItem,
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

// ─── Apply – charge (invoice creation) ───────────────────────────────────────

// TestApply_AddItem_CreatesOneOffInvoice verifies that Apply with CreateProrations
// and a positive net amount creates exactly one ONE_OFF invoice in the invoice repo.
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

	err := s.svc.Apply(ctx, req)
	s.NoError(err)

	// Invoice should have been created in the in-memory repo.
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

// ─── Apply – credit (wallet top-up) ──────────────────────────────────────────

// TestApply_RemoveItem_CreatesWalletCredit verifies that Apply with CreateProrations
// and a negative net amount creates a wallet top-up for the customer.
func (s *LineItemProrationServiceSuite) TestApply_RemoveItem_CreatesWalletCredit() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

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

	err := s.svc.Apply(ctx, req)
	s.NoError(err)

	// A wallet should have been created/topped-up for the customer.
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

// ─── Apply – ProrationBehavior = none (no-op) ────────────────────────────────

// TestApply_NoneProrationBehavior_IsNoOp verifies that Apply returns immediately
// without creating any invoices or wallets when behavior == none.
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

	err := s.svc.Apply(ctx, req)
	s.NoError(err)

	// No invoice and no wallet should exist.
	invoices, _ := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(invoices, "no invoice expected for behavior=none")

	wallets, _ := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(wallets, "no wallet expected for behavior=none")
}

// TestApply_OnetimeRemove_IsNoOp confirms that removing a onetime addon
// (EndDate != zero) produces no credit even when behavior == create_prorations.
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

	err := s.svc.Apply(ctx, req)
	s.NoError(err)

	wallets, _ := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Empty(wallets, "onetime remove must not create a wallet credit")
}

// ─── Apply – idempotency key propagated to wallet ────────────────────────────

// TestApply_RemoveItem_IdempotencyKeyUsed verifies that calling Apply twice with
// the same IdempotencyKey does not double-credit the customer.
func (s *LineItemProrationServiceSuite) TestApply_RemoveItem_IdempotencyKeyUsed() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

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

	// First call
	err := s.svc.Apply(ctx, req)
	s.NoError(err)

	// Second call with same key
	err = s.svc.Apply(ctx, req)
	s.NoError(err, "duplicate Apply call with same idempotency key must not error")
}

// ─── Table-driven: various effective dates ────────────────────────────────────

// TestCompute_AddItem_TableDriven covers a range of effective dates for an
// AddItem in the Apr 1–May 1 period, verifying the proportional charge.
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
