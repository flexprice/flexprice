package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
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

	// GetWalletBalance retrieves the real-time balance of a wallet
	GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)

	// TerminateWallet terminates a wallet by closing it and debiting remaining balance
	TerminateWallet(ctx context.Context, walletID string) error
}

type walletService struct {
	walletRepo       wallet.Repository
	logger           *logger.Logger
	subscriptionRepo subscription.Repository
	planRepo         plan.Repository
	priceRepo        price.Repository
	eventRepo        events.Repository
	meterRepo        meter.Repository
	customerRepo     customer.Repository
	eventPublisher   publisher.EventPublisher
	webhookPublisher webhookPublisher.WebhookPublisher
	invoiceRepo      invoice.Repository
	db               postgres.IClient
	config           *config.Configuration
}

// NewWalletService creates a new instance of WalletService
func NewWalletService(
	walletRepo wallet.Repository,
	logger *logger.Logger,
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	priceRepo price.Repository,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	db postgres.IClient,
	config *config.Configuration,
) WalletService {
	return &walletService{
		walletRepo:       walletRepo,
		logger:           logger,
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		eventRepo:        eventRepo,
		meterRepo:        meterRepo,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		eventPublisher:   eventPublisher,
		webhookPublisher: webhookPublisher,
		db:               db,
		config:           config,
	}
}

func (s *walletService) CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Check if customer already has an active wallet
	existingWallets, err := s.walletRepo.GetWalletsByCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing wallets: %w", err)
	}

	for _, w := range existingWallets {
		if w.WalletStatus == types.WalletStatusActive && w.Currency == req.Currency {
			s.logger.Warnw("customer already has an active wallet in the same currency",
				"customer_id", req.CustomerID,
				"existing_wallet_id", w.ID,
			)
			return nil, fmt.Errorf("customer already has an active wallet with ID: %s", w.ID)
		}
	}

	w := req.ToWallet(ctx)

	// Create wallet in DB and update the wallet object
	if err := s.walletRepo.CreateWallet(ctx, w); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	s.logger.Debugw("created wallet",
		"wallet_id", w.ID,
		"customer_id", w.CustomerID,
		"currency", w.Currency,
	)

	// Convert to response DTO
	return dto.FromWallet(w), nil
}

func (s *walletService) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error) {
	wallets, err := s.walletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallets: %w", err)
	}

	response := make([]*dto.WalletResponse, len(wallets))
	for i, w := range wallets {
		response[i] = dto.FromWallet(w)
	}

	return response, nil
}

func (s *walletService) GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error) {
	w, err := s.walletRepo.GetWalletByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	return dto.FromWallet(w), nil
}

func (s *walletService) GetWalletTransactions(ctx context.Context, walletID string, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error) {
	if filter == nil {
		filter = types.NewWalletTransactionFilter()
	}

	filter.WalletID = &walletID

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	transactions, err := s.walletRepo.ListWalletTransactions(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	count, err := s.walletRepo.CountWalletTransactions(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
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

func (s *walletService) TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.WalletResponse, error) {
	// Create a credit operation
	creditReq := &wallet.WalletOperation{
		WalletID:    walletID,
		Type:        types.TransactionTypeCredit,
		Amount:      req.Amount,
		Description: req.Description,
		Metadata:    req.Metadata,
	}

	if err := s.walletRepo.CreditWallet(ctx, creditReq); err != nil {
		return nil, fmt.Errorf("failed to credit wallet: %w", err)
	}

	// Get updated wallet
	return s.GetWalletByID(ctx, walletID)
}

func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	w, err := s.walletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	if w.WalletStatus != types.WalletStatusActive {
		return nil, fmt.Errorf("wallet is not active")
	}

	// Get invoice summary for unpaid amounts
	invoiceService := NewInvoiceService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.eventPublisher,
		s.webhookPublisher,
		s.db,
		s.logger,
		s.config,
	)

	invoiceSummary, err := invoiceService.GetCustomerInvoiceSummary(ctx, w.CustomerID, w.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to get invoice summary: %w", err)
	}

	// Get current period usage for active subscriptions
	subscriptionService := NewSubscriptionService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.eventPublisher,
		s.webhookPublisher,
		s.db,
		s.logger,
		s.config,
	)

	filter := types.NewSubscriptionFilter()
	filter.CustomerID = w.CustomerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
	}

	subscriptionsResp, err := subscriptionService.ListSubscriptions(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
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
			s.logger.Errorw("failed to get current period usage",
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

	s.logger.Debugw("calculated real-time balance",
		"wallet_id", walletID,
		"current_balance", w.Balance,
		"unpaid_invoices", invoiceSummary.TotalUnpaidAmount,
		"current_period_usage", currentPeriodUsage,
		"real_time_balance", realTimeBalance,
		"currency", w.Currency,
	)

	return &dto.WalletBalanceResponse{
		Wallet:              w,
		RealTimeBalance:     realTimeBalance,
		BalanceUpdatedAt:    time.Now().UTC(),
		UnpaidInvoiceAmount: invoiceSummary.TotalUnpaidAmount,
		CurrentPeriodUsage:  currentPeriodUsage,
	}, nil
}

func (s *walletService) TerminateWallet(ctx context.Context, walletID string) error {
	w, err := s.walletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}

	if w.WalletStatus == types.WalletStatusClosed {
		return fmt.Errorf("wallet is already closed")
	}

	// Use client's WithTx for atomic operations
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		// Debit remaining balance if any
		if w.Balance.GreaterThan(decimal.Zero) {
			debitReq := &wallet.WalletOperation{
				WalletID:    walletID,
				Amount:      w.Balance,
				Type:        types.TransactionTypeDebit,
				Description: "Wallet termination - remaining balance debit",
			}

			if err := s.walletRepo.DebitWallet(ctx, debitReq); err != nil {
				return fmt.Errorf("failed to debit wallet: %w", err)
			}
		}

		// Update wallet status to closed
		if err := s.walletRepo.UpdateWalletStatus(ctx, walletID, types.WalletStatusClosed); err != nil {
			return fmt.Errorf("failed to close wallet: %w", err)
		}

		return nil
	})
}
