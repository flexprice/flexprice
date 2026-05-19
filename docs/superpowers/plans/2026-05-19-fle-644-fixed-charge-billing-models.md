# FLE-644: Fixed Charge Billing Models — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive integration tests and DTO validation tests covering all three fixed-charge billing models (FLAT_FEE, PACKAGE, TIERED) with both ADVANCE and ARREAR invoice cadences, and fix any bugs found along the way.

**Architecture:** New test suite `FixedChargeBillingSuite` in `internal/service/fixed_charge_billing_test.go` that extends `BaseServiceTestSuite`. Each test method creates its own plan/price/subscription, calls `BillingService.CalculateFixedCharges()`, and asserts line item amounts. DTO validation tests live in `internal/api/dto/price_validation_test.go` and call `CreatePriceRequest.Validate()` directly.

**Tech Stack:** Go testify/suite, `testutil.BaseServiceTestSuite`, `shopspring/decimal`, `samber/lo`, `time`

---

## File Map

| Action | Path | Purpose |
|---|---|---|
| **Create** | `internal/service/fixed_charge_billing_test.go` | Service integration tests for all 8 fixed-charge scenarios |
| **Create** | `internal/api/dto/price_validation_test.go` | DTO validation tests (rejection + acceptance) |
| **Modify (if bugs found)** | `internal/service/billing.go` | Fix any calculation bugs uncovered by tests |
| **Modify (if bugs found)** | `internal/service/price.go` | Fix cost calculation bugs in PACKAGE/TIERED branches |

---

## Task 1: Create test file skeleton with suite setup

**Files:**
- Create: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Write the skeleton**

```go
package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type FixedChargeBillingSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService
}

func TestFixedChargeBilling(t *testing.T) {
	suite.Run(t, new(FixedChargeBillingSuite))
}

func (s *FixedChargeBillingSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewBillingService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		EventRepo:                s.GetStores().EventRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		PaymentRepo:              s.GetStores().PaymentRepo,
		CouponAssociationRepo:    s.GetStores().CouponAssociationRepo,
		CouponRepo:               s.GetStores().CouponRepo,
		CouponApplicationRepo:    s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		TaxRateRepo:              s.GetStores().TaxRateRepo,
		TaxAssociationRepo:       s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:           s.GetStores().TaxAppliedRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:         s.GetStores().FeatureUsageRepo,
	})
}

// seedCustomerAndPlan creates an Aruba customer and a plan, returns both.
func (s *FixedChargeBillingSuite) seedCustomerAndPlan(custID, planID string) (*customer.Customer, *plan.Plan) {
	ctx := s.GetContext()
	cust := &customer.Customer{
		ID:         custID,
		ExternalID: "ext_" + custID,
		Name:       "Aruba",
		Email:      "aruba@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        planID,
		Name:      "Aruba Plan " + planID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	return cust, pl
}

// seedPrice creates a price in PriceRepo and returns it.
func (s *FixedChargeBillingSuite) seedPrice(p *price.Price) *price.Price {
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

// seedSubscriptionWithLineItem creates a subscription and one line item, returns the subscription
// (with LineItems populated in memory).
func (s *FixedChargeBillingSuite) seedSubscriptionWithLineItem(
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) *subscription.Subscription {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	sub.LineItems = []*subscription.SubscriptionLineItem{li}
	return sub
}

// seedSubscriptionWithLineItems creates a subscription and multiple line items.
func (s *FixedChargeBillingSuite) seedSubscriptionWithLineItems(
	sub *subscription.Subscription,
	lis []*subscription.SubscriptionLineItem,
) *subscription.Subscription {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lis))
	sub.LineItems = lis
	return sub
}

// fixedPeriod returns a standard monthly period: Jan 1 – Feb 1 2025.
func fixedPeriod() (time.Time, time.Time) {
	start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}
```

- [ ] **Step 2: Verify file compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go build ./internal/service/...
```
Expected: no errors (no tests run yet, skeleton only)

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): add FixedChargeBillingSuite skeleton with shared helpers"
```

---

## Task 2: FLAT_FEE ADVANCE test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

Add inside `FixedChargeBillingSuite`:

```go
func (s *FixedChargeBillingSuite) TestFlatFee_Advance_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_ff_adv", "plan_ff_adv")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ff_adv",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ff_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_ff_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EntityID:       pl.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        p.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Flat Fee",
		Quantity:       decimal.NewFromInt(3),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      periodStart,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 line item for flat fee advance")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(300)),
		"expected $100 × 3 = $300, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(300)))
}
```

- [ ] **Step 2: Run test — expect pass or identify bug**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestFlatFee_Advance_Monthly
```
Expected: PASS. If FAIL, read the error and trace into `billing.go:CalculateFixedCharges` → `price.go:CalculateCost` FLAT_FEE branch.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify flat fee advance monthly produces correct line item amount"
```

---

## Task 3: FLAT_FEE ARREAR test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestFlatFee_Arrear_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_ff_arr", "plan_ff_arr")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ff_arr",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ff_arr",
		PlanID:             pl.ID,
		CustomerID:         "cust_ff_arr",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Flat Fee Arrear",
		Quantity:           decimal.NewFromInt(3),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	// Arrear: invoice generated at period end — period end falls inclusively in (periodStart, periodEnd]
	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 arrear line item")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(300)),
		"expected $100 × 3 = $300 for arrear, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestFlatFee_Arrear_Monthly
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify flat fee arrear monthly produces correct line item amount"
```

---

## Task 4: PACKAGE ADVANCE test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestPackage_Advance_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_pkg_adv", "plan_pkg_adv")

	// $50 per package of 10 units; quantity=25 → ceil(25/10)=3 packages → $150
	p := s.seedPrice(&price.Price{
		ID:                 "price_pkg_adv",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_PACKAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,
			Round:    types.RoundUp,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_pkg_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_pkg_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Package Fee",
		Quantity:           decimal.NewFromInt(25),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 package line item")
	// ceil(25/10) = 3 packages × $50 = $150
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(150)),
		"expected ceil(25/10)=3 packages × $50 = $150, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestPackage_Advance_Monthly
```
Expected: PASS. If FAIL with wrong amount, trace `price.go:CalculateCost` → `BILLING_MODEL_PACKAGE` branch; check `TransformQuantity.DivideBy` and `Round` are read correctly.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify package billing model advance computes ceil(qty/divide_by) correctly"
```

---

## Task 5: PACKAGE ARREAR test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestPackage_Arrear_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_pkg_arr", "plan_pkg_arr")

	p := s.seedPrice(&price.Price{
		ID:                 "price_pkg_arr",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_PACKAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,
			Round:    types.RoundUp,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_pkg_arr",
		PlanID:             pl.ID,
		CustomerID:         "cust_pkg_arr",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Package Fee Arrear",
		Quantity:           decimal.NewFromInt(25),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1)
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(150)),
		"expected $150 for arrear package, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestPackage_Arrear_Monthly
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify package billing model arrear produces correct amount"
```

---

## Task 6: TIERED SLAB ADVANCE test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestTieredSlab_Advance_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_slab_adv", "plan_slab_adv")

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// Tiers: 0–10 @ $5/unit, 11–50 @ $3/unit; quantity=20
	// SLAB: (10×$5) + (10×$3) = $50 + $30 = $80
	p := s.seedPrice(&price.Price{
		ID:                 "price_slab_adv",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_slab_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_slab_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Tiered Slab Fee",
		Quantity:           decimal.NewFromInt(20),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 tiered slab line item")
	// (10×$5) + (10×$3) = $80
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(80)),
		"expected SLAB (10×$5)+(10×$3)=$80, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestTieredSlab_Advance_Monthly
```
Expected: PASS. If FAIL: trace `price.go:calculateTieredCost` SLAB branch — check tier boundaries are inclusive and that remaining quantity after each tier is calculated correctly.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify tiered SLAB fixed charge produces progressive tier math"
```

---

## Task 7: TIERED VOLUME ADVANCE test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestTieredVolume_Advance_Monthly() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_vol_adv", "plan_vol_adv")

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// VOLUME: all 20 units priced at final matching tier → tier 2 ($3) → 20×$3 = $60
	p := s.seedPrice(&price.Price{
		ID:                 "price_vol_adv",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_VOLUME,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_vol_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_vol_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Tiered Volume Fee",
		Quantity:           decimal.NewFromInt(20),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 tiered volume line item")
	// VOLUME: all 20 units at tier-2 rate $3 → $60
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(60)),
		"expected VOLUME 20×$3=$60, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestTieredVolume_Advance_Monthly
```
Expected: PASS. If FAIL: check the VOLUME branch in `calculateTieredCost` — it should find the tier where `quantity <= tier.UpTo` and apply `quantity × tier.UnitAmount` (no progressive splitting).

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify tiered VOLUME fixed charge prices all units at matching tier"
```

---

## Task 8: FLAT_FEE ANNUAL ADVANCE test

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestFlatFee_Annual_Advance() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, pl := s.seedCustomerAndPlan("cust_ann_adv", "plan_ann_adv")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ann_adv",
		Amount:             decimal.NewFromInt(1200),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ann_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_ann_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Annual Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 annual line item")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(1200)),
		"expected full annual amount $1200, got %s", result.LineItems[0].Amount)
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestFlatFee_Annual_Advance
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify flat fee annual advance produces full period amount"
```

---

## Task 9: Mixed plan test (FLAT_FEE + TIERED on same plan)

**Files:**
- Modify: `internal/service/fixed_charge_billing_test.go`

- [ ] **Step 1: Add test method**

```go
func (s *FixedChargeBillingSuite) TestMixedPlan_FlatFeeAndTieredSlab_Advance() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_mix", "plan_mix")

	// Price 1: FLAT_FEE $100 × 1 = $100
	pFlat := s.seedPrice(&price.Price{
		ID:                 "price_mix_flat",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// Price 2: TIERED SLAB, quantity=20 → $80
	pTiered := s.seedPrice(&price.Price{
		ID:                 "price_mix_tiered",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_mix",
		PlanID:             pl.ID,
		CustomerID:         "cust_mix",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liFlat := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            pFlat.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Flat Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liTiered := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            pTiered.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Tiered Fee",
		Quantity:           decimal.NewFromInt(20),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItems(sub, []*subscription.SubscriptionLineItem{liFlat, liTiered})

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 2, "expected 2 line items for mixed plan")

	var flatAmt, tieredAmt decimal.Decimal
	for _, item := range result.LineItems {
		switch lo.FromPtr(item.PriceID) {
		case pFlat.ID:
			flatAmt = item.Amount
		case pTiered.ID:
			tieredAmt = item.Amount
		}
	}
	s.True(flatAmt.Equal(decimal.NewFromInt(100)), "flat fee line item should be $100, got %s", flatAmt)
	s.True(tieredAmt.Equal(decimal.NewFromInt(80)), "tiered slab line item should be $80, got %s", tieredAmt)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(180)), "total should be $180, got %s", result.TotalAmount)
}
```

- [ ] **Step 2: Add `lo` import** (add `"github.com/samber/lo"` to the import block at top of file)

- [ ] **Step 3: Run test**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling/TestMixedPlan_FlatFeeAndTieredSlab_Advance
```
Expected: PASS.

- [ ] **Step 4: Run all new tests together to check for interference**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v -race ./internal/service/... -run TestFixedChargeBilling
```
Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/fixed_charge_billing_test.go
git commit -m "test(fle-644): verify mixed fixed plan produces two line items with correct individual amounts"
```

---

## Task 10: DTO validation tests

**Files:**
- Create: `internal/api/dto/price_validation_test.go`

- [ ] **Step 1: Create file**

```go
package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validBaseRequest returns a minimal valid FLAT_FEE fixed price request.
func validBaseRequest() dto.CreatePriceRequest {
	return dto.CreatePriceRequest{
		Amount:             lo.ToPtr(decimal.NewFromInt(100)),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_123",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
	}
}

// --- Rejection cases ---

func TestCreatePriceRequest_Validate_RejectsMissingBillingModel(t *testing.T) {
	req := validBaseRequest()
	req.BillingModel = ""
	err := req.Validate()
	require.Error(t, err, "empty billing_model should fail validation")
}

func TestCreatePriceRequest_Validate_RejectsTieredWithNoTiers(t *testing.T) {
	req := validBaseRequest()
	req.BillingModel = types.BILLING_MODEL_TIERED
	req.Amount = nil
	req.Tiers = nil
	err := req.Validate()
	require.Error(t, err, "TIERED model with no tiers should fail validation")
}

func TestCreatePriceRequest_Validate_RejectsFlatFeeWithZeroAmount(t *testing.T) {
	req := validBaseRequest()
	zero := decimal.Zero
	req.Amount = &zero
	err := req.Validate()
	require.Error(t, err, "FLAT_FEE with amount=0 should fail validation")
}

// --- Acceptance cases ---

func TestCreatePriceRequest_Validate_AcceptsFlatFeeWithPositiveAmount(t *testing.T) {
	req := validBaseRequest()
	err := req.Validate()
	assert.NoError(t, err, "valid FLAT_FEE with amount>0 should pass")
}

func TestCreatePriceRequest_Validate_AcceptsTieredWithValidTiers(t *testing.T) {
	req := validBaseRequest()
	req.BillingModel = types.BILLING_MODEL_TIERED
	req.Amount = nil
	upTo10 := uint64(10)
	req.Tiers = []dto.CreatePriceTier{
		{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
		{UpTo: nil, UnitAmount: decimal.NewFromInt(3)},
	}
	err := req.Validate()
	assert.NoError(t, err, "TIERED with valid tiers should pass")
}

func TestCreatePriceRequest_Validate_AcceptsPackageWithTransformQuantity(t *testing.T) {
	req := validBaseRequest()
	req.BillingModel = types.BILLING_MODEL_PACKAGE
	req.TransformQuantity = &price.TransformQuantity{
		DivideBy: 10,
		Round:    types.RoundUp,
	}
	err := req.Validate()
	assert.NoError(t, err, "PACKAGE with valid transform_quantity should pass")
}
```

- [ ] **Step 2: Run validation tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -v ./internal/api/dto/... -run TestCreatePriceRequest_Validate
```
Expected: all 5 tests PASS. If a rejection test unexpectedly passes (no error), add the missing validation rule to `CreatePriceRequest.Validate()` in `internal/api/dto/price.go`.

- [ ] **Step 4: Commit**

```bash
git add internal/api/dto/price_validation_test.go
git commit -m "test(fle-644): add DTO validation tests for CreatePriceRequest covering rejection and acceptance"
```

---

## Task 11: Run full regression and verify no existing tests broken

- [ ] **Step 1: Run entire service test suite**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -race ./internal/service/... 2>&1 | tail -20
```
Expected: all tests PASS, no regressions in existing `BillingServiceSuite`.

- [ ] **Step 2: Run full DTO tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go test -race ./internal/api/dto/...
```
Expected: PASS.

- [ ] **Step 3: Vet**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/quirky-varahamihira-b7ee7c && go vet ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git commit --allow-empty -m "test(fle-644): all fixed charge billing model tests passing, no regressions"
```

---

## Notes on Bug Fixing

If any test above fails with wrong output, the fix pattern is:

1. FLAT_FEE wrong → check `PriceService.CalculateCost` flat fee branch: `amount × quantity`
2. PACKAGE wrong → check `TransformQuantity.DivideBy` is used, `Round=up` calls `math.Ceil`, `Round=down` calls `math.Floor`
3. TIERED SLAB wrong → `calculateTieredCost` SLAB mode: iterate tiers, cap quantity at `tier.UpTo - prevUpTo`, sum
4. TIERED VOLUME wrong → find the tier where `quantity <= tier.UpTo`, return `quantity × tier.UnitAmount`
5. Proration applied unexpectedly → check that FIXED prices with `BillingPeriod != sub.BillingPeriod` are not being prorated when not needed
