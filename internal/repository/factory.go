package repository

import (
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
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
	EntClient    *postgres.Client
	ClickHouseDB *clickhouse.ClickHouseStore
}

func NewEventRepository(p RepositoryParams) events.Repository {
	return clickhouseRepo.NewEventRepository(p.ClickHouseDB, p.Logger)
}

func NewMeterRepository(p RepositoryParams) meter.Repository {
	return postgresRepo.NewMeterRepository(p.DB, p.Logger)
}

func NewUserRepository(p RepositoryParams) user.Repository {
	return postgresRepo.NewUserRepository(p.DB, p.Logger)
}

func NewAuthRepository(p RepositoryParams) auth.Repository {
	return postgresRepo.NewAuthRepository(p.DB, p.Logger)
}

func NewPriceRepository(p RepositoryParams) price.Repository {
	return postgresRepo.NewPriceRepository(p.DB, p.Logger)
}

func NewCustomerRepository(p RepositoryParams) customer.Repository {
	return postgresRepo.NewCustomerRepository(p.DB, p.Logger)
}

func NewPlanRepository(p RepositoryParams) plan.Repository {
	return postgresRepo.NewPlanRepository(p.DB, p.Logger)
}

func NewSubscriptionRepository(p RepositoryParams) subscription.Repository {
	// Use Ent implementation if client is available
	if p.EntClient != nil {
		return entRepo.NewSubscriptionRepository(p.EntClient, p.Logger)
	}
	// Fallback to PostgreSQL implementation
	return postgresRepo.NewSubscriptionRepository(p.DB, p.Logger)
}

func NewWalletRepository(p RepositoryParams) wallet.Repository {
	// Use Ent implementation if client is available
	if p.EntClient != nil {
		return entRepo.NewWalletRepository(p.EntClient, p.Logger)
	}
	// Fallback to PostgreSQL implementation
	return postgresRepo.NewWalletRepository(p.DB, p.Logger)
}

func NewTenantRepository(p RepositoryParams) tenant.Repository {
	return postgresRepo.NewTenantRepository(p.DB, p.Logger)
}

func NewEnvironmentRepository(p RepositoryParams) environment.Repository {
	return postgresRepo.NewEnvironmentRepository(p.DB, p.Logger)
}
