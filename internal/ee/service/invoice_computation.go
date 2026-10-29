package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// applyInvoiceComputation persists computed amounts inside the caller's locked invoice transaction.
func (s *invoiceService) applyInvoiceComputation(ctx context.Context, inv *invoice.Invoice, req *dto.InvoiceComputeRequest) (bool, error) {
	// The math below runs in the denomination; credits applied at expiry must survive recompute.
	inv.RestoreFromDenomination()
	creditsAppliedToDraft := inv.TotalPrepaidCreditsApplied

	// Populate invoice from the computed request (uniform for all invoice types)
	if req != nil {
		var err error
		req, err = preserveBillingScheduleRequest(ctx, inv, req)
		if err != nil {
			return false, err
		}
		reconciledItems, err := s.reconcileLineItems(ctx, inv, req.LineItems)
		if err != nil {
			return false, err
		}

		inv.Subtotal = req.Subtotal
		inv.Total = req.Total
		inv.AmountDue = req.AmountDue
		inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)
		inv.Description = req.Description
		inv.DueDate = req.DueDate
		inv.LineItems = reconciledItems
	}

	isTrialStart := types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionTrialStart
	if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() && !lo.SomeBy(inv.LineItems, func(item *invoice.InvoiceLineItem) bool {
		return item != nil && !item.Amount.IsZero()
	}) && !isTrialStart && !creditsAppliedToDraft.IsPositive() {
		now := time.Now().UTC()
		inv.LastComputedAt = &now
		inv.InvoiceStatus = types.InvoiceStatusSkipped
		if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
			return false, err
		}
		return true, nil
	}

	// Apply coupons/discounts. For one-off and credit invoices, also apply
	// credits and taxes here so a computed draft carries a total. Finalization
	// recalculates the tax again. For subscription invoices, credits and taxes
	// are deferred to the finalization step so wallet debits only happen when
	// the invoice is actually sealed.
	applyTaxes := false
	if req != nil {
		if inv.InvoiceType == types.InvoiceTypeOneOff || inv.InvoiceType == types.InvoiceTypeCredit {
			// One-off / credit: apply coupons + credits now, taxes after the conversion
			if err := s.applyCreditsAndCouponsToInvoice(ctx, inv, *req); err != nil {
				return false, err
			}
			applyTaxes = true
		} else {
			// Subscription: coupons only — credits and taxes deferred to finalization
			if err := s.applyCouponsToInvoice(ctx, inv, *req); err != nil {
				return false, err
			}
			inv.TotalPrepaidCreditsApplied = creditsAppliedToDraft
			if creditsAppliedToDraft.IsPositive() {
				inv.Total = decimal.Max(inv.Subtotal.Sub(inv.TotalDiscount).Sub(creditsAppliedToDraft), decimal.Zero)
				inv.AmountDue = inv.Total
			}
		}
	}

	// The pipeline above ran in the subscription's currency. Snapshot it as the
	// denomination, then write the fiat columns back from it.
	inv.CaptureCustomCurrencyDenomination()
	inv.ProjectCustomCurrency()

	// Tax runs after the conversion: it writes tax_applied rows that go to external
	// integrations, so they have to be fiat.
	if applyTaxes {
		if err := s.applyTaxesToInvoice(ctx, inv, *req); err != nil {
			return false, err
		}
	}

	inv.AmountRemaining = decimal.Max(inv.AmountDue.Sub(inv.AmountPaid), decimal.Zero)
	// Negative usage can lower a paid draft below what was already collected.
	if inv.AmountPaid.GreaterThan(inv.AmountDue) {
		inv.PaymentStatus = types.PaymentStatusOverpaid
	}
	for _, item := range inv.LineItems {
		if err := s.InvoiceLineItemRepo.Update(ctx, item); err != nil {
			return false, err
		}
	}

	now := time.Now().UTC()
	inv.LastComputedAt = &now

	return false, s.InvoiceRepo.Update(ctx, inv)
}

func (s *invoiceService) afterInvoiceComputation(ctx context.Context, inv *invoice.Invoice, skipped, wasSkipped bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.saveCompletedCycleUsageCheckpoints(ctx, inv); err != nil {
		s.Logger.Error(ctx, "saving usage checkpoints failed; reads use the preceding checkpoint",
			"subscription_id", lo.FromPtr(inv.SubscriptionID), "invoice_id", inv.ID, "error", err)
	}

	// Notify draft-stage listeners (currently just Tabs sync) that the invoice changed. Skip when
	// nothing actually happened (concurrent finalize/void raced us) or when the invoice landed on
	// SKIPPED — zero-amount invoices are never synced downstream, so there's nothing to notify.
	if !skipped {
		s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdate, inv.ID)
	}
	if skipped != wasSkipped {
		recordInvoiceTransition(ctx, inv)
	}

	// A skipped renewal owes nothing, so release the new period's held grants now.
	if skipped && types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionCycle {
		if err := s.HandleIncompleteSubscriptionPayment(ctx, inv); err != nil {
			s.Logger.Error(ctx, "failed to release grants for skipped renewal invoice",
				"error", err,
				"invoice_id", inv.ID)
		}
	}
}
