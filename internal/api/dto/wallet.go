package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

// CreateWalletRequest represents the request to create a new wallet
type CreateWalletRequest struct {
	CustomerID string         `json:"customer_id" binding:"required"`
	Currency   string         `json:"currency" binding:"required"`
	Metadata   types.Metadata `json:"metadata,omitempty"`
}

func (r *CreateWalletRequest) ToWallet(ctx context.Context) *wallet.Wallet {
	return &wallet.Wallet{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
		CustomerID:   r.CustomerID,
		Currency:     r.Currency,
		Metadata:     r.Metadata,
		Balance:      decimal.Zero,
		WalletStatus: types.WalletStatusActive,
		BaseModel:    types.GetDefaultBaseModel(ctx),
	}
}

func (r *CreateWalletRequest) Validate() error {
	return validator.New().Struct(r)
}

// WalletResponse represents a wallet in API responses
type WalletResponse struct {
	ID           string             `json:"id"`
	CustomerID   string             `json:"customer_id"`
	Currency     string             `json:"currency"`
	Balance      decimal.Decimal    `json:"balance"`
	WalletStatus types.WalletStatus `json:"wallet_status"`
	Metadata     types.Metadata     `json:"metadata,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// WalletTransactionResponse represents a wallet transaction in API responses
type WalletTransactionResponse struct {
	ID                string                  `json:"id"`
	WalletID          string                  `json:"wallet_id"`
	Type              string                  `json:"type"`
	Amount            decimal.Decimal         `json:"amount"`
	BalanceBefore     decimal.Decimal         `json:"balance_before"`
	BalanceAfter      decimal.Decimal         `json:"balance_after"`
	TransactionStatus types.TransactionStatus `json:"transaction_status"`
	ReferenceType     string                  `json:"reference_type,omitempty"`
	ReferenceID       string                  `json:"reference_id,omitempty"`
	Description       string                  `json:"description,omitempty"`
	Metadata          types.Metadata          `json:"metadata,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
}

// FromWalletTransaction converts a wallet transaction to a WalletTransactionResponse
func FromWalletTransaction(t *wallet.Transaction) *WalletTransactionResponse {
	return &WalletTransactionResponse{
		ID:                t.ID,
		WalletID:          t.WalletID,
		Type:              string(t.Type),
		Amount:            t.Amount,
		BalanceBefore:     t.BalanceBefore,
		BalanceAfter:      t.BalanceAfter,
		TransactionStatus: t.TxStatus,
		ReferenceType:     t.ReferenceType,
		ReferenceID:       t.ReferenceID,
		Description:       t.Description,
		Metadata:          t.Metadata,
		CreatedAt:         t.CreatedAt,
	}
}

// ListWalletTransactionsResponse represents the response for listing wallet transactions
type ListWalletTransactionsResponse = types.ListResponse[*WalletTransactionResponse]

// TopUpWalletRequest represents a request to add credits to a wallet
type TopUpWalletRequest struct {
	Amount      decimal.Decimal `json:"amount" binding:"required"`
	Description string          `json:"description,omitempty"`
	Metadata    types.Metadata  `json:"metadata,omitempty"`
}

// WalletBalanceResponse represents the real-time balance of a wallet
type WalletBalanceResponse struct {
	RealTimeBalance  decimal.Decimal `json:"real_time_balance"`
	BalanceUpdatedAt time.Time       `json:"balance_updated_at"`
	*wallet.Wallet
}
