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

// ─────────────────────────────────────────────
// Suite definition
// ─────────────────────────────────────────────

type SubscriptionModificationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionModificationService
}

func TestSubscriptionModificationServiceSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionModificationServiceSuite))
}

func (s *SubscriptionModificationServiceSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *SubscriptionModificationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewSubscriptionModificationService(s.buildServiceParams())
}

func (s *SubscriptionModificationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionModificationServiceSuite) buildServiceParams() ServiceParams {
	return ServiceParams{
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
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	}
}

// ─────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) createCustomer(externalID string) *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		BaseModel:  types.GetDefaultBaseModel(ctx),
		ExternalID: externalID,
		Name:       "Test " + externalID,
		Email:      externalID + "@test.com",
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

func (s *SubscriptionModificationServiceSuite) createPlan() *plan.Plan {
	ctx := s.GetContext()
	p := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, p))
	return p
}

func (s *SubscriptionModificationServiceSuite) createActiveSub(customerID string) *subscription.Subscription {
	ctx := s.GetContext()
	now := s.GetNow()
	p := s.createPlan()
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         customerID,
		PlanID:             p.ID,
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      now,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		StartDate:          now,
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionModificationServiceSuite) createFixedLineItem(subID, customerID string, qty decimal.Decimal, cadence types.InvoiceCadence) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		StartDate:      now,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// createFixedPrice inserts a Price record into PriceRepo and returns it.
// Used by proration tests that need GetPrice to succeed.
func (s *SubscriptionModificationServiceSuite) createFixedPrice(
	amount decimal.Decimal,
	cadence types.InvoiceCadence,
) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		Amount:         amount,
		Currency:       "USD",
		Type:           types.PRICE_TYPE_FIXED,
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

// createFixedLineItemWithPrice creates a SubscriptionLineItem tied to a specific PriceID.
// Use this instead of createFixedLineItem when proration tests require GetPrice to resolve.
func (s *SubscriptionModificationServiceSuite) createFixedLineItemWithPrice(
	subID, customerID string,
	qty decimal.Decimal,
	cadence types.InvoiceCadence,
	priceID string,
) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		StartDate:      now,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// setSubPeriod overrides CurrentPeriodStart and CurrentPeriodEnd on the subscription
// stored in SubRepo. Use in math-regression tests that need a deterministic calendar month.
func (s *SubscriptionModificationServiceSuite) setSubPeriod(subID string, start, end time.Time) {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart = start
	sub.CurrentPeriodEnd = end
	sub.BillingAnchor = start
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
}

// ─────────────────────────────────────────────
// Advance proration tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_Advance verifies invoice creation for upgrades,
// wallet credit for downgrades, and same-quantity no-ops on ADVANCE (in-advance) line items.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_Advance() {
	type tc struct {
		name               string
		oldQty             decimal.Decimal
		newQty             decimal.Decimal
		effectiveDayOffset int                      // days after periodStart; -1 = special sentinel (periodEnd - 1s)
		wantLineItems      int                      // expected len(ChangedResources.LineItems)
		wantInvoiceAction  dto.ChangedInvoiceAction // "created", "wallet_credit", or "" (no invoice)
		wantNoOp           bool                     // old line item EndDate must remain zero
	}
	cases := []tc{
		{
			name:               "upgrade_midperiod",
			oldQty:             decimal.NewFromInt(1),
			newQty:             decimal.NewFromInt(3),
			effectiveDayOffset: 15,
			wantLineItems:      2,
			wantInvoiceAction:  dto.ChangedInvoiceActionCreated,
		},
		{
			name:               "downgrade_midperiod",
			oldQty:             decimal.NewFromInt(3),
			newQty:             decimal.NewFromInt(1),
			effectiveDayOffset: 15,
			wantLineItems:      2,
			wantInvoiceAction:  dto.ChangedInvoiceActionWalletCredit,
		},
		{
			name:               "upgrade_at_period_start",
			oldQty:             decimal.NewFromInt(1),
			newQty:             decimal.NewFromInt(3),
			effectiveDayOffset: 0,
			wantLineItems:      2,
			wantInvoiceAction:  dto.ChangedInvoiceActionCreated,
		},
		{
			name:               "upgrade_near_period_end",
			oldQty:             decimal.NewFromInt(1),
			newQty:             decimal.NewFromInt(3),
			effectiveDayOffset: -1, // sentinel: periodEnd - 1 second
			wantLineItems:      2,
			wantInvoiceAction:  "", // proration amount rounds to 0 at 1s before period end
		},
		{
			name:               "same_quantity_noop",
			oldQty:             decimal.NewFromInt(5),
			newQty:             decimal.NewFromInt(5),
			effectiveDayOffset: 5,
			wantLineItems:      0,
			wantInvoiceAction:  "",
			wantNoOp:           true,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			periodStart := s.GetNow()
			periodEnd := periodStart.AddDate(0, 1, 0)

			var effectiveDate time.Time
			switch tc.effectiveDayOffset {
			case -1:
				effectiveDate = periodEnd.Add(-time.Second)
			default:
				effectiveDate = periodStart.AddDate(0, 0, tc.effectiveDayOffset)
			}

			cust := s.createCustomer("adv-" + tc.name)
			sub := s.createActiveSub(cust.ID)

			priceAmount := decimal.NewFromInt(50)
			p := s.createFixedPrice(priceAmount, types.InvoiceCadenceAdvance)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceAdvance, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
					LineItems: []dto.LineItemQuantityChange{
						{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
					},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)

			if tc.wantNoOp {
				// Old line item must be untouched
				orig, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(err)
				s.True(orig.EndDate.IsZero(), "EndDate must remain zero for no-op")
				s.Empty(resp.ChangedResources.Invoices)
				return
			}

			if tc.wantInvoiceAction == "" {
				s.Empty(resp.ChangedResources.Invoices,
					"expected no proration invoice or wallet credit (tc=%s)", tc.name)
				return
			}

			s.Require().Len(resp.ChangedResources.Invoices, 1)
			inv := resp.ChangedResources.Invoices[0]
			s.Equal(tc.wantInvoiceAction, inv.Action)
			s.NotEqual(dto.ChangedInvoiceStatusFromPaymentStatus(types.PaymentStatusFailed), inv.Status)

			if tc.wantInvoiceAction == dto.ChangedInvoiceActionCreated {
				// Fetch real invoice and verify amount is positive and approximately correct
				realInv, fetchErr := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
				s.Require().NoError(fetchErr)
				s.True(realInv.AmountDue.GreaterThan(decimal.Zero),
					"invoice amount must be positive for upgrade, got %s", realInv.AmountDue.String())

				// Derive expected amount using same second-based formula as the service
				effectivePeriodEnd := periodEnd.Add(-time.Second)
				totalSec := effectivePeriodEnd.Sub(periodStart).Seconds()
				remainingSec := effectivePeriodEnd.Sub(effectiveDate).Seconds()
				if remainingSec < 0 {
					remainingSec = 0
				}
				coeff := decimal.NewFromFloat(remainingSec / totalSec)
				qtyDelta := tc.newQty.Sub(tc.oldQty)
				expectedAmt := qtyDelta.Mul(priceAmount).Mul(coeff)
				tolerance := decimal.NewFromFloat(0.01)
				diff := realInv.AmountDue.Sub(expectedAmt).Abs()
				s.True(diff.LessThanOrEqual(tolerance),
					"invoice amount %s should be ≈ %s (diff=%s)",
					realInv.AmountDue.String(), expectedAmt.String(), diff.String())
				s.Require().NotNil(inv.Invoice, "execute upgrade should include full invoice in changed_resources")
			}

			if tc.wantInvoiceAction == dto.ChangedInvoiceActionWalletCredit {
				s.Equal(dto.ChangedInvoiceStatusWalletIssued, inv.Status)
				s.Require().NotNil(inv.WalletTransaction, "execute downgrade should include wallet transaction")
				wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
				s.Require().NoError(err)
				s.Require().NotEmpty(wallets, "a PRE_PAID wallet must exist after downgrade credit")
				var totalBalance decimal.Decimal
				for _, w := range wallets {
					totalBalance = totalBalance.Add(w.Balance)
				}
				s.True(totalBalance.GreaterThan(decimal.Zero),
					"wallet balance must be positive after downgrade credit")
			}
		})
	}
}

// ─────────────────────────────────────────────
// Arrear tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_Arrear verifies that ARREAR line items are versioned
// (old item ended, new item created) but no proration invoice or wallet credit is issued.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_Arrear() {
	type tc struct {
		name          string
		oldQty        decimal.Decimal
		newQty        decimal.Decimal
		wantLineItems int
		wantNoOp      bool
	}
	cases := []tc{
		{name: "increase_arrear", oldQty: decimal.NewFromInt(1), newQty: decimal.NewFromInt(5), wantLineItems: 2},
		{name: "decrease_arrear", oldQty: decimal.NewFromInt(5), newQty: decimal.NewFromInt(1), wantLineItems: 2},
		{name: "same_qty_arrear", oldQty: decimal.NewFromInt(3), newQty: decimal.NewFromInt(3), wantLineItems: 0, wantNoOp: true},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			effectiveDate := s.GetNow().AddDate(0, 0, 5)

			cust := s.createCustomer("arr-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			p := s.createFixedPrice(decimal.NewFromInt(30), types.InvoiceCadenceArrear)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceArrear, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
					LineItems: []dto.LineItemQuantityChange{
						{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
					},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)
			s.Empty(resp.ChangedResources.Invoices, "ARREAR items must never generate a proration invoice")

			if !tc.wantNoOp {
				// Old line item must have EndDate set
				old, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(err)
				s.False(old.EndDate.IsZero(), "old line item EndDate should be set after versioning")

				// Verify the new line item exists with updated quantity
				var newLIID string
				for _, cli := range resp.ChangedResources.LineItems {
					if cli.ChangeAction == dto.ChangedLineItemActionCreated {
						newLIID = cli.ID
					}
				}
				s.Require().NotEmpty(newLIID, "a 'created' line item entry must exist")
				newLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, newLIID)
				s.Require().NoError(err)
				s.True(tc.newQty.Equal(newLI.Quantity), "new line item quantity mismatch")
			}

			// No wallet balance should exist (no credit issued)
			wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
			s.Require().NoError(err)
			var totalBalance decimal.Decimal
			for _, w := range wallets {
				totalBalance = totalBalance.Add(w.Balance)
			}
			s.True(totalBalance.IsZero(), "no wallet credit should be issued for ARREAR items")
		})
	}
}

// ─────────────────────────────────────────────
// Effective-date validation tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_EffectiveDateValidation tests all boundary conditions
// for the effective_date parameter.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_EffectiveDateValidation() {
	type tc struct {
		name      string
		buildDate func(start, end time.Time) time.Time
		wantError bool
	}
	cases := []tc{
		{
			name:      "before_period_start",
			buildDate: func(start, end time.Time) time.Time { return start.Add(-time.Nanosecond) },
			wantError: true,
		},
		{
			name:      "at_period_start",
			buildDate: func(start, end time.Time) time.Time { return start },
			wantError: false,
		},
		{
			name:      "at_period_end",
			buildDate: func(start, end time.Time) time.Time { return end },
			wantError: true,
		},
		{
			name:      "one_ns_before_end",
			buildDate: func(start, end time.Time) time.Time { return end.Add(-time.Nanosecond) },
			wantError: false,
		},
		{
			name:      "future_within_period",
			buildDate: func(start, end time.Time) time.Time { return start.AddDate(0, 0, 10) },
			wantError: false,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			periodStart := s.GetNow()
			periodEnd := periodStart.AddDate(0, 1, 0)

			effectiveDate := tc.buildDate(periodStart, periodEnd)

			cust := s.createCustomer("effdt-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			// Use a simple ARREAR item — cadence doesn't affect date validation
			li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceArrear)

			newQty := decimal.NewFromInt(4)
			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
					LineItems: []dto.LineItemQuantityChange{
						{ID: li.ID, Quantity: newQty, EffectiveDate: &effectiveDate},
					},
				},
			}
			_, err := s.service.Execute(ctx, sub.ID, req)
			if tc.wantError {
				s.Require().Error(err, "expected validation error for %s", tc.name)
			} else {
				s.Require().NoError(err, "expected no error for %s", tc.name)
				// Verify versioning occurred for success cases
				old, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
				s.Require().NoError(getErr)
				s.False(old.EndDate.IsZero(), "old line item must be ended after valid quantity change")
			}
		})
	}
}

// ─────────────────────────────────────────────
// Inheritance tests
// ─────────────────────────────────────────────

// TestExecuteInheritance_Success verifies that a standalone subscription is promoted to parent
// and a child inherited subscription is created for the given external customer.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_Success() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-001")
	child := s.createCustomer("ext-child-001")
	sub := s.createActiveSub(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	}

	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Two changed subscriptions: parent updated + child created
	s.Require().Len(resp.ChangedResources.Subscriptions, 2)

	actions := make(map[string]int)
	for _, cs := range resp.ChangedResources.Subscriptions {
		actions[string(cs.Action)]++
	}
	s.Equal(1, actions[string(dto.ChangedSubscriptionActionUpdated)], "expected one 'updated' entry")
	s.Equal(1, actions[string(dto.ChangedSubscriptionActionCreated)], "expected one 'created' entry")

	// The parent subscription type should now be "parent"
	updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeParent, updatedSub.SubscriptionType)
}

// TestExecuteInheritance_DuplicateChildRejected verifies that adding the same child twice
// returns an error on the second call.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_DuplicateChildRejected() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-002")
	child := s.createCustomer("ext-child-002")
	sub := s.createActiveSub(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	}

	// First call should succeed
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)

	// Second call with same child should fail
	_, err = s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
}

// TestExecuteInheritance_InheritedSubCannotAddChildren verifies that calling Execute on
// an inherited subscription returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_InheritedSubCannotAddChildren() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-003")
	child := s.createCustomer("ext-child-003")
	grandchild := s.createCustomer("ext-grandchild-003")

	parentSub := s.createActiveSub(parent.ID)

	// Create the first inheritance (parent -> child)
	_, err := s.service.Execute(ctx, parentSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	})
	s.Require().NoError(err)

	// Find the inherited (child) subscription
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = child.ID
	subs, err := s.GetStores().SubscriptionRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	childSub := subs[0]
	s.Equal(types.SubscriptionTypeInherited, childSub.SubscriptionType)

	// Attempting to add children to an inherited subscription should fail
	_, err = s.service.Execute(ctx, childSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{grandchild.ExternalID},
		},
	})
	s.Require().Error(err)
}

// ─────────────────────────────────────────────
// Quantity change tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_VersionsLineItem verifies that after Execute, the old line item
// has EndDate set and a new one is created with the updated quantity.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_VersionsLineItem() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-001")
	sub := s.createActiveSub(cust.ID)
	oldQty := decimal.NewFromInt(5)
	li := s.createFixedLineItem(sub.ID, cust.ID, oldQty, types.InvoiceCadenceArrear)

	newQty := decimal.NewFromInt(10)
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: newQty},
			},
		},
	}

	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Response should have exactly 2 changed line items (ended + created)
	s.Require().Len(resp.ChangedResources.LineItems, 2)

	actions := make(map[string]int)
	for _, cli := range resp.ChangedResources.LineItems {
		actions[string(cli.ChangeAction)]++
	}
	s.Equal(1, actions[string(dto.ChangedLineItemActionEnded)], "expected one 'ended' entry")
	s.Equal(1, actions[string(dto.ChangedLineItemActionCreated)], "expected one 'created' entry")

	// Verify old line item has EndDate set in the store
	oldLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.False(oldLI.EndDate.IsZero(), "old line item EndDate should be set after execute")

	// Verify new line item exists with updated quantity
	var newLIID string
	for _, cli := range resp.ChangedResources.LineItems {
		if cli.ChangeAction == dto.ChangedLineItemActionCreated {
			newLIID = cli.ID
		}
	}
	s.Require().NotEmpty(newLIID)
	newLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, newLIID)
	s.Require().NoError(err)
	s.True(newQty.Equal(newLI.Quantity), "new line item should have updated quantity")
}

// TestExecuteQuantityChange_WrongSubscriptionRejected verifies that providing a line item
// from a different subscription returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_WrongSubscriptionRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-002")
	sub1 := s.createActiveSub(cust.ID)
	sub2 := s.createActiveSub(cust.ID)

	// Create a line item belonging to sub2
	li := s.createFixedLineItem(sub2.ID, cust.ID, decimal.NewFromInt(3), types.InvoiceCadenceArrear)

	// Execute against sub1 with sub2's line item
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(7)},
			},
		},
	}

	_, err := s.service.Execute(ctx, sub1.ID, req)
	s.Require().Error(err)
}

// TestPreviewQuantityChange_DoesNotPersist verifies that after Preview, the original line item
// is unchanged (EndDate still zero).
func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange_DoesNotPersist() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-003")
	sub := s.createActiveSub(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(10)},
			},
		},
	}

	resp, err := s.service.Preview(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Original line item should be untouched in store
	origLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(origLI.EndDate.IsZero(), "Preview must not persist changes; EndDate should still be zero")
}

// TestExecuteQuantityChange_InvalidRequestRejected verifies that empty LineItems or zero
// quantity are rejected with validation errors.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_InvalidRequestRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-004")
	sub := s.createActiveSub(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)

	// Empty line items slice
	_, err := s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{},
		},
	})
	s.Require().Error(err, "empty LineItems should be rejected")

	// Zero quantity
	_, err = s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.Zero},
			},
		},
	})
	s.Require().Error(err, "zero quantity should be rejected")
}

// TestExecuteQuantityChange_EffectiveDateOutsideLineItemWindowRejected verifies that
// effective_date must fall within each line item's active window [StartDate, lineEnd),
// where an open-ended line item uses the subscription's current period end.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_EffectiveDateOutsideLineItemWindowRejected() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	cust := s.createCustomer("ext-qty-line-window-exec")
	sub := s.createActiveSub(cust.ID)
	s.setSubPeriod(sub.ID, periodStart, periodEnd)

	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)
	stored, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	lineStart := periodStart.Add(10 * 24 * time.Hour)
	stored.StartDate = lineStart
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, stored))

	effectiveBeforeLine := periodStart.Add(5 * 24 * time.Hour)
	_, err = s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(8), EffectiveDate: &effectiveBeforeLine},
			},
		},
	})
	s.Require().Error(err, "effective date before line item start should be rejected")

	stored2, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	stored2.StartDate = periodStart
	stored2.EndDate = periodStart.Add(15 * 24 * time.Hour)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, stored2))

	effectiveAfterLineEnd := periodStart.Add(20 * 24 * time.Hour)
	_, err = s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(9), EffectiveDate: &effectiveAfterLineEnd},
			},
		},
	})
	s.Require().Error(err, "effective date at or after line item end should be rejected")
}

// TestPreviewQuantityChange_EffectiveDateOutsideLineItemWindowRejected mirrors
// TestExecuteQuantityChange_EffectiveDateOutsideLineItemWindowRejected for Preview.
func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange_EffectiveDateOutsideLineItemWindowRejected() {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	cust := s.createCustomer("ext-qty-line-window-preview")
	sub := s.createActiveSub(cust.ID)
	s.setSubPeriod(sub.ID, periodStart, periodEnd)

	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)
	stored, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	lineStart := periodStart.Add(10 * 24 * time.Hour)
	stored.StartDate = lineStart
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, stored))

	effectiveBeforeLine := periodStart.Add(5 * 24 * time.Hour)
	_, err = s.service.Preview(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(8), EffectiveDate: &effectiveBeforeLine},
			},
		},
	})
	s.Require().Error(err, "effective date before line item start should be rejected (preview)")

	stored2, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	stored2.StartDate = periodStart
	stored2.EndDate = periodStart.Add(15 * 24 * time.Hour)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, stored2))

	effectiveAfterLineEnd := periodStart.Add(20 * 24 * time.Hour)
	_, err = s.service.Preview(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(9), EffectiveDate: &effectiveAfterLineEnd},
			},
		},
	})
	s.Require().Error(err, "effective date at or after line item end should be rejected (preview)")
}

// ─────────────────────────────────────────────
// Multi-line-item tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_MultiLineItem_MixedCadence verifies that in a single Execute
// call with one ADVANCE and one ARREAR line item, the ADVANCE item generates a proration
// invoice while the ARREAR item does not.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_MultiLineItem_MixedCadence() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 10)

	cust := s.createCustomer("multi-mixed-001")
	sub := s.createActiveSub(cust.ID)

	advPrice := s.createFixedPrice(decimal.NewFromInt(50), types.InvoiceCadenceAdvance)
	arrPrice := s.createFixedPrice(decimal.NewFromInt(30), types.InvoiceCadenceArrear)

	advLI := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, advPrice.ID)
	arrLI := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceArrear, arrPrice.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: advLI.ID, Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
				{ID: arrLI.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
			},
		},
	}
	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	s.Len(resp.ChangedResources.LineItems, 4)

	s.Require().Len(resp.ChangedResources.Invoices, 1)
	s.Equal(dto.ChangedInvoiceActionCreated, resp.ChangedResources.Invoices[0].Action)
	s.NotEqual(dto.ChangedInvoiceStatusFromPaymentStatus(types.PaymentStatusFailed), resp.ChangedResources.Invoices[0].Status)
	s.Require().NotNil(resp.ChangedResources.Invoices[0].Invoice)
}

// TestExecuteQuantityChange_MultiLineItem_AtomicRollback verifies that a batch aborts
// when a line item ID cannot be resolved. The invalid change is listed first so the
// service never mutates the valid line item; note that MockPostgresClient.WithTx does not
// simulate SQL rollback, so "valid row then missing row" ordering cannot assert full
// atomicity in this suite (that behavior depends on a real postgres transaction).
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_MultiLineItem_AtomicRollback() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("multi-rollback-001")
	sub := s.createActiveSub(cust.ID)

	p := s.createFixedPrice(decimal.NewFromInt(50), types.InvoiceCadenceAdvance)
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceAdvance, p.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: "nonexistent-id-xyz", Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
				{ID: li.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
			},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err, "should fail when a line item ID in the batch is invalid")

	orig, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(getErr)
	s.True(orig.EndDate.IsZero(), "line item must not be ended when the batch aborts before processing it")

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(listErr)
	s.Empty(invoices, "no invoices should exist when the batch fails before proration")
}

// ─────────────────────────────────────────────
// Preview tests (table-driven)
// ─────────────────────────────────────────────

// TestPreviewQuantityChange verifies that Preview returns the correct placeholder IDs
// and status values, and that no persistent store is mutated.
func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange() {
	type tc struct {
		name              string
		oldQty            decimal.Decimal
		newQty            decimal.Decimal
		cadence           types.InvoiceCadence
		wantLineItems     int
		wantInvoiceID     string
		wantInvoiceStatus string
	}
	cases := []tc{
		{
			name:              "upgrade_advance",
			oldQty:            decimal.NewFromInt(1),
			newQty:            decimal.NewFromInt(3),
			cadence:           types.InvoiceCadenceAdvance,
			wantLineItems:     2,
			wantInvoiceID:     "(preview-invoice)",
			wantInvoiceStatus: "preview",
		},
		{
			name:              "downgrade_advance",
			oldQty:            decimal.NewFromInt(3),
			newQty:            decimal.NewFromInt(1),
			cadence:           types.InvoiceCadenceAdvance,
			wantLineItems:     2,
			wantInvoiceID:     "(preview-wallet-credit)",
			wantInvoiceStatus: "preview",
		},
		{
			name:          "same_qty",
			oldQty:        decimal.NewFromInt(5),
			newQty:        decimal.NewFromInt(5),
			cadence:       types.InvoiceCadenceAdvance,
			wantLineItems: 0,
			wantInvoiceID: "",
		},
		{
			name:          "arrear_increase",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(5),
			cadence:       types.InvoiceCadenceArrear,
			wantLineItems: 2,
			wantInvoiceID: "",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			effectiveDate := s.GetNow().AddDate(0, 0, 10)

			cust := s.createCustomer("prev-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			p := s.createFixedPrice(decimal.NewFromInt(50), tc.cadence)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, tc.cadence, p.ID)

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
					LineItems: []dto.LineItemQuantityChange{
						{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &effectiveDate},
					},
				},
			}
			resp, err := s.service.Preview(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			s.Len(resp.ChangedResources.LineItems, tc.wantLineItems)

			orig, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
			s.Require().NoError(getErr)
			s.True(orig.EndDate.IsZero(),
				"Preview must not mutate the line item EndDate (tc=%s)", tc.name)

			filter := types.NewNoLimitInvoiceFilter()
			filter.SubscriptionID = sub.ID
			invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, filter)
			s.Require().NoError(listErr)
			s.Empty(invoices, "Preview must not persist any invoice (tc=%s)", tc.name)

			wallets, _ := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
			var totalBal decimal.Decimal
			for _, w := range wallets {
				totalBal = totalBal.Add(w.Balance)
			}
			s.True(totalBal.IsZero(), "Preview must not create wallet credits (tc=%s)", tc.name)

			if tc.wantInvoiceID == "" {
				s.Empty(resp.ChangedResources.Invoices,
					"expected no invoice entry in response (tc=%s)", tc.name)
			} else {
				s.Require().Len(resp.ChangedResources.Invoices, 1)
				inv := resp.ChangedResources.Invoices[0]
				s.Equal(tc.wantInvoiceID, inv.ID)
				s.Equal(dto.ChangedInvoiceStatus(tc.wantInvoiceStatus), inv.Status)
				switch tc.wantInvoiceID {
				case "(preview-invoice)":
					s.Require().NotNil(inv.Invoice, "preview upgrade should include synthetic invoice")
					s.Require().NotNil(inv.Invoice.SubscriptionID)
					s.Equal(sub.ID, *inv.Invoice.SubscriptionID)
					s.Equal(cust.ID, inv.Invoice.CustomerID)
				case "(preview-wallet-credit)":
					s.Require().NotNil(inv.WalletTransaction, "preview downgrade should include synthetic wallet tx")
					s.Equal(cust.ID, inv.WalletTransaction.CustomerID)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────
// Proration math regression tests
// ─────────────────────────────────────────────

// TestProrationMath_Upgrade pins upgrade proration to a deterministic value using a
// fixed 31-day billing period (January 2026).
func (s *SubscriptionModificationServiceSuite) TestProrationMath_Upgrade() {
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	effectivePeriodEnd := periodEnd.Add(-time.Second)

	type tc struct {
		name          string
		oldQty        decimal.Decimal
		newQty        decimal.Decimal
		pricePerUnit  decimal.Decimal
		effectiveDate time.Time
	}
	cases := []tc{
		{
			name:          "15_days_remaining",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(3),
			pricePerUnit:  decimal.NewFromInt(50),
			effectiveDate: time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "1_day_remaining",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(2),
			pricePerUnit:  decimal.NewFromInt(100),
			effectiveDate: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "full_period",
			oldQty:        decimal.NewFromInt(1),
			newQty:        decimal.NewFromInt(3),
			pricePerUnit:  decimal.NewFromInt(50),
			effectiveDate: periodStart,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()

			totalSec := decimal.NewFromFloat(effectivePeriodEnd.Sub(periodStart).Seconds())
			remainingSec := decimal.NewFromFloat(effectivePeriodEnd.Sub(tc.effectiveDate).Seconds())
			if remainingSec.LessThan(decimal.Zero) {
				remainingSec = decimal.Zero
			}
			coeff := remainingSec.Div(totalSec)
			qtyDelta := tc.newQty.Sub(tc.oldQty)
			expectedAmt := qtyDelta.Mul(tc.pricePerUnit).Mul(coeff)

			cust := s.createCustomer("math-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			s.setSubPeriod(sub.ID, periodStart, periodEnd)

			p := s.createFixedPrice(tc.pricePerUnit, types.InvoiceCadenceAdvance)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.oldQty, types.InvoiceCadenceAdvance, p.ID)
			storedLI, updErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
			s.Require().NoError(updErr)
			storedLI.StartDate = periodStart
			storedLI.EndDate = time.Time{}
			s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, storedLI))

			req := dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeQuantityChange,
				QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
					LineItems: []dto.LineItemQuantityChange{
						{ID: li.ID, Quantity: tc.newQty, EffectiveDate: &tc.effectiveDate},
					},
				},
			}
			resp, err := s.service.Execute(ctx, sub.ID, req)
			s.Require().NoError(err)
			s.Require().NotNil(resp)
			s.Require().Len(resp.ChangedResources.Invoices, 1)

			inv := resp.ChangedResources.Invoices[0]
			s.Equal(dto.ChangedInvoiceActionCreated, inv.Action)
			s.NotEqual(dto.ChangedInvoiceStatusFromPaymentStatus(types.PaymentStatusFailed), inv.Status)
			s.Require().NotNil(inv.Invoice)

			realInv, fetchErr := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
			s.Require().NoError(fetchErr)

			tolerance := decimal.NewFromFloat(0.01)
			diff := realInv.AmountDue.Sub(expectedAmt).Abs()
			s.True(diff.LessThanOrEqual(tolerance),
				"invoice amount %s should be ≈ %s (diff=%s, tc=%s)",
				realInv.AmountDue.String(), expectedAmt.String(), diff.String(), tc.name)
		})
	}
}

// ─────────────────────────────────────────────
// Guard condition tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_NonFixedPriceRejected verifies that attempting to change
// the quantity of a USAGE-type line item returns a validation error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_NonFixedPriceRejected() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("guard-usage-001")
	sub := s.createActiveSub(cust.ID)

	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: sub.ID,
		CustomerID:     cust.ID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_USAGE,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.GetNow(),
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(3), EffectiveDate: &effectiveDate},
			},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "not a fixed-price item")
}

// TestExecuteQuantityChange_InactiveLineItemRejected verifies that attempting to change
// the quantity of a non-published line item returns a validation error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_InactiveLineItemRejected() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("guard-inactive-001")
	sub := s.createActiveSub(cust.ID)

	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: sub.ID,
		CustomerID:     cust.ID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(2),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.GetNow(),
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	li.Status = types.StatusArchived
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
			LineItems: []dto.LineItemQuantityChange{
				{ID: li.ID, Quantity: decimal.NewFromInt(5), EffectiveDate: &effectiveDate},
			},
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "not active")
}

// ─────────────────────────────────────────────
// Validation tests
// ─────────────────────────────────────────────

// TestExecute_UnknownTypeRejected verifies that an unknown modification type returns a
// validation error.
func (s *SubscriptionModificationServiceSuite) TestExecute_UnknownTypeRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-unknown-001")
	sub := s.createActiveSub(cust.ID)

	_, err := s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyType("unknown"),
	})
	s.Require().Error(err)
}
