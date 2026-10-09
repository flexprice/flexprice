//go:build pgintegration

package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestWalletCreditReports
func TestWalletCreditReports(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	// Each run gets its own environment, so the exports see only this run's rows.
	runID := types.GenerateUUIDWithPrefix("rpt")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID)
	repo := NewWalletRepository(client, log, nil)
	tenantID, envID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	base := types.GetDefaultBaseModel(ctx)
	start := time.Now().UTC().Add(-time.Minute)

	cust, err := client.Writer(ctx).Customer.Create().
		SetID("cust_" + runID).
		SetTenantID(tenantID).
		SetEnvironmentID(envID).
		SetExternalID("ext_" + runID).
		SetName("Reports Co").
		SetCreatedBy(base.CreatedBy).
		SetUpdatedBy(base.UpdatedBy).
		Save(ctx)
	require.NoError(t, err)

	// A 500 top-up with a 10% coupon and 18% tax: paid 450 before tax.
	inv, err := client.Writer(ctx).Invoice.Create().
		SetID("inv_" + runID).
		SetTenantID(tenantID).
		SetEnvironmentID(envID).
		SetCustomerID(cust.ID).
		SetInvoiceType(types.InvoiceTypeOneOff).
		SetInvoiceStatus(types.InvoiceStatusFinalized).
		SetPaymentStatus(types.PaymentStatusSucceeded).
		SetCurrency("usd").
		SetAmountDue(decimal.NewFromInt(531)).
		SetAmountPaid(decimal.NewFromInt(531)).
		SetAmountRemaining(decimal.Zero).
		SetSubtotal(decimal.NewFromInt(500)).
		SetTotalDiscount(decimal.NewFromInt(50)).
		SetTotalTax(decimal.NewFromInt(81)).
		SetTotal(decimal.NewFromInt(531)).
		SetCreatedBy(base.CreatedBy).
		SetUpdatedBy(base.UpdatedBy).
		Save(ctx)
	require.NoError(t, err)

	w := &walletdomain.Wallet{
		ID:                  "wallet_" + runID,
		CustomerID:          cust.ID,
		Currency:            "usd",
		WalletType:          types.WalletTypePrePaid,
		WalletStatus:        types.WalletStatusActive,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		EnvironmentID:       envID,
		BaseModel:           base,
	}
	require.NoError(t, repo.CreateWallet(ctx, w))

	newTx := func(id string, txType types.TransactionType, reason types.TransactionReason, credits int64) *walletdomain.Transaction {
		return &walletdomain.Transaction{
			ID:                id,
			WalletID:          w.ID,
			CustomerID:        cust.ID,
			Type:              txType,
			Amount:            decimal.NewFromInt(credits),
			CreditAmount:      decimal.NewFromInt(credits),
			CreditsAvailable:  decimal.NewFromInt(credits),
			TxStatus:          types.TransactionStatusCompleted,
			TransactionReason: reason,
			Currency:          w.Currency,
			EnvironmentID:     envID,
			BaseModel:         types.GetDefaultBaseModel(ctx),
		}
	}

	purchase := newTx("wtx_p_"+runID, types.TransactionTypeCredit, types.TransactionReasonPurchasedCreditInvoiced, 500)
	purchase.SourceType, purchase.SourceID = types.WalletTxSourceTypeInvoice, inv.ID
	bonus := newTx("wtx_b_"+runID, types.TransactionTypeCredit, types.TransactionReasonPurchasedCreditBonus, 100)
	bonus.SourceType, bonus.SourceID = types.WalletTxSourceTypeInvoice, inv.ID
	adjustment := newTx("wtx_a_"+runID, types.TransactionTypeCredit, types.TransactionReasonCreditAdjustment, 40)
	for _, tx := range []*walletdomain.Transaction{purchase, bonus, adjustment} {
		require.NoError(t, repo.CreateTransaction(ctx, tx))
	}

	debit := newTx("wtx_d_"+runID, types.TransactionTypeDebit, types.TransactionReasonInvoicePayment, 230)
	debit.CreditsAvailable = decimal.Zero
	debit.SourceType, debit.SourceID = types.WalletTxSourceTypeInvoice, "inv_usage_"+runID
	debit.ConsumptionBreakdown = []types.WalletTxConsumption{
		{CreditTransactionID: purchase.ID, Credits: decimal.NewFromInt(200)},
		{CreditTransactionID: bonus.ID, Credits: decimal.NewFromInt(20)},
		{CreditTransactionID: adjustment.ID, Credits: decimal.NewFromInt(10)},
	}
	require.NoError(t, repo.CreateTransaction(ctx, debit))
	legacyDebit := newTx("wtx_l_"+runID, types.TransactionTypeDebit, types.TransactionReasonInvoicePayment, 5)
	require.NoError(t, repo.CreateTransaction(ctx, legacyDebit))

	end := time.Now().UTC().Add(time.Minute)

	t.Run("top-ups carry the invoice and what was paid", func(t *testing.T) {
		rows, err := repo.GetCreditTopupsForExport(ctx, tenantID, envID, start, end, 100, 0)
		require.NoError(t, err)
		byID := map[string]*walletdomain.CreditTopupsExportData{}
		for _, r := range rows {
			byID[r.TopupID] = r
		}

		require.Equal(t, inv.ID, byID[purchase.ID].InvoiceID)
		require.True(t, byID[purchase.ID].PaidAmount.Valid)
		require.True(t, decimal.NewFromInt(450).Equal(byID[purchase.ID].PaidAmount.Decimal), "after discount, before tax")
		require.True(t, decimal.NewFromInt(500).Equal(byID[purchase.ID].Credits))

		require.Equal(t, inv.ID, byID[bonus.ID].InvoiceID)
		require.True(t, byID[bonus.ID].PaidAmount.Decimal.IsZero(), "the bonus does not count the invoice again")
		require.True(t, byID[bonus.ID].PaidAmount.Valid)

		require.Empty(t, byID[adjustment.ID].InvoiceID)
		require.False(t, byID[adjustment.ID].PaidAmount.Valid)
	})

	t.Run("debits split by the batches they drew from", func(t *testing.T) {
		rows, err := repo.GetCreditDebitsForExport(ctx, tenantID, envID, start, end, 100, 0)
		require.NoError(t, err)
		require.Len(t, rows, 4)

		for _, r := range rows[:3] {
			require.Equal(t, debit.ID, r.DebitID)
			require.Equal(t, "inv_usage_"+runID, r.InvoiceID)
			require.Equal(t, "ext_"+runID, r.ExternalID)
		}
		require.Equal(t, purchase.ID, rows[0].CreditTransactionID)
		require.True(t, decimal.NewFromInt(200).Equal(rows[0].Credits))
		require.True(t, decimal.NewFromInt(500).Equal(rows[0].BatchCredits.Decimal))
		require.True(t, decimal.NewFromInt(450).Equal(rows[0].BatchPaidAmount.Decimal))

		require.Equal(t, bonus.ID, rows[1].CreditTransactionID)
		require.True(t, rows[1].BatchPaidAmount.Valid)
		require.True(t, rows[1].BatchPaidAmount.Decimal.IsZero())

		require.Equal(t, adjustment.ID, rows[2].CreditTransactionID)
		require.False(t, rows[2].BatchPaidAmount.Valid)

		require.Equal(t, legacyDebit.ID, rows[3].DebitID)
		require.Empty(t, rows[3].CreditTransactionID)
		require.True(t, decimal.NewFromInt(5).Equal(rows[3].Credits))
	})

	t.Run("pages do not repeat or skip batch rows", func(t *testing.T) {
		var ids []string
		for offset := 0; ; offset += 2 {
			page, err := repo.GetCreditDebitsForExport(ctx, tenantID, envID, start, end, 2, offset)
			require.NoError(t, err)
			for _, r := range page {
				ids = append(ids, r.DebitID+"/"+r.CreditTransactionID)
			}
			if len(page) < 2 {
				break
			}
		}
		require.Equal(t, []string{
			debit.ID + "/" + purchase.ID,
			debit.ID + "/" + bonus.ID,
			debit.ID + "/" + adjustment.ID,
			legacyDebit.ID + "/",
		}, ids)
	})
}
