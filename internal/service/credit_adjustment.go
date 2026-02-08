package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreditAdjustmentService is a type alias for the interface
type CreditAdjustmentService = interfaces.CreditAdjustmentService

// creditAdjustmentService implements the CreditAdjustmentService interface
type creditAdjustmentService struct {
	ServiceParams
}

// NewCreditAdjustmentService creates a new credit adjustment service
func NewCreditAdjustmentService(
	params ServiceParams,
) CreditAdjustmentService {
	return &creditAdjustmentService{
		ServiceParams: params,
	}
}

// CalculateCreditAdjustments calculates how much amount to apply from prepaid wallets to invoice line items.
//
// The basic idea is simple: we take all the money available in wallets, put it in a pool, then
// apply it to line items one by one until we run out. We only apply amounts to usage-based line
// items (not one-time charges), and we apply them to the amount after discounts.
//
// Here's how it works:
//
// First, we gather up all the wallet balances into one big pool. As we apply amounts, we track
// which wallets contributed what, so we can debit them later. We consume wallets in order
// (first wallet first, then second, etc.) until we've covered the line item or run out of available amount.
//
// For each line item, we:
//   - Skip it if it's not a usage item (only usage items get amounts applied)
//   - Calculate how much we need: the line item amount minus any discounts
//   - Figure out how much we can apply (can't exceed what's in the pool or what's needed)
//   - Take money from wallets one by one until we've covered it
//   - Round everything to the right currency precision (2 decimals for USD, 0 for JPY, etc.)
//   - Update the line item with how much was applied
//   - Subtract what we used from the pool
//
// At the end, we return a map showing how much to debit from each wallet. The actual debiting
// happens later in a database transaction.
//
// Why this approach? It's simpler than trying to distribute amounts proportionally, and the
// end result (total amount applied) is the same. We use a pool so we don't have to think
// about which wallet to use for which line item - we just grab from the pool as needed.
//
// NOTE: This is exported for testing only. In production, use ApplyCreditsToInvoice() which
// handles the full workflow including database operations.
func (s *creditAdjustmentService) CalculateCreditAdjustments(inv *invoice.Invoice, wallets []*wallet.Wallet) (map[string]decimal.Decimal, error) {
	amountsToDebitFromWallets := make(map[string]decimal.Decimal)

	// Nothing to do if there are no wallets
	if len(wallets) == 0 {
		return nil, nil
	}

	// Keep track of each wallet's balance as we use up amounts from them
	walletBalances := make(map[string]decimal.Decimal)
	for _, w := range wallets {
		walletBalances[w.ID] = w.Balance
	}

	// Add up all the wallet balances to create our available amount pool
	remainingAmountAvailable := decimal.Zero
	for _, w := range wallets {
		remainingAmountAvailable = remainingAmountAvailable.Add(w.Balance)
	}

	// If there's no amount available, we're done
	if remainingAmountAvailable.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}

	// We'll consume wallets in order (first wallet first, then second, etc.)
	currentWalletIdx := 0

	// Go through each line item and apply amounts
	for _, lineItem := range inv.LineItems {
		// Only usage-based items get amounts applied (one-time charges don't)
		if lineItem.PriceType == nil || lo.FromPtr(lineItem.PriceType) != string(types.PRICE_TYPE_USAGE) {
			lineItem.PrepaidCreditsApplied = decimal.Zero
			continue
		}

		// Figure out how much this line item actually costs after discounts
		// We apply amounts to the net amount, not the gross amount
		lineItemAmountAfterDiscounts := lineItem.Amount.Sub(lineItem.LineItemDiscount).Sub(lineItem.InvoiceLevelDiscount)

		// If it's already free (or negative), skip it
		if lineItemAmountAfterDiscounts.LessThanOrEqual(decimal.Zero) {
			lineItem.PrepaidCreditsApplied = decimal.Zero
			continue
		}

		// How much can we apply to this line item? Can't exceed what's available or what's needed
		maxAmountToApply := decimal.Min(remainingAmountAvailable, lineItemAmountAfterDiscounts)
		amountAppliedToLineItem := decimal.Zero

		// Take money from wallets one by one until we've covered this line item or run out
		for currentWalletIdx < len(wallets) && amountAppliedToLineItem.LessThan(maxAmountToApply) {
			currentWallet := wallets[currentWalletIdx]
			currentWalletBalance := walletBalances[currentWallet.ID]

			// Skip wallets that are already empty
			if currentWalletBalance.LessThanOrEqual(decimal.Zero) {
				currentWalletIdx++
				continue
			}

			// Calculate how much more we still need for this line item
			amountStillNeeded := maxAmountToApply.Sub(amountAppliedToLineItem)

			// Take as much as we can from this wallet (either all of it or what we need, whichever is less)
			rawAmount := decimal.Min(currentWalletBalance, amountStillNeeded)
			roundedAmountFromWallet := decimal.Min(types.RoundToCurrencyPrecision(rawAmount, inv.Currency), rawAmount)

			// Avoid hang when raw amount is positive but rounds to zero (e.g. 0.001 in USD)
			if roundedAmountFromWallet.IsZero() && rawAmount.GreaterThan(decimal.Zero) && currentWalletBalance.GreaterThan(decimal.Zero) {
				walletBalances[currentWallet.ID] = decimal.Zero
				currentWalletIdx++
				continue
			}

			if roundedAmountFromWallet.GreaterThan(decimal.Zero) {
				// Remember how much we're taking from this wallet (we'll debit it later)
				amountsToDebitFromWallets[currentWallet.ID] = amountsToDebitFromWallets[currentWallet.ID].Add(roundedAmountFromWallet)
				// Update our tracking of this wallet's balance
				walletBalances[currentWallet.ID] = currentWalletBalance.Sub(roundedAmountFromWallet)
				// Keep track of how much we've actually applied
				amountAppliedToLineItem = amountAppliedToLineItem.Add(roundedAmountFromWallet)
			}

			// Move to the next wallet if this one is empty or we couldn't take anything
			if walletBalances[currentWallet.ID].LessThanOrEqual(decimal.Zero) {
				currentWalletIdx++
			}
		}

		lineItem.PrepaidCreditsApplied = amountAppliedToLineItem

		// Subtract what we used from the pool (use unrounded value to keep precision)
		remainingAmountAvailable = remainingAmountAvailable.Sub(amountAppliedToLineItem)
	}

	// Return a map showing how much to take from each wallet
	return amountsToDebitFromWallets, nil
}

// ApplyCreditsToInvoice applies wallet amounts to an invoice.
//
// This method does the work in two phases to keep database transactions short. We do all the
// math first (outside the transaction), then write everything to the database in one go (inside
// the transaction). This way, we're not holding a database lock while we're doing calculations.
//
// Phase 1: Do all the calculations (outside transaction)
//
//	First, we get all the prepaid wallets for this customer. Only prepaid wallets can be used
//	for applying amounts - postpaid wallets are for payments, not amount applications.
//
//	Then we figure out how much amount to apply to each line item. This is where all the math
//	happens - we look at each line item, see how much it costs after discounts, and apply
//	amounts from the wallet pool. All of this happens in memory, so it's fast.
//
//	Finally, we build a lookup map so we can quickly find wallet details when we need them
//	during the transaction.
//
// Phase 2: Write everything to the database (inside transaction)
//
// Phase 1 (Outside Transaction): Calculation
//   - Retrieves eligible prepaid wallets for credit adjustment
//   - Calls calculateCreditAdjustments() which:
//   - Filters usage-based line items
//   - Calculates adjusted amount per line item (Amount - LineItemDiscount)
//   - Iterates wallets to determine how much credit to apply from each wallet
//   - Directly updates lineItem.PrepaidCreditsApplied in memory (not yet persisted)
//   - Returns a map of wallet debits (walletID -> amount to debit)
//
// Phase 2 (Inside Transaction): Database Writes Only
//   - Executes all wallet debits sequentially
//   - Updates line items in database with PrepaidCreditsApplied values
//   - Sets inv.TotalPrepaidCreditsApplied in memory and persists to the database
//
// IMPORTANT NOTES:
//   - This method ONLY updates PrepaidCreditsApplied in the database for line items
//   - The invoice's TotalPrepaidCreditsApplied is set in memory but NOT persisted to the database
//   - It is the CALLER'S RESPONSIBILITY to update the invoice in the database if needed
//   - This design allows callers to batch invoice updates with other operations if required
func (s *creditAdjustmentService) ApplyCreditsToInvoice(ctx context.Context, inv *invoice.Invoice) (*dto.CreditAdjustmentResult, error) {

	if len(inv.LineItems) == 0 {
		s.Logger.Infow("no line items to apply amounts to, returning zero result", "invoice_id", inv.ID)
		return &dto.CreditAdjustmentResult{
			TotalPrepaidCreditsApplied: decimal.Zero,
			Currency:                   inv.Currency,
		}, nil
	}

	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	// Get all the prepaid wallets we can use for this customer
	// Only prepaid wallets work here - postpaid wallets are for payments, not amount applications
	wallets, err := walletPaymentService.GetWalletsForCreditAdjustment(ctx, inv.CustomerID, inv.Currency)
	if err != nil {
		return nil, err
	}

	if len(wallets) == 0 {
		s.Logger.Infow("no wallets available for amount application, returning zero result", "invoice_id", inv.ID)
		return &dto.CreditAdjustmentResult{
			TotalPrepaidCreditsApplied: decimal.Zero,
			Currency:                   inv.Currency,
		}, nil
	}

	// Step 2: Calculate all credit adjustments (OUTSIDE TRANSACTION)
	// This method:
	// - Filters usage-based line items only
	// - Calculates adjusted amount per line item (Amount - LineItemDiscount)
	// - Determines how much credit to apply from each wallet
	// - Directly modifies lineItem.PrepaidCreditsApplied in memory (NOT persisted yet)
	// - Returns a map of wallet debits (walletID -> total amount to debit)
	amountsToDebitFromWallets, err := s.CalculateCreditAdjustments(inv, wallets)
	if err != nil {
		return nil, err
	}

	// If no amounts were calculated to apply, return zero result
	if len(amountsToDebitFromWallets) == 0 {
		return &dto.CreditAdjustmentResult{
			TotalPrepaidCreditsApplied: decimal.Zero,
			Currency:                   inv.Currency,
		}, nil
	}

	walletService := NewWalletService(s.ServiceParams)

	// Build a quick lookup map so we can find wallet details fast during the transaction
	walletLookupMap := make(map[string]*wallet.Wallet)
	for _, w := range wallets {
		walletLookupMap[w.ID] = w
	}

	var result *dto.CreditAdjustmentResult
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Take money from each wallet that contributed amounts
		for walletID, amountToDebit := range amountsToDebitFromWallets {
			walletToDebit, exists := walletLookupMap[walletID]
			if !exists {
				s.Logger.Warnw("wallet not found for debit",
					"wallet_id", walletID,
					"invoice_id", inv.ID)
				continue
			}

			// Generate unique idempotency key for this wallet operation
			generator := idempotency.NewGenerator()
			idempotencyKey := generator.GenerateKey(idempotency.ScopeWalletCreditAdjustment, map[string]interface{}{
				"invoice_id": inv.ID,
				"wallet_id":  walletID,
				"ts":         time.Now().UnixNano(),
			})

			walletDebitOperation := &wallet.WalletOperation{
				WalletID:          walletID,
				Type:              types.TransactionTypeDebit,
				Amount:            amountToDebit,
				ReferenceType:     types.WalletTxReferenceTypeInvoice,
				ReferenceID:       inv.ID,
				Description:       fmt.Sprintf("Amount applied as credit adjustment to invoice %s from wallet %s", inv.ID, walletID),
				TransactionReason: types.TransactionReasonCreditAdjustment,
				IdempotencyKey:    idempotencyKey,
				Metadata: types.Metadata{
					"invoice_id":      inv.ID,
					"customer_id":     inv.CustomerID,
					"wallet_type":     string(walletToDebit.WalletType),
					"adjustment_type": "amount_application",
				},
			}

			if err := walletService.DebitWallet(ctx, walletDebitOperation); err != nil {
				return err
			}
		}

		// Save how much was applied to each line item
		// We calculated these values earlier, now we're just saving them to the database
		totalAmountApplied := decimal.Zero
		for _, lineItem := range inv.LineItems {
			if lineItem.PrepaidCreditsApplied.GreaterThan(decimal.Zero) {
				totalAmountApplied = totalAmountApplied.Add(lineItem.PrepaidCreditsApplied)
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
		inv.TotalPrepaidCreditsApplied = totalAmountApplied

		result = &dto.CreditAdjustmentResult{
			TotalPrepaidCreditsApplied: totalAmountApplied,
			Currency:                   inv.Currency,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
