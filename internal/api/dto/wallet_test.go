package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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
		require.Contains(t, err.Error(), "payment_provider")
	})
}
