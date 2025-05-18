package service

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/httpclient"
	integrations "github.com/flexprice/flexprice/internal/integrations/manager"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/s3"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
)

// ServiceParams contains common dependencies for services
type ServiceParams struct {
	Logger       *logger.Logger
	Config       *config.Configuration
	DB           postgres.IClient
	PDFGenerator pdf.Generator
	S3           s3.Service

	// Repositories
	AuthRepo        auth.Repository
	UserRepo        user.Repository
	EventRepo       events.Repository
	MeterRepo       meter.Repository
	PriceRepo       price.Repository
	CustomerRepo    customer.Repository
	PlanRepo        plan.Repository
	SubRepo         subscription.Repository
	WalletRepo      wallet.Repository
	TenantRepo      tenant.Repository
	InvoiceRepo     invoice.Repository
	FeatureRepo     feature.Repository
	EntitlementRepo entitlement.Repository
	PaymentRepo     payment.Repository
	SecretRepo      secret.Repository
	EnvironmentRepo environment.Repository
	TaskRepo        task.Repository
	IntegrationRepo integration.Repository
	ConnectionRepo  connection.Repository

	// Publishers
	EventPublisher   publisher.EventPublisher
	WebhookPublisher webhookPublisher.WebhookPublisher

	// Integration manager
	GatewayManager integrations.GatewayManager

	// http client
	Client httpclient.Client
}

// Common service params
func NewServiceParams(
	logger *logger.Logger,
	config *config.Configuration,
	db postgres.IClient,
	pdfGenerator pdf.Generator,
	authRepo auth.Repository,
	userRepo user.Repository,
	eventRepo events.Repository,
	processedEventRepo events.ProcessedEventRepository,
	meterRepo meter.Repository,
	priceRepo price.Repository,
	customerRepo customer.Repository,
	planRepo plan.Repository,
	subRepo subscription.Repository,
	connectionRepo connection.Repository,
	walletRepo wallet.Repository,
	tenantRepo tenant.Repository,
	invoiceRepo invoice.Repository,
	featureRepo feature.Repository,
	entitlementRepo entitlement.Repository,
	paymentRepo payment.Repository,
	secretRepo secret.Repository,
	environmentRepo environment.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	taskRepo task.Repository,
	integrationRepo integration.Repository,
	gatewayManager integrations.GatewayManager,
) ServiceParams {
	return ServiceParams{
		Logger:           logger,
		Config:           config,
		DB:               db,
		UserRepo:         userRepo,
		EventRepo:        eventRepo,
		MeterRepo:        meterRepo,
		PriceRepo:        priceRepo,
		CustomerRepo:     customerRepo,
		PlanRepo:         planRepo,
		SubRepo:          subRepo,
		WalletRepo:       walletRepo,
		TenantRepo:       tenantRepo,
		InvoiceRepo:      invoiceRepo,
		FeatureRepo:      featureRepo,
		EntitlementRepo:  entitlementRepo,
		PaymentRepo:      paymentRepo,
		SecretRepo:       secretRepo,
		EnvironmentRepo:  environmentRepo,
		TaskRepo:         taskRepo,
		IntegrationRepo:  integrationRepo,
		ConnectionRepo:   connectionRepo,
		EventPublisher:   eventPublisher,
		WebhookPublisher: webhookPublisher,
		GatewayManager:   gatewayManager,
	}
}
