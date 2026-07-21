package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *InvoiceServiceSuite) createPercentageTaxRate(pct int64) *taxrate.TaxRate {
	ctx := s.GetContext()
	pctDec := decimal.NewFromInt(pct)
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "Checkout Draft Tax",
		Code:            "checkout_draft_tax_" + types.GenerateUUIDWithPrefix("code"),
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pctDec,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
	return tr
}

func (s *InvoiceServiceSuite) createSubscriptionTaxAssociation(taxRateID, subscriptionID string) *taxassociation.TaxAssociation {
	ctx := s.GetContext()
	assoc := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    types.TaxRateEntityTypeSubscription,
		EntityID:      subscriptionID,
		Priority:      100,
		AutoApply:     true,
		Currency:      "usd",
		StartDate:     time.Now().UTC().Add(-24 * time.Hour),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))
	return assoc
}

func (s *InvoiceServiceSuite) buildDraftSubscriptionInvoice(id string, subtotal decimal.Decimal, withLineItem bool) *invoice.Invoice {
	ctx := s.GetContext()
	periodStart := s.testData.subscription.CurrentPeriodStart
	periodEnd := s.testData.subscription.CurrentPeriodEnd
	bp := string(types.BILLING_PERIOD_MONTHLY)
	now := time.Now().UTC()

	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        subtotal,
		Total:           subtotal,
		AmountDue:       subtotal,
		AmountPaid:      decimal.Zero,
		AmountRemaining: subtotal,
		TotalTax:        decimal.Zero,
		BillingPeriod:   &bp,
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		LastComputedAt:  &now,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	if withLineItem {
		inv.LineItems = []*invoice.InvoiceLineItem{
			{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:      id,
				CustomerID:     s.testData.customer.ID,
				SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
				PriceID:        lo.ToPtr(s.testData.prices.storage.ID),
				PriceType:      lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				DisplayName:    lo.ToPtr("Fixed Plan Fee"),
				Amount:         subtotal,
				Quantity:       decimal.NewFromInt(1),
				Currency:       "usd",
				PeriodStart:    &periodStart,
				PeriodEnd:      &periodEnd,
				BaseModel:      types.GetDefaultBaseModel(ctx),
			},
		}
	}

	s.Require().NoError(s.invoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_WithSubscriptionTax() {
	ctx := s.GetContext()
	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_apply", subtotal, false)

	tr := s.createPercentageTaxRate(10)
	s.createSubscriptionTaxAssociation(tr.ID, s.testData.subscription.ID)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)

	expectedTax := decimal.NewFromInt(10)
	expectedDue := subtotal.Add(expectedTax)
	s.True(expectedTax.Equal(got.TotalTax), "expected total_tax %s, got %s", expectedTax, got.TotalTax)
	s.True(expectedDue.Equal(got.AmountDue), "expected amount_due %s, got %s", expectedDue, got.AmountDue)
	s.True(expectedDue.Equal(got.Total), "expected total %s, got %s", expectedDue, got.Total)
}

func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_NoTaxUnchanged() {
	ctx := s.GetContext()
	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_none", subtotal, false)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)

	s.True(got.TotalTax.IsZero(), "expected zero total_tax, got %s", got.TotalTax)
	s.True(subtotal.Equal(got.AmountDue), "expected amount_due unchanged at %s, got %s", subtotal, got.AmountDue)
	s.True(subtotal.Equal(got.Total), "expected total unchanged at %s, got %s", subtotal, got.Total)
}

func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_FinalizeNoDoubleTax() {
	ctx := s.GetContext()
	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_finalize", subtotal, true)

	tr := s.createPercentageTaxRate(10)
	s.createSubscriptionTaxAssociation(tr.ID, s.testData.subscription.ID)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)

	afterApply, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	taxAfterApply := afterApply.TotalTax
	s.True(decimal.NewFromInt(10).Equal(taxAfterApply), "expected tax 10 after apply, got %s", taxAfterApply)

	err = s.service.FinalizeInvoice(ctx, inv.ID)
	s.Require().NoError(err)

	afterFinalize, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, afterFinalize.InvoiceStatus)
	s.True(taxAfterApply.Equal(afterFinalize.TotalTax),
		"finalize must not double-tax: apply=%s finalize=%s", taxAfterApply, afterFinalize.TotalTax)
	s.True(subtotal.Add(taxAfterApply).Equal(afterFinalize.AmountDue),
		"expected amount_due %s after finalize, got %s", subtotal.Add(taxAfterApply), afterFinalize.AmountDue)
}
