package paymentmethod

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentMethod represents a saved payment method for a customer.
type PaymentMethod struct {
	ID                  string                    `json:"id"`
	CustomerID          string                    `json:"customer_id"`
	Type                types.PaymentMethodType   `json:"type"`
	Gateway             types.PaymentGatewayType  `json:"gateway"`
	GatewayMethodID     string                    `json:"gateway_method_id"`
	PaymentMethodStatus types.PaymentMethodStatus `json:"payment_method_status"`
	IsDefault           bool                      `json:"is_default"`
	// MethodDetails holds provider-specific card info: brand, last4, exp_month, exp_year, name.
	MethodDetails map[string]interface{} `json:"method_details,omitempty"`
	EnvironmentID string                 `json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent PaymentMethod to a domain PaymentMethod.
func FromEnt(e *ent.PaymentMethod) *PaymentMethod {
	if e == nil {
		return nil
	}
	return &PaymentMethod{
		ID:                  e.ID,
		CustomerID:          e.CustomerID,
		Type:                types.PaymentMethodType(e.Type),
		Gateway:             types.PaymentGatewayType(e.Gateway),
		GatewayMethodID:     e.GatewayMethodID,
		PaymentMethodStatus: types.PaymentMethodStatus(e.PaymentMethodStatus),
		IsDefault:           e.IsDefault,
		MethodDetails:       e.MethodDetails,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent PaymentMethods to domain PaymentMethods.
func FromEntList(list []*ent.PaymentMethod) []*PaymentMethod {
	result := make([]*PaymentMethod, len(list))
	for i, e := range list {
		result[i] = FromEnt(e)
	}
	return result
}
