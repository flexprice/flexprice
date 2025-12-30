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
	"github.com/flexprice/flexprice/internal/idempotency"
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
	TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error)

	// GetWalletTransactionByID retrieves a transaction by its ID
	GetWalletTransactionByID(ctx context.Context, transactionID string) (*dto.WalletTransactionResponse, error)

	// ListWalletTransactionsByFilter lists wallet transactions by filter
	ListWalletTransactionsByFilter(ctx context.Context, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error)

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

	// ManualBalanceDebit processes a manual balance debit operation on a wallet
	ManualBalanceDebit(ctx context.Context, walletID string, req *dto.ManualBalanceDebitRequest) (*dto.WalletResponse, error)

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

	// CompletePurchasedCreditTransaction completes a pending wallet transaction when payment succeeds
	CompletePurchasedCreditTransactionWithRetry(ctx context.Context, walletTransactionID string) error

	CheckWalletBalanceAlert(ctx context.Context, req *wallet.WalletBalanceAlertEvent) error

	// PublishWalletBalanceAlertEvent publishes a wallet balance alert event
	PublishWalletBalanceAlertEvent(ctx context.Context, customerID string, forceCalculateBalance bool, walletID string)
}

type walletService struct {
	ServiceParams
	idempGen *idempotency.Generator
}

// NewWalletService creates a new instance of WalletService
func NewWalletService(params ServiceParams) WalletService {
	return &walletService{
		ServiceParams: params,
		idempGen:      idempotency.NewGenerator(),
	}
}

func (s *walletService) CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error) {
	response := &dto.WalletResponse{}

	if req.PriceUnit != nil {
		pu, err := s.PriceUnitRepo.GetByCode(ctx, *req.PriceUnit)
		if err != nil {
			return nil, err
		}

		// Validate price unit is published
		if pu.Status != types.StatusPublished {
			return nil, ierr.NewError("price unit must be active").
				WithHint("The specified price unit is inactive").
				WithReportableDetails(map[string]interface{}{
					"price_unit": *req.PriceUnit,
					"status":     pu.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		// Set currency and conversion rate from price unit
		req.Currency = pu.BaseCurrency
		req.ConversionRate = pu.ConversionRate
	}

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
			idempotencyKey := s.idempGen.GenerateKey(idempotency.ScopeCreditGrant, map[string]interface{}{
				"wallet_id":          w.ID,
				"credits_to_add":     req.InitialCreditsToLoad,
				"transaction_reason": types.TransactionReasonFreeCredit,
				"timestamp":          time.Now().UTC().Format(time.RFC3339),
			})
			topUpResp, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
				CreditsToAdd:      req.InitialCreditsToLoad,
				TransactionReason: types.TransactionReasonFreeCredit,
				ExpiryDate:        req.InitialCreditsToLoadExpiryDate,
				ExpiryDateUTC:     req.InitialCreditsExpiryDateUTC,
				IdempotencyKey:    &idempotencyKey,
			})

			if err != nil {
				return err
			}
			// Update response with wallet from top-up response
			response = topUpResp.Wallet
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

func (s *walletService) ListWalletTransactionsByFilter(ctx context.Context, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error) {
	// Initialize filter if nil
	if filter == nil {
		filter = types.NewWalletTransactionFilter()
	}

	// Validate expand fields if any are requested
	if !filter.GetExpand().IsEmpty() {
		if err := filter.GetExpand().Validate(types.WalletTransactionExpandConfig); err != nil {
			return nil, err
		}
	}

	// Validate filter
	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	// Fetch transactions
	transactions, err := s.WalletRepo.ListWalletTransactions(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get total count for pagination
	count, err := s.WalletRepo.CountWalletTransactions(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build base response
	response := &dto.ListWalletTransactionsResponse{
		Items: make([]*dto.WalletTransactionResponse, len(transactions)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	// Early return if no transactions to avoid unnecessary expansion work
	if len(transactions) == 0 {
		return response, nil
	}

	expand := filter.GetExpand()
	if expand.IsEmpty() {
		// No expansion requested, just convert transactions to DTOs
		for i, txn := range transactions {
			response.Items[i] = dto.FromWalletTransaction(txn)
		}
		return response, nil
	}

	// Load expanded entities in bulk
	customersByID := s.loadCustomersForExpansion(ctx, expand, transactions)
	usersByID := s.loadUsersForExpansion(ctx, expand, transactions)
	walletsByID := s.loadWalletsForExpansion(ctx, expand, transactions)

	// Build response with expanded fields
	for i, txn := range transactions {
		response.Items[i] = dto.FromWalletTransaction(txn)

		// Attach expanded customer if requested and available
		if expand.Has(types.ExpandCustomer) && txn.CustomerID != "" {
			if cust, ok := customersByID[txn.CustomerID]; ok {
				response.Items[i].Customer = cust
			}
		}

		// Attach expanded user if requested and available
		if expand.Has(types.ExpandCreatedByUser) && txn.CreatedBy != "" {
			if user, ok := usersByID[txn.CreatedBy]; ok {
				response.Items[i].CreatedByUser = user
			}
		}

		// Attach expanded wallet if requested and available
		if expand.Has(types.ExpandWallet) && txn.WalletID != "" {
			if wallet, ok := walletsByID[txn.WalletID]; ok {
				response.Items[i].Wallet = wallet
			}
		}
	}

	return response, nil
}

// loadCustomersForExpansion loads customers in bulk for transaction expansion
func (s *walletService) loadCustomersForExpansion(ctx context.Context, expand types.Expand, transactions []*wallet.Transaction) map[string]*dto.CustomerResponse {
	if !expand.Has(types.ExpandCustomer) {
		return nil
	}

	// Extract unique customer IDs
	customerIDs := lo.Uniq(lo.FilterMap(transactions, func(txn *wallet.Transaction, _ int) (string, bool) {
		return txn.CustomerID, txn.CustomerID != ""
	}))

	if len(customerIDs) == 0 {
		return nil
	}

	// Fetch customers in bulk
	customerService := NewCustomerService(s.ServiceParams)
	customerFilter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerIDs: customerIDs,
	}

	customersResponse, err := customerService.GetCustomers(ctx, customerFilter)
	if err != nil {
		s.Logger.Errorw("failed to get customers for wallet transactions",
			"error", err,
			"customer_ids", customerIDs)
		return nil
	}

	// Create map for quick lookup
	customersByID := make(map[string]*dto.CustomerResponse, len(customersResponse.Items))
	for _, cust := range customersResponse.Items {
		customersByID[cust.Customer.ID] = cust
	}

	s.Logger.Debugw("fetched customers for wallet transactions", "count", len(customersResponse.Items))
	return customersByID
}

// loadUsersForExpansion loads users in bulk for transaction expansion
func (s *walletService) loadUsersForExpansion(ctx context.Context, expand types.Expand, transactions []*wallet.Transaction) map[string]*dto.UserResponse {
	if !expand.Has(types.ExpandCreatedByUser) {
		return nil
	}

	// Extract unique user IDs (created_by)
	userIDs := lo.Uniq(lo.FilterMap(transactions, func(txn *wallet.Transaction, _ int) (string, bool) {
		return txn.CreatedBy, txn.CreatedBy != ""
	}))

	if len(userIDs) == 0 {
		return nil
	}

	// Fetch users in bulk
	userService := NewUserService(s.UserRepo, s.TenantRepo, nil)
	userFilter := &types.UserFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		UserIDs:     userIDs,
	}

	usersResponse, err := userService.ListUsersByFilter(ctx, userFilter)
	if err != nil {
		s.Logger.Errorw("failed to get users for wallet transactions",
			"error", err,
			"user_ids", userIDs)
		return nil
	}

	// Create map for quick lookup
	usersByID := make(map[string]*dto.UserResponse, len(usersResponse.Items))
	for _, user := range usersResponse.Items {
		usersByID[user.ID] = user
	}

	s.Logger.Debugw("fetched users for wallet transactions", "count", len(usersResponse.Items))
	return usersByID
}

// loadWalletsForExpansion loads wallets in bulk for transaction expansion
func (s *walletService) loadWalletsForExpansion(ctx context.Context, expand types.Expand, transactions []*wallet.Transaction) map[string]*dto.WalletResponse {
	if !expand.Has(types.ExpandWallet) {
		return nil
	}

	// Extract unique wallet IDs
	walletIDs := lo.Uniq(lo.FilterMap(transactions, func(txn *wallet.Transaction, _ int) (string, bool) {
		return txn.WalletID, txn.WalletID != ""
	}))

	if len(walletIDs) == 0 {
		return nil
	}

	// Fetch wallets in bulk
	walletFilter := &types.WalletFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		WalletIDs:   walletIDs,
	}

	walletsResponse, err := s.GetWallets(ctx, walletFilter)
	if err != nil {
		s.Logger.Errorw("failed to get wallets for wallet transactions",
			"error", err,
			"wallet_ids", walletIDs)
		return nil
	}

	// Create map for quick lookup
	walletsByID := make(map[string]*dto.WalletResponse, len(walletsResponse.Items))
	for _, w := range walletsResponse.Items {
		walletsByID[w.ID] = dto.FromWallet(w)
	}

	s.Logger.Debugw("fetched wallets for wallet transactions", "count", len(walletsResponse.Items))
	return walletsByID
}

// Update the TopUpWallet method to use the new processWalletOperation
func (s *walletService) TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error) {
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

	// Generate idempotency key
	var idempotencyKey string
	if lo.FromPtr(req.IdempotencyKey) != "" {
		idempotencyKey = lo.FromPtr(req.IdempotencyKey)
	} else {
		idempotencyKey = s.idempGen.GenerateKey(idempotency.ScopeWalletTopUp, map[string]interface{}{
			"wallet_id":          walletID,
			"credits_to_add":     req.CreditsToAdd,
			"transaction_reason": req.TransactionReason,
			"timestamp":          time.Now().UTC().Format(time.RFC3339),
		})
	}

	// Handle special case for purchased credits with invoice
	if req.TransactionReason == types.TransactionReasonPurchasedCreditInvoiced {
		// This creates a PENDING wallet transaction and invoice
		// No wallet balance update happens yet
		walletTransactionID, invoiceID, err := s.handlePurchasedCreditInvoicedTransaction(
			ctx,
			walletID,
			lo.ToPtr(idempotencyKey),
			req,
		)
		if err != nil {
			return nil, err
		}

		s.Logger.Debugw("created pending credit purchase with invoice",
			"wallet_id", walletID,
			"wallet_transaction_id", walletTransactionID,
			"invoice_id", invoiceID,
			"credits", req.CreditsToAdd.String(),
		)

		// Get the wallet transaction
		tx, err := s.WalletRepo.GetTransactionByID(ctx, walletTransactionID)
		if err != nil {
			return nil, err
		}

		// Get updated wallet
		walletResp, err := s.GetWalletByID(ctx, walletID)
		if err != nil {
			return nil, err
		}

		// Return response with transaction, invoice ID, and wallet
		return &dto.TopUpWalletResponse{
			WalletTransaction: dto.FromWalletTransaction(tx),
			InvoiceID:         &invoiceID,
			Wallet:            walletResp,
		}, nil
	}

	// Handle direct credit purchase (PURCHASED_CREDIT_DIRECT) or any other transaction reason
	// This immediately credits the wallet
	referenceType := types.WalletTxReferenceTypeExternal
	referenceID := idempotencyKey

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

	// Process wallet credit immediately
	err = s.processWalletOperation(ctx, creditReq)
	if err != nil {
		return nil, err
	}

	// Get the wallet transaction by idempotency key
	tx, err := s.WalletRepo.GetTransactionByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, err
	}

	// Get updated wallet
	walletResp, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Return response with transaction and wallet (no invoice for direct credits)
	return &dto.TopUpWalletResponse{
		WalletTransaction: dto.FromWalletTransaction(tx),
		InvoiceID:         nil,
		Wallet:            walletResp,
	}, nil
}

func (s *walletService) handlePurchasedCreditInvoicedTransaction(ctx context.Context, walletID string, idempotencyKey *string, req *dto.TopUpWalletRequest) (string, string, error) {
	// Initialize required services
	invoiceService := NewInvoiceService(s.ServiceParams)

	settingsService := &settingsService{
		ServiceParams: s.ServiceParams,
	}

	// Retrieve wallet and customer details
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return "", "", err
	}

	// Get invoice config setting to check auto_complete flag
	invoiceConfig, err := GetSetting[types.InvoiceConfig](
		settingsService,
		ctx,
		types.SettingKeyInvoiceConfig,
	)
	if err != nil {
		return "", "", err
	}

	// Check if auto-complete is enabled
	autoCompleteEnabled := invoiceConfig.AutoCompletePurchasedCreditTransaction

	s.Logger.Debugw("processing purchased credit transaction",
		"wallet_id", walletID,
		"auto_complete_enabled", autoCompleteEnabled,
		"credits", req.CreditsToAdd.String(),
	)

	var walletTransactionID string
	var invoiceID string
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Step 1: Create wallet transaction (pending or completed based on setting)
		txStatus := types.TransactionStatusPending
		balanceAfter := w.CreditBalance
		creditsAvailable := decimal.Zero
		var description string

		if autoCompleteEnabled {
			// If auto-complete is enabled, create transaction as COMPLETED
			txStatus = types.TransactionStatusCompleted
			balanceAfter = w.CreditBalance.Add(req.CreditsToAdd)
			creditsAvailable = req.CreditsToAdd
			description = lo.Ternary(req.Description != "", req.Description, "Purchased credits - auto-completed")
		} else {
			description = lo.Ternary(req.Description != "", req.Description, "Purchased credits - pending payment")
		}

		txMetadata := req.Metadata
		if txMetadata == nil {
			txMetadata = types.Metadata{}
		}

		tx := &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            walletID,
			CustomerID:          w.CustomerID,
			Type:                types.TransactionTypeCredit,
			CreditAmount:        req.CreditsToAdd,
			Amount:              s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate),
			TxStatus:            txStatus,
			ReferenceType:       types.WalletTxReferenceTypeExternal,
			ReferenceID:         lo.FromPtr(idempotencyKey),
			Description:         description,
			Metadata:            txMetadata,
			TransactionReason:   types.TransactionReasonPurchasedCreditInvoiced,
			Priority:            req.Priority,
			IdempotencyKey:      lo.FromPtr(idempotencyKey),
			EnvironmentID:       w.EnvironmentID,
			CreditBalanceBefore: w.CreditBalance,
			CreditBalanceAfter:  balanceAfter,
			CreditsAvailable:    creditsAvailable,
			Currency:            w.Currency,
			ExpiryDate:          types.ParseYYYYMMDDToDate(req.ExpiryDate),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		// Create the transaction
		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create wallet transaction").
				Mark(ierr.ErrInternal)
		}

		// If auto-complete is enabled, update wallet balance immediately
		if autoCompleteEnabled {
			finalBalance := w.Balance.Add(tx.Amount)
			if err := s.WalletRepo.UpdateWalletBalance(ctx, walletID, finalBalance, balanceAfter); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update wallet balance").
					Mark(ierr.ErrInternal)
			}

			s.Logger.Infow("auto-completed wallet credit transaction",
				"wallet_transaction_id", tx.ID,
				"wallet_id", walletID,
				"credits_added", req.CreditsToAdd.String(),
				"new_credit_balance", balanceAfter.String(),
			)
		}

		walletTransactionID = tx.ID

		// Step 2: Create invoice for credit purchase with wallet_transaction_id in metadata
		amount := s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.ConversionRate)
		invoiceMetadata := make(types.Metadata)

		// Copy existing metadata from request if provided
		if req.Metadata != nil {
			for key, value := range req.Metadata {
				invoiceMetadata[key] = value
			}
		}

		// Ensure auto_topup flag is present on invoice metadata as well
		invoiceMetadata["auto_topup"] = lo.Ternary(req.Metadata != nil && req.Metadata["auto_topup"] == "true", "true", invoiceMetadata["auto_topup"])

		// Add required fields
		invoiceMetadata["wallet_transaction_id"] = walletTransactionID
		invoiceMetadata["wallet_id"] = walletID
		invoiceMetadata["credits_amount"] = req.CreditsToAdd.String()
		invoiceMetadata["auto_completed"] = fmt.Sprintf("%v", autoCompleteEnabled)

		// Add description to invoice metadata if provided
		if req.Description != "" {
			invoiceMetadata["description"] = req.Description
		}

		// Set payment status based on auto-complete setting
		paymentStatus := types.PaymentStatusPending
		var amountPaid *decimal.Decimal
		if autoCompleteEnabled {
			paymentStatus = types.PaymentStatusSucceeded
			amountPaid = &amount
		}

		invoice, err := invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     w.CustomerID,
			AmountDue:      amount,
			AmountPaid:     amountPaid,
			Subtotal:       amount,
			Total:          amount,
			Currency:       w.Currency,
			InvoiceType:    types.InvoiceTypeOneOff, // Changed from CREDIT to ONE_OFF
			DueDate:        lo.ToPtr(time.Now().UTC()),
			IdempotencyKey: idempotencyKey,
			InvoiceStatus:  lo.ToPtr(types.InvoiceStatusFinalized),
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					Amount:      amount,
					Quantity:    decimal.NewFromInt(1),
					DisplayName: lo.ToPtr(fmt.Sprintf("Purchase %s Credits", req.CreditsToAdd.String())),
				},
			},
			PaymentStatus: lo.ToPtr(paymentStatus),
			Metadata:      invoiceMetadata,
		})
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create invoice for purchased credits").
				Mark(ierr.ErrInternal)
		}

		invoiceID = invoice.ID

		if autoCompleteEnabled {
			s.Logger.Infow("created auto-completed credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", invoice.ID,
				"wallet_id", walletID,
				"credits", req.CreditsToAdd.String(),
				"amount", amount.String(),
				"payment_status", paymentStatus,
			)
		} else {
			s.Logger.Infow("created pending credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", invoice.ID,
				"wallet_id", walletID,
				"credits", req.CreditsToAdd.String(),
				"amount", amount.String(),
			)
		}

		return nil
	})

	if err != nil {
		return "", "", err
	}

	// If auto-completed, publish webhook event immediately
	if autoCompleteEnabled {
		s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, walletTransactionID)
	}

	return walletTransactionID, invoiceID, err
}

// CompletePurchasedCreditTransactionWithRetry completes a pending wallet transaction when payment succeeds
// Includes simple retry logic for transient failures
func (s *walletService) CompletePurchasedCreditTransactionWithRetry(ctx context.Context, walletTransactionID string) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := s.completePurchasedCreditTransaction(ctx, walletTransactionID)
		if err == nil {
			if attempt > 0 {
				s.Logger.Infow("successfully completed purchased credit transaction after retry",
					"wallet_transaction_id", walletTransactionID,
					"attempt", attempt+1,
				)
			}
			return nil
		}

		lastErr = err

		// Don't retry validation errors or if already completed
		if ierr.IsValidation(err) || ierr.IsInvalidOperation(err) {
			return err
		}

		// Log retry attempt
		if attempt < maxRetries-1 {
			s.Logger.Debugw("failed to complete purchased credit transaction, retrying",
				"error", err,
				"wallet_transaction_id", walletTransactionID,
				"attempt", attempt+1,
				"max_retries", maxRetries,
			)
			// Simple backoff: 100ms, 200ms
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}

	// All retries failed
	s.Logger.Errorw("failed to complete purchased credit transaction after all retries",
		"error", lastErr,
		"wallet_transaction_id", walletTransactionID,
		"attempts", maxRetries,
	)
	return lastErr
}

// completePurchasedCreditTransaction performs the actual completion logic
func (s *walletService) completePurchasedCreditTransaction(ctx context.Context, walletTransactionID string) error {
	// Get the pending transaction
	tx, err := s.WalletRepo.GetTransactionByID(ctx, walletTransactionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get wallet transaction").
			Mark(ierr.ErrNotFound)
	}

	// Validate transaction state
	if tx.TxStatus != types.TransactionStatusPending {
		s.Logger.Debugw("wallet transaction is not pending",
			"wallet_transaction_id", walletTransactionID,
			"current_status", tx.TxStatus,
		)
		// If already completed (e.g., via auto-complete setting), this is idempotent - return success
		if tx.TxStatus == types.TransactionStatusCompleted {
			s.Logger.Debugw("wallet transaction already completed",
				"wallet_transaction_id", walletTransactionID,
			)
			return nil
		}
		return ierr.NewError("wallet transaction is not in pending state").
			WithHint("Only pending transactions can be completed").
			WithReportableDetails(map[string]interface{}{
				"wallet_transaction_id": walletTransactionID,
				"current_status":        tx.TxStatus,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.Type != types.TransactionTypeCredit {
		return ierr.NewError("only credit transactions can be completed").
			WithHint("Only credit transactions can be completed").
			WithReportableDetails(map[string]interface{}{
				"wallet_transaction_id": walletTransactionID,
				"transaction_type":      tx.Type,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Get wallet to check current balance
	w, err := s.WalletRepo.GetWalletByID(ctx, tx.WalletID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get wallet").
			Mark(ierr.ErrNotFound)
	}

	// Execute completion in a transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Calculate new balances
		finalBalance := w.Balance.Add(tx.Amount)
		newCreditBalance := w.CreditBalance.Add(tx.CreditAmount)

		// Update transaction status and balances
		tx.TxStatus = types.TransactionStatusCompleted
		tx.CreditBalanceBefore = w.CreditBalance
		tx.CreditBalanceAfter = newCreditBalance
		tx.CreditsAvailable = tx.CreditAmount
		tx.UpdatedAt = time.Now().UTC()

		// Update the transaction
		if err := s.WalletRepo.UpdateTransaction(ctx, tx); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update wallet transaction").
				Mark(ierr.ErrInternal)
		}

		// Update wallet balance
		if err := s.WalletRepo.UpdateWalletBalance(ctx, tx.WalletID, finalBalance, newCreditBalance); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update wallet balance").
				Mark(ierr.ErrInternal)
		}

		s.Logger.Infow("completed purchased credit transaction",
			"wallet_transaction_id", walletTransactionID,
			"wallet_id", tx.WalletID,
			"credits_added", tx.CreditAmount.String(),
			"new_balance", newCreditBalance.String(),
		)

		return nil
	})

	if err != nil {
		return err
	}

	// Publish webhook event after transaction commits
	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, tx.ID)

	// Log credit balance alert after transaction completes
	if err := s.logCreditBalanceAlert(ctx, w, w.CreditBalance.Add(tx.CreditAmount)); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.Errorw("failed to log credit balance alert after completing purchased credit transaction",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	return nil
}

// logCreditBalanceAlert logs a credit balance alert for a wallet after a balance change
func (s *walletService) logCreditBalanceAlert(ctx context.Context, w *wallet.Wallet, newCreditBalance decimal.Decimal) error {
	// Check credit balance alerts after wallet operation
	var thresholdValue decimal.Decimal
	var alertStatus types.AlertState

	// Get wallet threshold or fall back to tenant-level settings
	if w.AlertConfig != nil && w.AlertConfig.Threshold != nil {
		thresholdValue = w.AlertConfig.Threshold.Value
	} else {
		// Fall back to tenant-level settings (GetSetting handles defaults automatically)
		settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
		walletAlertConfig, err := GetSetting[types.AlertConfig](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
		if err != nil {
			s.Logger.Errorw("failed to get wallet alert config from tenant settings",
				"error", err,
				"wallet_id", w.ID,
			)
		}
		thresholdValue = walletAlertConfig.Threshold.Value
	}

	// Threshold is stored in credits, credit balance is in credits - direct comparison
	// Determine alert status based on balance vs threshold (<= threshold triggers alert)
	if newCreditBalance.LessThanOrEqual(thresholdValue) {
		alertStatus = types.AlertStateInAlarm
	} else {
		alertStatus = types.AlertStateOk
	}

	// Create alert info
	alertInfo := types.AlertInfo{
		AlertSettings: &types.AlertSettings{
			Critical: &types.AlertThreshold{
				Threshold: thresholdValue,
				Condition: types.AlertConditionBelow,
			},
			AlertEnabled: lo.ToPtr(true),
		},
		ValueAtTime: newCreditBalance,
		Timestamp:   time.Now().UTC(),
	}

	// Log the alert
	alertService := NewAlertLogsService(s.ServiceParams)

	// Get customer ID from wallet if available
	var customerID *string
	if w.CustomerID != "" {
		customerID = lo.ToPtr(w.CustomerID)
	}

	logAlertReq := &LogAlertRequest{
		EntityType:  types.AlertEntityTypeWallet,
		EntityID:    w.ID,
		CustomerID:  customerID,
		AlertType:   types.AlertTypeLowCreditBalance,
		AlertStatus: alertStatus,
		AlertInfo:   alertInfo,
	}

	if err := alertService.LogAlert(ctx, logAlertReq); err != nil {
		s.Logger.Errorw("failed to log credit balance alert",
			"error", err,
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
			"threshold", thresholdValue,
			"alert_status", alertStatus,
		)
		return err
	}

	s.Logger.Infow("credit balance alert logged successfully",
		"wallet_id", w.ID,
		"new_credit_balance", newCreditBalance,
		"threshold", thresholdValue,
		"alert_status", alertStatus,
	)
	return nil
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

	// Initialize current period usage
	currentPeriodUsage := decimal.Zero
	totalPendingCharges := decimal.Zero

	if shouldIncludeUsage {
		// STEP 1: Get all active subscriptions to calculate current usage
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		subscriptions, err := subscriptionService.ListByCustomerID(ctx, w.CustomerID)
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

			currentPeriodUsage = currentPeriodUsage.Add(usageTotal)
		}
	}

	invoiceService := NewInvoiceService(s.ServiceParams)

	resp, err := invoiceService.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: w.CustomerID,
		Currency:   w.Currency,
	})

	if err != nil {
		return nil, err
	}

	totalPendingCharges = currentPeriodUsage.Add(resp.TotalUnpaidUsageCharges)

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
		UnpaidInvoicesAmount:  lo.ToPtr(resp.TotalUnpaidUsageCharges),
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
	if req.AutoTopup != nil {
		// Preserve existing fields so ent validator still gets required values
		current := existing.AutoTopup
		if current == nil {
			current = &types.AutoTopup{}
		}
		if req.AutoTopup.Enabled != nil {
			current.Enabled = req.AutoTopup.Enabled
		}
		if req.AutoTopup.Threshold != nil {
			current.Threshold = req.AutoTopup.Threshold
		}
		if req.AutoTopup.Amount != nil {
			current.Amount = req.AutoTopup.Amount
		}
		if req.AutoTopup.Invoicing != nil {
			current.Invoicing = req.AutoTopup.Invoicing
		}
		existing.AutoTopup = current
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
			Threshold: &types.WalletAlertThreshold{
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

	// Normalize all inputs into CreditAmount (internal processing field)
	// Priority: Amount > CreditAmount
	// Note: CreditAmount is used internally for BOTH credit and debit operations
	// The Type field determines direction (add vs subtract)

	switch {
	case req.Amount.GreaterThan(decimal.Zero):
		// Amount provided - convert to credits
		req.CreditAmount = s.GetCreditsFromCurrencyAmount(req.Amount, w.ConversionRate)

	case req.CreditAmount.GreaterThan(decimal.Zero):
		// CreditAmount already set - just convert to Amount
		req.Amount = s.GetCurrencyAmountFromCredits(req.CreditAmount, w.ConversionRate)

	default:
		return ierr.NewError("amount or credit_amount is required").
			WithHint("Amount or credit amount is required").
			Mark(ierr.ErrValidation)
	}

	// Credit amount is validated for it is used internally for both credit and debit operations
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
	credits := []*wallet.Transaction{}
	var err error
	if req.ParentCreditTxID != "" {
		// Get the parent debit transaction
		parentCreditTx, err := s.WalletRepo.GetTransactionByID(ctx, req.ParentCreditTxID)
		if err != nil {
			return err
		}
		credits = append(credits, parentCreditTx)

	} else {
		// Determine the time reference for finding eligible credits
		timeReference := time.Now().UTC()
		if req.InvoiceID != nil && *req.InvoiceID != "" {
			// Use invoice's period end as the time reference
			invoice, err := s.InvoiceRepo.Get(ctx, *req.InvoiceID)
			if err != nil {
				return err
			}
			timeReference = *invoice.PeriodEnd
		}
		credits, err = s.WalletRepo.FindEligibleCredits(ctx, req.WalletID, req.CreditAmount, 100, timeReference)
		if err != nil {
			return err
		}
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
		// if not manual debit, return error
		if req.TransactionReason != types.TransactionReasonManualBalanceDebit {
			return ierr.NewError("insufficient balance").
				WithHint("Insufficient balance to process debit operation").
				WithReportableDetails(map[string]interface{}{
					"wallet_id": req.WalletID,
					"amount":    req.CreditAmount,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
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
		CustomerID:          w.CustomerID,
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
		Currency:            w.Currency,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		IdempotencyKey:      req.IdempotencyKey,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	// Set credits available based on transaction type
	if req.Type == types.TransactionTypeCredit {
		tx.CreditsAvailable = decimal.Max(decimal.Zero, tx.CreditBalanceAfter)
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
		return nil
	})
	if err != nil {
		return err
	}

	// Publish webhook event after transaction commits
	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, tx.ID)

	s.PublishWalletBalanceAlertEvent(ctx, w.CustomerID, true, req.WalletID)

	// Log credit balance alert after wallet operation
	if err := s.logCreditBalanceAlert(ctx, w, newCreditBalance); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.Errorw("failed to log credit balance alert after wallet operation",
			"error", err,
			"wallet_id", w.ID,
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
		ParentCreditTxID:  tx.ID,
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
			response[i] = &dto.WalletBalanceResponse{
				Wallet: w,
			}
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
	balance, err := s.GetWalletBalanceV2(ctx, w.ID)
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
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		subscriptions, err := subscriptionService.ListByCustomerID(ctx, w.CustomerID)
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

func (s *walletService) ManualBalanceDebit(ctx context.Context, walletID string, req *dto.ManualBalanceDebitRequest) (*dto.WalletResponse, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if w.WalletStatus != types.WalletStatusActive {
		return nil, ierr.NewError("wallet is not active").
			WithHint("Wallet is not active").
			Mark(ierr.ErrInvalidOperation)
	}

	debitReq := &wallet.WalletOperation{
		WalletID:          walletID,
		CreditAmount:      req.Credits,
		Type:              types.TransactionTypeDebit,
		Description:       req.Description,
		TransactionReason: req.TransactionReason,
		ReferenceType:     types.WalletTxReferenceTypeRequest,
		IdempotencyKey:    *req.IdempotencyKey,
		ReferenceID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
	}

	err = s.DebitWallet(ctx, debitReq)
	if err != nil {
		return nil, err
	}

	return s.GetWalletByID(ctx, walletID)
}

func (s *walletService) fetchFeaturesWithAlertSettings(ctx context.Context) ([]*dto.FeatureResponse, error) {
	queryFilter := types.NewNoLimitQueryFilter()
	queryFilter.Status = lo.ToPtr(types.StatusPublished)

	featureService := NewFeatureService(s.ServiceParams)

	features, err := featureService.GetFeatures(ctx, &types.FeatureFilter{
		QueryFilter: queryFilter,
	})
	if err != nil {
		return nil, err
	}

	// Filter to only features with alert settings enabled
	featuresWithAlerts := lo.Filter(features.Items, func(feature *dto.FeatureResponse, _ int) bool {
		return feature.AlertSettings != nil && feature.AlertSettings.IsAlertEnabled()
	})

	return featuresWithAlerts, nil
}

func (s *walletService) CheckWalletBalanceAlert(ctx context.Context, req *wallet.WalletBalanceAlertEvent) error {
	// Set context values from event
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, req.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxTenantID, req.TenantID)

	s.Logger.Infow("checking wallet balance alerts",
		"event_id", req.ID,
		"customer_id", req.CustomerID,
		"tenant_id", req.TenantID,
		"environment_id", req.EnvironmentID,
		"wallet_id", req.WalletID,
		"source", req.Source,
		"force_calculate", req.ForceCalculateBalance,
	)

	// Get active wallets for this customer
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, req.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get wallets for customer",
			"error", err,
			"customer_id", req.CustomerID,
			"event_id", req.ID,
		)
		return err
	}

	if len(wallets) == 0 {
		s.Logger.Infow("no wallets found for customer",
			"customer_id", req.CustomerID,
			"event_id", req.ID,
		)
		return nil
	}

	featuresWithAlerts, err := s.fetchFeaturesWithAlertSettings(ctx)
	if err != nil {
		s.Logger.Errorw("failed to fetch features with alert settings",
			"error", err,
		)
	}

	alertLogsService := NewAlertLogsService(s.ServiceParams)
	settingsSvc := &settingsService{
		ServiceParams: s.ServiceParams,
	}

	// Process each wallet
	for _, w := range wallets {
		s.Logger.Debugw("processing wallet for alert check",
			"wallet_id", w.ID,
			"customer_id", w.CustomerID,
			"alert_enabled", w.AlertEnabled,
			"has_wallet_alert_config", w.AlertConfig != nil,
			"event_id", req.ID,
		)
		balance, err := s.GetWalletBalanceV2(ctx, w.ID)
		if err != nil {
			s.Logger.Errorw("failed to get wallet balance, skipping wallet",
				"error", err,
				"wallet_id", w.ID,
				"event_id", req.ID,
			)
			continue
		}

		// Skip if alerts are disabled for this wallet
		if !w.AlertEnabled {
			s.Logger.Debugw("skipping wallet - alerts disabled",
				"wallet_id", w.ID,
				"alert_enabled", w.AlertEnabled,
				"event_id", req.ID,
			)
			// Trigger auto top-up if enabled
			err := s.checkAutoTopup(ctx, w, lo.FromPtr(balance.RealTimeCreditBalance))
			if err != nil {
				s.Logger.Errorw("failed to trigger auto top-up",
					"error", err,
					"wallet_id", w.ID,
				)
			}
			continue
		}

		// Determine threshold: wallet-level config takes precedence over tenant-level settings
		var threshold decimal.Decimal
		if w.AlertConfig != nil && w.AlertConfig.Threshold != nil {
			// Use wallet-level threshold
			threshold = w.AlertConfig.Threshold.Value
		} else {
			// Fall back to tenant-level settings (GetSetting handles defaults automatically)
			walletAlertConfig, err := GetSetting[types.AlertConfig](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
			if err != nil {
				s.Logger.Errorw("failed to get wallet alert config from tenant settings, skipping wallet",
					"error", err,
					"wallet_id", w.ID,
					"event_id", req.ID,
				)
				continue
			}

			threshold = walletAlertConfig.Threshold.Value
		}

		// GetWalletBalanceV2 returns:
		// - RealTimeBalance: currency balance minus pending charges (in currency)
		// - RealTimeCreditBalance: credit balance converted from RealTimeBalance (in credits) - THIS IS THE ONGOING BALANCE
		// - Wallet.CreditBalance: stored credit balance (in credits)
		// - Wallet.Balance: stored currency balance (in currency)
		// - CurrentPeriodUsage: pending charges in currency

		// For ongoing balance alert: threshold is in credits, so use RealTimeCreditBalance directly
		// RealTimeCreditBalance is already calculated as: (currency balance - pending charges) / conversion_rate
		ongoingBalance := lo.FromPtr(balance.RealTimeCreditBalance)

		s.Logger.Infow("wallet balance details for alert check",
			"wallet_id", w.ID,
			"real_time_balance", balance.RealTimeBalance,
			"wallet_current_balance", balance.Wallet.Balance,
			"threshold", threshold,
			"pending_charges_currency", balance.CurrentPeriodUsage,
			"conversion_rate", w.ConversionRate,
			"event_id", req.ID,
		)

		// Check feature alerts
		for _, feature := range featuresWithAlerts {
			// Determine alert status based on ongoing balance vs alert settings
			alertStatus, err := feature.AlertSettings.AlertState(ongoingBalance)
			if err != nil {
				s.Logger.Errorw("failed to determine alert status",
					"feature_id", feature.ID,
					"feature_name", feature.Name,
					"wallet_id", w.ID,
					"ongoing_balance", ongoingBalance,
					"error", err,
				)
				continue
			}

			s.Logger.Debugw("feature alert status determined",
				"feature_id", feature.ID,
				"feature_name", feature.Name,
				"wallet_id", w.ID,
				"ongoing_balance", ongoingBalance,
				"critical", feature.AlertSettings.Critical,
				"warning", feature.AlertSettings.Warning,
				"alert_enabled", feature.AlertSettings.AlertEnabled,
				"alert_status", alertStatus,
			)

			// Log the alert using AlertLogsService (includes state transition logic and webhook publishing)
			// Get customer ID from wallet if available
			var customerID *string
			if w.CustomerID != "" {
				customerID = lo.ToPtr(w.CustomerID)
			}

			err = alertLogsService.LogAlert(ctx, &LogAlertRequest{
				EntityType:       types.AlertEntityTypeFeature,
				EntityID:         feature.ID,
				ParentEntityType: lo.ToPtr("wallet"), // Parent entity is the wallet
				ParentEntityID:   lo.ToPtr(w.ID),     // Wallet ID as parent entity ID
				CustomerID:       customerID,         // Customer ID from wallet
				AlertType:        types.AlertTypeFeatureWalletBalance,
				AlertStatus:      alertStatus,
				AlertInfo: types.AlertInfo{
					AlertSettings: feature.AlertSettings, // Include full alert settings
					ValueAtTime:   ongoingBalance,        // Ongoing balance at time of check
					Timestamp:     time.Now().UTC(),
				},
			})
			if err != nil {
				s.Logger.Errorw("failed to check feature alert",
					"feature_id", feature.ID,
					"wallet_id", w.ID,
					"alert_status", alertStatus,
					"error", err,
				)
				continue
			}

			s.Logger.Debugw("feature alert check completed",
				"feature_id", feature.ID,
				"wallet_id", w.ID,
				"alert_status", alertStatus,
			)
		}

		// Ongoing balance alert check
		// Check if ongoing balance is <= threshold (below or equal triggers alert)
		isOngoingBalanceBelowOrEqualThreshold := ongoingBalance.LessThanOrEqual(threshold)

		// Determine alert status based on balance check
		var alertStatus types.AlertState
		if isOngoingBalanceBelowOrEqualThreshold {
			alertStatus = types.AlertStateInAlarm
		} else {
			alertStatus = types.AlertStateOk
		}

		s.Logger.Infow("ongoing balance alert check - determined status",
			"wallet_id", w.ID,
			"ongoing_balance", ongoingBalance,
			"threshold", threshold,
			"is_below_or_equal_threshold", isOngoingBalanceBelowOrEqualThreshold,
			"alert_status", alertStatus,
			"event_id", req.ID,
		)

		// Get customer ID from wallet if available
		var customerID *string
		if w.CustomerID != "" {
			customerID = lo.ToPtr(w.CustomerID)
		}

		// Prepare alert log request
		logAlertReq := &LogAlertRequest{
			EntityType:  types.AlertEntityTypeWallet,
			EntityID:    w.ID,
			CustomerID:  customerID,
			AlertType:   types.AlertTypeLowOngoingBalance,
			AlertStatus: alertStatus,
			AlertInfo: types.AlertInfo{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: threshold,
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(true),
				},
				ValueAtTime: ongoingBalance,
				Timestamp:   time.Now().UTC(),
			},
		}

		// Log the alert (AlertLogsService handles state transitions and webhook publishing)
		err = alertLogsService.LogAlert(ctx, logAlertReq)
		if err != nil {
			s.Logger.Errorw("failed to log ongoing balance alert",
				"error", err,
				"wallet_id", w.ID,
				"alert_type", types.AlertTypeLowOngoingBalance,
				"alert_status", alertStatus,
				"ongoing_balance", ongoingBalance,
				"threshold", threshold,
				"event_id", req.ID,
			)
			continue
		}

		s.Logger.Infow("successfully logged ongoing balance alert",
			"wallet_id", w.ID,
			"alert_status", alertStatus,
			"ongoing_balance", ongoingBalance,
			"threshold", threshold,
			"event_id", req.ID,
		)

		// Update wallet alert state to match the logged status (if it changed)
		// Need to refetch wallet to get current AlertState
		currentWallet, err := s.WalletRepo.GetWalletByID(ctx, w.ID)
		if err != nil {
			s.Logger.Errorw("failed to get wallet for alert state update",
				"error", err,
				"wallet_id", w.ID,
				"event_id", req.ID,
			)
			continue
		}

		if currentWallet.AlertState != string(alertStatus) {
			s.Logger.Debugw("updating wallet alert state",
				"wallet_id", w.ID,
				"old_state", currentWallet.AlertState,
				"new_state", alertStatus,
				"event_id", req.ID,
			)

			if err := s.UpdateWalletAlertState(ctx, w.ID, alertStatus); err != nil {
				s.Logger.Errorw("failed to update wallet alert state",
					"error", err,
					"wallet_id", w.ID,
					"old_state", currentWallet.AlertState,
					"new_state", alertStatus,
					"event_id", req.ID,
				)
				continue
			}

			s.Logger.Infow("wallet alert state updated successfully",
				"wallet_id", w.ID,
				"old_state", currentWallet.AlertState,
				"new_state", alertStatus,
				"event_id", req.ID,
			)
		} else {
			s.Logger.Debugw("wallet alert state unchanged, skipping update",
				"wallet_id", w.ID,
				"current_state", currentWallet.AlertState,
				"event_id", req.ID,
			)
		}

		s.Logger.Infow("wallet ongoing balance alert check completed",
			"wallet_id", w.ID,
			"alert_status", alertStatus,
			"event_id", req.ID,
		)

		// Check auto top-up
		err = s.checkAutoTopup(ctx, w, lo.FromPtr(balance.RealTimeCreditBalance))
		if err != nil {
			s.Logger.Errorw("failed to trigger auto top-up",
				"error", err,
				"wallet_id", w.ID,
			)
			continue
		}
	}
	s.Logger.Infow("completed wallet balance alert check for customer",
		"customer_id", req.CustomerID,
		"wallets_processed", len(wallets),
		"event_id", req.ID,
	)

	return nil
}

func (s *walletService) PublishWalletBalanceAlertEvent(ctx context.Context, customerID string, forceCalculateBalance bool, walletID string) {

	walletBalanceAlertService := NewWalletBalanceAlertService(s.ServiceParams)

	event := &wallet.WalletBalanceAlertEvent{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT),
		Timestamp:             time.Now().UTC(),
		Source:                EventSourceWalletTransaction,
		CustomerID:            customerID,
		ForceCalculateBalance: forceCalculateBalance,
		TenantID:              types.GetTenantID(ctx),
		EnvironmentID:         types.GetEnvironmentID(ctx),
		WalletID:              walletID,
	}

	err := walletBalanceAlertService.PublishEvent(ctx, event)
	if err != nil {
		s.Logger.Errorw("failed to publish wallet balance alert event",
			"error", err,
			"customer_id", customerID,
			"force_calculate_balance", forceCalculateBalance,
		)
	}
}

// checkAutoTopup checks if auto top-up is enabled and triggers it if needed
func (s *walletService) checkAutoTopup(ctx context.Context, w *wallet.Wallet, ongoingBalance decimal.Decimal) error {

	if w.AutoTopup == nil || w.AutoTopup.Enabled == nil || !*w.AutoTopup.Enabled {
		return ierr.NewError("auto top-up is not enabled").
			WithHint("Auto top-up is not enabled").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": w.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Check if ongoing balance is below threshold
	if ongoingBalance.LessThanOrEqual(*w.AutoTopup.Threshold) {
		// Top up wallet
		_, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
			CreditsToAdd:      *w.AutoTopup.Amount, // treat auto-topup amount as credits
			Amount:            *w.AutoTopup.Amount,
			TransactionReason: lo.Ternary(w.AutoTopup.Invoicing != nil && *w.AutoTopup.Invoicing, types.TransactionReasonPurchasedCreditInvoiced, types.TransactionReasonPurchasedCreditDirect),
			IdempotencyKey:    lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION)),
			Description:       "Auto top-up triggered for low ongoing balance",
			Metadata:          types.Metadata{"auto_topup": "true"},
		})
		if err != nil {
			s.Logger.Errorw("failed to top up wallet for auto top-up",
				"error", err,
				"wallet_id", w.ID,
				"auto_topup_threshold", *w.AutoTopup.Threshold,
				"auto_topup_amount", *w.AutoTopup.Amount,
			)
		}
		s.Logger.Debugw("auto top-up triggered",
			"wallet_id", w.ID,
			"auto_topup_threshold", *w.AutoTopup.Threshold,
			"auto_topup_amount", *w.AutoTopup.Amount,
		)
	}

	s.Logger.Infow("auto top-up completed",
		"wallet_id", w.ID,
		"auto_topup_threshold", *w.AutoTopup.Threshold,
		"auto_topup_amount", *w.AutoTopup.Amount,
	)

	return nil
}
