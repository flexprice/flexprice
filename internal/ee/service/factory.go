package service

import (
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	"github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	domainAnalytics "github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/auth"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/commitment"
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
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/hostedinvoicetoken"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/secret"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/task"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxapplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/pubsub"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
)

// ServiceParams holds common dependencies for services
// TODO: start using this for all services init
type ServiceParams struct {
	Logger        *logger.Logger
	Config        *config.Configuration
	DB            postgres.IClient
	PDFGenerator  pdf.Generator
	TracingSvc    *tracing.Service
	InMemoryCache cache.InMemoryCache
	RedisCache    cache.RedisCache
	Locker        cache.Locker

	// Repositories
	AuthRepo                     auth.Repository
	UserRepo                     user.Repository
	EventRepo                    events.Repository
	ProcessedEventRepo           events.ProcessedEventRepository
	RawEventRepo                 events.RawEventRepository
	MeterUsageRepo               events.MeterUsageRepository
	MeterUsageCheckpointRepo     meterusagecheckpoint.Repository
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
	InvoiceLineItemRepo          invoice.LineItemRepository
	FeatureRepo                  feature.Repository
	EntitlementRepo              entitlement.Repository
	EntitlementGrantRepo         entitlementgrant.Repository
	PaymentRepo                  payment.Repository
	RefundRepo                   refund.Repository
	SecretRepo                   secret.Repository
	EnvironmentRepo              environment.Repository
	TaskRepo                     task.Repository
	CreditGrantRepo              creditgrant.Repository
	CostSheetRepo                costsheet.Repository
	CreditNoteRepo               creditnote.Repository
	CreditNoteLineItemRepo       creditnote.CreditNoteLineItemRepository
	CreditGrantApplicationRepo   creditgrantapplication.Repository
	TaxRateRepo                  taxrate.Repository
	FXRateRepo                   fxrate.Repository
	HostedInvoiceTokenRepo       hostedinvoicetoken.Repository
	TaxAssociationRepo           taxassociation.Repository
	TaxAppliedRepo               taxapplied.Repository
	CouponRepo                   coupon.Repository
	CouponAssociationRepo        coupon_association.Repository
	CouponApplicationRepo        coupon_application.Repository
	AddonRepo                    addon.Repository
	AddonAssociationRepo         addonassociation.Repository
	ConnectionRepo               connection.Repository
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	UsageRecordRepo              usagerecord.Repository
	SettingsRepo                 settings.Repository
	AlertLogsRepo                alertlogs.Repository
	AlertRepo                    alert.Repository
	GroupRepo                    group.Repository
	ScheduledTaskRepo            scheduledtask.Repository
	PlanPriceSyncRepo            planpricesync.Repository
	AddonPriceSyncRepo           addonpricesync.Repository
	WorkflowExecutionRepo        workflowexecution.Repository
	CheckoutSessionRepo          domainCheckout.Repository
	AnalyticsViewRepo            domainAnalytics.Repository
	ReportsRepo                  domainAnalytics.ReportsRepository
	RevenueFactRepo              revenuefact.Repository
	ActivityRepo                 domainActivity.Repository
	SelfBillingClickHouseRepo    domainsb.ClickHouseRepository
	SelfBillingPostgresRepo      domainsb.PostgresRepository
	CommitmentRepo               commitment.Repository
	ProductRepo                  productRepo.Repository
	ContractService              contractsvc.ContractService
	POBService                   pobsvc.PerformanceObligationService
	JournalRepo                  journalRepo.Repository
	JournalService               journalsvc.JournalService
	EventDeletionRequestRepo     events.DeletionRequestRepository
	EventDeletionLogRepo         events.DeletedEventsLogRepository

	// Publishers
	EventPublisher   publisher.EventPublisher
	WebhookPublisher webhookPublisher.WebhookPublisher

	// http client
	Client httpclient.Client

	// Proration
	ProrationCalculator proration.Calculator

	// Integration Factory
	IntegrationFactory *integration.Factory

	// StorageResolver selects the Flexprice-owned Storage backend for a Purpose
	// (invoice/export/import). Services must reach cloud object storage through
	// this — no direct AWS/GCP SDK imports outside internal/storage.
	StorageResolver storage.Resolver

	// Security
	EncryptionService security.EncryptionService

	// PubSubs
	WalletBalanceAlertPubSub types.WalletBalanceAlertPubSub
	WebhookPubSub            pubsub.PubSub

	// RevenueFacts is the revenue_facts service the invoice hooks call after
	// finalize/void. Implemented by internal/ee/service/revenue and injected
	// in main — a plain constructor here would import that package back into
	// this one. Nil skips the hooks (facts not wired in this deployment).
	RevenueFacts interfaces.RevenueService

	// SelfBilling provisions new tenants on Flexprice Cloud. Injected in main to avoid an import cycle; a no-op unless configured.
	SelfBilling interfaces.SelfBillingHook
}

// Common service params
func NewServiceParams(
	logger *logger.Logger,
	config *config.Configuration,
	db postgres.IClient,
	pdfGenerator pdf.Generator,
	tracingSvc *tracing.Service,
	inMemoryCache cache.InMemoryCache,
	redisCache cache.RedisCache,
	locker cache.Locker,
	authRepo auth.Repository,
	userRepo user.Repository,
	eventRepo events.Repository,
	processedEventRepo events.ProcessedEventRepository,
	rawEventRepo events.RawEventRepository,
	meterUsageRepo events.MeterUsageRepository,
	meterUsageCheckpointRepo meterusagecheckpoint.Repository,
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
	invoiceLineItemRepo invoice.LineItemRepository,
	featureRepo feature.Repository,
	creditGrantApplicationRepo creditgrantapplication.Repository,
	entitlementRepo entitlement.Repository,
	entitlementGrantRepo entitlementgrant.Repository,
	paymentRepo payment.Repository,
	refundRepo refund.Repository,
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
	fxRateRepo fxrate.Repository,
	hostedInvoiceTokenRepo hostedinvoicetoken.Repository,
	couponRepo coupon.Repository,
	couponAssociationRepo coupon_association.Repository,
	couponApplicationRepo coupon_application.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	storageResolver storage.Resolver,
	client httpclient.Client,
	addonRepo addon.Repository,
	addonAssociationRepo addonassociation.Repository,
	connectionRepo connection.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	settingsRepo settings.Repository,
	alertLogsRepo alertlogs.Repository,
	alertRepo alert.Repository,
	groupRepo group.Repository,
	scheduledTaskRepo scheduledtask.Repository,
	prorationCalculator proration.Calculator,
	integrationFactory *integration.Factory,
	walletBalanceAlertPubSub types.WalletBalanceAlertPubSub,
	webhookPubSub pubsub.PubSub,
	planPriceSyncRepo planpricesync.Repository,
	addonPriceSyncRepo addonpricesync.Repository,
	workflowExecutionRepo workflowexecution.Repository,
	checkoutSessionRepo domainCheckout.Repository,
	usageRecordRepo usagerecord.Repository,
	encryptionService security.EncryptionService,
	analyticsViewRepo domainAnalytics.Repository,
	reportsRepo domainAnalytics.ReportsRepository,
	revenueFactRepo revenuefact.Repository,
	activityRepo domainActivity.Repository,
	selfBillingClickHouseRepo domainsb.ClickHouseRepository,
	selfBillingPostgresRepo domainsb.PostgresRepository,
	commitmentRepo commitment.Repository,
	productRepo productRepo.Repository,
	contractService contractsvc.ContractService,
	pobService pobsvc.PerformanceObligationService,
	journalRepo journalRepo.Repository,
	journalService journalsvc.JournalService,
	eventDeletionRequestRepo events.DeletionRequestRepository,
	eventDeletionLogRepo events.DeletedEventsLogRepository,
) ServiceParams {
	return ServiceParams{
		Logger:                       logger,
		Config:                       config,
		DB:                           db,
		PDFGenerator:                 pdfGenerator,
		TracingSvc:                   tracingSvc,
		InMemoryCache:                inMemoryCache,
		RedisCache:                   redisCache,
		Locker:                       locker,
		AuthRepo:                     authRepo,
		UserRepo:                     userRepo,
		EventRepo:                    eventRepo,
		ProcessedEventRepo:           processedEventRepo,
		RawEventRepo:                 rawEventRepo,
		MeterUsageRepo:               meterUsageRepo,
		MeterUsageCheckpointRepo:     meterUsageCheckpointRepo,
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
		InvoiceLineItemRepo:          invoiceLineItemRepo,
		FeatureRepo:                  featureRepo,
		EntitlementRepo:              entitlementRepo,
		EntitlementGrantRepo:         entitlementGrantRepo,
		PaymentRepo:                  paymentRepo,
		RefundRepo:                   refundRepo,
		SecretRepo:                   secretRepo,
		EnvironmentRepo:              environmentRepo,
		CreditGrantRepo:              creditGrantRepo,
		CreditGrantApplicationRepo:   creditGrantApplicationRepo,
		TaskRepo:                     taskRepo,
		CostSheetRepo:                costSheetRepo,
		CreditNoteRepo:               creditNoteRepo,
		CreditNoteLineItemRepo:       creditNoteLineItemRepo,
		TaxRateRepo:                  taxRateRepo,
		FXRateRepo:                   fxRateRepo,
		HostedInvoiceTokenRepo:       hostedInvoiceTokenRepo,
		TaxAssociationRepo:           taxConfigRepo,
		TaxAppliedRepo:               taxAppliedRepo,
		EventPublisher:               eventPublisher,
		WebhookPublisher:             webhookPublisher,
		StorageResolver:              storageResolver,
		Client:                       client,
		CouponRepo:                   couponRepo,
		CouponAssociationRepo:        couponAssociationRepo,
		CouponApplicationRepo:        couponApplicationRepo,
		AddonRepo:                    addonRepo,
		AddonAssociationRepo:         addonAssociationRepo,
		ConnectionRepo:               connectionRepo,
		EntityIntegrationMappingRepo: entityIntegrationMappingRepo,
		UsageRecordRepo:              usageRecordRepo,
		SettingsRepo:                 settingsRepo,
		AlertLogsRepo:                alertLogsRepo,
		AlertRepo:                    alertRepo,
		GroupRepo:                    groupRepo,
		ScheduledTaskRepo:            scheduledTaskRepo,
		ProrationCalculator:          prorationCalculator,
		IntegrationFactory:           integrationFactory,
		EncryptionService:            encryptionService,
		WalletBalanceAlertPubSub:     walletBalanceAlertPubSub,
		WebhookPubSub:                webhookPubSub,
		PlanPriceSyncRepo:            planPriceSyncRepo,
		AddonPriceSyncRepo:           addonPriceSyncRepo,
		WorkflowExecutionRepo:        workflowExecutionRepo,
		CheckoutSessionRepo:          checkoutSessionRepo,
		AnalyticsViewRepo:            analyticsViewRepo,
		ReportsRepo:                  reportsRepo,
		RevenueFactRepo:              revenueFactRepo,
		ActivityRepo:                 activityRepo,
		SelfBillingClickHouseRepo:    selfBillingClickHouseRepo,
		SelfBillingPostgresRepo:      selfBillingPostgresRepo,
		CommitmentRepo:               commitmentRepo,
		ProductRepo:                  productRepo,
		ContractService:              contractService,
		POBService:                   pobService,
		JournalRepo:                  journalRepo,
		JournalService:               journalService,
		EventDeletionRequestRepo:     eventDeletionRequestRepo,
		EventDeletionLogRepo:         eventDeletionLogRepo,
	}
}
