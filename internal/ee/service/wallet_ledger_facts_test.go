package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Ledger facts: every invoice-linked transaction carries source_type/source_id, and every
// debit records which credit batches it drew from.

func (s *WalletServiceSuite) resetWalletBalance() {
	s.NoError(s.GetStores().WalletRepo.UpdateWalletBalance(s.GetContext(), s.testData.wallet.ID, decimal.Zero, decimal.Zero))
}

func (s *WalletServiceSuite) grantFreeCredits(key string, credits int64, priority int) *wallet.Transaction {
	resp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(credits),
		TransactionReason: types.TransactionReasonFreeCredit,
		Priority:          lo.ToPtr(priority),
		IdempotencyKey:    lo.ToPtr(key),
	})
	s.Require().NoError(err)
	return resp.WalletTransaction.Transaction
}

func (s *WalletServiceSuite) debit(key string, credits int64, reason types.TransactionReason) *wallet.Transaction {
	s.Require().NoError(s.service.DebitWallet(s.GetContext(), &wallet.WalletOperation{
		WalletID:          s.testData.wallet.ID,
		Type:              types.TransactionTypeDebit,
		CreditAmount:      decimal.NewFromInt(credits),
		TransactionReason: reason,
		IdempotencyKey:    key,
	}))
	tx, err := s.GetStores().WalletRepo.GetTransactionByIdempotencyKey(s.GetContext(), key)
	s.Require().NoError(err)
	return tx
}

func sumConsumed(entries []types.WalletTxConsumption) decimal.Decimal {
	total := decimal.Zero
	for _, e := range entries {
		total = total.Add(e.Credits)
	}
	return total
}

func (s *WalletServiceSuite) TestLedgerFacts_DebitRecordsPerBatchConsumption() {
	s.resetWalletBalance()
	first := s.grantFreeCredits("ledger_first", 50, 1)
	second := s.grantFreeCredits("ledger_second", 30, 2)

	debit := s.debit("ledger_debit", 70, types.TransactionReasonInvoicePayment)

	s.Require().Len(debit.ConsumptionBreakdown, 2)
	s.Equal(first.ID, debit.ConsumptionBreakdown[0].CreditTransactionID)
	s.True(decimal.NewFromInt(50).Equal(debit.ConsumptionBreakdown[0].Credits))
	s.Equal(second.ID, debit.ConsumptionBreakdown[1].CreditTransactionID)
	s.True(decimal.NewFromInt(20).Equal(debit.ConsumptionBreakdown[1].Credits))
	s.True(debit.CreditAmount.Equal(sumConsumed(debit.ConsumptionBreakdown)))
	s.Equal(first.ID+","+second.ID, debit.Metadata["consumed_credit_tx_ids"], "the legacy ID list is still written")
	s.Empty(debit.SourceType, "a debit not applied to an invoice has no source")

	remaining, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), second.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(remaining.CreditsAvailable), "breakdown matches what left the batch")
}

func (s *WalletServiceSuite) TestLedgerFacts_ManualOverdraftRecordsUncoveredCredits() {
	s.resetWalletBalance()
	only := s.grantFreeCredits("ledger_overdraft_credit", 10, 1)

	debit := s.debit("ledger_overdraft_debit", 25, types.TransactionReasonManualBalanceDebit)

	s.Require().Len(debit.ConsumptionBreakdown, 2)
	s.Equal(only.ID, debit.ConsumptionBreakdown[0].CreditTransactionID)
	s.True(decimal.NewFromInt(10).Equal(debit.ConsumptionBreakdown[0].Credits))
	s.Empty(debit.ConsumptionBreakdown[1].CreditTransactionID, "credits below zero come from no batch")
	s.True(decimal.NewFromInt(15).Equal(debit.ConsumptionBreakdown[1].Credits))
	s.True(debit.CreditAmount.Equal(sumConsumed(debit.ConsumptionBreakdown)))
}

func (s *WalletServiceSuite) TestLedgerFacts_CreditsHaveNoBreakdown() {
	credit := s.grantFreeCredits("ledger_credit_only", 10, 1)

	s.Empty(credit.ConsumptionBreakdown)
	s.Empty(credit.SourceType, "free credits have no invoice")
}

func (s *WalletServiceSuite) TestLedgerFacts_PaidPurchaseLinksItsInvoice() {
	s.seedAutoComplete(false)

	resp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    lo.ToPtr("ledger_paid_purchase"),
	})
	s.Require().NoError(err)
	s.Empty(resp.WalletTransaction.SourceType, "the source is written when the purchase completes")

	invoiceID := lo.FromPtr(resp.InvoiceID)
	s.Require().NoError(s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), resp.WalletTransaction.ID, invoiceID))

	purchase, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), resp.WalletTransaction.ID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, purchase.TxStatus)
	s.Equal(types.WalletTxSourceTypeInvoice, purchase.SourceType)
	s.Equal(invoiceID, purchase.SourceID)
}

func (s *WalletServiceSuite) TestLedgerFacts_AutoCompletedPurchaseLinksItsInvoice() {
	s.seedAutoComplete(true)

	resp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(100),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    lo.ToPtr("ledger_auto_complete"),
	})
	s.Require().NoError(err)

	purchase, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), resp.WalletTransaction.ID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, purchase.TxStatus)
	s.Equal(types.WalletTxSourceTypeInvoice, purchase.SourceType)
	s.Equal(lo.FromPtr(resp.InvoiceID), purchase.SourceID)
}

func (s *CreditExpiryInvoiceRaceSuite) TestLedgerFacts_CreditAdjustmentDebitLinksItsInvoice() {
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now.Add(-3 * time.Hour)
	grant := s.seedGrant("wtxn_ledger_grant", decimal.NewFromInt(30), periodStart, periodEnd)
	inv := s.subscriptionInvoice("inv_ledger_apply", decimal.NewFromInt(10), periodStart, periodEnd)

	_, err := s.creditAdjustment.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)

	debits := s.walletTransactions(types.TransactionReasonCreditAdjustment)
	s.Require().Len(debits, 1)
	s.Equal(types.WalletTxSourceTypeInvoice, debits[0].SourceType)
	s.Equal(inv.ID, debits[0].SourceID)
	s.Require().Len(debits[0].ConsumptionBreakdown, 1)
	s.Equal(grant.ID, debits[0].ConsumptionBreakdown[0].CreditTransactionID)
	s.True(debits[0].CreditAmount.Equal(debits[0].ConsumptionBreakdown[0].Credits))
}

func (s *CreditExpiryInvoiceRaceSuite) TestLedgerFacts_ExpirySettlementDebitLinksItsInvoice() {
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	inv := s.subscriptionInvoice("inv_ledger_expiry", decimal.NewFromInt(20), periodStart, periodEnd)

	_, err := s.creditAdjustment.ApplyExpiringCreditToInvoice(s.GetContext(), inv.ID, s.wallet, tx, decimal.NewFromInt(20))
	s.Require().NoError(err)

	debits := s.walletTransactions(types.TransactionReasonCreditAdjustment)
	s.Require().Len(debits, 1)
	s.Equal(types.WalletTxSourceTypeInvoice, debits[0].SourceType)
	s.Equal(inv.ID, debits[0].SourceID)
	s.Require().Len(debits[0].ConsumptionBreakdown, 1)
	s.Equal(tx.ID, debits[0].ConsumptionBreakdown[0].CreditTransactionID)
}

func (s *RefundServiceSuite) TestLedgerFacts_RefundToWalletLinksTheRefundedInvoice() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(80)), s.testData.invoice, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.service.Dispatch(s.GetContext(), rows[0].ID))

	settled, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.Require().NoError(err)
	credit, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), lo.FromPtr(settled.RefundDestinationID))
	s.Require().NoError(err)
	s.Equal(types.TransactionReasonCreditNote, credit.TransactionReason)
	s.Equal(types.WalletTxSourceTypeInvoice, credit.SourceType)
	s.Equal(s.testData.invoice.ID, credit.SourceID)
}
