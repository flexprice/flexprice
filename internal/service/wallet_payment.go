package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

	// Process payments using wallets
	// calculate usage only amounts from the invoice
	usageOnlyAmount := decimal.Zero
	for _, lineItem := range inv.LineItems {
		if lo.FromPtr(lineItem.PriceType) == types.PRICE_TYPE_USAGE.String() {
			usageOnlyAmount = usageOnlyAmount.Add(lineItem.Amount)
		}
	}

	// total paid via credits already
	totalPaidViaCredits := decimal.Zero
	eligibleUsageAmountRemainingToBePaidViaCredits := usageOnlyAmount

	// check if there is any wallet with restrictions imposed on price types
	walletsWithRestrictions := make([]*wallet.Wallet, 0)
	for _, w := range wallets {
		if w.IsUsageRestricted() {
			walletsWithRestrictions = append(walletsWithRestrictions, w)
		}
	}

	hasUsageRestrictedWallets := len(walletsWithRestrictions) > 0
	if hasUsageRestrictedWallets {
		paymentFilter := &types.PaymentFilter{
			DestinationType:   lo.ToPtr(types.PaymentDestinationTypeInvoice.String()),
			DestinationID:     lo.ToPtr(inv.ID),
			PaymentMethodType: lo.ToPtr(types.PaymentMethodTypeCredits.String()),
			PaymentStatus:     lo.ToPtr(types.PaymentStatusSucceeded.String()),
		}

		payments, err := s.PaymentRepo.List(ctx, paymentFilter)
		if err != nil {
			return decimal.Zero, err
		}

		for _, payment := range payments {
			totalPaidViaCredits = totalPaidViaCredits.Add(payment.Amount)
		}

		eligibleUsageAmountRemainingToBePaidViaCredits = usageOnlyAmount.Sub(totalPaidViaCredits)

		// sort wallets such that the usage restricted wallets are attempted first
		// but preserve the strategy ordering within each group
		s.sortWalletsWithUsageRestrictionsFirst(wallets, options.Strategy)
	}

	remainingAmount := inv.AmountRemaining
	initialAmount := inv.AmountRemaining
	paymentService := NewPaymentService(s.ServiceParams)

	// Limit the number of wallets if specified
	maxWallets := len(wallets)
	if options.MaxWalletsToUse > 0 && options.MaxWalletsToUse < maxWallets {
		maxWallets = options.MaxWalletsToUse
	}

	for i, w := range wallets {
		if i >= maxWallets || remainingAmount.IsZero() {
			break
		}

		paymentAmount := decimal.Min(remainingAmount, w.Balance)
		if paymentAmount.IsZero() {
			continue
		}

		// CRITICAL FIX: For usage-restricted wallets, check if there's any eligible usage amount
		// If not, they cannot pay anything
		if w.IsUsageRestricted() {
			if !eligibleUsageAmountRemainingToBePaidViaCredits.IsPositive() {
				// No usage amount available for restricted wallets
				continue
			}
			// Limit payment to eligible usage amount for restricted wallets
			paymentAmount = decimal.Min(eligibleUsageAmountRemainingToBePaidViaCredits, w.Balance)

			if remainingAmount.Sub(paymentAmount).LessThan(decimal.Zero) {
				// ideally this should not happen, but if it does, we should not pay anything
				s.Logger.Errorw("usage only amount is greater than the remaining amount",
					"invoice_id", inv.ID,
					"usage_only_amount", usageOnlyAmount,
					"remaining_amount", remainingAmount,
					"wallet_id", w.ID,
					"wallet_type", w.WalletType)
				continue
			}
		}

		// Create payment request
		metadata := types.Metadata{
			"wallet_type": string(w.WalletType),
			"wallet_id":   w.ID,
		}

		// Add additional metadata if provided
		for k, v := range options.AdditionalMetadata {
			metadata[k] = v
		}

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
		if err != nil {
			s.Logger.Errorw("failed to create credits payment",
				"error", err,
				"invoice_id", inv.ID,
				"wallet_id", w.ID,
				"wallet_type", w.WalletType)
			continue
		}

		remainingAmount = remainingAmount.Sub(paymentAmount)

		// Update tracking for usage-restricted wallets
		// Any payment (restricted or unrestricted) reduces the remaining amount that future restricted wallets can pay
		if hasUsageRestrictedWallets {
			// For restricted wallets, their payment directly reduces the eligible usage amount
			if w.IsUsageRestricted() {
				eligibleUsageAmountRemainingToBePaidViaCredits = eligibleUsageAmountRemainingToBePaidViaCredits.Sub(paymentAmount)
			} else {
				// For unrestricted wallets, we need to determine how much of their payment went towards usage vs fixed
				// We assume payments go towards usage first (this matches typical billing logic)
				usagePaymentPortion := decimal.Min(paymentAmount, eligibleUsageAmountRemainingToBePaidViaCredits)
				eligibleUsageAmountRemainingToBePaidViaCredits = eligibleUsageAmountRemainingToBePaidViaCredits.Sub(usagePaymentPortion)
			}
			totalPaidViaCredits = totalPaidViaCredits.Add(paymentAmount)
		}
	}

	amountPaid := initialAmount.Sub(remainingAmount)

	if !amountPaid.IsZero() {
		s.Logger.Infow("payment processed using wallets",
			"invoice_id", inv.ID,
			"amount_paid", amountPaid,
			"remaining_amount", remainingAmount)
	} else {
		s.Logger.Infow("no payments processed using wallets",
			"invoice_id", inv.ID,
			"amount", initialAmount)
	}

	return amountPaid, nil
}

// sortWalletsWithUsageRestrictionsFirst sorts wallets to prioritize usage-restricted wallets first,
// while preserving the strategy ordering within each group (restricted vs unrestricted)
func (s *walletPaymentService) sortWalletsWithUsageRestrictionsFirst(wallets []*wallet.Wallet, strategy WalletPaymentStrategy) {
	// Use stable sort to preserve existing order within groups
	sort.SliceStable(wallets, func(i, j int) bool {
		// First, prioritize usage-restricted wallets
		iRestricted := wallets[i].IsUsageRestricted()
		jRestricted := wallets[j].IsUsageRestricted()

		if iRestricted != jRestricted {
			return iRestricted // restricted wallets come first
		}

		// If both have the same restriction status, preserve original strategy order
		// (no additional sorting needed here since strategy was already applied)
		return false
	})
}

// GetWalletsForPayment retrieves and filters wallets suitable for payment
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

	// Filter active wallets with matching currency
	activeWallets := make([]*wallet.Wallet, 0)
	for _, w := range wallets {
		if w.WalletStatus == types.WalletStatusActive &&
			types.IsMatchingCurrency(w.Currency, currency) &&
			w.Balance.GreaterThan(decimal.Zero) {
			activeWallets = append(activeWallets, w)
		}
	}

	// Sort wallets based on the selected strategy
	sortedWallets := s.sortWalletsByStrategy(activeWallets, options.Strategy)

	return sortedWallets, nil
}

// sortWalletsByStrategy sorts wallets based on the specified strategy
func (s *walletPaymentService) sortWalletsByStrategy(
	wallets []*wallet.Wallet,
	strategy WalletPaymentStrategy,
) []*wallet.Wallet {
	result := make([]*wallet.Wallet, 0, len(wallets))
	if len(wallets) == 0 {
		return result
	}

	// Copy wallets to avoid modifying the original slice
	result = append(result, wallets...)

	if strategy == "" {
		strategy = PromotionalFirstStrategy
	}

	// Sort by balance (smallest first to minimize leftover balances)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Balance.LessThan(result[j].Balance)
	})

	switch strategy {
	case PromotionalFirstStrategy:
		// First separate wallets by type
		sort.Slice(result, func(i, j int) bool {
			return result[i].WalletType == types.WalletTypePromotional
		})
	case PrepaidFirstStrategy:
		sort.Slice(result, func(i, j int) bool {
			return result[i].WalletType == types.WalletTypePrePaid
		})
	case BalanceOptimizedStrategy:
		// Sort by balance (smallest first to minimize leftover balances)
		sort.Slice(result, func(i, j int) bool {
			return result[i].Balance.LessThan(result[j].Balance)
		})
	default:
		// Default to promotional first if strategy is not recognized
		return s.sortWalletsByStrategy(wallets, PromotionalFirstStrategy)
	}

	return result
}
