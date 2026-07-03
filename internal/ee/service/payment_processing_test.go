package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PaymentProcessingSuite covers PaymentProcessorService.ProcessPayment for the
// in-memory-testable payment methods: offline, credits, attempt tracking, invoice
// reconciliation, idempotent duplicate delivery, and the no-gateway error paths
// of payment links.
type PaymentProcessingSuite struct {
	testutil.BaseServiceTestSuite
	processor      PaymentProcessorService
	paymentService PaymentService
	testData       struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
		now      time.Time
	}
}

func TestPaymentProcessingSuite(t *testing.T) {
	suite.Run(t, new(PaymentProcessingSuite))
}

func (s *PaymentProcessingSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	s.processor = NewPaymentProcessorService(params)
	s.paymentService = NewPaymentService(params)
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_pay_proc",
		ExternalID: "ext_cust_pay_proc",
		Name:       "Payment Processing Customer",
		Email:      "pay_proc@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.invoice = s.createInvoice("inv_pay_proc", decimal.NewFromInt(100))
}

func (s *PaymentProcessingSuite) createInvoice(id string, amountDue decimal.Decimal) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       amountDue,
		AmountPaid:      decimal.Zero,
		AmountRemaining: amountDue,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

// createWallet creates a wallet with a backing credit transaction so the balance
// is actually consumable by debit operations.
func (s *PaymentProcessingSuite) createWallet(id string, balance decimal.Decimal, mutate func(*wallet.Wallet)) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  id,
		CustomerID:          s.testData.customer.ID,
		Name:                "Wallet " + id,
		Currency:            "usd",
		Balance:             balance,
		CreditBalance:       balance,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          types.WalletTypePostPaid,
		AlertState:          types.AlertStateOk,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(w)
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))

	if balance.GreaterThan(decimal.Zero) {
		s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            w.ID,
			CustomerID:          w.CustomerID,
			Type:                types.TransactionTypeCredit,
			Amount:              balance,
			CreditAmount:        balance,
			CreditsAvailable:    balance,
			CreditBalanceBefore: decimal.Zero,
			CreditBalanceAfter:  balance,
			TxStatus:            types.TransactionStatusCompleted,
			ReferenceType:       types.WalletTxReferenceTypeExternal,
			ReferenceID:         "seed_" + w.ID,
			IdempotencyKey:      "seed_" + w.ID,
			Currency:            w.Currency,
			BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
		}))
	}
	return w
}

func (s *PaymentProcessingSuite) createStoredPayment(id string, mutate func(*payment.Payment)) *payment.Payment {
	p := &payment.Payment{
		ID:                id,
		IdempotencyKey:    "idem_" + id,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            decimal.NewFromInt(100),
		Currency:          "usd",
		PaymentStatus:     types.PaymentStatusPending,
		EnvironmentID:     types.GetEnvironmentID(s.GetContext()),
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(p)
	}
	s.NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), p))
	return p
}

func (s *PaymentProcessingSuite) TestProcessPaymentNotFound() {
	_, err := s.processor.ProcessPayment(s.GetContext(), "pay_does_not_exist")
	s.Error(err)
}

func (s *PaymentProcessingSuite) TestProcessPaymentInvalidStatus() {
	testCases := []struct {
		name   string
		mutate func(*payment.Payment)
	}{
		{
			name: "offline_payment_already_succeeded",
			mutate: func(p *payment.Payment) {
				p.PaymentStatus = types.PaymentStatusSucceeded
			},
		},
		{
			name: "offline_payment_already_failed",
			mutate: func(p *payment.Payment) {
				p.PaymentStatus = types.PaymentStatusFailed
			},
		},
		{
			name: "payment_link_in_failed_state",
			mutate: func(p *payment.Payment) {
				p.PaymentMethodType = types.PaymentMethodTypePaymentLink
				p.PaymentGateway = lo.ToPtr(string(types.PaymentGatewayTypeStripe))
				p.PaymentStatus = types.PaymentStatusFailed
			},
		},
	}

	for i, tc := range testCases {
		s.Run(tc.name, func() {
			p := s.createStoredPayment("pay_bad_status_"+string(rune('a'+i)), tc.mutate)
			originalStatus := p.PaymentStatus

			_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
			s.Error(err)
			s.True(ierr.IsInvalidOperation(err))

			stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
			s.NoError(err)
			s.Equal(originalStatus, stored.PaymentStatus, "invalid-status processing must not mutate the payment")
		})
	}
}

func (s *PaymentProcessingSuite) TestProcessPaymentOfflineReconcilesInvoice() {
	p := s.createStoredPayment("pay_offline_full", nil)

	processed, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, processed.PaymentStatus)
	s.NotNil(processed.SucceededAt)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(100)))
	s.True(inv.AmountRemaining.IsZero())
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
	s.NotNil(inv.PaidAt)
}

func (s *PaymentProcessingSuite) TestProcessPaymentDuplicateDeliveryDoesNotDoubleCharge() {
	p := s.createStoredPayment("pay_offline_dup", nil)

	_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.NoError(err)

	// Duplicate delivery: the payment is already succeeded, so reprocessing must
	// be rejected and the invoice totals must not change.
	_, err = s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(100)), "duplicate delivery must not double the paid amount")
	s.True(inv.AmountRemaining.IsZero())
}

func (s *PaymentProcessingSuite) TestProcessPaymentPartialPaymentsAccumulate() {
	first := s.createStoredPayment("pay_partial_1", func(p *payment.Payment) {
		p.Amount = decimal.NewFromInt(40)
	})
	_, err := s.processor.ProcessPayment(s.GetContext(), first.ID)
	s.NoError(err)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(40)))
	s.True(inv.AmountRemaining.Equal(decimal.NewFromInt(60)))
	s.Equal(types.PaymentStatusPending, inv.PaymentStatus, "partial payment keeps invoice pending")
	s.Nil(inv.PaidAt)

	second := s.createStoredPayment("pay_partial_2", func(p *payment.Payment) {
		p.Amount = decimal.NewFromInt(60)
	})
	_, err = s.processor.ProcessPayment(s.GetContext(), second.ID)
	s.NoError(err)

	inv, err = s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(100)))
	s.True(inv.AmountRemaining.IsZero())
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
	s.NotNil(inv.PaidAt)
}

func (s *PaymentProcessingSuite) TestProcessPaymentOverpaymentClampsRemainingToZero() {
	p := s.createStoredPayment("pay_overpay", func(p *payment.Payment) {
		p.Amount = decimal.NewFromInt(150)
	})

	_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.NoError(err)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(150)))
	s.True(inv.AmountRemaining.IsZero(), "amount remaining must never go negative on overpayment")
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
}

func (s *PaymentProcessingSuite) TestProcessPaymentCreditsDebitsWalletExactlyOnce() {
	w := s.createWallet("wallet_credits_once", decimal.NewFromInt(150), nil)
	p := s.createStoredPayment("pay_credits_once", func(p *payment.Payment) {
		p.PaymentMethodType = types.PaymentMethodTypeCredits
		p.PaymentMethodID = w.ID
	})

	processed, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, processed.PaymentStatus)

	walletAfter, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.NoError(err)
	s.True(walletAfter.Balance.Equal(decimal.NewFromInt(50)), "wallet must be debited by exactly the payment amount")

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.Equal(decimal.NewFromInt(100)))
	s.True(inv.AmountRemaining.IsZero())

	// Duplicate delivery must not debit the wallet a second time.
	_, err = s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))

	walletAfterDup, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.NoError(err)
	s.True(walletAfterDup.Balance.Equal(decimal.NewFromInt(50)), "duplicate delivery must not double-debit the wallet")
}

func (s *PaymentProcessingSuite) TestProcessPaymentCreditsFailureBranches() {
	testCases := []struct {
		name         string
		walletMutate func(*wallet.Wallet)
		balance      decimal.Decimal
	}{
		{
			name:    "inactive_wallet_fails_payment",
			balance: decimal.NewFromInt(500),
			walletMutate: func(w *wallet.Wallet) {
				w.WalletStatus = types.WalletStatusFrozen
			},
		},
		{
			name:    "currency_mismatch_fails_payment",
			balance: decimal.NewFromInt(500),
			walletMutate: func(w *wallet.Wallet) {
				w.Currency = "eur"
			},
		},
		{
			name:         "insufficient_balance_fails_payment",
			balance:      decimal.NewFromInt(10),
			walletMutate: nil,
		},
	}

	for i, tc := range testCases {
		s.Run(tc.name, func() {
			w := s.createWallet("wallet_credits_fail_"+string(rune('a'+i)), tc.balance, tc.walletMutate)
			p := s.createStoredPayment("pay_credits_fail_"+string(rune('a'+i)), func(p *payment.Payment) {
				p.PaymentMethodType = types.PaymentMethodTypeCredits
				p.PaymentMethodID = w.ID
			})

			_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
			s.Error(err)

			stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
			s.NoError(err)
			s.Equal(types.PaymentStatusFailed, stored.PaymentStatus)
			s.NotNil(stored.FailedAt)
			s.NotEmpty(lo.FromPtr(stored.ErrorMessage))

			walletAfter, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
			s.NoError(err)
			s.True(walletAfter.Balance.Equal(tc.balance), "failed payment must not debit the wallet")
		})
	}
}

func (s *PaymentProcessingSuite) TestProcessPaymentTracksAttemptsAcrossRetries() {
	// Wallet with insufficient balance guarantees a deterministic failure.
	w := s.createWallet("wallet_attempts", decimal.NewFromInt(1), nil)
	p := s.createStoredPayment("pay_attempts", func(p *payment.Payment) {
		p.PaymentMethodType = types.PaymentMethodTypeCredits
		p.PaymentMethodID = w.ID
		p.TrackAttempts = true
	})

	_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.Error(err)

	attempt, err := s.GetStores().PaymentRepo.GetLatestAttempt(s.GetContext(), p.ID)
	s.NoError(err)
	s.Equal(1, attempt.AttemptNumber)
	s.Equal(types.PaymentStatusFailed, attempt.PaymentStatus)
	s.NotEmpty(lo.FromPtr(attempt.ErrorMessage))

	// Reset to pending (simulating an operator retry) and process again: the
	// attempt counter must increment.
	stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
	s.NoError(err)
	stored.PaymentStatus = types.PaymentStatusPending
	s.NoError(s.GetStores().PaymentRepo.Update(s.GetContext(), stored))

	_, err = s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.Error(err)

	attempt, err = s.GetStores().PaymentRepo.GetLatestAttempt(s.GetContext(), p.ID)
	s.NoError(err)
	s.Equal(2, attempt.AttemptNumber)

	attempts, err := s.GetStores().PaymentRepo.ListAttempts(s.GetContext(), p.ID)
	s.NoError(err)
	s.Len(attempts, 2)
}

func (s *PaymentProcessingSuite) TestProcessPaymentUnsupportedMethods() {
	testCases := []struct {
		name       string
		methodType types.PaymentMethodType
	}{
		{name: "ach_not_implemented", methodType: types.PaymentMethodTypeACH},
		{name: "unknown_method_type_rejected", methodType: types.PaymentMethodType("CARRIER_PIGEON")},
	}

	for i, tc := range testCases {
		s.Run(tc.name, func() {
			p := s.createStoredPayment("pay_unsupported_"+string(rune('a'+i)), func(p *payment.Payment) {
				p.PaymentMethodType = tc.methodType
				p.PaymentMethodID = "pm_something"
			})

			_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
			s.Error(err)
			s.True(ierr.IsInvalidOperation(err))

			stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
			s.NoError(err)
			s.Equal(types.PaymentStatusFailed, stored.PaymentStatus)
			s.NotNil(stored.FailedAt)
		})
	}
}

func (s *PaymentProcessingSuite) TestProcessPaymentCustomerDestinationSucceedsWithoutInvoiceReconciliation() {
	p := s.createStoredPayment("pay_customer_dest", func(p *payment.Payment) {
		p.DestinationType = types.PaymentDestinationTypeCustomer
		p.DestinationID = s.testData.customer.ID
	})

	processed, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
	s.NoError(err, "post-processing failure for unsupported destination is logged, not returned")
	s.Equal(types.PaymentStatusSucceeded, processed.PaymentStatus)

	// The invoice must be untouched since the payment did not target it.
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.True(inv.AmountPaid.IsZero())
}

func (s *PaymentProcessingSuite) TestProcessPaymentLinkWithoutGatewayConnection() {
	testCases := []struct {
		name    string
		gateway types.PaymentGatewayType
	}{
		{name: "stripe_link_stays_initiated_without_connection", gateway: types.PaymentGatewayTypeStripe},
		{name: "razorpay_link_stays_initiated_without_connection", gateway: types.PaymentGatewayTypeRazorpay},
		{name: "nomod_link_stays_initiated_without_connection", gateway: types.PaymentGatewayTypeNomod},
		{name: "unsupported_link_gateway_stays_initiated", gateway: types.PaymentGatewayTypePaddle},
	}

	for i, tc := range testCases {
		s.Run(tc.name, func() {
			p := s.createStoredPayment("pay_link_"+string(rune('a'+i)), func(p *payment.Payment) {
				p.PaymentMethodType = types.PaymentMethodTypePaymentLink
				p.PaymentGateway = lo.ToPtr(string(tc.gateway))
				p.PaymentStatus = types.PaymentStatusInitiated
				p.TrackAttempts = true
			})

			_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
			s.Error(err)

			stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
			s.NoError(err)
			s.Equal(types.PaymentStatusInitiated, stored.PaymentStatus,
				"payment link must stay INITIATED when gateway link creation fails")
			s.Nil(stored.FailedAt, "payment links are not marked failed on gateway errors")
			s.Nil(stored.ErrorMessage)

			// Attempts for payment links stay pending, carrying the error message.
			attempt, err := s.GetStores().PaymentRepo.GetLatestAttempt(s.GetContext(), p.ID)
			s.NoError(err)
			s.Equal(types.PaymentStatusPending, attempt.PaymentStatus)
			s.NotEmpty(lo.FromPtr(attempt.ErrorMessage))
		})
	}
}

func (s *PaymentProcessingSuite) TestProcessPaymentActivatesIncompleteSubscription() {
	pl := &plan.Plan{
		ID:        "plan_pay_proc",
		Name:      "Payment Proc Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), pl))

	newSub := func(id string, status types.SubscriptionStatus) *subscription.Subscription {
		sub := &subscription.Subscription{
			ID:                 id,
			PlanID:             pl.ID,
			CustomerID:         s.testData.customer.ID,
			Currency:           "usd",
			StartDate:          s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingAnchor:      s.testData.now.Add(-24 * time.Hour),
			SubscriptionStatus: status,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
		return sub
	}

	s.Run("subscription_create_invoice_paid_activates_subscription", func() {
		sub := newSub("subs_activate_me", types.SubscriptionStatusIncomplete)
		inv := s.createInvoice("inv_sub_create", decimal.NewFromInt(30))
		inv.SubscriptionID = &sub.ID
		inv.InvoiceType = types.InvoiceTypeSubscription
		inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCreate)
		s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

		p := s.createStoredPayment("pay_activate_sub", func(p *payment.Payment) {
			p.DestinationID = inv.ID
			p.Amount = decimal.NewFromInt(30)
		})

		_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
		s.NoError(err)

		subAfter, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, subAfter.SubscriptionStatus,
			"paying the SUBSCRIPTION_CREATE invoice must activate the incomplete subscription")
	})

	s.Run("non_qualifying_billing_reason_leaves_subscription_untouched", func() {
		sub := newSub("subs_stays_incomplete", types.SubscriptionStatusIncomplete)
		inv := s.createInvoice("inv_sub_cycle", decimal.NewFromInt(30))
		inv.SubscriptionID = &sub.ID
		inv.InvoiceType = types.InvoiceTypeSubscription
		inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
		s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

		p := s.createStoredPayment("pay_no_activate", func(p *payment.Payment) {
			p.DestinationID = inv.ID
			p.Amount = decimal.NewFromInt(30)
		})

		_, err := s.processor.ProcessPayment(s.GetContext(), p.ID)
		s.NoError(err)

		subAfter, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusIncomplete, subAfter.SubscriptionStatus,
			"a renewal invoice payment must not activate the subscription")
	})
}

func (s *PaymentProcessingSuite) TestCreatePaymentWithProcessPaymentPropagatesFailure() {
	// Explicit postpaid wallet with insufficient consumable credits: creation
	// succeeds (balance check happens in the processor), processing fails.
	w := s.createWallet("wallet_process_fail", decimal.NewFromInt(500), nil)
	walletStored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.NoError(err)
	walletStored.Balance = decimal.NewFromInt(20)
	walletStored.CreditBalance = decimal.NewFromInt(20)
	s.NoError(s.GetStores().WalletRepo.UpdateWallet(s.GetContext(), walletStored.ID, walletStored))

	_, err = s.paymentService.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeCredits,
		PaymentMethodID:   w.ID,
		Amount:            decimal.NewFromInt(100),
		Currency:          "usd",
		ProcessPayment:    true,
	})
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))

	// The payment record must exist and be marked failed.
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		QueryFilter:   types.NewNoLimitQueryFilter(),
		DestinationID: lo.ToPtr(s.testData.invoice.ID),
	})
	s.NoError(err)
	s.Len(payments, 1)
	s.Equal(types.PaymentStatusFailed, payments[0].PaymentStatus)
	s.NotEmpty(lo.FromPtr(payments[0].ErrorMessage))
}
