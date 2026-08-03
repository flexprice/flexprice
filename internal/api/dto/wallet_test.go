package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopUpWalletRequest_ValidateCheckout(t *testing.T) {
	validCheckout := &CheckoutParams{
		PaymentParams: PaymentParams{
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		},
	}

	t.Run("no_checkout_ok", func(t *testing.T) {
		req := &TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		}
		require.NoError(t, req.Validate())
	})

	t.Run("checkout_with_invoiced_ok", func(t *testing.T) {
		req := &TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
			Checkout:          validCheckout,
		}
		require.NoError(t, req.Validate())
	})

	t.Run("checkout_with_direct_rejected", func(t *testing.T) {
		req := &TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonPurchasedCreditDirect,
			Checkout:          validCheckout,
		}
		err := req.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "checkout is only supported for PURCHASED_CREDIT_INVOICED")
	})

	t.Run("checkout_with_free_credit_rejected", func(t *testing.T) {
		req := &TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonFreeCredit,
			Checkout:          validCheckout,
		}
		err := req.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "checkout is only supported for PURCHASED_CREDIT_INVOICED")
	})

	t.Run("checkout_missing_payment_provider_rejected", func(t *testing.T) {
		req := &TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
			Checkout:          &CheckoutParams{},
		}
		err := req.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "PaymentProvider")
	})
}

func TestWalletResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		w := &WalletResponse{Wallet: &wallet.Wallet{ID: "wallet_1"}}
		out := w.ToWebhookPayload(types.WebhookEventWalletUpdated)
		assert.Equal(t, "wallet_1", out.ID)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var w *WalletResponse
		assert.Nil(t, w.ToWebhookPayload(types.WebhookEventWalletUpdated))
	})
}

func TestWalletTransactionResponse_ToWebhookPayload(t *testing.T) {
	t.Run("delegates trimming to nested Customer and Wallet", func(t *testing.T) {
		tx := &WalletTransactionResponse{
			Transaction: &wallet.Transaction{ID: "txn_1"},
			Customer:    &CustomerResponse{Customer: &customer.Customer{ID: "cust_1"}},
			Wallet:      &WalletResponse{Wallet: &wallet.Wallet{ID: "wallet_1"}},
		}
		out := tx.ToWebhookPayload(types.WebhookEventWalletTransactionCreated)
		assert.Equal(t, "cust_1", out.Customer.ID)
		assert.Equal(t, "wallet_1", out.Wallet.ID)
	})

	t.Run("nil Customer and Wallet fields pass through as nil", func(t *testing.T) {
		tx := &WalletTransactionResponse{Transaction: &wallet.Transaction{ID: "txn_1"}}
		out := tx.ToWebhookPayload(types.WebhookEventWalletTransactionCreated)
		assert.Nil(t, out.Customer)
		assert.Nil(t, out.Wallet)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var tx *WalletTransactionResponse
		assert.Nil(t, tx.ToWebhookPayload(types.WebhookEventWalletTransactionCreated))
	})
}
