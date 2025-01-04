package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryWalletStore struct {
	mu           sync.RWMutex
	wallets      map[string]*wallet.Wallet
	transactions map[string]*wallet.Transaction
}

func NewInMemoryWalletStore() *InMemoryWalletStore {
	return &InMemoryWalletStore{
		wallets:      make(map[string]*wallet.Wallet),
		transactions: make(map[string]*wallet.Transaction),
	}
}

func (r *InMemoryWalletStore) CreateWallet(ctx context.Context, w *wallet.Wallet) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.wallets[w.ID]; exists {
		return fmt.Errorf("wallet already exists")
	}

	r.wallets[w.ID] = w
	return nil
}

func (r *InMemoryWalletStore) GetWalletByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if w, exists := r.wallets[id]; exists {
		return w, nil
	}
	return nil, fmt.Errorf("wallet not found")
}

func (r *InMemoryWalletStore) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*wallet.Wallet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*wallet.Wallet
	for _, w := range r.wallets {
		if w.CustomerID == customerID {
			result = append(result, w)
		}
	}
	return result, nil
}

func (r *InMemoryWalletStore) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, exists := r.wallets[id]
	if !exists {
		return fmt.Errorf("wallet not found")
	}

	w.WalletStatus = status
	return nil
}

func (r *InMemoryWalletStore) DebitWallet(ctx context.Context, op *wallet.WalletOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, exists := r.wallets[op.WalletID]
	if !exists {
		return fmt.Errorf("wallet not found")
	}

	if w.Balance.LessThan(op.Amount) {
		return fmt.Errorf("insufficient balance")
	}

	// Create a transaction
	txn := &wallet.Transaction{
		ID:            fmt.Sprintf("txn-%s", op.WalletID),
		WalletID:      op.WalletID,
		Type:          op.Type,
		Amount:        op.Amount,
		BalanceBefore: w.Balance,
		BalanceAfter:  w.Balance.Sub(op.Amount),
		TxStatus:      types.TransactionStatusCompleted,
		Description:   op.Description,
		Metadata:      op.Metadata,
	}
	r.transactions[txn.ID] = txn

	w.Balance = w.Balance.Sub(op.Amount)
	return nil
}

func (r *InMemoryWalletStore) CreditWallet(ctx context.Context, op *wallet.WalletOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, exists := r.wallets[op.WalletID]
	if !exists {
		return fmt.Errorf("wallet not found")
	}

	// Create a transaction
	txn := &wallet.Transaction{
		ID:            fmt.Sprintf("txn-%s", op.WalletID),
		WalletID:      op.WalletID,
		Type:          op.Type,
		Amount:        op.Amount,
		BalanceBefore: w.Balance,
		BalanceAfter:  w.Balance.Add(op.Amount),
		TxStatus:      types.TransactionStatusCompleted,
		Description:   op.Description,
		Metadata:      op.Metadata,
	}
	r.transactions[txn.ID] = txn

	w.Balance = w.Balance.Add(op.Amount)
	return nil
}

func (r *InMemoryWalletStore) GetTransactionsByWalletID(ctx context.Context, walletID string, limit, offset int) ([]*wallet.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*wallet.Transaction
	for _, txn := range r.transactions {
		if txn.WalletID == walletID {
			result = append(result, txn)
		}
	}

	// Apply pagination
	if offset >= len(result) {
		return []*wallet.Transaction{}, nil
	}

	end := offset + limit
	if end > len(result) {
		end = len(result)
	}

	return result[offset:end], nil
}

func (r *InMemoryWalletStore) GetTransactionByID(ctx context.Context, id string) (*wallet.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if txn, exists := r.transactions[id]; exists {
		return txn, nil
	}
	return nil, fmt.Errorf("transaction not found")
}

func (r *InMemoryWalletStore) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	txn, exists := r.transactions[id]
	if !exists {
		return fmt.Errorf("transaction not found")
	}

	txn.TxStatus = status
	return nil
}

func (s *InMemoryWalletStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wallets = make(map[string]*wallet.Wallet)
	s.transactions = make(map[string]*wallet.Transaction)
}
