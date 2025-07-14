package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/custompricingunit"
	"github.com/flexprice/flexprice/internal/cache"
	domainCPU "github.com/flexprice/flexprice/internal/domain/custompricingunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type customPricingUnitRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

// NewCustomPricingUnitRepository creates a new instance of customPricingUnitRepository
func NewCustomPricingUnitRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCPU.Repository {
	return &customPricingUnitRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

func (r *customPricingUnitRepository) Create(ctx context.Context, unit *domainCPU.CustomPricingUnit) error {
	client := r.client.Querier(ctx)

	_, err := client.CustomPricingUnit.Create().
		SetID(unit.ID).
		SetName(unit.Name).
		SetCode(unit.Code).
		SetSymbol(unit.Symbol).
		SetBaseCurrency(unit.BaseCurrency).
		SetConversionRate(unit.ConversionRate).
		SetPrecision(unit.Precision).
		SetStatus(string(types.StatusPublished)). // Set default status to published
		SetTenantID(unit.TenantID).
		SetEnvironmentID(unit.EnvironmentID).
		SetCreatedAt(unit.CreatedAt).
		SetUpdatedAt(unit.UpdatedAt).
		Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A custom pricing unit with this code already exists").
				WithReportableDetails(map[string]any{
					"code": unit.Code,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// GetByID retrieves a custom pricing unit by ID
func (r *customPricingUnitRepository) GetByID(ctx context.Context, id string) (*domainCPU.CustomPricingUnit, error) {
	client := r.client.Querier(ctx)

	unit, err := client.CustomPricingUnit.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return domainCPU.FromEnt(unit), nil
}

func (r *customPricingUnitRepository) List(ctx context.Context, filter *domainCPU.CustomPricingUnitFilter) ([]*domainCPU.CustomPricingUnit, error) {
	client := r.client.Querier(ctx)

	query := client.CustomPricingUnit.Query()

	if filter.Status != "" {
		query = query.Where(custompricingunit.StatusEQ(string(filter.Status)))
	}

	if filter.TenantID != "" {
		query = query.Where(custompricingunit.TenantIDEQ(filter.TenantID))
	}

	if filter.EnvironmentID != "" {
		query = query.Where(custompricingunit.EnvironmentIDEQ(filter.EnvironmentID))
	}

	units, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list custom pricing units").
			Mark(ierr.ErrDatabase)
	}

	return domainCPU.FromEntList(units), nil
}

func (r *customPricingUnitRepository) Count(ctx context.Context, filter *domainCPU.CustomPricingUnitFilter) (int, error) {
	client := r.client.Querier(ctx)

	query := client.CustomPricingUnit.Query()

	if filter.Status != "" {
		query = query.Where(custompricingunit.StatusEQ(string(filter.Status)))
	}

	if filter.TenantID != "" {
		query = query.Where(custompricingunit.TenantIDEQ(filter.TenantID))
	}

	if filter.EnvironmentID != "" {
		query = query.Where(custompricingunit.EnvironmentIDEQ(filter.EnvironmentID))
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count custom pricing units").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *customPricingUnitRepository) Update(ctx context.Context, unit *domainCPU.CustomPricingUnit) error {
	client := r.client.Querier(ctx)

	_, err := client.CustomPricingUnit.UpdateOneID(unit.ID).
		SetName(unit.Name).
		SetSymbol(unit.Symbol).
		SetPrecision(unit.Precision).
		SetStatus(string(unit.Status)).
		SetUpdatedAt(unit.UpdatedAt).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *customPricingUnitRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	err := client.CustomPricingUnit.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *customPricingUnitRepository) GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*domainCPU.CustomPricingUnit, error) {
	client := r.client.Querier(ctx)

	q := client.CustomPricingUnit.Query().
		Where(
			custompricingunit.CodeEQ(code),
			custompricingunit.TenantIDEQ(tenantID),
			custompricingunit.EnvironmentIDEQ(environmentID),
		)
	if status != "" {
		q = q.Where(custompricingunit.StatusEQ(status))
	}
	unit, err := q.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get custom pricing unit").
			Mark(ierr.ErrDatabase)
	}
	return domainCPU.FromEnt(unit), nil
}

func (r *customPricingUnitRepository) GetConversionRate(ctx context.Context, code, tenantID, environmentID string) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	return unit.ConversionRate, nil
}

func (r *customPricingUnitRepository) GetSymbol(ctx context.Context, code, tenantID, environmentID string) (string, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return "", err
	}
	return unit.Symbol, nil
}

func (r *customPricingUnitRepository) ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, customAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	// amount in fiat currency = amount in custom currency * conversion_rate
	return customAmount.Mul(unit.ConversionRate), nil
}

func (r *customPricingUnitRepository) ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	// amount in custom currency = amount in fiat currency / conversion_rate
	return fiatAmount.Div(unit.ConversionRate), nil
}
