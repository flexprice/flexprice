package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// convertInvoiceAmounts converts a draft's amounts to the billing currency at a frozen rate, spreading
// the rounding residual over lines so they sum to the net. Tax is left to the caller.
func convertInvoiceAmounts(inv *invoice.Invoice, resolution *FXRateResolution, convertedAt time.Time) error {
	if inv == nil {
		return ierr.NewError("invoice cannot be nil").WithHint("invoice cannot be nil").Mark(ierr.ErrValidation)
	}
	if resolution == nil {
		return ierr.NewError("fx resolution cannot be nil").WithHint("a resolved rate is required").Mark(ierr.ErrValidation)
	}

	chargeCurrency := inv.Currency
	billingCurrency := resolution.To

	// Defensive no-op: nothing to convert when the currencies match.
	if types.IsMatchingCurrency(chargeCurrency, billingCurrency) {
		return nil
	}

	rate := resolution.Rate

	srcSubtotal := inv.Subtotal
	srcDiscount := inv.TotalDiscount
	srcPrepaid := inv.TotalPrepaidCreditsApplied
	netCharge := srcSubtotal.Sub(srcDiscount).Sub(srcPrepaid)
	netBilling := types.RoundToCurrencyPrecision(netCharge.Mul(rate), billingCurrency)

	// A rate too small for the billing currency's precision would issue a free invoice for a real charge.
	if !netCharge.IsZero() && netBilling.IsZero() {
		return ierr.NewError("fx rate too small for the billing currency precision").
			WithHintf("%s %s at %s rounds to zero %s; fix the rate before finalizing.", netCharge.String(), chargeCurrency, rate.String(), billingCurrency).
			WithReportableDetails(map[string]any{"net": netCharge.String(), "rate": rate.String(), "billing_currency": billingCurrency}).
			Mark(ierr.ErrInvalidOperation)
	}

	convert := func(v decimal.Decimal) decimal.Decimal {
		return types.RoundToCurrencyPrecision(v.Mul(rate), billingCurrency)
	}

	if len(inv.LineItems) > 0 {
		for _, li := range inv.LineItems {
			discount := li.LineItemDiscount.Add(li.InvoiceLevelDiscount)
			li.FxConversion = &types.FxConversion{
				ChargeCurrency: li.Currency,
				Source: types.FxConversionSource{
					Subtotal:                   li.Amount,
					TotalDiscount:              discount,
					TotalPrepaidCreditsApplied: li.PrepaidCreditsApplied,
					Net:                        li.Amount.Sub(discount).Sub(li.PrepaidCreditsApplied),
				},
			}
			li.Amount = convert(li.Amount)
			li.LineItemDiscount = convert(li.LineItemDiscount)
			li.InvoiceLevelDiscount = convert(li.InvoiceLevelDiscount)
			li.PrepaidCreditsApplied = convert(li.PrepaidCreditsApplied)
			li.Currency = billingCurrency
		}

		netFromLines := decimal.Zero
		for _, li := range inv.LineItems {
			netFromLines = netFromLines.Add(li.Amount).
				Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)
		}

		allocateResidual(inv.LineItems, netBilling.Sub(netFromLines))

		inv.Subtotal = decimal.Zero
		inv.TotalDiscount = decimal.Zero
		inv.TotalPrepaidCreditsApplied = decimal.Zero
		for _, li := range inv.LineItems {
			inv.Subtotal = inv.Subtotal.Add(li.Amount)
			inv.TotalDiscount = inv.TotalDiscount.Add(li.LineItemDiscount).Add(li.InvoiceLevelDiscount)
			inv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied.Add(li.PrepaidCreditsApplied)
		}
	} else {
		inv.Subtotal = convert(srcSubtotal)
		inv.TotalDiscount = convert(srcDiscount)
		inv.TotalPrepaidCreditsApplied = convert(srcPrepaid)
	}

	// Pre-tax total; tax is added on the converted amounts in the finalize step that follows.
	inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalPrepaidCreditsApplied)
	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total // nothing is paid before conversion
	inv.Currency = billingCurrency

	inv.FxConversion = &types.FxConversion{
		ChargeCurrency:  chargeCurrency,
		BillingCurrency: billingCurrency,
		Rate:            rate,
		RateID:          resolution.RateID,
		Scope:           resolution.Scope,
		ConvertedAt:     convertedAt,
		Source: types.FxConversionSource{
			Subtotal:                   srcSubtotal,
			TotalDiscount:              srcDiscount,
			TotalPrepaidCreditsApplied: srcPrepaid,
			Net:                        netCharge,
		},
	}

	return nil
}

// allocateResidual spreads the rounding residual over lines, largest positive first. A line takes all
// of it when that moves its net away from zero, otherwise only enough to bring its net to zero.
func allocateResidual(lines []*invoice.InvoiceLineItem, residual decimal.Decimal) {
	if residual.IsZero() || len(lines) == 0 {
		return
	}

	ordered := slices.Clone(lines)
	slices.SortStableFunc(ordered, func(a, b *invoice.InvoiceLineItem) int {
		switch {
		case betterResidualCandidate(a, b):
			return -1
		case betterResidualCandidate(b, a):
			return 1
		}
		return 0
	})

	remaining := residual
	for _, li := range ordered {
		net := li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)
		take := remaining
		if net.Sign() != remaining.Sign() && net.Abs().LessThan(remaining.Abs()) {
			take = net.Neg()
		}
		li.Amount = li.Amount.Add(take)
		remaining = remaining.Sub(take)
		if remaining.IsZero() {
			return
		}
	}

	// Every line is at zero net; keep the invoice balanced on the preferred line.
	ordered[0].Amount = ordered[0].Amount.Add(remaining)
}

// betterResidualCandidate orders lines for the residual: positive before non-positive, then larger
// absolute amount, then lower id.
func betterResidualCandidate(candidate, best *invoice.InvoiceLineItem) bool {
	cPos := candidate.Amount.IsPositive()
	bPos := best.Amount.IsPositive()

	// A positive line always beats a non-positive one.
	if cPos != bPos {
		return cPos
	}

	if cPos {
		// Both positive: larger amount wins, lower id breaks ties.
		if candidate.Amount.Equal(best.Amount) {
			return candidate.ID < best.ID
		}
		return candidate.Amount.GreaterThan(best.Amount)
	}

	// Neither positive: larger absolute value wins, lower id breaks ties.
	cAbs := candidate.Amount.Abs()
	bAbs := best.Amount.Abs()
	if cAbs.Equal(bAbs) {
		return candidate.ID < best.ID
	}
	return cAbs.GreaterThan(bAbs)
}

// validateInvoiceBillingCurrency rejects a pre-paid one-off for a customer billed in another
// currency: the amount is only known in that currency after finalize.
func (s *invoiceService) validateInvoiceBillingCurrency(ctx context.Context, req dto.CreateInvoiceRequest) error {
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

// conversionTarget returns the billing currency an unconverted invoice must be converted to, or "".
func (s *invoiceService) conversionTarget(ctx context.Context, inv *invoice.Invoice) (string, error) {
	if inv.FxConversion != nil || inv.CustomCurrency != nil {
		return "", nil
	}
	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return "", err
	}
	billing := lo.FromPtr(cust.BillingCurrency)
	if billing == "" || types.IsMatchingCurrency(billing, inv.Currency) {
		return "", nil
	}
	return billing, nil
}

// convertToBillingCurrency converts an invoice to its customer's billing currency once and re-taxes it.
// No-op when nothing needs converting or the invoice is already paid; a missing rate errors.
func (s *invoiceService) convertToBillingCurrency(ctx context.Context, inv *invoice.Invoice) error {
	if inv.FxConversion != nil || inv.CustomCurrency != nil {
		return nil
	}
	if !inv.AmountPaid.IsZero() {
		s.Logger.Info(ctx, "skipping fx conversion: invoice already carries a payment",
			"invoice_id", inv.ID, "amount_paid", inv.AmountPaid.String(), "currency", inv.Currency)
		return nil
	}

	billing, err := s.conversionTarget(ctx, inv)
	if err != nil || billing == "" {
		return err
	}

	fxSvc := NewFXRateService(s.ServiceParams)
	resolution, err := fxSvc.ResolveRate(ctx, ResolveFXRateRequest{
		From:           inv.Currency,
		To:             billing,
		CustomerID:     inv.CustomerID,
		SubscriptionID: lo.FromPtr(inv.SubscriptionID),
	})
	if err != nil {
		s.Logger.Error(ctx, "cannot convert invoice: no fx rate for the pair",
			"error", err, "invoice_id", inv.ID, "from", inv.Currency, "to", billing)
		// A fresh error, not a wrap: the resolver's not-found would otherwise also match and make
		// the HTTP status non-deterministic. Invalid-operation keeps Temporal from retrying.
		return ierr.NewErrorf("no exchange rate configured for %s to %s", inv.Currency, billing).
			WithHintf("No exchange rate configured for %s to %s; set a rate for the pair, then retry.", inv.Currency, billing).
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

	if err := convertInvoiceAmounts(inv, resolution, time.Now().UTC()); err != nil {
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

	return s.retaxConvertedInvoice(ctx, inv)
}

// retaxConvertedInvoice re-taxes a converted invoice: subscription invoices from their
// tax associations, others from the rates recorded on their tax_applied rows.
func (s *invoiceService) retaxConvertedInvoice(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceType == types.InvoiceTypeSubscription && inv.SubscriptionID != nil {
		_, err := s.RecalculateTaxesOnInvoice(ctx, inv)
		return err
	}

	taxService := NewTaxService(s.ServiceParams)
	taxRates, err := taxService.PrepareTaxRatesFromApplied(ctx, inv)
	if err != nil {
		return err
	}

	result, err := taxService.ApplyTaxesOnInvoice(ctx, inv, taxRates)
	if err != nil {
		return err
	}
	applyTaxResultToInvoice(inv, result)
	return s.InvoiceRepo.Update(ctx, inv)
}
