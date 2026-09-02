package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Checkout params for customer portal specific apis with limited capabilities.
// Omits PaymentProviderConfig on purpose: collection_method and max_mandate_limit
// are not a customer's to choose. PaymentProvider is optional here but required
// on the admin PaymentParams — the service resolves it when only one is configured.
// There is no save_payment_method flag: portal checkouts always vault, so the
// service must pass that to the provider.
type PortalCheckoutParams struct {
	RedirectionParams
	PaymentProvider *types.PaymentGatewayType `json:"payment_provider,omitempty"`
	// Authorisation for this one payment, given while the customer is present.
	// Deliberately not persisted. Falls back to a link when no usable saved
	// method exists or the charge declines — read payment_action, not the URL.
	UseSavedMethod bool              `json:"use_saved_method,omitempty"`
	IdempotencyKey *string           `json:"idempotency_key,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// transaction_reason, expiry and priority are pinned server-side so a portal
// customer cannot grant themselves free credits or reorder consumption.
type PortalTopUpWalletRequest struct {
	CreditsToAdd   decimal.Decimal       `json:"credits_to_add" swaggertype:"string"`
	Amount         decimal.Decimal       `json:"amount,omitempty" swaggertype:"string"`
	IdempotencyKey *string               `json:"idempotency_key,omitempty"`
	Description    string                `json:"description,omitempty"`
	Checkout       *PortalCheckoutParams `json:"checkout,omitempty"`
}

// Exists because CheckoutSessionResponse embeds the domain session, including
// provider_result (gateway ids, intent ids) and payment_provider_config, none of
// which may reach a browser. payment_action is the only channel for provider data.
type PortalCheckoutSessionResponse struct {
	ID                string                   `json:"id"`
	CheckoutStatus    types.CheckoutStatus     `json:"checkout_status"`
	PaymentProvider   types.PaymentGatewayType `json:"payment_provider"`
	PaymentAction     *types.PaymentAction     `json:"payment_action,omitempty"`
	CheckoutInvoiceID *string                  `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string                  `json:"checkout_payment_id,omitempty"`
	ExpiresAt         time.Time                `json:"expires_at"`
	CompletedAt       *time.Time               `json:"completed_at,omitempty"`
	CancelledAt       *time.Time               `json:"cancelled_at,omitempty"`
	FailureReason     *string                  `json:"failure_reason,omitempty"`
}

type PortalTopUpWalletResponse struct {
	WalletTransaction *WalletTransactionResponse     `json:"wallet_transaction"`
	InvoiceID         *string                        `json:"invoice_id,omitempty"`
	Wallet            *WalletResponse                `json:"wallet"`
	CheckoutSession   *PortalCheckoutSessionResponse `json:"checkout_session,omitempty"`
}

// Flow-neutral: whether the provider uses a hosted page, an in-page SDK or a
// direct vault is its property, not a client choice, so there is no per-flow
// endpoint. Read AddPaymentMethodResponse.Action for what to do next.
type PortalAddPaymentMethodRequest struct {
	PaymentProvider types.PaymentGatewayType `json:"payment_provider" binding:"required"`
	RedirectionParams
}

type PortalDeletePaymentMethodRequest struct {
	PaymentProvider types.PaymentGatewayType `json:"payment_provider" binding:"required"`
	PaymentMethodID string                   `json:"payment_method_id" binding:"required"`
}

type PortalSetDefaultPaymentMethodRequest struct {
	PaymentProvider types.PaymentGatewayType `json:"payment_provider" binding:"required"`
	PaymentMethodID string                   `json:"payment_method_id" binding:"required"`
}

// Narrower than the admin UpdateWalletRequest, which also carries Config,
// AlertSettings and Metadata. Invoicing is withheld — it selects the transaction
// reason and is the tenant's call. No auto-charge flag: enabling auto top-up is
// itself the consent to be charged unattended, so it always tries the saved method.
type PortalUpdateAutoTopupRequest struct {
	Enabled   bool             `json:"enabled"`
	Threshold *decimal.Decimal `json:"threshold,omitempty" swaggertype:"string"`
	Amount    *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	Cooldown  *types.Duration  `json:"cooldown,omitempty"`
}

// Amount is absent on purpose — it comes from the invoice, so a customer cannot
// part-pay. SaveCardAndMakeDefault and GatewayOptions from CreatePaymentRequest
// are withheld for the same reason: paying must not silently vault a default card.
type PortalPayInvoiceRequest struct {
	RedirectionParams
	PaymentProvider *types.PaymentGatewayType `json:"payment_provider,omitempty"`
	IdempotencyKey  *string                   `json:"idempotency_key,omitempty"`
}

type PortalPayInvoiceResponse struct {
	PaymentID     string               `json:"payment_id"`
	InvoiceID     string               `json:"invoice_id"`
	Status        types.PaymentStatus  `json:"status"`
	Amount        decimal.Decimal      `json:"amount" swaggertype:"string"`
	Currency      string               `json:"currency"`
	PaymentAction *types.PaymentAction `json:"payment_action,omitempty"`
}
