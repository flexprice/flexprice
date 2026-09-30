package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Credit expiry settlement: with the setting on, an expiring credit first pays usage
// from before its expiry on the current period's draft invoice, and only the rest expires.
// Draft creation/compute and usage charges are stubbed through the walletService seams.

func (s *CreditExpiryInvoiceRaceSuite) enableExpirySettlement() {
	setting := &domainSettings.Setting{
		ID:            "setting_expiry_settlement",
		Key:           types.SettingKeyCreditExpirySettlement,
		Value:         map[string]interface{}{"enabled": true},
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))
}

func (s *CreditExpiryInvoiceRaceSuite) activeSubscription(id string, periodStart, periodEnd time.Time) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         s.cust.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// stubExpirySettlement maps each subscription to its draft invoice and its usage up to the expiry.
func (s *CreditExpiryInvoiceRaceSuite) stubExpirySettlement(drafts map[string]string, usage map[string]decimal.Decimal) *walletService {
	ws := s.walletService.(*walletService)
	ws.settlementDraftInvoice = func(ctx context.Context, sub *subscription.Subscription) (*invoice.Invoice, bool, error) {
		invoiceID, ok := drafts[sub.ID]
		if !ok {
			return nil, true, nil
		}
		inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
		return inv, false, err
	}
	ws.settlementUsageCharges = func(_ context.Context, sub *subscription.Subscription, _, _, _ time.Time) (decimal.Decimal, error) {
		return usage[sub.ID], nil
	}
	return ws
}

// midPeriodGrant is a grant that expired 3h ago, inside a period that is still running.
func (s *CreditExpiryInvoiceRaceSuite) midPeriodGrant(credits int64) (*wallet.Transaction, time.Time, time.Time) {
	now := time.Now().UTC()
	periodStart := now.Add(-20 * 24 * time.Hour)
	periodEnd := now.Add(10 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_free_grant", decimal.NewFromInt(credits), periodStart, now.Add(-3*time.Hour))
	return tx, periodStart, periodEnd
}

func (s *CreditExpiryInvoiceRaceSuite) walletTransactions(reason types.TransactionReason) []*wallet.Transaction {
	filter := types.NewNoLimitWalletTransactionFilter()
	filter.WalletID = &s.wallet.ID
	filter.TransactionReason = &reason
	txs, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), filter)
	s.Require().NoError(err)
	return txs
}

// Case 2: 30 free credits, 20 of usage before expiry. 20 pays the draft, 10 expires.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_AppliesUsageBeforeExpiryAndExpiresTheRest() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(20), periodStart, periodEnd)
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired)
	s.True(decimal.NewFromInt(20).Equal(result.Applied), "applied %s", result.Applied)
	s.True(s.creditsAvailable(tx.ID).IsZero())

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, updated.InvoiceStatus)
	s.True(decimal.NewFromInt(20).Equal(updated.TotalPrepaidCreditsApplied))
	s.True(updated.Total.IsZero())
	s.True(updated.AmountRemaining.IsZero())

	adjustments := s.walletTransactions(types.TransactionReasonCreditAdjustment)
	s.Require().Len(adjustments, 1)
	s.True(decimal.NewFromInt(20).Equal(adjustments[0].CreditAmount))
	s.Equal(types.WalletTxReferenceTypeInvoice, adjustments[0].ReferenceType)
	s.Equal(inv.ID, adjustments[0].ReferenceID)

	expired := s.walletTransactions(types.TransactionReasonCreditExpired)
	s.Require().Len(expired, 1)
	s.True(decimal.NewFromInt(10).Equal(expired[0].CreditAmount))

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.wallet.ID)
	s.Require().NoError(err)
	s.True(w.CreditBalance.IsZero(), "the whole credit leaves the wallet at expiry, got %s", w.CreditBalance)
}

// Usage above the credit: the credit is used in full and nothing expires.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_CreditFullyUsedNothingExpires() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(40), periodStart, periodEnd)
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(40)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.False(result.Expired)
	s.True(decimal.NewFromInt(30).Equal(result.Applied))
	s.Empty(s.walletTransactions(types.TransactionReasonCreditExpired))

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(updated.AmountRemaining))
}

// The draft covers usage after the expiry too; only usage before the expiry may use the credit.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_CappedAtUsageBeforeExpiry() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(35), periodStart, periodEnd)
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.Applied))

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(updated.AmountRemaining))
}

// Finalization never pays invoices from postpaid or inactive wallets, so their expiring credits
// expire in full without touching the draft.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_OnlyActivePrepaidWallets() {
	cases := []struct {
		name       string
		walletType types.WalletType
		status     types.WalletStatus
	}{
		{"postpaid", types.WalletTypePostPaid, types.WalletStatusActive},
		{"frozen", types.WalletTypePrePaid, types.WalletStatusFrozen},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.enableExpirySettlement()
			tx, periodStart, periodEnd := s.midPeriodGrant(30)
			s.wallet.WalletType = tc.walletType
			s.wallet.WalletStatus = tc.status
			s.NoError(s.GetStores().WalletRepo.UpdateWallet(s.GetContext(), s.wallet.ID, s.wallet))
			sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
			inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(35), periodStart, periodEnd)
			s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

			result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
			s.Require().NoError(err)
			s.True(result.Applied.IsZero(), "applied %s", result.Applied)
			s.True(result.Expired)

			updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
			s.Require().NoError(err)
			s.True(updated.TotalPrepaidCreditsApplied.IsZero())
		})
	}
}

// Voiding a draft that carries credits applied at expiry refunds them like any void: a wallet
// top-up with no expiry.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_VoidRefundsAppliedCredits() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(35), periodStart, periodEnd)
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	_, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)

	_, err = s.invoiceService.VoidInvoice(s.GetContext(), inv.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)

	refunds := s.walletTransactions(types.TransactionReasonInvoiceVoidRefund)
	s.Require().Len(refunds, 1)
	s.True(decimal.NewFromInt(20).Equal(refunds[0].CreditAmount), "refunded %s", refunds[0].CreditAmount)
	s.Nil(refunds[0].ExpiryDate)
}

// Two grants that expired before one job run. The earlier one must be processed first: it can only
// pay usage before day 10, while the later one can pay anything before day 20. Usage: 8 before
// day 10, 24 before day 20. Earliest expiry first applies 8 + 16 = 24; the reverse applies 20 + 0.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_ProcessingOrderOfTwoGrants() {
	cases := []struct {
		name         string
		earlierFirst bool
		want         int64
	}{
		{"earliest expiry first", true, 24},
		{"latest expiry first", false, 20},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.enableExpirySettlement()
			now := time.Now().UTC()
			periodStart, periodEnd := now.Add(-25*24*time.Hour), now.Add(5*24*time.Hour)
			day10, day20 := periodStart.Add(9*24*time.Hour), periodStart.Add(19*24*time.Hour)
			g1 := s.seedGrant("wtxn_g1", decimal.NewFromInt(10), periodStart, day10)
			g2 := s.seedGrant("wtxn_g2", decimal.NewFromInt(20), periodStart.Add(11*24*time.Hour), day20)
			sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
			inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(29), periodStart, periodEnd)
			ws := s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, nil)
			ws.settlementUsageCharges = func(_ context.Context, _ *subscription.Subscription, _, _, until time.Time) (decimal.Decimal, error) {
				if until.After(day10) {
					return decimal.NewFromInt(24), nil
				}
				return decimal.NewFromInt(8), nil
			}

			order := []*wallet.Transaction{g1, g2}
			if !tc.earlierFirst {
				order = []*wallet.Transaction{g2, g1}
			}
			for _, g := range order {
				_, err := s.walletService.ExpireCredits(s.GetContext(), g.ID)
				s.Require().NoError(err)
			}

			updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
			s.Require().NoError(err)
			s.True(decimal.NewFromInt(tc.want).Equal(updated.TotalPrepaidCreditsApplied), "applied %s", updated.TotalPrepaidCreditsApplied)
		})
	}
}

// discountedDraft is a draft with a usage line of usage less discount, plus a fixed fee line.
func (s *CreditExpiryInvoiceRaceSuite) discountedDraft(id string, usage, discount, fee decimal.Decimal, periodStart, periodEnd time.Time) *invoice.Invoice {
	ctx := s.GetContext()
	total := usage.Sub(discount).Add(fee)
	line := func(suffix string, amount, lineDiscount decimal.Decimal, priceType types.PriceType) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{
			ID:               id + suffix,
			InvoiceID:        id,
			CustomerID:       s.cust.ID,
			Amount:           amount,
			LineItemDiscount: lineDiscount,
			Currency:         "usd",
			Quantity:         decimal.NewFromInt(1),
			PriceType:        lo.ToPtr(string(priceType)),
			PeriodStart:      lo.ToPtr(periodStart),
			PeriodEnd:        lo.ToPtr(periodEnd),
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
	}
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.cust.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        usage.Add(fee),
		TotalDiscount:   discount,
		Total:           total,
		AmountDue:       total,
		AmountRemaining: total,
		PeriodStart:     lo.ToPtr(periodStart),
		PeriodEnd:       lo.ToPtr(periodEnd),
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			line("_usage", usage, discount, types.PRICE_TYPE_USAGE),
			line("_fee", fee, decimal.Zero, types.PRICE_TYPE_FIXED),
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

// Finalization places credits only on usage after discounts, so expiry applies no more than that.
// $100 usage with a 50% coupon and a $200 fee: all usage before expiry places $50, not $100.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_CappedAtUsageAfterDiscounts() {
	cases := []struct {
		name         string
		beforeExpiry int64
		want         int64
	}{
		{"all usage before expiry", 100, 50},
		{"part of usage before expiry", 60, 30},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.enableExpirySettlement()
			tx, periodStart, periodEnd := s.midPeriodGrant(100)
			sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
			inv := s.discountedDraft("inv_settlement", decimal.NewFromInt(100), decimal.NewFromInt(50), decimal.NewFromInt(200), periodStart, periodEnd)
			s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(tc.beforeExpiry)})

			result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
			s.Require().NoError(err)
			s.True(decimal.NewFromInt(tc.want).Equal(result.Applied), "applied %s", result.Applied)
			s.True(result.Expired)
		})
	}
}

// The applied amount is whole cents, rounded down: $50 of usage at a 2/3 discount ratio is
// $33.333…, applied as $33.33. A conversion rate that doesn't divide evenly still lands on cents.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_AppliesWholeCents() {
	for _, rate := range []int64{1, 7} {
		s.Run(fmt.Sprintf("rate %d", rate), func() {
			s.SetupTest()
			s.enableExpirySettlement()
			tx, periodStart, periodEnd := s.midPeriodGrant(100)
			s.wallet.ConversionRate = decimal.NewFromInt(rate)
			s.NoError(s.GetStores().WalletRepo.UpdateWallet(s.GetContext(), s.wallet.ID, s.wallet))
			sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
			inv := s.discountedDraft("inv_settlement", decimal.NewFromInt(90), decimal.NewFromInt(30), decimal.Zero, periodStart, periodEnd)
			s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(50)})

			result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
			s.Require().NoError(err)
			want := decimal.RequireFromString("33.33")
			s.True(want.Equal(result.Applied), "applied %s", result.Applied)

			updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
			s.Require().NoError(err)
			s.True(want.Equal(updated.TotalPrepaidCreditsApplied), "invoice applied %s", updated.TotalPrepaidCreditsApplied)
			s.True(decimal.RequireFromString("26.67").Equal(updated.AmountRemaining), "remaining %s", updated.AmountRemaining)

			debits := s.walletTransactions(types.TransactionReasonCreditAdjustment)
			s.Require().Len(debits, 1)
			debited := debits[0].CreditAmount.Mul(s.wallet.ConversionRate)
			s.True(debited.LessThanOrEqual(want) && debited.GreaterThan(decimal.RequireFromString("33.32")), "debited %s", debited)
		})
	}
}

// A credit that expired earlier in the period already paid part of the same usage.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_CountsCreditsAlreadyAppliedToTheDraft() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_settlement", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_settlement", decimal.NewFromInt(25), periodStart, periodEnd)
	inv.TotalPrepaidCreditsApplied = decimal.NewFromInt(15)
	inv.Total = decimal.NewFromInt(10)
	inv.AmountDue = decimal.NewFromInt(10)
	inv.AmountRemaining = decimal.NewFromInt(10)
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(5).Equal(result.Applied), "20 of usage minus 15 already applied, got %s", result.Applied)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(updated.TotalPrepaidCreditsApplied))
}

// No subscription with a draft for this period: the credit expires in full, as today.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_NoEligibleSubscriptionExpiresInFull() {
	s.enableExpirySettlement()
	tx, _, _ := s.midPeriodGrant(30)
	s.stubExpirySettlement(nil, nil)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired)
	s.True(result.Applied.IsZero())
	s.True(s.creditsAvailable(tx.ID).IsZero())
}

// Subscriptions that can't hold usage before expiry are skipped: other currency, auto-invoice
// threshold, and a period that starts after the expiry.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_SkipsIneligibleSubscriptions() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)

	eur := s.activeSubscription("subs_eur", periodStart, periodEnd)
	eur.Currency = "eur"
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), eur))

	threshold := s.activeSubscription("subs_threshold", periodStart, periodEnd)
	limit := decimal.NewFromInt(100)
	threshold.AutoInvoiceThreshold = &limit
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), threshold))

	rolled := s.activeSubscription("subs_rolled", time.Now().UTC().Add(-time.Hour), periodEnd)

	drafts := map[string]string{}
	usage := map[string]decimal.Decimal{}
	for _, sub := range []*subscription.Subscription{eur, threshold, rolled} {
		inv := s.subscriptionInvoice("inv_"+sub.ID, decimal.NewFromInt(20), sub.CurrentPeriodStart, periodEnd)
		drafts[sub.ID] = inv.ID
		usage[sub.ID] = decimal.NewFromInt(20)
	}
	s.stubExpirySettlement(drafts, usage)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Applied.IsZero(), "no eligible subscription, got %s applied", result.Applied)
	s.True(result.Expired)
}

// Several subscriptions share the wallet: the one whose period ends first is paid first.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_EarliestPeriodEndFirst() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)

	later := s.activeSubscription("subs_later", periodStart, periodEnd.Add(5*24*time.Hour))
	earlier := s.activeSubscription("subs_earlier", periodStart, periodEnd)
	laterInv := s.subscriptionInvoice("inv_later", decimal.NewFromInt(25), periodStart, later.CurrentPeriodEnd)
	earlierInv := s.subscriptionInvoice("inv_earlier", decimal.NewFromInt(20), periodStart, earlier.CurrentPeriodEnd)
	s.stubExpirySettlement(
		map[string]string{later.ID: laterInv.ID, earlier.ID: earlierInv.ID},
		map[string]decimal.Decimal{later.ID: decimal.NewFromInt(25), earlier.ID: decimal.NewFromInt(20)},
	)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.Applied))
	s.False(result.Expired)

	e, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), earlierInv.ID)
	s.Require().NoError(err)
	l, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), laterInv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(e.TotalPrepaidCreditsApplied), "earlier got %s", e.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(10).Equal(l.TotalPrepaidCreditsApplied), "later got %s", l.TotalPrepaidCreditsApplied)
}

// Setting off: today's behavior. The draft path must not run.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_SettingOffKeepsTodaysBehavior() {
	tx, _, _ := s.midPeriodGrant(30)
	ws := s.walletService.(*walletService)
	ws.settlementDraftInvoice = func(context.Context, *subscription.Subscription) (*invoice.Invoice, bool, error) {
		s.Fail("draft path must not run with the setting off")
		return nil, true, nil
	}

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired)
	s.True(result.Applied.IsZero())
	s.Empty(s.walletTransactions(types.TransactionReasonCreditAdjustment))
}

// Applying the same credit to the same invoice twice debits once.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyExpiringCreditToInvoice_IsIdempotent() {
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	inv := s.subscriptionInvoice("inv_idempotent", decimal.NewFromInt(20), periodStart, periodEnd)

	first, err := s.creditAdjustment.ApplyExpiringCreditToInvoice(s.GetContext(), inv.ID, s.wallet, tx, decimal.NewFromInt(20))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(first))

	second, err := s.creditAdjustment.ApplyExpiringCreditToInvoice(s.GetContext(), inv.ID, s.wallet, tx, decimal.NewFromInt(20))
	s.Require().NoError(err)
	s.True(second.IsZero())
	s.Len(s.walletTransactions(types.TransactionReasonCreditAdjustment), 1)
	s.True(decimal.NewFromInt(10).Equal(s.creditsAvailable(tx.ID)))
}

// An invoice finalized in the meantime takes nothing; the credit then expires in full.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyExpiringCreditToInvoice_SkipsNonDraft() {
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	inv := s.subscriptionInvoice("inv_finalized", decimal.NewFromInt(20), periodStart, periodEnd)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	applied, err := s.creditAdjustment.ApplyExpiringCreditToInvoice(s.GetContext(), inv.ID, s.wallet, tx, decimal.NewFromInt(20))
	s.Require().NoError(err)
	s.True(applied.IsZero())
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(tx.ID)))
}

func (s *CreditExpiryInvoiceRaceSuite) TestCreditExpiryCutoff() {
	before := time.Now().UTC()
	cutoff, err := s.walletService.CreditExpiryCutoff(s.GetContext())
	s.Require().NoError(err)
	s.WithinDuration(before.Add(-creditExpiryGracePeriod), cutoff, time.Minute)

	s.enableExpirySettlement()
	cutoff, err = s.walletService.CreditExpiryCutoff(s.GetContext())
	s.Require().NoError(err)
	s.WithinDuration(before.Add(-settlementGracePeriod), cutoff, time.Minute)
}

// Custom-currency invoice: the credit is applied in the denomination (the wallet's currency) and
// projected to fiat once. 1 CRED = 0.5 USD.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_CustomCurrencyAppliedInDenomination() {
	s.enableExpirySettlement()
	ctx := s.GetContext()
	now := time.Now().UTC()
	periodStart := now.Add(-20 * 24 * time.Hour)
	periodEnd := now.Add(10 * 24 * time.Hour)

	credWallet := &wallet.Wallet{
		ID:                  "wallet_cred",
		CustomerID:          s.cust.ID,
		Currency:            "cred",
		WalletType:          types.WalletTypePrePaid,
		Balance:             decimal.NewFromInt(30),
		CreditBalance:       decimal.NewFromInt(30),
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(ctx, credWallet))
	expiry := now.Add(-3 * time.Hour)
	tx := &wallet.Transaction{
		ID:                  "wtxn_cred_grant",
		WalletID:            credWallet.ID,
		CustomerID:          s.cust.ID,
		Currency:            "cred",
		Type:                types.TransactionTypeCredit,
		Amount:              decimal.NewFromInt(30),
		CreditAmount:        decimal.NewFromInt(30),
		CreditsAvailable:    decimal.NewFromInt(30),
		CreditBalanceBefore: decimal.Zero,
		CreditBalanceAfter:  decimal.NewFromInt(30),
		TxStatus:            types.TransactionStatusCompleted,
		TransactionReason:   types.TransactionReasonFreeCredit,
		ReferenceType:       types.WalletTxReferenceTypeRequest,
		ReferenceID:         "wtxn_cred_grant",
		IdempotencyKey:      "wtxn_cred_grant",
		ExpiryDate:          &expiry,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(ctx, tx))

	sub := s.activeSubscription("subs_cred", periodStart, periodEnd)
	sub.Currency = "cred"
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	// 20 CRED of usage = 10 USD.
	inv := s.subscriptionInvoice("inv_cred", decimal.NewFromInt(10), periodStart, periodEnd)
	inv.CustomCurrency = &types.CustomCurrency{
		Code:      "cred",
		Rate:      decimal.NewFromFloat(0.5),
		Subtotal:  decimal.NewFromInt(20),
		Total:     decimal.NewFromInt(20),
		AmountDue: decimal.NewFromInt(20),
	}
	inv.LineItems[0].CustomCurrency = &types.CustomCurrencyLineItem{Amount: decimal.NewFromInt(20)}
	// The expiry path always computes the draft first; an uncomputed one would be recomputed
	// inline by the balance check that runs after the wallet debit.
	inv.LastComputedAt = &now
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))
	s.stubExpirySettlement(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(ctx, tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.Applied), "applied %s CRED", result.Applied)
	s.True(result.Expired, "10 CRED left over expires")

	updated, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Require().NotNil(updated.CustomCurrency)
	s.True(decimal.NewFromInt(20).Equal(updated.CustomCurrency.TotalPrepaidCreditsApplied))
	s.True(updated.CustomCurrency.Total.IsZero())
	s.True(decimal.NewFromInt(10).Equal(updated.TotalPrepaidCreditsApplied), "fiat applied %s", updated.TotalPrepaidCreditsApplied)
	s.True(updated.Total.IsZero())
	s.True(updated.AmountRemaining.IsZero())
}

// A wallet in another currency than the invoice's denomination can't pay it.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyExpiringCreditToInvoice_SkipsOtherCurrency() {
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	inv := s.subscriptionInvoice("inv_eur", decimal.NewFromInt(20), periodStart, periodEnd)
	inv.Currency = "eur"
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	applied, err := s.creditAdjustment.ApplyExpiringCreditToInvoice(s.GetContext(), inv.ID, s.wallet, tx, decimal.NewFromInt(20))
	s.Require().NoError(err)
	s.True(applied.IsZero())
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(tx.ID)))
}

// ---------------------------------------------------------------------------
// Finalization: credits applied to the draft before finalization come first in the pool and are
// placed on the lines like any credit, without a second debit.
// ---------------------------------------------------------------------------

// draftWithAppliedCredits is a subscription draft with the given usage lines and credits already
// applied at invoice level, as ApplyExpiringCreditToInvoice leaves it.
func (s *CreditExpiryInvoiceRaceSuite) draftWithAppliedCredits(id string, applied decimal.Decimal, usageLines ...int64) *invoice.Invoice {
	now := time.Now().UTC()
	periodStart, periodEnd := now.Add(-20*24*time.Hour), now.Add(10*24*time.Hour)
	subtotal := decimal.Zero
	lines := make([]*invoice.InvoiceLineItem, 0, len(usageLines))
	for i, amt := range usageLines {
		amount := decimal.NewFromInt(amt)
		subtotal = subtotal.Add(amount)
		lines = append(lines, &invoice.InvoiceLineItem{
			ID:          id + "_li_" + string(rune('a'+i)),
			InvoiceID:   id,
			CustomerID:  s.cust.ID,
			Amount:      amount,
			Currency:    "usd",
			Quantity:    decimal.NewFromInt(1),
			PriceType:   lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
			PeriodStart: lo.ToPtr(periodStart),
			PeriodEnd:   lo.ToPtr(periodEnd),
			BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
		})
	}
	total := decimal.Max(decimal.Zero, subtotal.Sub(applied))
	inv := &invoice.Invoice{
		ID:                         id,
		CustomerID:                 s.cust.ID,
		InvoiceType:                types.InvoiceTypeSubscription,
		InvoiceStatus:              types.InvoiceStatusDraft,
		PaymentStatus:              types.PaymentStatusPending,
		Currency:                   "usd",
		Subtotal:                   subtotal,
		TotalPrepaidCreditsApplied: applied,
		Total:                      total,
		AmountDue:                  total,
		AmountRemaining:            total,
		PeriodStart:                lo.ToPtr(periodStart),
		PeriodEnd:                  lo.ToPtr(periodEnd),
		BaseModel:                  types.GetDefaultBaseModel(s.GetContext()),
		LineItems:                  lines,
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

func (s *CreditExpiryInvoiceRaceSuite) purchasedCredits(credits int64) *wallet.Transaction {
	return s.seedGrant("wtxn_purchased", decimal.NewFromInt(credits), time.Now().UTC().Add(-40*24*time.Hour),
		time.Now().UTC().Add(365*24*time.Hour))
}

func lineCredits(inv *invoice.Invoice) decimal.Decimal {
	sum := decimal.Zero
	for _, li := range inv.LineItems {
		sum = sum.Add(li.PrepaidCreditsApplied)
	}
	return sum
}

// 20 paid at expiry, 35 of usage: other credits pay only 15, and the lines add up to 35.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_SettledCreditsFirstThenWallet() {
	purchased := s.purchasedCredits(100)
	inv := s.draftWithAppliedCredits("inv_fin_mixed", decimal.NewFromInt(20), 35)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(35).Equal(result.TotalPrepaidCreditsApplied), "total %s", result.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(35).Equal(inv.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(35).Equal(lineCredits(inv)), "lines must add up to the invoice total")
	s.True(decimal.NewFromInt(85).Equal(s.creditsAvailable(purchased.ID)), "only 15 debited, got %s left", s.creditsAvailable(purchased.ID))
}

// Credits settled at expiry cover the usage: no wallet is debited.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_SettledCreditsCoverEverything() {
	purchased := s.purchasedCredits(100)
	inv := s.draftWithAppliedCredits("inv_fin_covered", decimal.NewFromInt(20), 20)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(100).Equal(s.creditsAvailable(purchased.ID)))
	s.Empty(s.walletTransactions(types.TransactionReasonCreditAdjustment))
}

// Case 4: nothing left in any wallet. The credits settled at expiry still land on the lines.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_SettledCreditsWithNoWalletBalance() {
	inv := s.draftWithAppliedCredits("inv_fin_nowallet", decimal.NewFromInt(30), 30)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(30).Equal(lineCredits(inv)))
}

// The amount settled at expiry fills lines in order and spills into the next one.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_SettledCreditsSpillAcrossLines() {
	inv := s.draftWithAppliedCredits("inv_fin_spill", decimal.NewFromInt(20), 15, 10)

	_, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(inv.LineItems[0].PrepaidCreditsApplied))
	s.True(decimal.NewFromInt(5).Equal(inv.LineItems[1].PrepaidCreditsApplied))
}

// One-off invoices never carry credits settled at expiry; an existing total there is not treated as paid.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_OneOffInvoiceIgnoresExistingTotal() {
	purchased := s.purchasedCredits(100)
	inv := s.draftWithAppliedCredits("inv_fin_oneoff", decimal.NewFromInt(20), 30)
	inv.InvoiceType = types.InvoiceTypeOneOff

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(70).Equal(s.creditsAvailable(purchased.ID)), "all 30 comes from the wallet")
}

// The pool counts only credits the debit can use. A credit already past expiry (not yet removed
// by the expiry job) is in wallet.balance but not usable; before, the debit failed with
// "insufficient balance".
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_PoolExcludesCreditsPastExpiry() {
	now := time.Now().UTC()
	s.seedGrant("wtxn_past_expiry", decimal.NewFromInt(30), now.Add(-40*24*time.Hour), now.Add(-time.Hour))
	purchased := s.purchasedCredits(10)
	inv := s.draftWithAppliedCredits("inv_fin_pool", decimal.Zero, 25)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err, "the pool must not count the credit past expiry")
	s.True(decimal.NewFromInt(10).Equal(result.TotalPrepaidCreditsApplied), "only the usable 10, got %s", result.TotalPrepaidCreditsApplied)
	s.True(s.creditsAvailable(purchased.ID).IsZero())
}

// ---------------------------------------------------------------------------
// Ongoing balance: a subscription's current-period draft is covered by live usage, so it is not
// unpaid; credits already applied to it are reported so the caller nets them off that usage.
// ---------------------------------------------------------------------------

// subscriptionDraft is a computed draft for subID over [start, end) with one usage line and
// credits already applied at invoice level.
func (s *CreditExpiryInvoiceRaceSuite) subscriptionDraft(id, subID string, usage, applied decimal.Decimal, start, end time.Time) *invoice.Invoice {
	inv := s.draftWithAppliedCredits(id, applied, usage.IntPart())
	inv.SubscriptionID = lo.ToPtr(subID)
	inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
	inv.PeriodStart = lo.ToPtr(start)
	inv.PeriodEnd = lo.ToPtr(end)
	computedAt := time.Now().UTC()
	inv.LastComputedAt = &computedAt
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	return inv
}

func (s *CreditExpiryInvoiceRaceSuite) unpaid(starts map[string]time.Time) *dto.GetUnpaidInvoicesToBePaidResponse {
	resp, err := s.invoiceService.GetUnpaidInvoicesToBePaid(s.GetContext(), dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID:          s.cust.ID,
		Currency:            "usd",
		CurrentPeriodStarts: starts,
	})
	s.Require().NoError(err)
	return resp
}

// Mid-period: the current draft is not unpaid; its 20 applied at expiry is reported.
func (s *CreditExpiryInvoiceRaceSuite) TestUnpaid_CurrentPeriodDraftReportsAppliedCredits() {
	now := time.Now().UTC()
	start, end := now.Add(-20*24*time.Hour), now.Add(10*24*time.Hour)
	s.subscriptionDraft("inv_current", "subs_a", decimal.NewFromInt(30), decimal.NewFromInt(20), start, end)

	resp := s.unpaid(map[string]time.Time{"subs_a": start})
	s.Empty(resp.Invoices)
	s.True(resp.TotalUnpaidUsageCharges.IsZero())
	s.True(decimal.NewFromInt(20).Equal(resp.CurrentPeriodCreditsApplied["subs_a"]))
}

// Period ended but the billing run hasn't rolled the subscription yet: live usage still covers
// it, so the draft must not be counted a second time as unpaid.
func (s *CreditExpiryInvoiceRaceSuite) TestUnpaid_EndedButNotRolledDraftNotCountedTwice() {
	now := time.Now().UTC()
	start, end := now.Add(-30*24*time.Hour), now.Add(-time.Minute)
	s.subscriptionDraft("inv_unrolled", "subs_a", decimal.NewFromInt(30), decimal.Zero, start, end)

	s.Empty(s.unpaid(map[string]time.Time{"subs_a": start}).Invoices)

	// Without the caller's periods, today's rule applies: the ended draft is unpaid.
	s.Len(s.unpaid(nil).Invoices, 1)
}

// After rollover the previous period's draft is unpaid, net of credits applied at expiry even
// though its lines don't carry them yet.
func (s *CreditExpiryInvoiceRaceSuite) TestUnpaid_PreviousPeriodDraftNetOfAppliedCredits() {
	now := time.Now().UTC()
	prevStart, prevEnd := now.Add(-31*24*time.Hour), now.Add(-time.Hour)
	s.subscriptionDraft("inv_previous", "subs_a", decimal.NewFromInt(32), decimal.NewFromInt(20), prevStart, prevEnd)

	resp := s.unpaid(map[string]time.Time{"subs_a": prevEnd})
	s.Len(resp.Invoices, 1)
	s.True(decimal.NewFromInt(12).Equal(resp.TotalUnpaidUsageCharges), "32 usage − 20 applied, got %s", resp.TotalUnpaidUsageCharges)
	s.True(decimal.NewFromInt(12).Equal(resp.TotalUnpaidAmount))
}

// Only the cycle draft is covered by live usage. Another draft starting on the same day (here a
// proration invoice from a cancel) is still unpaid, and its credits aren't netted off usage.
func (s *CreditExpiryInvoiceRaceSuite) TestUnpaid_OnlyCycleDraftIsCurrentPeriod() {
	now := time.Now().UTC()
	start := now.Add(-20 * 24 * time.Hour)
	s.subscriptionDraft("inv_current", "subs_a", decimal.NewFromInt(30), decimal.NewFromInt(20), start, now.Add(10*24*time.Hour))
	proration := s.subscriptionDraft("inv_proration", "subs_a", decimal.NewFromInt(50), decimal.NewFromInt(5), start, now.Add(-time.Hour))
	proration.BillingReason = string(types.InvoiceBillingReasonProration)
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), proration))

	resp := s.unpaid(map[string]time.Time{"subs_a": start})
	s.Require().Len(resp.Invoices, 1)
	s.Equal(proration.ID, resp.Invoices[0].ID)
	s.True(decimal.NewFromInt(45).Equal(resp.TotalUnpaidUsageCharges), "50 usage − 5 applied, got %s", resp.TotalUnpaidUsageCharges)
	s.True(decimal.NewFromInt(20).Equal(resp.CurrentPeriodCreditsApplied["subs_a"]), "cycle draft only, got %s", resp.CurrentPeriodCreditsApplied["subs_a"])
}

func (s *CreditExpiryInvoiceRaceSuite) TestUsageNetOfDraftCredits() {
	usage := map[string]currentPeriodUsage{
		"a": {amount: decimal.NewFromInt(35)},
		"b": {amount: decimal.NewFromInt(10)},
		"c": {amount: decimal.NewFromInt(5)},
	}
	applied := map[string]decimal.Decimal{"a": decimal.NewFromInt(20), "b": decimal.NewFromInt(30)}
	// a: 35−20=15, b: capped at its usage → 0, c: no credits → 5
	s.True(decimal.NewFromInt(20).Equal(usageNetOfDraftCredits(usage, applied)))
	s.True(decimal.NewFromInt(50).Equal(usageNetOfDraftCredits(usage, nil)))
}

// ---------------------------------------------------------------------------
// Earlier periods: when the expiry job runs, the period the credit belongs to may already have
// rolled over. Its unfinalized draft still gets the share for usage before the expiry.
// ---------------------------------------------------------------------------

// cycleDraft is a computed SUBSCRIPTION_CYCLE draft for subID over [start, end) with one usage line.
func (s *CreditExpiryInvoiceRaceSuite) cycleDraft(id, subID string, usage int64, start, end time.Time) *invoice.Invoice {
	inv := s.draftWithAppliedCredits(id, decimal.Zero, usage)
	inv.SubscriptionID = lo.ToPtr(subID)
	inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
	inv.PeriodStart, inv.PeriodEnd = lo.ToPtr(start), lo.ToPtr(end)
	computedAt := end.Add(15 * time.Minute)
	inv.LastComputedAt = &computedAt
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	return inv
}

func (s *CreditExpiryInvoiceRaceSuite) failOnCurrentDraft() {
	s.walletService.(*walletService).settlementDraftInvoice = func(context.Context, *subscription.Subscription) (*invoice.Invoice, bool, error) {
		s.Fail("no current-period draft should be created")
		return nil, true, nil
	}
}

// Billing-cycle grant: expires exactly at the period end. By the time the job runs the
// subscription has rolled, so the credit pays the previous period's draft.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_BillingCycleCreditPaysPreviousPeriodDraft() {
	s.enableExpirySettlement()
	now := time.Now().UTC()
	boundary := now.Add(-3 * time.Hour)
	prevStart := boundary.Add(-30 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_cycle_grant", decimal.NewFromInt(30), prevStart, boundary)
	sub := s.activeSubscription("subs_rolled", boundary, boundary.Add(30*24*time.Hour))
	prev := s.cycleDraft("inv_prev", sub.ID, 20, prevStart, boundary)
	s.stubExpirySettlement(nil, nil)
	s.failOnCurrentDraft()

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.Applied), "applied %s", result.Applied)
	s.True(result.Expired)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), prev.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(updated.TotalPrepaidCreditsApplied))
}

// The credit expires an hour before the previous period ended: only usage before the expiry counts.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_PreviousPeriodCappedAtUsageBeforeExpiry() {
	s.enableExpirySettlement()
	now := time.Now().UTC()
	boundary := now.Add(-2 * time.Hour)
	prevStart := boundary.Add(-30 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_mid_grant", decimal.NewFromInt(30), prevStart, boundary.Add(-time.Hour))
	sub := s.activeSubscription("subs_rolled", boundary, boundary.Add(30*24*time.Hour))
	prev := s.cycleDraft("inv_prev", sub.ID, 25, prevStart, boundary)
	s.stubExpirySettlement(nil, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(15)})
	s.failOnCurrentDraft()

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(result.Applied), "applied %s", result.Applied)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), prev.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(updated.TotalPrepaidCreditsApplied))
}

// Both periods hold usage before expiry: the older draft is paid first.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_PreviousPeriodBeforeCurrent() {
	s.enableExpirySettlement()
	now := time.Now().UTC()
	boundary := now.Add(-10 * 24 * time.Hour)
	prevStart := boundary.Add(-30 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_grant", decimal.NewFromInt(20), prevStart, now.Add(-3*time.Hour))
	sub := s.activeSubscription("subs_two", boundary, boundary.Add(30*24*time.Hour))
	prev := s.cycleDraft("inv_prev", sub.ID, 10, prevStart, boundary)
	cur := s.subscriptionInvoice("inv_cur", decimal.NewFromInt(15), boundary, boundary.Add(30*24*time.Hour))
	s.stubExpirySettlement(map[string]string{sub.ID: cur.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(15)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.Applied))
	s.False(result.Expired)

	p, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), prev.ID)
	s.Require().NoError(err)
	c, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), cur.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(p.TotalPrepaidCreditsApplied), "previous got %s", p.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(10).Equal(c.TotalPrepaidCreditsApplied), "current got %s", c.TotalPrepaidCreditsApplied)
}

// No usage before the expiry in the current period: no draft is created for it.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_NoCurrentDraftWithoutUsageBeforeExpiry() {
	s.enableExpirySettlement()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	s.activeSubscription("subs_idle", periodStart, periodEnd)
	s.stubExpirySettlement(nil, map[string]decimal.Decimal{})
	s.failOnCurrentDraft()

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Applied.IsZero())
	s.True(result.Expired)
}

// A finalized previous-period invoice can't take credit settled at expiry.
func (s *CreditExpiryInvoiceRaceSuite) TestExpirySettlement_SkipsFinalizedPreviousPeriod() {
	s.enableExpirySettlement()
	now := time.Now().UTC()
	boundary := now.Add(-3 * time.Hour)
	prevStart := boundary.Add(-30 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_cycle_grant", decimal.NewFromInt(30), prevStart, boundary)
	sub := s.activeSubscription("subs_rolled", boundary, boundary.Add(30*24*time.Hour))
	prev := s.cycleDraft("inv_prev", sub.ID, 20, prevStart, boundary)
	prev.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), prev))
	s.stubExpirySettlement(nil, nil)
	s.failOnCurrentDraft()

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Applied.IsZero())
	s.True(result.Expired)
}

// ---------------------------------------------------------------------------
// Finalization waits while a credit that expired inside the draft's period is still unprocessed.
// ---------------------------------------------------------------------------

// finalizationCase is a cycle draft whose period ended 130m ago (computed 125m ago, so the default
// 2h delay has passed) and a credit expiring at expiryAgo.
func (s *CreditExpiryInvoiceRaceSuite) finalizationCase(expiryAgo time.Duration) (*invoice.Invoice, *wallet.Transaction) {
	now := time.Now().UTC()
	end := now.Add(-130 * time.Minute)
	start := end.Add(-30 * 24 * time.Hour)
	tx := s.seedGrant("wtxn_pending", decimal.NewFromInt(30), start, now.Add(-expiryAgo))
	inv := s.cycleDraft("inv_hold", "subs_hold", 20, start, end)
	computedAt := now.Add(-125 * time.Minute)
	inv.LastComputedAt = &computedAt
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	return inv, tx
}

func (s *CreditExpiryInvoiceRaceSuite) finalizationDue(invoiceID string) bool {
	due, err := s.invoiceService.IsFinalizationDue(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	return due
}

func (s *CreditExpiryInvoiceRaceSuite) TestFinalizationHold_WaitsForPendingExpiry() {
	s.enableExpirySettlement()
	inv, _ := s.finalizationCase(150 * time.Minute) // expired inside the period, 150m ago
	s.False(s.finalizationDue(inv.ID))
}

func (s *CreditExpiryInvoiceRaceSuite) TestFinalizationHold_SettingOff() {
	inv, _ := s.finalizationCase(150 * time.Minute)
	s.True(s.finalizationDue(inv.ID))
}

// A credit expiring exactly at the period end is eligible at finalization, so nothing waits.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalizationHold_ExpiryAtPeriodEnd() {
	s.enableExpirySettlement()
	inv, _ := s.finalizationCase(130 * time.Minute)
	s.True(s.finalizationDue(inv.ID))
}

// Once processed (nothing left on the credit), finalization goes ahead.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalizationHold_ReleasedOnceProcessed() {
	s.enableExpirySettlement()
	inv, tx := s.finalizationCase(150 * time.Minute)
	tx.CreditsAvailable = decimal.Zero
	s.NoError(s.GetStores().WalletRepo.UpdateTransaction(s.GetContext(), tx))
	s.True(s.finalizationDue(inv.ID))
}

// A stuck expiry job can't block billing: past the hold limit, finalization goes ahead.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalizationHold_SafetyCap() {
	s.enableExpirySettlement()
	inv, _ := s.finalizationCase(settlementFinalizationHold + 10*time.Minute)
	s.True(s.finalizationDue(inv.ID))
}
