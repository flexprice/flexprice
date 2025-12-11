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
	TotalCreditsApplied decimal.Decimal
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

// ApplyCreditsToInvoice applies available wallet credits to invoice line items
func (s *CreditAdjustmentService) ApplyCreditsToInvoice(ctx context.Context, inv *invoice.Invoice) (*CreditAdjustmentResult, error) {
	// Get eligible wallets for the customer
	wallets, err := s.getEligibleWallets(ctx, inv.CustomerID, inv.Currency)
	if err != nil {
		return nil, err
	}

	if len(wallets) == 0 {
		// Return zero result when no wallets available
		return &CreditAdjustmentResult{
			TotalCreditsApplied: decimal.Zero,
			Currency:            inv.Currency,
		}, nil
	}

	// Create wallet service
	walletService := NewWalletService(s.ServiceParams)

	// Transactional workflow begins here
	var result *CreditAdjustmentResult
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		totalApplied := decimal.Zero
		walletIndex := 0

		// Apply credits to each usage line item
		for _, lineItem := range inv.LineItems {
			// Only apply to usage-based line items
			if lineItem.PriceType == nil || lo.FromPtr(lineItem.PriceType) != string(types.PRICE_TYPE_USAGE) {
				continue
			}

			if lineItem.Amount.LessThanOrEqual(decimal.Zero) || walletIndex >= len(wallets) {
				continue
			}

			// Apply from wallets until line item is covered or wallets exhausted
			amountToApply := decimal.Zero
			for walletIndex < len(wallets) && amountToApply.LessThan(lineItem.Amount) {
				selectedWallet := wallets[walletIndex]
				remaining := lineItem.Amount.Sub(amountToApply)

				if selectedWallet.Balance.GreaterThan(decimal.Zero) {
					// Take minimum of what's needed and what's available
					fromThisWallet := decimal.Min(selectedWallet.Balance, remaining)
					amountToApply = amountToApply.Add(fromThisWallet)

					// Create wallet operation for this debit
					operation := &wallet.WalletOperation{
						WalletID:          selectedWallet.ID,
						Type:              types.TransactionTypeDebit,
						Amount:            fromThisWallet,
						ReferenceType:     types.WalletTxReferenceTypeExternal,
						ReferenceID:       inv.ID,
						Description:       fmt.Sprintf("Credit adjustment for invoice %s line item %s", inv.ID, lineItem.ID),
						TransactionReason: types.TransactionReasonCreditAdjustment,
						Metadata: types.Metadata{
							"invoice_id":      inv.ID,
							"line_item_id":    lineItem.ID,
							"customer_id":     inv.CustomerID,
							"wallet_type":     string(selectedWallet.WalletType),
							"adjustment_type": "credit_application",
						},
					}

					// Debit wallet using proper wallet service
					if err := walletService.DebitWallet(ctx, operation); err != nil {
						return err
					}

					// Update local wallet balance for next iteration
					selectedWallet.Balance = selectedWallet.Balance.Sub(fromThisWallet)
				}

				// Move to next wallet if current one is exhausted
				if selectedWallet.Balance.LessThanOrEqual(decimal.Zero) {
					walletIndex++
				}
			}

			// Apply the amount to the line item
			if amountToApply.GreaterThan(decimal.Zero) {
				lineItem.CreditsApplied = amountToApply
				totalApplied = totalApplied.Add(amountToApply)
			}
		}

		// Update invoice total
		inv.TotalCreditsApplied = totalApplied

		// Update line items in database if credits were applied
		if totalApplied.GreaterThan(decimal.Zero) {
			for _, item := range inv.LineItems {
				if item.CreditsApplied.GreaterThan(decimal.Zero) {
					if err := s.InvoiceRepo.UpdateLineItem(ctx, item); err != nil {
						return err
					}
				}
			}
		}

		// Store result in closure variable
		result = &CreditAdjustmentResult{
			TotalCreditsApplied: totalApplied,
			Currency:            inv.Currency,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return the result from the transaction
	return result, nil
}

// getEligibleWallets retrieves wallets that can be used for credit adjustments
func (s *CreditAdjustmentService) getEligibleWallets(ctx context.Context, customerID, currency string) ([]*wallet.Wallet, error) {
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Filter: active, matching currency, positive balance
	var eligible []*wallet.Wallet
	for _, w := range wallets {
		if w.WalletStatus == types.WalletStatusActive &&
			w.Currency == currency &&
			w.Balance.GreaterThan(decimal.Zero) {
			eligible = append(eligible, w)
		}
	}

	return eligible, nil
}
