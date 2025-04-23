package repository

import (
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/auth"
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
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	clickhouseRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"go.uber.org/fx"
)

// RepositoryParams holds common dependencies for repositories
type RepositoryParams struct {
	fx.In

	Logger       *logger.Logger
	EntClient    postgres.IClient
	ClickHouseDB *clickhouse.ClickHouseStore
	Cache        cache.Cache
}

func NewEventRepository(p RepositoryParams) events.Repository {
	return clickhouseRepo.NewEventRepository(p.ClickHouseDB, p.Logger)
}

func NewMeterRepository(p RepositoryParams) meter.Repository {
	return entRepo.NewMeterRepository(p.EntClient, p.Logger, p.Cache)
}

func NewUserRepository(p RepositoryParams) user.Repository {
	return entRepo.NewUserRepository(p.EntClient, p.Logger)
}

func NewAuthRepository(p RepositoryParams) auth.Repository {
	return entRepo.NewAuthRepository(p.EntClient, p.Logger)
}

func NewPriceRepository(p RepositoryParams) price.Repository {
	return entRepo.NewPriceRepository(p.EntClient, p.Logger, p.Cache)
}

func NewCustomerRepository(p RepositoryParams) customer.Repository {
	return entRepo.NewCustomerRepository(p.EntClient, p.Logger, p.Cache)
}

func NewPlanRepository(p RepositoryParams) plan.Repository {
	return entRepo.NewPlanRepository(p.EntClient, p.Logger, p.Cache)
}

func NewSubscriptionRepository(p RepositoryParams) subscription.Repository {
	return entRepo.NewSubscriptionRepository(p.EntClient, p.Logger, p.Cache)
}

func NewWalletRepository(p RepositoryParams) wallet.Repository {
	return entRepo.NewWalletRepository(p.EntClient, p.Logger, p.Cache)
}

func NewTenantRepository(p RepositoryParams) tenant.Repository {
	return entRepo.NewTenantRepository(p.EntClient, p.Logger, p.Cache)
}

func NewEnvironmentRepository(p RepositoryParams) environment.Repository {
	return entRepo.NewEnvironmentRepository(p.EntClient, p.Logger)
}

func NewInvoiceRepository(p RepositoryParams) invoice.Repository {
	return entRepo.NewInvoiceRepository(p.EntClient, p.Logger, p.Cache)
}

func NewFeatureRepository(p RepositoryParams) feature.Repository {
	return entRepo.NewFeatureRepository(p.EntClient, p.Logger, p.Cache)
}

func NewEntitlementRepository(p RepositoryParams) entitlement.Repository {
	return entRepo.NewEntitlementRepository(p.EntClient, p.Logger, p.Cache)
}

func NewPaymentRepository(p RepositoryParams) payment.Repository {
	return entRepo.NewPaymentRepository(p.EntClient, p.Logger, p.Cache)
}

func NewTaskRepository(p RepositoryParams) task.Repository {
	return entRepo.NewTaskRepository(p.EntClient, p.Logger)
}

func NewSecretRepository(p RepositoryParams) secret.Repository {
	return entRepo.NewSecretRepository(p.EntClient, p.Logger, p.Cache)
}
