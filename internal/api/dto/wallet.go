package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

// CreateWalletRequest represents the request to create a new wallet
type CreateWalletRequest struct {
	CustomerID          string                 `json:"customer_id" binding:"required"`
	Name                string                 `json:"name,omitempty"`
	Currency            string                 `json:"currency" binding:"required"`
	Description         string                 `json:"description,omitempty"`
	Metadata            types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    types.AutoTopupTrigger `json:"auto_topup_trigger,omitempty" default:"disabled"`
	AutoTopupMinBalance decimal.Decimal        `json:"auto_topup_min_balance,omitempty"`
	AutoTopupAmount     decimal.Decimal        `json:"auto_topup_amount,omitempty"`
}

// UpdateWalletRequest represents the request to update a wallet
type UpdateWalletRequest struct {
	Name                *string                 `json:"name,omitempty"`
	Description         *string                 `json:"description,omitempty"`
	Metadata            *types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    *types.AutoTopupTrigger `json:"auto_topup_trigger,omitempty" default:"disabled"`
	AutoTopupMinBalance *decimal.Decimal        `json:"auto_topup_min_balance,omitempty"`
	AutoTopupAmount     *decimal.Decimal        `json:"auto_topup_amount,omitempty"`
}

func (r *UpdateWalletRequest) Validate() error {
	if r.AutoTopupTrigger != nil {
		if err := r.AutoTopupTrigger.Validate(); err != nil {
			return err
		}
	}

	if *r.AutoTopupTrigger == types.AutoTopupTriggerBalanceBelowThreshold {
		if r.AutoTopupMinBalance == nil || r.AutoTopupAmount == nil {
			return errors.New(errors.ErrCodeValidation, "auto_topup_min_balance and auto_topup_amount are required when auto_topup_trigger is balance_below_threshold")
		}
		if r.AutoTopupMinBalance.LessThanOrEqual(decimal.Zero) {
			return errors.New(errors.ErrCodeValidation, "auto_topup_min_balance must be greater than 0")
		}
		if r.AutoTopupAmount.LessThanOrEqual(decimal.Zero) {
			return errors.New(errors.ErrCodeValidation, "auto_topup_amount must be greater than 0")
		}
	}

	return validator.New().Struct(r)
}

// ToWallet converts a create wallet request to a wallet
func (r *CreateWalletRequest) ToWallet(ctx context.Context) *wallet.Wallet {
	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return nil
	}

	return &wallet.Wallet{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
		CustomerID:          r.CustomerID,
		Name:                r.Name,
		Currency:            strings.ToLower(r.Currency),
		Description:         r.Description,
		Metadata:            r.Metadata,
		AutoTopupTrigger:    r.AutoTopupTrigger,
		AutoTopupMinBalance: r.AutoTopupMinBalance,
		AutoTopupAmount:     r.AutoTopupAmount,
		Balance:             decimal.Zero,
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
}

func (r *CreateWalletRequest) Validate() error {
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}
	if err := types.AutoTopupTrigger(r.AutoTopupTrigger).Validate(); err != nil {
		return err
	}
	return validator.New().Struct(r)
}

// WalletResponse represents a wallet in API responses
type WalletResponse struct {
	ID                  string                 `json:"id"`
	CustomerID          string                 `json:"customer_id"`
	Name                string                 `json:"name,omitempty"`
	Currency            string                 `json:"currency"`
	Description         string                 `json:"description,omitempty"`
	Balance             decimal.Decimal        `json:"balance"`
	WalletStatus        types.WalletStatus     `json:"wallet_status"`
	Metadata            types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    types.AutoTopupTrigger `json:"auto_topup_trigger"`
	AutoTopupMinBalance decimal.Decimal        `json:"auto_topup_min_balance"`
	AutoTopupAmount     decimal.Decimal        `json:"auto_topup_amount"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

// ToWalletResponse converts domain Wallet to WalletResponse
func FromWallet(w *wallet.Wallet) *WalletResponse {
	if w == nil {
		return nil
	}
	return &WalletResponse{
		ID:                  w.ID,
		CustomerID:          w.CustomerID,
		Currency:            w.Currency,
		Balance:             w.Balance,
		Name:                w.Name,
		Description:         w.Description,
		WalletStatus:        w.WalletStatus,
		Metadata:            w.Metadata,
		AutoTopupTrigger:    w.AutoTopupTrigger,
		AutoTopupMinBalance: w.AutoTopupMinBalance,
		AutoTopupAmount:     w.AutoTopupAmount,
		CreatedAt:           w.CreatedAt,
		UpdatedAt:           w.UpdatedAt,
	}
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

// WalletBalanceResponse represents the response for getting wallet balance
type WalletBalanceResponse struct {
	*wallet.Wallet
	RealTimeBalance     decimal.Decimal `json:"real_time_balance"`
	BalanceUpdatedAt    time.Time       `json:"balance_updated_at"`
	UnpaidInvoiceAmount decimal.Decimal `json:"unpaid_invoice_amount"`
	CurrentPeriodUsage  decimal.Decimal `json:"current_period_usage"`
}
