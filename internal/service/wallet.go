package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// WalletService defines the interface for wallet operations
type WalletService interface {
	// CreateWallet creates a new wallet for a customer
	CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error)

	// GetWalletsByCustomerID retrieves all wallets for a customer
	GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error)

	// GetWalletByID retrieves a wallet by its ID and calculates real-time balance
	GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error)

	// GetWalletTransactions retrieves transactions for a wallet with pagination
	GetWalletTransactions(ctx context.Context, walletID string, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error)

	// TopUpWallet adds credits to a wallet
	TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.WalletResponse, error)

	// GetWalletTransactionByID retrieves a transaction by its ID
	GetWalletTransactionByID(ctx context.Context, transactionID string) (*dto.WalletTransactionResponse, error)

	// GetWalletBalance retrieves the real-time balance of a wallet
	GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)

	// GetWalletBalance Version 2
	GetWalletBalanceV2(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)

	// TerminateWallet terminates a wallet by closing it and debiting remaining balance
	TerminateWallet(ctx context.Context, walletID string) error

	// UpdateWallet updates a wallet
	UpdateWallet(ctx context.Context, id string, req *dto.UpdateWalletRequest) (*wallet.Wallet, error)

	// DebitWallet processes a debit operation on a wallet
	DebitWallet(ctx context.Context, req *wallet.WalletOperation) error

	// CreditWallet processes a credit operation on a wallet
	CreditWallet(ctx context.Context, req *wallet.WalletOperation) error

	// ExpireCredits expires credits for a given transaction
	ExpireCredits(ctx context.Context, transactionID string) error

	// conversion rate operations
	GetCurrencyAmountFromCredits(credits decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal
	GetCreditsFromCurrencyAmount(amount decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal

	// GetCustomerWallets retrieves all wallets for a customer
	GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error)

	// GetWallets retrieves wallets based on filter
	GetWallets(ctx context.Context, filter *types.WalletFilter) (*types.ListResponse[*wallet.Wallet], error)

	// UpdateWalletAlertState updates the alert state of a wallet
	UpdateWalletAlertState(ctx context.Context, walletID string, state types.AlertState) error

	// PublishEvent publishes a webhook event for a wallet
	PublishEvent(ctx context.Context, eventName string, w *wallet.Wallet) error

	// CheckBalanceThresholds checks if wallet balance is below threshold and triggers alerts
	CheckBalanceThresholds(ctx context.Context, w *wallet.Wallet, balance *dto.WalletBalanceResponse) error

	// TopUpWalletForProratedCharge tops up a wallet for proration credits from subscription changes
	TopUpWalletForProratedCharge(ctx context.Context, customerID string, amount decimal.Decimal, currency string) error
}

type walletService struct {
	ServiceParams
}

// NewWalletService creates a new instance of WalletService
func NewWalletService(params ServiceParams) WalletService {
	return &walletService{
		ServiceParams: params,
	}
}

func (s *walletService) CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error) {
	response := &dto.WalletResponse{}

	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid wallet request").
			Mark(ierr.ErrValidation)
	}

	if req.CustomerID == "" {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err != nil {
			return nil, err
		}
		req.CustomerID = customer.ID
	}

	// Check if customer already has an active wallet
	existingWallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	for _, w := range existingWallets {
		if w.WalletStatus == types.WalletStatusActive && w.Currency == req.Currency && w.WalletType == req.WalletType {
			return nil, ierr.NewError("customer already has an active wallet with the same currency and wallet type").
				WithHint("A customer can only have one active wallet per currency and wallet type").
				WithReportableDetails(map[string]interface{}{
					"customer_id": req.CustomerID,
					"wallet_id":   w.ID,
					"currency":    req.Currency,
					"wallet_type": req.WalletType,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}

	// Convert to domain wallet model
	w := req.ToWallet(ctx)

	// create a DB transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create wallet in DB and update the wallet object
		if err := s.WalletRepo.CreateWallet(ctx, w); err != nil {
			return err // Repository already using ierr
		}
		response = dto.FromWallet(w)

		s.Logger.Debugw("created wallet",
			"wallet_id", w.ID,
			"customer_id", w.CustomerID,
			"currency", w.Currency,
			"conversion_rate", w.ConversionRate,
		)

		// Load initial credits to wallet
		if req.InitialCreditsToLoad.GreaterThan(decimal.Zero) {
			topUpResp, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
				CreditsToAdd:      req.InitialCreditsToLoad,
				TransactionReason: types.TransactionReasonFreeCredit,
				ExpiryDate:        req.InitialCreditsToLoadExpiryDate,
				ExpiryDateUTC:     req.InitialCreditsExpiryDateUTC,
			})

			if err != nil {
				return err
			}
			response = topUpResp
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert to response DTO
	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletCreated, w.ID)

	return response, nil
}

func (s *walletService) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	response := make([]*dto.WalletResponse, len(wallets))
	for i, w := range wallets {
		response[i] = dto.FromWallet(w)
	}

	return response, nil
}

func (s *walletService) GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error) {
	if id == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	w, err := s.WalletRepo.GetWalletByID(ctx, id)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	return dto.FromWallet(w), nil
}

func (s *walletService) GetWalletTransactions(ctx context.Context, walletID string, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	// Ensure filter is initialized
	if filter == nil {
		filter = types.NewWalletTransactionFilter()
	}

	// Set wallet ID in filter
	filter.WalletID = &walletID

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	transactions, err := s.WalletRepo.ListWalletTransactions(ctx, filter)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	// Get total count
	count, err := s.WalletRepo.CountWalletTransactions(ctx, filter)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	response := &dto.ListWalletTransactionsResponse{
		Items: make([]*dto.WalletTransactionResponse, len(transactions)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	for i, txn := range transactions {
		response.Items[i] = dto.FromWalletTransaction(txn)
	}

	return response, nil
}

// Update the TopUpWallet method to use the new processWalletOperation
func (s *walletService) TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.WalletResponse, error) {
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, ierr.NewError("Wallet not found").
			WithHint("Wallet not found").
			Mark(ierr.ErrNotFound)
	}

	// If Credits to Add is not provided then convert the currency amount to credits
	// If both provided we give priority to Credits to add
	if req.CreditsToAdd.IsZero() && !req.Amount.IsZero() {
		req.CreditsToAdd = s.GetCreditsFromCurrencyAmount(req.Amount, w.ConversionRate)
	}

	// If ExpiryDateUTC is provided, convert it to YYYYMMDD format
	if req.ExpiryDateUTC != nil && req.ExpiryDate == nil {
		expiryDate := req.ExpiryDateUTC.UTC()
		parsedDate, err := strconv.Atoi(expiryDate.Format("20060102"))
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid expiry date").
				Mark(ierr.ErrValidation)
		}
		req.ExpiryDate = &parsedDate
	}

	// Create a credit operation
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid top up wallet request").
			Mark(ierr.ErrValidation)
	}

	// Generate or use provided idempotency key
	var idempotencyKey string
	if lo.FromPtr(req.IdempotencyKey) != "" {
		idempotencyKey = lo.FromPtr(req.IdempotencyKey)
	} else {
		idempotencyKey = types.GenerateUUID()
	}

	// Prepare credit operation details
	referenceType := types.WalletTxReferenceTypeExternal
	referenceID := idempotencyKey

	// Handle special case for purchased credits with invoice
	if req.TransactionReason == types.TransactionReasonPurchasedCreditInvoiced {
		paymentID, err := s.handlePurchasedCreditInvoicedTransaction(
			ctx,
			walletID,
			lo.ToPtr(idempotencyKey),
			req,
		)
		if err != nil {
			return nil, err
		}
		referenceID = paymentID
		referenceType = types.WalletTxReferenceTypePayment
	}

	// Create wallet credit operation
	creditReq := &wallet.WalletOperation{
		WalletID:          walletID,
		Type:              types.TransactionTypeCredit,
		CreditAmount:      req.CreditsToAdd,
		Description:       req.Description,
		Metadata:          req.Metadata,
		TransactionReason: req.TransactionReason,
		ReferenceType:     referenceType,
		ReferenceID:       referenceID,
		ExpiryDate:        req.ExpiryDate,
		IdempotencyKey:    idempotencyKey,
		Priority:          req.Priority,
	}

	// Process wallet credit
	if err := s.CreditWallet(ctx, creditReq); err != nil {
		return nil, err
	}

	return s.GetWalletByID(ctx, walletID)
}

func (s *walletService) handlePurchasedCreditInvoicedTransaction(ctx context.Context, walletID string, idempotencyKey *string, req *dto.TopUpWalletRequest) (string, error) {
	// Initialize required services
	invoiceService := NewInvoiceService(s.ServiceParams)
	paymentService := NewPaymentService(s.ServiceParams)

	// Retrieve wallet and customer details
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return "", err
	}

	var paymentID string
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create invoice for credit purchase
		amount := s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate)
		invoice, err := invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     w.CustomerID,
			AmountDue:      amount,
			Subtotal:       amount,
			Total:          amount,
			Currency:       w.Currency,
			InvoiceType:    types.InvoiceTypeCredit,
			DueDate:        lo.ToPtr(time.Now().UTC()),
			IdempotencyKey: idempotencyKey,
			InvoiceStatus:  lo.ToPtr(types.InvoiceStatusFinalized),
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					Amount:      amount,
					Quantity:    decimal.NewFromInt(1),
					DisplayName: lo.ToPtr("Purchased Credits"),
				},
			},
			PaymentStatus: lo.ToPtr(types.PaymentStatusPending),
		})
		if err != nil {
			return err
		}

		// Create payment for the invoice
		// Process : true will process the payment and update the invoice payment status
		payment, err := paymentService.CreatePayment(ctx, &dto.CreatePaymentRequest{
			IdempotencyKey:    lo.FromPtr(idempotencyKey),
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     invoice.ID,
			PaymentMethodType: types.PaymentMethodTypeOffline,
			Amount:            s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate),
			Currency:          w.Currency,
			ProcessPayment:    true,
		})
		if err != nil {
			return err
		}
		paymentID = payment.ID
		return nil
	})

	return paymentID, err
}

// GetWalletBalance calculates the real-time available balance for a wallet
// It considers:
// 1. Current wallet balance
// 2. Unpaid invoices
// 3. Current period charges (usage charges with entitlements)
func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get wallet details
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Safety check: Return zero balance for inactive wallets
	// This prevents any calculations on invalid wallet states
	if w.WalletStatus != types.WalletStatusActive {
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(decimal.Zero),
			RealTimeCreditBalance: lo.ToPtr(decimal.Zero),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
		}, nil
	}

	// Determine if we should include usage based on wallet's allowed price types
	// If wallet has no allowed price types (nil or empty), treat as ALL (include usage)
	// Otherwise, check if wallet allows USAGE or ALL price types
	shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)

	// Initialize total pending charges
	totalPendingCharges := decimal.Zero

	if shouldIncludeUsage {
		// STEP 1: Get all active subscriptions to calculate current usage
		subscriptions, err := s.SubRepo.ListByCustomerID(ctx, w.CustomerID)
		if err != nil {
			return nil, err
		}

		// Filter subscriptions by currency
		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if sub.Currency == w.Currency {
				filteredSubscriptions = append(filteredSubscriptions, sub)
				s.Logger.Infow("found matching subscription",
					"subscription_id", sub.ID,
					"currency", sub.Currency,
					"period_start", sub.CurrentPeriodStart,
					"period_end", sub.CurrentPeriodEnd)
			}
		}

		billingService := NewBillingService(s.ServiceParams)
		subscriptionService := NewSubscriptionService(s.ServiceParams)

		// Calculate total pending charges (usage) only if usage is allowed
		for _, sub := range filteredSubscriptions {
			// Get current period
			periodStart := sub.CurrentPeriodStart
			periodEnd := sub.CurrentPeriodEnd

			usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: sub.ID,
				StartTime:      periodStart,
				EndTime:        periodEnd,
			})

			if err != nil {
				return nil, err
			}

			// Calculate usage charges
			usageCharges, usageTotal, err := billingService.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)
			if err != nil {
				return nil, err
			}

			s.Logger.Infow("subscription charges details",
				"subscription_id", sub.ID,
				"usage_total", usageTotal,
				"num_usage_charges", len(usageCharges))

			totalPendingCharges = totalPendingCharges.Add(usageTotal)
		}
	}

	// Calculate real-time balance
	realTimeBalance := w.Balance.Sub(totalPendingCharges)

	s.Logger.Debugw("detailed balance calculation",
		"wallet_id", w.ID,
		"current_balance", w.Balance,
		"pending_charges", totalPendingCharges,
		"real_time_balance", realTimeBalance,
		"credit_balance", w.CreditBalance)

	// Convert real-time balance to credit balance
	realTimeCreditBalance := s.GetCreditsFromCurrencyAmount(realTimeBalance, w.ConversionRate)

	return &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeBalance:       lo.ToPtr(realTimeBalance),
		RealTimeCreditBalance: lo.ToPtr(realTimeCreditBalance),
		BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
		CurrentPeriodUsage:    lo.ToPtr(totalPendingCharges),
	}, nil
}

// Update the TerminateWallet method to use the new processWalletOperation
func (s *walletService) TerminateWallet(ctx context.Context, walletID string) error {
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return err
	}

	if w.WalletStatus == types.WalletStatusClosed {
		return ierr.NewError("wallet is already closed").
			WithHint("Wallet is already terminated").
			Mark(ierr.ErrInvalidOperation)
	}

	// Use client's WithTx for atomic operations
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Debit remaining balance if any
		if w.CreditBalance.GreaterThan(decimal.Zero) {
			debitReq := &wallet.WalletOperation{
				WalletID:          walletID,
				CreditAmount:      w.CreditBalance,
				Type:              types.TransactionTypeDebit,
				Description:       "Wallet termination - remaining balance debit",
				TransactionReason: types.TransactionReasonWalletTermination,
				ReferenceType:     types.WalletTxReferenceTypeRequest,
				IdempotencyKey:    walletID,
				ReferenceID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			}

			if err := s.DebitWallet(ctx, debitReq); err != nil {
				return err
			}
		}

		// Update wallet status to closed
		if err := s.WalletRepo.UpdateWalletStatus(ctx, walletID, types.WalletStatusClosed); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Publish webhook event
	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletTerminated, walletID)
	return nil
}

func (s *walletService) UpdateWallet(ctx context.Context, id string, req *dto.UpdateWalletRequest) (*wallet.Wallet, error) {
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid wallet request").
			Mark(ierr.ErrValidation)
	}

	// Get existing wallet
	existing, err := s.WalletRepo.GetWalletByID(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update fields if provided
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Metadata != nil {
		existing.Metadata = *req.Metadata
	}
	if req.AutoTopupTrigger != nil {
		existing.AutoTopupTrigger = *req.AutoTopupTrigger
	}
	if req.AutoTopupMinBalance != nil {
		existing.AutoTopupMinBalance = *req.AutoTopupMinBalance
	}
	if req.AutoTopupAmount != nil {
		existing.AutoTopupAmount = *req.AutoTopupAmount
	}
	if req.Config != nil {
		existing.Config = *req.Config
	}

	// Update alert config
	if req.AlertEnabled != nil {
		existing.AlertEnabled = *req.AlertEnabled
		// If alerts are disabled, clear the config and state
		if !*req.AlertEnabled {
			existing.AlertConfig = nil
			existing.AlertState = string(types.AlertStateOk)
		}
	}

	// Update alert config if provided and alerts are enabled
	if req.AlertConfig != nil {
		if !existing.AlertEnabled {
			return nil, ierr.NewError("cannot set alert config when alerts are disabled").
				WithHint("Enable alerts first before setting alert config").
				Mark(ierr.ErrValidation)
		}

		// Convert AlertConfig to types.AlertConfig
		existing.AlertConfig = &types.AlertConfig{
			Threshold: &types.AlertThreshold{
				Type:  types.AlertThresholdType(req.AlertConfig.Threshold.Type),
				Value: req.AlertConfig.Threshold.Value,
			},
		}
	}

	// Update wallet
	if err := s.WalletRepo.UpdateWallet(ctx, id, existing); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Publish webhook event
	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletUpdated, id)
	return existing, nil
}

// DebitWallet processes a debit operation on a wallet
func (s *walletService) DebitWallet(ctx context.Context, req *wallet.WalletOperation) error {
	if req.Type != types.TransactionTypeDebit {
		return ierr.NewError("invalid transaction type").
			WithHint("Invalid transaction type").
			Mark(ierr.ErrValidation)
	}

	if req.ReferenceType == "" || req.ReferenceID == "" {
		req.ReferenceType = types.WalletTxReferenceTypeRequest
		req.ReferenceID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION)
	}

	return s.processWalletOperation(ctx, req)
}

// CreditWallet processes a credit operation on a wallet
func (s *walletService) CreditWallet(ctx context.Context, req *wallet.WalletOperation) error {
	if req.Type != types.TransactionTypeCredit {
		return ierr.NewError("invalid transaction type").
			WithHint("Invalid transaction type").
			Mark(ierr.ErrValidation)
	}

	if req.ReferenceType == "" || req.ReferenceID == "" {
		req.ReferenceType = types.WalletTxReferenceTypeRequest
		req.ReferenceID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION)
	}

	return s.processWalletOperation(ctx, req)
}

// Wallet operations

// validateWalletOperation validates the wallet operation request
func (s *walletService) validateWalletOperation(w *wallet.Wallet, req *wallet.WalletOperation) error {
	if err := req.Validate(); err != nil {
		return err
	}

	// Convert amount to credit amount if provided and perform credit operation
	if req.Amount.GreaterThan(decimal.Zero) {
		req.CreditAmount = s.GetCreditsFromCurrencyAmount(req.Amount, w.ConversionRate)
	} else if req.CreditAmount.GreaterThan(decimal.Zero) {
		req.Amount = s.GetCurrencyAmountFromCredits(req.CreditAmount, w.ConversionRate)
	} else {
		return ierr.NewError("amount or credit amount is required").
			WithHint("Amount or credit amount is required").
			Mark(ierr.ErrValidation)
	}

	if req.CreditAmount.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("wallet transaction amount must be greater than 0").
			WithHint("Wallet transaction amount must be greater than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// processDebitOperation handles the debit operation with credit selection and consumption
func (s *walletService) processDebitOperation(ctx context.Context, req *wallet.WalletOperation) error {
	// Find eligible credits with pagination
	credits, err := s.WalletRepo.FindEligibleCredits(ctx, req.WalletID, req.CreditAmount, 100)
	if err != nil {
		return err
	}

	// Calculate total available balance
	var totalAvailable decimal.Decimal
	for _, c := range credits {
		totalAvailable = totalAvailable.Add(c.CreditsAvailable)
		if totalAvailable.GreaterThanOrEqual(req.CreditAmount) {
			break
		}
	}

	if totalAvailable.LessThan(req.CreditAmount) {
		return ierr.NewError("insufficient balance").
			WithHint("Insufficient balance to process debit operation").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": req.WalletID,
				"amount":    req.CreditAmount,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Process debit across credits
	if err := s.WalletRepo.ConsumeCredits(ctx, credits, req.CreditAmount); err != nil {
		return err
	}

	return nil
}

// processWalletOperation handles both credit and debit operations
func (s *walletService) processWalletOperation(ctx context.Context, req *wallet.WalletOperation) error {
	s.Logger.Debugw("Processing wallet operation", "req", req)

	// Get wallet
	w, err := s.WalletRepo.GetWalletByID(ctx, req.WalletID)
	if err != nil {
		return err
	}

	// Validate operation
	if err := s.validateWalletOperation(w, req); err != nil {
		return err
	}

	var newCreditBalance decimal.Decimal

	// For debit operations, find and consume available credits
	if req.Type == types.TransactionTypeDebit {
		newCreditBalance = w.CreditBalance.Sub(req.CreditAmount)
		// Process debit operation first
		err = s.processDebitOperation(ctx, req)
		if err != nil {
			return err
		}
	} else {
		// Process credit operation
		newCreditBalance = w.CreditBalance.Add(req.CreditAmount)
	}

	finalBalance := s.GetCurrencyAmountFromCredits(newCreditBalance, w.ConversionRate)

	// Create transaction record
	tx := &wallet.Transaction{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:            req.WalletID,
		Type:                req.Type,
		Amount:              req.Amount,
		CreditAmount:        req.CreditAmount,
		ReferenceType:       req.ReferenceType,
		ReferenceID:         req.ReferenceID,
		Description:         req.Description,
		Metadata:            req.Metadata,
		TxStatus:            types.TransactionStatusCompleted,
		TransactionReason:   req.TransactionReason,
		ExpiryDate:          types.ParseYYYYMMDDToDate(req.ExpiryDate),
		Priority:            req.Priority,
		CreditBalanceBefore: w.CreditBalance,
		CreditBalanceAfter:  newCreditBalance,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		IdempotencyKey:      req.IdempotencyKey,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	// Set credits available based on transaction type
	if req.Type == types.TransactionTypeCredit {
		tx.CreditsAvailable = req.CreditAmount
	} else {
		tx.CreditsAvailable = decimal.Zero
	}

	if req.Type == types.TransactionTypeCredit && req.ExpiryDate != nil {
		tx.ExpiryDate = types.ParseYYYYMMDDToDate(req.ExpiryDate)
	}

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {

		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return err
		}

		// Update wallet balance
		if err := s.WalletRepo.UpdateWalletBalance(ctx, req.WalletID, finalBalance, newCreditBalance); err != nil {
			return err
		}

		s.Logger.Debugw("Wallet operation completed")
		s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, tx.ID)
		return nil
	})
	if err != nil {
		return err
	}

	// Check credit balance alerts after wallet operation
	var thresholdValue decimal.Decimal
	var alertStatus types.AlertState

	// Get wallet threshold or use default (0)
	if w.AlertConfig != nil && w.AlertConfig.Threshold != nil {
		thresholdValue = w.AlertConfig.Threshold.Value
	} else {
		thresholdValue = decimal.Zero
	}

	// Determine alert status based on balance vs threshold
	if newCreditBalance.LessThan(thresholdValue) {
		alertStatus = types.AlertStateInAlarm
	} else {
		alertStatus = types.AlertStateOk
	}

	// Create alert info
	alertInfo := types.AlertInfo{
		Threshold: types.AlertThreshold{
			Type:  types.AlertThresholdTypeAmount,
			Value: thresholdValue,
		},
		ValueAtTime: newCreditBalance,
		Timestamp:   time.Now().UTC(),
	}

	// Log the alert
	alertService := NewAlertLogsService(s.ServiceParams)
	logAlertReq := &LogAlertRequest{
		EntityType:  types.AlertEntityTypeWallet,
		EntityID:    w.ID,
		AlertType:   types.AlertTypeLowCreditBalance,
		AlertStatus: alertStatus,
		AlertInfo:   alertInfo,
	}

	if err := alertService.LogAlert(ctx, logAlertReq); err != nil {
		// Log error but don't fail the transaction
		s.Logger.Errorw("failed to log credit balance alert",
			"error", err,
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
			"threshold", thresholdValue,
			"alert_status", alertStatus,
		)
	} else {
		s.Logger.Infow("credit balance alert logged successfully",
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
			"threshold", thresholdValue,
			"alert_status", alertStatus,
		)
	}
	return nil
}

// ExpireCredits expires credits for a given transaction
func (s *walletService) ExpireCredits(ctx context.Context, transactionID string) error {
	// Get the transaction
	tx, err := s.WalletRepo.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return err
	}

	// Validate transaction
	if tx.Type != types.TransactionTypeCredit {
		return ierr.NewError("can only expire credit transactions").
			WithHint("Only credit transactions can be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.ExpiryDate == nil {
		return ierr.NewError("transaction has no expiry date").
			WithHint("Transaction must have an expiry date to be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.ExpiryDate.After(time.Now().UTC()) {
		return ierr.NewError("transaction has not expired yet").
			WithHint("Transaction must have expired to be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.CreditsAvailable.IsZero() {
		return ierr.NewError("no credits available to expire").
			WithHint("Transaction has no credits available to expire").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Create a debit operation for the expired credits
	debitReq := &wallet.WalletOperation{
		WalletID:          tx.WalletID,
		Type:              types.TransactionTypeDebit,
		CreditAmount:      tx.CreditsAvailable,
		Description:       fmt.Sprintf("Credit expiry for transaction %s", tx.ID),
		TransactionReason: types.TransactionReasonCreditExpired,
		ReferenceType:     types.WalletTxReferenceTypeRequest,
		ReferenceID:       tx.ID,
		IdempotencyKey:    tx.ID,
		Metadata: types.Metadata{
			"expired_transaction_id": tx.ID,
			"expiry_date":            tx.ExpiryDate.Format(time.RFC3339),
		},
	}

	// Process the debit operation within a transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Process debit operation
		if err := s.DebitWallet(ctx, debitReq); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (s *walletService) publishInternalWalletWebhookEvent(ctx context.Context, eventName string, walletID string) {

	webhookPayload, err := json.Marshal(webhookDto.InternalWalletEvent{
		WalletID:  walletID,
		TenantID:  types.GetTenantID(ctx),
		EventType: eventName,
	})

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

func (s *walletService) GetWalletTransactionByID(ctx context.Context, transactionID string) (*dto.WalletTransactionResponse, error) {
	tx, err := s.WalletRepo.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return nil, err
	}
	return dto.FromWalletTransaction(tx), nil
}

func (s *walletService) publishInternalTransactionWebhookEvent(ctx context.Context, eventName string, transactionID string) {

	webhookPayload, err := json.Marshal(webhookDto.InternalTransactionEvent{
		TransactionID: transactionID,
		TenantID:      types.GetTenantID(ctx),
	})

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

// conversion rate operations
func (s *walletService) GetCurrencyAmountFromCredits(credits decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal {
	return credits.Mul(conversionRate)
}

func (s *walletService) GetCreditsFromCurrencyAmount(amount decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal {
	return amount.Div(conversionRate)
}

func (s *walletService) GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var customerID string
	if req.ID != "" {
		customerID = req.ID
		_, err := s.CustomerRepo.Get(ctx, customerID)
		if err != nil {
			return nil, err
		}
	} else {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.LookupKey)
		if err != nil {
			return nil, err
		}
		customerID = customer.ID
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	// if no wallets found, return empty slice
	if len(wallets) == 0 {
		return []*dto.WalletBalanceResponse{}, nil
	}

	response := make([]*dto.WalletBalanceResponse, len(wallets))

	if req.IncludeRealTimeBalance {
		for i, w := range wallets {
			balance, err := s.GetWalletBalance(ctx, w.ID)
			if err != nil {
				return nil, err
			}
			response[i] = balance
		}
	} else {
		for i, w := range wallets {
			response[i] = dto.ToWalletBalanceResponse(w)
		}
	}
	return response, nil
}

// GetWallets retrieves wallets based on filter
func (s *walletService) GetWallets(ctx context.Context, filter *types.WalletFilter) (*types.ListResponse[*wallet.Wallet], error) {
	if filter == nil {
		filter = types.NewWalletFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	// Get wallets using filter
	wallets, err := s.WalletRepo.GetWalletsByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &types.ListResponse[*wallet.Wallet]{
		Items: wallets,
		Pagination: types.PaginationResponse{
			Total:  len(wallets),
			Limit:  50,
			Offset: 0,
		},
	}, nil
}

// UpdateWalletAlertState updates the alert state of a wallet
func (s *walletService) UpdateWalletAlertState(ctx context.Context, walletID string, state types.AlertState) error {
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return err
	}

	// Update alert state directly
	w.AlertState = string(state)

	return s.WalletRepo.UpdateWallet(ctx, walletID, w)
}

// PublishEvent publishes a webhook event for a wallet
func (s *walletService) PublishEvent(ctx context.Context, eventName string, w *wallet.Wallet) error {
	if s.WebhookPublisher == nil {
		s.Logger.Warnw("webhook publisher not initialized", "event", eventName)
		return nil
	}

	// Get real-time balance
	balance, err := s.GetWalletBalance(ctx, w.ID)
	if err != nil {
		s.Logger.Errorw("failed to get wallet balance for webhook",
			"wallet_id", w.ID,
			"error", err,
		)
		return err
	}

	// Create internal event
	internalEvent := &webhookDto.InternalWalletEvent{
		EventType: eventName,
		WalletID:  w.ID,
		TenantID:  w.TenantID,
		Balance:   balance,
	}

	// Add alert info for alert events
	if w.AlertConfig != nil && w.AlertConfig.Threshold != nil {
		currentBalance := balance.RealTimeBalance
		if currentBalance == nil {
			currentBalance = &w.Balance
		}
		creditBalance := balance.RealTimeCreditBalance
		if creditBalance == nil {
			creditBalance = &w.CreditBalance
		}

		internalEvent.Alert = &webhookDto.WalletAlertInfo{
			State:          w.AlertState,
			Threshold:      w.AlertConfig.Threshold.Value,
			CurrentBalance: *currentBalance,
			CreditBalance:  *creditBalance,
			AlertConfig:    w.AlertConfig,
			AlertType:      getAlertType(eventName),
		}

		s.Logger.Infow("added alert info to webhook event",
			"wallet_id", w.ID,
			"alert_state", w.AlertState,
			"alert_type", getAlertType(eventName),
			"threshold", w.AlertConfig.Threshold.Value,
			"current_balance", *currentBalance,
			"credit_balance", *creditBalance,
		)
	}

	// Convert to JSON
	eventJSON, err := json.Marshal(internalEvent)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal internal event").
			Mark(ierr.ErrInternal)
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUID(),
		EventName:     eventName,
		TenantID:      w.TenantID,
		EnvironmentID: w.EnvironmentID,
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       eventJSON,
	}

	s.Logger.Infow("publishing webhook event",
		"event_id", webhookEvent.ID,
		"event_name", eventName,
		"wallet_id", w.ID,
		"alert_state", w.AlertState,
		"alert_config", w.AlertConfig,
	)

	return s.WebhookPublisher.PublishWebhook(ctx, webhookEvent)
}

func getAlertType(eventName string) string {
	switch eventName {
	case types.WebhookEventWalletCreditBalanceDropped, types.WebhookEventWalletCreditBalanceRecovered:
		return string(types.AlertTypeLowCreditBalance)
	case types.WebhookEventWalletOngoingBalanceDropped, types.WebhookEventWalletOngoingBalanceRecovered:
		return string(types.AlertTypeLowOngoingBalance)
	default:
		return ""
	}
}

// CheckBalanceThresholds checks if wallet balance is below threshold and triggers alerts
func (s *walletService) CheckBalanceThresholds(ctx context.Context, w *wallet.Wallet, balance *dto.WalletBalanceResponse) error {
	// Skip if alerts not enabled or no config
	if !w.AlertEnabled || w.AlertConfig == nil || w.AlertConfig.Threshold == nil {
		return nil
	}

	threshold := w.AlertConfig.Threshold.Value
	currentBalance := balance.RealTimeBalance
	if currentBalance == nil {
		currentBalance = &w.Balance
	}
	creditBalance := balance.RealTimeCreditBalance
	if creditBalance == nil {
		creditBalance = &w.CreditBalance
	}

	s.Logger.Infow("checking balance thresholds",
		"wallet_id", w.ID,
		"threshold", threshold,
		"current_balance", currentBalance,
		"credit_balance", creditBalance,
		"alert_state", w.AlertState,
	)

	// Check if any balance is below threshold
	isCurrentBalanceBelowThreshold := currentBalance.LessThanOrEqual(threshold)
	isCreditBalanceBelowThreshold := creditBalance.LessThanOrEqual(threshold)
	isAnyBalanceBelowThreshold := isCurrentBalanceBelowThreshold || isCreditBalanceBelowThreshold

	// Handle balance above threshold (recovery)
	if !isAnyBalanceBelowThreshold {
		s.Logger.Infow("all balances above threshold - checking recovery",
			"wallet_id", w.ID,
			"threshold", threshold,
			"current_balance", currentBalance,
			"credit_balance", creditBalance,
			"alert_state", w.AlertState,
		)

		// If current state is alert, update to ok (recovery)
		if w.AlertState == string(types.AlertStateInAlarm) {
			if err := s.UpdateWalletAlertState(ctx, w.ID, types.AlertStateOk); err != nil {
				s.Logger.Errorw("failed to update wallet alert state",
					"wallet_id", w.ID,
					"error", err,
				)
				return err
			}
			s.Logger.Infow("wallet recovered from alert state",
				"wallet_id", w.ID,
			)
			return s.PublishEvent(ctx, types.WebhookEventWalletUpdated, w)
		}
		return nil
	}

	// Skip if already in alert state
	if w.AlertState == string(types.AlertStateInAlarm) {
		s.Logger.Infow("skipping alert - already in alert state",
			"wallet_id", w.ID,
		)
		return nil
	}

	s.Logger.Infow("balance below/equal threshold - triggering alert",
		"wallet_id", w.ID,
		"threshold", threshold,
		"current_balance", currentBalance,
		"credit_balance", creditBalance,
	)

	// Update wallet state to alert
	if err := s.UpdateWalletAlertState(ctx, w.ID, types.AlertStateInAlarm); err != nil {
		s.Logger.Errorw("failed to update wallet alert state",
			"wallet_id", w.ID,
			"error", err,
		)
		return err
	}

	// Trigger alerts based on which balance is below threshold
	var errs []error
	if isCreditBalanceBelowThreshold {
		s.Logger.Infow("triggering credit balance alert",
			"wallet_id", w.ID,
			"credit_balance", creditBalance,
			"threshold", threshold,
		)
		if err := s.PublishEvent(ctx, types.WebhookEventWalletCreditBalanceDropped, w); err != nil {
			s.Logger.Errorw("failed to publish credit balance alert",
				"wallet_id", w.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	if isCurrentBalanceBelowThreshold {
		s.Logger.Infow("triggering ongoing balance alert",
			"wallet_id", w.ID,
			"balance", currentBalance,
			"threshold", threshold,
		)
		if err := s.PublishEvent(ctx, types.WebhookEventWalletOngoingBalanceDropped, w); err != nil {
			s.Logger.Errorw("failed to publish ongoing balance alert",
				"wallet_id", w.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}
	return nil
}

func (s *walletService) TopUpWalletForProratedCharge(ctx context.Context, customerID string, amount decimal.Decimal, currency string) error {
	if customerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Customer ID is required for wallet top-up").
			Mark(ierr.ErrValidation)
	}

	if amount.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("amount must be positive").
			WithHint("Top-up amount must be greater than zero").
			Mark(ierr.ErrValidation)
	}

	if currency == "" {
		currency = "usd" // Default to USD if no currency provided
	}

	// Get customer to validate existence
	_, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get customer").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get existing wallets for the customer
	existingWallets, err := s.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get existing wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Find or create a suitable wallet for the proration credit

	var selectedWallet *dto.WalletResponse
	for _, w := range existingWallets {
		if w.WalletStatus == types.WalletStatusActive &&
			types.IsMatchingCurrency(w.Currency, currency) &&
			w.WalletType == types.WalletTypePrePaid {
			selectedWallet = w
			break
		}
	}

	// Create a new wallet if none exists
	if selectedWallet == nil {
		s.Logger.Infow("creating new wallet for proration credit",
			"customer_id", customerID,
			"currency", currency,
			"amount", amount.String())

		walletReq := &dto.CreateWalletRequest{
			Name:           "Proration Credit Wallet",
			CustomerID:     customerID,
			Currency:       currency,
			ConversionRate: decimal.NewFromInt(1), // 1:1 conversion rate for credits
			WalletType:     types.WalletTypePrePaid,
			Metadata: types.Metadata{
				"created_for": "proration_credit",
				"source":      "subscription_change",
			},
		}

		selectedWallet, err = s.CreateWallet(ctx, walletReq)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create wallet for proration credit").
				WithReportableDetails(map[string]interface{}{
					"customer_id": customerID,
					"currency":    currency,
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	// Top up the wallet with the proration credit
	topUpReq := &dto.TopUpWalletRequest{
		Amount:            amount,
		TransactionReason: types.TransactionReasonSubscriptionCredit,
		Description:       "Proration credit from subscription change",
		Metadata: types.Metadata{
			"source":      "subscription_change_proration",
			"customer_id": customerID,
		},
		IdempotencyKey: lo.ToPtr(fmt.Sprintf("proration_credit_%s_%s", customerID, time.Now().Format("20060102150405"))),
	}

	_, err = s.TopUpWallet(ctx, selectedWallet.ID, topUpReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to top up wallet with proration credit").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
				"wallet_id":   selectedWallet.ID,
				"amount":      amount.String(),
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully topped up wallet for proration credit",
		"customer_id", customerID,
		"wallet_id", selectedWallet.ID,
		"amount", amount.String(),
		"currency", currency)

	return nil
}

func (s *walletService) GetWalletBalanceV2(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get wallet details
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Safety check: Return zero balance for inactive wallets
	// This prevents any calculations on invalid wallet states
	if w.WalletStatus != types.WalletStatusActive {
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(decimal.Zero),
			RealTimeCreditBalance: lo.ToPtr(decimal.Zero),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
		}, nil
	}

	// If wallet has no allowed price types (nil or empty), treat as ALL (include usage)
	shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)

	totalPendingCharges := decimal.Zero
	if shouldIncludeUsage {

		// STEP 1: Get all active subscriptions to calculate current usage
		subscriptions, err := s.SubRepo.ListByCustomerID(ctx, w.CustomerID)
		if err != nil {
			return nil, err
		}

		// Filter subscriptions by currency
		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if sub.Currency == w.Currency {
				filteredSubscriptions = append(filteredSubscriptions, sub)
				s.Logger.Infow("found matching subscription",
					"subscription_id", sub.ID,
					"currency", sub.Currency,
					"period_start", sub.CurrentPeriodStart,
					"period_end", sub.CurrentPeriodEnd)
			}
		}

		billingService := NewBillingService(s.ServiceParams)
		subscriptionService := NewSubscriptionService(s.ServiceParams)

		// Calculate total pending charges (usage)
		for _, sub := range filteredSubscriptions {

			// Get current period
			periodStart := sub.CurrentPeriodStart
			periodEnd := sub.CurrentPeriodEnd

			// Get usage data for current period
			usage, err := subscriptionService.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: sub.ID,
				StartTime:      periodStart,
				EndTime:        periodEnd,
			})
			if err != nil {
				return nil, err
			}

			// Calculate usage charges
			usageCharges, usageTotal, err := billingService.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)
			if err != nil {
				return nil, err
			}

			s.Logger.Infow("subscription charges details",
				"subscription_id", sub.ID,
				"usage_total", usageTotal,
				"num_usage_charges", len(usageCharges))

			totalPendingCharges = totalPendingCharges.Add(usageTotal)
		}
	}

	// Calculate real-time balance
	realTimeBalance := w.Balance.Sub(totalPendingCharges)

	s.Logger.Debugw("detailed balance calculation",
		"wallet_id", w.ID,
		"current_balance", w.Balance,
		"pending_charges", totalPendingCharges,
		"real_time_balance", realTimeBalance,
		"credit_balance", w.CreditBalance)

	// Convert real-time balance to credit balance
	realTimeCreditBalance := s.GetCreditsFromCurrencyAmount(realTimeBalance, w.ConversionRate)

	return &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeBalance:       &realTimeBalance,
		RealTimeCreditBalance: &realTimeCreditBalance,
		BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
		CurrentPeriodUsage:    &totalPendingCharges,
	}, nil
}
