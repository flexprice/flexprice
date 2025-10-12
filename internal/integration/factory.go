package integration

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// Factory manages different payment integration providers
type Factory struct {
	config                       *config.Configuration
	logger                       *logger.Logger
	connectionRepo               connection.Repository
	customerRepo                 customer.Repository
	invoiceRepo                  invoice.Repository
	paymentRepo                  payment.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	encryptionService            security.EncryptionService
}

// NewFactory creates a new integration factory
func NewFactory(
	config *config.Configuration,
	logger *logger.Logger,
	connectionRepo connection.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	paymentRepo payment.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	encryptionService security.EncryptionService,
) *Factory {
	return &Factory{
		config:                       config,
		logger:                       logger,
		connectionRepo:               connectionRepo,
		customerRepo:                 customerRepo,
		invoiceRepo:                  invoiceRepo,
		paymentRepo:                  paymentRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		encryptionService:            encryptionService,
	}
}

// GetStripeIntegration returns a complete Stripe integration setup
func (f *Factory) GetStripeIntegration(ctx context.Context) (*StripeIntegration, error) {
	// Create Stripe client
	stripeClient := stripe.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := stripe.NewCustomerService(
		stripeClient,
		f.customerRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create invoice sync service first
	invoiceSyncSvc := stripe.NewInvoiceSyncService(
		stripeClient,
		customerSvc,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create payment service
	paymentSvc := stripe.NewPaymentService(
		stripeClient,
		customerSvc,
		invoiceSyncSvc,
		f.invoiceRepo,
		f.paymentRepo,
		f.logger,
	)

	planSvc := stripe.NewStripePlanService(
		stripeClient,
		f.logger,
	)

	subSvc := stripe.NewStripeSubscriptionService(
		stripeClient,
		f.logger,
		planSvc,
	)

	// Create webhook handler
	webhookHandler := webhook.NewHandler(
		stripeClient,
		customerSvc,
		paymentSvc,
		invoiceSyncSvc,
		planSvc,
		subSvc,
		f.entityIntegrationMappingRepo,
		f.connectionRepo,
		f.logger,
	)

	return &StripeIntegration{
		Client:         stripeClient,
		CustomerSvc:    customerSvc,
		PaymentSvc:     paymentSvc,
		InvoiceSyncSvc: invoiceSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetIntegrationByProvider returns the appropriate integration for the given provider type
func (f *Factory) GetIntegrationByProvider(ctx context.Context, providerType types.SecretProvider) (interface{}, error) {
	switch providerType {
	case types.SecretProviderStripe:
		return f.GetStripeIntegration(ctx)
	default:
		return nil, ierr.NewError("unsupported integration provider").
			WithHint("Provider type is not supported").
			WithReportableDetails(map[string]interface{}{
				"provider_type": providerType,
			}).
			Mark(ierr.ErrValidation)
	}
}

// GetSupportedProviders returns all supported integration provider types
func (f *Factory) GetSupportedProviders() []types.SecretProvider {
	return []types.SecretProvider{
		types.SecretProviderStripe,
	}
}

// HasProvider checks if a provider is supported
func (f *Factory) HasProvider(providerType types.SecretProvider) bool {
	supportedProviders := f.GetSupportedProviders()
	for _, provider := range supportedProviders {
		if provider == providerType {
			return true
		}
	}
	return false
}

// StripeIntegration contains all Stripe integration services
type StripeIntegration struct {
	Client         *stripe.Client
	CustomerSvc    *stripe.CustomerService
	PaymentSvc     *stripe.PaymentService
	InvoiceSyncSvc *stripe.InvoiceSyncService
	WebhookHandler *webhook.Handler
}

// IntegrationProvider defines the interface for all integration providers
type IntegrationProvider interface {
	GetProviderType() types.SecretProvider
	IsAvailable(ctx context.Context) bool
}

// StripeProvider implements IntegrationProvider for Stripe
type StripeProvider struct {
	integration *StripeIntegration
}

// GetProviderType returns the provider type
func (p *StripeProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderStripe
}

// IsAvailable checks if Stripe integration is available
func (p *StripeProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasStripeConnection(ctx)
}

// GetAvailableProviders returns all available providers for the current environment
func (f *Factory) GetAvailableProviders(ctx context.Context) ([]IntegrationProvider, error) {
	var providers []IntegrationProvider

	// Check Stripe
	stripeIntegration, err := f.GetStripeIntegration(ctx)
	if err == nil {
		stripeProvider := &StripeProvider{integration: stripeIntegration}
		if stripeProvider.IsAvailable(ctx) {
			providers = append(providers, stripeProvider)
		}
	}

	// Future providers can be added here
	// razorpayIntegration, err := f.GetRazorpayIntegration(ctx)
	// if err == nil {
	//     razorpayProvider := &RazorpayProvider{integration: razorpayIntegration}
	//     if razorpayProvider.IsAvailable(ctx) {
	//         providers = append(providers, razorpayProvider)
	//     }
	// }

	return providers, nil
}
