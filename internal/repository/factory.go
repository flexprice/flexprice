package repository

import (
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/costsheet"
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
	"github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	domainsystemevent "github.com/flexprice/flexprice/internal/domain/systemevent"
	"github.com/flexprice/flexprice/internal/domain/task"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxapplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	clickhouseRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"go.uber.org/fx"
)

// RepositoryParams holds common dependencies for repositories
type RepositoryParams struct {
	fx.In

	Logger        *logger.Logger
	EntClient     postgres.IClient
	ClickHouseDB  *clickhouse.ClickHouseStore
	InMemoryCache cache.InMemoryCache
	RedisCache    cache.RedisCache
}

func NewEventRepository(p RepositoryParams) events.Repository {
	return clickhouseRepo.NewEventRepository(p.ClickHouseDB, p.Logger)
}

func NewProcessedEventRepository(p RepositoryParams) events.ProcessedEventRepository {
	return clickhouseRepo.NewProcessedEventRepository(p.ClickHouseDB, p.Logger)
}

func NewFeatureUsageRepository(p RepositoryParams) events.FeatureUsageRepository {
	return clickhouseRepo.NewFeatureUsageRepository(p.ClickHouseDB, p.Logger)
}

func NewRawEventRepository(p RepositoryParams) events.RawEventRepository {
	return clickhouseRepo.NewRawEventRepository(p.ClickHouseDB, p.Logger)
}

func NewMeterRepository(p RepositoryParams) meter.Repository {
	return entRepo.NewMeterRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewUserRepository(p RepositoryParams) user.Repository {
	return entRepo.NewUserRepository(p.EntClient, p.Logger)
}

func NewAuthRepository(p RepositoryParams) auth.Repository {
	return entRepo.NewAuthRepository(p.EntClient, p.Logger)
}

func NewPriceRepository(p RepositoryParams) price.Repository {
	return entRepo.NewPriceRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCustomerRepository(p RepositoryParams) customer.Repository {
	return entRepo.NewCustomerRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewPlanRepository(p RepositoryParams) plan.Repository {
	return entRepo.NewPlanRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewPlanPriceSyncRepository(p RepositoryParams) planpricesync.Repository {
	return entRepo.NewPlanPriceSyncRepository(p.EntClient, p.Logger)
}

func NewSubscriptionRepository(p RepositoryParams) subscription.Repository {
	return entRepo.NewSubscriptionRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewSubscriptionLineItemRepository(p RepositoryParams) subscription.LineItemRepository {
	return entRepo.NewSubscriptionLineItemRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewSubscriptionPhaseRepository(p RepositoryParams) subscription.SubscriptionPhaseRepository {
	return entRepo.NewSubscriptionPhaseRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewSubscriptionScheduleRepository(p RepositoryParams) subscription.SubscriptionScheduleRepository {
	return entRepo.NewSubscriptionScheduleRepository(p.EntClient, p.Logger)
}

func NewWalletRepository(p RepositoryParams) wallet.Repository {
	return entRepo.NewWalletRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewTenantRepository(p RepositoryParams) tenant.Repository {
	return entRepo.NewTenantRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewEnvironmentRepository(p RepositoryParams) environment.Repository {
	return entRepo.NewEnvironmentRepository(p.EntClient, p.Logger)
}

func NewInvoiceRepository(p RepositoryParams) invoice.Repository {
	return entRepo.NewInvoiceRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewInvoiceLineItemRepository(p RepositoryParams) invoice.LineItemRepository {
	return entRepo.NewInvoiceLineItemRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewFeatureRepository(p RepositoryParams) feature.Repository {
	return entRepo.NewFeatureRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewEntitlementRepository(p RepositoryParams) entitlement.Repository {
	return entRepo.NewEntitlementRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewPaymentRepository(p RepositoryParams) payment.Repository {
	return entRepo.NewPaymentRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewPaymentMethodRepository(p RepositoryParams) paymentmethod.Repository {
	return entRepo.NewPaymentMethodRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewTaskRepository(p RepositoryParams) task.Repository {
	return entRepo.NewTaskRepository(p.EntClient, p.Logger)
}

func NewSecretRepository(p RepositoryParams) secret.Repository {
	return entRepo.NewSecretRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCreditGrantRepository(p RepositoryParams) creditgrant.Repository {
	return entRepo.NewCreditGrantRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCostsheetRepository(p RepositoryParams) costsheet.Repository {
	return entRepo.NewCostsheetRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCreditGrantApplicationRepository(p RepositoryParams) creditgrantapplication.Repository {
	return entRepo.NewCreditGrantApplicationRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCouponRepository(p RepositoryParams) coupon.Repository {
	return entRepo.NewCouponRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCouponAssociationRepository(p RepositoryParams) coupon_association.Repository {
	return entRepo.NewCouponAssociationRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCouponApplicationRepository(p RepositoryParams) coupon_application.Repository {
	return entRepo.NewCouponApplicationRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCreditNoteRepository(p RepositoryParams) creditnote.Repository {
	return entRepo.NewCreditNoteRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewCreditNoteLineItemRepository(p RepositoryParams) creditnote.CreditNoteLineItemRepository {
	return entRepo.NewCreditNoteLineItemRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewConnectionRepository(p RepositoryParams) connection.Repository {
	return entRepo.NewConnectionRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewEntityIntegrationMappingRepository(p RepositoryParams) entityintegrationmapping.Repository {
	return entRepo.NewEntityIntegrationMappingRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewTaxRateRepository(p RepositoryParams) taxrate.Repository {
	return entRepo.NewTaxRateRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewTaxAssociationRepository(p RepositoryParams) taxassociation.Repository {
	return entRepo.NewTaxAssociationRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewTaxAppliedRepository(p RepositoryParams) taxapplied.Repository {
	return entRepo.NewTaxAppliedRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewPriceUnitRepository(p RepositoryParams) priceunit.Repository {
	return entRepo.NewPriceUnitRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewAddonRepository(p RepositoryParams) addon.Repository {
	return entRepo.NewAddonRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewAddonAssociationRepository(p RepositoryParams) addonassociation.Repository {
	return entRepo.NewAddonAssociationRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewSettingsRepository(p RepositoryParams) settings.Repository {
	return entRepo.NewSettingsRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewAlertLogsRepository(p RepositoryParams) alertlogs.Repository {
	return entRepo.NewAlertLogsRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewSystemEventRepository(p RepositoryParams) *entRepo.SystemEventRepository {
	return entRepo.NewSystemEventRepository(p.EntClient)
}

func NewSystemEventDomainRepository(repo *entRepo.SystemEventRepository) domainsystemevent.Repository {
	return repo
}

func NewGroupRepository(p RepositoryParams) group.Repository {
	return entRepo.NewGroupRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewScheduledTaskRepository(p RepositoryParams) scheduledtask.Repository {
	return entRepo.NewScheduledTaskRepository(p.EntClient, p.Logger)
}

func NewCostSheetUsageRepository(p RepositoryParams) events.CostSheetUsageRepository {
	return clickhouseRepo.NewCostSheetUsageRepository(p.ClickHouseDB, p.Logger)
}

func NewMeterUsageRepository(p RepositoryParams) events.MeterUsageRepository {
	return clickhouseRepo.NewMeterUsageRepository(p.ClickHouseDB, p.Logger)
}

func NewUsageBenchmarkRepository(p RepositoryParams) events.UsageBenchmarkRepository {
	return clickhouseRepo.NewUsageBenchmarkRepository(p.ClickHouseDB, p.Logger)
}

func NewAnalyticsBenchmarkRepository(p RepositoryParams) events.AnalyticsBenchmarkRepository {
	return clickhouseRepo.NewAnalyticsBenchmarkRepository(p.ClickHouseDB, p.Logger)
}

func NewWorkflowExecutionRepository(p RepositoryParams) workflowexecution.Repository {
	return entRepo.NewWorkflowExecutionRepository(p.EntClient, p.Logger, p.InMemoryCache)
}

func NewIncomingWebhookEventRepository(p RepositoryParams) incomingwebhookevent.Repository {
	return entRepo.NewIncomingWebhookEventRepository(p.EntClient)
}
