package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// WalletPaymentStrategy defines the strategy for selecting wallets for payment
type WalletPaymentStrategy string

const (
	// PromotionalFirstStrategy prioritizes promotional wallets before prepaid wallets
	PromotionalFirstStrategy WalletPaymentStrategy = "promotional_first"
	// PrepaidFirstStrategy prioritizes prepaid wallets before promotional wallets
	PrepaidFirstStrategy WalletPaymentStrategy = "prepaid_first"
	// BalanceOptimizedStrategy selects wallets to minimize leftover balances
	BalanceOptimizedStrategy WalletPaymentStrategy = "balance_optimized"
)

// WalletPaymentOptions defines options for wallet payment processing
type WalletPaymentOptions struct {
	// Strategy determines the order in which wallets are selected
	Strategy WalletPaymentStrategy
	// MaxWalletsToUse limits the number of wallets to use (0 means no limit)
	MaxWalletsToUse int
	// AdditionalMetadata to include in payment requests
	AdditionalMetadata types.Metadata
}

// DefaultWalletPaymentOptions returns the default options for wallet payments
func DefaultWalletPaymentOptions() WalletPaymentOptions {
	return WalletPaymentOptions{
		Strategy:           PromotionalFirstStrategy,
		MaxWalletsToUse:    0,
		AdditionalMetadata: types.Metadata{},
	}
}

// WalletPaymentService defines the interface for wallet payment operations
type WalletPaymentService interface {
	// ProcessInvoicePaymentWithWallets attempts to pay an invoice using available wallets
	ProcessInvoicePaymentWithWallets(ctx context.Context, inv *invoice.Invoice, options WalletPaymentOptions) (decimal.Decimal, error)

	// GetWalletsForPayment retrieves and filters wallets suitable for payment
	GetWalletsForPayment(ctx context.Context, customerID string, currency string, options WalletPaymentOptions) ([]*wallet.Wallet, error)
}

type walletPaymentService struct {
	ServiceParams
}

// NewWalletPaymentService creates a new wallet payment service
func NewWalletPaymentService(params ServiceParams) WalletPaymentService {
	return &walletPaymentService{
		ServiceParams: params,
	}
}

// ProcessInvoicePaymentWithWallets attempts to pay an invoice using available wallets
func (s *walletPaymentService) ProcessInvoicePaymentWithWallets(
	ctx context.Context,
	inv *invoice.Invoice,
	options WalletPaymentOptions,
) (decimal.Decimal, error) {
	if inv == nil {
		return decimal.Zero, ierr.NewError("invoice cannot be nil").
			Mark(ierr.ErrInvalidOperation)
	}

	// Check if there's any amount remaining to pay
	if inv.AmountRemaining.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, nil
	}

	// Get wallets suitable for payment
	wallets, err := s.GetWalletsForPayment(ctx, inv.CustomerID, inv.Currency, options)
	if err != nil {
		return decimal.Zero, err
	}

	if len(wallets) == 0 {
		s.Logger.Infow("no suitable wallets found for payment",
			"customer_id", inv.CustomerID,
			"invoice_id", inv.ID,
			"currency", inv.Currency)
		return decimal.Zero, nil
	}

	// Calculate price type breakdown for the invoice using existing line items
	priceTypeAmounts := s.calculatePriceTypeAmounts(inv.LineItems)

	s.Logger.Infow("calculated price type breakdown for invoice",
		"invoice_id", inv.ID,
		"price_type_amounts", priceTypeAmounts)

	// Process payments using wallets with price type restrictions
	amountPaid := s.processWalletPayments(ctx, inv, wallets, priceTypeAmounts, options)

	if !amountPaid.IsZero() {
		remainingAmount := inv.AmountRemaining.Sub(amountPaid)
		s.Logger.Infow("payment processed using wallets",
			"invoice_id", inv.ID,
			"amount_paid", amountPaid,
			"remaining_amount", remainingAmount)
	} else {
		s.Logger.Infow("no payments processed using wallets",
			"invoice_id", inv.ID,
			"amount", inv.AmountRemaining)
	}

	return amountPaid, nil
}

// GetWalletsForPayment retrieves and filters wallets suitable for payment
// Returns wallets prioritized by price type restrictions and sorted by balance (highest first)
func (s *walletPaymentService) GetWalletsForPayment(
	ctx context.Context,
	customerID string,
	currency string,
	options WalletPaymentOptions,
) ([]*wallet.Wallet, error) {
	// Get all wallets for the customer
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Filter active wallets with matching currency and positive balance
	activeWallets := make([]*wallet.Wallet, 0)
	for _, w := range wallets {
		if w.WalletStatus == types.WalletStatusActive &&
			types.IsMatchingCurrency(w.Currency, currency) &&
			w.Balance.GreaterThan(decimal.Zero) {
			activeWallets = append(activeWallets, w)
		}
	}

	if len(activeWallets) == 0 {
		return activeWallets, nil
	}

	// Categorize wallets by their price type restrictions
	usageWallets := make([]*wallet.Wallet, 0)
	fixedWallets := make([]*wallet.Wallet, 0)
	allWallets := make([]*wallet.Wallet, 0)

	for _, w := range activeWallets {
		// If wallet has no allowed price types, treat as ALL
		if len(w.Config.AllowedPriceTypes) == 0 {
			allWallets = append(allWallets, w)
			continue
		}

		// Check wallet's price type restrictions
		hasAll := false
		hasUsage := false
		hasFixed := false

		for _, priceType := range w.Config.AllowedPriceTypes {
			switch priceType {
			case types.WalletConfigPriceTypeAll:
				hasAll = true
			case types.WalletConfigPriceTypeUsage:
				hasUsage = true
			case types.WalletConfigPriceTypeFixed:
				hasFixed = true
			}
		}

		// Categorize wallet based on its restrictions
		if hasAll {
			allWallets = append(allWallets, w)
		} else if hasUsage && hasFixed {
			// Wallet can pay both usage and fixed, treat as ALL
			allWallets = append(allWallets, w)
		} else if hasUsage {
			usageWallets = append(usageWallets, w)
		} else if hasFixed {
			fixedWallets = append(fixedWallets, w)
		}
	}

	// Sort each category by balance (highest first) to minimize wallet usage
	s.sortWalletsByBalanceDesc(usageWallets)
	s.sortWalletsByBalanceDesc(fixedWallets)
	s.sortWalletsByBalanceDesc(allWallets)

	// Return wallets in priority order: specific type wallets first, then ALL wallets
	result := make([]*wallet.Wallet, 0, len(activeWallets))

	// Add usage wallets first (for usage charges)
	result = append(result, usageWallets...)
	// Add fixed wallets next (for fixed charges)
	result = append(result, fixedWallets...)
	// Add ALL wallets last (can pay anything)
	result = append(result, allWallets...)

	s.Logger.Infow("categorized wallets for payment",
		"customer_id", customerID,
		"usage_wallets", len(usageWallets),
		"fixed_wallets", len(fixedWallets),
		"all_wallets", len(allWallets),
		"total_wallets", len(result))

	return result, nil
}

// processWalletPayments processes payments using the provided wallets in order
// Returns the total amount paid across all wallets
func (s *walletPaymentService) processWalletPayments(
	ctx context.Context,
	inv *invoice.Invoice,
	wallets []*wallet.Wallet,
	priceTypeAmounts map[string]decimal.Decimal,
	options WalletPaymentOptions,
) decimal.Decimal {
	remainingAmount := inv.AmountRemaining
	initialAmount := inv.AmountRemaining
	paymentService := NewPaymentService(s.ServiceParams)

	for _, w := range wallets {
		if remainingAmount.IsZero() {
			break
		}

		// Calculate how much this wallet can pay based on its price type restrictions
		allowedAmount := s.calculateAllowedPaymentAmount(w, priceTypeAmounts, remainingAmount)
		if allowedAmount.IsZero() {
			s.Logger.Infow("wallet cannot pay any amount due to price type restrictions",
				"wallet_id", w.ID,
				"wallet_config", w.Config,
				"price_type_amounts", priceTypeAmounts)
			continue
		}

		paymentAmount := decimal.Min(allowedAmount, w.Balance)
		if paymentAmount.IsZero() {
			continue
		}

		// Create and process the payment
		if err := s.createWalletPayment(ctx, inv, w, paymentAmount, options.AdditionalMetadata, paymentService); err != nil {
			s.Logger.Errorw("failed to create wallet payment",
				"error", err,
				"invoice_id", inv.ID,
				"wallet_id", w.ID,
				"wallet_type", w.WalletType)
			continue
		}

		remainingAmount = remainingAmount.Sub(paymentAmount)

		// Update the price type amounts to reflect what was paid
		s.updatePriceTypeAmountsAfterPayment(priceTypeAmounts, paymentAmount, &w.Config)
	}

	return initialAmount.Sub(remainingAmount)
}

// createWalletPayment creates a single payment from a wallet to an invoice
func (s *walletPaymentService) createWalletPayment(
	ctx context.Context,
	inv *invoice.Invoice,
	w *wallet.Wallet,
	paymentAmount decimal.Decimal,
	additionalMetadata types.Metadata,
	paymentService PaymentService,
) error {
	// Create payment metadata
	metadata := types.Metadata{
		"wallet_type": string(w.WalletType),
		"wallet_id":   w.ID,
	}

	// Add additional metadata if provided
	for k, v := range additionalMetadata {
		metadata[k] = v
	}

	// Create payment request
	paymentReq := dto.CreatePaymentRequest{
		Amount:            paymentAmount,
		Currency:          inv.Currency,
		PaymentMethodType: types.PaymentMethodTypeCredits,
		PaymentMethodID:   w.ID,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		ProcessPayment:    true,
		Metadata:          metadata,
	}

	_, err := paymentService.CreatePayment(ctx, &paymentReq)
	return err
}

// sortWalletsByBalanceDesc sorts wallets by balance in descending order (highest first)
func (s *walletPaymentService) sortWalletsByBalanceDesc(wallets []*wallet.Wallet) {
	sort.Slice(wallets, func(i, j int) bool {
		return wallets[i].Balance.GreaterThan(wallets[j].Balance)
	})
}

// calculatePriceTypeAmounts calculates the total amount for each price type in the invoice
func (s *walletPaymentService) calculatePriceTypeAmounts(lineItems []*invoice.InvoiceLineItem) map[string]decimal.Decimal {
	priceTypeAmounts := map[string]decimal.Decimal{
		string(types.PRICE_TYPE_USAGE): decimal.Zero,
		string(types.PRICE_TYPE_FIXED): decimal.Zero,
	}

	for _, item := range lineItems {
		if item.PriceType != nil {
			priceType := *item.PriceType
			if amount, exists := priceTypeAmounts[priceType]; exists {
				priceTypeAmounts[priceType] = amount.Add(item.Amount)
			} else {
				// If it's not USAGE or FIXED, treat it as FIXED by default
				priceTypeAmounts[string(types.PRICE_TYPE_FIXED)] = priceTypeAmounts[string(types.PRICE_TYPE_FIXED)].Add(item.Amount)
			}
		} else {
			// If price type is nil, treat it as FIXED by default
			priceTypeAmounts[string(types.PRICE_TYPE_FIXED)] = priceTypeAmounts[string(types.PRICE_TYPE_FIXED)].Add(item.Amount)
		}
	}

	return priceTypeAmounts
}

// calculateAllowedPaymentAmount calculates how much a wallet can pay based on its price type restrictions
func (s *walletPaymentService) calculateAllowedPaymentAmount(
	w *wallet.Wallet,
	priceTypeAmounts map[string]decimal.Decimal,
	remainingAmount decimal.Decimal,
) decimal.Decimal {
	// If wallet has no allowed price types, use default (ALL)
	if len(w.Config.AllowedPriceTypes) == 0 {
		// Return the minimum of remaining amount and wallet balance
		return decimal.Min(remainingAmount, w.Balance)
	}

	allowedAmount := decimal.Zero

	for _, allowedPriceType := range w.Config.AllowedPriceTypes {
		switch allowedPriceType {
		case types.WalletConfigPriceTypeAll:
			// If ALL is allowed, wallet can pay the full remaining amount (up to its balance)
			return decimal.Min(remainingAmount, w.Balance)
		case types.WalletConfigPriceTypeUsage:
			// Add the remaining USAGE amount (only what's left to pay)
			usageAmount := priceTypeAmounts[string(types.PRICE_TYPE_USAGE)]
			if usageAmount.GreaterThan(decimal.Zero) {
				allowedAmount = allowedAmount.Add(usageAmount)
			}
		case types.WalletConfigPriceTypeFixed:
			// Add the remaining FIXED amount (only what's left to pay)
			fixedAmount := priceTypeAmounts[string(types.PRICE_TYPE_FIXED)]
			if fixedAmount.GreaterThan(decimal.Zero) {
				allowedAmount = allowedAmount.Add(fixedAmount)
			}
		}
	}

	// Return the minimum of allowed amount and wallet balance
	// Don't limit by remainingAmount here as priceTypeAmounts already reflects what's left
	return decimal.Min(allowedAmount, w.Balance)
}

// updatePriceTypeAmountsAfterPayment updates the price type amounts after a payment is made
func (s *walletPaymentService) updatePriceTypeAmountsAfterPayment(
	priceTypeAmounts map[string]decimal.Decimal,
	paymentAmount decimal.Decimal,
	walletConfig *types.WalletConfig,
) {
	// If wallet has no allowed price types, treat as ALL (can pay anything)
	if len(walletConfig.AllowedPriceTypes) == 0 {
		s.deductFromPriceTypes(priceTypeAmounts, paymentAmount, true, true)
		return
	}

	// Check wallet's allowed price types
	canPayUsage := false
	canPayFixed := false
	canPayAll := false

	for _, allowedPriceType := range walletConfig.AllowedPriceTypes {
		switch allowedPriceType {
		case types.WalletConfigPriceTypeAll:
			canPayAll = true
		case types.WalletConfigPriceTypeUsage:
			canPayUsage = true
		case types.WalletConfigPriceTypeFixed:
			canPayFixed = true
		}
	}

	// If wallet can pay ALL, it can pay any price type
	if canPayAll {
		s.deductFromPriceTypes(priceTypeAmounts, paymentAmount, true, true)
		return
	}

	// Deduct from specific price types based on wallet restrictions
	s.deductFromPriceTypes(priceTypeAmounts, paymentAmount, canPayUsage, canPayFixed)
}

// deductFromPriceTypes deducts payment amount from specific price types
// Priority: USAGE first, then FIXED (to optimize for usage-restricted wallets)
func (s *walletPaymentService) deductFromPriceTypes(
	priceTypeAmounts map[string]decimal.Decimal,
	paymentAmount decimal.Decimal,
	canPayUsage bool,
	canPayFixed bool,
) {
	remainingPayment := paymentAmount

	// Check if we have any price type amounts to deduct from
	totalPriceTypeAmount := decimal.Zero
	for _, amount := range priceTypeAmounts {
		totalPriceTypeAmount = totalPriceTypeAmount.Add(amount)
	}

	// If there are no price type amounts (e.g., invoice with no line items),
	// don't try to deduct from specific price types - this is valid for ALL wallets
	if totalPriceTypeAmount.IsZero() {
		s.Logger.Debugw("no price type amounts to deduct from - invoice may have no line items",
			"payment_amount", paymentAmount,
			"can_pay_usage", canPayUsage,
			"can_pay_fixed", canPayFixed)
		return
	}

	// First, deduct from USAGE if allowed and available
	if canPayUsage && remainingPayment.GreaterThan(decimal.Zero) {
		usageAmount := priceTypeAmounts[string(types.PRICE_TYPE_USAGE)]
		if usageAmount.GreaterThan(decimal.Zero) {
			deductAmount := decimal.Min(usageAmount, remainingPayment)
			priceTypeAmounts[string(types.PRICE_TYPE_USAGE)] = usageAmount.Sub(deductAmount)
			remainingPayment = remainingPayment.Sub(deductAmount)

			s.Logger.Debugw("deducted from usage price type",
				"deduct_amount", deductAmount,
				"remaining_usage", priceTypeAmounts[string(types.PRICE_TYPE_USAGE)],
				"remaining_payment", remainingPayment)
		}
	}

	// Then, deduct from FIXED if allowed and available
	if canPayFixed && remainingPayment.GreaterThan(decimal.Zero) {
		fixedAmount := priceTypeAmounts[string(types.PRICE_TYPE_FIXED)]
		if fixedAmount.GreaterThan(decimal.Zero) {
			deductAmount := decimal.Min(fixedAmount, remainingPayment)
			priceTypeAmounts[string(types.PRICE_TYPE_FIXED)] = fixedAmount.Sub(deductAmount)
			remainingPayment = remainingPayment.Sub(deductAmount)

			s.Logger.Debugw("deducted from fixed price type",
				"deduct_amount", deductAmount,
				"remaining_fixed", priceTypeAmounts[string(types.PRICE_TYPE_FIXED)],
				"remaining_payment", remainingPayment)
		}
	}

	// Log if there's still remaining payment (shouldn't happen with correct logic)
	if remainingPayment.GreaterThan(decimal.Zero) {
		s.Logger.Warnw("payment amount not fully deducted from price types",
			"remaining_payment", remainingPayment,
			"can_pay_usage", canPayUsage,
			"can_pay_fixed", canPayFixed,
			"price_type_amounts", priceTypeAmounts,
			"total_price_type_amount", totalPriceTypeAmount)
	}
}
