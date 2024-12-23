package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryWalletRepository struct {
	mu           sync.RWMutex
	wallets      map[string]*wallet.Wallet
	transactions map[string]*wallet.Transaction
}

func NewInMemoryWalletStore() *InMemoryWalletRepository {
	return &InMemoryWalletRepository{
		wallets:      make(map[string]*wallet.Wallet),
		transactions: make(map[string]*wallet.Transaction),
	}
}

func (r *InMemoryWalletRepository) CreateWallet(ctx context.Context, w *wallet.Wallet) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.wallets[w.ID]; exists {
		return fmt.Errorf("wallet already exists")
	}

	r.wallets[w.ID] = w
	return nil
}

func (r *InMemoryWalletRepository) GetWalletByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if w, exists := r.wallets[id]; exists {
		return w, nil
	}
	return nil, fmt.Errorf("wallet not found")
}

func (r *InMemoryWalletRepository) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*wallet.Wallet, error) {
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

func (r *InMemoryWalletRepository) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, exists := r.wallets[id]
	if !exists {
		return fmt.Errorf("wallet not found")
	}

	w.WalletStatus = status
	return nil
}

func (r *InMemoryWalletRepository) DebitWallet(ctx context.Context, op *wallet.WalletOperation) error {
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

func (r *InMemoryWalletRepository) CreditWallet(ctx context.Context, op *wallet.WalletOperation) error {
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

func (r *InMemoryWalletRepository) GetTransactionsByWalletID(ctx context.Context, walletID string, limit, offset int) ([]*wallet.Transaction, error) {
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

func (r *InMemoryWalletRepository) GetTransactionByID(ctx context.Context, id string) (*wallet.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if txn, exists := r.transactions[id]; exists {
		return txn, nil
	}
	return nil, fmt.Errorf("transaction not found")
}

func (r *InMemoryWalletRepository) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	txn, exists := r.transactions[id]
	if !exists {
		return fmt.Errorf("transaction not found")
	}

	txn.TxStatus = status
	return nil
}

func (r *InMemoryWalletRepository) WithTx(ctx context.Context, fn func(wallet.Repository) error) error {
	// Clone the current state to simulate transaction isolation
	r.mu.Lock()
	defer r.mu.Unlock()

	clonedWallets := make(map[string]*wallet.Wallet)
	for k, v := range r.wallets {
		cloned := *v
		clonedWallets[k] = &cloned
	}

	clonedTransactions := make(map[string]*wallet.Transaction)
	for k, v := range r.transactions {
		cloned := *v
		clonedTransactions[k] = &cloned
	}

	// Create a temporary repository for the transaction
	tempRepo := &InMemoryWalletRepository{
		wallets:      clonedWallets,
		transactions: clonedTransactions,
	}

	// Execute the transaction function
	if err := fn(tempRepo); err != nil {
		return err // Rollback by not applying changes
	}

	// Commit changes back to the main repository
	r.wallets = tempRepo.wallets
	r.transactions = tempRepo.transactions
	return nil
}
