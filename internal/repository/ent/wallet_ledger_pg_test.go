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
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestWalletLedgerExport
func TestWalletLedgerExport(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	runID := types.GenerateUUIDWithPrefix("ldg")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID)
	repo := NewWalletRepository(client, log, nil)
	tenantID, envID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	base := types.GetDefaultBaseModel(ctx)
	t0 := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

	newCustomer := func(suffix string) string {
		c, err := client.Writer(ctx).Customer.Create().
			SetID("cust_" + suffix + runID).
			SetTenantID(tenantID).
			SetEnvironmentID(envID).
			SetExternalID("ext_" + suffix + runID).
			SetName("Ledger " + suffix).
			SetCreatedBy(base.CreatedBy).
			SetUpdatedBy(base.UpdatedBy).
			Save(ctx)
		require.NoError(t, err)
		return c.ID
	}
	newWallet := func(customerID, suffix string) *walletdomain.Wallet {
		w := &walletdomain.Wallet{
			ID:                  "wallet_" + suffix + runID,
			CustomerID:          customerID,
			Currency:            "usd",
			WalletType:          types.WalletTypePrePaid,
			WalletStatus:        types.WalletStatusActive,
			ConversionRate:      decimal.NewFromInt(1),
			TopupConversionRate: decimal.NewFromInt(1),
			EnvironmentID:       envID,
			BaseModel:           base,
		}
		require.NoError(t, repo.CreateWallet(ctx, w))
		return w
	}
	custA, custB := newCustomer("a"), newCustomer("b")
	walletA, walletB := newWallet(custA, "a"), newWallet(custB, "b")

	// The purchase is created at t0 but only paid, and completed, at t0+3m: 500 credits for 450
	// before tax.
	paidAt := at(3)
	inv, err := client.Writer(ctx).Invoice.Create().
		SetID("inv_" + runID).
		SetTenantID(tenantID).
		SetEnvironmentID(envID).
		SetCustomerID(custA).
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
		SetPaidAt(paidAt).
		SetCreatedBy(base.CreatedBy).
		SetUpdatedBy(base.UpdatedBy).
		Save(ctx)
	require.NoError(t, err)

	// debit2 pays the October usage invoice.
	periodStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	usageInv, err := client.Writer(ctx).Invoice.Create().
		SetID("inv_usage_" + runID).
		SetTenantID(tenantID).
		SetEnvironmentID(envID).
		SetCustomerID(custA).
		SetInvoiceType(types.InvoiceTypeSubscription).
		SetInvoiceStatus(types.InvoiceStatusFinalized).
		SetCurrency("usd").
		SetPeriodStart(periodStart).
		SetPeriodEnd(periodEnd).
		SetCreatedBy(base.CreatedBy).
		SetUpdatedBy(base.UpdatedBy).
		Save(ctx)
	require.NoError(t, err)

	newTx := func(id string, w *walletdomain.Wallet, txType types.TransactionType, reason types.TransactionReason, credits, before, after int64, createdAt time.Time) *walletdomain.Transaction {
		tx := &walletdomain.Transaction{
			ID:                  id + runID,
			WalletID:            w.ID,
			CustomerID:          w.CustomerID,
			Type:                txType,
			Amount:              decimal.NewFromInt(credits),
			CreditAmount:        decimal.NewFromInt(credits),
			CreditBalanceBefore: decimal.NewFromInt(before),
			CreditBalanceAfter:  decimal.NewFromInt(after),
			TxStatus:            types.TransactionStatusCompleted,
			TransactionReason:   reason,
			Currency:            w.Currency,
			EnvironmentID:       envID,
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}
		tx.CreatedAt = createdAt
		return tx
	}

	free := newTx("wtx_f_", walletA, types.TransactionTypeCredit, types.TransactionReasonFreeCredit, 100, 0, 100, at(-1))
	purchase := newTx("wtx_p_", walletA, types.TransactionTypeCredit, types.TransactionReasonPurchasedCreditInvoiced, 500, 50, 550, at(0))
	purchase.SourceType, purchase.SourceID = types.WalletTxSourceTypeInvoice, inv.ID
	debit1 := newTx("wtx_d1_", walletA, types.TransactionTypeDebit, types.TransactionReasonInvoicePayment, 50, 100, 50, at(1))
	debit1.ConsumptionBreakdown = []*types.WalletTxConsumption{{CreditTransactionID: free.ID, Credits: decimal.NewFromInt(50)}}
	debit2 := newTx("wtx_d2_", walletA, types.TransactionTypeDebit, types.TransactionReasonInvoicePayment, 200, 550, 350, at(4))
	debit2.ConsumptionBreakdown = []*types.WalletTxConsumption{{CreditTransactionID: purchase.ID, Credits: decimal.NewFromInt(200)}}
	debit2.SourceType, debit2.SourceID = types.WalletTxSourceTypeInvoice, usageInv.ID
	other := newTx("wtx_o_", walletB, types.TransactionTypeCredit, types.TransactionReasonFreeCredit, 10, 0, 10, at(0))
	for _, tx := range []*walletdomain.Transaction{free, purchase, debit1, debit2, other} {
		require.NoError(t, repo.CreateTransaction(ctx, tx))
	}

	start, end := at(-5), time.Now().UTC().Add(time.Minute)
	onlyA := []string{"ext_a" + runID}
	ledger := func(t *testing.T, from, to time.Time, customers []string) []*walletdomain.WalletLedgerExportData {
		walletIDs, err := repo.ListWalletIDsForLedgerExport(ctx, tenantID, envID, from, to, customers)
		require.NoError(t, err)
		rows, err := repo.GetWalletLedgerForExport(ctx, tenantID, envID, walletIDs, from, to)
		require.NoError(t, err)
		return rows
	}
	ids := func(rows []*walletdomain.WalletLedgerExportData) []string {
		return lo.Map(rows, func(r *walletdomain.WalletLedgerExportData, _ int) string { return r.TransactionID })
	}

	t.Run("a purchase paid later sorts where it changed the balance", func(t *testing.T) {
		rows := ledger(t, start, end, onlyA)
		require.Equal(t, []string{free.ID, debit1.ID, purchase.ID, debit2.ID}, ids(rows))
		require.True(t, paidAt.Equal(rows[2].EffectiveAt), "effective at the invoice's paid time")
		require.True(t, at(0).Equal(rows[2].CreatedAt))

		balances := lo.Map(rows, func(r *walletdomain.WalletLedgerExportData, _ int) string {
			return r.CreditBalanceBefore.String() + ">" + r.CreditBalanceAfter.String()
		})
		require.Equal(t, []string{"0>100", "100>50", "50>550", "550>350"}, balances, "each row starts where the last ended")

		values := lo.Map(rows, func(r *walletdomain.WalletLedgerExportData, _ int) string {
			return r.TopUpAmountRemainingBefore.String() + ">" + r.TopUpAmountRemainingAfter.String()
		})
		require.Equal(t, []string{"0>0", "0>0", "0>450", "450>270"}, values)

		require.True(t, decimal.NewFromInt(450).Equal(*rows[2].AmountPaid))
		require.Equal(t, inv.ID, rows[2].InvoiceID)
		require.Equal(t, purchase.ID, rows[3].CreditTransactionID)
		require.True(t, decimal.NewFromInt(450).Equal(*rows[3].WalletTxnAmountPaid))
		require.Nil(t, rows[3].AmountPaid, "a debit has no paid amount of its own")

		require.Nil(t, rows[2].InvoicePeriodStart, "a one-off top-up invoice has no period")
		require.Equal(t, usageInv.ID, rows[3].InvoiceID)
		require.True(t, periodStart.Equal(*rows[3].InvoicePeriodStart), "the paid invoice's period")
		require.True(t, periodEnd.Equal(*rows[3].InvoicePeriodEnd))
		require.Nil(t, rows[1].InvoicePeriodStart, "no invoice, no period")
	})

	t.Run("the customer filter keeps only those customers", func(t *testing.T) {
		require.NotContains(t, ids(ledger(t, start, end, onlyA)), other.ID)
		require.Contains(t, ids(ledger(t, start, end, nil)), other.ID, "no filter means every customer")
		require.Empty(t, ledger(t, start, end, []string{"ext_unknown" + runID}))
	})

	t.Run("the window is on the effective time and history carries in", func(t *testing.T) {
		rows := ledger(t, at(2), end, onlyA)
		require.Equal(t, []string{purchase.ID, debit2.ID}, ids(rows), "created before the window, paid inside it")
		require.Equal(t, "0", rows[0].TopUpAmountRemainingBefore.String())
		require.Equal(t, "270", rows[1].TopUpAmountRemainingAfter.String())
	})

	t.Run("without a filter the wallets are those active in the window", func(t *testing.T) {
		walletIDs, err := repo.ListWalletIDsForLedgerExport(ctx, tenantID, envID, start, end, nil)
		require.NoError(t, err)
		require.Equal(t, []string{walletA.ID, walletB.ID}, walletIDs, "in order, each once")

		// Only the purchase paid at t0+3m lands in this window; nothing was created in it.
		rows := ledger(t, at(2), paidAt.Add(30*time.Second), nil)
		require.Equal(t, []string{purchase.ID}, ids(rows), "a late payment brings its wallet in")
	})

	t.Run("a batch holds only its wallets", func(t *testing.T) {
		rows, err := repo.GetWalletLedgerForExport(ctx, tenantID, envID, []string{walletB.ID}, start, end)
		require.NoError(t, err)
		require.Equal(t, []string{other.ID}, ids(rows))
	})

	t.Run("a bonus follows its purchase and an overdrawn debit closes at its balance", func(t *testing.T) {
		walletC := newWallet(newCustomer("c"), "c")
		// The bonus's id sorts before its purchase's and both share a time: only the parent link orders them.
		purchaseC := newTx("wtx_z_", walletC, types.TransactionTypeCredit, types.TransactionReasonPurchasedCreditDirect, 100, 0, 100, at(-2))
		bonus := newTx("wtx_b_", walletC, types.TransactionTypeCredit, types.TransactionReasonPurchasedCreditBonus, 10, 100, 110, at(-2))
		bonus.ParentTransactionID = purchaseC.ID
		// A manual debit of 130 when 110 was left: the breakdown holds the 110 drawn.
		manual := newTx("wtx_m_", walletC, types.TransactionTypeDebit, types.TransactionReasonManualBalanceDebit, 130, 110, -20, at(-1))
		manual.ConsumptionBreakdown = []*types.WalletTxConsumption{
			{CreditTransactionID: purchaseC.ID, Credits: decimal.NewFromInt(100)},
			{CreditTransactionID: bonus.ID, Credits: decimal.NewFromInt(10)},
		}
		for _, tx := range []*walletdomain.Transaction{purchaseC, bonus, manual} {
			require.NoError(t, repo.CreateTransaction(ctx, tx))
		}

		rows := ledger(t, start, end, []string{"ext_c" + runID})
		require.Equal(t, []string{purchaseC.ID, bonus.ID, manual.ID, manual.ID, manual.ID}, ids(rows))
		balances := lo.Map(rows, func(r *walletdomain.WalletLedgerExportData, _ int) string {
			return r.CreditTransactionID + ":" + r.Credits.String() + ":" + r.CreditBalanceBefore.String() + ">" + r.CreditBalanceAfter.String()
		})
		require.Equal(t, []string{
			":100:0>100",
			":10:100>110",
			purchaseC.ID + ":100:110>10",
			bonus.ID + ":10:10>0",
			":20:0>-20",
		}, balances, "the part past the credits gets its own row and the debit closes at its saved balance")
	})
}
