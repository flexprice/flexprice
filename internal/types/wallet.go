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
	TransactionReasonInvoiceRefund           TransactionReason = "INVOICE_REFUND"
	TransactionReasonCreditExpired           TransactionReason = "CREDIT_EXPIRED"
	TransactionReasonWalletTermination       TransactionReason = "WALLET_TERMINATION"
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
		string(TransactionReasonInvoiceRefund),
		string(TransactionReasonCreditExpired),
		string(TransactionReasonWalletTermination),
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

// AutoTopupTrigger represents the type of trigger for auto top-up
type AutoTopupTrigger string

const (
	// AutoTopupTriggerDisabled represents disabled auto top-up
	AutoTopupTriggerDisabled AutoTopupTrigger = "disabled"
	// AutoTopupTriggerBalanceBelowThreshold represents auto top-up when balance goes below threshold
	AutoTopupTriggerBalanceBelowThreshold AutoTopupTrigger = "balance_below_threshold"
)

func (t AutoTopupTrigger) Validate() error {
	allowedValues := []string{
		string(AutoTopupTriggerDisabled),
		string(AutoTopupTriggerBalanceBelowThreshold),
	}
	if t == "" {
		return nil
	}

	if !lo.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid auto top-up trigger").
			WithHint("Invalid auto top-up trigger").
			WithReportableDetails(map[string]any{
				"allowed": allowedValues,
				"type":    t,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// String returns the string representation of AutoTopupTrigger
func (t AutoTopupTrigger) String() string {
	return string(t)
}

// WalletTransactionFilter represents the filter options for wallet transactions
type WalletTransactionFilter struct {
	*QueryFilter
	*TimeRangeFilter
	WalletID           *string            `json:"id,omitempty" form:"id"`
	Type               *TransactionType   `json:"type,omitempty" form:"type"`
	TransactionStatus  *TransactionStatus `json:"transaction_status,omitempty" form:"transaction_status"`
	ReferenceType      *string            `json:"reference_type,omitempty" form:"reference_type"`
	ReferenceID        *string            `json:"reference_id,omitempty" form:"reference_id"`
	ExpiryDateBefore   *time.Time         `json:"expiry_date_before,omitempty" form:"expiry_date_before"`
	ExpiryDateAfter    *time.Time         `json:"expiry_date_after,omitempty" form:"expiry_date_after"`
	CreditsAvailableGT *decimal.Decimal   `json:"credits_available_gt,omitempty" form:"credits_available_gt"`
	TransactionReason  *TransactionReason `json:"transaction_reason,omitempty" form:"transaction_reason"`
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

	// TODO: Add validation for transaction reason if needed

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
