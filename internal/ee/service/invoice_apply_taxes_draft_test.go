package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
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
	_, err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
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
	_, err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
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
	updated, err := s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)
	taxAfterApply := updated.TotalTax
	s.True(decimal.NewFromInt(10).Equal(taxAfterApply), "expected tax 10 after apply, got %s", taxAfterApply)

	err = s.service.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{})
	s.Require().NoError(err)

	afterFinalize, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, afterFinalize.InvoiceStatus)
	s.True(taxAfterApply.Equal(afterFinalize.TotalTax),
		"finalize must not double-tax: apply=%s finalize=%s", taxAfterApply, afterFinalize.TotalTax)
	s.True(subtotal.Add(taxAfterApply).Equal(afterFinalize.AmountDue),
		"expected amount_due %s after finalize, got %s", subtotal.Add(taxAfterApply), afterFinalize.AmountDue)
}

func (s *InvoiceServiceSuite) markCustomerExempt() {
	ctx := s.GetContext()
	s.testData.customer.TaxTreatment = types.TaxTreatmentExempt
	s.Require().NoError(s.GetStores().CustomerRepo.Update(ctx, s.testData.customer))
}

func (s *InvoiceServiceSuite) selectStripeTaxEngine() {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyTaxConfig,
		Value:         map[string]interface{}{"enabled": true, "provider": string(types.TaxProviderStripe)},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *InvoiceServiceSuite) invoiceTaxApplied(invoiceID string) int {
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = invoiceID
	records, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return len(records)
}

// An exempt customer is billed the subtotal as priced. The subscription's tax association is
// never resolved, and the rows written while the customer was taxable are archived.
func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_ExemptCustomerIsNotTaxed() {
	ctx := s.GetContext()
	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_exempt", subtotal, false)

	tr := s.createPercentageTaxRate(10)
	s.createSubscriptionTaxAssociation(tr.ID, s.testData.subscription.ID)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	taxed, err := s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(taxed.TotalTax), "taxable first, got %s", taxed.TotalTax)
	s.Equal(1, s.invoiceTaxApplied(inv.ID))

	s.markCustomerExempt()

	fresh, err = s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	_, err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err)

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.True(got.TotalTax.IsZero(), "expected zero total_tax, got %s", got.TotalTax)
	s.True(subtotal.Equal(got.Total), "expected total %s, got %s", subtotal, got.Total)
	s.True(subtotal.Equal(got.AmountDue), "expected amount_due %s, got %s", subtotal, got.AmountDue)
	s.Require().NotNil(got.TaxExemptionReasonCode)
	s.Equal(types.TaxExemptionReasonCustomerExempt, *got.TaxExemptionReasonCode)
	s.Equal(0, s.invoiceTaxApplied(inv.ID), "earlier tax rows must be archived")
}

// With Stripe selected as the tax engine and no Stripe connection, an exempt customer's invoice
// still computes and finalizes, because the engine is never resolved.
func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_ExemptCustomerSkipsExternalEngine() {
	ctx := s.GetContext()
	s.selectStripeTaxEngine()
	s.markCustomerExempt()

	rec := &recordingConnectionRepo{Repository: s.GetStores().ConnectionRepo}
	s.service.(*invoiceService).ConnectionRepo = rec

	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_exempt_stripe", subtotal, true)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	_, err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().NoError(err, "an exempt customer must not resolve the external engine")

	s.Require().NoError(s.service.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{}))

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, got.InvoiceStatus)
	s.True(got.TotalTax.IsZero(), "expected zero total_tax, got %s", got.TotalTax)
	s.True(subtotal.Equal(got.Total), "expected total %s, got %s", subtotal, got.Total)
	s.Require().NotNil(got.TaxExemptionReasonCode)
	s.Equal(types.TaxExemptionReasonCustomerExempt, *got.TaxExemptionReasonCode)
	s.Equal(0, s.invoiceTaxApplied(inv.ID))
	s.Equal(1, lo.Count(rec.getByProviderCalls, types.SecretProviderStripe),
		"only the hosted invoice check at finalize may look up Stripe; the tax engine must not for an exempt customer")
}

// Control for the test above: the same setup for a taxable customer does reach Stripe and fails.
func (s *InvoiceServiceSuite) TestRecalculateTaxesOnInvoice_TaxableCustomerReachesExternalEngine() {
	ctx := s.GetContext()
	s.selectStripeTaxEngine()
	s.Require().NoError(s.GetStores().ConnectionRepo.Create(ctx, &connection.Connection{
		ID:            types.GenerateUUIDWithPrefix("conn"),
		Name:          "stripe",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	inv := s.buildDraftSubscriptionInvoice("inv_draft_tax_taxable_stripe", decimal.NewFromInt(100), true)

	fresh, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	_, err = s.service.RecalculateTaxesOnInvoice(ctx, fresh)
	s.Require().Error(err, "a taxable customer is sent to Stripe, which has no credentials here")
}

// Subscription invoices finalize through ProcessDraftInvoice. With Stripe selected and no Stripe
// connection, an exempt customer's invoice still finalizes, and tax adds no Stripe lookups on top
// of the ones payment collection makes on its own.
func (s *InvoiceServiceSuite) TestProcessDraftInvoice_ExemptCustomerSkipsExternalEngine() {
	ctx := s.GetContext()
	s.markCustomerExempt()

	rec := &recordingConnectionRepo{Repository: s.GetStores().ConnectionRepo}
	s.service.(*invoiceService).ConnectionRepo = rec

	baseline := s.buildDraftSubscriptionInvoice("inv_draft_process_exempt_native", decimal.NewFromInt(100), true)
	s.Require().NoError(s.service.ProcessDraftInvoice(ctx, baseline.ID, nil, s.testData.subscription, types.InvoiceFlowManual))
	paymentLookups := len(rec.getByProviderCalls)

	s.selectStripeTaxEngine()
	rec.getByProviderCalls = nil

	subtotal := decimal.NewFromInt(100)
	inv := s.buildDraftSubscriptionInvoice("inv_draft_process_exempt_stripe", subtotal, true)

	err := s.service.ProcessDraftInvoice(ctx, inv.ID, nil, s.testData.subscription, types.InvoiceFlowManual)
	s.Require().NoError(err, "an exempt customer must not resolve the external engine")

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, got.InvoiceStatus)
	s.True(got.TotalTax.IsZero(), "expected zero total_tax, got %s", got.TotalTax)
	s.True(subtotal.Equal(got.Total), "expected total %s, got %s", subtotal, got.Total)
	s.Require().NotNil(got.TaxExemptionReasonCode)
	s.Equal(types.TaxExemptionReasonCustomerExempt, *got.TaxExemptionReasonCode)
	s.Len(rec.getByProviderCalls, paymentLookups,
		"selecting Stripe as the tax engine must add no connection lookups for an exempt customer")
}

// sealedInvoiceCustomerRepo fails customer reads once the watched invoice is finalized, so only
// reads made after sealing fail.
type sealedInvoiceCustomerRepo struct {
	customer.Repository
	invoices  invoice.Repository
	invoiceID string
}

func (r *sealedInvoiceCustomerRepo) Get(ctx context.Context, id string) (*customer.Customer, error) {
	inv, err := r.invoices.Get(ctx, r.invoiceID)
	if err == nil && inv.InvoiceStatus == types.InvoiceStatusFinalized {
		return nil, ierr.NewError("customer read failed").Mark(ierr.ErrDatabase)
	}
	return r.Repository.Get(ctx, id)
}

// The customer is read after the invoice is sealed and only to decide tax filing, so a failed
// read is logged and the finalize still succeeds.
func (s *InvoiceServiceSuite) TestFinalizeInvoice_CustomerReadFailureDoesNotFailFinalize() {
	ctx := s.GetContext()
	inv := s.buildDraftSubscriptionInvoice("inv_draft_oneoff_customer_read_fails", decimal.NewFromInt(100), true)
	inv.InvoiceType = types.InvoiceTypeOneOff
	inv.SubscriptionID = nil
	s.Require().NoError(s.invoiceRepo.Update(ctx, inv))

	s.service.(*invoiceService).CustomerRepo = &sealedInvoiceCustomerRepo{
		Repository: s.GetStores().CustomerRepo,
		invoices:   s.invoiceRepo,
		invoiceID:  inv.ID,
	}

	s.Require().NoError(s.service.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{}))

	got, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, got.InvoiceStatus)
}
