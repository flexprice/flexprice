package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// WalletStatus represents the current state of a wallet
type WalletStatus string

const (
	WalletStatusActive WalletStatus = "active"
	WalletStatusFrozen WalletStatus = "frozen"
	WalletStatusClosed WalletStatus = "closed"
)

// WalletType represents the type of wallet
type WalletType string

const (
	WalletTypePromotional WalletType = "PROMOTIONAL"
	WalletTypePrePaid     WalletType = "PRE_PAID"
)

func (t WalletType) Validate() error {
	if t == "" {
		return nil
	}

	allowedValues := []string{
		string(WalletTypePromotional),
		string(WalletTypePrePaid),
	}
	if !lo.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid wallet type").
			WithHint("Invalid wallet type").
			WithReportableDetails(map[string]any{
				"allowed": allowedValues,
				"type":    t,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TransactionReason represents the reason for a wallet transaction
type TransactionReason string

const (
	TransactionReasonInvoicePayment          TransactionReason = "INVOICE_PAYMENT"
	TransactionReasonFreeCredit              TransactionReason = "FREE_CREDIT_GRANT"
	TransactionReasonSubscriptionCredit      TransactionReason = "SUBSCRIPTION_CREDIT_GRANT"
	TransactionReasonPurchasedCreditInvoiced TransactionReason = "PURCHASED_CREDIT_INVOICED"
	TransactionReasonPurchasedCreditDirect   TransactionReason = "PURCHASED_CREDIT_DIRECT"
	TransactionReasonCreditNote              TransactionReason = "CREDIT_NOTE"
	TransactionReasonCreditExpired           TransactionReason = "CREDIT_EXPIRED"
	TransactionReasonWalletTermination       TransactionReason = "WALLET_TERMINATION"
	TransactionReasonManualBalanceDebit      TransactionReason = "MANUAL_BALANCE_DEBIT"
)

func (t TransactionReason) Validate() error {
	if t == "" {
		return nil
	}

	allowedValues := []string{
		string(TransactionReasonInvoicePayment),
		string(TransactionReasonFreeCredit),
		string(TransactionReasonSubscriptionCredit),
		string(TransactionReasonPurchasedCreditInvoiced),
		string(TransactionReasonPurchasedCreditDirect),
		string(TransactionReasonCreditNote),
		string(TransactionReasonCreditExpired),
		string(TransactionReasonWalletTermination),
		string(TransactionReasonManualBalanceDebit),
	}
	if !lo.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid transaction reason").
			WithHint("Invalid transaction reason").
			WithReportableDetails(map[string]any{
				"allowed": allowedValues,
				"type":    t,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type WalletTxReferenceType string

const (
	// WalletTxReferenceTypePayment is used for flexprice system payment IDs
	WalletTxReferenceTypePayment WalletTxReferenceType = "PAYMENT"
	// WalletTxReferenceTypeExternal is used for external reference IDs in case
	// the user wants to map a wallet transaction to their own reference ID
	WalletTxReferenceTypeExternal WalletTxReferenceType = "EXTERNAL"
	// WalletTxReferenceTypeRequest is used for auto generated reference IDs
	WalletTxReferenceTypeRequest WalletTxReferenceType = "REQUEST"
)

func (t WalletTxReferenceType) Validate() error {
	if t == "" {
		return nil
	}
	allowedValues := []string{
		string(WalletTxReferenceTypePayment),
		string(WalletTxReferenceTypeExternal),
		string(WalletTxReferenceTypeRequest),
	}
	if !lo.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid wallet transaction reference type").
			WithHint("Invalid wallet transaction reference type").
			WithReportableDetails(map[string]any{
				"allowed": allowedValues,
				"type":    t,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// WalletTransactionFilter represents the filter options for wallet transactions
type WalletTransactionFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	WalletID           *string            `json:"id,omitempty" form:"id"`
	Type               *TransactionType   `json:"type,omitempty" form:"type"`
	TransactionStatus  *TransactionStatus `json:"transaction_status,omitempty" form:"transaction_status"`
	ReferenceType      *string            `json:"reference_type,omitempty" form:"reference_type"`
	ReferenceID        *string            `json:"reference_id,omitempty" form:"reference_id"`
	ExpiryDateBefore   *time.Time         `json:"expiry_date_before,omitempty" form:"expiry_date_before"`
	ExpiryDateAfter    *time.Time         `json:"expiry_date_after,omitempty" form:"expiry_date_after"`
	CreditsAvailableGT *decimal.Decimal   `json:"credits_available_gt,omitempty" form:"credits_available_gt"`
	TransactionReason  *TransactionReason `json:"transaction_reason,omitempty" form:"transaction_reason"`
	Priority           *int               `json:"priority,omitempty" form:"priority"`
	CreatedBy          *string            `json:"created_by,omitempty" form:"created_by"`
}

func NewWalletTransactionFilter() *WalletTransactionFilter {
	return &WalletTransactionFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewNoLimitWalletTransactionFilter() *WalletTransactionFilter {
	return &WalletTransactionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f WalletTransactionFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.Type != nil {
		if err := f.Type.Validate(); err != nil {
			return err
		}
	}

	if f.TransactionStatus != nil {
		if err := f.TransactionStatus.Validate(); err != nil {
			return err
		}
	}

	if f.ReferenceType != nil && f.ReferenceID == nil || f.ReferenceID != nil && *f.ReferenceID == "" {
		return ierr.NewError("reference_type and reference_id must be provided together").
			WithHint("Reference type and reference id must be provided together").
			Mark(ierr.ErrValidation)
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if f.ExpiryDateBefore != nil && f.ExpiryDateAfter != nil {
		if f.ExpiryDateBefore.Before(*f.ExpiryDateAfter) {
			return ierr.NewError("expiry_date_before must be after expiry_date_after").
				WithHint("Expiry date before must be after expiry date after").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *WalletTransactionFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *WalletTransactionFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *WalletTransactionFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *WalletTransactionFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *WalletTransactionFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *WalletTransactionFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *WalletTransactionFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

type WalletConfigPriceType string

const (
	WalletConfigPriceTypeAll   WalletConfigPriceType = "ALL"
	WalletConfigPriceTypeUsage WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_USAGE)
	WalletConfigPriceTypeFixed WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_FIXED)
)

// WalletConfig represents configuration constraints for a wallet
type WalletConfig struct {
	// AllowedPriceTypes is a list of price types that are allowed for the wallet
	// nil means all price types are allowed
	AllowedPriceTypes []WalletConfigPriceType `json:"allowed_price_types,omitempty"`
}

func GetDefaultWalletConfig() *WalletConfig {
	return &WalletConfig{
		AllowedPriceTypes: []WalletConfigPriceType{WalletConfigPriceTypeAll},
	}
}

func (c WalletConfig) Validate() error {
	allowedPriceTypes := []string{
		string(WalletConfigPriceTypeAll),
		string(WalletConfigPriceTypeUsage),
		string(WalletConfigPriceTypeFixed),
	}

	if c.AllowedPriceTypes != nil {
		for _, priceType := range c.AllowedPriceTypes {
			if !lo.Contains(allowedPriceTypes, string(priceType)) {
				return ierr.NewError("invalid price type").
					WithHint("Invalid price type").
					WithReportableDetails(map[string]any{
						"allowed": allowedPriceTypes,
						"type":    priceType,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}
	return nil
}

type CheckAlertsRequest struct {
	TenantIDs []string              `json:"tenant_ids"`
	EnvIDs    []string              `json:"env_ids"`
	WalletIDs []string              `json:"wallet_ids"`
	Threshold *WalletAlertThreshold `json:"threshold,omitempty"`
}

// WalletFilter represents the filter options for wallets
type WalletFilter struct {
	*QueryFilter
	WalletIDs    []string      `json:"wallet_ids,omitempty" form:"wallet_ids"`
	Status       *WalletStatus `json:"status,omitempty" form:"status"`
	AlertEnabled *bool         `json:"alert_enabled,omitempty" form:"alert_enabled"`
}

func NewWalletFilter() *WalletFilter {
	return &WalletFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func (f *WalletFilter) Validate() error {
	if f.QueryFilter == nil {
		f.QueryFilter = NewDefaultQueryFilter()
	}
	return f.QueryFilter.Validate()
}

// AutoTopup represents the auto top-up configuration for a wallet
type AutoTopup struct {
	Enabled   *bool            `json:"enabled"`
	Threshold *decimal.Decimal `json:"threshold"`
	Amount    *decimal.Decimal `json:"amount"`
	Invoicing *bool            `json:"invoicing"`
}

func (a *AutoTopup) Validate() error {
	if a.Threshold == nil {
		return ierr.NewError("threshold is required").
			WithHint("Threshold is required").
			Mark(ierr.ErrValidation)
	}
	if a.Amount == nil {
		return ierr.NewError("amount is required").
			WithHint("Amount is required").
			Mark(ierr.ErrValidation)
	}
	if a.Invoicing == nil {
		return ierr.NewError("invoicing boolean is required").
			WithHint("Invoicing boolean is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}
