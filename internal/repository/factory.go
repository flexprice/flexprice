package repository

import (
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	clickhouseRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	postgresRepo "github.com/flexprice/flexprice/internal/repository/postgres"
	"go.uber.org/fx"
)

// RepositoryParams holds common dependencies for repositories
type RepositoryParams struct {
	fx.In

	Logger       *logger.Logger
	DB           *postgres.DB
	EntClient    postgres.IClient
	ClickHouseDB *clickhouse.ClickHouseStore
}

func NewEventRepository(p RepositoryParams) events.Repository {
	return clickhouseRepo.NewEventRepository(p.ClickHouseDB, p.Logger)
}

func NewMeterRepository(p RepositoryParams) meter.Repository {
	return entRepo.NewMeterRepository(p.EntClient, p.Logger)
}

func NewUserRepository(p RepositoryParams) user.Repository {
	return postgresRepo.NewUserRepository(p.DB, p.Logger)
}

func NewAuthRepository(p RepositoryParams) auth.Repository {
	return postgresRepo.NewAuthRepository(p.DB, p.Logger)
}

func NewPriceRepository(p RepositoryParams) price.Repository {
	return entRepo.NewPriceRepository(p.EntClient, p.Logger)
}

func NewCustomerRepository(p RepositoryParams) customer.Repository {
	return entRepo.NewCustomerRepository(p.EntClient, p.Logger)
}

func NewPlanRepository(p RepositoryParams) plan.Repository {
	return entRepo.NewPlanRepository(p.EntClient, p.Logger)
}

func NewSubscriptionRepository(p RepositoryParams) subscription.Repository {
	return entRepo.NewSubscriptionRepository(p.EntClient, p.Logger)
}

func NewWalletRepository(p RepositoryParams) wallet.Repository {
	return entRepo.NewWalletRepository(p.EntClient, p.Logger)
}

func NewTenantRepository(p RepositoryParams) tenant.Repository {
	return postgresRepo.NewTenantRepository(p.DB, p.Logger)
}

func NewEnvironmentRepository(p RepositoryParams) environment.Repository {
	return postgresRepo.NewEnvironmentRepository(p.DB, p.Logger)
}

func NewInvoiceRepository(p RepositoryParams) invoice.Repository {
	return entRepo.NewInvoiceRepository(p.EntClient, p.Logger)
}

func NewFeatureRepository(p RepositoryParams) feature.Repository {
	return entRepo.NewFeatureRepository(p.EntClient, p.Logger)
}

func NewEntitlementRepository(p RepositoryParams) entitlement.Repository {
	return entRepo.NewEntitlementRepository(p.EntClient, p.Logger)
}
