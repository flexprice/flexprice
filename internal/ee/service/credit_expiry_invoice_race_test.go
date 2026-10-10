package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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
		SubScheduleRepo:              stores.SubscriptionScheduleRepo,
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
		RefundRepo:                   stores.RefundRepo,
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

// Only credits added for the billed period pay its invoice: once the period's grant is drained
// the rest stays unpaid, and the next period's grant is untouched.
func (s *CreditExpiryInvoiceRaceSuite) TestApplyCreditsToInvoice_DoesNotSpillIntoNextPeriodsGrant() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-3 * time.Hour)

	periodGrant := s.seedGrant("wtxn_period_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	nextGrant := s.seedGrant("wtxn_next_period_grant", decimal.NewFromInt(30),
		periodEnd.Add(5*time.Minute), periodEnd.Add(30*24*time.Hour))

	inv := s.subscriptionInvoice("inv_race_spill", decimal.NewFromInt(40), periodStart, periodEnd)

	result, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.TotalPrepaidCreditsApplied),
		"expected 30 applied, got %s", result.TotalPrepaidCreditsApplied)

	s.True(s.creditsAvailable(periodGrant.ID).IsZero(),
		"the period's grant must be drained, got %s remaining", s.creditsAvailable(periodGrant.ID))
	s.True(decimal.NewFromInt(30).Equal(s.creditsAvailable(nextGrant.ID)),
		"the next period's grant must be untouched, got %s remaining", s.creditsAvailable(nextGrant.ID))
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
