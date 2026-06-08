package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionLineItemServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionService
	testData struct {
		customer     *customer.Customer
		plan         *plan.Plan
		subscription *subscription.Subscription
		price        *price.Price
		lineItem     *subscription.SubscriptionLineItem
	}
}

func TestSubscriptionLineItemService(t *testing.T) {
	suite.Run(t, new(SubscriptionLineItemServiceSuite))
}

func (s *SubscriptionLineItemServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *SubscriptionLineItemServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionLineItemServiceSuite) setupService() {
	s.service = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
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
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionLineItemServiceSuite) setupTestData() {
	ctx := s.GetContext()
	now := time.Now().UTC()
	lineItemStart := now.AddDate(0, 0, -3) // 3 days ago for effective-date tests

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_lineitem",
		Name:       "Line Item Test Customer",
		Email:      "lineitem@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:        "Line Item Test Plan",
		Description: "Plan for line item tests",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.price = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))

	s.testData.subscription = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
		BillingAnchor:      now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.testData.subscription))

	s.testData.lineItem = &subscription.SubscriptionLineItem{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:  s.testData.subscription.ID,
		CustomerID:      s.testData.customer.ID,
		EntityID:        s.testData.plan.ID,
		EntityType:      types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.testData.plan.Name,
		PriceID:         s.testData.price.ID,
		PriceType:       s.testData.price.Type,
		DisplayName:     "Test line item",
		Quantity:        decimal.NewFromInt(1),
		Currency:        s.testData.subscription.Currency,
		BillingPeriod:   s.testData.subscription.BillingPeriod,
		InvoiceCadence:  types.InvoiceCadenceAdvance,
		StartDate:       lineItemStart,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, s.testData.lineItem))
}

func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromBeforeStartDate() {
	ctx := s.GetContext()
	// EffectiveFrom before line item's StartDate (3 days ago)
	effectiveBefore := s.testData.lineItem.StartDate.Add(-24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveBefore,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective from date must be on or after start date")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.True(li.EndDate.IsZero(), "line item should remain unterminated")
}

func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromOnOrAfterStartDate() {
	ctx := s.GetContext()
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour) // 1 day after start

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveFrom,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.False(resp.SubscriptionLineItem.EndDate.IsZero())
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), li.EndDate.Truncate(time.Second).Unix())
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromBeforeStartDate() {
	ctx := s.GetContext()
	effectiveBefore := s.testData.lineItem.StartDate.Add(-24 * time.Hour)
	newAmount := decimal.NewFromInt(100)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &effectiveBefore,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective date must be on or after line item start date")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.True(li.EndDate.IsZero())
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromBackdated() {
	ctx := s.GetContext()
	// EffectiveFrom in the past but on or after line item start (e.g. 1 day after start)
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour)
	newAmount := decimal.NewFromInt(200)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &effectiveFrom,
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEqual(s.testData.lineItem.ID, resp.SubscriptionLineItem.ID, "new line item should be created")
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.StartDate.Truncate(time.Second).Unix())

	oldLi, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.False(oldLi.EndDate.IsZero())
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), oldLi.EndDate.Truncate(time.Second).Unix())
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromWithoutCriticalField() {
	ctx := s.GetContext()
	effectiveFrom := time.Now().UTC().Add(24 * time.Hour)

	req := dto.UpdateSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveFrom,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective_from requires at least one critical field")
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_Success() {
	ctx := s.GetContext()
	// Use a second price so we can add another line item
	price2 := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, price2))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              price2.ID,
		Quantity:             decimal.NewFromInt(2),
		SkipEntitlementCheck: true,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.SubscriptionLineItem.ID)
	s.Equal(s.testData.subscription.ID, resp.SubscriptionLineItem.SubscriptionID)
	s.Equal(price2.ID, resp.SubscriptionLineItem.PriceID)
	s.True(resp.SubscriptionLineItem.Quantity.Equal(decimal.NewFromInt(2)))

	_, err = s.GetStores().SubscriptionLineItemRepo.Get(ctx, resp.SubscriptionLineItem.ID)
	s.NoError(err)
}

// TestAddSubscriptionLineItem_DateBoundsValidation asserts that when sub is passed, date-bounds validation runs:
// line item start_date cannot be before subscription start date; line item end_date cannot be after subscription end date.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_DateBoundsValidation() {
	ctx := s.GetContext()

	// 1) start_date before subscription start -> validation error
	startBeforeSub := s.testData.subscription.StartDate.Add(-24 * time.Hour)
	reqStartBefore := dto.CreateSubscriptionLineItemRequest{
		PriceID:              s.testData.price.ID,
		StartDate:            &startBeforeSub,
		SkipEntitlementCheck: true,
	}
	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, reqStartBefore)
	s.Error(err)
	s.Contains(err.Error(), "line item start_date cannot be before subscription start date")

	// 2) subscription with end date; line item end_date after subscription end -> validation error
	subEnd := s.testData.subscription.StartDate.Add(30 * 24 * time.Hour)
	subWithEnd := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.subscription.StartDate,
		EndDate:            &subEnd,
		CurrentPeriodStart: s.testData.subscription.StartDate,
		CurrentPeriodEnd:   subEnd,
		BillingAnchor:      s.testData.subscription.StartDate,
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, subWithEnd, nil))

	lineItemEndAfterSub := subEnd.Add(24 * time.Hour)
	reqEndAfter := dto.CreateSubscriptionLineItemRequest{
		PriceID:              s.testData.price.ID,
		EndDate:              &lineItemEndAfterSub,
		SkipEntitlementCheck: true,
	}
	_, err = s.service.AddSubscriptionLineItem(ctx, subWithEnd.ID, reqEndAfter)
	s.Error(err)
	s.Contains(err.Error(), "line item end_date cannot be after subscription end date")
}

// TestAddSubscriptionLineItem_ValidationErrors covers invalid or out-of-bound values: both/neither price,
// start after end, date bounds (line item and inline price), negative quantity.
// ─── Proration integration – AddSubscriptionLineItem ─────────────────────────

// TestAddSubscriptionLineItem_WithCreateProrations_CreatesInvoice verifies that
// calling AddSubscriptionLineItem with ProrationBehavior=create_prorations creates
// a ONE_OFF invoice for the prorated portion of the billing period.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_WithCreateProrations_CreatesInvoice() {
	ctx := s.GetContext()

	// Create a second distinct price so we can add it as a new line item.
	secondPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(30),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, secondPrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              secondPrice.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorCreateProrations,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.SubscriptionLineItem.ID)

	// A ONE_OFF proration invoice should have been created.
	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	var prorationInvoices []*types.InvoiceFilter
	_ = prorationInvoices
	var found bool
	for _, inv := range invoices {
		if inv.InvoiceType == types.InvoiceTypeOneOff {
			found = true
			s.True(inv.AmountDue.GreaterThan(decimal.Zero),
				"proration invoice amount must be positive, got %s", inv.AmountDue)
			break
		}
	}
	s.True(found, "expected a ONE_OFF proration invoice to be created")
}

// TestAddSubscriptionLineItem_NoneProration_NoInvoiceCreated confirms that
// ProrationBehavior=none does not create any invoice.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_NoneProration_NoInvoiceCreated() {
	ctx := s.GetContext()

	thirdPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(40),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, thirdPrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              thirdPrice.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorNone,
	}

	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)

	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	for _, inv := range invoices {
		s.NotEqual(types.InvoiceTypeOneOff, inv.InvoiceType,
			"no ONE_OFF proration invoice expected for behavior=none")
	}
}

// TestAddSubscriptionLineItem_UsagePrice_SkipsProration confirms that adding a
// usage-type price with create_prorations does not produce a charge invoice
// (future consumption is unknown).
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_UsagePrice_SkipsProration() {
	ctx := s.GetContext()

	usagePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              usagePrice.ID,
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorCreateProrations,
	}

	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)

	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	for _, inv := range invoices {
		s.NotEqual(types.InvoiceTypeOneOff, inv.InvoiceType,
			"usage price add must not trigger a proration invoice")
	}
}

// ─── Proration integration – DeleteSubscriptionLineItem ──────────────────────

// TestDeleteSubscriptionLineItem_WithCreateProrations_CreatesWalletCredit verifies
// that deleting a fixed-price line item with create_prorations issues a wallet credit
// for the unused portion of the billing period.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_WithCreateProrations_CreatesWalletCredit() {
	ctx := s.GetContext()
	// effectiveFrom must be (a) >= lineItem.StartDate and (b) within the subscription's current
	// billing period so that FindPeriodForDate can locate it by walking forward.
	// CurrentPeriodStart + 1 hour satisfies both constraints.
	effectiveFrom := s.testData.subscription.CurrentPeriodStart.Add(time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.False(resp.SubscriptionLineItem.EndDate.IsZero())

	// A wallet credit should have been issued.
	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Require().NotEmpty(wallets, "expected a wallet to be created for the proration credit")

	w := wallets[0]
	s.True(w.Balance.GreaterThan(decimal.Zero),
		"wallet balance %s must be positive after proration credit", w.Balance)
}

// TestDeleteSubscriptionLineItem_NoneProration_NoWalletCredit confirms that
// deleting with ProrationBehavior=none does not issue any wallet credit.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_NoneProration_NoWalletCredit() {
	ctx := s.GetContext()
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorNone,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)

	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Empty(wallets, "behavior=none must not create a wallet credit")
}

// TestDeleteSubscriptionLineItem_SnapshotBeforeMutation_OnetimeSkipped verifies
// the critical invariant: the pre-mutation snapshot (EndDate==zero) is passed to
// proration Compute so that onetime-cadence check works correctly.
//
// A line item with EndDate == effectiveFrom after the Update call would otherwise
// be misidentified as a onetime addon and skipped. The snapshot must have EndDate==zero.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_SnapshotBeforeMutation_OnetimeSkipped() {
	ctx := s.GetContext()

	// Create a fresh line item with a non-zero EndDate to simulate a onetime addon.
	// Deleting it with create_prorations should produce NO credit (non-refundable).
	onetimePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ONETIME,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, onetimePrice))

	onetimeEnd := s.testData.subscription.CurrentPeriodEnd
	onetimeItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.testData.subscription.ID,
		CustomerID:     s.testData.customer.ID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        onetimePrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Onetime addon",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_ONETIME,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.testData.lineItem.StartDate,
		EndDate:        onetimeEnd, // non-zero EndDate marks this as onetime/already-terminated
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, onetimeItem))

	// The onetime item already has an EndDate, so DeleteSubscriptionLineItem should
	// reject it as already terminated.
	effectiveFrom := onetimeItem.StartDate.Add(time.Hour)
	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, onetimeItem.ID, req)
	s.Error(err, "deleting an already-terminated line item must return an error")
	s.Contains(err.Error(), "already terminated")

	// No wallet credit should have been issued.
	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Empty(wallets, "no wallet credit expected for already-terminated (onetime) line item")
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_ValidationErrors() {
	ctx := s.GetContext()
	subStart := s.testData.subscription.StartDate
	subEnd := subStart.Add(30 * 24 * time.Hour)
	subWithEnd := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          subStart,
		EndDate:            &subEnd,
		CurrentPeriodStart: subStart,
		CurrentPeriodEnd:   subEnd,
		BillingAnchor:      subStart,
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, subWithEnd, nil))

	validInlinePrice := &dto.SubscriptionPriceCreateRequest{
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Amount:             lo.ToPtr(decimal.NewFromInt(1)),
		LookupKey:          "validation_test",
	}

	tests := []struct {
		name        string
		subID       string
		req         dto.CreateSubscriptionLineItemRequest
		wantErrCont string
	}{
		{
			name:        "both price_id and price",
			subID:       s.testData.subscription.ID,
			req:         dto.CreateSubscriptionLineItemRequest{PriceID: s.testData.price.ID, Price: validInlinePrice, SkipEntitlementCheck: true},
			wantErrCont: "cannot provide both price_id and price",
		},
		{
			name:        "neither price_id nor price",
			subID:       s.testData.subscription.ID,
			req:         dto.CreateSubscriptionLineItemRequest{SkipEntitlementCheck: true},
			wantErrCont: "either price_id or price is required",
		},
		{
			name:  "start_date after end_date",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				StartDate:            lo.ToPtr(subStart.Add(48 * time.Hour)),
				EndDate:              lo.ToPtr(subStart.Add(24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "start_date cannot be after end_date",
		},
		{
			name:  "line item start_date before subscription start",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				StartDate:            lo.ToPtr(subStart.Add(-24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "line item start_date cannot be before subscription start date",
		},
		{
			name:  "line item end_date after subscription end",
			subID: subWithEnd.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				EndDate:              lo.ToPtr(subEnd.Add(24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "line item end_date cannot be after subscription end date",
		},
		{
			name:  "inline price start_date before subscription start",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				Price: &dto.SubscriptionPriceCreateRequest{
					Type:               types.PRICE_TYPE_FIXED,
					PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:     types.InvoiceCadenceAdvance,
					Amount:             lo.ToPtr(decimal.NewFromInt(1)),
					LookupKey:          "inline_bad_start",
					StartDate:          lo.ToPtr(subStart.Add(-24 * time.Hour)),
				},
				SkipEntitlementCheck: true,
			},
			wantErrCont: "price start_date cannot be before subscription start date",
		},
		{
			name:  "inline price end_date after subscription end",
			subID: subWithEnd.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				Price: &dto.SubscriptionPriceCreateRequest{
					Type:               types.PRICE_TYPE_FIXED,
					PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:     types.InvoiceCadenceAdvance,
					Amount:             lo.ToPtr(decimal.NewFromInt(1)),
					LookupKey:          "inline_bad_end",
					EndDate:            lo.ToPtr(subEnd.Add(24 * time.Hour)),
				},
				SkipEntitlementCheck: true,
			},
			wantErrCont: "price end_date cannot be after subscription end date",
		},
		{
			name:  "negative quantity",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				Quantity:             decimal.NewFromInt(-1),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "quantity must be positive",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.service.AddSubscriptionLineItem(ctx, tt.subID, tt.req)
			s.Error(err, "expected validation error for: %s", tt.name)
			s.Contains(err.Error(), tt.wantErrCont, "error should contain: %s", tt.wantErrCont)
		})
	}
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_BySubscriptionID() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}

	resp, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)
	found := lo.ContainsBy(resp.Items, func(item *dto.SubscriptionLineItemResponse) bool {
		return item.SubscriptionLineItem.ID == s.testData.lineItem.ID
	})
	s.True(found)
	s.GreaterOrEqual(resp.Pagination.Total, 1)
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_InvalidExpand() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.QueryFilter = types.NewDefaultQueryFilter()
	filter.QueryFilter.Expand = lo.ToPtr("plan")

	_, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.Error(err)
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_ExpandPrices() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.QueryFilter = types.NewDefaultQueryFilter()
	filter.QueryFilter.Expand = lo.ToPtr("prices")

	resp, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)
	var target *dto.SubscriptionLineItemResponse
	for _, item := range resp.Items {
		if item.SubscriptionLineItem.ID == s.testData.lineItem.ID {
			target = item
			break
		}
	}
	s.Require().NotNil(target)
	s.Require().NotNil(target.Price)
	s.Equal(s.testData.price.ID, target.Price.ID)
}

// TestAddSubscriptionLineItem_WithBuckets_MaterializesPrices verifies that when a
// CreateSubscriptionLineItemRequest includes CommitmentTimeBuckets, the service
// calls resolveBucketPrices and the persisted line item's CommitmentTimeBuckets
// carries materialized bucket prices (non-empty PriceID and ID).
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_WithBuckets_MaterializesPrices() {
	ctx := s.GetContext()

	// 1. Create a meter with BucketSize (required for CommitmentWindowed=true validation).
	meterWithBucket := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Meter",
		EventName: "bucket_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, meterWithBucket))

	// 2. Create a SUBSCRIPTION-scoped usage price referencing the meter.
	usagePriceForBucket := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           s.testData.subscription.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            meterWithBucket.ID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePriceForBucket))

	// 3. Build the request with one CommitmentTimeBuckets entry.
	overageFactor := decimal.NewFromFloat(1.5)
	commitmentAmount := decimal.NewFromInt(500)
	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:                 usagePriceForBucket.ID,
		SkipEntitlementCheck:    true,
		CommitmentAmount:        &commitmentAmount,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overageFactor,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 17, Minute: 0},
				Price: dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(15)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             s.testData.subscription.ID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            "bucket_test_price",
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(200),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
				TrueUpEnabled:   true,
			},
		},
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.Require().NotNil(resp)

	li := resp.SubscriptionLineItem

	// The persisted line item must have exactly one materialized bucket.
	s.Require().Len(li.CommitmentTimeBuckets, 1, "expected 1 materialized bucket on the line item")

	bucket := li.CommitmentTimeBuckets[0]

	// PriceID must be non-empty (set by resolveBucketPrices).
	s.NotEmpty(bucket.PriceID, "bucket.PriceID must be set after materialization")

	// ID must be a non-empty UUID (assigned by resolveBucketPrices).
	s.NotEmpty(bucket.ID, "bucket.ID must be set after materialization")

	// The created price must exist in the repo as SUBSCRIPTION-scoped.
	createdPrice, getErr := s.GetStores().PriceRepo.Get(ctx, bucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, createdPrice.EntityType)

	// Verify the line item is persisted correctly.
	persisted, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.NoError(getErr)
	s.Require().Len(persisted.CommitmentTimeBuckets, 1, "persisted line item must have 1 bucket")
	s.Equal(bucket.PriceID, persisted.CommitmentTimeBuckets[0].PriceID)
}

// TestResolveBucketPrices_CreatesSubscriptionPriceAndAssignsID verifies that
// resolveBucketPrices creates a SUBSCRIPTION-scoped Price for each bucket and
// populates the resulting TimeOfDayBuckets with the created price's ID.
func (s *SubscriptionLineItemServiceSuite) TestResolveBucketPrices_CreatesSubscriptionPriceAndAssignsID() {
	ctx := s.GetContext()

	// Access the concrete *subscriptionService so we can call the unexported helper.
	concreteSvc := s.service.(*subscriptionService)

	reqs := []dto.CommitmentBucketRequest{
		{
			Start: types.Bucket{Hour: 9, Minute: 0},
			End:   types.Bucket{Hour: 17, Minute: 0},
			Price: dto.CreatePriceRequest{
				Amount:             lo.ToPtr(decimal.NewFromInt(10)),
				Currency:           "usd",
				EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
				EntityID:           s.testData.subscription.ID,
				Type:               types.PRICE_TYPE_FIXED,
				PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
				SkipEntityValidation: true,
			},
			CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(100),
			OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			TrueUpEnabled:   true,
		},
	}

	out, err := concreteSvc.resolveBucketPrices(ctx, s.testData.subscription.ID, reqs)
	s.NoError(err)
	s.Require().Len(out, 1)

	bucket := out[0]

	// ID should be a non-empty UUID assigned by the helper.
	s.NotEmpty(bucket.ID)

	// PriceID should reference the newly created price.
	s.NotEmpty(bucket.PriceID)

	// Verify the price actually exists in the repo with SUBSCRIPTION entity type.
	createdPrice, getErr := s.GetStores().PriceRepo.Get(ctx, bucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, createdPrice.EntityType)
	s.Equal(s.testData.subscription.ID, createdPrice.EntityID)

	// Other bucket fields should be copied verbatim from the request.
	s.Equal(types.Bucket{Hour: 9, Minute: 0}, bucket.Start)
	s.Equal(types.Bucket{Hour: 17, Minute: 0}, bucket.End)
	s.Equal(types.COMMITMENT_TYPE_AMOUNT, bucket.CommitmentType)
	s.True(bucket.CommitmentValue.Equal(decimal.NewFromInt(100)))
	s.Require().NotNil(bucket.OverageFactor)
	s.True(bucket.OverageFactor.Equal(decimal.NewFromFloat(1.5)))
	s.True(bucket.TrueUpEnabled)
}

// TestResolveBucketPrices_EmptySlice returns nil, nil without error.
func (s *SubscriptionLineItemServiceSuite) TestResolveBucketPrices_EmptySlice() {
	ctx := s.GetContext()
	concreteSvc := s.service.(*subscriptionService)

	out, err := concreteSvc.resolveBucketPrices(ctx, s.testData.subscription.ID, nil)
	s.NoError(err)
	s.Nil(out)

	out2, err2 := concreteSvc.resolveBucketPrices(ctx, s.testData.subscription.ID, []dto.CommitmentBucketRequest{})
	s.NoError(err2)
	s.Nil(out2)
}

// TestResolveBucketPrices_MultipleBuckets verifies that each bucket gets its own price and a distinct UUID.
func (s *SubscriptionLineItemServiceSuite) TestResolveBucketPrices_MultipleBuckets() {
	ctx := s.GetContext()
	concreteSvc := s.service.(*subscriptionService)

	makeBucketReq := func(startH, endH int, lookupKey string) dto.CommitmentBucketRequest {
		return dto.CommitmentBucketRequest{
			Start: types.Bucket{Hour: startH, Minute: 0},
			End:   types.Bucket{Hour: endH, Minute: 0},
			Price: dto.CreatePriceRequest{
				Amount:               lo.ToPtr(decimal.NewFromInt(5)),
				Currency:             "usd",
				EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
				EntityID:             s.testData.subscription.ID,
				Type:                 types.PRICE_TYPE_FIXED,
				PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
				BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount:   1,
				BillingModel:         types.BILLING_MODEL_FLAT_FEE,
				InvoiceCadence:       types.InvoiceCadenceAdvance,
				LookupKey:            lookupKey,
				SkipEntityValidation: true,
			},
			CommitmentType:  types.COMMITMENT_TYPE_QUANTITY,
			CommitmentValue: decimal.NewFromInt(50),
			OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
		}
	}

	reqs := []dto.CommitmentBucketRequest{
		makeBucketReq(0, 8, "bucket_multi_test_1"),
		makeBucketReq(8, 16, "bucket_multi_test_2"),
		makeBucketReq(16, 24, "bucket_multi_test_3"),
	}

	out, err := concreteSvc.resolveBucketPrices(ctx, s.testData.subscription.ID, reqs)
	s.NoError(err)
	s.Require().Len(out, 3)

	// All bucket IDs must be distinct.
	ids := map[string]bool{}
	priceIDs := map[string]bool{}
	for _, b := range out {
		s.NotEmpty(b.ID)
		s.NotEmpty(b.PriceID)
		s.False(ids[b.ID], "bucket ID must be unique: %s", b.ID)
		s.False(priceIDs[b.PriceID], "price ID must be unique: %s", b.PriceID)
		ids[b.ID] = true
		priceIDs[b.PriceID] = true
	}
}

// ─── Bucket update semantics (Task 10) ───────────────────────────────────────

// makeBucketLineItem is a helper that creates a windowed-commitment usage line
// item with one materialized bucket on the given subscription, returning the
// persisted line item and the ID of the bucket price so callers can assert
// against it.
func (s *SubscriptionLineItemServiceSuite) makeBucketLineItem(subID string, meterID string, lookupKey string) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()

	// Subscription-scoped usage price referencing the meter.
	usagePr := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           subID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            meterID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePr))

	overage := decimal.NewFromFloat(1.5)
	commitment := decimal.NewFromInt(300)
	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:                 usagePr.ID,
		SkipEntitlementCheck:    true,
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 17, Minute: 0},
				Price: dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(20)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             subID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            lookupKey,
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(150),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
				TrueUpEnabled:   true,
			},
		},
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, subID, req)
	s.Require().NoError(err)
	s.Require().Len(resp.CommitmentTimeBuckets, 1)
	return resp.SubscriptionLineItem
}

// TestUpdateSubscriptionLineItem_ReplaceAllBuckets verifies that when
// UpdateSubscriptionLineItem is called with a new commitment_time_buckets list,
// the update creates a successor line item whose buckets are freshly
// materialized (new PriceIDs / IDs), replacing the old ones. This is the
// "replace-all" semantic: CommitmentBucketRequest has no stable ID field, so
// the entire bucket set is replaced atomically.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ReplaceAllBuckets() {
	ctx := s.GetContext()

	// Create a meter with BucketSize for windowed commitment validation.
	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Update Meter",
		EventName: "bucket_update_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	// Create a line item with one bucket (9:00–17:00 @ $20).
	original := s.makeBucketLineItem(s.testData.subscription.ID, m.ID, "update_bucket_orig")
	oldBucketPriceID := original.CommitmentTimeBuckets[0].PriceID
	oldBucketID := original.CommitmentTimeBuckets[0].ID

	// Update: replace the single bucket with a different time window (0:00–8:00 @ $12).
	overage := decimal.NewFromFloat(1.2)
	commitment := decimal.NewFromInt(400)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(true),
		CommitmentTimeBuckets: lo.ToPtr([]dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 0, Minute: 0},
				End:   types.Bucket{Hour: 8, Minute: 0},
				Price: dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(12)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             s.testData.subscription.ID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            "update_bucket_new",
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(200),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
				TrueUpEnabled:   false,
			},
		}),
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem

	// A new line item must have been created (different ID).
	s.NotEqual(original.ID, newLI.ID, "successor line item must have a new ID")

	// New line item must have exactly one materialized bucket.
	s.Require().Len(newLI.CommitmentTimeBuckets, 1, "successor must have 1 bucket")

	newBucket := newLI.CommitmentTimeBuckets[0]

	// The new bucket must have fresh IDs (not inherited from the original).
	s.NotEmpty(newBucket.PriceID, "new bucket must have a PriceID")
	s.NotEmpty(newBucket.ID, "new bucket must have an ID")
	s.NotEqual(oldBucketPriceID, newBucket.PriceID, "new bucket PriceID must differ from old")
	s.NotEqual(oldBucketID, newBucket.ID, "new bucket ID must differ from old")

	// The new bucket time window must match the request (0:00–8:00).
	s.Equal(types.Bucket{Hour: 0, Minute: 0}, newBucket.Start)
	s.Equal(types.Bucket{Hour: 8, Minute: 0}, newBucket.End)

	// The new price must exist in the repo.
	newPrice, getErr := s.GetStores().PriceRepo.Get(ctx, newBucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, newPrice.EntityType)

	// Original line item must now be terminated.
	oldLI, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, original.ID)
	s.NoError(getErr)
	s.False(oldLI.EndDate.IsZero(), "original line item must be terminated after update")
}

// TestUpdateSubscriptionLineItem_ClearBuckets verifies that passing an explicit
// empty slice for commitment_time_buckets clears all buckets on the successor
// line item.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ClearBuckets() {
	ctx := s.GetContext()

	// Create a meter with BucketSize.
	m2 := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Clear Meter",
		EventName: "bucket_clear_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m2))

	// Create a line item with one bucket.
	original := s.makeBucketLineItem(s.testData.subscription.ID, m2.ID, "clear_bucket_orig")
	s.Require().Len(original.CommitmentTimeBuckets, 1, "precondition: original has 1 bucket")

	// Update with an explicit empty slice — this should clear buckets.
	// We must also change a critical field to trigger ShouldCreateNewLineItem.
	// Providing an explicit empty CommitmentTimeBuckets alone is sufficient because
	// CommitmentTimeBuckets != nil triggers ShouldCreateNewLineItem.
	overage := decimal.NewFromFloat(1.3)
	commitment := decimal.NewFromInt(250)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(false),
		CommitmentTimeBuckets:   lo.ToPtr([]dto.CommitmentBucketRequest{}), // explicit empty
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem
	s.NotEqual(original.ID, newLI.ID, "successor line item must have a new ID")
	s.Empty(newLI.CommitmentTimeBuckets, "successor must have no buckets when empty slice is passed")
}

// TestUpdateSubscriptionLineItem_NilBuckets_InheritsExisting verifies that
// when CommitmentTimeBuckets is omitted (nil pointer) in the update request,
// the successor line item inherits the existing buckets verbatim.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_NilBuckets_InheritsExisting() {
	ctx := s.GetContext()

	// Create a meter with BucketSize.
	m3 := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Inherit Meter",
		EventName: "bucket_inherit_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m3))

	// Create a line item with one bucket.
	original := s.makeBucketLineItem(s.testData.subscription.ID, m3.ID, "inherit_bucket_orig")
	s.Require().Len(original.CommitmentTimeBuckets, 1, "precondition: original has 1 bucket")
	origBucketPriceID := original.CommitmentTimeBuckets[0].PriceID

	// Update without providing CommitmentTimeBuckets (nil pointer) —
	// the update changes the overage factor, which triggers ShouldCreateNewLineItem
	// via CommitmentOverageFactor.
	newOverage := decimal.NewFromFloat(1.8)
	commitment := decimal.NewFromInt(300)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &newOverage,
		CommitmentWindowed:      lo.ToPtr(true),
		// CommitmentTimeBuckets is nil — should inherit original buckets
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem
	s.NotEqual(original.ID, newLI.ID, "successor must have new ID")

	// Successor must carry the original bucket (same PriceID — inherited, not re-created).
	s.Require().Len(newLI.CommitmentTimeBuckets, 1, "successor must inherit 1 bucket")
	s.Equal(origBucketPriceID, newLI.CommitmentTimeBuckets[0].PriceID,
		"inherited bucket PriceID must match original")
}
