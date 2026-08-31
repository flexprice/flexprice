package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PortalAnalyticsRequest represents a request for usage analytics from the customer portal
// The ExternalCustomerID is implicitly derived from the authentication context
type PortalAnalyticsRequest struct {
	FeatureIDs      []string            `json:"feature_ids,omitempty" example:"feat_123,feat_456"`
	Sources         []string            `json:"sources,omitempty" example:"api,web"`
	StartTime       time.Time           `json:"start_time,omitempty" example:"2024-01-01T00:00:00Z"`
	EndTime         time.Time           `json:"end_time,omitempty" example:"2024-01-31T23:59:59Z"`
	GroupBy         []string            `json:"group_by,omitempty" example:"source,feature_id"`
	WindowSize      types.WindowSize    `json:"window_size,omitempty" example:"DAY"`
	Expand          []string            `json:"expand,omitempty" example:"price,meter,feature"`
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
}

// ToInternalRequest converts the portal analytics request to an internal GetUsageAnalyticsRequest
// with the customer ID injected from the authentication context
func (r *PortalAnalyticsRequest) ToInternalRequest(externalCustomerID string) *GetUsageAnalyticsRequest {
	return &GetUsageAnalyticsRequest{
		ExternalCustomerID: externalCustomerID,
		FeatureIDs:         r.FeatureIDs,
		Sources:            r.Sources,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		GroupBy:            r.GroupBy,
		WindowSize:         r.WindowSize,
		Expand:             r.Expand,
		PropertyFilters:    r.PropertyFilters,
	}
}

// PortalCostAnalyticsRequest represents a request for cost analytics from the customer portal
// The ExternalCustomerID is implicitly derived from the authentication context
type PortalCostAnalyticsRequest struct {
	FeatureIDs []string  `json:"feature_ids,omitempty" example:"feat_123,feat_456"`
	StartTime  time.Time `json:"start_time" binding:"required" example:"2024-01-01T00:00:00Z"`
	EndTime    time.Time `json:"end_time" binding:"required" example:"2024-01-31T23:59:59Z"`
	// Expand specifies which related entities to include. Supported values: "meter", "price"
	Expand []string `json:"expand,omitempty" example:"meter,price"`
}

// ToInternalRequest converts the portal cost analytics request to an internal GetCostAnalyticsRequest
// with the customer ID injected from the authentication context
func (r *PortalCostAnalyticsRequest) ToInternalRequest(externalCustomerID string) *GetCostAnalyticsRequest {
	return &GetCostAnalyticsRequest{
		ExternalCustomerID: externalCustomerID,
		FeatureIDs:         r.FeatureIDs,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		Expand:             r.Expand,
	}
}

// PortalPaginatedRequest represents a paginated request from the customer portal
type PortalPaginatedRequest struct {
	Page  int `form:"page" json:"page" example:"1"`
	Limit int `form:"limit" json:"limit" example:"20"`
}

// PortalTopUpWalletRequest is the customer-portal top-up payload.
//
// It deliberately omits transaction_reason: the portal service pins it to
// PURCHASED_CREDIT_INVOICED so a portal customer cannot grant themselves free
// or subscription credits by choosing another reason.
type PortalTopUpWalletRequest struct {
	// credits_to_add is the number of credits to add to the wallet
	CreditsToAdd decimal.Decimal `json:"credits_to_add" swaggertype:"string"`
	// amount is the amount in the wallet currency to add. Ignored when credits_to_add is set.
	Amount decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	// idempotency_key is required. WalletService.TopUpWallet falls back to a key
	// derived from an RFC3339 timestamp when none is supplied, so two identical
	// requests a second apart produce different keys and the dedup on the wallet
	// transaction and its invoice both miss — a double-submit from the portal
	// would grant the credits twice and raise two payment obligations.
	IdempotencyKey *string `json:"idempotency_key" binding:"required"`
	// description to add any specific details about the transaction
	Description string `json:"description,omitempty"`
	// checkout opts into pay-first hosted checkout: credits land only after the
	// payment succeeds. Omit for the pay-later / invoiced behavior.
	Checkout *CheckoutParams `json:"checkout,omitempty"`
}

func (r *PortalTopUpWalletRequest) Validate() error {
	if r.IdempotencyKey == nil || *r.IdempotencyKey == "" {
		return ierr.NewError("idempotency_key is required").
			WithHint("Send a unique idempotency_key per top-up attempt, and reuse it when retrying").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToTopUpWalletRequest maps the portal payload onto the shared wallet top-up
// request, pinning the transaction reason to a purchased-credit top-up.
func (r *PortalTopUpWalletRequest) ToTopUpWalletRequest() *TopUpWalletRequest {
	return &TopUpWalletRequest{
		CreditsToAdd:      r.CreditsToAdd,
		Amount:            r.Amount,
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    r.IdempotencyKey,
		Description:       r.Description,
		Checkout:          r.Checkout,
	}
}

// PortalUpdateAutoTopupRequest configures auto top-up on a portal customer's wallet.
type PortalUpdateAutoTopupRequest struct {
	AutoTopup *types.AutoTopup `json:"auto_topup" binding:"required"`
}

func (r *PortalUpdateAutoTopupRequest) Validate() error {
	if r.AutoTopup == nil {
		return ierr.NewError("auto_topup is required").
			WithHint("Provide the auto top-up configuration").
			Mark(ierr.ErrValidation)
	}
	return r.AutoTopup.Validate()
}

// ToUpdateWalletRequest narrows the portal payload to an auto-topup-only wallet
// update, so the portal cannot rename a wallet or rewrite its config/metadata.
func (r *PortalUpdateAutoTopupRequest) ToUpdateWalletRequest() *UpdateWalletRequest {
	return &UpdateWalletRequest{AutoTopup: r.AutoTopup}
}

// PortalListPaymentMethodsRequest lists saved payment methods for the portal customer.
// The customer is taken from the session token, never from the request.
type PortalListPaymentMethodsRequest struct {
	Provider      string `form:"provider" json:"provider"`
	Limit         int    `form:"limit" json:"limit,omitempty"`
	StartingAfter string `form:"starting_after" json:"starting_after,omitempty"`
	EndingBefore  string `form:"ending_before" json:"ending_before,omitempty"`
}

// ToListPaymentMethodsRequest maps to the shared request, defaulting the
// provider to Stripe, which is the only provider the list path supports today.
func (r *PortalListPaymentMethodsRequest) ToListPaymentMethodsRequest() *ListPaymentMethodsRequest {
	provider := r.Provider
	if provider == "" {
		provider = string(types.SecretProviderStripe)
	}
	return &ListPaymentMethodsRequest{
		Provider:      provider,
		Limit:         r.Limit,
		StartingAfter: r.StartingAfter,
		EndingBefore:  r.EndingBefore,
	}
}

// PortalAddPaymentMethodRequest starts a hosted card-capture session for the portal customer.
type PortalAddPaymentMethodRequest struct {
	Provider   types.SecretProvider `json:"provider,omitempty"`
	SuccessURL string               `json:"success_url,omitempty"`
	CancelURL  string               `json:"cancel_url,omitempty"`
	SetDefault bool                 `json:"set_default,omitempty"`
}

// ToCreateSetupIntentRequest maps to the shared setup-intent request. Usage is
// pinned to off_session so the saved card can back auto top-up later.
func (r *PortalAddPaymentMethodRequest) ToCreateSetupIntentRequest() *CreateSetupIntentRequest {
	provider := r.Provider
	if provider == "" {
		provider = types.SecretProviderStripe
	}
	return &CreateSetupIntentRequest{
		Provider:   provider,
		Usage:      "off_session",
		SuccessURL: r.SuccessURL,
		CancelURL:  r.CancelURL,
		SetDefault: r.SetDefault,
	}
}

// PortalSetDefaultPaymentMethodRequest marks one of the portal customer's saved
// methods as the default for future charges.
type PortalSetDefaultPaymentMethodRequest struct {
	PaymentMethodID string `json:"payment_method_id" binding:"required"`
}

func (r *PortalSetDefaultPaymentMethodRequest) Validate() error {
	if r.PaymentMethodID == "" {
		return ierr.NewError("payment_method_id is required").
			WithHint("Provide the payment method to set as default").
			Mark(ierr.ErrValidation)
	}
	return nil
}
