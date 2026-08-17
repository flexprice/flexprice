package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// buildWalletTransactionResponse builds a realistic full wallet transaction API response,
// including the nested wallet the minimal webhook payload drops. No natural scaling
// collection exists on a transaction, so this is a single fixed fixture.
func buildWalletTransactionResponse() *dto.WalletTransactionResponse {
	return &dto.WalletTransactionResponse{
		Transaction: &wallet.Transaction{
			ID:                  "waltxn_bench_transaction",
			WalletID:            "wallet_bench_wallet",
			CustomerID:          "cust_bench_customer",
			Type:                types.TransactionTypeCredit,
			Amount:              decimal.NewFromInt(500),
			CreditAmount:        decimal.NewFromInt(500),
			CreditBalanceBefore: decimal.NewFromInt(4500),
			CreditBalanceAfter:  decimal.NewFromInt(5000),
			TxStatus:            types.TransactionStatusCompleted,
			ReferenceType:       types.WalletTxReferenceTypeInvoice,
			ReferenceID:         "inv_bench_invoice",
			Description:         "Benchmark wallet top-up",
			TransactionReason:   types.TransactionReasonPurchasedCreditDirect,
			Currency:            "usd",
			Metadata:            types.Metadata{"source": "benchmark"},
		},
		Wallet: buildWalletResponse(),
	}
}

func BenchmarkWalletPayloadSize(b *testing.B) {
	full := buildWalletResponse()

	b.Run("fixed/old", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			data, err := json.Marshal(full)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})

	b.Run("fixed/new", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			payload := NewWalletWebhookPayload(full, nil, types.WebhookEventWalletUpdated)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}

func BenchmarkWalletTransactionPayloadSize(b *testing.B) {
	full := buildWalletTransactionResponse()

	b.Run("fixed/old", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			data, err := json.Marshal(full)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})

	b.Run("fixed/new", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			payload := NewTransactionWebhookPayload(full, types.WebhookEventWalletTransactionCreated)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
