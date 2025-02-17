package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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
		if t.ReferenceType != *f.ReferenceType || t.ReferenceID != *f.ReferenceID {
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
		return fmt.Errorf("wallet cannot be nil")
	}

	// Set default status if not set
	if w.Status == "" {
		w.Status = types.StatusPublished
	}
	if w.WalletStatus == "" {
		w.WalletStatus = types.WalletStatusActive
	}

	return s.wallets.Create(ctx, w.ID, w)
}

func (s *InMemoryWalletStore) GetWalletByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	return s.wallets.Get(ctx, id)
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
	return s.wallets.List(ctx, nil, filterFn, nil)
}

func (s *InMemoryWalletStore) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	w, err := s.GetWalletByID(ctx, id)
	if err != nil {
		return err
	}

	w.WalletStatus = status
	return s.wallets.Update(ctx, id, w)
}

func (s *InMemoryWalletStore) DebitWallet(ctx context.Context, op *wallet.WalletOperation) error {
	w, err := s.GetWalletByID(ctx, op.WalletID)
	if err != nil {
		return err
	}

	if w.Balance.LessThan(op.Amount) {
		return fmt.Errorf("insufficient balance")
	}

	// Create a transaction
	txn := &wallet.Transaction{
		ID:            fmt.Sprintf("txn-%s-%d", op.WalletID, time.Now().UnixNano()),
		WalletID:      op.WalletID,
		Type:          op.Type,
		Amount:        op.Amount,
		BalanceBefore: w.Balance,
		BalanceAfter:  w.Balance.Sub(op.Amount),
		TxStatus:      types.TransactionStatusCompleted,
		Description:   op.Description,
		Metadata:      op.Metadata,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := s.transactions.Create(ctx, txn.ID, txn); err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	w.Balance = w.Balance.Sub(op.Amount)
	return s.wallets.Update(ctx, w.ID, w)
}

func (s *InMemoryWalletStore) CreditWallet(ctx context.Context, op *wallet.WalletOperation) error {
	w, err := s.GetWalletByID(ctx, op.WalletID)
	if err != nil {
		return err
	}

	// Create a transaction
	txn := &wallet.Transaction{
		ID:            fmt.Sprintf("txn-%s-%d", op.WalletID, time.Now().UnixNano()),
		WalletID:      op.WalletID,
		Type:          op.Type,
		Amount:        op.Amount,
		BalanceBefore: w.Balance,
		BalanceAfter:  w.Balance.Add(op.Amount),
		TxStatus:      types.TransactionStatusCompleted,
		Description:   op.Description,
		Metadata:      op.Metadata,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := s.transactions.Create(ctx, txn.ID, txn); err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	w.Balance = w.Balance.Add(op.Amount)
	return s.wallets.Update(ctx, w.ID, w)
}

func (s *InMemoryWalletStore) GetTransactionByID(ctx context.Context, id string) (*wallet.Transaction, error) {
	return s.transactions.Get(ctx, id)
}

func (s *InMemoryWalletStore) ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*wallet.Transaction, error) {
	return s.transactions.List(ctx, f, transactionFilterFn, transactionSortFn)
}

func (s *InMemoryWalletStore) ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*wallet.Transaction, error) {
	// Create a copy of the filter without pagination
	filterCopy := *f
	filterCopy.QueryFilter.Limit = nil
	return s.transactions.List(ctx, &filterCopy, transactionFilterFn, transactionSortFn)
}

func (s *InMemoryWalletStore) CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error) {
	return s.transactions.Count(ctx, f, transactionFilterFn)
}

func (s *InMemoryWalletStore) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	txn, err := s.GetTransactionByID(ctx, id)
	if err != nil {
		return err
	}

	txn.TxStatus = status
	return s.transactions.Update(ctx, id, txn)
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
		return errors.New(errors.ErrCodeNotFound, "wallet not found")
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

	// Update metadata
	existing.UpdatedBy = types.GetUserID(ctx)
	existing.UpdatedAt = time.Now().UTC()

	// Save back to store
	if err := s.wallets.Update(ctx, id, existing); err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError, "failed to update wallet")
	}
	*w = *existing

	return nil
}
