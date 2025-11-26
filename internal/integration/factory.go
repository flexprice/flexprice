package integration

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/chargebee"
	chargebeewebhook "github.com/flexprice/flexprice/internal/integration/chargebee/webhook"
	"github.com/flexprice/flexprice/internal/integration/hubspot"
	hubspotwebhook "github.com/flexprice/flexprice/internal/integration/hubspot/webhook"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	razorpaywebhook "github.com/flexprice/flexprice/internal/integration/razorpay/webhook"
	"github.com/flexprice/flexprice/internal/integration/s3"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// Factory manages different payment integration providers and storage providers
type Factory struct {
	config                       *config.Configuration
	logger                       *logger.Logger
	connectionRepo               connection.Repository
	customerRepo                 customer.Repository
	subscriptionRepo             subscription.Repository
	invoiceRepo                  invoice.Repository
	paymentRepo                  payment.Repository
	priceRepo                    price.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	meterRepo                    meter.Repository
	featureRepo                  feature.Repository
	encryptionService            security.EncryptionService

	// Storage clients (cached for reuse)
	s3Client *s3.Client
}

// NewFactory creates a new integration factory
func NewFactory(
	config *config.Configuration,
	logger *logger.Logger,
	connectionRepo connection.Repository,
	customerRepo customer.Repository,
	subscriptionRepo subscription.Repository,
	invoiceRepo invoice.Repository,
	paymentRepo payment.Repository,
	priceRepo price.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	meterRepo meter.Repository,
	featureRepo feature.Repository,
	encryptionService security.EncryptionService,
) *Factory {
	return &Factory{
		config:                       config,
		logger:                       logger,
		connectionRepo:               connectionRepo,
		customerRepo:                 customerRepo,
		subscriptionRepo:             subscriptionRepo,
		invoiceRepo:                  invoiceRepo,
		paymentRepo:                  paymentRepo,
		priceRepo:                    priceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		meterRepo:                    meterRepo,
		featureRepo:                  featureRepo,
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

// GetHubSpotIntegration returns a complete HubSpot integration setup
func (f *Factory) GetHubSpotIntegration(ctx context.Context) (*HubSpotIntegration, error) {
	// Create HubSpot client
	hubspotClient := hubspot.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := hubspot.NewCustomerService(
		hubspotClient,
		f.customerRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create invoice sync service
	invoiceSyncSvc := hubspot.NewInvoiceSyncService(
		hubspotClient,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create deal sync service
	dealSyncSvc := hubspot.NewDealSyncService(
		hubspotClient,
		f.customerRepo,
		f.subscriptionRepo,
		f.priceRepo,
		f.logger,
	)

	// Create webhook handler
	webhookHandler := hubspotwebhook.NewHandler(
		hubspotClient,
		customerSvc,
		f.connectionRepo,
		f.logger,
	)

	return &HubSpotIntegration{
		Client:         hubspotClient,
		CustomerSvc:    customerSvc,
		InvoiceSyncSvc: invoiceSyncSvc,
		DealSyncSvc:    dealSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetRazorpayIntegration returns a complete Razorpay integration setup
func (f *Factory) GetRazorpayIntegration(ctx context.Context) (*RazorpayIntegration, error) {
	// Create Razorpay client
	razorpayClient := razorpay.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := razorpay.NewCustomerService(
		razorpayClient,
		f.customerRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create invoice sync service
	invoiceSyncSvc := razorpay.NewInvoiceSyncService(
		razorpayClient,
		customerSvc.(*razorpay.CustomerService),
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create payment service
	paymentSvc := razorpay.NewPaymentService(
		razorpayClient,
		customerSvc,
		invoiceSyncSvc,
		f.logger,
	)

	// Create webhook handler
	webhookHandler := razorpaywebhook.NewHandler(
		razorpayClient,
		paymentSvc,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	return &RazorpayIntegration{
		Client:         razorpayClient,
		CustomerSvc:    customerSvc,
		PaymentSvc:     paymentSvc,
		InvoiceSyncSvc: invoiceSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetChargebeeIntegration returns a complete Chargebee integration setup
func (f *Factory) GetChargebeeIntegration(ctx context.Context) (*ChargebeeIntegration, error) {
	// Create Chargebee client
	chargebeeClient := chargebee.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create item family service
	itemFamilySvc := chargebee.NewItemFamilyService(chargebee.ItemFamilyServiceParams{
		Client: chargebeeClient,
		Logger: f.logger,
	})

	// Create item service
	itemSvc := chargebee.NewItemService(chargebee.ItemServiceParams{
		Client: chargebeeClient,
		Logger: f.logger,
	})

	// Create item price service
	itemPriceSvc := chargebee.NewItemPriceService(chargebee.ItemPriceServiceParams{
		Client: chargebeeClient,
		Logger: f.logger,
	})

	// Create customer service
	customerSvc := chargebee.NewCustomerService(chargebee.CustomerServiceParams{
		Client:                       chargebeeClient,
		CustomerRepo:                 f.customerRepo,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		Logger:                       f.logger,
	})

	// Create invoice service
	invoiceSvc := chargebee.NewInvoiceService(chargebee.InvoiceServiceParams{
		Client:                       chargebeeClient,
		CustomerSvc:                  customerSvc,
		InvoiceRepo:                  f.invoiceRepo,
		PaymentRepo:                  f.paymentRepo,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		Logger:                       f.logger,
	})

	// Create plan sync service
	planSyncSvc := chargebee.NewPlanSyncService(chargebee.PlanSyncServiceParams{
		Client:                       chargebeeClient,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		MeterRepo:                    f.meterRepo,
		FeatureRepo:                  f.featureRepo,
		Logger:                       f.logger,
	})

	// Create webhook handler
	webhookHandler := chargebeewebhook.NewHandler(
		chargebeeClient,
		invoiceSvc.(*chargebee.InvoiceService),
		f.logger,
	)

	return &ChargebeeIntegration{
		Client:         chargebeeClient,
		ItemFamilySvc:  itemFamilySvc,
		ItemSvc:        itemSvc,
		ItemPriceSvc:   itemPriceSvc,
		CustomerSvc:    customerSvc,
		InvoiceSvc:     invoiceSvc,
		PlanSyncSvc:    planSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetIntegrationByProvider returns the appropriate integration for the given provider type
func (f *Factory) GetIntegrationByProvider(ctx context.Context, providerType types.SecretProvider) (interface{}, error) {
	switch providerType {
	case types.SecretProviderStripe:
		return f.GetStripeIntegration(ctx)
	case types.SecretProviderHubSpot:
		return f.GetHubSpotIntegration(ctx)
	case types.SecretProviderRazorpay:
		return f.GetRazorpayIntegration(ctx)
	case types.SecretProviderChargebee:
		return f.GetChargebeeIntegration(ctx)
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
		types.SecretProviderHubSpot,
		types.SecretProviderRazorpay,
		types.SecretProviderChargebee,
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

// HubSpotIntegration contains all HubSpot integration services
type HubSpotIntegration struct {
	Client         hubspot.HubSpotClient
	CustomerSvc    hubspot.HubSpotCustomerService
	InvoiceSyncSvc *hubspot.InvoiceSyncService
	DealSyncSvc    *hubspot.DealSyncService
	WebhookHandler *hubspotwebhook.Handler
}

// RazorpayIntegration contains all Razorpay integration services
type RazorpayIntegration struct {
	Client         razorpay.RazorpayClient
	CustomerSvc    razorpay.RazorpayCustomerService
	PaymentSvc     *razorpay.PaymentService
	InvoiceSyncSvc *razorpay.InvoiceSyncService
	WebhookHandler *razorpaywebhook.Handler
}

// ChargebeeIntegration contains all Chargebee integration services
type ChargebeeIntegration struct {
	Client         chargebee.ChargebeeClient
	ItemFamilySvc  chargebee.ChargebeeItemFamilyService
	ItemSvc        chargebee.ChargebeeItemService
	ItemPriceSvc   chargebee.ChargebeeItemPriceService
	CustomerSvc    chargebee.ChargebeeCustomerService
	InvoiceSvc     chargebee.ChargebeeInvoiceService
	PlanSyncSvc    chargebee.ChargebeePlanSyncService
	WebhookHandler *chargebeewebhook.Handler
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

// HubSpotProvider implements IntegrationProvider for HubSpot
type HubSpotProvider struct {
	integration *HubSpotIntegration
}

// GetProviderType returns the provider type
func (p *HubSpotProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderHubSpot
}

// IsAvailable checks if HubSpot integration is available
func (p *HubSpotProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasHubSpotConnection(ctx)
}

// RazorpayProvider implements IntegrationProvider for Razorpay
type RazorpayProvider struct {
	integration *RazorpayIntegration
}

// GetProviderType returns the provider type
func (p *RazorpayProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderRazorpay
}

// IsAvailable checks if Razorpay integration is available
func (p *RazorpayProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasRazorpayConnection(ctx)
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

	// Check HubSpot
	hubspotIntegration, err := f.GetHubSpotIntegration(ctx)
	if err == nil {
		hubspotProvider := &HubSpotProvider{integration: hubspotIntegration}
		if hubspotProvider.IsAvailable(ctx) {
			providers = append(providers, hubspotProvider)
		}
	}

	// Check Razorpay
	razorpayIntegration, err := f.GetRazorpayIntegration(ctx)
	if err == nil {
		razorpayProvider := &RazorpayProvider{integration: razorpayIntegration}
		if razorpayProvider.IsAvailable(ctx) {
			providers = append(providers, razorpayProvider)
		}
	}

	return providers, nil
}

// GetStorageProvider returns an S3 storage client for the given connection
// Currently only S3 is supported. In the future, Azure Blob Storage, Google Cloud Storage,
// and other providers can be added by checking the connection's provider type.
func (f *Factory) GetStorageProvider(ctx context.Context, connectionID string) (*s3.Client, error) {
	if f.s3Client == nil {
		f.s3Client = s3.NewClient(
			f.connectionRepo,
			f.encryptionService,
			f.logger,
		)
	}

	return f.s3Client, nil
}

// GetS3Client returns the S3 client directly (for backward compatibility)
// Deprecated: Use GetStorageProvider instead for future-proof code
func (f *Factory) GetS3Client(ctx context.Context) (*s3.Client, error) {
	if f.s3Client == nil {
		f.s3Client = s3.NewClient(
			f.connectionRepo,
			f.encryptionService,
			f.logger,
		)
	}
	return f.s3Client, nil
}
