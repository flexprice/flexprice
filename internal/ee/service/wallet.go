package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/postgres"
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

	// GetWalletBalanceFromCache retrieves wallet balance from cache
	// maxLiveSeconds controls cache staleness: if non-nil, cached entries older than this are skipped
	GetWalletBalanceFromCache(ctx context.Context, walletID string, maxLiveSeconds *int64) (*dto.WalletBalanceResponse, error)

	// TerminateWallet terminates a wallet by closing it and debiting remaining balance
	TerminateWallet(ctx context.Context, walletID string) error

	// UpdateWallet updates a wallet
	UpdateWallet(ctx context.Context, id string, req *dto.UpdateWalletRequest) (*wallet.Wallet, error)

	// ModifyWallet modifies a wallet
	ModifyWallet(ctx context.Context, id string, req *dto.ModifyWalletRequest) (*dto.WalletModificationResponse, error)

	// DebitWallet processes a debit operation on a wallet
	DebitWallet(ctx context.Context, req *wallet.WalletOperation) error

	// ManualBalanceDebit processes a manual balance debit operation on a wallet
	ManualBalanceDebit(ctx context.Context, walletID string, req *dto.ManualBalanceDebitRequest) (*dto.WalletResponse, error)

	// CreditWallet processes a credit operation on a wallet
	CreditWallet(ctx context.Context, req *wallet.WalletOperation) error

	// ExpireCredits expires credits for a given transaction. Returns result with Expired or SkipReason (active_subscription, active_invoice).
	ExpireCredits(ctx context.Context, transactionID string) (*types.ExpireCreditsResult, error)

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
	PublishEvent(ctx context.Context, eventName types.WebhookEventName, w *wallet.Wallet) error

	// CheckBalanceThresholds checks if wallet balance is below threshold and triggers alerts
	CheckBalanceThresholds(ctx context.Context, w *wallet.Wallet, balance *dto.WalletBalanceResponse) error

	// TopUpWalletForProratedCharge tops up a wallet for proration credits from subscription changes.
	// idempotencyKey should be a stable string derived from the change (e.g. lineItemID + effectiveDate)
	// to prevent duplicate credits on retries.
	TopUpWalletForProratedCharge(ctx context.Context, customerID string, amount decimal.Decimal, currency string, idempotencyKey string) (*dto.WalletTransactionResponse, error)

	// CompletePurchasedCreditTransaction completes a pending wallet transaction when payment succeeds
	CompletePurchasedCreditTransactionWithRetry(ctx context.Context, walletTransactionID string) error

	// TODO: Cleanup this method, moved to `EvaluateAlertsForWallet`
	CheckWalletBalanceAlert(ctx context.Context, req *wallet.WalletBalanceAlertEvent) error

	// PublishWalletBalanceAlertEvent publishes a wallet balance alert event
	PublishWalletBalanceAlertEvent(ctx context.Context, customerID string, forceCalculateBalance bool, walletID string)

	// GetCreditsAvailableBreakdown retrieves the breakdown of available credits by type (purchased, free, other)
	GetCreditsAvailableBreakdown(ctx context.Context, walletID string) (*types.CreditBreakdown, error)

	// EvaluateAlertsForWallet runs the full per-wallet alert dance for a
	// single wallet: resolve alert settings, short-circuit if nothing is
	// configured (no wallet alert, no feature alerts, no auto-topup), fetch
	// real-time balance, then run wallet / feature / auto-topup handlers in
	// that order. features is the shared result of FetchFeaturesWithAlertSettings
	// (nil / empty is fine). Per-step failures are logged and skipped so a
	// single bad wallet handler doesn't block the next; only fatal setup
	// errors return.
	//
	// autoTopupIdempotencySeed, when non-empty, is combined with the wallet ID
	// to form a stable idempotency key for the auto-topup call. Callers driving
	// this from a retryable context (Temporal activity) must pass a seed that
	// is constant across retries of the same logical evaluation. Empty seed
	// preserves the legacy fresh-UUID-per-call behavior.
	EvaluateAlertsForWallet(ctx context.Context, w *wallet.Wallet, alertLogs AlertLogsService, autoTopupIdempotencySeed string) error
}

// walletBalanceComputeTimeout bounds the realtime-balance computation in
// GetWalletBalanceV2 / GetWalletBalanceFromCache. On exceedance the wrapper
// falls back to the last cached balance (if any), otherwise surfaces the
// timeout as the original error.
const walletBalanceComputeTimeout = 80 * time.Second

type walletService struct {
	ServiceParams
	idempGen *idempotency.Generator

	// Test seams. Defaulted in NewWalletService; Tests in package service may set them via type assertion.
	computeBalanceTimeout  time.Duration
	computeRealtimeBalance func(ctx context.Context, w *wallet.Wallet) (*dto.WalletBalanceResponse, error)
}

// NewWalletService creates a new instance of WalletService
func NewWalletService(params ServiceParams) WalletService {
	s := &walletService{
		ServiceParams: params,
		idempGen:      idempotency.NewGenerator(),
	}

	// Test seams. Production code must not override these.
	s.computeBalanceTimeout = walletBalanceComputeTimeout
	s.computeRealtimeBalance = s.computeRealtimeBalanceDefault
	return s
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

	// Convert to domain wallet model
	w := req.ToWallet(ctx)

	for _, existing := range existingWallets {
		if existing.WalletStatus == types.WalletStatusActive && existing.Currency == w.Currency && existing.WalletType == w.WalletType {
			return nil, ierr.NewError("customer already has an active wallet with the same currency and wallet type").
				WithHint("A customer can only have one active wallet per currency and wallet type").
				WithReportableDetails(map[string]interface{}{
					"customer_id": req.CustomerID,
					"wallet_id":   existing.ID,
					"currency":    w.Currency,
					"wallet_type": w.WalletType,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}

	// create a DB transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create wallet in DB and update the wallet object
		if err := s.WalletRepo.CreateWallet(ctx, w); err != nil {
			return err // Repository already using ierr
		}
		response = dto.FromWallet(w)

		s.Logger.Debug(ctx, "created wallet",
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
		s.Logger.Error(ctx, "failed to get customers for wallet transactions",
			"error", err,
			"customer_ids", customerIDs)
		return nil
	}

	// Create map for quick lookup
	customersByID := make(map[string]*dto.CustomerResponse, len(customersResponse.Items))
	for _, cust := range customersResponse.Items {
		customersByID[cust.Customer.ID] = cust
	}

	s.Logger.Debug(ctx, "fetched customers for wallet transactions", "count", len(customersResponse.Items))
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
	userService := NewUserService(s.UserRepo, s.TenantRepo, nil, nil, nil, nil, nil, nil, s.Logger)
	userFilter := &types.UserFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		UserIDs:     userIDs,
	}

	usersResponse, err := userService.ListUsersByFilter(ctx, userFilter)
	if err != nil {
		s.Logger.Error(ctx, "failed to get users for wallet transactions",
			"error", err,
			"user_ids", userIDs)
		return nil
	}

	// Create map for quick lookup
	usersByID := make(map[string]*dto.UserResponse, len(usersResponse.Items))
	for _, user := range usersResponse.Items {
		usersByID[user.ID] = user
	}

	s.Logger.Debug(ctx, "fetched users for wallet transactions", "count", len(usersResponse.Items))
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
		s.Logger.Error(ctx, "failed to get wallets for wallet transactions",
			"error", err,
			"wallet_ids", walletIDs)
		return nil
	}

	// Create map for quick lookup
	walletsByID := make(map[string]*dto.WalletResponse, len(walletsResponse.Items))
	for _, w := range walletsResponse.Items {
		walletsByID[w.ID] = dto.FromWallet(w)
	}

	s.Logger.Debug(ctx, "fetched wallets for wallet transactions", "count", len(walletsResponse.Items))
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
		req.CreditsToAdd = s.GetCreditsFromCurrencyAmount(req.Amount, w.TopupConversionRate)
	}

	// Create a credit operation
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid top up wallet request").
			Mark(ierr.ErrValidation)
	}

	// Resolve bonus credits from the tenant's slab config when the caller didn't pass an
	// explicit override. Only "purchased" (paid) top-up reasons trigger slab resolution;
	// req.BonusCreditsToAdd is mutated in place so every downstream step just reads it.
	if req.BonusCreditsToAdd == nil &&
		(req.TransactionReason == types.TransactionReasonPurchasedCreditDirect ||
			req.TransactionReason == types.TransactionReasonPurchasedCreditInvoiced) {
		settingsSvc := &settingsService{ServiceParams: s.ServiceParams}
		bonusCfg, err := GetSetting[types.BonusCreditsTopupConfig](settingsSvc, ctx, types.SettingKeyBonusCreditsTopupConfig)
		if err != nil {
			return nil, err
		}
		if bonusCfg.Enabled {
			if slab := findBonusSlab(bonusCfg.Slabs, req.CreditsToAdd); slab != nil {
				req.BonusCreditsToAdd = lo.ToPtr(resolveBonusCredits(slab, req.CreditsToAdd))
			}
		}
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

		s.Logger.Debug(ctx, "created pending credit purchase with invoice",
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
		ExpiryDateTime:    req.ExpiryDateUTC, // ExpiryDateUTC is the preferred input: a full-precision timestamp
		IdempotencyKey:    idempotencyKey,
		Priority:          req.Priority,
		BonusCreditAmount: req.BonusCreditsToAdd,
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

// findBonusSlab returns the highest-threshold slab that credits clears. Requires slabs sorted
// descending by Threshold (enforced by BonusCreditsTopupConfig.Validate) — first match wins.
func findBonusSlab(slabs []types.BonusCreditsSlab, credits decimal.Decimal) *types.BonusCreditsSlab {
	for i := range slabs {
		if slabs[i].Operator != types.GREATER_THAN_EQUAL {
			continue
		}
		if credits.GreaterThanOrEqual(slabs[i].Threshold) {
			return &slabs[i]
		}
	}
	return nil
}

// resolveBonusCredits turns a matched slab into an actual credit amount.
func resolveBonusCredits(slab *types.BonusCreditsSlab, creditsToAdd decimal.Decimal) decimal.Decimal {
	switch slab.Bonus.Type {
	case types.BonusValueTypeFlat:
		return slab.Bonus.Value
	case types.BonusValueTypePercentage:
		return creditsToAdd.Mul(slab.Bonus.Value).Div(decimal.NewFromInt(100))
	default:
		return decimal.Zero
	}
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

	s.Logger.Debug(ctx, "processing purchased credit transaction",
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
		var description string

		if autoCompleteEnabled {
			// If auto-complete is enabled, create transaction as COMPLETED
			txStatus = types.TransactionStatusCompleted
			balanceAfter = w.CreditBalance.Add(req.CreditsToAdd)
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
			Amount:              s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.TopupConversionRate),
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
			Currency:            w.Currency,
			TopupConversionRate: lo.ToPtr(w.TopupConversionRate),
			ExpiryDate:          lo.Ternary(req.ExpiryDateUTC != nil, req.ExpiryDateUTC, types.ParseYYYYMMDDToDate(req.ExpiryDate)),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		// Compute credits available for the transaction
		tx.CreditsAvailable, err = tx.ComputeCreditsAvailable()
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to compute credits available").
				Mark(ierr.ErrInternal)
		}

		// Create the transaction
		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return err
		}

		walletTransactionID = tx.ID

		// Create the bonus grant, sharing the purchase tx's fate: same status (pending or
		// completed), completed atomically with the purchase when the purchase completes here.
		var bonusAmountInCurrency decimal.Decimal
		if req.BonusCreditsToAdd != nil && req.BonusCreditsToAdd.GreaterThan(decimal.Zero) {
			bonusCreditBalanceBefore := balanceAfter
			bonusCreditBalanceAfter := balanceAfter
			if autoCompleteEnabled {
				bonusCreditBalanceAfter = balanceAfter.Add(*req.BonusCreditsToAdd)
			}

			bonusAmountInCurrency = s.GetCurrencyAmountFromCredits(*req.BonusCreditsToAdd, w.TopupConversionRate)

			bonusTx := &wallet.Transaction{
				ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:            walletID,
				CustomerID:          w.CustomerID,
				Type:                types.TransactionTypeCredit,
				CreditAmount:        *req.BonusCreditsToAdd,
				Amount:              bonusAmountInCurrency,
				TxStatus:            txStatus, // shares the purchase tx's status: pending or completed
				ReferenceType:       types.WalletTxReferenceTypeExternal,
				ReferenceID:         lo.FromPtr(idempotencyKey),
				Description:         "Bonus credits for purchase",
				Metadata:            txMetadata,
				TransactionReason:   types.TransactionReasonPurchasedCreditBonus,
				ParentTransactionID: tx.ID,
				EnvironmentID:       w.EnvironmentID,
				CreditBalanceBefore: bonusCreditBalanceBefore,
				CreditBalanceAfter:  bonusCreditBalanceAfter,
				Currency:            w.Currency,
				TopupConversionRate: lo.ToPtr(w.TopupConversionRate),
				BaseModel:           types.GetDefaultBaseModel(ctx),
			}

			bonusTx.CreditsAvailable, err = bonusTx.ComputeCreditsAvailable()
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to compute credits available for bonus transaction").
					Mark(ierr.ErrInternal)
			}

			if err := s.WalletRepo.CreateTransaction(ctx, bonusTx); err != nil {
				return err
			}

			if autoCompleteEnabled {
				balanceAfter = bonusCreditBalanceAfter
			}
		}

		// If auto-complete is enabled, update wallet balance immediately (purchase + bonus, if any)
		if autoCompleteEnabled {
			finalBalance := w.Balance.Add(tx.Amount).Add(bonusAmountInCurrency)
			if err := s.WalletRepo.UpdateWalletBalance(ctx, walletID, finalBalance, balanceAfter); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update wallet balance").
					Mark(ierr.ErrInternal)
			}

			s.Logger.Info(ctx, "auto-completed wallet credit transaction",
				"wallet_transaction_id", tx.ID,
				"wallet_id", walletID,
				"credits_added", req.CreditsToAdd.String(),
				"new_credit_balance", balanceAfter.String(),
			)
		}

		// Step 2: Create invoice for credit purchase with wallet_transaction_id in metadata
		amount := s.GetCurrencyAmountFromCredits(req.CreditsToAdd, w.TopupConversionRate)
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

		invReq := dto.CreateInvoiceRequest{
			CustomerID:     w.CustomerID,
			AmountDue:      amount,
			AmountPaid:     amountPaid,
			Subtotal:       amount,
			Total:          amount,
			Currency:       w.Currency,
			InvoiceType:    types.InvoiceTypeOneOff,
			DueDate:        lo.ToPtr(time.Now().UTC()),
			IdempotencyKey: idempotencyKey,
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					Amount:      amount,
					Quantity:    decimal.NewFromInt(1),
					DisplayName: lo.ToPtr(fmt.Sprintf("Purchase %s Credits", req.CreditsToAdd.String())),
				},
			},
			PaymentStatus: lo.ToPtr(paymentStatus),
			Metadata:      invoiceMetadata,
			BillingReason: req.BillingReason,
		}
		// ForceSyncInvoice is omitted
		inv, err := invoiceService.CreateOneOffInvoice(ctx, invReq)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create invoice for purchased credits").
				Mark(ierr.ErrInternal)
		}

		invoiceID = inv.ID

		if autoCompleteEnabled {
			s.Logger.Info(ctx, "created auto-completed credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", inv.ID,
				"wallet_id", walletID,
				"credits", req.CreditsToAdd.String(),
				"amount", amount.String(),
				"payment_status", paymentStatus,
			)
		} else {
			s.Logger.Info(ctx, "created pending credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", inv.ID,
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

	// Synchronous Moyasar sync (if requested) runs here, outside the DB transaction above,
	// since it involves real network calls (invoice creation + optional token charge).
	// Best-effort: never fails the top-up.
	if req.ForceSyncInvoice {
		if err := invoiceService.SyncInvoiceToMoyasarIfEnabled(ctx, invoiceID); err != nil {
			s.Logger.Error(ctx, "force sync to Moyasar failed",
				"error", err, "invoice_id", invoiceID, "wallet_id", walletID)
		}
	}

	// If auto-completed, publish webhook event immediately
	if autoCompleteEnabled {
		s.invalidateWalletRealtimeBalanceCache(ctx, walletID)
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
				s.Logger.Info(ctx, "successfully completed purchased credit transaction after retry",
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
			s.Logger.Debug(ctx, "failed to complete purchased credit transaction, retrying",
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
	s.Logger.Error(ctx, "failed to complete purchased credit transaction after all retries",
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
		s.Logger.Debug(ctx, "wallet transaction is not pending",
			"wallet_transaction_id", walletTransactionID,
			"current_status", tx.TxStatus,
		)
		// If already completed (e.g., via auto-complete setting), this is idempotent - return success
		if tx.TxStatus == types.TransactionStatusCompleted {
			s.Logger.Debug(ctx, "wallet transaction already completed",
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

		// Compute credits available for the transaction
		tx.CreditsAvailable, err = tx.ComputeCreditsAvailable()
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to compute credits available").
				Mark(ierr.ErrInternal)
		}

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

		s.Logger.Info(ctx, "completed purchased credit transaction",
			"wallet_transaction_id", walletTransactionID,
			"wallet_id", tx.WalletID,
			"credits_added", tx.CreditAmount.String(),
			"new_balance", newCreditBalance.String(),
		)

		// Complete the pending bonus grant earned from this purchase, in the same transaction.
		bonusTx, err := s.WalletRepo.GetPendingTransactionByParent(ctx, tx.ID)
		if err != nil && !ierr.IsNotFound(err) {
			return err
		}
		if bonusTx != nil {
			bonusTx.TxStatus = types.TransactionStatusCompleted
			bonusTx.CreditBalanceBefore = newCreditBalance
			bonusTx.CreditBalanceAfter = newCreditBalance.Add(bonusTx.CreditAmount)
			if bonusTx.CreditsAvailable, err = bonusTx.ComputeCreditsAvailable(); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to compute credits available for bonus transaction").
					Mark(ierr.ErrInternal)
			}
			bonusTx.UpdatedAt = time.Now().UTC()

			if err := s.WalletRepo.UpdateTransaction(ctx, bonusTx); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update bonus wallet transaction").
					Mark(ierr.ErrInternal)
			}

			bonusFinalBalance := finalBalance.Add(bonusTx.Amount)
			if err := s.WalletRepo.UpdateWalletBalance(ctx, bonusTx.WalletID, bonusFinalBalance, bonusTx.CreditBalanceAfter); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update wallet balance for bonus transaction").
					Mark(ierr.ErrInternal)
			}

			s.Logger.Info(ctx, "completed linked bonus credit transaction",
				"wallet_transaction_id", bonusTx.ID,
				"parent_transaction_id", tx.ID,
				"wallet_id", bonusTx.WalletID,
				"bonus_credits_added", bonusTx.CreditAmount.String(),
			)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Publish webhook event after transaction commits
	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionUpdated, tx.ID)

	// Log credit balance alert after transaction completes
	if err := s.logCreditBalanceAlert(ctx, w, w.CreditBalance.Add(tx.CreditAmount)); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.Error(ctx, "failed to log credit balance alert after completing purchased credit transaction",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	// Re-trigger wallet balance alert so triggerAutoTopup can fire a new invoice if
	// the balance is still below threshold after payment. The previous invoice is now
	// SUCCEEDED so the guard in triggerAutoTopup will allow a fresh one.
	// This is async (Kafka) and non-fatal.
	s.PublishWalletBalanceAlertEvent(ctx, tx.CustomerID, true, tx.WalletID)

	return nil
}

// logCreditBalanceAlert logs a credit balance alert for a wallet after a balance change
func (s *walletService) logCreditBalanceAlert(ctx context.Context, w *wallet.Wallet, newCreditBalance decimal.Decimal) error {
	// Check credit balance alerts after wallet operation
	var alertSettings *types.AlertSettings
	var alertStatus types.AlertState

	// Get wallet alert settings or fall back to tenant-level settings
	if w.AlertSettings != nil {
		alertSettings = w.AlertSettings
	} else {
		// Fall back to tenant-level settings (GetSetting handles defaults automatically)
		settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
		walletAlertSettings, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
		if err != nil {
			s.Logger.Error(ctx, "failed to get wallet alert config from tenant settings",
				"error", err,
				"wallet_id", w.ID,
			)
			// Use default settings if tenant settings fetch fails
			alertSettings = &types.AlertSettings{
				Critical: &types.AlertThreshold{
					Threshold: decimal.NewFromFloat(0.0),
					Condition: types.AlertConditionBelow,
				},
				AlertEnabled: lo.ToPtr(true),
			}
		} else {
			alertSettings = &walletAlertSettings
		}
	}

	// Determine alert status based on balance vs alert settings
	var err error
	alertStatus, err = alertSettings.AlertState(newCreditBalance)
	if err != nil {
		s.Logger.Error(ctx, "failed to determine alert status",
			"error", err,
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
		)
		return err
	}

	// Create alert info
	alertInfo := types.AlertInfo{
		AlertSettings: alertSettings,
		ValueAtTime:   newCreditBalance,
		Timestamp:     time.Now().UTC(),
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
		s.Logger.Error(ctx, "failed to log credit balance alert",
			"error", err,
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
			"alert_settings", alertSettings,
			"alert_status", alertStatus,
		)
		return err
	}

	s.Logger.Info(ctx, "credit balance alert logged successfully",
		"wallet_id", w.ID,
		"new_credit_balance", newCreditBalance,
		"alert_settings", alertSettings,
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

	// POST_PAID wallets: balance doesn't deplete with usage, real-time balance = wallet balance
	if w.WalletType == types.WalletTypePostPaid {
		realTimeCreditBalance := s.GetCreditsFromCurrencyAmount(w.Balance, w.ConversionRate)
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(w.Balance),
			RealTimeCreditBalance: lo.ToPtr(realTimeCreditBalance),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
			UnpaidInvoicesAmount:  lo.ToPtr(decimal.Zero),
		}, nil
	}

	// PRE_PAID wallets: calculate pending usage charges that will consume prepaid balance
	var totalPendingCharges decimal.Decimal
	shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)

	if shouldIncludeUsage {
		// Get all active subscriptions to calculate current usage
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		subscriptions, err := subscriptionService.ListByCustomerID(ctx, w.CustomerID)
		if err != nil {
			return nil, err
		}

		// Filter subscriptions by currency
		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if sub.Currency != w.Currency {
				s.Logger.Info(ctx, "skipping subscription - currency mismatch")
				continue
			}
			if sub.SubscriptionType != types.SubscriptionTypeStandalone && sub.SubscriptionType != types.SubscriptionTypeParent {
				s.Logger.Info(ctx, "skipping subscription - not a standalone or parent subscription")
				continue
			}

			filteredSubscriptions = append(filteredSubscriptions, sub)
		}

		billingService := NewBillingService(s.ServiceParams)

		// Calculate total pending charges (usage)
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
			usageResult, err := billingService.CalculateUsageCharges(ctx, &dto.CalculateUsageChargesParams{
				Subscription: sub,
				Usage:        usage,
				PeriodStart:  periodStart,
				PeriodEnd:    periodEnd,
			})
			if err != nil {
				return nil, err
			}

			s.Logger.Debug(ctx, "subscription charges details",
				"subscription_id", sub.ID,
				"usage_total", usageResult.TotalAmount,
				"num_usage_charges", len(usageResult.LineItems))

			totalPendingCharges = totalPendingCharges.Add(usageResult.TotalAmount)
		}
	}

	// Get unpaid invoices for PRE_PAID wallets
	invoiceService := NewInvoiceService(s.ServiceParams)
	resp, err := invoiceService.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: w.CustomerID,
		Currency:   w.Currency,
	})
	if err != nil {
		return nil, err
	}

	if lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll) || lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeFixed) {
		totalPendingCharges = totalPendingCharges.Add(resp.TotalUnpaidAmount)
	} else {
		totalPendingCharges = totalPendingCharges.Add(resp.TotalUnpaidUsageCharges).Sub(resp.TotalPaidInvoiceAmount)
	}

	// Calculate real-time balance: wallet balance minus pending charges
	realTimeBalance := w.Balance.Sub(totalPendingCharges)

	s.Logger.Debug(ctx, "detailed balance calculation",
		"wallet_id", w.ID,
		"wallet_type", w.WalletType,
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

	// Update alert settings if provided
	if req.AlertSettings != nil {
		// Update wallet alert settings
		existing.AlertSettings = req.AlertSettings

		// If alerts are being disabled (AlertSettings.AlertEnabled = false), reset state
		if !req.AlertSettings.IsAlertEnabled() {
			existing.AlertState = types.AlertStateOk
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

func (s *walletService) ModifyWallet(ctx context.Context, id string, req *dto.ModifyWalletRequest) (*dto.WalletModificationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid wallet modification request").
			Mark(ierr.ErrValidation)
	}

	switch req.ModificationType {
	case dto.WalletModificationTypePrepaidToPostpaid:
		return s.convertToPostpaid(ctx, id)
	default:
		return nil, ierr.NewError("invalid modification type").
			WithHint("Invalid modification type").
			Mark(ierr.ErrValidation)
	}
}

// ConvertToPostpaid converts a prepaid wallet to a postpaid wallet.
// The operation is atomic and follows these steps:
// 1. Lock the wallet to prevent concurrent operations
// 2. Pick current balance from DB
// 3. Terminate the wallet (debit remaining balance, set status to closed)
// 4. Create a new postpaid wallet with AllowedPriceTypes set to ALL (fixed + usage)
// 5. Top up the credits from the prepaid wallet if any
// 6. All within a single database transaction
func (s *walletService) convertToPostpaid(ctx context.Context, id string) (*dto.WalletModificationResponse, error) {
	if id == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	var originalWallet *wallet.Wallet
	var newWallet *wallet.Wallet
	var creditsTransferred decimal.Decimal

	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Step 1: Acquire advisory lock for the wallet
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: id}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire wallet lock").
				Mark(ierr.ErrInternal)
		}

		// Step 2: Get wallet inside transaction (after acquiring lock)
		var err error
		originalWallet, err = s.WalletRepo.GetWalletByID(ctx, id)
		if err != nil {
			return err
		}

		// Validation: wallet is prepaid and active
		if originalWallet.WalletType != types.WalletTypePrePaid {
			return ierr.NewError("wallet is not a prepaid wallet").
				WithHint("Only prepaid wallets can be converted to postpaid").
				WithReportableDetails(map[string]interface{}{
					"wallet_id":   id,
					"wallet_type": originalWallet.WalletType,
				}).
				Mark(ierr.ErrInvalidOperation)
		}

		if originalWallet.WalletStatus != types.WalletStatusActive {
			return ierr.NewError("wallet is not active").
				WithHint("Only active wallets can be converted").
				WithReportableDetails(map[string]interface{}{
					"wallet_id":     id,
					"wallet_status": originalWallet.WalletStatus,
				}).
				Mark(ierr.ErrInvalidOperation)
		}

		// Validation: customer does not have an active postpaid wallet with the same currency
		existingWallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, originalWallet.CustomerID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check existing wallets before conversion").
				Mark(ierr.ErrDatabase)
		}
		for _, existing := range existingWallets {
			if existing.WalletStatus == types.WalletStatusActive &&
				existing.WalletType == types.WalletTypePostPaid &&
				existing.Currency == originalWallet.Currency {
				return ierr.NewError("customer already has an active postpaid wallet with the same currency").
					WithHint("A customer can only have one active postpaid wallet per currency").
					WithReportableDetails(map[string]interface{}{
						"customer_id": originalWallet.CustomerID,
						"wallet_id":   existing.ID,
						"currency":    originalWallet.Currency,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
		}

		// Step 3: Store the current credit balance before termination
		creditsTransferred = originalWallet.CreditBalance

		// Step 4: Terminate the wallet (debit remaining balance and close)
		if originalWallet.CreditBalance.GreaterThan(decimal.Zero) {
			debitReq := &wallet.WalletOperation{
				WalletID:          id,
				CreditAmount:      originalWallet.CreditBalance,
				Type:              types.TransactionTypeDebit,
				Description:       "Wallet conversion - prepaid to postpaid",
				TransactionReason: types.TransactionReasonWalletTermination,
				ReferenceType:     types.WalletTxReferenceTypeRequest,
				IdempotencyKey:    id + "_conversion_debit",
				ReferenceID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			}

			if err := s.DebitWallet(ctx, debitReq); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to debit prepaid wallet during conversion").
					Mark(ierr.ErrInternal)
			}
		}

		// Update wallet status to closed
		if err := s.WalletRepo.UpdateWalletStatus(ctx, id, types.WalletStatusClosed); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to close prepaid wallet").
				Mark(ierr.ErrDatabase)
		}

		// Update local state to reflect termination
		originalWallet.WalletStatus = types.WalletStatusClosed
		originalWallet.CreditBalance = decimal.Zero
		originalWallet.Balance = decimal.Zero

		// Step 6: Create a new postpaid wallet
		newWallet = &wallet.Wallet{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
			CustomerID:          originalWallet.CustomerID,
			Name:                "Postpaid Wallet - " + strings.ToUpper(originalWallet.Currency),
			Currency:            originalWallet.Currency,
			Description:         "Converted from prepaid wallet " + originalWallet.ID,
			Metadata:            types.Metadata{"converted_from": originalWallet.ID},
			Balance:             decimal.Zero,
			CreditBalance:       decimal.Zero,
			WalletStatus:        types.WalletStatusActive,
			WalletType:          types.WalletTypePostPaid,
			Config:              types.WalletConfig{AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}},
			ConversionRate:      originalWallet.ConversionRate,
			TopupConversionRate: originalWallet.TopupConversionRate,
			AlertSettings:       originalWallet.AlertSettings,
			AlertState:          types.AlertStateOk,
			AutoTopup:           originalWallet.AutoTopup,
			EnvironmentID:       types.GetEnvironmentID(ctx),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		if err := s.WalletRepo.CreateWallet(ctx, newWallet); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create postpaid wallet").
				Mark(ierr.ErrDatabase)
		}

		s.Logger.Info(ctx, "created postpaid wallet during conversion",
			"new_wallet_id", newWallet.ID,
			"original_wallet_id", originalWallet.ID,
			"customer_id", originalWallet.CustomerID,
		)

		// Step 7: Top up the new wallet with credits from the prepaid wallet
		if creditsTransferred.GreaterThan(decimal.Zero) {
			idempotencyKey := s.idempGen.GenerateKey(idempotency.ScopeCreditGrant, map[string]interface{}{
				"wallet_id":           newWallet.ID,
				"source_wallet_id":    originalWallet.ID,
				"credits_to_add":      creditsTransferred,
				"transaction_reason":  types.TransactionReasonFreeCredit,
				"conversion_transfer": true,
			})

			creditReq := &wallet.WalletOperation{
				WalletID:          newWallet.ID,
				CreditAmount:      creditsTransferred,
				Type:              types.TransactionTypeCredit,
				Description:       "Credits transferred from prepaid wallet " + originalWallet.ID,
				TransactionReason: types.TransactionReasonFreeCredit,
				ReferenceType:     types.WalletTxReferenceTypeRequest,
				IdempotencyKey:    idempotencyKey,
				ReferenceID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			}

			if err := s.CreditWallet(ctx, creditReq); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to transfer credits to postpaid wallet").
					Mark(ierr.ErrInternal)
			}

			// Update local state to reflect credit transfer
			newWallet.CreditBalance = newWallet.CreditBalance.Add(creditsTransferred)
			newWallet.Balance = s.GetCurrencyAmountFromCredits(newWallet.CreditBalance, newWallet.ConversionRate)

			s.Logger.Info(ctx, "transferred credits during wallet conversion",
				"new_wallet_id", newWallet.ID,
				"original_wallet_id", originalWallet.ID,
				"credits_transferred", creditsTransferred,
			)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Publish webhook events outside the transaction
	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletTerminated, originalWallet.ID)
	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletCreated, newWallet.ID)

	return &dto.WalletModificationResponse{
		OriginalWallet: dto.FromWallet(originalWallet),
		NewWallet:      dto.FromWallet(newWallet),
	}, nil
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
	// For credit operations (topup), use TopupConversionRate; for debit operations, use ConversionRate
	conversionRate := w.ConversionRate
	if req.Type == types.TransactionTypeCredit {
		conversionRate = w.TopupConversionRate
	}

	switch {
	case req.Amount.GreaterThan(decimal.Zero):
		// Amount provided - convert to credits
		req.CreditAmount = s.GetCreditsFromCurrencyAmount(req.Amount, conversionRate)

	case req.CreditAmount.GreaterThan(decimal.Zero):
		// CreditAmount already set - just convert to Amount
		req.Amount = s.GetCurrencyAmountFromCredits(req.CreditAmount, conversionRate)

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
func (s *walletService) processDebitOperation(ctx context.Context, req *wallet.WalletOperation) ([]*wallet.Transaction, error) {
	// Find eligible credits with pagination
	credits := []*wallet.Transaction{}
	var err error
	if req.ParentCreditTxID != "" {
		// Get the parent debit transaction
		parentCreditTx, err := s.WalletRepo.GetTransactionByID(ctx, req.ParentCreditTxID)
		if err != nil {
			return nil, err
		}
		credits = append(credits, parentCreditTx)

	} else {
		// Determine the time reference for finding eligible credits
		timeReference := time.Now().UTC()
		if req.InvoiceID != nil && *req.InvoiceID != "" {
			// Use invoice's period end as the time reference
			invoice, err := s.InvoiceRepo.Get(ctx, *req.InvoiceID)
			if err != nil {
				return nil, err
			}
			if invoice.PeriodEnd != nil {
				timeReference = lo.FromPtr(invoice.PeriodEnd)
			}
		}
		credits, err = s.WalletRepo.FindEligibleCredits(ctx, req.WalletID, req.CreditAmount, 100, timeReference)
		if err != nil {
			return nil, err
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
			return nil, ierr.NewError("insufficient balance").
				WithHint("Insufficient balance to process debit operation").
				WithReportableDetails(map[string]interface{}{
					"wallet_id": req.WalletID,
					"amount":    req.CreditAmount,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	// Process debit across credits
	consumedCredits, err := s.WalletRepo.ConsumeCredits(ctx, credits, req.CreditAmount)
	if err != nil {
		return nil, err
	}

	return consumedCredits, nil
}

// processWalletOperation handles both credit and debit operations
func (s *walletService) processWalletOperation(ctx context.Context, req *wallet.WalletOperation) error {
	s.Logger.Debug(ctx, "Processing wallet operation", "req", req)

	var w *wallet.Wallet
	var tx *wallet.Transaction
	var newCreditBalance decimal.Decimal
	var finalBalance decimal.Decimal

	metadata := make(types.Metadata)
	if req.Metadata != nil {
		metadata = req.Metadata
	}

	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Step 1: Acquire advisory lock for the wallet
		// This ensures only one operation on this wallet can proceed at a time across all servers
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: req.WalletID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire wallet lock").
				Mark(ierr.ErrInternal)
		}

		// Step 2: Get wallet inside transaction (after acquiring lock)
		// This ensures we read the latest committed state
		var err error
		w, err = s.WalletRepo.GetWalletByID(ctx, req.WalletID)
		if err != nil {
			return err
		}

		// Step 3: Validate operation
		if err := s.validateWalletOperation(w, req); err != nil {
			return err
		}

		// Step 4: Process operation-specific logic
		if req.Type == types.TransactionTypeDebit {
			newCreditBalance = w.CreditBalance.Sub(req.CreditAmount)
			// Process debit operation (credit selection and consumption)
			consumedCredits, err := s.processDebitOperation(ctx, req)
			if err != nil {
				return err
			}

			if len(consumedCredits) > 0 {
				consumedCreditsIDs := make([]string, 0)
				for _, c := range consumedCredits {
					consumedCreditsIDs = append(consumedCreditsIDs, c.ID)
				}

				metadata["consumed_credit_tx_ids"] = strings.Join(consumedCreditsIDs, ",")
			}
		} else {
			// Process credit operation
			newCreditBalance = w.CreditBalance.Add(req.CreditAmount)
		}

		finalBalance = s.GetCurrencyAmountFromCredits(newCreditBalance, w.ConversionRate)

		// Step 5: Create transaction record
		tx = &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            req.WalletID,
			CustomerID:          w.CustomerID,
			Type:                req.Type,
			Amount:              req.Amount,
			CreditAmount:        req.CreditAmount,
			ReferenceType:       req.ReferenceType,
			ReferenceID:         req.ReferenceID,
			Description:         req.Description,
			Metadata:            metadata,
			TxStatus:            types.TransactionStatusCompleted,
			TransactionReason:   req.TransactionReason,
			ExpiryDate:          req.ResolvedExpiryDate(),
			Priority:            req.Priority,
			CreditBalanceBefore: w.CreditBalance,
			CreditBalanceAfter:  newCreditBalance,
			Currency:            w.Currency,
			EnvironmentID:       types.GetEnvironmentID(ctx),
			IdempotencyKey:      req.IdempotencyKey,
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		// Compute credits available for the transaction
		tx.CreditsAvailable, err = tx.ComputeCreditsAvailable()
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to compute credits available").
				Mark(ierr.ErrInternal)
		}

		// Set transaction-specific fields based on transaction type
		if req.Type == types.TransactionTypeCredit {
			tx.TopupConversionRate = lo.ToPtr(w.TopupConversionRate)
		} else if req.Type == types.TransactionTypeDebit {
			tx.ConversionRate = lo.ToPtr(w.ConversionRate)
		}

		// Step 6: Create transaction record
		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return err
		}

		// Create the bonus grant earned from this purchase, in the same transaction, already
		// completed — no reason to round-trip through pending when the purchase completes here.
		if req.BonusCreditAmount != nil && req.BonusCreditAmount.GreaterThan(decimal.Zero) {
			bonusCreditBalanceBefore := newCreditBalance
			newCreditBalance = newCreditBalance.Add(*req.BonusCreditAmount)

			bonusTx := &wallet.Transaction{
				ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:            req.WalletID,
				CustomerID:          w.CustomerID,
				Type:                types.TransactionTypeCredit,
				Amount:              s.GetCurrencyAmountFromCredits(*req.BonusCreditAmount, w.TopupConversionRate),
				CreditAmount:        *req.BonusCreditAmount,
				Description:         "Bonus credits for purchase",
				Metadata:            metadata,
				TxStatus:            types.TransactionStatusCompleted,
				TransactionReason:   types.TransactionReasonPurchasedCreditBonus,
				ParentTransactionID: tx.ID,
				CreditBalanceBefore: bonusCreditBalanceBefore,
				CreditBalanceAfter:  newCreditBalance,
				Currency:            w.Currency,
				EnvironmentID:       types.GetEnvironmentID(ctx),
				TopupConversionRate: lo.ToPtr(w.TopupConversionRate),
				BaseModel:           types.GetDefaultBaseModel(ctx),
			}
			bonusTx.CreditsAvailable, err = bonusTx.ComputeCreditsAvailable()
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to compute credits available for bonus transaction").
					Mark(ierr.ErrInternal)
			}
			if err := s.WalletRepo.CreateTransaction(ctx, bonusTx); err != nil {
				return err
			}

			finalBalance = s.GetCurrencyAmountFromCredits(newCreditBalance, w.ConversionRate)
		}

		// Step 7: Update wallet balance atomically
		if err := s.WalletRepo.UpdateWalletBalance(ctx, req.WalletID, finalBalance, newCreditBalance); err != nil {
			return err
		}

		s.Logger.Debug(ctx, "Wallet operation completed")
		return nil
	})
	if err != nil {
		return err
	}

	// Publish webhook event after transaction commits
	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, tx.ID)

	// Log credit balance alert after wallet operation
	if err := s.logCreditBalanceAlert(ctx, w, newCreditBalance); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.Error(ctx, "failed to log credit balance alert after wallet operation",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	// Only the wallet we just changed can have moved its alert state, so drive
	// the per-wallet path directly instead of fanning out to every customer wallet.
	if err := s.EvaluateAlertsForWallet(ctx, w, NewAlertLogsService(s.ServiceParams), ""); err != nil {
		s.Logger.Error(ctx, "failed to evaluate wallet alerts after wallet operation",
			"error", err,
			"wallet_id", req.WalletID,
			"customer_id", w.CustomerID,
		)
	}

	return nil
}

// ExpireCredits expires credits for a given transaction
func (s *walletService) ExpireCredits(ctx context.Context, transactionID string) (*types.ExpireCreditsResult, error) {
	// Get the transaction
	tx, err := s.WalletRepo.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return nil, err
	}

	// Validate transaction
	if tx.Type != types.TransactionTypeCredit {
		return nil, ierr.NewError("can only expire credit transactions").
			WithHint("Only credit transactions can be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.ExpiryDate == nil {
		return nil, ierr.NewError("transaction has no expiry date").
			WithHint("Transaction must have an expiry date to be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.ExpiryDate.After(time.Now().UTC()) {
		return nil, ierr.NewError("transaction has not expired yet").
			WithHint("Transaction must have expired to be expired").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if tx.CreditsAvailable.IsZero() {
		return nil, ierr.NewError("no credits available to expire").
			WithHint("Transaction has no credits available to expire").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	skipReason, err := s.shouldSkipCreditExpiryDueToActiveSubscriptionOrInvoice(ctx, tx)
	if err != nil {
		return nil, err
	}
	if skipReason != types.CreditExpirySkipReasonNone {
		return &types.ExpireCreditsResult{Expired: false, SkipReason: skipReason}, nil
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
		return nil, err
	}

	return &types.ExpireCreditsResult{Expired: true}, nil
}

// shouldSkipCreditExpiryDueToActiveSubscriptionOrInvoice checks if there is any subscription or invoice
// for the customer with current_period_end/end time before now. If so, credit expiry should be skipped.
// It returns the skip reason when expiry should be skipped, CreditExpirySkipReasonNone when expiry can proceed, and err on error.
func (s *walletService) shouldSkipCreditExpiryDueToActiveSubscriptionOrInvoice(ctx context.Context, tx *wallet.Transaction) (types.CreditExpirySkipReason, error) {
	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = tx.CustomerID
	subFilter.Limit = lo.ToPtr(1)
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}

	// This is very important to only check for active subscriptions of type standalone and parent
	subFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeStandalone, types.SubscriptionTypeParent}
	subFilter.TimeRangeFilter = &types.TimeRangeFilter{
		EndTime: lo.ToPtr(time.Now().UTC()),
	}

	subscriptions, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return types.CreditExpirySkipReasonNone, err
	}
	if len(subscriptions) > 0 {
		s.Logger.Debug(ctx, "there is a subscription for this customer with current_period_end < now and credits available to expire",
			"transaction_id", tx.ID,
			"subscription_id", subscriptions[0].ID,
			"credits_available", tx.CreditsAvailable,
		)
		return types.CreditExpirySkipReasonActiveSubscription, nil
	}

	// Find invoices whose billing period contains the grant's created_at and whose period ended before grant expiry
	// (skip expiry if there is such an invoice - grant was created in that period and period is not "very before")
	invoiceFilter := types.NewInvoiceFilter()
	invoiceFilter.CustomerID = tx.CustomerID
	invoiceFilter.InvoiceType = types.InvoiceTypeSubscription
	invoiceFilter.Currency = tx.Currency // wallets are per-currency; an EUR invoice can't be paid by a USD wallet
	invoiceFilter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized, types.InvoiceStatusDraft}
	invoiceFilter.AmountRemainingGt = lo.ToPtr(decimal.Zero)
	invoiceFilter.Limit = lo.ToPtr(1)
	invoiceFilter.PeriodStartLTE = &tx.CreatedAt // period_start <= grant created_at
	invoiceFilter.PeriodEndGTE = &tx.CreatedAt   // period_end >= grant created_at → grant created in this period
	invoiceFilter.PeriodEndLTE = tx.ExpiryDate   // period_end <= grant expiry → exclude invoices that ended long after expiry

	invoices, err := s.InvoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return types.CreditExpirySkipReasonNone, err
	}
	if len(invoices) > 0 {
		s.Logger.Debug(ctx, "there is an invoice for this customer with current_period_end < now and credits available to expire",
			"transaction_id", tx.ID,
			"invoice_id", invoices[0].ID,
		)
		return types.CreditExpirySkipReasonActiveInvoice, nil
	}

	return types.CreditExpirySkipReasonNone, nil
}

func (s *walletService) publishInternalWalletWebhookEvent(ctx context.Context, eventName types.WebhookEventName, walletID string) {

	webhookPayload, err := json.Marshal(webhookDto.InternalWalletEvent{
		WalletID:  walletID,
		TenantID:  types.GetTenantID(ctx),
		EventType: eventName,
	})

	if err != nil {
		s.Logger.Error(ctx, "failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeWallet,
		EntityID:      walletID,
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish webhook event", "event_name", webhookEvent.EventName, "error", err)
	}
}

func (s *walletService) publishOngoingBalanceUpdatedWebhookEvent(ctx context.Context, walletID string, balance *dto.WalletBalanceResponse) {
	if s.WebhookPublisher == nil || balance == nil || balance.RealTimeCreditBalance == nil {
		return
	}

	webhookPayload, err := json.Marshal(webhookDto.InternalWalletEvent{
		WalletID:  walletID,
		Balance:   balance,
		TenantID:  types.GetTenantID(ctx),
		EventType: types.WebhookEventWalletOngoingBalanceUpdated,
	})
	if err != nil {
		s.Logger.Error(ctx, "failed to marshal ongoing balance webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     types.WebhookEventWalletOngoingBalanceUpdated,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeWallet,
		EntityID:      walletID,
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish ongoing balance webhook event", "error", err)
		return
	}
}

func (s *walletService) GetWalletTransactionByID(ctx context.Context, transactionID string) (*dto.WalletTransactionResponse, error) {
	tx, err := s.WalletRepo.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return nil, err
	}
	return dto.FromWalletTransaction(tx), nil
}

func (s *walletService) publishInternalTransactionWebhookEvent(ctx context.Context, eventName types.WebhookEventName, transactionID string) {

	webhookPayload, err := json.Marshal(webhookDto.InternalTransactionEvent{
		TransactionID: transactionID,
		TenantID:      types.GetTenantID(ctx),
	})

	if err != nil {
		s.Logger.Error(ctx, "failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeWallet,
		EntityID:      transactionID,
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish webhook event", "event_name", webhookEvent.EventName, "error", err)
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
			var balance *dto.WalletBalanceResponse
			var err error
			if req.FromCache {
				balance, err = s.GetWalletBalanceFromCache(ctx, w.ID, req.MaxLiveSeconds)
				if err != nil {
					return nil, err
				}
			} else {
				balance, err = s.GetWalletBalanceV2(ctx, w.ID)
				if err != nil {
					return nil, err
				}
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
	w.AlertState = state

	return s.WalletRepo.UpdateWallet(ctx, walletID, w)
}

// PublishEvent publishes a webhook event for a wallet
func (s *walletService) PublishEvent(ctx context.Context, eventName types.WebhookEventName, w *wallet.Wallet) error {
	if s.WebhookPublisher == nil {
		s.Logger.Info(ctx, "webhook publisher not initialized", "event", eventName)
		return nil
	}

	// Get real-time balance
	balance, err := s.GetWalletBalanceV2(ctx, w.ID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get wallet balance for webhook",
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
	if w.AlertSettings != nil {
		currentBalance := balance.RealTimeBalance
		if currentBalance == nil {
			currentBalance = &w.Balance
		}
		creditBalance := balance.RealTimeCreditBalance
		if creditBalance == nil {
			creditBalance = &w.CreditBalance
		}

		internalEvent.Alert = &webhookDto.WalletAlertInfo{
			State:          string(w.AlertState),
			CurrentBalance: *currentBalance,
			CreditBalance:  *creditBalance,
			AlertSettings:  w.AlertSettings,
			AlertType:      getAlertType(eventName),
		}

		s.Logger.Info(ctx, "added alert info to webhook event",
			"wallet_id", w.ID,
			"alert_state", w.AlertState,
			"alert_type", getAlertType(eventName),
			"alert_settings", w.AlertSettings,
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
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      w.TenantID,
		EnvironmentID: w.EnvironmentID,
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       eventJSON,
		EntityType:    types.SystemEntityTypeWallet,
		EntityID:      w.ID,
	}

	s.Logger.Info(ctx, "publishing webhook event",
		"event_id", webhookEvent.ID,
		"event_name", eventName,
		"wallet_id", w.ID,
		"alert_state", w.AlertState,
		"alert_settings", w.AlertSettings,
	)

	return s.WebhookPublisher.PublishWebhook(ctx, webhookEvent)
}

func getAlertType(eventName types.WebhookEventName) string {
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
	// Skip if alerts not enabled
	if !w.IsAlertEnabled() {
		return nil
	}

	alertSettings := w.AlertSettings
	currentBalance := balance.RealTimeBalance
	if currentBalance == nil {
		currentBalance = &w.Balance
	}
	creditBalance := balance.RealTimeCreditBalance
	if creditBalance == nil {
		creditBalance = &w.CreditBalance
	}

	s.Logger.Info(ctx, "checking balance thresholds",
		"wallet_id", w.ID,
		"alert_settings", alertSettings,
		"current_balance", currentBalance,
		"credit_balance", creditBalance,
		"alert_state", w.AlertState,
	)

	// Determine alert status for current balance
	currentBalanceAlertStatus, err := alertSettings.AlertState(*currentBalance)
	if err != nil {
		s.Logger.Error(ctx, "failed to determine current balance alert status",
			"wallet_id", w.ID,
			"error", err,
		)
		return err
	}

	// Determine alert status for credit balance
	creditBalanceAlertStatus, err := alertSettings.AlertState(*creditBalance)
	if err != nil {
		s.Logger.Error(ctx, "failed to determine credit balance alert status",
			"wallet_id", w.ID,
			"error", err,
		)
		return err
	}

	// Check if any balance triggered an alert (critical, warning, or info)
	isCurrentBalanceInAlert := currentBalanceAlertStatus != types.AlertStateOk
	isCreditBalanceInAlert := creditBalanceAlertStatus != types.AlertStateOk
	isAnyBalanceInAlert := isCurrentBalanceInAlert || isCreditBalanceInAlert

	// Handle balance recovery (all balances are OK)
	if !isAnyBalanceInAlert {
		s.Logger.Info(ctx, "all balances OK - checking recovery",
			"wallet_id", w.ID,
			"alert_settings", alertSettings,
			"current_balance", currentBalance,
			"credit_balance", creditBalance,
			"alert_state", w.AlertState,
		)

		// If current state is alert, update to ok (recovery)
		if w.AlertState == types.AlertStateInAlarm {
			if err := s.UpdateWalletAlertState(ctx, w.ID, types.AlertStateOk); err != nil {
				s.Logger.Error(ctx, "failed to update wallet alert state",
					"wallet_id", w.ID,
					"error", err,
				)
				return err
			}
			s.Logger.Info(ctx, "wallet recovered from alert state",
				"wallet_id", w.ID,
			)
			return s.PublishEvent(ctx, types.WebhookEventWalletUpdated, w)
		}
		return nil
	}

	// Skip if already in alert state
	if w.AlertState == types.AlertStateInAlarm {
		s.Logger.Info(ctx, "skipping alert - already in alert state",
			"wallet_id", w.ID,
		)
		return nil
	}

	s.Logger.Info(ctx, "balance triggered alert - updating state",
		"wallet_id", w.ID,
		"alert_settings", alertSettings,
		"current_balance", currentBalance,
		"credit_balance", creditBalance,
		"current_balance_alert_status", currentBalanceAlertStatus,
		"credit_balance_alert_status", creditBalanceAlertStatus,
	)

	// Update wallet state to alert
	if err := s.UpdateWalletAlertState(ctx, w.ID, types.AlertStateInAlarm); err != nil {
		s.Logger.Error(ctx, "failed to update wallet alert state",
			"wallet_id", w.ID,
			"error", err,
		)
		return err
	}

	// Trigger alerts based on which balance triggered an alert
	var errs []error
	if isCreditBalanceInAlert {
		s.Logger.Info(ctx, "triggering credit balance alert",
			"wallet_id", w.ID,
			"credit_balance", creditBalance,
			"alert_status", creditBalanceAlertStatus,
		)
		if err := s.PublishEvent(ctx, types.WebhookEventWalletCreditBalanceDropped, w); err != nil {
			s.Logger.Error(ctx, "failed to publish credit balance alert",
				"wallet_id", w.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	if isCurrentBalanceInAlert {
		s.Logger.Info(ctx, "triggering ongoing balance alert",
			"wallet_id", w.ID,
			"balance", currentBalance,
			"alert_status", currentBalanceAlertStatus,
		)
		if err := s.PublishEvent(ctx, types.WebhookEventWalletOngoingBalanceDropped, w); err != nil {
			s.Logger.Error(ctx, "failed to publish ongoing balance alert",
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

func (s *walletService) TopUpWalletForProratedCharge(ctx context.Context, customerID string, amount decimal.Decimal, currency string, idempotencyKey string) (*dto.WalletTransactionResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").
			WithHint("Customer ID is required for wallet top-up").
			Mark(ierr.ErrValidation)
	}

	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, ierr.NewError("amount must be positive").
			WithHint("Top-up amount must be greater than zero").
			Mark(ierr.ErrValidation)
	}

	if currency == "" {
		currency = "usd" // Default to USD if no currency provided
	}

	// Get customer to validate existence
	_, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get existing wallets for the customer
	existingWallets, err := s.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get existing wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Find or create a suitable prepaid wallet for the proration credit.
	// Prepaid wallets represent customer-owned balance that can be consumed on future invoices.
	var selectedWallet *dto.WalletResponse
	for _, w := range existingWallets {
		if w.WalletStatus == types.WalletStatusActive &&
			types.IsMatchingCurrency(w.Currency, currency) &&
			w.WalletType == types.WalletTypePrePaid {
			selectedWallet = w
			break
		}
	}

	// Create a new prepaid wallet if none exists
	if selectedWallet == nil {
		s.Logger.Info(ctx, "creating new prepaid wallet for proration credit",
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
			return nil, ierr.WithError(err).
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
		IdempotencyKey: lo.ToPtr(idempotencyKey),
	}

	topUpResp, err := s.TopUpWallet(ctx, selectedWallet.ID, topUpReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to top up wallet with proration credit").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
				"wallet_id":   selectedWallet.ID,
				"amount":      amount.String(),
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Info(ctx, "successfully topped up wallet for proration credit",
		"customer_id", customerID,
		"wallet_id", selectedWallet.ID,
		"amount", amount.String(),
		"currency", currency)

	if topUpResp == nil || topUpResp.WalletTransaction == nil {
		return nil, ierr.NewError("wallet top-up returned no transaction").
			WithHint("Proration credit was applied but transaction details are missing").
			Mark(ierr.ErrInternal)
	}

	return topUpResp.WalletTransaction, nil
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

	// POST_PAID wallets: balance doesn't deplete with usage, real-time balance = wallet balance
	if w.WalletType == types.WalletTypePostPaid {
		realTimeCreditBalance := s.GetCreditsFromCurrencyAmount(w.Balance, w.ConversionRate)
		s.setWalletRealtimeBalanceToCache(ctx, walletID, w.Balance)
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(w.Balance),
			RealTimeCreditBalance: lo.ToPtr(realTimeCreditBalance),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
			UnpaidInvoicesAmount:  lo.ToPtr(decimal.Zero),
		}, nil
	}

	// PRE_PAID: try real-time compute under a deadline so a hung or slow DB
	// trips fallback instead of starving the request. On any failure (timeout
	// or otherwise) fall back to the last cached balance if available.

	cached := s.getWalletRealtimeBalanceFromCache(ctx, walletID, nil)

	if cached != nil {
		// Calculate the timeout duration based on the context deadline
		var timeoutDuration time.Duration
		currentTimeout, ok := ctx.Deadline()
		if ok && !currentTimeout.IsZero() && time.Until(currentTimeout) < s.computeBalanceTimeout {
			timeoutDuration = time.Until(currentTimeout)
		} else {
			timeoutDuration = s.computeBalanceTimeout
		}

		computeCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
		defer cancel()

		resp, err := s.computeRealtimeBalance(computeCtx, w)
		if err != nil {
			// Parent context was canceled or its deadline exceeded, the caller
			// has given up, so propagate instead of serving stale cached data.
			if ctx.Err() != nil {
				return nil, err
			}
			s.Logger.Error(ctx, "wallet balance fallback to cache",
				"wallet_id", walletID,
				"tenant_id", types.GetTenantID(ctx),
				"environment_id", types.GetEnvironmentID(ctx),
				"endpoint", "real_time",
				"error", err.Error(),
			)
			return s.buildResponseFromCachedBalance(w, *cached), nil
		}

		if resp != nil && resp.RealTimeBalance != nil {
			s.setWalletRealtimeBalanceToCache(ctx, walletID, *resp.RealTimeBalance)
		}
		return resp, nil
	}

	// if no cached balance, compute the real-time balance and set the cache

	resp, err := s.computeRealtimeBalance(ctx, w)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.RealTimeBalance != nil {
		s.setWalletRealtimeBalanceToCache(ctx, walletID, *resp.RealTimeBalance)
	}
	return resp, nil
}

// buildResponseFromCachedBalance builds a WalletBalanceResponse from a cached
// real-time balance and flags IsCachedFallback so callers know the data is
// not freshly computed. Shared by V2 fallback and FromCache (hit + fallback).
func (s *walletService) buildResponseFromCachedBalance(w *wallet.Wallet, balance decimal.Decimal) *dto.WalletBalanceResponse {
	rt := balance
	credit := s.GetCreditsFromCurrencyAmount(rt, w.ConversionRate)
	zero := decimal.Zero
	return &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeBalance:       &rt,
		RealTimeCreditBalance: &credit,
		BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
		CurrentPeriodUsage:    &zero,
		IsCachedFallback:      true,
	}
}

// computeRealtimeBalanceDefault is the production implementation of the
// realtime-balance computation. Defaulted into the computeRealtimeBalance
// field by NewWalletService; tests may override the field to inject failures.
func (s *walletService) computeRealtimeBalanceDefault(ctx context.Context, w *wallet.Wallet) (*dto.WalletBalanceResponse, error) {
	// PRE_PAID wallets: calculate pending usage charges that will consume prepaid balance
	var totalPendingCharges decimal.Decimal
	shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)

	if shouldIncludeUsage {
		// Get all active subscriptions to calculate current usage
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		subscriptions, err := subscriptionService.ListByCustomerID(ctx, w.CustomerID)
		if err != nil {
			return nil, err
		}

		// Filter subscriptions by currency
		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if sub.Currency != w.Currency {
				s.Logger.Info(ctx, "skipping subscription - currency mismatch")
				continue
			}
			if sub.SubscriptionType != types.SubscriptionTypeStandalone && sub.SubscriptionType != types.SubscriptionTypeParent {
				s.Logger.Info(ctx, "skipping subscription - not a standalone or parent subscription")
				continue
			}
			filteredSubscriptions = append(filteredSubscriptions, sub)
		}

		billingService := NewBillingService(s.ServiceParams)

		// Calculate total pending charges (usage)
		for _, sub := range filteredSubscriptions {
			// Get current period
			periodStart := sub.CurrentPeriodStart
			periodEnd := sub.CurrentPeriodEnd

			usageReq := &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: sub.ID,
				StartTime:      periodStart,
				EndTime:        periodEnd,
				Source:         string(types.UsageSourceWallet),
			}

			usage, err := subscriptionService.GetMeterUsageBySubscription(ctx, usageReq)
			if err != nil {
				return nil, err
			}

			lineItems, totalAmount, err := billingService.CalculateMeterUsageCharges(
				ctx, sub, usage, periodStart, periodEnd, types.UsageSourceWallet,
			)
			if err != nil {
				return nil, err
			}

			s.Logger.Debug(ctx, "subscription charges details",
				"subscription_id", sub.ID,
				"usage_total", totalAmount,
				"num_usage_charges", len(lineItems))

			totalPendingCharges = totalPendingCharges.Add(totalAmount)
		}
	}

	// Get unpaid invoices for PRE_PAID wallets
	invoiceService := NewInvoiceService(s.ServiceParams)
	resp, err := invoiceService.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: w.CustomerID,
		Currency:   w.Currency,
	})
	if err != nil {
		return nil, err
	}

	if lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll) || lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeFixed) {
		totalPendingCharges = totalPendingCharges.Add(resp.TotalUnpaidAmount)
	} else {
		totalPendingCharges = totalPendingCharges.Add(resp.TotalUnpaidUsageCharges).Sub(resp.TotalPaidInvoiceAmount)
	}

	// Calculate real-time balance: wallet balance minus pending charges
	realTimeBalance := w.Balance.Sub(totalPendingCharges)

	s.Logger.Debug(ctx, "detailed balance calculation",
		"wallet_id", w.ID,
		"wallet_type", w.WalletType,
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
		UnpaidInvoicesAmount:  lo.ToPtr(resp.TotalUnpaidUsageCharges),
	}, nil
}

func (s *walletService) GetWalletBalanceFromCache(ctx context.Context, walletID string, maxLiveSeconds *int64) (*dto.WalletBalanceResponse, error) {
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

	// POST_PAID wallets: balance doesn't deplete with usage, real-time balance = wallet balance
	if w.WalletType == types.WalletTypePostPaid {
		realTimeCreditBalance := s.GetCreditsFromCurrencyAmount(w.Balance, w.ConversionRate)
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(w.Balance),
			RealTimeCreditBalance: lo.ToPtr(realTimeCreditBalance),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
			UnpaidInvoicesAmount:  lo.ToPtr(decimal.Zero),
		}, nil
	}

	// Caller-driven cache read (honors maxLiveSeconds). A hit is the happy
	// path for this endpoint: the caller asked for cached data, we have it.
	if cached := s.getWalletRealtimeBalanceFromCache(ctx, walletID, maxLiveSeconds); cached != nil {
		s.Logger.Debug(ctx, "using cached real-time balance",
			"wallet_id", walletID,
			"cached_balance", cached,
		)
		return s.buildResponseFromCachedBalance(w, *cached), nil
	}

	// attempt to fetch older cached balance
	// if no cached balance is not found within the max live seconds
	// then, compute the real-time balance and set the cache if successful

	cached := s.getWalletRealtimeBalanceFromCache(ctx, walletID, nil)

	if cached != nil {
		// Calculate the timeout duration based on the context deadline
		var timeoutDuration time.Duration
		currentTimeout, ok := ctx.Deadline()
		if ok && !currentTimeout.IsZero() && time.Until(currentTimeout) < s.computeBalanceTimeout {
			timeoutDuration = time.Until(currentTimeout)
		} else {
			timeoutDuration = s.computeBalanceTimeout
		}

		computeCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
		defer cancel()

		resp, err := s.computeRealtimeBalance(computeCtx, w)
		if err != nil {
			// Parent context was canceled or its deadline exceeded, the caller
			// has given up, so propagate instead of serving stale cached data.
			if ctx.Err() != nil {
				return nil, err
			}
			s.Logger.Error(ctx, "wallet balance fallback to cache",
				"wallet_id", walletID,
				"tenant_id", types.GetTenantID(ctx),
				"environment_id", types.GetEnvironmentID(ctx),
				"endpoint", "real_time",
				"error", err.Error(),
			)
			return s.buildResponseFromCachedBalance(w, *cached), nil
		}

		if resp != nil && resp.RealTimeBalance != nil {
			s.setWalletRealtimeBalanceToCache(ctx, walletID, *resp.RealTimeBalance)
		}
		return resp, nil
	}

	// if no cached balance, compute the real-time balance and set the cache

	resp, err := s.computeRealtimeBalance(ctx, w)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.RealTimeBalance != nil {
		s.setWalletRealtimeBalanceToCache(ctx, walletID, *resp.RealTimeBalance)
	}
	return resp, nil
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
		Metadata:          req.Metadata,
	}

	err = s.DebitWallet(ctx, debitReq)
	if err != nil {
		return nil, err
	}

	return s.GetWalletByID(ctx, walletID)
}

// EvaluateAlertsForWallet is the exported per-wallet driver used by the
// alert-evaluation coordinator (alert_evaluation.go). Bundles the three
// steps (wallet-level, feature-level, auto-topup) with the balance fetch
// and short-circuit so callers don't need access to the private helpers.
func (s *walletService) EvaluateAlertsForWallet(ctx context.Context, w *wallet.Wallet, alertLogs AlertLogsService, autoTopupIdempotencySeed string) error {
	if w == nil {
		return nil
	}
	settingsSvc := &settingsService{ServiceParams: s.ServiceParams}
	alertSettings, err := s.resolveWalletAlertSettings(ctx, w, settingsSvc)
	hasWalletAlert := false
	if err != nil {
		s.Logger.Error(ctx, "wallet alerts: failed to resolve wallet alert settings", "error", err, "wallet_id", w.ID)
	} else {
		hasWalletAlert = alertSettings.IsAlertEnabled()
	}

	autoTopupEnabled := w.AutoTopup != nil && lo.FromPtr(w.AutoTopup.Enabled)

	// Skip balance fetch entirely if nothing is configured — the caller has
	// already gated on the tenant-level wallet-alert setting; this is the
	// per-wallet short-circuit.
	if !hasWalletAlert && !autoTopupEnabled {
		return nil
	}

	// Note: same cache key is being read & updated by `GetWalletBalanceV2`
	// so if tenant called get wallet balance v2, then the cached balance will be updated
	// and if there's no usage in-between, no balance updated webhook will be sent
	// expecting that tenant already got the updated balance
	cachedBalance := s.getWalletRealtimeBalanceFromCache(ctx, w.ID, nil)

	balance, err := s.GetWalletBalanceV2(ctx, w.ID)
	if err != nil {
		s.Logger.Error(ctx, "wallet alerts: failed to get wallet balance", "error", err, "wallet_id", w.ID)
		return nil
	}
	ongoingBalance := lo.FromPtr(balance.RealTimeCreditBalance)

	if hasWalletAlert {
		eventID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT)
		if err := s.processWalletBalanceAlert(ctx, w, ongoingBalance, alertSettings, alertLogs, eventID); err != nil {
			s.Logger.Error(ctx, "failed to process wallet balance alert", "error", err, "wallet_id", w.ID)
		}
	}

	if autoTopupEnabled {
		autoTopupKey := ""
		if autoTopupIdempotencySeed != "" {
			autoTopupKey = autoTopupIdempotencySeed + "-topup-" + w.ID
		}
		if err := s.triggerAutoTopup(ctx, w, ongoingBalance, autoTopupKey); err != nil {
			s.Logger.Error(ctx, "failed to trigger auto top-up", "error", err, "wallet_id", w.ID)
		}
	}

	// processFeatureWalletBalanceAlert is not being supported anymore

	// send balance updated webhook if there's no cached balance OR the cached balance is different from the ongoing balance
	if cachedBalance == nil || ongoingBalance.Cmp(*cachedBalance) != 0 {
		s.publishOngoingBalanceUpdatedWebhookEvent(ctx, w.ID, balance)
	}

	return nil
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

// resolveWalletAlertSettings returns the effective alert settings for a wallet.
// Wallet-level settings take precedence over tenant-level settings.
func (s *walletService) resolveWalletAlertSettings(ctx context.Context, w *wallet.Wallet, settingsSvc *settingsService) (*types.AlertSettings, error) {
	if w.AlertSettings != nil {
		return w.AlertSettings, nil
	}
	walletAlertSettings, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
	if err != nil {
		return nil, err
	}
	return &walletAlertSettings, nil
}

// processFeatureWalletBalanceAlert checks and logs feature-level wallet balance alerts for a wallet.
// Errors on individual features are logged and skipped; the method always returns nil.
func (s *walletService) processFeatureWalletBalanceAlert(ctx context.Context, w *wallet.Wallet, ongoingBalance decimal.Decimal, featuresWithAlerts []*dto.FeatureResponse, alertLogsService AlertLogsService) error {
	var customerID *string
	if w.CustomerID != "" {
		customerID = lo.ToPtr(w.CustomerID)
	}

	for _, feature := range featuresWithAlerts {
		alertStatus, err := feature.AlertSettings.AlertState(ongoingBalance)
		if err != nil {
			s.Logger.Error(ctx, "failed to determine feature alert status",
				"feature_id", feature.ID,
				"feature_name", feature.Name,
				"wallet_id", w.ID,
				"ongoing_balance", ongoingBalance,
				"error", err,
			)
			continue
		}

		s.Logger.Debug(ctx, "feature alert status determined",
			"feature_id", feature.ID,
			"feature_name", feature.Name,
			"wallet_id", w.ID,
			"ongoing_balance", ongoingBalance,
			"critical", feature.AlertSettings.Critical,
			"warning", feature.AlertSettings.Warning,
			"alert_enabled", feature.AlertSettings.AlertEnabled,
			"alert_status", alertStatus,
		)

		err = alertLogsService.LogAlert(ctx, &LogAlertRequest{
			EntityType:       types.AlertEntityTypeFeature,
			EntityID:         feature.ID,
			ParentEntityType: lo.ToPtr("wallet"),
			ParentEntityID:   lo.ToPtr(w.ID),
			CustomerID:       customerID,
			AlertType:        types.AlertTypeFeatureWalletBalance,
			AlertStatus:      alertStatus,
			AlertInfo: types.AlertInfo{
				AlertSettings: feature.AlertSettings,
				ValueAtTime:   ongoingBalance,
				Timestamp:     time.Now().UTC(),
			},
		})
		if err != nil {
			s.Logger.Error(ctx, "failed to log feature alert",
				"feature_id", feature.ID,
				"wallet_id", w.ID,
				"alert_status", alertStatus,
				"error", err,
			)
			continue
		}

		s.Logger.Debug(ctx, "feature alert check completed",
			"feature_id", feature.ID,
			"wallet_id", w.ID,
			"alert_status", alertStatus,
		)
	}

	return nil
}

// processWalletBalanceAlert checks and logs the ongoing balance alert for a wallet
// and updates the wallet's alert state if it changed.
func (s *walletService) processWalletBalanceAlert(ctx context.Context, w *wallet.Wallet, ongoingBalance decimal.Decimal, alertSettings *types.AlertSettings, alertLogsService AlertLogsService, eventID string) error {
	alertStatus, err := alertSettings.AlertState(ongoingBalance)
	if err != nil {
		s.Logger.Error(ctx, "failed to determine wallet alert status",
			"wallet_id", w.ID,
			"ongoing_balance", ongoingBalance,
			"error", err,
		)
		return err
	}

	s.Logger.Debug(ctx, "ongoing balance alert check - determined status",
		"wallet_id", w.ID,
		"ongoing_balance", ongoingBalance,
		"alert_settings", alertSettings,
		"alert_status", alertStatus,
		"event_id", eventID,
	)

	var customerID *string
	if w.CustomerID != "" {
		customerID = lo.ToPtr(w.CustomerID)
	}

	err = alertLogsService.LogAlert(ctx, &LogAlertRequest{
		EntityType:  types.AlertEntityTypeWallet,
		EntityID:    w.ID,
		CustomerID:  customerID,
		AlertType:   types.AlertTypeLowOngoingBalance,
		AlertStatus: alertStatus,
		AlertInfo: types.AlertInfo{
			AlertSettings: alertSettings,
			ValueAtTime:   ongoingBalance,
			Timestamp:     time.Now().UTC(),
		},
	})
	if err != nil {
		s.Logger.Error(ctx, "failed to log ongoing balance alert",
			"error", err,
			"wallet_id", w.ID,
			"alert_type", types.AlertTypeLowOngoingBalance,
			"alert_status", alertStatus,
			"ongoing_balance", ongoingBalance,
			"alert_settings", alertSettings,
			"event_id", eventID,
		)
		return err
	}

	s.Logger.Debug(ctx, "successfully logged ongoing balance alert",
		"wallet_id", w.ID,
		"alert_status", alertStatus,
		"ongoing_balance", ongoingBalance,
		"alert_settings", alertSettings,
		"event_id", eventID,
	)

	currentWallet, err := s.WalletRepo.GetWalletByID(ctx, w.ID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get wallet for alert state update",
			"error", err,
			"wallet_id", w.ID,
			"event_id", eventID,
		)
		return err
	}

	if currentWallet.AlertState != alertStatus {
		s.Logger.Debug(ctx, "updating wallet alert state",
			"wallet_id", w.ID,
			"old_state", currentWallet.AlertState,
			"new_state", alertStatus,
			"event_id", eventID,
		)
		if err := s.UpdateWalletAlertState(ctx, w.ID, alertStatus); err != nil {
			s.Logger.Error(ctx, "failed to update wallet alert state",
				"error", err,
				"wallet_id", w.ID,
				"old_state", currentWallet.AlertState,
				"new_state", alertStatus,
				"event_id", eventID,
			)
			return err
		}
		s.Logger.Info(ctx, "wallet alert state updated successfully",
			"wallet_id", w.ID,
			"old_state", currentWallet.AlertState,
			"new_state", alertStatus,
			"event_id", eventID,
		)
	} else {
		s.Logger.Debug(ctx, "wallet alert state unchanged, skipping update",
			"wallet_id", w.ID,
			"current_state", currentWallet.AlertState,
			"event_id", eventID,
		)
	}

	s.Logger.Debug(ctx, "wallet ongoing balance alert check completed",
		"wallet_id", w.ID,
		"alert_status", alertStatus,
		"event_id", eventID,
	)

	return nil
}

func (s *walletService) CheckWalletBalanceAlert(ctx context.Context, req *wallet.WalletBalanceAlertEvent) error {
	// Set context values from event
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, req.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxTenantID, req.TenantID)

	s.Logger.Debug(ctx, "checking wallet balance alerts",
		"event_id", req.ID,
		"customer_id", req.CustomerID,
		"tenant_id", req.TenantID,
		"environment_id", req.EnvironmentID,
		"wallet_id", req.WalletID,
		"source", req.Source,
		"force_calculate", req.ForceCalculateBalance,
		"get_from_cache", req.GetFromCache,
	)

	// Get active wallets for this customer
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, req.CustomerID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get wallets for customer",
			"error", err,
			"customer_id", req.CustomerID,
			"event_id", req.ID,
		)
		return err
	}

	if len(wallets) == 0 {
		s.Logger.Info(ctx, "no wallets found for customer",
			"customer_id", req.CustomerID,
			"event_id", req.ID,
		)
		return nil
	}

	// Fetch features with alert settings
	featuresWithAlerts, err := s.fetchFeaturesWithAlertSettings(ctx)
	if err != nil {
		s.Logger.Error(ctx, "failed to fetch features with alert settings",
			"error", err,
		)
	}
	// Determine if feature alerts are enabled
	featureAlertsEnabled := len(featuresWithAlerts) > 0

	alertLogsService := NewAlertLogsService(s.ServiceParams)
	settingsSvc := &settingsService{ServiceParams: s.ServiceParams}

	// Process each wallet
	for _, w := range wallets {
		s.Logger.Debug(ctx, "processing wallet for alert check",
			"wallet_id", w.ID,
			"customer_id", w.CustomerID,
			"has_wallet_alert_settings", w.AlertSettings != nil,
			"event_id", req.ID,
		)

		// Resolve wallet-level alert settings
		walletAlertsEnabled := false
		alertSettings, err := s.resolveWalletAlertSettings(ctx, w, settingsSvc)
		if err != nil {
			s.Logger.Error(ctx, "failed to resolve wallet alert settings, alert checks will be skipped",
				"error", err,
				"wallet_id", w.ID,
				"event_id", req.ID,
			)
		} else {
			walletAlertsEnabled = alertSettings.IsAlertEnabled()
		}

		// Determine if auto top-up is enabled
		autoTopupEnabled := w.AutoTopup != nil && lo.FromPtr(w.AutoTopup.Enabled)

		// Skip wallet if neither wallet alert, feature alert, nor auto top-up are enabled
		if !walletAlertsEnabled && !featureAlertsEnabled && !autoTopupEnabled {
			s.Logger.Debug(ctx, "skipping balance alert check for wallet — wallet alerts, feature alerts, and auto top-up are all disabled; no action required",
				"wallet_id", w.ID,
				"customer_id", w.CustomerID,
				"event_id", req.ID,
			)
			continue
		}

		// 3. Fetch balance — deferred until we know at least one action requires it
		var balance *dto.WalletBalanceResponse
		if req.GetFromCache {
			maxLive := int64(60) // 1 minute in seconds
			balance, err = s.GetWalletBalanceFromCache(ctx, w.ID, &maxLive)
		} else {
			balance, err = s.GetWalletBalanceV2(ctx, w.ID)
		}
		if err != nil {
			s.Logger.Error(ctx, "failed to get wallet balance, skipping wallet",
				"error", err,
				"wallet_id", w.ID,
				"event_id", req.ID,
			)
			continue
		}

		// RealTimeCreditBalance = (currency balance - pending charges) / conversion_rate
		ongoingBalance := lo.FromPtr(balance.RealTimeCreditBalance)

		// Process wallet-level balance alert BEFORE triggering auto top-up so the
		// pre-topup state (e.g. Warning) is recorded first. The nested
		// CheckWalletBalanceAlert call that fires inside processWalletOperation
		// during the top-up credit will then log the post-topup state (ok).
		if walletAlertsEnabled {
			s.Logger.Debug(ctx, "wallet balance details for alert check",
				"wallet_id", w.ID,
				"real_time_balance", balance.RealTimeBalance,
				"wallet_current_balance", balance.Wallet.Balance,
				"alert_settings", alertSettings,
				"pending_charges_currency", balance.CurrentPeriodUsage,
				"conversion_rate", w.ConversionRate,
				"event_id", req.ID,
			)

			if err := s.processWalletBalanceAlert(ctx, w, ongoingBalance, alertSettings, alertLogsService, req.ID); err != nil {
				s.Logger.Error(ctx, "failed to process wallet balance alert",
					"error", err,
					"wallet_id", w.ID,
					"event_id", req.ID,
				)
			}
		}

		// Process feature-level alerts if enabled
		if featureAlertsEnabled {
			if err := s.processFeatureWalletBalanceAlert(ctx, w, ongoingBalance, featuresWithAlerts, alertLogsService); err != nil {
				s.Logger.Error(ctx, "failed to process feature wallet balance alerts",
					"error", err,
					"wallet_id", w.ID,
					"event_id", req.ID,
				)
			}
		}

		// Trigger auto top-up after alerts have been logged. The nested
		// CheckWalletBalanceAlert that fires inside processWalletOperation during
		// the top-up credit will record the recovered (ok) state automatically.
		if autoTopupEnabled {
			// Legacy Kafka path — retries are governed by Kafka delivery and
			// each delivery has its own message identity, so keep the fresh-UUID
			// behavior here rather than plumbing a Kafka-scoped stable key.
			if err := s.triggerAutoTopup(ctx, w, ongoingBalance, ""); err != nil {
				s.Logger.Error(ctx, "failed to trigger auto top-up",
					"error", err,
					"wallet_id", w.ID,
				)
			}
		}

		s.publishOngoingBalanceUpdatedWebhookEvent(ctx, w.ID, balance)
	}

	s.Logger.Debug(ctx, "completed wallet balance alert check for customer",
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
		s.Logger.Error(ctx, "failed to publish wallet balance alert event",
			"error", err,
			"customer_id", customerID,
			"force_calculate_balance", forceCalculateBalance,
		)
	}
}

// hasPendingAutoTopupInvoice returns true if there is already a FINALIZED, unpaid
// auto-topup invoice for this customer. Used to prevent duplicate invoices while
// waiting for the customer to pay.
func (s *walletService) hasPendingAutoTopupInvoice(ctx context.Context, customerID string) (bool, error) {
	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = customerID
	filter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
	filter.PaymentStatus = []types.PaymentStatus{
		types.PaymentStatusPending,
		types.PaymentStatusFailed,
		types.PaymentStatusProcessing,
		types.PaymentStatusInitiated,
	}
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
	filter.SkipLineItems = true
	filter.Limit = lo.ToPtr(1)

	invoices, err := s.InvoiceRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}
	return len(invoices) > 0, nil
}

// triggerAutoTopup checks if auto top-up is enabled and triggers it if needed.
//
// autoTopupIdempotencyKey, when non-empty, is used verbatim as the TopUpWallet
// idempotency key so retries of the same logical evaluation collapse into a
// single top-up. Callers that own a stable per-evaluation identity (e.g. a
// Temporal workflow run id) should pass it here. Empty string preserves the
// legacy behavior of minting a fresh UUID per call — appropriate for callers
// that already have their own retry/dedup barrier (Kafka consumers, tests).
func (s *walletService) triggerAutoTopup(ctx context.Context, w *wallet.Wallet, ongoingBalance decimal.Decimal, autoTopupIdempotencyKey string) error {

	if w.AutoTopup == nil || w.AutoTopup.Enabled == nil || !*w.AutoTopup.Enabled {
		s.Logger.Debug(ctx, "auto top-up not enabled, skipping",
			"wallet_id", w.ID,
		)
		return nil
	}

	// Check if ongoing balance is below threshold
	if ongoingBalance.LessThanOrEqual(*w.AutoTopup.Threshold) {

		isInvoiced := w.AutoTopup.Invoicing != nil && *w.AutoTopup.Invoicing

		// Guard: for invoiced mode, skip if there is already a pending auto-topup invoice.
		// This prevents flooding the customer with invoices while they wait to pay.
		if isInvoiced {
			hasPending, err := s.hasPendingAutoTopupInvoice(ctx, w.CustomerID)
			if err != nil {
				s.Logger.Error(ctx, "failed to check for pending auto-topup invoice",
					"error", err,
					"wallet_id", w.ID,
					"customer_id", w.CustomerID,
				)
				return err
			}
			if hasPending {
				s.Logger.Info(ctx, "pending auto-topup invoice exists, skipping",
					"wallet_id", w.ID,
					"customer_id", w.CustomerID,
					"auto_topup_threshold", *w.AutoTopup.Threshold,
				)
				return nil
			}
		}

		transactionReason := lo.Ternary(isInvoiced,
			types.TransactionReasonPurchasedCreditInvoiced,
			types.TransactionReasonPurchasedCreditDirect,
		)
		billingReason := lo.Ternary(isInvoiced,
			types.InvoiceBillingReasonWalletAutoTopup,
			types.InvoiceBillingReason(""),
		)

		idempotencyKey := autoTopupIdempotencyKey
		if idempotencyKey == "" {
			idempotencyKey = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION)
		}
		_, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
			CreditsToAdd:      *w.AutoTopup.Amount,
			Amount:            *w.AutoTopup.Amount,
			TransactionReason: transactionReason,
			BillingReason:     billingReason,
			IdempotencyKey:    lo.ToPtr(idempotencyKey),
			Description:       "Auto top-up triggered for low ongoing balance",
			Metadata:          types.Metadata{"auto_topup": "true"},
		})
		if err != nil {
			s.Logger.Error(ctx, "failed to top up wallet for auto top-up",
				"error", err,
				"wallet_id", w.ID,
				"auto_topup_threshold", *w.AutoTopup.Threshold,
				"auto_topup_amount", *w.AutoTopup.Amount,
			)
			return err
		}
		s.Logger.Debug(ctx, "auto top-up triggered",
			"wallet_id", w.ID,
			"auto_topup_threshold", *w.AutoTopup.Threshold,
			"auto_topup_amount", *w.AutoTopup.Amount,
			"invoiced", isInvoiced,
		)
	}

	s.Logger.Info(ctx, "auto top-up check completed",
		"wallet_id", w.ID,
		"ongoing_balance", ongoingBalance,
		"auto_topup_threshold", *w.AutoTopup.Threshold,
	)

	return nil
}

// GetCreditsAvailableBreakdown retrieves the breakdown of available credits by type (purchased, free, other)
func (s *walletService) GetCreditsAvailableBreakdown(ctx context.Context, walletID string) (*types.CreditBreakdown, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	breakdown, err := s.WalletRepo.GetCreditsAvailableBreakdown(ctx, walletID)
	if err != nil {
		return nil, err
	}

	return breakdown, nil
}

func (s *walletService) setWalletRealtimeBalanceToCache(ctx context.Context, walletID string, balance decimal.Decimal) {
	if s.RedisCache == nil {
		return
	}

	span, spanCtx := cache.StartRedisCacheSpan(ctx, "wallet", "set", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(spanCtx, cache.PrefixWallet, walletID)
	s.RedisCache.ForceCacheSet(spanCtx, cacheKey, balance.String(), cache.ExpiryWalletBalance)
}

func (s *walletService) invalidateWalletRealtimeBalanceCache(ctx context.Context, walletID string) {
	if walletID == "" || s.RedisCache == nil {
		return
	}
	cacheKey := cache.GenerateKey(ctx, cache.PrefixWallet, walletID)
	s.RedisCache.ForceCacheDelete(ctx, cacheKey)
}

func (s *walletService) getWalletRealtimeBalanceFromCache(ctx context.Context, walletID string, maxLiveSeconds *int64) *decimal.Decimal {
	if s.RedisCache == nil {
		return nil
	}

	span, spanCtx := cache.StartRedisCacheSpan(ctx, "wallet", "get", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(spanCtx, cache.PrefixWallet, walletID)

	// When maxLiveSeconds is specified, check cache age via TTL
	if maxLiveSeconds != nil {
		cachedValue, remainingTTL, found := s.RedisCache.ForceCacheGetWithTTL(spanCtx, cacheKey)
		if !found {
			return nil
		}

		// Calculate cache age: original expiry minus remaining TTL
		cacheAge := cache.ExpiryWalletBalance - remainingTTL
		maxAge := time.Duration(*maxLiveSeconds) * time.Second

		if cacheAge > maxAge {
			// Cache entry is too old, treat as miss
			s.Logger.Debug(ctx, "cache entry exceeds max-live, treating as miss",
				"wallet_id", walletID,
				"cache_age_seconds", cacheAge.Seconds(),
				"max_live_seconds", *maxLiveSeconds,
			)
			return nil
		}

		balance, success := cache.UnmarshalCacheValue[decimal.Decimal](cachedValue)
		if !success {
			return nil
		}
		return balance
	}

	// Default path: no max-live check
	cachedValue, found := s.RedisCache.ForceCacheGet(spanCtx, cacheKey)
	if !found {
		return nil
	}

	balance, success := cache.UnmarshalCacheValue[decimal.Decimal](cachedValue)
	if !success {
		return nil
	}

	return balance
}

