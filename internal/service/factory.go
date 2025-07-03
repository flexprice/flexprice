package service

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	costsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/task"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxconfig"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/s3"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
)

// ServiceParams holds common dependencies for services
// TODO: start using this for all services init
type ServiceParams struct {
	Logger       *logger.Logger
	Config       *config.Configuration
	DB           postgres.IClient
	PDFGenerator pdf.Generator
	S3           s3.Service

	// Repositories
	AuthRepo                   auth.Repository
	UserRepo                   user.Repository
	EventRepo                  events.Repository
	ProcessedEventRepo         events.ProcessedEventRepository
	MeterRepo                  meter.Repository
	PriceRepo                  price.Repository
	CustomerRepo               customer.Repository
	PlanRepo                   plan.Repository
	SubRepo                    subscription.Repository
	SubscriptionScheduleRepo   subscription.SubscriptionScheduleRepository
	WalletRepo                 wallet.Repository
	TenantRepo                 tenant.Repository
	InvoiceRepo                invoice.Repository
	FeatureRepo                feature.Repository
	EntitlementRepo            entitlement.Repository
	PaymentRepo                payment.Repository
	SecretRepo                 secret.Repository
	EnvironmentRepo            environment.Repository
	TaskRepo                   task.Repository
	CreditGrantRepo            creditgrant.Repository
	CostSheetRepo              costsheet.Repository
	CreditNoteRepo             creditnote.Repository
	CreditNoteLineItemRepo     creditnote.CreditNoteLineItemRepository
	CreditGrantApplicationRepo creditgrantapplication.Repository
	TaxRateRepo                taxrate.Repository
	TaxConfigRepo              taxconfig.Repository
	// Publishers
	EventPublisher   publisher.EventPublisher
	WebhookPublisher webhookPublisher.WebhookPublisher

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
	subscriptionScheduleRepo subscription.SubscriptionScheduleRepository,
	walletRepo wallet.Repository,
	tenantRepo tenant.Repository,
	invoiceRepo invoice.Repository,
	featureRepo feature.Repository,
	creditGrantApplicationRepo creditgrantapplication.Repository,
	entitlementRepo entitlement.Repository,
	paymentRepo payment.Repository,
	secretRepo secret.Repository,
	environmentRepo environment.Repository,
	creditGrantRepo creditgrant.Repository,
	creditNoteRepo creditnote.Repository,
	creditNoteLineItemRepo creditnote.CreditNoteLineItemRepository,
	taxConfigRepo taxconfig.Repository,
	taskRepo task.Repository,
	costSheetRepo costsheet.Repository,
	taxRateRepo taxrate.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	s3Service s3.Service,
	client httpclient.Client,

) ServiceParams {
	return ServiceParams{
		Logger:                     logger,
		Config:                     config,
		DB:                         db,
		PDFGenerator:               pdfGenerator,
		AuthRepo:                   authRepo,
		UserRepo:                   userRepo,
		EventRepo:                  eventRepo,
		ProcessedEventRepo:         processedEventRepo,
		MeterRepo:                  meterRepo,
		PriceRepo:                  priceRepo,
		CustomerRepo:               customerRepo,
		PlanRepo:                   planRepo,
		SubRepo:                    subRepo,
		SubscriptionScheduleRepo:   subscriptionScheduleRepo,
		WalletRepo:                 walletRepo,
		TenantRepo:                 tenantRepo,
		InvoiceRepo:                invoiceRepo,
		FeatureRepo:                featureRepo,
		EntitlementRepo:            entitlementRepo,
		PaymentRepo:                paymentRepo,
		SecretRepo:                 secretRepo,
		EnvironmentRepo:            environmentRepo,
		CreditGrantRepo:            creditGrantRepo,
		CreditGrantApplicationRepo: creditGrantApplicationRepo,
		TaskRepo:                   taskRepo,
		CostSheetRepo:              costSheetRepo,
		CreditNoteRepo:             creditNoteRepo,
		CreditNoteLineItemRepo:     creditNoteLineItemRepo,
		TaxRateRepo:                taxRateRepo,
		TaxConfigRepo:              taxConfigRepo,
		EventPublisher:             eventPublisher,
		WebhookPublisher:           webhookPublisher,
		S3:                         s3Service,
		Client:                     client,
	}
}
