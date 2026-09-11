package service

import (
	"context"
	"github.com/flexprice/flexprice/internal/api/dto"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// CreditExpiryInvoiceRaceSuite covers the interaction between the two independent
// async workflows that touch prepaid credits:
//
//	A. WalletCreditExpiryWorkflow  — expires grants past their expiry_date (6h grace).
//	B. DraftAndCompute + FinalizeDraftInvoice — bills a closed subscription period and
//	   applies prepaid credits at finalization.
//
// They can run hours apart, so a grant can sit past its expiry_date while the invoice
// for the very period it belongs to is still being prepared. Two invariants must hold:
//
//  1. Finalization consumes the grant that belongs to the invoice's period, even when it
//     runs after that grant's expiry_date (regression: it silently consumed the *next*
//     period's grant and the period's own grant was then expired in full).
//  2. Expiry holds off while an invoice covering the grant's period still has credits to
//     apply, and releases once that invoice is settled (so credits do not leak forever).
type CreditExpiryInvoiceRaceSuite struct {
	testutil.BaseServiceTestSuite
	params           ServiceParams
	walletService    WalletService
	creditAdjustment CreditAdjustmentService
	invoiceService   InvoiceService

	cust   *customer.Customer
	wallet *wallet.Wallet
}

func TestCreditExpiryInvoiceRace(t *testing.T) {
	suite.Run(t, new(CreditExpiryInvoiceRaceSuite))
}

func (s *CreditExpiryInvoiceRaceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *CreditExpiryInvoiceRaceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.BaseServiceTestSuite.ClearStores()

	stores := s.GetStores()
	s.params = ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		RedisCache:                   s.GetRedisCache(),
		WalletRepo:                   stores.WalletRepo,
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		EventRepo:                    stores.EventRepo,
		MeterRepo:                    stores.MeterRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		EntitlementGrantRepo:         stores.EntitlementGrantRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		FeatureRepo:                  stores.FeatureRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		TenantRepo:                   stores.TenantRepo,
		UserRepo:                     stores.UserRepo,
		AuthRepo:                     stores.AuthRepo,
		PaymentRepo:                  stores.PaymentRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,
		CreditNoteRepo:               stores.CreditNoteRepo,
		CreditNoteLineItemRepo:       stores.CreditNoteLineItemRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		SettingsRepo:                 stores.SettingsRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		IntegrationFactory:           s.GetIntegrationFactory(),
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}

	s.walletService = NewWalletService(s.params)
	s.creditAdjustment = NewCreditAdjustmentService(s.params)
	s.invoiceService = NewInvoiceService(s.params)

	s.cust = &customer.Customer{
		ID:         "cust_credit_expiry_race",
		ExternalID: "ext_cust_credit_expiry_race",
		Name:       "Credit Expiry Race Customer",
		Email:      "race@test.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(stores.CustomerRepo.Create(s.GetContext(), s.cust))

	s.wallet = &wallet.Wallet{
		ID:                  "wallet_credit_expiry_race",
		CustomerID:          s.cust.ID,
		Currency:            "usd",
		WalletType:          types.WalletTypePrePaid,
		Balance:             decimal.Zero,
		CreditBalance:       decimal.Zero,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(stores.WalletRepo.CreateWallet(s.GetContext(), s.wallet))
}

func (s *CreditExpiryInvoiceRaceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// seedGrant writes a completed subscription credit grant directly, so the test can
// place a grant whose expiry_date is already in the past (TopUpWallet rejects those).
func (s *CreditExpiryInvoiceRaceSuite) seedGrant(id string, credits decimal.Decimal, createdAt, expiry time.Time) *wallet.Transaction {
	base := types.GetDefaultBaseModel(s.GetContext())
	base.CreatedAt = createdAt
	base.UpdatedAt = createdAt

	tx := &wallet.Transaction{
		ID:                  id,
		WalletID:            s.wallet.ID,
		CustomerID:          s.cust.ID,
		Currency:            s.wallet.Currency,
		Type:                types.TransactionTypeCredit,
		Amount:              credits,
		CreditAmount:        credits,
		CreditsAvailable:    credits,
		CreditBalanceBefore: decimal.Zero,
		CreditBalanceAfter:  credits,
		TxStatus:            types.TransactionStatusCompleted,
		TransactionReason:   types.TransactionReasonSubscriptionCredit,
		ReferenceType:       types.WalletTxReferenceTypeRequest,
		ReferenceID:         id,
		IdempotencyKey:      id,
		ExpiryDate:          lo.ToPtr(expiry),
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           base,
	}
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), tx))

	// Keep the wallet balance consistent with the seeded grants.
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.wallet.ID)
	s.NoError(err)
	newCredits := w.CreditBalance.Add(credits)
	s.NoError(s.GetStores().WalletRepo.UpdateWalletBalance(s.GetContext(), s.wallet.ID, newCredits, newCredits))
	return tx
}

// subscriptionInvoice builds a draft subscription invoice for [periodStart, periodEnd)
// carrying a single usage line item.
func (s *CreditExpiryInvoiceRaceSuite) subscriptionInvoice(id string, amount decimal.Decimal, periodStart, periodEnd time.Time) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.cust.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        amount,
		Total:           amount,
		AmountDue:       amount,
		AmountPaid:      decimal.Zero,
		AmountRemaining: amount,
		PeriodStart:     lo.ToPtr(periodStart),
		PeriodEnd:       lo.ToPtr(periodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:                    id + "_li",
				InvoiceID:             id,
				CustomerID:            s.cust.ID,
				Amount:                amount,
				Currency:              "usd",
				Quantity:              decimal.NewFromInt(1),
				PriceType:             lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
				PeriodStart:           lo.ToPtr(periodStart),
				PeriodEnd:             lo.ToPtr(periodEnd),
				LineItemDiscount:      decimal.Zero,
				PrepaidCreditsApplied: decimal.Zero,
				BaseModel:             types.GetDefaultBaseModel(s.GetContext()),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

func (s *CreditExpiryInvoiceRaceSuite) creditsAvailable(txID string) decimal.Decimal {
	tx, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), txID)
	s.Require().NoError(err)
	return tx.CreditsAvailable
}

// ---------------------------------------------------------------------------
// Workflow B — credit application at finalization
// ---------------------------------------------------------------------------

// The production regression, reduced: the grant whose expiry_date equals the invoice's
// period_end must pay that invoice even though finalization runs hours after that
// timestamp. Selecting eligible credits against `now` skips it and silently bills the
// next period's grant instead.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyCreditsToInvoice_ConsumesTheBilledPeriodsGrant() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-3 * time.Hour) // period closed 3h ago; expiry cron has not run yet

	periodGrant := s.seedGrant("wtxn_period_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	nextGrant := s.seedGrant("wtxn_next_period_grant", decimal.NewFromInt(30),
		periodEnd.Add(5*time.Minute), periodEnd.Add(30*24*time.Hour))

	inv := s.subscriptionInvoice("inv_race_apply", decimal.NewFromFloat(4.60), periodStart, periodEnd)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromFloat(4.60).Equal(result.TotalPrepaidCreditsApplied),
		"expected 4.60 applied, got %s", result.TotalPrepaidCreditsApplied)

	s.True(decimal.NewFromFloat(25.40).Equal(s.creditsAvailable(periodGrant.ID)),
		"the billed period's grant must fund its own invoice, got %s remaining",
		s.creditsAvailable(periodGrant.ID))
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(nextGrant.ID)),
		"the next period's grant must be untouched, got %s remaining",
		s.creditsAvailable(nextGrant.ID))
}

// Eligible credits stay ordered by expiry: the period's grant drains first and only the
// overflow reaches the next period's grant.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyCreditsToInvoice_DrainsPeriodGrantBeforeSpillingOver() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-3 * time.Hour)

	periodGrant := s.seedGrant("wtxn_period_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	nextGrant := s.seedGrant("wtxn_next_period_grant", decimal.NewFromInt(30),
		periodEnd.Add(5*time.Minute), periodEnd.Add(30*24*time.Hour))

	inv := s.subscriptionInvoice("inv_race_spill", decimal.NewFromInt(40), periodStart, periodEnd)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(result.TotalPrepaidCreditsApplied),
		"expected 40 applied, got %s", result.TotalPrepaidCreditsApplied)

	s.True(s.creditsAvailable(periodGrant.ID).IsZero(),
		"the period's grant must be drained first, got %s remaining", s.creditsAvailable(periodGrant.ID))
	s.True(decimal.NewFromInt(20).Equal(s.creditsAvailable(nextGrant.ID)),
		"only the overflow may reach the next grant, got %s remaining", s.creditsAvailable(nextGrant.ID))
}

// Using period_end as the reference must not reach back and revive grants that had
// already expired before the billed period even started.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyCreditsToInvoice_IgnoresGrantsExpiredBeforeThePeriod() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-3 * time.Hour)

	staleGrant := s.seedGrant("wtxn_stale_grant", decimal.NewFromInt(30),
		periodStart.Add(-30*24*time.Hour), periodStart.Add(-time.Hour))
	periodGrant := s.seedGrant("wtxn_period_grant", decimal.NewFromInt(30), periodStart, periodEnd)

	inv := s.subscriptionInvoice("inv_race_stale", decimal.NewFromInt(10), periodStart, periodEnd)

	_, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)

	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(staleGrant.ID)),
		"a grant that expired before the period must stay untouched, got %s", s.creditsAvailable(staleGrant.ID))
	s.True(decimal.NewFromInt(20).Equal(s.creditsAvailable(periodGrant.ID)),
		"the period's grant pays instead, got %s", s.creditsAvailable(periodGrant.ID))
}

// ---------------------------------------------------------------------------
// Workflow A — the expiry hold
// ---------------------------------------------------------------------------

// closedPeriodGrant seeds the common shape: a grant for a period that closed 7h ago,
// expired (past the 6h grace) but not yet processed by the expiry workflow.
func (s *CreditExpiryInvoiceRaceSuite) closedPeriodGrant() (*wallet.Transaction, time.Time, time.Time) {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-7 * time.Hour)
	tx := s.seedGrant("wtxn_expiring_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	return tx, periodStart, periodEnd
}

// A draft invoice holds the grant even before compute has populated its amounts —
// credits are only applied at finalization, so amount_remaining is not yet meaningful.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_HeldWhileInvoiceIsAnUncomputedDraft() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	inv := s.subscriptionInvoice("inv_hold_draft_zero", decimal.Zero, periodStart, periodEnd)
	s.Equal(types.InvoiceStatusDraft, inv.InvoiceStatus)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.False(result.Expired, "an uncomputed draft for this period must hold the grant")
	s.Equal(types.CreditExpirySkipReasonActiveInvoice, result.SkipReason)
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(tx.ID)))
}

func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_HeldWhileInvoiceIsAComputedDraft() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	s.subscriptionInvoice("inv_hold_draft", decimal.NewFromInt(12), periodStart, periodEnd)

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.False(result.Expired)
	s.Equal(types.CreditExpirySkipReasonActiveInvoice, result.SkipReason)
}

// A finalized invoice that still owes money can be settled from the wallet, so the hold
// stays on.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_HeldWhileFinalizedInvoiceStillOwes() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	inv := s.subscriptionInvoice("inv_hold_finalized_unpaid", decimal.NewFromInt(12), periodStart, periodEnd)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.AmountRemaining = decimal.NewFromInt(12)
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.False(result.Expired)
	s.Equal(types.CreditExpirySkipReasonActiveInvoice, result.SkipReason)
}

// The hold must release once the period's invoice is settled, otherwise leftover credits
// are pinned forever and never expire.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_ReleasedOnceFinalizedInvoiceIsSettled() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	inv := s.subscriptionInvoice("inv_settled", decimal.NewFromInt(12), periodStart, periodEnd)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.PaymentStatus = types.PaymentStatusSucceeded
	inv.AmountPaid = decimal.NewFromInt(12)
	inv.AmountRemaining = decimal.Zero
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired, "a settled invoice must not pin the grant forever")
	s.True(s.creditsAvailable(tx.ID).IsZero())
}

// The subscription arm of the hold: an active subscription whose period has closed but
// has not been rotated yet means billing is still pending.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_HeldWhileSubscriptionPeriodIsUnrotated() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	sub := &subscription.Subscription{
		ID:                 "subs_unrotated",
		CustomerID:         s.cust.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd, // period closed, not yet rotated
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.False(result.Expired)
	s.Equal(types.CreditExpirySkipReasonActiveSubscription, result.SkipReason)
}

func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_ExpiresWhenNothingHoldsIt() {
	tx, _, _ := s.closedPeriodGrant()

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired)
	s.Equal(types.CreditExpirySkipReasonNone, result.SkipReason)
	s.True(s.creditsAvailable(tx.ID).IsZero())

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.wallet.ID)
	s.NoError(err)
	s.True(w.CreditBalance.IsZero(), "expiry debits the balance, got %s", w.CreditBalance)
}

// A draft for a period the grant does not belong to must not hold it.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_IgnoresDraftFromAnotherPeriod() {
	tx, _, periodEnd := s.closedPeriodGrant()

	s.subscriptionInvoice("inv_other_period", decimal.NewFromInt(12),
		periodEnd.Add(time.Minute), periodEnd.Add(30*24*time.Hour))

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired, "an invoice for a different period must not hold this grant")
}

// Wallets are per-currency, so an invoice this wallet could never pay must not hold it.
func (s *CreditExpiryInvoiceRaceSuite) TestExpireCredits_IgnoresDraftInAnotherCurrency() {
	tx, periodStart, periodEnd := s.closedPeriodGrant()

	inv := s.subscriptionInvoice("inv_other_currency", decimal.NewFromInt(12), periodStart, periodEnd)
	inv.Currency = "eur"
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	result, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	s.True(result.Expired, "a eur invoice must not hold a usd grant")
}

// ---------------------------------------------------------------------------
// Both workflows interleaved
// ---------------------------------------------------------------------------

// The full production timeline, in order:
//
//	t0            period grant issued, expires at period_end
//	period_end    period closes; subscription rotates and issues the next grant;
//	              the draft invoice for the closed period is created
//	+6h           expiry workflow runs → must hold (invoice still draft)
//	+7h           finalize workflow runs → must consume the *period's* grant
//	+7h           expiry workflow runs again → the period grant's leftover expires,
//	              the next period's grant is untouched
func (s *CreditExpiryInvoiceRaceSuite) TestTwoWorkflows_FinalizationConsumesGrantBeforeExpiryReclaimsIt() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-7 * time.Hour)

	periodGrant := s.seedGrant("wtxn_period_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	nextGrant := s.seedGrant("wtxn_next_period_grant", decimal.NewFromInt(30),
		periodEnd.Add(5*time.Minute), periodEnd.Add(30*24*time.Hour))

	inv := s.subscriptionInvoice("inv_two_workflows", decimal.NewFromFloat(4.60), periodStart, periodEnd)

	// --- expiry workflow, first pass: the invoice is still a draft ---
	held, err := s.walletService.ExpireCredits(s.GetContext(), periodGrant.ID)
	s.Require().NoError(err)
	s.False(held.Expired, "expiry must wait for the draft invoice to be finalized")
	s.Equal(types.CreditExpirySkipReasonActiveInvoice, held.SkipReason)

	// --- finalize workflow: credits are applied here ---
	s.Require().NoError(s.invoiceService.FinalizeInvoice(s.GetContext(), inv.ID, dto.FinalizeInvoiceRequest{}))

	finalized, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
	s.True(decimal.NewFromFloat(4.60).Equal(finalized.TotalPrepaidCreditsApplied),
		"finalization applies the credits, got %s", finalized.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromFloat(25.40).Equal(s.creditsAvailable(periodGrant.ID)),
		"the period's own grant must pay its invoice, got %s", s.creditsAvailable(periodGrant.ID))
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(nextGrant.ID)),
		"the next period's grant must be untouched, got %s", s.creditsAvailable(nextGrant.ID))

	// --- expiry workflow, second pass: the hold is released, the leftover expires ---
	expired, err := s.walletService.ExpireCredits(s.GetContext(), periodGrant.ID)
	s.Require().NoError(err)
	s.True(expired.Expired, "once the invoice is settled the leftover must expire")
	s.True(s.creditsAvailable(periodGrant.ID).IsZero())
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(nextGrant.ID)),
		"expiry of the old grant must not touch the new one, got %s", s.creditsAvailable(nextGrant.ID))

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.wallet.ID)
	s.NoError(err)
	s.True(decimal.NewFromInt(30).Equal(w.CreditBalance),
		"only the next period's 30 credits survive, got %s", w.CreditBalance)
}
