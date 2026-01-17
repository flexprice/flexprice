package service

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/connection"
	costsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/task"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxapplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/types"
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
	AuthRepo                     auth.Repository
	UserRepo                     user.Repository
	EventRepo                    events.Repository
	CostSheetUsageRepo           events.CostSheetUsageRepository
	ProcessedEventRepo           events.ProcessedEventRepository
	FeatureUsageRepo             events.FeatureUsageRepository
	MeterRepo                    meter.Repository
	PriceRepo                    price.Repository
	PriceUnitRepo                priceunit.Repository
	CustomerRepo                 customer.Repository
	PlanRepo                     plan.Repository
	SubRepo                      subscription.Repository
	SubscriptionLineItemRepo     subscription.LineItemRepository
	SubscriptionPhaseRepo        subscription.SubscriptionPhaseRepository
	SubScheduleRepo              subscription.SubscriptionScheduleRepository
	WalletRepo                   wallet.Repository
	TenantRepo                   tenant.Repository
	InvoiceRepo                  invoice.Repository
	FeatureRepo                  feature.Repository
	EntitlementRepo              entitlement.Repository
	PaymentRepo                  payment.Repository
	SecretRepo                   secret.Repository
	EnvironmentRepo              environment.Repository
	TaskRepo                     task.Repository
	CreditGrantRepo              creditgrant.Repository
	CostSheetRepo                costsheet.Repository
	CreditNoteRepo               creditnote.Repository
	CreditNoteLineItemRepo       creditnote.CreditNoteLineItemRepository
	CreditGrantApplicationRepo   creditgrantapplication.Repository
	TaxRateRepo                  taxrate.Repository
	TaxAssociationRepo           taxassociation.Repository
	TaxAppliedRepo               taxapplied.Repository
	CouponRepo                   coupon.Repository
	CouponAssociationRepo        coupon_association.Repository
	CouponApplicationRepo        coupon_application.Repository
	AddonRepo                    addon.Repository
	AddonAssociationRepo         addonassociation.Repository
	ConnectionRepo               connection.Repository
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	SettingsRepo                 settings.Repository
	AlertLogsRepo                alertlogs.Repository
	GroupRepo                    group.Repository
	ScheduledTaskRepo            scheduledtask.Repository

	// Publishers
	EventPublisher   publisher.EventPublisher
	WebhookPublisher webhookPublisher.WebhookPublisher

	// http client
	Client httpclient.Client

	// Proration
	ProrationCalculator proration.Calculator

	// Integration Factory
	IntegrationFactory *integration.Factory

	// PubSubs
	WalletBalanceAlertPubSub types.WalletBalanceAlertPubSub
	WebhookPubSub            pubsub.PubSub
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
	costSheetUsageRepo events.CostSheetUsageRepository,
	processedEventRepo events.ProcessedEventRepository,
	featureUsageRepo events.FeatureUsageRepository,
	meterRepo meter.Repository,
	priceRepo price.Repository,
	priceUnitRepo priceunit.Repository,
	customerRepo customer.Repository,
	planRepo plan.Repository,
	subRepo subscription.Repository,
	subscriptionLineItemRepo subscription.LineItemRepository,
	subscriptionPhaseRepo subscription.SubscriptionPhaseRepository,
	subScheduleRepo subscription.SubscriptionScheduleRepository,
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
	taxConfigRepo taxassociation.Repository,
	taskRepo task.Repository,
	costSheetRepo costsheet.Repository,
	taxAppliedRepo taxapplied.Repository,
	taxRateRepo taxrate.Repository,
	couponRepo coupon.Repository,
	couponAssociationRepo coupon_association.Repository,
	couponApplicationRepo coupon_application.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	s3Service s3.Service,
	client httpclient.Client,
	addonRepo addon.Repository,
	addonAssociationRepo addonassociation.Repository,
	connectionRepo connection.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	settingsRepo settings.Repository,
	alertLogsRepo alertlogs.Repository,
	groupRepo group.Repository,
	scheduledTaskRepo scheduledtask.Repository,
	prorationCalculator proration.Calculator,
	integrationFactory *integration.Factory,
	walletBalanceAlertPubSub types.WalletBalanceAlertPubSub,
	webhookPubSub pubsub.PubSub,
) ServiceParams {
	return ServiceParams{
		Logger:                       logger,
		Config:                       config,
		DB:                           db,
		PDFGenerator:                 pdfGenerator,
		AuthRepo:                     authRepo,
		UserRepo:                     userRepo,
		EventRepo:                    eventRepo,
		CostSheetUsageRepo:           costSheetUsageRepo,
		ProcessedEventRepo:           processedEventRepo,
		FeatureUsageRepo:             featureUsageRepo,
		MeterRepo:                    meterRepo,
		PriceRepo:                    priceRepo,
		PriceUnitRepo:                priceUnitRepo,
		CustomerRepo:                 customerRepo,
		PlanRepo:                     planRepo,
		SubRepo:                      subRepo,
		SubscriptionLineItemRepo:     subscriptionLineItemRepo,
		SubscriptionPhaseRepo:        subscriptionPhaseRepo,
		SubScheduleRepo:              subScheduleRepo,
		WalletRepo:                   walletRepo,
		TenantRepo:                   tenantRepo,
		InvoiceRepo:                  invoiceRepo,
		FeatureRepo:                  featureRepo,
		EntitlementRepo:              entitlementRepo,
		PaymentRepo:                  paymentRepo,
		SecretRepo:                   secretRepo,
		EnvironmentRepo:              environmentRepo,
		CreditGrantRepo:              creditGrantRepo,
		CreditGrantApplicationRepo:   creditGrantApplicationRepo,
		TaskRepo:                     taskRepo,
		CostSheetRepo:                costSheetRepo,
		CreditNoteRepo:               creditNoteRepo,
		CreditNoteLineItemRepo:       creditNoteLineItemRepo,
		TaxRateRepo:                  taxRateRepo,
		TaxAssociationRepo:           taxConfigRepo,
		TaxAppliedRepo:               taxAppliedRepo,
		EventPublisher:               eventPublisher,
		WebhookPublisher:             webhookPublisher,
		S3:                           s3Service,
		Client:                       client,
		CouponRepo:                   couponRepo,
		CouponAssociationRepo:        couponAssociationRepo,
		CouponApplicationRepo:        couponApplicationRepo,
		AddonRepo:                    addonRepo,
		AddonAssociationRepo:         addonAssociationRepo,
		ConnectionRepo:               connectionRepo,
		EntityIntegrationMappingRepo: entityIntegrationMappingRepo,
		SettingsRepo:                 settingsRepo,
		AlertLogsRepo:                alertLogsRepo,
		GroupRepo:                    groupRepo,
		ScheduledTaskRepo:            scheduledTaskRepo,
		ProrationCalculator:          prorationCalculator,
		IntegrationFactory:           integrationFactory,
		WalletBalanceAlertPubSub:     walletBalanceAlertPubSub,
		WebhookPubSub:                webhookPubSub,
	}
}
