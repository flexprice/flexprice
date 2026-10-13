package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/shopspring/decimal"
)

// Credits applied to a subscription draft before finalization (settled at a credit's expiry)
// must survive recompute: totals stay net of them, and the invoice is never marked SKIPPED.

// computedCycleDraft creates a subscription with one fixed arrear charge of amount (or none when
// amount is zero) and returns its computed cycle draft for the current period.
func (s *InvoiceServiceSuite) computedCycleDraft(id, currency string, amount decimal.Decimal) *invoice.Invoice {
	ctx := s.GetContext()
	now := s.testData.now

	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.AddDate(0, 0, -20),
		CurrentPeriodStart: now.AddDate(0, 0, -20),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		BillingAnchor:      now.AddDate(0, 0, -20),
		Currency:           currency,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	var lineItems []*subscription.SubscriptionLineItem
	if amount.IsPositive() {
		p := &price.Price{
			ID:                 "price_" + id,
			Amount:             amount,
			Currency:           currency,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           s.testData.plan.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  sub.ID,
			CustomerID:      sub.CustomerID,
			EntityID:        s.testData.plan.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			DisplayName:     "Platform fee",
			Quantity:        decimal.NewFromInt(1),
			Currency:        currency,
			BillingPeriod:   sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceArrear,
			StartDate:       sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))

	draft, err := s.service.CreateDraftInvoiceForSubscription(ctx, dto.CreateSubscriptionDraftInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    sub.CurrentPeriodStart,
		PeriodEnd:      sub.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.Require().NoError(err)
	inv, _, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	return inv
}

// applyToDraft sets credits applied to a draft the way ApplyExpiringCreditToInvoice leaves it.
func (s *InvoiceServiceSuite) applyToDraft(inv *invoice.Invoice, amount decimal.Decimal) {
	inv.RestoreFromDenomination()
	inv.TotalPrepaidCreditsApplied = amount
	inv.Total = decimal.Max(decimal.Zero, inv.Subtotal.Sub(inv.TotalDiscount).Sub(amount))
	inv.AmountDue = inv.Total
	inv.CaptureCustomCurrencyDenomination()
	inv.ProjectCustomCurrency()
	inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)
	s.NoError(s.invoiceRepo.Update(s.GetContext(), inv))
}

func (s *InvoiceServiceSuite) TestComputeInvoice_KeepsCreditsAppliedToDraft() {
	ctx := s.GetContext()
	s.invoiceRepo.Clear()

	inv := s.computedCycleDraft("sub_prepaid_keep", "usd", decimal.NewFromInt(100))
	s.Require().True(decimal.NewFromInt(100).Equal(inv.Subtotal), "subtotal %s", inv.Subtotal)
	s.applyToDraft(inv, decimal.NewFromInt(20))

	recomputed, skipped, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)
	s.True(decimal.NewFromInt(20).Equal(recomputed.TotalPrepaidCreditsApplied), "applied %s", recomputed.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(80).Equal(recomputed.Total), "total %s", recomputed.Total)
	s.True(decimal.NewFromInt(80).Equal(recomputed.AmountDue))
	s.True(decimal.NewFromInt(80).Equal(recomputed.AmountRemaining))
}

func (s *InvoiceServiceSuite) TestComputeInvoice_NoCreditsAppliedUnchanged() {
	s.invoiceRepo.Clear()

	inv := s.computedCycleDraft("sub_prepaid_none", "usd", decimal.NewFromInt(100))
	s.True(inv.TotalPrepaidCreditsApplied.IsZero())
	s.True(decimal.NewFromInt(100).Equal(inv.Total))
	s.True(decimal.NewFromInt(100).Equal(inv.AmountRemaining))
}

// A draft with credits applied is kept as a draft even when it has no charges, so the credits
// aren't stranded on a SKIPPED invoice that never finalizes.
func (s *InvoiceServiceSuite) TestComputeInvoice_NeverSkipsDraftWithCreditsApplied() {
	ctx := s.GetContext()
	s.invoiceRepo.Clear()

	inv := s.computedCycleDraft("sub_prepaid_zero", "usd", decimal.Zero)
	s.Equal(types.InvoiceStatusSkipped, inv.InvoiceStatus, "no charges and no credits: skipped as today")

	inv.InvoiceStatus = types.InvoiceStatusDraft
	s.applyToDraft(inv, decimal.NewFromInt(20))

	recomputed, skipped, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)
	s.Equal(types.InvoiceStatusDraft, recomputed.InvoiceStatus)
	s.True(recomputed.Total.IsZero())
}

// Custom currency: recompute reads the applied credits from the denomination, not the fiat
// column. 1 CRD = 0.5 USD.
func (s *InvoiceServiceSuite) TestComputeInvoice_KeepsCreditsAppliedToCustomCurrencyDraft() {
	ctx := s.GetContext()
	s.invoiceRepo.Clear()

	cfg := types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			"crd": {Name: "Credits", Symbol: "CR", FiatConversionFactors: map[string]decimal.Decimal{"usd": decimal.NewFromFloat(0.5)}},
		},
		DefaultFiatCurrency: "usd",
	}
	s.NoError(cfg.Validate())
	value, err := utils.ToMap(cfg)
	s.NoError(err)
	s.NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyCustomCurrencyConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	inv := s.computedCycleDraft("sub_prepaid_cred", "crd", decimal.NewFromInt(100))
	s.Require().NotNil(inv.CustomCurrency)
	s.Require().True(decimal.NewFromInt(100).Equal(inv.CustomCurrency.Subtotal), "denomination subtotal %s", inv.CustomCurrency.Subtotal)
	s.applyToDraft(inv, decimal.NewFromInt(20))

	recomputed, _, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
	s.Require().NoError(err)
	s.Require().NotNil(recomputed.CustomCurrency)
	s.True(decimal.NewFromInt(20).Equal(recomputed.CustomCurrency.TotalPrepaidCreditsApplied),
		"denomination applied %s", recomputed.CustomCurrency.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(80).Equal(recomputed.CustomCurrency.Total))
	s.True(decimal.NewFromInt(10).Equal(recomputed.TotalPrepaidCreditsApplied), "fiat applied %s", recomputed.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(40).Equal(recomputed.Total), "fiat total %s", recomputed.Total)
	s.True(decimal.NewFromInt(40).Equal(recomputed.AmountRemaining))
}
