package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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
		invoice, err := invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     w.CustomerID,
			AmountDue:      s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate),
			Currency:       w.Currency,
			InvoiceType:    types.InvoiceTypeCredit,
			DueDate:        lo.ToPtr(time.Now().UTC()),
			IdempotencyKey: idempotencyKey,
			InvoiceStatus:  lo.ToPtr(types.InvoiceStatusFinalized),
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					Amount:      s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate),
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

func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	response := &dto.WalletBalanceResponse{
		RealTimeBalance: lo.ToPtr(decimal.Zero),
	}

	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if w.WalletStatus != types.WalletStatusActive {
		response.Wallet = w
		response.RealTimeBalance = lo.ToPtr(decimal.Zero)
		response.RealTimeCreditBalance = lo.ToPtr(decimal.Zero)
		response.BalanceUpdatedAt = lo.ToPtr(w.UpdatedAt)
		return response, nil
	}

	// Get invoice summary for unpaid amounts
	invoiceService := NewInvoiceService(s.ServiceParams)

	invoiceSummary, err := invoiceService.GetCustomerInvoiceSummary(ctx, w.CustomerID, w.Currency)
	if err != nil {
		return nil, err
	}

	// Get current period usage for active subscriptions
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	filter := types.NewSubscriptionFilter()
	filter.CustomerID = w.CustomerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
	}

	subscriptionsResp, err := subscriptionService.ListSubscriptions(ctx, filter)
	if err != nil {
		return nil, err
	}

	currentPeriodUsage := decimal.Zero
	for _, sub := range subscriptionsResp.Items {
		// Skip subscriptions with different currency
		if !types.IsMatchingCurrency(sub.Subscription.Currency, w.Currency) {
			continue
		}

		// Get current period usage for subscription
		usageResp, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: sub.Subscription.ID,
			StartTime:      sub.Subscription.CurrentPeriodStart,
			EndTime:        time.Now().UTC(),
			LifetimeUsage:  false, // Only get current period usage
		})
		if err != nil {
			s.Logger.Errorw("failed to get current period usage",
				"wallet_id", walletID,
				"subscription_id", sub.ID,
				"error", err,
			)
			continue
		}

		if usageResp.Amount > 0 {
			currentPeriodUsage = currentPeriodUsage.Add(decimal.NewFromFloat(usageResp.Amount))
		}
	}

	// Calculate real-time balance:
	// wallet_balance - (unpaid_invoices + current_period_usage)
	// NOTE: in future, we can add a feature to allow customers to set a threshold for real-time balance
	// NOTE: in future we can restrict a wallet balance to be adjusted only for usage or fixed amount
	realTimeBalance := w.Balance.
		Sub(invoiceSummary.TotalUnpaidAmount).
		Sub(currentPeriodUsage)

	s.Logger.Debugw("calculated real-time balance",
		"wallet_id", walletID,
		"current_balance", w.Balance,
		"unpaid_invoices", invoiceSummary.TotalUnpaidAmount,
		"current_period_usage", currentPeriodUsage,
		"real_time_balance", realTimeBalance,
		"currency", w.Currency,
	)

	return &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeBalance:       lo.ToPtr(realTimeBalance),
		RealTimeCreditBalance: lo.ToPtr(s.GetCreditsFromCurrencyAmount(realTimeBalance, w.ConversionRate)),
		BalanceUpdatedAt:      lo.ToPtr(time.Now().UTC()),
		UnpaidInvoiceAmount:   lo.ToPtr(invoiceSummary.TotalUnpaidAmount),
		CurrentPeriodUsage:    lo.ToPtr(currentPeriodUsage),
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

	return s.DB.WithTx(ctx, func(ctx context.Context) error {
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

	// TODO: Implement a retry mechanism or handle the error appropriately.
	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionPaymentSuccess, tx.ID)

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
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName: eventName,
		TenantID:  types.GetTenantID(ctx),
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(webhookPayload),
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
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName: eventName,
		TenantID:  types.GetTenantID(ctx),
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(webhookPayload),
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
