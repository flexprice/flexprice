package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *WalletServiceSuite) checkoutParamsRazorpay() *dto.CheckoutParams {
	return &dto.CheckoutParams{
		PaymentParams: dto.PaymentParams{
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		},
	}
}

func (s *WalletServiceSuite) seedPendingWalletTopupCheckout(walletID string, idempotencyKey *string) *domainCheckout.CheckoutSession {
	ctx := s.GetContext()
	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionWalletTopup,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			WalletTopupParams: &types.WalletTopupParams{WalletID: walletID},
		}),
		IdempotencyKey: idempotencyKey,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))
	return session
}

func (s *WalletServiceSuite) TestTopUpWallet_InvoicedNoCheckout_RegressionPending() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	balanceBefore := s.testData.wallet.CreditBalance

	resp, err := s.service.TopUpWallet(ctx, s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    lo.ToPtr("topup-invoiced-no-checkout"),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Nil(resp.CheckoutSession)
	s.Require().NotNil(resp.WalletTransaction)
	s.Equal(types.TransactionStatusPending, resp.WalletTransaction.TxStatus)
	s.Require().NotNil(resp.InvoiceID)
	s.True(balanceBefore.Equal(resp.Wallet.CreditBalance), "pay-later without auto-complete must not credit balance")
}

func (s *WalletServiceSuite) TestTopUpWallet_CheckoutWrongReason_Rejected() {
	ctx := s.GetContext()
	balanceBefore := s.testData.wallet.CreditBalance

	_, err := s.service.TopUpWallet(ctx, s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
		Checkout:          s.checkoutParamsRazorpay(),
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "checkout is only supported for PURCHASED_CREDIT_INVOICED")

	w, err := s.GetStores().WalletRepo.GetWalletByID(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(balanceBefore.Equal(w.CreditBalance))

	sessions, err := s.GetStores().CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerIDs: []string{s.testData.customer.ID},
		Actions:     []types.CheckoutAction{types.CheckoutActionWalletTopup},
	})
	s.Require().NoError(err)
	s.Empty(sessions)
}

func (s *WalletServiceSuite) TestTopUpWallet_CheckoutConcurrentGuard() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	s.seedPendingWalletTopupCheckout(s.testData.wallet.ID, nil)

	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = s.testData.customer.ID
	before, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)

	_, err = s.service.TopUpWallet(ctx, s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		Checkout:          s.checkoutParamsRazorpay(),
		IdempotencyKey:    lo.ToPtr("topup-checkout-concurrent"),
	})
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected concurrent guard AlreadyExists, got %v", err)

	after, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Equal(len(before), len(after), "guard must reject before creating a credit-purchase draft")
}

func (s *WalletServiceSuite) TestTopUpWallet_CheckoutSessionCreateFailureArchivesDraft() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	// Same customer + checkout idempotency key, different wallet in config so the
	// concurrent guard does not fire — only session Create's idempotency check fails
	// after the draft invoice is created.
	idempKey := "wallet-topup-orphan-idemp-key"
	otherWalletID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET)
	s.seedPendingWalletTopupCheckout(otherWalletID, &idempKey)

	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = s.testData.customer.ID
	before, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)

	checkout := s.checkoutParamsRazorpay()
	checkout.IdempotencyKey = &idempKey

	_, err = s.service.TopUpWallet(ctx, s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		Checkout:          checkout,
		IdempotencyKey:    lo.ToPtr("wallet-tx-orphan-1"),
	})
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected session create AlreadyExists, got %v", err)

	after, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Equal(len(before), len(after), "draft invoice must be archived when session create fails")
}

func (s *WalletServiceSuite) TestHandlePurchasedCreditInvoiced_PayFirstForcesPendingDespiteAutoComplete() {
	s.seedAutoComplete(true)
	ctx := s.GetContext()
	balanceBefore := s.testData.wallet.CreditBalance
	ws := s.service.(*walletService)

	txID, invID, err := ws.handlePurchasedCreditInvoicedTransaction(
		ctx,
		s.testData.wallet.ID,
		lo.ToPtr("payfirst-force-pending"),
		&dto.TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(250),
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		},
		true,
	)
	s.Require().NoError(err)

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusPending, tx.TxStatus)

	invSvc := NewInvoiceService(s.buildServiceParams())
	inv, err := invSvc.GetInvoice(ctx, invID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, inv.InvoiceStatus)
	s.Equal(txID, inv.Metadata["wallet_transaction_id"])

	w, err := s.GetStores().WalletRepo.GetWalletByID(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(balanceBefore.Equal(w.CreditBalance), "pay-first must not credit before payment")
}

func (s *WalletServiceSuite) TestCompleteWalletTopupCheckout_CreditsWalletAndBonus() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	balanceBefore := s.testData.wallet.CreditBalance
	credits := decimal.NewFromInt(500)
	bonus := decimal.NewFromInt(50)
	params := s.buildServiceParams()
	ws := s.service.(*walletService)

	txID, invID, err := ws.handlePurchasedCreditInvoicedTransaction(
		ctx,
		s.testData.wallet.ID,
		lo.ToPtr("payfirst-complete-bonus"),
		&dto.TopUpWalletRequest{
			CreditsToAdd:      credits,
			BonusCreditsToAdd: &bonus,
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		},
		true,
	)
	s.Require().NoError(err)

	invSvc := NewInvoiceService(params)
	draftInv, err := invSvc.GetInvoice(ctx, invID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, draftInv.InvoiceStatus)

	checkoutSvc := &checkoutSessionService{ServiceParams: params}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draftInv.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionWalletTopup,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			WalletTopupParams: &types.WalletTopupParams{
				WalletID:            s.testData.wallet.ID,
				WalletTransactionID: txID,
			},
		}),
		CheckoutInvoiceID: &invID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	err = checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_wallet_topup_test_001",
	})
	s.Require().NoError(err)

	purchaseTx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, purchaseTx.TxStatus)

	filter := types.NewWalletTransactionFilter()
	filter.WalletID = &s.testData.wallet.ID
	txs, err := s.GetStores().WalletRepo.ListWalletTransactions(ctx, filter)
	s.Require().NoError(err)
	var foundBonus bool
	for _, tx := range txs {
		if tx.ParentTransactionID == txID && tx.TransactionReason == types.TransactionReasonPurchasedCreditBonus {
			s.Equal(types.TransactionStatusCompleted, tx.TxStatus)
			s.True(bonus.Equal(tx.CreditAmount))
			foundBonus = true
			break
		}
	}
	s.True(foundBonus, "expected completed bonus child")

	w, err := s.GetStores().WalletRepo.GetWalletByID(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	expected := balanceBefore.Add(credits).Add(bonus)
	s.True(expected.Equal(w.CreditBalance), "expected credit balance %s, got %s", expected, w.CreditBalance)

	finalInv, err := invSvc.GetInvoice(ctx, invID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalInv.InvoiceStatus)

	paySvc := NewPaymentService(params)
	finalPay, err := paySvc.GetPayment(ctx, payResp.ID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusSucceeded, finalPay.PaymentStatus)

	completed, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, completed.CheckoutStatus)
}
