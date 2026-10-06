package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// rejectPrepaidCrossCurrencyOneOff blocks creating a one-off invoice pre-paid (payment_status or
// amount_paid set) for a customer billed in a different currency: the invoice is issued in the
// billing currency only at finalize, so a payment recorded at create would settle the wrong amount.
// Today those fields are silently dropped; this makes it a loud error instead (§8.4).
func (s *invoiceService) rejectPrepaidCrossCurrencyOneOff(ctx context.Context, req dto.CreateInvoiceRequest) error {
	hasPayment := (req.AmountPaid != nil && !req.AmountPaid.IsZero()) ||
		(req.PaymentStatus != nil && *req.PaymentStatus != types.PaymentStatusPending)
	if !hasPayment {
		return nil
	}

	cust, err := s.CustomerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return err
	}
	if cust.BillingCurrency == nil || *cust.BillingCurrency == "" {
		return nil
	}
	if types.IsMatchingCurrency(*cust.BillingCurrency, req.Currency) {
		return nil
	}

	return ierr.NewError("cannot create a pre-paid invoice for a cross-currency customer").
		WithHintf("This customer is billed in %s; create the invoice without payment, then pay it once it is finalized in %s.",
			strings.ToUpper(*cust.BillingCurrency), strings.ToUpper(*cust.BillingCurrency)).
		WithReportableDetails(map[string]any{
			"request_currency": req.Currency,
			"billing_currency": *cust.BillingCurrency,
		}).
		Mark(ierr.ErrValidation)
}

// convertAndRetaxAtFinalize runs the shared conversion step (§5.2 steps 4–6) for any invoice whose
// customer has a billing currency that differs from the charge currency. It converts the invoice
// once at a frozen rate and recomputes tax on the converted amounts. It is a no-op for invoices with
// no billing currency, a matching currency, an already-set fx_conversion, a custom currency (handled
// in a later PR), or a non-zero amount_paid (pay-first checkout, handled in a later PR). A missing
// rate leaves the invoice DRAFT via an invalid-operation error so the finalize is not auto-retried.
func (s *invoiceService) convertAndRetaxAtFinalize(ctx context.Context, inv *invoice.Invoice) error {
	if inv.FxConversion != nil {
		return nil
	}
	if inv.CustomCurrency != nil {
		return nil
	}
	if !inv.AmountPaid.IsZero() {
		s.Logger.Info(ctx, "skipping fx conversion: invoice already carries a payment",
			"invoice_id", inv.ID, "amount_paid", inv.AmountPaid.String(), "currency", inv.Currency)
		return nil
	}

	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return err
	}
	if cust.BillingCurrency == nil || *cust.BillingCurrency == "" {
		return nil
	}
	billing := *cust.BillingCurrency
	if types.IsMatchingCurrency(billing, inv.Currency) {
		return nil
	}

	fxSvc := NewFXRateService(s.ServiceParams)
	resolution, err := fxSvc.ResolveRate(ctx, ResolveFXRateRequest{
		From:           inv.Currency,
		To:             billing,
		CustomerID:     inv.CustomerID,
		SubscriptionID: lo.FromPtr(inv.SubscriptionID),
	})
	if err != nil {
		s.Logger.Error(ctx, "cannot finalize invoice: no fx rate for conversion",
			"error", err, "invoice_id", inv.ID, "from", inv.Currency, "to", billing)
		// A fresh error, not a wrap: the resolver's not-found would otherwise also match and make
		// the HTTP status non-deterministic. Invalid-operation keeps Temporal from retrying.
		return ierr.NewErrorf("no exchange rate configured for %s to %s", inv.Currency, billing).
			WithHintf("No exchange rate configured for %s to %s; set a rate, then finalize.", inv.Currency, billing).
			WithReportableDetails(map[string]any{"from": inv.Currency, "to": billing}).
			Mark(ierr.ErrInvalidOperation)
	}

	if inv.LineItems == nil {
		lineItems, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
		if err != nil {
			return err
		}
		inv.LineItems = lineItems
	}

	if err := ConvertInvoice(inv, resolution, time.Now().UTC()); err != nil {
		return err
	}

	for _, item := range inv.LineItems {
		if err := s.InvoiceLineItemRepo.Update(ctx, item); err != nil {
			return err
		}
	}
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	return s.recomputeTaxOnConvertedInvoice(ctx, inv)
}

// recomputeTaxOnConvertedInvoice recomputes tax on a just-converted invoice's billing-currency
// amounts. Subscription invoices re-derive their rates from the subscription's associations; other
// invoice types re-apply the rates already recorded against the invoice (one-off invoices tax at
// compute, not at finalize, so their rates only exist as applied records by now).
func (s *invoiceService) recomputeTaxOnConvertedInvoice(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceType == types.InvoiceTypeSubscription && inv.SubscriptionID != nil {
		_, err := s.RecalculateTaxesOnInvoice(ctx, inv)
		return err
	}

	taxService := NewTaxService(s.ServiceParams)
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	applied, err := taxService.ListTaxApplied(ctx, filter)
	if err != nil {
		return err
	}

	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return err
	}

	// No tax rows: zero the tax and recompute totals in the billing currency.
	if len(applied.Items) == 0 {
		applyTaxResultToInvoice(inv, &TaxCalculationResult{
			TotalTaxAmount:    decimal.Zero,
			TaxAppliedRecords: []*dto.TaxAppliedResponse{},
		})
		return s.InvoiceRepo.Update(ctx, inv)
	}

	behaviorByRateID := make(map[string]types.TaxBehavior, len(applied.Items))
	rateIDs := make([]string, 0, len(applied.Items))
	for _, a := range applied.Items {
		if _, seen := behaviorByRateID[a.TaxRateID]; seen {
			continue
		}
		behaviorByRateID[a.TaxRateID] = a.TaxBehavior
		rateIDs = append(rateIDs, a.TaxRateID)
	}

	rateFilter := types.NewNoLimitTaxRateFilter()
	rateFilter.TaxRateIDs = rateIDs
	rates, err := taxService.ListTaxRates(ctx, rateFilter)
	if err != nil {
		return err
	}
	resolved := make([]*dto.TaxRateWithBehavior, 0, len(rates.Items))
	for _, r := range rates.Items {
		resolved = append(resolved, &dto.TaxRateWithBehavior{TaxRateResponse: r, TaxBehavior: behaviorByRateID[r.ID]})
	}

	result, err := taxService.ApplyTaxesOnInvoice(ctx, inv, dto.NewInvoiceTaxRates(resolved, cust))
	if err != nil {
		return err
	}
	applyTaxResultToInvoice(inv, result)
	return s.InvoiceRepo.Update(ctx, inv)
}
