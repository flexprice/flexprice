package paymentmethod

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentMethod represents a saved payment method for a customer at a specific gateway
type PaymentMethod struct {
	ID string `json:"id"`
	// CustomerID is the Flexprice customer this payment method belongs to
	CustomerID string `json:"customer_id"`
	// Type is the payment instrument type (CARD, ACH, etc.)
	Type types.PaymentMethodType `json:"type"`
	// Gateway is the payment provider that holds the token (moyasar, stripe, etc.)
	Gateway string `json:"gateway"`
	// GatewayMethodID is the token or method identifier at the gateway (e.g. token_xxx in Moyasar)
	GatewayMethodID string `json:"gateway_method_id"`
	// PaymentMethodStatus indicates whether this method is usable
	PaymentMethodStatus types.PaymentMethodStatus `json:"payment_method_status"`
	// IsDefault indicates whether this is the customer's default payment method for the gateway
	IsDefault bool `json:"is_default"`
	// MethodDetails holds gateway-specific display metadata (brand, last4, expiry, etc.)
	MethodDetails map[string]interface{} `json:"method_details,omitempty"`

	EnvironmentID string `json:"environment_id"`
	types.BaseModel
}

func (pm *PaymentMethod) Validate() error {
	if pm.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			Mark(ierr.ErrValidation)
	}
	if pm.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			Mark(ierr.ErrValidation)
	}
	if pm.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			Mark(ierr.ErrValidation)
	}
	if pm.Gateway == "" {
		return ierr.NewError("gateway is required").
			Mark(ierr.ErrValidation)
	}
	if pm.GatewayMethodID == "" {
		return ierr.NewError("gateway_method_id is required").
			Mark(ierr.ErrValidation)
	}
	if err := pm.Type.Validate(); err != nil {
		return err
	}
	if err := pm.PaymentMethodStatus.Validate(); err != nil {
		return err
	}
	return nil
}

func (pm *PaymentMethod) TableName() string {
	return "payment_methods"
}
