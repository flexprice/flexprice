package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Credit expiry settlement: an expiring credit first pays the usage from before its expiry
// on the subscription's unfinalized cycle drafts, and only the rest expires.

const (
	// creditExpiryGracePeriod is how long after expiry the job waits when expiry settlement
	// consumption is off, so the period's invoice can finalize and use the credit first.
	creditExpiryGracePeriod = 6 * time.Hour
	// settlementGracePeriod lets late events timestamped before the expiry
	// arrive before the credit is applied to drafts.
	settlementGracePeriod = 2 * time.Hour
	// settlementFinalizationHold is how long after an expiry finalization waits for the expiry job:
	// the grace, the job's 15-minute schedule, and margin for a failed run.
	settlementFinalizationHold = settlementGracePeriod + time.Hour
)

func (s *walletService) CreditExpiryCutoff(ctx context.Context) (time.Time, error) {
	enabled, err := creditExpirySettlementEnabled(ctx, s.ServiceParams)
	if err != nil {
		return time.Time{}, err
	}
	grace := creditExpiryGracePeriod
	if enabled {
		grace = settlementGracePeriod
	}
	return time.Now().UTC().Add(-grace), nil
}

// creditExpirySettlementEnabled reports whether the tenant environment has credit expiry
// settlement turned on.
func creditExpirySettlementEnabled(ctx context.Context, params ServiceParams) (bool, error) {
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return false, nil
	}
	settingsSvc := NewSettingsService(params).(*settingsService)
	cfg, err := GetSetting[types.CreditExpirySettlementConfig](settingsSvc, ctx, types.SettingKeyCreditExpirySettlement)
	if err != nil {
		return false, err
	}
	return cfg.Enabled, nil
}

// settlementPlan is a draft invoice and the most it may take from an expiring credit.
type settlementPlan struct {
	invoiceID string
	currency  string
	periodEnd time.Time
	maxAmount decimal.Decimal
}

// settlementTarget is a draft invoice and its usage charges from before the expiry.
type settlementTarget struct {
	draft        *invoice.Invoice
	beforeExpiry decimal.Decimal
}

// settleExpiringCredit settles an expiring credit against drafts for usage before the expiry,
// then expires the rest. Drafts are computed first; apply and expire share one tx.
func (s *walletService) settleExpiringCredit(ctx context.Context, tx *wallet.Transaction) (*types.ExpireCreditsResult, error) {
	subs, err := s.settlementSubscriptions(ctx, tx)
	if err != nil {
		return nil, err
	}

	expiry := lo.FromPtr(tx.ExpiryDate)
	plans := make([]settlementPlan, 0, len(subs))
	for _, sub := range subs {
		targets, err := s.settlementTargets(ctx, sub, expiry)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			draft := target.draft
			// Usage is priced in the subscription's currency, so compare in the invoice's denomination.
			alreadyApplied, unpaid := draft.TotalPrepaidCreditsApplied, draft.AmountRemaining
			if cc := draft.CustomCurrency; cc != nil {
				alreadyApplied, unpaid = cc.TotalPrepaidCreditsApplied, cc.AmountDue.Sub(cc.FromFiat(draft.AmountPaid))
			}
			// Finalization places credits only on usage after discounts; never apply more than it can place.
			maxAmount := decimal.Min(decimal.Min(target.beforeExpiry, usageNet(draft)).Sub(alreadyApplied), unpaid)
			if !maxAmount.IsPositive() {
				continue
			}
			plans = append(plans, settlementPlan{invoiceID: draft.ID, currency: draft.DenominationCurrency(), periodEnd: lo.FromPtr(draft.PeriodEnd), maxAmount: maxAmount})
		}
	}
	// Oldest period first: that invoice finalizes first.
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].periodEnd.Before(plans[j].periodEnd) })

	applied := decimal.Zero
	expired := false
	creditAdjustmentService := NewCreditAdjustmentService(s.ServiceParams)
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		current, err := s.WalletRepo.GetTransactionByID(ctx, tx.ID)
		if err != nil {
			return err
		}
		w, err := s.WalletRepo.GetWalletByID(ctx, current.WalletID)
		if err != nil {
			return err
		}

		remaining := current.CreditsAvailable
		for _, plan := range plans {
			if !remaining.IsPositive() {
				break
			}
			// Whole cents, rounded down so it never exceeds what finalization can place.
			amount := decimal.Min(plan.maxAmount, s.GetCurrencyAmountFromCredits(remaining, w.ConversionRate)).
				RoundFloor(types.GetCurrencyPrecision(plan.currency))
			if !amount.IsPositive() {
				continue
			}
			credits := s.GetCreditsFromCurrencyAmount(amount, w.ConversionRate)
			placed, err := creditAdjustmentService.ApplyExpiringCreditToInvoice(ctx, plan.invoiceID, w, current, credits)
			if err != nil {
				return err
			}
			if placed.IsPositive() {
				remaining = remaining.Sub(credits)
				applied = applied.Add(placed)
			}
		}

		if !remaining.IsPositive() {
			return nil
		}
		expired = true
		return s.debitExpiredCredits(ctx, current, remaining)
	})
	if err != nil {
		return nil, err
	}

	return &types.ExpireCreditsResult{Expired: expired, Applied: applied}, nil
}

// settlementSubscriptions returns the customer's subscriptions an expiring credit can pay
// usage for, earliest period end first.
func (s *walletService) settlementSubscriptions(ctx context.Context, tx *wallet.Transaction) ([]*subscription.Subscription, error) {
	w, err := s.WalletRepo.GetWalletByID(ctx, tx.WalletID)
	if err != nil {
		return nil, err
	}
	// Finalization pays invoices only from active prepaid wallets.
	if w.WalletStatus != types.WalletStatusActive || w.WalletType != types.WalletTypePrePaid {
		return nil, nil
	}

	subs, err := NewSubscriptionService(s.ServiceParams).ListByCustomerID(ctx, tx.CustomerID)
	if err != nil {
		return nil, err
	}
	eligible := lo.Filter(subs, func(sub *subscription.Subscription, _ int) bool {
		return sub.SubscriptionStatus == types.SubscriptionStatusActive &&
			(sub.SubscriptionType == types.SubscriptionTypeStandalone || sub.SubscriptionType == types.SubscriptionTypeParent) &&
			types.IsMatchingCurrency(sub.Currency, tx.Currency) &&
			// Threshold invoices move current_period_start and would orphan the draft.
			!sub.HasPositiveAutoInvoiceThreshold()
	})

	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if !a.CurrentPeriodEnd.Equal(b.CurrentPeriodEnd) {
			return a.CurrentPeriodEnd.Before(b.CurrentPeriodEnd)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return eligible, nil
}

// settlementTargets returns the subscription's unfinalized cycle drafts with usage before the
// expiry, oldest first. The current period's draft is created only if it has such usage.
func (s *walletService) settlementTargets(ctx context.Context, sub *subscription.Subscription, expiry time.Time) ([]settlementTarget, error) {
	invoiceService := NewInvoiceService(s.ServiceParams)
	startedBefore := sub.CurrentPeriodStart
	if expiry.Before(startedBefore) {
		startedBefore = expiry
	}
	earlier, err := invoiceService.ListOpenCycleDrafts(ctx, sub.ID, startedBefore)
	if err != nil {
		return nil, err
	}

	targets := make([]settlementTarget, 0, len(earlier)+1)
	for _, inv := range earlier {
		// Its usage is final only once computed after the period ended.
		if inv.LastComputedAt == nil || inv.LastComputedAt.Before(*inv.PeriodEnd) {
			computed, skipped, err := invoiceService.ComputeInvoice(ctx, inv.ID, nil)
			if err != nil {
				return nil, err
			}
			if skipped {
				continue
			}
			inv = computed
		}
		beforeExpiry, err := s.earlierDraftUsageBeforeExpiry(ctx, sub, inv, expiry)
		if err != nil {
			return nil, err
		}
		if beforeExpiry.IsPositive() {
			targets = append(targets, settlementTarget{draft: inv, beforeExpiry: beforeExpiry})
		}
	}

	if !sub.CurrentPeriodStart.Before(expiry) {
		return targets, nil
	}
	// Check usage first so no draft is created for a period with nothing to pay.
	charges, err := NewBillingService(s.ServiceParams).UsageChargesForWindow(ctx, sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, expiry)
	if err != nil {
		return nil, err
	}
	if !lo.ContainsBy(charges, func(c dto.CreateInvoiceLineItemRequest) bool { return c.Amount.IsPositive() }) {
		return targets, nil
	}
	draft, skipped, err := invoiceService.GetOrComputeCurrentPeriodDraft(ctx, sub)
	if err != nil {
		return nil, err
	}
	if !skipped && draft != nil {
		targets = append(targets, settlementTarget{draft: draft, beforeExpiry: netOfLineDiscounts(draft, charges)})
	}
	return targets, nil
}

// usageNet returns the draft's usage charges after discounts, in its denomination: what
// finalization can place credits on.
func usageNet(draft *invoice.Invoice) decimal.Decimal {
	net := decimal.Zero
	for _, item := range usageLines(draft) {
		net = net.Add(lineNet(item))
	}
	return net
}

// netOfLineDiscounts scales each charge by its draft line's discounts (net / gross), the way
// finalization prices that line. A charge with no matching line can't be placed and counts as zero.
func netOfLineDiscounts(draft *invoice.Invoice, charges []dto.CreateInvoiceLineItemRequest) decimal.Decimal {
	lines := usageLines(draft)
	total := decimal.Zero
	for _, charge := range charges {
		line, ok := lo.Find(lines, func(item *invoice.InvoiceLineItem) bool { return chargeOnLine(charge, item) })
		if !ok {
			continue
		}
		gross := line.Denomination().Amount
		if !gross.IsPositive() {
			continue
		}
		total = total.Add(charge.Amount.Mul(lineNet(line)).Div(gross))
	}
	return total
}

// chargeOnLine reports whether charge is billed on line: same price, and the charge's period
// starts inside the line's.
func chargeOnLine(charge dto.CreateInvoiceLineItemRequest, line *invoice.InvoiceLineItem) bool {
	if lo.FromPtr(charge.PriceID) == "" || lo.FromPtr(charge.PriceID) != lo.FromPtr(line.PriceID) {
		return false
	}
	start := lo.FromPtr(charge.PeriodStart)
	if start.IsZero() || line.PeriodStart == nil || line.PeriodEnd == nil {
		return true
	}
	return !start.Before(*line.PeriodStart) && start.Before(*line.PeriodEnd)
}

func usageLines(draft *invoice.Invoice) []*invoice.InvoiceLineItem {
	return lo.Filter(draft.LineItems, func(item *invoice.InvoiceLineItem, _ int) bool {
		return lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_USAGE)
	})
}

func lineNet(item *invoice.InvoiceLineItem) decimal.Decimal {
	d := item.Denomination()
	return decimal.Max(decimal.Zero, d.Amount.Sub(d.LineItemDiscount).Sub(d.InvoiceLevelDiscount))
}

// earlierDraftUsageBeforeExpiry returns an earlier period's usage from before the expiry, after
// each line's discounts.
func (s *walletService) earlierDraftUsageBeforeExpiry(ctx context.Context, sub *subscription.Subscription, draft *invoice.Invoice, expiry time.Time) (decimal.Decimal, error) {
	periodStart, periodEnd := lo.FromPtr(draft.PeriodStart), lo.FromPtr(draft.PeriodEnd)
	if !periodEnd.After(expiry) {
		// The whole period is before the expiry: the computed draft's usage lines are that usage.
		return usageNet(draft), nil
	}
	charges, err := NewBillingService(s.ServiceParams).UsageChargesForWindow(ctx, sub, periodStart, periodEnd, expiry)
	if err != nil {
		return decimal.Zero, err
	}
	return netOfLineDiscounts(draft, charges), nil
}

func (s *walletService) HasPendingExpiringCredit(ctx context.Context, customerID, currency string, periodStart, periodEnd time.Time) (bool, error) {
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, w := range wallets {
		if w.WalletStatus != types.WalletStatusActive || w.WalletType != types.WalletTypePrePaid ||
			!types.IsMatchingCurrency(w.Currency, currency) {
			continue
		}
		filter := types.NewNoLimitWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(w.ID)
		filter.Type = lo.ToPtr(types.TransactionTypeCredit)
		filter.TransactionStatus = lo.ToPtr(types.TransactionStatusCompleted)
		filter.CreditsAvailableGT = lo.ToPtr(decimal.Zero)
		filter.ExpiryDateAfter = lo.ToPtr(periodStart)
		filter.ExpiryDateBefore = lo.ToPtr(periodEnd)
		credits, err := s.WalletRepo.ListWalletTransactions(ctx, filter)
		if err != nil {
			return false, err
		}
		for _, c := range credits {
			expiry := lo.FromPtr(c.ExpiryDate)
			// Strictly inside the period; a credit expiring at period end is eligible at finalization.
			if !expiry.After(periodStart) || !expiry.Before(periodEnd) {
				continue
			}
			if now.Before(expiry.Add(settlementFinalizationHold)) {
				return true, nil
			}
			s.Logger.Error(ctx, "expiring credit still unprocessed past the finalization hold",
				"error", "expiry job did not process credit in time",
				"credit_transaction_id", c.ID, "expiry_date", expiry)
		}
	}
	return false, nil
}
