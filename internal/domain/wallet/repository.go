package wallet

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Repository defines the interface for wallet persistence operations
type Repository interface {
	// Wallet operations
	CreateWallet(ctx context.Context, w *Wallet) error
	GetWalletByID(ctx context.Context, id string) (*Wallet, error)
	GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*Wallet, error)
	GetWalletsByFilter(ctx context.Context, filter *types.WalletFilter) ([]*Wallet, error)
	UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error
	UpdateWallet(ctx context.Context, id string, wallet *Wallet) error

	// Transaction operations
	GetTransactionByID(ctx context.Context, id string) (*Transaction, error)
	GetTransactionByIdempotencyKey(ctx context.Context, idempotencyKey string) (*Transaction, error)
	ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*Transaction, error)
	ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*Transaction, error)
	CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error)
	UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error
	UpdateTransaction(ctx context.Context, tx *Transaction) error

	// Credit/Debit specific operations
	FindEligibleCredits(ctx context.Context, walletID string, requiredAmount decimal.Decimal, pageSize int, timeReference time.Time) ([]*Transaction, error)
	ConsumeCredits(ctx context.Context, credits []*Transaction, amount decimal.Decimal) error
	CreateTransaction(ctx context.Context, tx *Transaction) error
	UpdateWalletBalance(ctx context.Context, walletID string, finalBalance, newCreditBalance decimal.Decimal) error

	// Export operations
	GetCreditTopupsForExport(ctx context.Context, tenantID, envID string, startTime, endTime time.Time, limit, offset int) ([]*CreditTopupsExportData, error)
}
