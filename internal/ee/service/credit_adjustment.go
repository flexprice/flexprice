package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

// spreadPrepaidCreditsAcrossLineItems re-derives each usage line item's PrepaidCreditsApplied from the
// invoice-level authority inv.TotalPrepaidCreditsApplied. It performs NO wallet movement. Usage line items
// receive credit up to their ceiling (amount - line_discount - invoice_level_discount), in list order;
// non-usage lines get zero. Returns the total actually placed on line items (may be less than the
// authority when ceilings cannot absorb it all).
func spreadPrepaidCreditsAcrossLineItems(inv *invoice.Invoice) decimal.Decimal {
	remaining := inv.TotalPrepaidCreditsApplied
	if remaining.IsNegative() {
		remaining = decimal.Zero
	}
	totalApplied := decimal.Zero
	for _, lineItem := range inv.LineItems {
		if lineItem.PriceType == nil || lo.FromPtr(lineItem.PriceType) != string(types.PRICE_TYPE_USAGE) {
			lineItem.PrepaidCreditsApplied = decimal.Zero
			continue
		}
		ceiling := lineItem.Amount.Sub(lineItem.LineItemDiscount).Sub(lineItem.InvoiceLevelDiscount)
		if ceiling.LessThanOrEqual(decimal.Zero) || remaining.LessThanOrEqual(decimal.Zero) {
			lineItem.PrepaidCreditsApplied = decimal.Zero
			continue
		}
		applied := decimal.Min(remaining, ceiling)
		lineItem.PrepaidCreditsApplied = applied
		remaining = remaining.Sub(applied)
		totalApplied = totalApplied.Add(applied)
	}
	return totalApplied
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
		// Only usage-based items get amounts applied (one-time charges don't). Non-usage lines are
		// left untouched here (not reset) - resetting is the caller's job (see spreadPrepaidCreditsAcrossLineItems).
		if lineItem.PriceType == nil || lo.FromPtr(lineItem.PriceType) != string(types.PRICE_TYPE_USAGE) {
			continue
		}

		// Figure out how much this line item actually costs after discounts
		// We apply amounts to the net amount, not the gross amount
		lineItemAmountAfterDiscounts := lineItem.Amount.Sub(lineItem.LineItemDiscount).Sub(lineItem.InvoiceLevelDiscount)

		// Additive floor: existing PrepaidCreditsApplied is only added to, never overwritten.
		// The room left to apply is the ceiling minus what's already been applied to this line.
		remainingCeiling := lineItemAmountAfterDiscounts.Sub(lineItem.PrepaidCreditsApplied)

		// If there's no room left (already free, negative, or fully applied), skip it
		if remainingCeiling.LessThanOrEqual(decimal.Zero) {
			continue
		}

		// How much can we apply to this line item? Can't exceed what's available or what's needed
		maxAmountToApply := decimal.Min(remainingAmountAvailable, remainingCeiling)
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

		// Additive: add the newly applied amount on top of what was already on the line.
		lineItem.PrepaidCreditsApplied = lineItem.PrepaidCreditsApplied.Add(amountAppliedToLineItem)

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
		s.Logger.Info(ctx, "no line items to apply amounts to, returning zero result", "invoice_id", inv.ID)
		return &dto.CreditAdjustmentResult{
			TotalPrepaidCreditsApplied: inv.TotalPrepaidCreditsApplied,
			Currency:                   inv.Currency,
		}, nil
	}

	// Spread the already-applied invoice-level authority onto current line items first, so
	// CalculateCreditAdjustments' additive floor (line.PrepaidCreditsApplied) reflects prior state
	// correctly even after a recompute rebuilt the line items (which zeroes the per-line field but
	// never the invoice-level authority).
	spreadPrepaidCreditsAcrossLineItems(inv)

	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	// Get all the prepaid wallets we can use for this customer
	// Only prepaid wallets work here - postpaid wallets are for payments, not amount applications
	wallets, err := walletPaymentService.GetWalletsForCreditAdjustment(ctx, inv.CustomerID, inv.Currency)
	if err != nil {
		return nil, err
	}

	// Step 2: Calculate all credit adjustments (OUTSIDE TRANSACTION)
	// This method:
	// - Filters usage-based line items only
	// - Calculates adjusted amount per line item (Amount - LineItemDiscount)
	// - Determines how much credit to apply from each wallet
	// - Directly modifies lineItem.PrepaidCreditsApplied in memory (NOT persisted yet)
	// - Returns a map of wallet debits (walletID -> total amount to debit)
	var amountsToDebitFromWallets map[string]decimal.Decimal
	if len(wallets) > 0 {
		amountsToDebitFromWallets, err = s.CalculateCreditAdjustments(inv, wallets)
		if err != nil {
			return nil, err
		}
	}

	walletService := NewWalletService(s.ServiceParams)

	// Build a quick lookup map so we can find wallet details fast during the transaction
	walletLookupMap := make(map[string]*wallet.Wallet)
	for _, w := range wallets {
		walletLookupMap[w.ID] = w
	}

	// Always enter the transaction and persist line items - even when there's no NEW credit to debit,
	// the spread above may have changed per-line PrepaidCreditsApplied values (e.g. after a recompute
	// shrank a line's ceiling below what was previously applied), and those must land in the DB
	// regardless of whether this call found additional wallet balance to apply.
	var result *dto.CreditAdjustmentResult
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Take money from each wallet that contributed amounts (no-op if amountsToDebitFromWallets is nil/empty)
		for walletID, amountToDebit := range amountsToDebitFromWallets {
			walletToDebit, exists := walletLookupMap[walletID]
			if !exists {
				s.Logger.Info(ctx, "wallet not found for debit",
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
			totalAmountApplied = totalAmountApplied.Add(lineItem.PrepaidCreditsApplied)
			if err := s.InvoiceLineItemRepo.Update(ctx, lineItem); err != nil {
				return err
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

// applyExpiringCreditToInvoice applies up to tx.CreditsAvailable of credit from ONE specific about-to-
// expire wallet transaction to an invoice, spreading it across usage line items and debiting ONLY that
// transaction's grant (via ParentCreditTxID). Unlike ApplyCreditsToInvoice, this never touches
// FindEligibleCredits or wallet pooling: the source and amount are already fully known, and generic FIFO
// selection would exclude this credit anyway (FindEligibleCredits excludes any credit whose expiry has
// already passed - exactly the population pre-expiry processes).
//
// The invoice-level total is set in memory only; the CALLER must persist the invoice's own Total/AmountDue
// fields (this function DOES persist every line item's PrepaidCreditsApplied itself, unconditionally,
// whether or not a new debit was made - see the Task 4 lesson above for why that matters).
func (s *creditAdjustmentService) applyExpiringCreditToInvoice(ctx context.Context, inv *invoice.Invoice, tx *wallet.Transaction) (*dto.CreditAdjustmentResult, error) {
	if len(inv.LineItems) == 0 {
		return &dto.CreditAdjustmentResult{TotalPrepaidCreditsApplied: inv.TotalPrepaidCreditsApplied, Currency: inv.Currency}, nil
	}

	// Full ceiling across all usage lines (amount - discounts), independent of what's already applied.
	totalCeiling := decimal.Zero
	for _, li := range inv.LineItems {
		if li.PriceType == nil || lo.FromPtr(li.PriceType) != string(types.PRICE_TYPE_USAGE) {
			continue
		}
		ceiling := li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount)
		if ceiling.IsPositive() {
			totalCeiling = totalCeiling.Add(ceiling)
		}
	}
	remainingCeiling := totalCeiling.Sub(inv.TotalPrepaidCreditsApplied)
	if remainingCeiling.IsNegative() {
		remainingCeiling = decimal.Zero
	}

	rawAmount := decimal.Min(remainingCeiling, tx.CreditsAvailable)
	amountToApply := decimal.Min(types.RoundToCurrencyPrecision(rawAmount, inv.Currency), rawAmount)
	if amountToApply.IsNegative() {
		amountToApply = decimal.Zero
	}

	// Always enter the transaction and persist every line item - even when there's no NEW credit to debit
	// (amountToApply may be zero), spreadPrepaidCreditsAcrossLineItems below may still change per-line
	// values relative to what's currently in the DB (e.g. after a recompute reset them), and those must be
	// persisted regardless of whether a debit happens. See the Task 4 lesson above.
	var result *dto.CreditAdjustmentResult
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		if amountToApply.GreaterThan(decimal.Zero) {
			generator := idempotency.NewGenerator()
			// Deterministic per (invoice, source grant): naturally dedupes activity retries, and is distinct
			// from ExpireCredits' remainder-expiry key (tx.ID alone) so the two debits never collide on the
			// unique (tenant, environment, idempotency_key) index.
			idempotencyKey := generator.GenerateKey(idempotency.ScopeWalletCreditAdjustment, map[string]interface{}{
				"invoice_id":   inv.ID,
				"source_tx_id": tx.ID,
			})

			walletService := NewWalletService(s.ServiceParams)
			walletDebitOperation := &wallet.WalletOperation{
				WalletID:          tx.WalletID,
				ParentCreditTxID:  tx.ID,
				Type:              types.TransactionTypeDebit,
				Amount:            amountToApply,
				ReferenceType:     types.WalletTxReferenceTypeInvoice,
				ReferenceID:       inv.ID,
				Description:       fmt.Sprintf("Pre-expiry credit adjustment applied to invoice %s from transaction %s", inv.ID, tx.ID),
				TransactionReason: types.TransactionReasonCreditAdjustment,
				IdempotencyKey:    idempotencyKey,
				Metadata: types.Metadata{
					"invoice_id":      inv.ID,
					"customer_id":     inv.CustomerID,
					"source_tx_id":    tx.ID,
					"adjustment_type": "pre_expiry_credit_adjustment",
				},
			}
			if err := walletService.DebitWallet(ctx, walletDebitOperation); err != nil {
				return err
			}

			inv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied.Add(amountToApply)
		}

		totalApplied := spreadPrepaidCreditsAcrossLineItems(inv)

		// Persist re-derived per-line values. totalApplied may be less than the prior authority when
		// ceilings shrank (e.g. after recompute) — keep the invoice-level total in sync with lines.
		for _, li := range inv.LineItems {
			if err := s.InvoiceLineItemRepo.Update(ctx, li); err != nil {
				return err
			}
		}
		inv.TotalPrepaidCreditsApplied = totalApplied

		result = &dto.CreditAdjustmentResult{TotalPrepaidCreditsApplied: totalApplied, Currency: inv.Currency}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ConsumeExpiringCreditIntoInvoices best-effort applies an about-to-expire credit transaction against the
// customer's active standalone/parent subscription draft invoices before the remaining credit is expired.
// Returns the total amount consumed into invoices. Per-invoice failures are logged and skipped so that
// credit expiry is never blocked.
func (s *creditAdjustmentService) ConsumeExpiringCreditIntoInvoices(ctx context.Context, tx *wallet.Transaction) (decimal.Decimal, error) {
	consumed := decimal.Zero
	if tx.ExpiryDate == nil {
		return consumed, nil
	}

	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.CustomerID = tx.CustomerID
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
	subFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeStandalone, types.SubscriptionTypeParent}
	subs, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return consumed, err
	}

	for _, sub := range subs {
		f := types.NewNoLimitInvoiceFilter()
		f.SubscriptionID = sub.ID
		f.InvoiceStatus = []types.InvoiceStatus{
			types.InvoiceStatusDraft,
		}
		f.Currency = tx.Currency // wallets are per-currency
		invoices, err := s.InvoiceRepo.List(ctx, f)
		if err != nil {
			s.Logger.Error(ctx, "pre_expiry_list_invoices_failed", "subscription_id", sub.ID, "error", err)
			continue
		}

		for _, inv := range invoices {
			// Re-read the tx: prior invoices in this pass may have drawn down the credit.
			latestTx, err := s.WalletRepo.GetTransactionByID(ctx, tx.ID)
			if err != nil {
				s.Logger.Error(ctx, "pre_expiry_reread_transaction_failed", "transaction_id", tx.ID, "error", err)
				continue // best-effort
			}
			if latestTx.CreditsAvailable.LessThanOrEqual(decimal.Zero) {
				return consumed, nil
			}

			applied, err := s.consumeExpiringCreditIntoInvoice(ctx, inv.ID, latestTx)
			if err != nil {
				s.Logger.Error(ctx, "pre_expiry_apply_failed", "invoice_id", inv.ID, "transaction_id", tx.ID, "error", err)
				continue // best-effort
			}
			consumed = consumed.Add(applied)
		}
	}

	return consumed, nil
}

// consumeExpiringCreditIntoInvoice recomputes one draft invoice (usage as of now — ComputeInvoice is called
// with a nil request, no period override; usage naturally reflects "as of now" since there's no future
// event data to aggregate) and applies up to the credit's CreditsAvailable of prepaid credit to it, under a
// per-invoice Redis lock. Returns how much credit was newly applied to this invoice.
func (s *creditAdjustmentService) consumeExpiringCreditIntoInvoice(ctx context.Context, invoiceID string, tx *wallet.Transaction) (decimal.Decimal, error) {
	if s.Locker == nil {
		return decimal.Zero, ierr.NewError("prepaid credit apply lock unavailable").
			WithHint("Redis cache is not available").
			Mark(ierr.ErrServiceUnavailable)
	}
	lockKey := cache.GenerateKey(ctx, cache.PrefixPrepaidCreditApplyLock, invoiceID)
	lock, err := s.Locker.AcquireLock(ctx, lockKey, cache.ExpiryPrepaidCreditApplyLock)
	if err != nil {
		return decimal.Zero, err
	}
	if !lock.AcquiredSuccessfully() {
		s.Logger.Info(ctx, "pre_expiry_lock_rejected", "invoice_id", invoiceID)
		return decimal.Zero, nil // another worker holds it; retried next schedule tick
	}
	defer func() { _ = lock.Release(ctx) }()

	invoiceService := NewInvoiceService(s.ServiceParams)

	// Standard recompute — usage naturally reflects "as of now". No period override. Runs OUTSIDE the
	// transaction (it's expensive/ClickHouse-backed), matching ComputeInvoice's own established pattern
	// of not holding a DB lock during the compute.
	if _, err := invoiceService.ComputeInvoice(ctx, invoiceID, nil); err != nil {
		return decimal.Zero, err
	}

	var newlyApplied decimal.Decimal
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {

		lockedInv, err := s.InvoiceRepo.Get(ctx, invoiceID)
		if err != nil {
			return err
		}
		// A zero-usage recompute may mark the invoice SKIPPED, or another process may have finalized/
		// voided it; only DRAFT invoices are eligible.
		if lockedInv.InvoiceStatus != types.InvoiceStatusDraft {
			return nil
		}
		lineItems, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, invoiceID)
		if err != nil {
			return err
		}
		lockedInv.LineItems = lineItems

		before := lockedInv.TotalPrepaidCreditsApplied
		result, err := s.applyExpiringCreditToInvoice(ctx, lockedInv, tx)
		if err != nil {
			return err
		}

		newTotal := lockedInv.Subtotal.Sub(lockedInv.TotalDiscount).Sub(result.TotalPrepaidCreditsApplied)
		if newTotal.IsNegative() {
			newTotal = decimal.Zero
		}
		lockedInv.Total = newTotal
		lockedInv.AmountDue = lockedInv.Total
		lockedInv.AmountRemaining = lockedInv.Total.Sub(lockedInv.AmountPaid)

		newlyApplied = result.TotalPrepaidCreditsApplied.Sub(before)
		if newlyApplied.IsNegative() {
			newlyApplied = decimal.Zero
		}
		return s.InvoiceRepo.Update(ctx, lockedInv)
	})
	if err != nil {
		return decimal.Zero, err
	}
	return newlyApplied, nil
}
