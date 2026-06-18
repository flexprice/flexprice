package paymentmethod

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// FromEnt converts an Ent PaymentMethod to the domain model
func FromEnt(e *ent.PaymentMethod) *PaymentMethod {
	if e == nil {
		return nil
	}

	return &PaymentMethod{
		ID:                  e.ID,
		CustomerID:          e.CustomerID,
		Type:                types.PaymentMethodType(e.Type),
		Gateway:             e.Gateway,
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

// FromEntList converts a list of Ent PaymentMethods to domain models
func FromEntList(list []*ent.PaymentMethod) []*PaymentMethod {
	if list == nil {
		return nil
	}
	result := make([]*PaymentMethod, len(list))
	for i, e := range list {
		result[i] = FromEnt(e)
	}
	return result
}
