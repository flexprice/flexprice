package wallet

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Repository defines the interface for wallet persistence operations
type Repository interface {
	// CreateWallet creates a new wallet and updates the wallet object with the created data
	CreateWallet(ctx context.Context, w *Wallet) error

	// GetWalletByID retrieves a wallet by its ID
	GetWalletByID(ctx context.Context, id string) (*Wallet, error)

	// GetWalletsByCustomerID retrieves all wallets for a customer
	GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*Wallet, error)

	// UpdateWalletStatus updates the status of a wallet
	UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error

	// UpdateWallet updates a wallet
	UpdateWallet(ctx context.Context, id string, wallet *Wallet) error

	// DebitWallet debits amount from wallet
	DebitWallet(ctx context.Context, req *WalletOperation) error

	// CreditWallet credits amount to wallet
	CreditWallet(ctx context.Context, req *WalletOperation) error

	// Transaction operations
	GetTransactionByID(ctx context.Context, id string) (*Transaction, error)
	ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*Transaction, error)
	ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*Transaction, error)
	CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error)
	UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error
}

// WalletOperation represents the request to credit or debit a wallet
type WalletOperation struct {
	WalletID      string                `json:"wallet_id"`
	Type          types.TransactionType `json:"type"`
	Amount        decimal.Decimal       `json:"amount"`
	ReferenceType string                `json:"reference_type,omitempty"`
	ReferenceID   string                `json:"reference_id,omitempty"`
	Description   string                `json:"description,omitempty"`
	Metadata      types.Metadata        `json:"metadata,omitempty"`
}
