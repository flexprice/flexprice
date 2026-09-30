package service

import (
	"context"
	"sort"
	"time"

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
	// creditExpiryGracePeriod is how long after expiry the job waits when credit expiry settlement
	// is off, so the period's invoice can finalize and use the credit first.
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

// draftAllocation is a draft invoice and the most it may take from an expiring credit.
type draftAllocation struct {
	invoiceID string
	currency  string
	periodEnd time.Time
	maxAmount decimal.Decimal
}

// settlementTarget is a draft invoice and its usage charges from before the expiry.
type settlementTarget struct {
	draft *invoice.Invoice
	usage decimal.Decimal
}

// settleExpiringCredit settles an expiring credit against drafts for usage before the expiry,
// then expires the rest. Drafts are computed first; apply and expire share one tx.
func (s *walletService) settleExpiringCredit(ctx context.Context, tx *wallet.Transaction) (*types.ExpireCreditsResult, error) {
	subs, err := s.settlementSubscriptions(ctx, tx)
	if err != nil {
		return nil, err
	}

	expiry := lo.FromPtr(tx.ExpiryDate)
	allocations := make([]draftAllocation, 0, len(subs))
	for _, sub := range subs {
		targets, err := s.settlementTargets(ctx, sub, expiry)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			draft := target.draft
			// A payment recorded on the draft is left to finalization to reconcile.
			if draft.AmountPaid.IsPositive() {
				s.Logger.Info(ctx, "skipping expiry settlement on a draft with a recorded payment",
					"invoice_id", draft.ID, "amount_paid", draft.AmountPaid)
				continue
			}
			// Finalization places credits only on usage after discounts; never apply more than it can place.
			gross, net := usageLineTotals(draft)
			beforeExpiry := target.usage
			if gross.IsPositive() && net.LessThan(gross) {
				beforeExpiry = beforeExpiry.Mul(net).Div(gross)
			}
			maxAmount := decimal.Min(beforeExpiry, net).Sub(draft.DenominationPrepaidCreditsApplied())
			if !maxAmount.IsPositive() {
				continue
			}
			allocations = append(allocations, draftAllocation{invoiceID: draft.ID, currency: draft.DenominationCurrency(), periodEnd: lo.FromPtr(draft.PeriodEnd), maxAmount: maxAmount})
		}
	}
	// Oldest period first: that invoice finalizes first.
	sort.SliceStable(allocations, func(i, j int) bool { return allocations[i].periodEnd.Before(allocations[j].periodEnd) })

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
		for _, alloc := range allocations {
			if !remaining.IsPositive() {
				break
			}
			// Whole cents, rounded down so it never exceeds what finalization can place.
			amount := decimal.Min(alloc.maxAmount, s.GetCurrencyAmountFromCredits(remaining, w.ConversionRate)).
				RoundFloor(types.GetCurrencyPrecision(alloc.currency))
			if !amount.IsPositive() {
				continue
			}
			credits := s.GetCreditsFromCurrencyAmount(amount, w.ConversionRate)
			placed, err := creditAdjustmentService.ApplyExpiringCreditToInvoice(ctx, alloc.invoiceID, w, current, credits)
			if err != nil {
				return err
			}
			if placed.IsPositive() {
				remaining = remaining.Sub(credits)
				applied = applied.Add(placed)
			}
		}

		// Re-read: the applies above debited this credit.
		left, err := s.WalletRepo.GetTransactionByID(ctx, tx.ID)
		if err != nil {
			return err
		}
		if !left.CreditsAvailable.IsPositive() {
			return nil
		}
		expired = true
		return s.debitExpiredCredits(ctx, left)
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
		usage, err := s.earlierDraftUsageBeforeExpiry(ctx, sub, inv, expiry)
		if err != nil {
			return nil, err
		}
		if usage.IsPositive() {
			targets = append(targets, settlementTarget{draft: inv, usage: usage})
		}
	}

	if !sub.CurrentPeriodStart.Before(expiry) {
		return targets, nil
	}
	// Check usage first so no draft is created for a period with nothing to pay.
	usage, err := NewBillingService(s.ServiceParams).UsageChargesForWindow(ctx, sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, expiry)
	if err != nil {
		return nil, err
	}
	if !usage.IsPositive() {
		return targets, nil
	}
	draft, skipped, err := invoiceService.GetOrComputeCurrentPeriodDraft(ctx, sub)
	if err != nil {
		return nil, err
	}
	if !skipped && draft != nil {
		targets = append(targets, settlementTarget{draft: draft, usage: usage})
	}
	return targets, nil
}

// usageLineTotals returns the draft's usage charges before and after discounts, in its denomination.
func usageLineTotals(draft *invoice.Invoice) (gross, net decimal.Decimal) {
	for _, item := range draft.LineItems {
		if lo.FromPtr(item.PriceType) != string(types.PRICE_TYPE_USAGE) {
			continue
		}
		d := item.Denomination()
		gross = gross.Add(d.Amount)
		net = net.Add(decimal.Max(decimal.Zero, d.Amount.Sub(d.LineItemDiscount).Sub(d.InvoiceLevelDiscount)))
	}
	return gross, net
}

// earlierDraftUsageBeforeExpiry returns an earlier period's usage charges from before the expiry.
func (s *walletService) earlierDraftUsageBeforeExpiry(ctx context.Context, sub *subscription.Subscription, draft *invoice.Invoice, expiry time.Time) (decimal.Decimal, error) {
	periodStart, periodEnd := lo.FromPtr(draft.PeriodStart), lo.FromPtr(draft.PeriodEnd)
	if periodEnd.After(expiry) {
		return NewBillingService(s.ServiceParams).UsageChargesForWindow(ctx, sub, periodStart, periodEnd, expiry)
	}

	// The whole period is before the expiry: the computed draft's usage lines are that usage.
	usage := decimal.Zero
	for _, item := range draft.LineItems {
		if lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_USAGE) {
			usage = usage.Add(item.Denomination().Amount)
		}
	}
	return usage, nil
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
