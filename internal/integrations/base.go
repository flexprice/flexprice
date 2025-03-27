package integrations

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// BaseIntegrationGateway provides a base implementation of IntegrationGateway
// This is not used anywhere but is provided as a convenience for implementing integrations
// So for any new integrations, we can replicate this and implement the supported methods
type BaseIntegrationGateway struct {
	ProviderName string
	Capabilities []types.IntegrationCapability
	Logger       *logger.Logger
}

// GetProviderName returns the provider name
func (g *BaseIntegrationGateway) GetProviderName() string {
	return g.ProviderName
}

// GetCapabilities returns the supported capabilities
func (g *BaseIntegrationGateway) GetCapabilities() []types.IntegrationCapability {
	return g.Capabilities
}

// SupportsCapability checks if a capability is supported
func (g *BaseIntegrationGateway) SupportsCapability(capability types.IntegrationCapability) bool {
	for _, c := range g.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// CreateCustomer creates a customer in the external system
func (g *BaseIntegrationGateway) CreateCustomer(ctx context.Context, cust *customer.Customer) (string, error) {
	return "", newUnsupportedError("CreateCustomer", g.ProviderName)
}

// UpdateCustomer updates a customer in the external system
func (g *BaseIntegrationGateway) UpdateCustomer(ctx context.Context, cust *customer.Customer, providerID string) error {
	return newUnsupportedError("UpdateCustomer", g.ProviderName)
}

// ListPaymentMethods lists payment methods for a customer in the external system
func (g *BaseIntegrationGateway) ListPaymentMethods(ctx context.Context, customerProviderID string) ([]PaymentMethodInfo, error) {
	return nil, newUnsupportedError("ListPaymentMethods", g.ProviderName)
}

// GetDefaultPaymentMethod gets the default payment method for a customer in the external system
func (g *BaseIntegrationGateway) GetDefaultPaymentMethod(ctx context.Context, customerProviderID string) (string, error) {
	return "", newUnsupportedError("GetDefaultPaymentMethod", g.ProviderName)
}

// CreatePayment creates a payment in the external system
func (g *BaseIntegrationGateway) CreatePayment(ctx context.Context, payment *payment.Payment, options map[string]interface{}) (string, error) {
	return "", newUnsupportedError("CreatePayment", g.ProviderName)
}

// Helper function to create unsupported operation errors
func newUnsupportedError(operation, provider string) error {
	return ierr.NewError(fmt.Sprintf("operation %s is not supported by %s", operation, provider)).
		WithHint(fmt.Sprintf("The %s provider does not support the %s operation", provider, operation)).
		Mark(ierr.ErrInvalidOperation)
}
