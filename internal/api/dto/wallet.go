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
	ExternalCustomerID string              `json:"external_customer_id,omitempty"`
	Name               string              `json:"name,omitempty"`
	Currency           string              `json:"currency" binding:"required"`
	Description        string              `json:"description,omitempty"`
	Metadata           types.Metadata      `json:"metadata,omitempty"`
	WalletType         types.WalletType    `json:"wallet_type"`
	Config             *types.WalletConfig `json:"config,omitempty"`
	// amount in the currency =  number of credits * conversion_rate
	// ex if conversion_rate is 1, then 1 USD = 1 credit
	// ex if conversion_rate is 2, then 1 USD = 0.5 credits
	// ex if conversion_rate is 0.5, then 1 USD = 2 credits
	ConversionRate decimal.Decimal `json:"conversion_rate" default:"1" swaggertype:"string"`
	// initial_credits_to_load is the number of credits to load to the wallet
	// if not provided, the wallet will be created with 0 balance
	// NOTE: this is not the amount in the currency, but the number of credits
	InitialCreditsToLoad decimal.Decimal `json:"initial_credits_to_load,omitempty" default:"0" swaggertype:"string"`
	// initial_credits_to_load_expiry_date YYYYMMDD format in UTC timezone (optional to set nil means no expiry)
	// for ex 20250101 means the credits will expire on 2025-01-01 00:00:00 UTC
	// hence they will be available for use until 2024-12-31 23:59:59 UTC
	InitialCreditsToLoadExpiryDate *int `json:"initial_credits_to_load_expiry_date,omitempty"`

	// initial_credits_expiry_date_utc is the expiry date in UTC timezone (optional to set nil means no expiry)
	// ex 2025-01-01 00:00:00 UTC
	InitialCreditsExpiryDateUTC *time.Time `json:"initial_credits_expiry_date_utc,omitempty"`

	// alert_enabled is the flag to enable alerts for the wallet
	// defaults to true, can be explicitly set to false to disable alerts
	AlertEnabled bool `json:"alert_enabled,omitempty"`

	// alert_config is the alert configuration for the wallet (optional)
	AlertConfig *AlertConfig `json:"alert_config,omitempty"`

	// auto top-up object
	AutoTopup *types.AutoTopup `json:"auto_topup,omitempty"`

	PriceUnit *string `json:"price_unit,omitempty"`
}

type AlertConfig struct {
	Threshold *Threshold `json:"threshold,omitempty"`
}

type Threshold struct {
	Type  string          `json:"type"` //amount
	Value decimal.Decimal `json:"value" swaggertype:"string"`
}

// UpdateWalletRequest represents the request to update a wallet
type UpdateWalletRequest struct {
	Name         *string             `json:"name,omitempty"`
	Description  *string             `json:"description,omitempty"`
	Metadata     *types.Metadata     `json:"metadata,omitempty"`
	AutoTopup    *types.AutoTopup    `json:"auto_topup,omitempty"`
	Config       *types.WalletConfig `json:"config,omitempty"`
	AlertEnabled *bool               `json:"alert_enabled,omitempty"`
	AlertConfig  *AlertConfig        `json:"alert_config,omitempty"`
}

func (r *UpdateWalletRequest) Validate() error {
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

	if r.Name == "" {
		if r.WalletType == types.WalletTypePrePaid {
			r.Name = fmt.Sprintf("Prepaid Wallet - %s", r.Currency)
		} else if r.WalletType == types.WalletTypePromotional {
			r.Name = fmt.Sprintf("Promotional Wallet - %s", r.Currency)
		}
	}

	// Convert AlertConfig to types.AlertConfig
	var alertConfig *types.AlertConfig
	if r.AlertConfig != nil {
		alertConfig = &types.AlertConfig{
			Threshold: &types.WalletAlertThreshold{
				Type:  types.AlertThresholdType(r.AlertConfig.Threshold.Type),
				Value: r.AlertConfig.Threshold.Value,
			},
		}
	}

	return &wallet.Wallet{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
		CustomerID:     r.CustomerID,
		Name:           r.Name,
		Currency:       strings.ToLower(r.Currency),
		Description:    r.Description,
		Metadata:       r.Metadata,
		AutoTopup:      r.AutoTopup,
		Balance:        decimal.Zero,
		CreditBalance:  decimal.Zero,
		WalletStatus:   types.WalletStatusActive,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		WalletType:     r.WalletType,
		Config:         lo.FromPtr(r.Config),
		ConversionRate: r.ConversionRate,
		AlertEnabled:   true, // Always enabled by default
		AlertConfig:    alertConfig,
		AlertState:     string(types.AlertStateOk), // Always starts in "ok" state
	}
}

func (r *CreateWalletRequest) Validate() error {
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}
	if r.AutoTopup != nil {
		if err := r.AutoTopup.Validate(); err != nil {
			return err
		}
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

	// Validate alert config if provided
	if r.AlertConfig != nil {
		if r.AlertConfig.Threshold == nil {
			return ierr.NewError("alert_config.threshold is required").
				WithHint("Threshold must be provided when alert config is set").
				Mark(ierr.ErrValidation)
		}
		if r.AlertConfig.Threshold.Type == "" {
			return ierr.NewError("alert_config.threshold.type is required").
				WithHint("Threshold type must be provided when alert config is set").
				Mark(ierr.ErrValidation)
		}
		if r.AlertConfig.Threshold.Value.IsZero() {
			return ierr.NewError("alert_config.threshold.value must be greater than 0").
				WithHint("Threshold value must be provided when alert config is set").
				Mark(ierr.ErrValidation)
		}
	}

	return validator.ValidateRequest(r)
}

// WalletResponse represents a wallet in API responses
type WalletResponse struct {
	ID             string             `json:"id"`
	CustomerID     string             `json:"customer_id"`
	Name           string             `json:"name,omitempty"`
	Currency       string             `json:"currency"`
	Description    string             `json:"description,omitempty"`
	Balance        decimal.Decimal    `json:"balance" swaggertype:"string"`
	CreditBalance  decimal.Decimal    `json:"credit_balance" swaggertype:"string"`
	WalletStatus   types.WalletStatus `json:"wallet_status"`
	Metadata       types.Metadata     `json:"metadata,omitempty"`
	AutoTopup      *types.AutoTopup   `json:"auto_topup,omitempty"`
	WalletType     types.WalletType   `json:"wallet_type"`
	Config         types.WalletConfig `json:"config,omitempty"`
	ConversionRate decimal.Decimal    `json:"conversion_rate" swaggertype:"string"`
	AlertEnabled   bool               `json:"alert_enabled"`
	AlertConfig    *types.AlertConfig `json:"alert_config,omitempty"`
	AlertState     string             `json:"alert_state,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// ToWalletResponse converts domain Wallet to WalletResponse
func FromWallet(w *wallet.Wallet) *WalletResponse {
	if w == nil {
		return nil
	}
	alertConfig := w.AlertConfig
	if w.AlertConfig == nil || w.AlertConfig.Threshold == nil {
		alertConfig = nil
	}
	return &WalletResponse{
		ID:             w.ID,
		CustomerID:     w.CustomerID,
		Currency:       w.Currency,
		Balance:        w.Balance,
		CreditBalance:  w.CreditBalance,
		Name:           w.Name,
		Description:    w.Description,
		WalletStatus:   w.WalletStatus,
		Metadata:       w.Metadata,
		AutoTopup:      w.AutoTopup,
		WalletType:     w.WalletType,
		Config:         w.Config,
		ConversionRate: w.ConversionRate,
		AlertEnabled:   w.AlertEnabled,
		AlertConfig:    alertConfig,
		AlertState:     w.AlertState,
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
	}
}

func ToWalletBalanceResponse(w *wallet.Wallet) *WalletBalanceResponse {
	return &WalletBalanceResponse{
		Wallet: w,
	}
}

// WalletTransactionResponse represents a wallet transaction in API responses
type WalletTransactionResponse struct {
	*wallet.Transaction

	Customer      *CustomerResponse `json:"customer,omitempty"`
	CreatedByUser *UserResponse     `json:"created_by_user,omitempty"`
	Wallet        *WalletResponse   `json:"wallet,omitempty"`
}

// FromWalletTransaction converts a wallet transaction to a WalletTransactionResponse
func FromWalletTransaction(t *wallet.Transaction) *WalletTransactionResponse {
	return &WalletTransactionResponse{
		Transaction: t,
	}
}

func (*WalletTransactionResponse) WithWallet(w *wallet.Wallet) *WalletTransactionResponse {
	return &WalletTransactionResponse{
		Wallet: FromWallet(w),
	}
}

// ListWalletTransactionsResponse represents the response for listing wallet transactions
type ListWalletTransactionsResponse = types.ListResponse[*WalletTransactionResponse]

// TopUpWalletRequest represents a request to add credits to a wallet
type TopUpWalletRequest struct {
	// credits_to_add is the number of credits to add to the wallet
	CreditsToAdd decimal.Decimal `json:"credits_to_add" swaggertype:"string"`
	// amount is the amount in the currency of the wallet to be added
	// NOTE: this is not the number of credits to add, but the amount in the currency
	// amount = credits_to_add * conversion_rate
	// if both amount and credits_to_add are provided, amount will be ignored
	// ex if the wallet has a conversion_rate of 2 then adding an amount of
	// 10 USD in the wallet wil add 5 credits in the wallet
	Amount            decimal.Decimal         `json:"amount" swaggertype:"string"`
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
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
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

// TopUpWalletResponse represents the response for topping up a wallet
type TopUpWalletResponse struct {
	// Wallet transaction created (could be PENDING or COMPLETED)
	WalletTransaction *WalletTransactionResponse `json:"wallet_transaction"`
	// Invoice ID if an invoice was created (only for PURCHASED_CREDIT_INVOICED)
	InvoiceID *string `json:"invoice_id,omitempty"`
	// Wallet details after the operation
	Wallet *WalletResponse `json:"wallet"`
}

// WalletBalanceResponse represents the response for getting wallet balance
type WalletBalanceResponse struct {
	*wallet.Wallet
	RealTimeBalance       *decimal.Decimal `json:"real_time_balance,omitempty" swaggertype:"string"`
	RealTimeCreditBalance *decimal.Decimal `json:"real_time_credit_balance,omitempty" swaggertype:"string"`
	BalanceUpdatedAt      *time.Time       `json:"balance_updated_at,omitempty"`
	CurrentPeriodUsage    *decimal.Decimal `json:"current_period_usage,omitempty" swaggertype:"string"`
	UnpaidInvoicesAmount  *decimal.Decimal `json:"unpaid_invoices_amount,omitempty" swaggertype:"string"`
}

type ExpiredCreditsResponseItem struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	Count         int    `json:"count"`
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

// ManualBalanceDebitRequest represents a request to debit credits from a wallet
type ManualBalanceDebitRequest struct {
	// credits is the number of credits to debit from the wallet
	Credits decimal.Decimal `json:"credits" swaggertype:"string"`
	// transaction_reason is the reason for the transaction
	TransactionReason types.TransactionReason `json:"transaction_reason,omitempty" binding:"required"`
	// idempotency_key is a unique key for the transaction
	IdempotencyKey *string `json:"idempotency_key" binding:"required"`
	// description to add any specific details about the transaction
	Description string `json:"description,omitempty"`
	// metadata is a map of key-value pairs to store any additional information about the transaction
	Metadata types.Metadata `json:"metadata,omitempty"`
}

func (r *ManualBalanceDebitRequest) Validate() error {
	if r.Credits.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("credits must be greater than 0").
			WithHint("Credits to debit must be a positive value").
			WithReportableDetails(map[string]interface{}{
				"credits": r.Credits,
			}).
			Mark(ierr.ErrValidation)
	}

	allowedTransactionReasons := []types.TransactionReason{
		types.TransactionReasonManualBalanceDebit,
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

	return nil
}
