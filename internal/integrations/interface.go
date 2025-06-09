package integrations

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// IntegrationGateway defines a minimal interface for integration operations
type IntegrationGateway interface {
	// GetProviderName returns the name of this provider
	GetProviderName() string

	// GetCapabilities returns the capabilities supported by this gateway
	GetCapabilities() []types.IntegrationCapability

	// SupportsCapability checks if this gateway supports a specific capability
	SupportsCapability(capability types.IntegrationCapability) bool

	// Customer operations
	CreateCustomer(ctx context.Context, customer *customer.Customer) (string, error)
	UpdateCustomer(ctx context.Context, customer *customer.Customer, providerID string) error
	GetCustomer(ctx context.Context, customerProviderID string) (*customer.Customer, error)
	DeleteCustomer(ctx context.Context, customerProviderID string) error

	// Payment method operations
	ListPaymentMethods(ctx context.Context, customerProviderID string) ([]PaymentMethodInfo, error)
	GetDefaultPaymentMethod(ctx context.Context, customerProviderID string) (string, error)

	// Payment operations
	CreatePayment(ctx context.Context, payment *payment.Payment, options map[string]interface{}) (string, error)
}

// GatewayFactory creates gateway instances
type GatewayFactory func(credentials map[string]string, logger *logger.Logger) (IntegrationGateway, error)
