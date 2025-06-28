package dto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreateWalletRequest represents the request to create a wallet
type CreateWalletRequest struct {
	CustomerID string `json:"customer_id,omitempty"`

	// external_customer_id is the customer id in the external system
	ExternalCustomerID  string                 `json:"external_customer_id,omitempty"`
	Name                string                 `json:"name,omitempty"`
	Currency            string                 `json:"currency" binding:"required"`
	Description         string                 `json:"description,omitempty"`
	Metadata            types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    types.AutoTopupTrigger `json:"auto_topup_trigger,omitempty"`
	AutoTopupMinBalance decimal.Decimal        `json:"auto_topup_min_balance,omitempty"`
	AutoTopupAmount     decimal.Decimal        `json:"auto_topup_amount,omitempty"`
	WalletType          types.WalletType       `json:"wallet_type"`
	Config              *types.WalletConfig    `json:"config,omitempty"`
	// amount in the currency =  number of credits * conversion_rate
	// ex if conversion_rate is 1, then 1 USD = 1 credit
	// ex if conversion_rate is 2, then 1 USD = 0.5 credits
	// ex if conversion_rate is 0.5, then 1 USD = 2 credits
	ConversionRate decimal.Decimal `json:"conversion_rate" default:"1"`
	// initial_credits_to_load is the number of credits to load to the wallet
	// if not provided, the wallet will be created with 0 balance
	// NOTE: this is not the amount in the currency, but the number of credits
	InitialCreditsToLoad decimal.Decimal `json:"initial_credits_to_load,omitempty" default:"0"`
	// initial_credits_to_load_expiry_date YYYYMMDD format in UTC timezone (optional to set nil means no expiry)
	// for ex 20250101 means the credits will expire on 2025-01-01 00:00:00 UTC
	// hence they will be available for use until 2024-12-31 23:59:59 UTC
	InitialCreditsToLoadExpiryDate *int `json:"initial_credits_to_load_expiry_date,omitempty"`

	// initial_credits_expiry_date_utc is the expiry date in UTC timezone (optional to set nil means no expiry)
	// ex 2025-01-01 00:00:00 UTC
	InitialCreditsExpiryDateUTC *time.Time `json:"initial_credits_expiry_date_utc,omitempty"`
}

// UpdateWalletRequest represents the request to update a wallet
type UpdateWalletRequest struct {
	Name                *string                 `json:"name,omitempty"`
	Description         *string                 `json:"description,omitempty"`
	Metadata            *types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    *types.AutoTopupTrigger `json:"auto_topup_trigger,omitempty"`
	AutoTopupMinBalance *decimal.Decimal        `json:"auto_topup_min_balance,omitempty"`
	AutoTopupAmount     *decimal.Decimal        `json:"auto_topup_amount,omitempty"`
	Config              *types.WalletConfig     `json:"config,omitempty"`
}

func (r *UpdateWalletRequest) Validate() error {
	if r.AutoTopupTrigger != nil {
		if err := r.AutoTopupTrigger.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Invalid auto-topup trigger").
				Mark(ierr.ErrValidation)
		}
	}

	if r.AutoTopupTrigger != nil && *r.AutoTopupTrigger == types.AutoTopupTriggerBalanceBelowThreshold {
		if r.AutoTopupMinBalance == nil || r.AutoTopupAmount == nil {
			return ierr.NewError("auto_topup_min_balance and auto_topup_amount are required when auto_topup_trigger is balance_below_threshold").
				WithHint("Both minimum balance and topup amount must be provided for threshold-based auto-topup").
				Mark(ierr.ErrValidation)
		}
		if r.AutoTopupMinBalance.LessThanOrEqual(decimal.Zero) {
			return ierr.NewError("auto_topup_min_balance must be greater than 0").
				WithHint("Minimum balance threshold must be a positive value").
				WithReportableDetails(map[string]interface{}{
					"min_balance": r.AutoTopupMinBalance,
				}).
				Mark(ierr.ErrValidation)
		}
		if r.AutoTopupAmount.LessThanOrEqual(decimal.Zero) {
			return ierr.NewError("auto_topup_amount must be greater than 0").
				WithHint("Auto-topup amount must be a positive value").
				WithReportableDetails(map[string]interface{}{
					"topup_amount": r.AutoTopupAmount,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if r.Config != nil {
		if err := r.Config.Validate(); err != nil {
			return err
		}
	}

	return validator.ValidateRequest(r)
}

// ToWallet converts a create wallet request to a wallet
func (r *CreateWalletRequest) ToWallet(ctx context.Context) *wallet.Wallet {
	if r.ConversionRate.IsZero() {
		r.ConversionRate = decimal.NewFromInt(1)
	}

	if r.WalletType == "" {
		r.WalletType = types.WalletTypePrePaid
	}

	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return nil
	}

	if r.Config == nil {
		r.Config = types.GetDefaultWalletConfig()
	}

	if r.ConversionRate.LessThanOrEqual(decimal.NewFromInt(0)) {
		r.ConversionRate = decimal.NewFromInt(1)
	}

	if r.AutoTopupTrigger == "" {
		r.AutoTopupTrigger = types.AutoTopupTriggerDisabled
	}

	if r.Name == "" {
		if r.WalletType == types.WalletTypePrePaid {
			r.Name = fmt.Sprintf("Prepaid Wallet - %s", r.Currency)
		} else if r.WalletType == types.WalletTypePromotional {
			r.Name = fmt.Sprintf("Promotional Wallet - %s", r.Currency)
		}
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
		CreditBalance:       decimal.Zero,
		WalletStatus:        types.WalletStatusActive,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
		WalletType:          r.WalletType,
		Config:              lo.FromPtr(r.Config),
		ConversionRate:      r.ConversionRate,
	}
}

func (r *CreateWalletRequest) Validate() error {
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}
	if err := types.AutoTopupTrigger(r.AutoTopupTrigger).Validate(); err != nil {
		return err
	}
	if err := r.WalletType.Validate(); err != nil {
		return err
	}
	if r.ConversionRate.LessThan(decimal.Zero) {
		return ierr.NewError("conversion_rate must be greater than 0").
			WithHint("Conversion rate must be a positive value").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": r.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.CustomerID == "" && r.ExternalCustomerID == "" {
		return ierr.NewError("customer_id or external_customer_id is required").
			WithHint("Please provide either customer_id or external_customer_id").
			Mark(ierr.ErrValidation)
	}

	if r.ExternalCustomerID != "" {
		if err := types.ValidateExternalCustomerID(r.ExternalCustomerID); err != nil {
			return err
		}
	}

	if r.CustomerID != "" {
		if err := types.ValidateCustomerID(r.CustomerID); err != nil {
			return err
		}
	}

	if r.InitialCreditsExpiryDateUTC != nil {
		if r.InitialCreditsExpiryDateUTC.Before(time.Now().UTC()) {
			return ierr.NewError("initial_credits_to_load_expiry_date_utc cannot be in the past").
				WithHint("Expiry date must be in the future").
				Mark(ierr.ErrValidation)
		}
	}
	return validator.ValidateRequest(r)
}

// WalletResponse represents a wallet in API responses
type WalletResponse struct {
	ID                  string                 `json:"id"`
	CustomerID          string                 `json:"customer_id"`
	Name                string                 `json:"name,omitempty"`
	Currency            string                 `json:"currency"`
	Description         string                 `json:"description,omitempty"`
	Balance             decimal.Decimal        `json:"balance"`
	CreditBalance       decimal.Decimal        `json:"credit_balance"`
	WalletStatus        types.WalletStatus     `json:"wallet_status"`
	Metadata            types.Metadata         `json:"metadata,omitempty"`
	AutoTopupTrigger    types.AutoTopupTrigger `json:"auto_topup_trigger"`
	AutoTopupMinBalance decimal.Decimal        `json:"auto_topup_min_balance"`
	AutoTopupAmount     decimal.Decimal        `json:"auto_topup_amount"`
	WalletType          types.WalletType       `json:"wallet_type"`
	Config              types.WalletConfig     `json:"config,omitempty"`
	ConversionRate      decimal.Decimal        `json:"conversion_rate"`
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
		CreditBalance:       w.CreditBalance,
		Name:                w.Name,
		Description:         w.Description,
		WalletStatus:        w.WalletStatus,
		Metadata:            w.Metadata,
		AutoTopupTrigger:    w.AutoTopupTrigger,
		AutoTopupMinBalance: w.AutoTopupMinBalance,
		AutoTopupAmount:     w.AutoTopupAmount,
		WalletType:          w.WalletType,
		Config:              w.Config,
		ConversionRate:      w.ConversionRate,
		CreatedAt:           w.CreatedAt,
		UpdatedAt:           w.UpdatedAt,
	}
}

func ToWalletBalanceResponse(w *wallet.Wallet) *WalletBalanceResponse {
	return &WalletBalanceResponse{
		Wallet: w,
	}
}

// WalletTransactionResponse represents a wallet transaction in API responses
type WalletTransactionResponse struct {
	ID                  string                      `json:"id"`
	WalletID            string                      `json:"wallet_id"`
	Type                string                      `json:"type"`
	Amount              decimal.Decimal             `json:"amount"`
	CreditAmount        decimal.Decimal             `json:"credit_amount"`
	CreditBalanceBefore decimal.Decimal             `json:"credit_balance_before"`
	CreditBalanceAfter  decimal.Decimal             `json:"credit_balance_after"`
	TransactionStatus   types.TransactionStatus     `json:"transaction_status"`
	ExpiryDate          *time.Time                  `json:"expiry_date,omitempty"`
	Priority            *int                        `json:"priority,omitempty"`
	CreditsAvailable    decimal.Decimal             `json:"credits_available,omitempty"`
	TransactionReason   types.TransactionReason     `json:"transaction_reason,omitempty"`
	ReferenceType       types.WalletTxReferenceType `json:"reference_type,omitempty"`
	ReferenceID         string                      `json:"reference_id,omitempty"`
	Description         string                      `json:"description,omitempty"`
	Metadata            types.Metadata              `json:"metadata,omitempty"`
	CreatedAt           time.Time                   `json:"created_at"`
	UpdatedAt           time.Time                   `json:"updated_at"`
}

// FromWalletTransaction converts a wallet transaction to a WalletTransactionResponse
func FromWalletTransaction(t *wallet.Transaction) *WalletTransactionResponse {
	return &WalletTransactionResponse{
		ID:                  t.ID,
		WalletID:            t.WalletID,
		Type:                string(t.Type),
		Amount:              t.Amount,
		CreditAmount:        t.CreditAmount,
		CreditBalanceBefore: t.CreditBalanceBefore,
		CreditBalanceAfter:  t.CreditBalanceAfter,
		TransactionStatus:   t.TxStatus,
		ExpiryDate:          t.ExpiryDate,
		Priority:            t.Priority,
		CreditsAvailable:    t.CreditsAvailable,
		TransactionReason:   t.TransactionReason,
		ReferenceType:       t.ReferenceType,
		ReferenceID:         t.ReferenceID,
		Description:         t.Description,
		Metadata:            t.Metadata,
		CreatedAt:           t.CreatedAt,
	}
}

// ListWalletTransactionsResponse represents the response for listing wallet transactions
type ListWalletTransactionsResponse = types.ListResponse[*WalletTransactionResponse]

// TopUpWalletRequest represents a request to add credits to a wallet
type TopUpWalletRequest struct {
	// credits_to_add is the number of credits to add to the wallet
	CreditsToAdd decimal.Decimal `json:"credits_to_add"`
	// amount is the amount in the currency of the wallet to be added
	// NOTE: this is not the number of credits to add, but the amount in the currency
	// amount = credits_to_add * conversion_rate
	// if both amount and credits_to_add are provided, amount will be ignored
	// ex if the wallet has a conversion_rate of 2 then adding an amount of
	// 10 USD in the wallet wil add 5 credits in the wallet
	Amount            decimal.Decimal         `json:"amount"`
	TransactionReason types.TransactionReason `json:"transaction_reason,omitempty" binding:"required"`
	// expiry_date YYYYMMDD format in UTC timezone (optional to set nil means no expiry)
	// for ex 20250101 means the credits will expire on 2025-01-01 00:00:00 UTC
	// hence they will be available for use until 2024-12-31 23:59:59 UTC
	ExpiryDate *int `json:"-"`
	// expiry_date_utc is the expiry date in UTC timezone
	// ex 2025-01-01 00:00:00 UTC
	ExpiryDateUTC *time.Time `json:"expiry_date_utc,omitempty"`
	// priority is the priority of the transaction
	// lower number means higher priority
	// default is nil which means no priority at all
	Priority *int `json:"priority,omitempty"`
	// idempotency_key is a unique key for the transaction
	IdempotencyKey *string `json:"idempotency_key" binding:"required"`
	// description to add any specific details about the transaction
	Description string `json:"description,omitempty"`
	// metadata is a map of key-value pairs to store any additional information about the transaction
	Metadata types.Metadata `json:"metadata,omitempty"`
}

func (r *TopUpWalletRequest) Validate() error {
	if r.CreditsToAdd.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("credits_to_add must be greater than 0").
			WithHint("Credits to add must be a positive value").
			WithReportableDetails(map[string]interface{}{
				"credits_to_add": r.CreditsToAdd,
			}).
			Mark(ierr.ErrValidation)
	}

	allowedTransactionReasons := []types.TransactionReason{
		types.TransactionReasonFreeCredit,
		types.TransactionReasonPurchasedCreditInvoiced,
		types.TransactionReasonPurchasedCreditDirect,
		types.TransactionReasonSubscriptionCredit,
		types.TransactionReasonCreditNote,
	}

	if !lo.Contains(allowedTransactionReasons, r.TransactionReason) {
		return ierr.NewError("transaction_reason must be one of the allowed values").
			WithHint("Invalid transaction reason").
			WithReportableDetails(map[string]interface{}{
				"transaction_reason": r.TransactionReason,
				"allowed_reasons":    allowedTransactionReasons,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.ExpiryDateUTC != nil {
		// check if the expiry date is in the past
		if r.ExpiryDateUTC.Before(time.Now().UTC()) {
			return ierr.NewError("expiry_date_utc cannot be in the past").
				WithHint("Expiry date must be in the future").
				Mark(ierr.ErrValidation)
		}
	}

	if r.Priority != nil && *r.Priority < 0 {
		return ierr.NewError("priority must be greater than or equal to 0").
			WithHint("Priority must be a non-negative integer").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// WalletBalanceResponse represents the response for getting wallet balance
type WalletBalanceResponse struct {
	*wallet.Wallet
	RealTimeBalance       *decimal.Decimal `json:"real_time_balance,omitempty"`
	RealTimeCreditBalance *decimal.Decimal `json:"real_time_credit_balance,omitempty"`
	BalanceUpdatedAt      *time.Time       `json:"balance_updated_at,omitempty"`
	UnpaidInvoiceAmount   *decimal.Decimal `json:"unpaid_invoice_amount,omitempty"`
	CurrentPeriodUsage    *decimal.Decimal `json:"current_period_usage,omitempty"`
}

type ExpiredCreditsResponseItem struct {
	TenantID string `json:"tenant_id"`
	Count    int    `json:"count"`
}

type ExpiredCreditsResponse struct {
	Items   []*ExpiredCreditsResponseItem `json:"items"`
	Total   int                           `json:"total"`
	Success int                           `json:"success"`
	Failed  int                           `json:"failed"`
}

type GetCustomerWalletsRequest struct {
	ID                     string `form:"id"`
	LookupKey              string `form:"lookup_key"`
	IncludeRealTimeBalance bool   `form:"include_real_time_balance" default:"false"`
}

func (r *GetCustomerWalletsRequest) Validate() error {
	if r.ID == "" && r.LookupKey == "" {
		return ierr.NewError("id or lookup_key is required").
			WithHint("Please provide either id or lookup_key").
			Mark(ierr.ErrValidation)
	}

	if r.ID != "" && r.LookupKey != "" {
		return ierr.NewError("only one of id or lookup_key is required").
			WithHint("Please provide either 'id' or 'lookup_key', but not both.").
			Mark(ierr.ErrValidation)
	}

	return nil
}
