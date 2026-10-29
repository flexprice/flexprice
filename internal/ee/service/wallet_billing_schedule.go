package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func walletAllowsScheduledPayments(w *wallet.Wallet) bool {
	return len(w.Config.AllowedPriceTypes) == 0 || lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll) || lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeFixed)
}

func (s *walletService) pendingBillingSchedule(ctx context.Context, w *wallet.Wallet, usage map[string]currentPeriodUsage, unpaid *dto.GetUnpaidInvoicesToBePaidResponse) (decimal.Decimal, map[string]currentPeriodUsage, error) {
	subscriptions, err := billingProjectionSubscriptions(ctx, s.ServiceParams, w.CustomerID)
	if err != nil {
		return decimal.Zero, nil, err
	}
	owners, err := billingProjectionOwners(ctx, s.ServiceParams, subscriptions)
	if err != nil {
		return decimal.Zero, nil, err
	}
	projection, err := projectBillingSchedule(ctx, s.ServiceParams, owners, w.CustomerID, w.Currency, time.Now().UTC())
	if err != nil {
		return decimal.Zero, nil, err
	}
	projectedUsage := lo.Assign(map[string]currentPeriodUsage{}, usage)
	extra := decimal.Zero
	for _, item := range projection.due {
		extra = extra.Add(item.Amount)
	}
	counted := make(map[string]bool)
	listed := make(map[string]bool)
	invoices := make(map[string]*invoice.Invoice)
	for _, inv := range unpaid.Invoices {
		invoices[inv.ID] = &inv.Invoice
		listed[inv.ID] = true
		for _, line := range inv.LineItems {
			if line.BillingScheduleItemID != nil && len(w.Config.AllowedPriceTypes) > 0 {
				counted[*line.BillingScheduleItemID] = true
			}
		}
	}
	for _, inv := range unpaid.ExcludedDraftInvoices {
		invoices[inv.ID] = &inv.Invoice
	}
	periods := lo.MapValues(usage, func(u currentPeriodUsage, _ string) time.Time { return u.periodStart })
	missingByInvoice := make(map[string]decimal.Decimal)
	for _, item := range projection.assigned {
		if counted[item.ID] {
			continue
		}
		known := item.InvoiceID != nil && invoices[*item.InvoiceID] != nil
		inv, line, err := projectedScheduleLine(ctx, s.ServiceParams, item, invoices)
		if err != nil {
			return decimal.Zero, nil, err
		}
		if line == nil || !inv.AmountRemaining.IsPositive() {
			continue
		}
		amount := decimal.Max(line.Amount.Sub(line.LineItemDiscount).Sub(line.InvoiceLevelDiscount), decimal.Zero)
		if isCurrentPeriodDraft(dto.NewInvoiceResponse(inv), periods) {
			id := *inv.SubscriptionID
			current := projectedUsage[id]
			current.amount = current.amount.Add(amount)
			projectedUsage[id] = current
			if !known {
				unpaid.CurrentPeriodCreditsApplied[id] = unpaid.CurrentPeriodCreditsApplied[id].Add(inv.TotalPrepaidCreditsApplied)
			}
			continue
		}
		missingByInvoice[inv.ID] = missingByInvoice[inv.ID].Add(amount)
	}
	for id, amount := range missingByInvoice {
		allowance := invoices[id].AmountRemaining
		if len(w.Config.AllowedPriceTypes) == 0 && listed[id] {
			allowance = legacyScheduleAllowance(invoices[id])
		}
		extra = extra.Add(decimal.Min(amount, allowance))
	}
	return extra, projectedUsage, nil
}

// Legacy projections already deduct invoice payments; only cap the additional principal.
func legacyScheduleAllowance(inv *invoice.Invoice) decimal.Decimal {
	usage, creditsOnUsage := decimal.Zero, decimal.Zero
	for _, line := range inv.LineItems {
		if lo.FromPtr(line.PriceType) == string(types.PRICE_TYPE_USAGE) {
			usage = usage.Add(line.Amount).Sub(line.PrepaidCreditsApplied).Sub(line.LineItemDiscount)
			creditsOnUsage = creditsOnUsage.Add(line.PrepaidCreditsApplied)
		}
	}
	if unallocatedCredits := inv.TotalPrepaidCreditsApplied.Sub(creditsOnUsage); unallocatedCredits.IsPositive() {
		usage = usage.Sub(unallocatedCredits)
	}
	legacyPending := decimal.Max(usage, decimal.Zero).Sub(inv.AmountPaid)
	return decimal.Max(inv.AmountRemaining.Sub(legacyPending), decimal.Zero)
}
