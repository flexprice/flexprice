package service

import (
	"context"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentGatewayFactory manages different payment gateway implementations
type PaymentGatewayFactory struct {
	services ServiceParams
}

// NewPaymentGatewayFactory creates a new payment gateway factory
func NewPaymentGatewayFactory(services ServiceParams) *PaymentGatewayFactory {
	return &PaymentGatewayFactory{
		services: services,
	}
}

// GetGateway returns the appropriate payment gateway service for the given type
func (f *PaymentGatewayFactory) GetGateway(ctx context.Context, gatewayType types.PaymentGatewayType) (interface{}, error) {
	switch gatewayType {
	case types.PaymentGatewayTypeStripe:
		return NewStripeService(f.services), nil
	case types.PaymentGatewayTypePaddle:
		return NewPaddleService(f.services), nil
	case types.PaymentGatewayTypeRazorpay:
		// TODO: Implement Razorpay service
		return nil, ierr.NewError("gateway not implemented").
			WithHint(fmt.Sprintf("Gateway type '%s' is not yet implemented", gatewayType)).
			WithReportableDetails(map[string]interface{}{
				"gateway_type": gatewayType,
			}).
			Mark(ierr.ErrValidation)
	case types.PaymentGatewayTypeFinix:
		// TODO: Implement Finix service
		return nil, ierr.NewError("gateway not implemented").
			WithHint(fmt.Sprintf("Gateway type '%s' is not yet implemented", gatewayType)).
			WithReportableDetails(map[string]interface{}{
				"gateway_type": gatewayType,
			}).
			Mark(ierr.ErrValidation)
	default:
		return nil, ierr.NewError("unsupported gateway type").
			WithHint(fmt.Sprintf("Gateway type '%s' is not supported", gatewayType)).
			WithReportableDetails(map[string]interface{}{
				"gateway_type": gatewayType,
			}).
			Mark(ierr.ErrValidation)
	}
}

// GetPreferredGateway returns the preferred payment gateway for the environment
func (f *PaymentGatewayFactory) GetPreferredGateway(ctx context.Context) (types.PaymentGatewayType, error) {
	// For now, return Stripe as the preferred gateway
	// In the future, this could be based on environment configuration, success rates, etc.
	return types.PaymentGatewayTypeStripe, nil
}

// GetSupportedGateways returns all supported gateway types
func (f *PaymentGatewayFactory) GetSupportedGateways() []types.PaymentGatewayType {
	return []types.PaymentGatewayType{
		types.PaymentGatewayTypeStripe,
		types.PaymentGatewayTypePaddle,
		types.PaymentGatewayTypeRazorpay,
		types.PaymentGatewayTypeFinix,
	}
}
