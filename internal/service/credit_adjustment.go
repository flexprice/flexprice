package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreditAdjustmentResult holds the result of applying credit adjustments to an invoice
type CreditAdjustmentResult struct {
	TotalPrepaidApplied decimal.Decimal
	Currency            string
}

// CreditAdjustmentService handles applying wallet credits as invoice adjustments
type CreditAdjustmentService struct {
	ServiceParams
}

// NewCreditAdjustmentService creates a new credit adjustment service
func NewCreditAdjustmentService(
	params ServiceParams,
) *CreditAdjustmentService {
	return &CreditAdjustmentService{
		ServiceParams: params,
	}
}

// NOTE: This method is exported ONLY for testing purposes. Do not use it directly in production code.
// Use ApplyCreditsToInvoice() instead, which handles the full workflow including database operations.
func (s *CreditAdjustmentService) CalculateCreditAdjustments(inv *invoice.Invoice, wallets []*wallet.Wallet) (map[string]decimal.Decimal, error) {
	walletDebits := make(map[string]decimal.Decimal)

	// Track remaining balances as we use wallets across multiple line items
	walletBalances := make(map[string]decimal.Decimal)
	for _, w := range wallets {
		walletBalances[w.ID] = w.Balance
	}

	for _, lineItem := range inv.LineItems {
		// Only usage line items get credits
		if lineItem.PriceType == nil || lo.FromPtr(lineItem.PriceType) != string(types.PRICE_TYPE_USAGE) {
			continue
		}

		// We adjust the discounted amount, not the original amount
		adjustedAmount := lineItem.Amount.Sub(lineItem.LineItemDiscount)

		if adjustedAmount.LessThanOrEqual(decimal.Zero) {
			continue
		}

		amountToApply := decimal.Zero
		walletIndex := 0

		for walletIndex < len(wallets) && amountToApply.LessThan(adjustedAmount) {
			selectedWallet := wallets[walletIndex]
			walletBalance := walletBalances[selectedWallet.ID]

			// Skip wallets with zero balance
			if walletBalance.LessThanOrEqual(decimal.Zero) {
				walletIndex++
				continue
			}

			// Calculate amount needed from this wallet and round once
			needed := adjustedAmount.Sub(amountToApply)
			fromThisWallet := decimal.Min(walletBalance, needed)
			roundedFromThisWallet := types.RoundToCurrencyPrecision(fromThisWallet, inv.Currency)

			// Apply credits if rounded amount is positive
			if roundedFromThisWallet.GreaterThan(decimal.Zero) {

				amountToApply = amountToApply.Add(roundedFromThisWallet)

				walletDebits[selectedWallet.ID] = walletDebits[selectedWallet.ID].Add(roundedFromThisWallet)

				walletBalances[selectedWallet.ID] = walletBalance.Sub(roundedFromThisWallet)
			}

			// Move to next wallet if current one is exhausted or rounded amount is zero
			if walletBalances[selectedWallet.ID].LessThanOrEqual(decimal.Zero) || roundedFromThisWallet.LessThanOrEqual(decimal.Zero) {
				walletIndex++
			}
		}

		// amountToApply is already the sum of rounded wallet contributions, so use it directly
		lineItem.PrepaidCreditsApplied = amountToApply
	}

	return walletDebits, nil
}

// ApplyCreditsToInvoice applies wallet credits to invoice line items.
//
// HOW IT WORKS:
// This method follows a two-phase approach to minimize transaction time and improve maintainability:
//
// Phase 1 (Outside Transaction): Calculation
//   - Retrieves eligible prepaid wallets for credit adjustment
//   - Calls calculateCreditAdjustments() which:
//   - Filters usage-based line items
//   - Calculates adjusted amount per line item (Amount - LineItemDiscount - InvoiceLevelDiscount)
//   - Iterates wallets to determine how much credit to apply from each wallet
//   - Directly updates lineItem.PrepaidCreditsApplied in memory (not yet persisted)
//   - Returns a map of wallet debits (walletID -> amount to debit)
//
// Phase 2 (Inside Transaction): Database Writes Only
//   - Executes all wallet debits sequentially
//   - Updates line items in database with PrepaidCreditsApplied values
//   - Sets inv.TotalPrepaidApplied in memory (for return value)
//
// IMPORTANT NOTES:
//   - This method ONLY updates PrepaidCreditsApplied in the database for line items
//   - The invoice's TotalPrepaidApplied is set in memory but NOT persisted to the database
//   - It is the CALLER'S RESPONSIBILITY to update the invoice in the database if needed
//   - This design allows callers to batch invoice updates with other operations if required
func (s *CreditAdjustmentService) ApplyCreditsToInvoice(ctx context.Context, inv *invoice.Invoice) (*CreditAdjustmentResult, error) {

	if len(inv.LineItems) == 0 {
		s.Logger.Infow("no line items to apply credits to, returning zero result", "invoice_id", inv.ID)
		return &CreditAdjustmentResult{
			TotalPrepaidApplied: decimal.Zero,
			Currency:            inv.Currency,
		}, nil
	}

	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	// Step 1: Get eligible prepaid wallets for credit adjustment
	// Only prepaid wallets can be used for credit adjustments (postpaid wallets are for payments)
	wallets, err := walletPaymentService.GetWalletsForCreditAdjustment(ctx, inv.CustomerID, inv.Currency)
	if err != nil {
		return nil, err
	}

	if len(wallets) == 0 {
		s.Logger.Infow("no wallets available for credit adjustment, returning zero result", "invoice_id", inv.ID)
		return &CreditAdjustmentResult{
			TotalPrepaidApplied: decimal.Zero,
			Currency:            inv.Currency,
		}, nil
	}

	// Step 2: Calculate all credit adjustments (OUTSIDE TRANSACTION)
	// This method:
	// - Filters usage-based line items only
	// - Calculates adjusted amount per line item (Amount - LineItemDiscount - InvoiceLevelDiscount)
	// - Determines how much credit to apply from each wallet
	// - Directly modifies lineItem.PrepaidCreditsApplied in memory (NOT persisted yet)
	// - Returns a map of wallet debits (walletID -> total amount to debit)
	walletDebits, err := s.CalculateCreditAdjustments(inv, wallets)
	if err != nil {
		return nil, err
	}

	// If no credits were calculated to apply, return zero result
	if len(walletDebits) == 0 {
		return &CreditAdjustmentResult{
			TotalPrepaidApplied: decimal.Zero,
			Currency:            inv.Currency,
		}, nil
	}

	walletService := NewWalletService(s.ServiceParams)

	// Create wallet lookup map for O(1) access during transaction
	// This avoids linear search when looking up wallet details for each debit
	walletMap := make(map[string]*wallet.Wallet)
	for _, w := range wallets {
		walletMap[w.ID] = w
	}

	// Now do all the DB writes in a transaction
	var result *CreditAdjustmentResult
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Step 1: Execute all wallet debits
		// For each wallet that was used, debit the calculated amount
		// This creates wallet transaction records and updates wallet balances in the database
		for walletID, debitAmount := range walletDebits {
			selectedWallet, exists := walletMap[walletID]
			if !exists {
				s.Logger.Warnw("wallet not found for debit",
					"wallet_id", walletID,
					"invoice_id", inv.ID)
				continue
			}

			operation := &wallet.WalletOperation{
				WalletID:          walletID,
				Type:              types.TransactionTypeDebit,
				Amount:            debitAmount,
				ReferenceType:     types.WalletTxReferenceTypeRequest,
				ReferenceID:       inv.ID,
				Description:       fmt.Sprintf("Credit adjustment for invoice %s", inv.ID),
				TransactionReason: types.TransactionReasonCreditAdjustment,
				Metadata: types.Metadata{
					"invoice_id":      inv.ID,
					"customer_id":     inv.CustomerID,
					"wallet_type":     string(selectedWallet.WalletType),
					"adjustment_type": "credit_application",
				},
			}

			if err := walletService.DebitWallet(ctx, operation); err != nil {
				return err
			}
		}

		// Step 2: Update line items in database with PrepaidCreditsApplied values
		// The PrepaidCreditsApplied values were calculated in Phase 1 and are now persisted here
		// We also calculate totalApplied as we iterate (sum of all PrepaidCreditsApplied)
		totalApplied := decimal.Zero
		for _, lineItem := range inv.LineItems {
			if lineItem.PrepaidCreditsApplied.GreaterThan(decimal.Zero) {
				totalApplied = totalApplied.Add(lineItem.PrepaidCreditsApplied)
				if err := s.InvoiceRepo.UpdateLineItem(ctx, lineItem); err != nil {
					return err
				}
			}
		}

		// Step 3: Set inv.TotalPrepaidApplied in memory (NOT persisted to database)
		// IMPORTANT: This value is set in memory for the return value, but it is NOT
		// persisted to the database. The caller is responsible for updating the invoice
		// in the database if they need to persist TotalPrepaidApplied.
		// This design allows callers to batch invoice updates with other operations.
		inv.TotalPrepaidCreditsApplied = totalApplied

		result = &CreditAdjustmentResult{
			TotalPrepaidApplied: totalApplied,
			Currency:            inv.Currency,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
