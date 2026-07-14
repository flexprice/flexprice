package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/chargebee"
	chargebeewebhook "github.com/flexprice/flexprice/internal/integration/chargebee/webhook"
	"github.com/flexprice/flexprice/internal/integration/hubspot"
	hubspotwebhook "github.com/flexprice/flexprice/internal/integration/hubspot/webhook"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	moyasarwebhook "github.com/flexprice/flexprice/internal/integration/moyasar/webhook"
	"github.com/flexprice/flexprice/internal/integration/nomod"
	nomodwebhook "github.com/flexprice/flexprice/internal/integration/nomod/webhook"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	paddlewebhook "github.com/flexprice/flexprice/internal/integration/paddle/webhook"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/integration/quickbooks"
	quickbookswebhook "github.com/flexprice/flexprice/internal/integration/quickbooks/webhook"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	razorpaywebhook "github.com/flexprice/flexprice/internal/integration/razorpay/webhook"
	"github.com/flexprice/flexprice/internal/integration/s3"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/integration/tabs"
	"github.com/flexprice/flexprice/internal/integration/whop"
	whopwebhook "github.com/flexprice/flexprice/internal/integration/whop/webhook"
	"github.com/flexprice/flexprice/internal/integration/zoho"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Factory manages different payment integration providers and storage providers
type Factory struct {
	config                       *config.Configuration
	logger                       *logger.Logger
	connectionRepo               connection.Repository
	customerRepo                 customer.Repository
	subscriptionRepo             subscription.Repository
	planRepo                     plan.Repository
	invoiceRepo                  invoice.Repository
	paymentRepo                  payment.Repository
	paymentMethodRepo            paymentmethod.Repository
	priceRepo                    price.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	entityIntegrationMappingSvc  interfaces.EntityIntegrationMappingService
	meterRepo                    meter.Repository
	featureRepo                  feature.Repository
	encryptionService            security.EncryptionService
	locker                       cache.Locker

	// Storage clients (cached for reuse)
	s3Client *s3.Client

	temporalSvc    temporalservice.TemporalService
	paymentService interfaces.PaymentService
	invoiceService interfaces.InvoiceService
	lifecycle      *payments.PaymentLifecycle
}

// NewFactory creates a new integration factory
func NewFactory(
	config *config.Configuration,
	logger *logger.Logger,
	connectionRepo connection.Repository,
	customerRepo customer.Repository,
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	invoiceRepo invoice.Repository,
	paymentRepo payment.Repository,
	paymentMethodRepo paymentmethod.Repository,
	priceRepo price.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	meterRepo meter.Repository,
	featureRepo feature.Repository,
	encryptionService security.EncryptionService,
	temporalSvc temporalservice.TemporalService,
	locker cache.Locker,
) *Factory {
	return &Factory{
		config:                       config,
		logger:                       logger,
		connectionRepo:               connectionRepo,
		customerRepo:                 customerRepo,
		subscriptionRepo:             subscriptionRepo,
		planRepo:                     planRepo,
		invoiceRepo:                  invoiceRepo,
		paymentRepo:                  paymentRepo,
		paymentMethodRepo:            paymentMethodRepo,
		priceRepo:                    priceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		entityIntegrationMappingSvc:  NewEntityIntegrationMappingAdapter(entityIntegrationMappingRepo),
		meterRepo:                    meterRepo,
		featureRepo:                  featureRepo,
		encryptionService:            encryptionService,
		locker:                       locker,
		temporalSvc:                  temporalSvc,
	}
}

// SetServices sets payment and invoice services on the factory.
// Called via fx.Invoke after all services are constructed to break the
// ServiceParams → Factory → PaymentService → ServiceParams cycle.
func (f *Factory) SetServices(paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) {
	f.paymentService = paymentService
	f.invoiceService = invoiceService
	f.lifecycle = payments.NewPaymentLifecycle(paymentService, invoiceService, f.logger)
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

	// Create quote sync service
	quoteSyncSvc := hubspot.NewQuoteSyncService(
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
		QuoteSyncSvc:   quoteSyncSvc,
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

	// Create invoice sync service; paymentSvc is wired in below (mutual dependency).
	invoiceSyncSvc := razorpay.NewInvoiceSyncService(
		razorpayClient,
		customerSvc.(*razorpay.CustomerService),
		f.invoiceRepo,
		f.paymentRepo,
		f.entityIntegrationMappingRepo,
		f.locker,
		f.logger,
	)

	// Create payment service
	paymentSvc := razorpay.NewPaymentService(
		razorpayClient,
		customerSvc,
		invoiceSyncSvc,
		f.locker,
		f.logger,
	)

	// Wire back: InvoiceSyncService needs PaymentService for AutoCharge calls.
	invoiceSyncSvc.SetPaymentService(paymentSvc)

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

	// Create plan sync service
	planSyncSvc := chargebee.NewPlanSyncService(chargebee.PlanSyncServiceParams{
		Client:                       chargebeeClient,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		MeterRepo:                    f.meterRepo,
		FeatureRepo:                  f.featureRepo,
		Logger:                       f.logger,
	})

	// Create invoice service
	invoiceSvc := chargebee.NewInvoiceService(chargebee.InvoiceServiceParams{
		Client:                       chargebeeClient,
		CustomerSvc:                  customerSvc,
		InvoiceRepo:                  f.invoiceRepo,
		PaymentRepo:                  f.paymentRepo,
		PriceRepo:                    f.priceRepo,
		PlanSyncSvc:                  planSyncSvc,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
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

// GetQuickBooksIntegration returns a complete QuickBooks integration setup
func (f *Factory) GetQuickBooksIntegration(ctx context.Context) (*QuickBooksIntegration, error) {
	// Verify a QuickBooks connection exists for this environment before building the integration
	conn, err := f.connectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != types.StatusPublished {
		return nil, ierr.NewError("Connection with provider quickbooks is not configured in this environment").
			WithHint("QuickBooks connection must be configured and published before use").
			Mark(ierr.ErrNotFound)
	}

	// Create QuickBooks client
	qbClient := quickbooks.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := quickbooks.NewCustomerService(quickbooks.CustomerServiceParams{
		Client:                       qbClient,
		CustomerRepo:                 f.customerRepo,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		Logger:                       f.logger,
	})

	// Create item sync service
	itemSyncSvc := quickbooks.NewItemSyncService(quickbooks.ItemSyncServiceParams{
		Client:                       qbClient,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		MeterRepo:                    f.meterRepo,
		Logger:                       f.logger,
	})

	// Create invoice service
	invoiceSvc := quickbooks.NewInvoiceService(quickbooks.InvoiceServiceParams{
		Client:                       qbClient,
		CustomerSvc:                  customerSvc,
		CustomerRepo:                 f.customerRepo,
		InvoiceRepo:                  f.invoiceRepo,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		Logger:                       f.logger,
	})

	// Create payment service
	paymentSvc := quickbooks.NewPaymentService(quickbooks.PaymentServiceParams{
		Client:                       qbClient,
		InvoiceRepo:                  f.invoiceRepo,
		EntityIntegrationMappingRepo: f.entityIntegrationMappingRepo,
		Logger:                       f.logger,
	})

	// Create webhook handler
	webhookHandler := quickbookswebhook.NewHandler(
		qbClient,
		paymentSvc,
		f.connectionRepo,
		f.logger,
	)

	return &QuickBooksIntegration{
		Client:         qbClient,
		CustomerSvc:    customerSvc,
		ItemSyncSvc:    itemSyncSvc,
		InvoiceSvc:     invoiceSvc,
		PaymentSvc:     paymentSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetPaddleIntegration returns a complete Paddle integration setup
func (f *Factory) GetPaddleIntegration(ctx context.Context) (*PaddleIntegration, error) {
	conn, err := f.connectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != types.StatusPublished {
		return nil, ierr.NewError("Connection with provider paddle is not configured in this environment").
			WithHint("Paddle connection must be configured and published before use").
			Mark(ierr.ErrNotFound)
	}

	paddleClient := paddle.NewClient(f.connectionRepo, f.encryptionService, f.logger)

	syncSvc := paddle.NewPaddleSyncService(
		paddleClient,
		f.customerRepo,
		f.invoiceRepo,
		f.subscriptionRepo,
		f.entityIntegrationMappingSvc,
		f.connectionRepo,
		f.logger,
		f.config.Auth.Secret,
		f.temporalSvc,
	)
	syncSvc.SetServices(f.paymentService, f.invoiceService)

	paymentSvc := paddle.NewPaymentService(f.logger)

	webhookHandler := paddlewebhook.NewHandler(
		paymentSvc,
		syncSvc,
		f.logger,
	)

	return &PaddleIntegration{
		Client:         paddleClient,
		SyncSvc:        syncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetNomodIntegration returns a complete Nomod integration setup
func (f *Factory) GetNomodIntegration(ctx context.Context) (*NomodIntegration, error) {
	// Create Nomod client
	nomodClient := nomod.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := nomod.NewCustomerService(
		nomodClient,
		f.customerRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create invoice sync service
	invoiceSyncSvc := nomod.NewInvoiceSyncService(
		nomodClient,
		customerSvc.(*nomod.CustomerService),
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create payment service
	paymentSvc := nomod.NewPaymentService(
		nomodClient,
		customerSvc,
		invoiceSyncSvc,
		f.logger,
	)

	// Create webhook handler
	webhookHandler := nomodwebhook.NewHandler(
		nomodClient,
		paymentSvc,
		invoiceSyncSvc,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	return &NomodIntegration{
		Client:         nomodClient,
		CustomerSvc:    customerSvc,
		PaymentSvc:     paymentSvc,
		InvoiceSyncSvc: invoiceSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetMoyasarIntegration returns a complete Moyasar integration setup
func (f *Factory) GetMoyasarIntegration(ctx context.Context) (*MoyasarIntegration, error) {
	// Create Moyasar client
	moyasarClient := moyasar.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)

	// Create customer service
	customerSvc := moyasar.NewCustomerService(
		moyasarClient,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create invoice sync service
	invoiceSyncSvc := moyasar.NewInvoiceSyncService(
		moyasarClient,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	// Create payment service
	paymentSvc := moyasar.NewPaymentService(
		moyasarClient,
		customerSvc,
		invoiceSyncSvc,
		f.logger,
	)

	// Create webhook handler
	webhookHandler := moyasarwebhook.NewHandler(
		moyasarClient,
		paymentSvc,
		f.entityIntegrationMappingRepo,
		f.paymentMethodRepo,
		f.lifecycle,
		f.logger,
	)

	return &MoyasarIntegration{
		Client:            moyasarClient,
		CustomerSvc:       customerSvc,
		PaymentSvc:        paymentSvc,
		InvoiceSyncSvc:    invoiceSyncSvc,
		WebhookHandler:    webhookHandler,
		Lifecycle:         f.lifecycle,
		PaymentMethodRepo: f.paymentMethodRepo,
		Logger:            f.logger,
	}, nil
}

// GetWhopIntegration returns a complete Whop integration setup
func (f *Factory) GetWhopIntegration(ctx context.Context) (*WhopIntegration, error) {
	whopClient := whop.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
		f.config,
	)

	invoiceSyncSvc := whop.NewInvoiceSyncService(
		whopClient,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	webhookHandler := whopwebhook.NewHandler(
		f.entityIntegrationMappingRepo,
		invoiceSyncSvc,
		whopClient,
		f.logger,
	)

	return &WhopIntegration{
		Client:         whopClient,
		InvoiceSyncSvc: invoiceSyncSvc,
		WebhookHandler: webhookHandler,
	}, nil
}

// GetZohoBooksIntegration returns a complete Zoho Books integration setup
func (f *Factory) GetZohoBooksIntegration(ctx context.Context) (*ZohoBooksIntegration, error) {
	conn, err := f.connectionRepo.GetByProvider(ctx, types.SecretProviderZohoBooks)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != types.StatusPublished {
		return nil, ierr.NewError("Connection with provider zoho_books is not configured in this environment").
			WithHint("Zoho Books connection must be configured and published before use").
			Mark(ierr.ErrNotFound)
	}

	zohoClient := zoho.NewClient(
		f.connectionRepo,
		f.encryptionService,
		f.logger,
	)
	customerSvc := zoho.NewCustomerService(
		zohoClient,
		f.customerRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)
	taxSvc := zoho.NewTaxService(zohoClient, f.logger)
	itemSyncSvc := zoho.NewItemSyncService(zoho.ItemSyncServiceParams{
		Client:      zohoClient,
		MappingRepo: f.entityIntegrationMappingRepo,
		Logger:      f.logger,
	})
	invoiceSvc := zoho.NewInvoiceService(
		zohoClient,
		customerSvc,
		itemSyncSvc,
		taxSvc,
		f.customerRepo,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	return &ZohoBooksIntegration{
		Client:      zohoClient,
		CustomerSvc: customerSvc,
		InvoiceSvc:  invoiceSvc,
		ItemSyncSvc: itemSyncSvc,
		TaxSvc:      taxSvc,
	}, nil
}

// GetTabsIntegration returns a complete Tabs integration setup for the current environment. It
// mirrors the other providers: it resolves the published Tabs connection and wires the invoice
// sync service with the repositories it needs.
func (f *Factory) GetTabsIntegration(ctx context.Context) (*TabsIntegration, error) {
	conn, err := f.connectionRepo.GetByProvider(ctx, types.SecretProviderTabs)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != types.StatusPublished {
		return nil, ierr.NewError("Connection with provider tabs is not configured in this environment").
			WithHint("Tabs connection must be configured and published before use").
			Mark(ierr.ErrNotFound)
	}

	client := tabs.NewClient(f.connectionRepo, f.encryptionService, f.logger)
	invoiceSvc := tabs.NewInvoiceService(
		client,
		f.customerRepo,
		f.subscriptionRepo,
		f.planRepo,
		f.priceRepo,
		f.invoiceRepo,
		f.entityIntegrationMappingRepo,
		f.logger,
	)

	return &TabsIntegration{
		Client:     client,
		InvoiceSvc: invoiceSvc,
	}, nil
}

// GetIntegrationByProvider returns the appropriate integration for the given provider type
func (f *Factory) GetIntegrationByProvider(ctx context.Context, providerType types.SecretProvider) (Base, error) {
	switch providerType {
	case types.SecretProviderStripe:
		return f.GetStripeIntegration(ctx)
	case types.SecretProviderHubSpot:
		return f.GetHubSpotIntegration(ctx)
	case types.SecretProviderRazorpay:
		return f.GetRazorpayIntegration(ctx)
	case types.SecretProviderChargebee:
		return f.GetChargebeeIntegration(ctx)
	case types.SecretProviderQuickBooks:
		return f.GetQuickBooksIntegration(ctx)
	case types.SecretProviderNomod:
		return f.GetNomodIntegration(ctx)
	case types.SecretProviderPaddle:
		return f.GetPaddleIntegration(ctx)
	case types.SecretProviderMoyasar:
		return f.GetMoyasarIntegration(ctx)
	case types.SecretProviderZohoBooks:
		return f.GetZohoBooksIntegration(ctx)
	case types.SecretProviderWhop:
		return f.GetWhopIntegration(ctx)
	case types.SecretProviderTabs:
		return f.GetTabsIntegration(ctx)
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
		types.SecretProviderQuickBooks,
		types.SecretProviderNomod,
		types.SecretProviderPaddle,
		types.SecretProviderMoyasar,
		types.SecretProviderZohoBooks,
		types.SecretProviderWhop,
		types.SecretProviderTabs,
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

func (s *StripeIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for stripe")
}

// HubSpotIntegration contains all HubSpot integration services
type HubSpotIntegration struct {
	Client         hubspot.HubSpotClient
	CustomerSvc    hubspot.HubSpotCustomerService
	InvoiceSyncSvc *hubspot.InvoiceSyncService
	DealSyncSvc    *hubspot.DealSyncService
	QuoteSyncSvc   *hubspot.QuoteSyncService
	WebhookHandler *hubspotwebhook.Handler
}

func (h *HubSpotIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for hubspot")
}

// RazorpayIntegration contains all Razorpay integration services
type RazorpayIntegration struct {
	Client         razorpay.RazorpayClient
	CustomerSvc    razorpay.RazorpayCustomerService
	PaymentSvc     *razorpay.PaymentService
	InvoiceSyncSvc *razorpay.InvoiceSyncService
	WebhookHandler *razorpaywebhook.Handler
}

func (r *RazorpayIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for razorpay")
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

func (c *ChargebeeIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for chargebee")
}

// QuickBooksIntegration contains all QuickBooks integration services
type QuickBooksIntegration struct {
	Client         quickbooks.QuickBooksClient
	CustomerSvc    quickbooks.QuickBooksCustomerService
	ItemSyncSvc    quickbooks.QuickBooksItemSyncService
	InvoiceSvc     quickbooks.QuickBooksInvoiceService
	PaymentSvc     quickbooks.QuickBooksPaymentService
	WebhookHandler *quickbookswebhook.Handler
}

func (q *QuickBooksIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for quickbooks")
}

// PaddleIntegration contains all Paddle integration services
type PaddleIntegration struct {
	Client         paddle.PaddleClient
	SyncSvc        *paddle.PaddleSyncService
	WebhookHandler *paddlewebhook.Handler
}

func (p *PaddleIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return p.SyncSvc.PullAndUpdateInvoice(ctx, invoiceID)
}

// NomodIntegration contains all Nomod integration services
type NomodIntegration struct {
	Client         nomod.NomodClient
	CustomerSvc    nomod.NomodCustomerService
	PaymentSvc     *nomod.PaymentService
	InvoiceSyncSvc *nomod.InvoiceSyncService
	WebhookHandler *nomodwebhook.Handler
}

func (n *NomodIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for nomod")
}

// MoyasarIntegration contains all Moyasar integration services
type MoyasarIntegration struct {
	Client            moyasar.MoyasarClient
	CustomerSvc       moyasar.MoyasarCustomerService
	PaymentSvc        *moyasar.PaymentService
	InvoiceSyncSvc    *moyasar.InvoiceSyncService
	WebhookHandler    *moyasarwebhook.Handler
	Lifecycle         *payments.PaymentLifecycle
	PaymentMethodRepo paymentmethod.Repository
	Logger            *logger.Logger
}

func (m *MoyasarIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for moyasar")
}

// VoidOrRefundAuthPayment attempts to void an AUTH payment in Moyasar.
// If void fails, it falls back to a full refund.
// Returns (voided, refunded, err).
func (m *MoyasarIntegration) VoidOrRefundAuthPayment(ctx context.Context, flexpricePaymentID string, gatewayPaymentID string) (voided bool, refunded bool, err error) {
	if m.Lifecycle == nil {
		return false, false, ierr.NewError("lifecycle not initialised").Mark(ierr.ErrInternal)
	}

	// Try void first
	if _, voidErr := m.Client.VoidPayment(ctx, gatewayPaymentID); voidErr == nil {
		if lifecycleErr := m.Lifecycle.RecordPaymentVoided(ctx, payments.RecordPaymentVoidedParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   gatewayPaymentID,
			VoidedAt:           time.Now().UTC(),
		}); lifecycleErr != nil {
			return false, false, lifecycleErr
		}
		return true, false, nil
	} else {
		// Log void failure so we can distinguish transient vs permanent failures in observability
		m.Logger.Info(ctx, "void attempt failed, falling back to refund",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", voidErr.Error(),
		)
	}

	// Void failed — try full refund (amount=0 means full refund)
	if _, refundErr := m.Client.RefundPayment(ctx, gatewayPaymentID, 0); refundErr != nil {
		return false, false, refundErr
	}
	if lifecycleErr := m.Lifecycle.RecordPaymentRefunded(ctx, payments.RecordPaymentRefundedParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   gatewayPaymentID,
		RefundedAt:         time.Now().UTC(),
	}); lifecycleErr != nil {
		return false, false, lifecycleErr
	}
	return false, true, nil
}

// InitiateTokenization creates a Flexprice AUTH payment record for card tokenization.
// Returns the flexprice_payment_id (anchor for webhook reconciliation) and the
// Moyasar publishable key (needed by Moyasar.js on the frontend).
func (m *MoyasarIntegration) InitiateTokenization(ctx context.Context, customerID string) (flexpricePaymentID string, publishableKey string, err error) {
	if m.Lifecycle == nil {
		return "", "", ierr.NewError("lifecycle not initialised").Mark(ierr.ErrInternal)
	}

	flexpricePaymentID, err = m.Lifecycle.InitiatePayment(ctx, payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeCustomer,
		DestinationID:     customerID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.SecretProviderMoyasar),
		Amount:            decimal.NewFromInt(1),
		Currency:          moyasar.DefaultCurrency,
	})
	if err != nil {
		return "", "", err
	}

	config, err := m.Client.GetMoyasarConfig(ctx)
	if err != nil {
		return "", "", err
	}

	return flexpricePaymentID, config.PublishableKey, nil
}

// ChargeInvoiceWithToken charges an invoice using the customer's default saved Moyasar token.
// Returns (true, nil) if the charge was initiated, (false, nil) if no active token exists
// (caller should fall through to invoice-link flow), or (false, err) on failure.
func (m *MoyasarIntegration) ChargeInvoiceWithToken(ctx context.Context, invoiceID, customerID string, amount decimal.Decimal, currency, moyasarInvoiceID string) (charged bool, err error) {
	if m.Lifecycle == nil || m.PaymentMethodRepo == nil {
		return false, ierr.NewError("lifecycle or payment method repo not initialised").Mark(ierr.ErrInternal)
	}

	gateway := string(types.SecretProviderMoyasar)
	paymentMethod, err := m.PaymentMethodRepo.GetDefaultForCustomer(ctx, customerID, gateway)
	if err != nil {
		if ierr.IsNotFound(err) {
			return false, nil // No saved token — fall through to invoice-link flow
		}
		return false, err
	}
	if paymentMethod.PaymentMethodStatus != types.PaymentMethodStatusActive {
		return false, nil // Token exists but not active — fall through
	}

	flexpricePaymentID, err := m.Lifecycle.InitiatePayment(ctx, payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           gateway,
		Amount:            amount,
		Currency:          currency,
	})
	if err != nil {
		return false, err
	}

	chargeResp, err := m.PaymentSvc.ChargeSavedPaymentMethod(
		ctx,
		customerID,
		paymentMethod.GatewayMethodID,
		amount,
		currency,
		"",
		moyasarInvoiceID,
		flexpricePaymentID,
	)
	if err != nil {
		if failErr := m.Lifecycle.RecordPaymentFailure(ctx, payments.RecordPaymentFailureParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   "",
			FailedAt:           time.Now().UTC(),
			ErrorMessage:       err.Error(),
		}); failErr != nil {
			m.Logger.Error(ctx, "failed to record payment failure after charge error",
				"flexprice_payment_id", flexpricePaymentID,
				"error", failErr,
			)
		}
		return false, err
	}

	// Transition to PENDING now that Moyasar accepted the charge
	if err := m.Lifecycle.ConfirmGatewayPayment(ctx, flexpricePaymentID, chargeResp.ID); err != nil {
		return false, err
	}

	return true, nil
}

// WhopIntegration contains all Whop integration services
type WhopIntegration struct {
	Client         whop.WhopClient
	InvoiceSyncSvc *whop.InvoiceSyncService
	WebhookHandler *whopwebhook.Handler
}

func (w *WhopIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for whop")
}

// ZohoBooksIntegration contains all Zoho Books integration services
type ZohoBooksIntegration struct {
	Client      zoho.ZohoClient
	CustomerSvc zoho.ZohoCustomerService
	InvoiceSvc  zoho.ZohoInvoiceService
	ItemSyncSvc zoho.ZohoItemSyncService
	TaxSvc      zoho.ZohoTaxService
}

func (z *ZohoBooksIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for zohobooks")
}

// TabsIntegration contains all Tabs integration services
type TabsIntegration struct {
	Client     tabs.TabsClient
	InvoiceSvc tabs.TabsInvoiceService
}

func (t *TabsIntegration) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull sync not supported for tabs")
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

// QuickBooksProvider implements IntegrationProvider for QuickBooks
type QuickBooksProvider struct {
	integration *QuickBooksIntegration
}

// GetProviderType returns the provider type
func (p *QuickBooksProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderQuickBooks
}

// IsAvailable checks if QuickBooks integration is available
func (p *QuickBooksProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasQuickBooksConnection(ctx)
}

// NomodProvider implements IntegrationProvider for Nomod
type NomodProvider struct {
	integration *NomodIntegration
}

// GetProviderType returns the provider type
func (p *NomodProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderNomod
}

// IsAvailable checks if Nomod integration is available
func (p *NomodProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasNomodConnection(ctx)
}

// PaddleProvider implements IntegrationProvider for Paddle
type PaddleProvider struct {
	integration *PaddleIntegration
}

// ZohoBooksProvider implements IntegrationProvider for Zoho Books
type ZohoBooksProvider struct {
	integration *ZohoBooksIntegration
}

// GetProviderType returns the provider type
func (p *ZohoBooksProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderZohoBooks
}

// IsAvailable checks if Zoho Books integration is available
func (p *ZohoBooksProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasZohoBooksConnection(ctx)
}

// GetProviderType returns the provider type
func (p *PaddleProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderPaddle
}

// IsAvailable checks if Paddle integration is available
func (p *PaddleProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasPaddleConnection(ctx)
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

	// Check QuickBooks
	quickbooksIntegration, err := f.GetQuickBooksIntegration(ctx)
	if err == nil {
		quickbooksProvider := &QuickBooksProvider{integration: quickbooksIntegration}
		if quickbooksProvider.IsAvailable(ctx) {
			providers = append(providers, quickbooksProvider)
		}
	}

	// Check Nomod
	nomodIntegration, err := f.GetNomodIntegration(ctx)
	if err == nil {
		nomodProvider := &NomodProvider{integration: nomodIntegration}
		if nomodProvider.IsAvailable(ctx) {
			providers = append(providers, nomodProvider)
		}
	}

	// Check Paddle
	paddleIntegration, err := f.GetPaddleIntegration(ctx)
	if err == nil {
		paddleProvider := &PaddleProvider{integration: paddleIntegration}
		if paddleProvider.IsAvailable(ctx) {
			providers = append(providers, paddleProvider)
		}
	}

	// Check Zoho Books
	zohoIntegration, err := f.GetZohoBooksIntegration(ctx)
	if err == nil {
		zohoProvider := &ZohoBooksProvider{integration: zohoIntegration}
		if zohoProvider.IsAvailable(ctx) {
			providers = append(providers, zohoProvider)
		}
	}

	// Check Whop
	whopIntegration, err := f.GetWhopIntegration(ctx)
	if err == nil {
		whopProvider := &WhopProvider{integration: whopIntegration}
		if whopProvider.IsAvailable(ctx) {
			providers = append(providers, whopProvider)
		}
	}

	// Check Tabs
	tabsIntegration, err := f.GetTabsIntegration(ctx)
	if err == nil {
		tabsProvider := &TabsProvider{integration: tabsIntegration}
		if tabsProvider.IsAvailable(ctx) {
			providers = append(providers, tabsProvider)
		}
	}

	return providers, nil
}

// WhopProvider implements IntegrationProvider for Whop
type WhopProvider struct {
	integration *WhopIntegration
}

// GetProviderType returns the provider type
func (p *WhopProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderWhop
}

// IsAvailable checks if Whop integration is available
func (p *WhopProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasWhopConnection(ctx)
}

// TabsProvider implements IntegrationProvider for Tabs
type TabsProvider struct {
	integration *TabsIntegration
}

// GetProviderType returns the provider type
func (p *TabsProvider) GetProviderType() types.SecretProvider {
	return types.SecretProviderTabs
}

// IsAvailable checks if Tabs integration is available
func (p *TabsProvider) IsAvailable(ctx context.Context) bool {
	return p.integration.Client.HasTabsConnection(ctx)
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

// GetCheckoutProvider returns the CheckoutProvider adapter for the given payment provider.
// Returns ErrValidation for providers that do not support hosted checkout.
func (f *Factory) GetCheckoutProvider(ctx context.Context, provider types.CheckoutPaymentProvider, customerSvc interfaces.CustomerService, invoiceSvc interfaces.InvoiceService) (interfaces.CheckoutProvider, error) {
	switch provider {
	case types.CheckoutPaymentProviderRazorpay:
		i, err := f.GetRazorpayIntegration(ctx)
		if err != nil {
			return nil, err
		}
		return &razorpay.CheckoutAdapter{Svc: i.PaymentSvc, CustomerSvc: customerSvc, InvoiceSvc: invoiceSvc}, nil
	default:
		return nil, ierr.NewError("payment provider not supported for checkout").
			WithHintf("%s does not support hosted checkout sessions", provider).
			Mark(ierr.ErrValidation)
	}
}
