package service

import (
	"context"
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

// Pre-expiry credit consumption: with the setting on, an expiring credit first pays usage
// from before its expiry on the current period's draft invoice, and only the rest expires.
// Draft creation/compute and usage charges are stubbed through the walletService seams.

func (s *CreditExpiryInvoiceRaceSuite) enablePreExpiryConsumption() {
	setting := &domainSettings.Setting{
		ID:            "setting_pre_expiry",
		Key:           types.SettingKeyPreExpiryCreditConsumption,
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

// stubPreExpiry maps each subscription to its draft invoice and its usage up to the expiry.
func (s *CreditExpiryInvoiceRaceSuite) stubPreExpiry(drafts map[string]string, usage map[string]decimal.Decimal) *walletService {
	ws := s.walletService.(*walletService)
	ws.preExpiryDraftInvoice = func(ctx context.Context, sub *subscription.Subscription) (*invoice.Invoice, bool, error) {
		invoiceID, ok := drafts[sub.ID]
		if !ok {
			return nil, true, nil
		}
		inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
		return inv, false, err
	}
	ws.preExpiryUsageCharges = func(_ context.Context, sub *subscription.Subscription, _ time.Time) (decimal.Decimal, error) {
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
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_AppliesUsageBeforeExpiryAndExpiresTheRest() {
	s.enablePreExpiryConsumption()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_pre_expiry", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_pre_expiry", decimal.NewFromInt(20), periodStart, periodEnd)
	s.stubPreExpiry(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

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
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_CreditFullyUsedNothingExpires() {
	s.enablePreExpiryConsumption()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_pre_expiry", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_pre_expiry", decimal.NewFromInt(40), periodStart, periodEnd)
	s.stubPreExpiry(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(40)})

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
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_CappedAtUsageBeforeExpiry() {
	s.enablePreExpiryConsumption()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_pre_expiry", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_pre_expiry", decimal.NewFromInt(35), periodStart, periodEnd)
	s.stubPreExpiry(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.Applied))

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(updated.AmountRemaining))
}

// A credit that expired earlier in the period already paid part of the same usage.
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_CountsCreditsAlreadyAppliedToTheDraft() {
	s.enablePreExpiryConsumption()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	sub := s.activeSubscription("subs_pre_expiry", periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_pre_expiry", decimal.NewFromInt(25), periodStart, periodEnd)
	inv.TotalPrepaidCreditsApplied = decimal.NewFromInt(15)
	inv.Total = decimal.NewFromInt(10)
	inv.AmountDue = decimal.NewFromInt(10)
	inv.AmountRemaining = decimal.NewFromInt(10)
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	s.stubPreExpiry(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(5).Equal(result.Applied), "20 of usage minus 15 already applied, got %s", result.Applied)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(updated.TotalPrepaidCreditsApplied))
}

// No subscription with a draft for this period: the credit expires in full, as today.
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_NoEligibleSubscriptionExpiresInFull() {
	s.enablePreExpiryConsumption()
	tx, _, _ := s.midPeriodGrant(30)
	s.stubPreExpiry(nil, nil)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired)
	s.True(result.Applied.IsZero())
	s.True(s.creditsAvailable(tx.ID).IsZero())
}

// Subscriptions that can't hold pre-expiry usage are skipped: other currency, auto-invoice
// threshold, and a period that starts after the expiry.
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_SkipsIneligibleSubscriptions() {
	s.enablePreExpiryConsumption()
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
	s.stubPreExpiry(drafts, usage)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Applied.IsZero(), "no eligible subscription, got %s applied", result.Applied)
	s.True(result.Expired)
}

// Several subscriptions share the wallet: the one whose period ends first is paid first.
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_EarliestPeriodEndFirst() {
	s.enablePreExpiryConsumption()
	tx, periodStart, periodEnd := s.midPeriodGrant(30)

	later := s.activeSubscription("subs_later", periodStart, periodEnd.Add(5*24*time.Hour))
	earlier := s.activeSubscription("subs_earlier", periodStart, periodEnd)
	laterInv := s.subscriptionInvoice("inv_later", decimal.NewFromInt(25), periodStart, later.CurrentPeriodEnd)
	earlierInv := s.subscriptionInvoice("inv_earlier", decimal.NewFromInt(20), periodStart, earlier.CurrentPeriodEnd)
	s.stubPreExpiry(
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
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_SettingOffKeepsTodaysBehavior() {
	tx, _, _ := s.midPeriodGrant(30)
	ws := s.walletService.(*walletService)
	ws.preExpiryDraftInvoice = func(context.Context, *subscription.Subscription) (*invoice.Invoice, bool, error) {
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

	s.enablePreExpiryConsumption()
	cutoff, err = s.walletService.CreditExpiryCutoff(s.GetContext())
	s.Require().NoError(err)
	s.WithinDuration(before.Add(-preExpiryCreditExpiryGracePeriod), cutoff, time.Minute)
}

// Custom-currency invoice: the credit is applied in the denomination (the wallet's currency) and
// projected to fiat once. 1 CRED = 0.5 USD.
func (s *CreditExpiryInvoiceRaceSuite) TestPreExpiry_CustomCurrencyAppliedInDenomination() {
	s.enablePreExpiryConsumption()
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
	// The expiry path always computes the draft first; an uncomputed one would be recomputed
	// inline by the balance check that runs after the wallet debit.
	inv.LastComputedAt = &now
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))
	s.stubPreExpiry(map[string]string{sub.ID: inv.ID}, map[string]decimal.Decimal{sub.ID: decimal.NewFromInt(20)})

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
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_PreExpiryCreditsFirstThenWallet() {
	purchased := s.purchasedCredits(100)
	inv := s.draftWithAppliedCredits("inv_fin_mixed", decimal.NewFromInt(20), 35)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(35).Equal(result.TotalPrepaidCreditsApplied), "total %s", result.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(35).Equal(inv.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(35).Equal(lineCredits(inv)), "lines must add up to the invoice total")
	s.True(decimal.NewFromInt(85).Equal(s.creditsAvailable(purchased.ID)), "only 15 debited, got %s left", s.creditsAvailable(purchased.ID))
}

// Pre-expiry credits cover the usage: no wallet is debited.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_PreExpiryCreditsCoverEverything() {
	purchased := s.purchasedCredits(100)
	inv := s.draftWithAppliedCredits("inv_fin_covered", decimal.NewFromInt(20), 20)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(result.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(100).Equal(s.creditsAvailable(purchased.ID)))
	s.Empty(s.walletTransactions(types.TransactionReasonCreditAdjustment))
}

// Case 4: nothing left in any wallet. The pre-expiry credits still land on the lines.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_PreExpiryCreditsWithNoWalletBalance() {
	inv := s.draftWithAppliedCredits("inv_fin_nowallet", decimal.NewFromInt(30), 30)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.TotalPrepaidCreditsApplied))
	s.True(decimal.NewFromInt(30).Equal(lineCredits(inv)))
}

// The pre-expiry amount fills lines in order and spills into the next one.
func (s *CreditExpiryInvoiceRaceSuite) TestFinalize_PreExpiryCreditsSpillAcrossLines() {
	inv := s.draftWithAppliedCredits("inv_fin_spill", decimal.NewFromInt(20), 15, 10)

	_, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(15).Equal(inv.LineItems[0].PrepaidCreditsApplied))
	s.True(decimal.NewFromInt(5).Equal(inv.LineItems[1].PrepaidCreditsApplied))
}

// One-off invoices never carry pre-expiry credits; an existing total there is not treated as paid.
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

func (s *CreditExpiryInvoiceRaceSuite) TestUsageNetOfDraftCredits() {
	usage := map[string]decimal.Decimal{"a": decimal.NewFromInt(35), "b": decimal.NewFromInt(10), "c": decimal.NewFromInt(5)}
	applied := map[string]decimal.Decimal{"a": decimal.NewFromInt(20), "b": decimal.NewFromInt(30)}
	// a: 35−20=15, b: capped at its usage → 0, c: no credits → 5
	s.True(decimal.NewFromInt(20).Equal(usageNetOfDraftCredits(usage, applied)))
	s.True(decimal.NewFromInt(50).Equal(usageNetOfDraftCredits(usage, nil)))
}
