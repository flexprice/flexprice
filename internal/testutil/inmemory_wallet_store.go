package testutil

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InMemoryWalletStore struct {
	wallets      *InMemoryStore[*wallet.Wallet]
	transactions *InMemoryStore[*wallet.Transaction]
}

func NewInMemoryWalletStore() *InMemoryWalletStore {
	return &InMemoryWalletStore{
		wallets:      NewInMemoryStore[*wallet.Wallet](),
		transactions: NewInMemoryStore[*wallet.Transaction](),
	}
}

// walletFilterFn implements filtering logic for wallets
func walletFilterFn(ctx context.Context, w *wallet.Wallet, filter interface{}) bool {
	if w == nil {
		return false
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if w.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, w.EnvironmentID) {
		return false
	}

	// Check wallet status
	if w.Status != types.StatusPublished {
		return false
	}

	// Check wallet status is active
	if w.WalletStatus != types.WalletStatusActive {
		return false
	}

	return true
}

// transactionFilterFn implements filtering logic for transactions
func transactionFilterFn(ctx context.Context, t *wallet.Transaction, filter interface{}) bool {
	if t == nil {
		return false
	}

	f, ok := filter.(*types.WalletTransactionFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if t.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, t.EnvironmentID) {
		return false
	}

	// Filter by status
	if f.Status != nil && t.Status != *f.Status {
		return false
	}

	// Filter by wallet ID
	if f.WalletID != nil && t.WalletID != *f.WalletID {
		return false
	}

	// Filter by transaction type
	if f.Type != nil && t.Type != *f.Type {
		return false
	}

	// Filter by transaction status
	if f.TransactionStatus != nil && t.TxStatus != *f.TransactionStatus {
		return false
	}

	// Filter by reference type and ID
	if f.ReferenceType != nil && f.ReferenceID != nil {
		if string(t.ReferenceType) != string(*f.ReferenceType) || t.ReferenceID != *f.ReferenceID {
			return false
		}
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && t.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && t.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	if f.CreditsAvailableGT != nil && t.CreditsAvailable.LessThanOrEqual(*f.CreditsAvailableGT) {
		return false
	}

	if f.ExpiryDateBefore != nil && t.ExpiryDate != nil && t.ExpiryDate.After(*f.ExpiryDateBefore) {
		return false
	}

	if f.ExpiryDateAfter != nil && t.ExpiryDate != nil && t.ExpiryDate.Before(*f.ExpiryDateAfter) {
		return false
	}

	if f.TransactionReason != nil && t.TransactionReason != *f.TransactionReason {
		return false
	}

	return true
}

// transactionSortFn implements sorting logic for transactions
func transactionSortFn(i, j *wallet.Transaction) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryWalletStore) CreateWallet(ctx context.Context, w *wallet.Wallet) error {
	if w == nil {
		return ierr.NewError("wallet cannot be nil").
			WithHint("A valid wallet object must be provided").
			Mark(ierr.ErrValidation)
	}

	// Set default status if not set
	if w.Status == "" {
		w.Status = types.StatusPublished
	}
	if w.WalletStatus == "" {
		w.WalletStatus = types.WalletStatusActive
	}

	// Set environment ID from context if not already set
	if w.EnvironmentID == "" && types.GetEnvironmentID(ctx) != "" {
		w.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.wallets.Create(ctx, w.ID, w)
}

func (s *InMemoryWalletStore) GetWalletByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	wallet, err := s.wallets.Get(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return wallet, nil
}

func (s *InMemoryWalletStore) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*wallet.Wallet, error) {
	// Create a filter function that checks customer ID
	filterFn := func(ctx context.Context, w *wallet.Wallet, filter interface{}) bool {
		if !walletFilterFn(ctx, w, filter) {
			return false
		}
		return w.CustomerID == customerID
	}

	// List all wallets with the customer ID filter
	wallets, err := s.wallets.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve customer wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return wallets, nil
}

func (s *InMemoryWalletStore) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	w, err := s.GetWalletByID(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve wallet for status update").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrNotFound)
	}

	w.WalletStatus = status
	if err := s.wallets.Update(ctx, id, w); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet status").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// FindEligibleCredits finds eligible credits for a wallet
func (s *InMemoryWalletStore) FindEligibleCredits(ctx context.Context, walletID string, requiredAmount decimal.Decimal, pageSize int) ([]*wallet.Transaction, error) {
	var allCredits []*wallet.Transaction

	// Get all eligible credits first
	credits, err := s.transactions.List(ctx, nil, func(ctx context.Context, t *wallet.Transaction, filter interface{}) bool {
		if t == nil {
			return false
		}

		// Check basic conditions
		if t.WalletID != walletID ||
			t.Type != types.TransactionTypeCredit ||
			t.CreditsAvailable.LessThanOrEqual(decimal.Zero) ||
			t.Status != types.StatusPublished {
			return false
		}

		// Check expiry date
		if t.ExpiryDate != nil && t.ExpiryDate.Before(time.Now().UTC()) {
			return false
		}

		return true
	}, nil)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query valid credits").
			WithReportableDetails(map[string]interface{}{
				"wallet_id":       walletID,
				"required_amount": requiredAmount,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Sort credits by priority (ascending), expiry date (ascending), and credit amount (descending)
	sort.Slice(credits, func(i, j int) bool {
		// First, sort by priority (nil values come last)
		if credits[i].Priority == nil && credits[j].Priority != nil {
			return false
		}
		if credits[i].Priority != nil && credits[j].Priority == nil {
			return true
		}
		if credits[i].Priority != nil && credits[j].Priority != nil && *credits[i].Priority != *credits[j].Priority {
			return *credits[i].Priority < *credits[j].Priority
		}

		// Then sort by expiry date (nil dates come last)
		if credits[i].ExpiryDate == nil && credits[j].ExpiryDate != nil {
			return false
		}
		if credits[i].ExpiryDate != nil && credits[j].ExpiryDate == nil {
			return true
		}
		if credits[i].ExpiryDate != nil && credits[j].ExpiryDate != nil && !credits[i].ExpiryDate.Equal(*credits[j].ExpiryDate) {
			return credits[i].ExpiryDate.Before(*credits[j].ExpiryDate)
		}

		// Finally, sort by credit amount (descending)
		return credits[i].CreditsAvailable.GreaterThan(credits[j].CreditsAvailable)
	})

	// Collect only enough credits to satisfy the required amount, respecting pageSize
	var totalAvailable decimal.Decimal
	for _, credit := range credits {
		allCredits = append(allCredits, credit)
		totalAvailable = totalAvailable.Add(credit.CreditsAvailable)

		if totalAvailable.GreaterThanOrEqual(requiredAmount) || len(allCredits) == pageSize {
			break
		}
	}

	return allCredits, nil
}

// ConsumeCredits consumes credits from a wallet
func (s *InMemoryWalletStore) ConsumeCredits(ctx context.Context, credits []*wallet.Transaction, amount decimal.Decimal) error {
	remainingAmount := amount

	for _, credit := range credits {
		if remainingAmount.IsZero() {
			break
		}

		toConsume := decimal.Min(remainingAmount, credit.CreditsAvailable)
		newAvailable := credit.CreditsAvailable.Sub(toConsume)

		credit.CreditsAvailable = newAvailable
		credit.UpdatedAt = time.Now().UTC()
		credit.UpdatedBy = types.GetUserID(ctx)

		// Update credit's available amount
		if err := s.transactions.Update(ctx, credit.ID, credit); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update credit available amount").
				WithReportableDetails(map[string]interface{}{
					"credit_id": credit.ID,
					"amount":    toConsume,
				}).
				Mark(ierr.ErrDatabase)
		}

		remainingAmount = remainingAmount.Sub(toConsume)
	}

	return nil
}

// CreateTransaction creates a new wallet transaction record
func (s *InMemoryWalletStore) CreateTransaction(ctx context.Context, tx *wallet.Transaction) error {
	// Set environment ID from context if not already set
	if tx.EnvironmentID == "" {
		tx.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	if err := s.transactions.Create(ctx, tx.ID, tx); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create wallet transaction").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": tx.WalletID,
				"type":      tx.Type,
				"amount":    tx.Amount,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// UpdateWalletBalance updates the wallet's balance and credit balance
func (s *InMemoryWalletStore) UpdateWalletBalance(ctx context.Context, walletID string, finalBalance, newCreditBalance decimal.Decimal) error {
	w, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve wallet for balance update").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": walletID,
			}).
			Mark(ierr.ErrNotFound)
	}

	w.Balance = finalBalance
	w.CreditBalance = newCreditBalance
	w.UpdatedAt = time.Now().UTC()
	w.UpdatedBy = types.GetUserID(ctx)

	if err := s.wallets.Update(ctx, walletID, w); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet balance").
			WithReportableDetails(map[string]interface{}{
				"wallet_id":      walletID,
				"balance":        finalBalance,
				"credit_balance": newCreditBalance,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (s *InMemoryWalletStore) GetTransactionByID(ctx context.Context, id string) (*wallet.Transaction, error) {
	tx, err := s.transactions.Get(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve transaction").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return tx, nil
}

func (s *InMemoryWalletStore) ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*wallet.Transaction, error) {
	transactions, err := s.transactions.List(ctx, f, transactionFilterFn, transactionSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return transactions, nil
}

func (s *InMemoryWalletStore) ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*wallet.Transaction, error) {
	// Create a copy of the filter without pagination
	filterCopy := *f
	filterCopy.QueryFilter.Limit = nil

	transactions, err := s.transactions.List(ctx, &filterCopy, transactionFilterFn, transactionSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list all wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return transactions, nil
}

func (s *InMemoryWalletStore) CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error) {
	count, err := s.transactions.Count(ctx, f, transactionFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryWalletStore) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	txn, err := s.GetTransactionByID(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve transaction for status update").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
				"status":         status,
			}).
			Mark(ierr.ErrNotFound)
	}

	txn.TxStatus = status
	if err := s.transactions.Update(ctx, id, txn); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update transaction status").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
				"status":         status,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryWalletStore) Clear() {
	s.wallets.Clear()
	s.transactions.Clear()
}

// UpdateWallet updates a wallet in the in-memory store
func (s *InMemoryWalletStore) UpdateWallet(ctx context.Context, id string, w *wallet.Wallet) error {
	// Check if wallet exists and belongs to tenant
	existing, err := s.wallets.Get(ctx, id)
	if err != nil || existing.TenantID != types.GetTenantID(ctx) || existing.Status != types.StatusPublished {
		return ierr.NewError("wallet not found").
			WithHint("The wallet may not exist or may not belong to this tenant").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Update fields if provided
	if w.Name != "" {
		existing.Name = w.Name
	}
	if w.Description != "" {
		existing.Description = w.Description
	}
	if w.Metadata != nil {
		existing.Metadata = w.Metadata
	}
	if w.AutoTopupTrigger != "" {
		existing.AutoTopupTrigger = w.AutoTopupTrigger
	}
	if !w.AutoTopupMinBalance.IsZero() {
		existing.AutoTopupMinBalance = w.AutoTopupMinBalance
	}
	if !w.AutoTopupAmount.IsZero() {
		existing.AutoTopupAmount = w.AutoTopupAmount
	}
	// Update config if provided (WalletConfig is a struct type, so we always update it)
	existing.Config = w.Config

	// Update metadata
	existing.UpdatedBy = types.GetUserID(ctx)
	existing.UpdatedAt = time.Now().UTC()

	// Save back to store
	if err := s.wallets.Update(ctx, id, existing); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	*w = *existing

	return nil
}
