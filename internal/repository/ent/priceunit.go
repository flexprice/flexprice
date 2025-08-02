package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/priceunit"
	"github.com/flexprice/flexprice/internal/cache"
	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type priceUnitRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

// NewPriceUnitRepository creates a new instance of priceUnitRepository
func NewPriceUnitRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainPriceUnit.Repository {
	return &priceUnitRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

func (r *priceUnitRepository) Create(ctx context.Context, unit *domainPriceUnit.PriceUnit) error {
	client := r.client.Querier(ctx)

	// Get tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Validate tenant isolation
	if unit.TenantID != tenantID {
		return ierr.WithError(errors.New("price unit tenant ID does not match context tenant ID")).
			WithHint("Cannot create price unit for different tenant").
			WithReportableDetails(map[string]any{
				"expected_tenant_id": tenantID,
				"actual_tenant_id":   unit.TenantID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if unit.EnvironmentID == "" {
		unit.EnvironmentID = environmentID
	}

	// Validate environment isolation
	if unit.EnvironmentID != environmentID {
		return ierr.WithError(errors.New("price unit environment ID does not match context environment ID")).
			WithHint("Cannot create price unit for different environment").
			WithReportableDetails(map[string]any{
				"expected_environment_id": environmentID,
				"actual_environment_id":   unit.EnvironmentID,
			}).
			Mark(ierr.ErrValidation)
	}

	_, err := client.PriceUnit.Create().
		SetID(unit.ID).
		SetName(unit.Name).
		SetCode(unit.Code).
		SetSymbol(unit.Symbol).
		SetBaseCurrency(unit.BaseCurrency).
		SetConversionRate(unit.ConversionRate).
		SetPrecision(unit.Precision).
		SetStatus(string(types.StatusPublished)). // Set default status to published
		SetTenantID(tenantID).                    // Always use context tenant ID
		SetEnvironmentID(environmentID).          // Always use context environment ID
		SetCreatedAt(unit.CreatedAt).
		SetUpdatedAt(unit.UpdatedAt).
		Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A pricing unit with this code already exists").
				WithReportableDetails(map[string]any{
					"code": unit.Code,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// GetByID retrieves a pricing unit by ID
func (r *priceUnitRepository) GetByID(ctx context.Context, id string) (*domainPriceUnit.PriceUnit, error) {
	client := r.client.Querier(ctx)

	unit, err := client.PriceUnit.Query().
		Where(
			priceunit.ID(id),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return domainPriceUnit.FromEnt(unit), nil
}

func (r *priceUnitRepository) List(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) ([]*domainPriceUnit.PriceUnit, error) {
	client := r.client.Querier(ctx)

	query := client.PriceUnit.Query().
		Where(
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	if filter.Status != "" {
		query = query.Where(priceunit.StatusEQ(string(filter.Status)))
	} else {
		// By default, exclude archived items
		query = query.Where(priceunit.StatusNEQ(string(types.StatusArchived)))
	}

	// Apply pagination
	if filter.QueryFilter != nil {
		if filter.QueryFilter.Offset != nil {
			query = query.Offset(*filter.QueryFilter.Offset)
		}
		if filter.QueryFilter.Limit != nil {
			query = query.Limit(*filter.QueryFilter.Limit)
		}
		// Apply sorting
		if filter.QueryFilter.Sort != nil && filter.QueryFilter.Order != nil {
			switch *filter.QueryFilter.Sort {
			case "created_at":
				if *filter.QueryFilter.Order == "desc" {
					query = query.Order(ent.Desc(priceunit.FieldCreatedAt))
				} else {
					query = query.Order(ent.Asc(priceunit.FieldCreatedAt))
				}
			case "updated_at":
				if *filter.QueryFilter.Order == "desc" {
					query = query.Order(ent.Desc(priceunit.FieldUpdatedAt))
				} else {
					query = query.Order(ent.Asc(priceunit.FieldUpdatedAt))
				}
			case "name":
				if *filter.QueryFilter.Order == "desc" {
					query = query.Order(ent.Desc(priceunit.FieldName))
				} else {
					query = query.Order(ent.Asc(priceunit.FieldName))
				}
			case "code":
				if *filter.QueryFilter.Order == "desc" {
					query = query.Order(ent.Desc(priceunit.FieldCode))
				} else {
					query = query.Order(ent.Asc(priceunit.FieldCode))
				}
			default:
				// Default sorting by created_at desc
				query = query.Order(ent.Desc(priceunit.FieldCreatedAt))
			}
		} else {
			// Default sorting by created_at desc
			query = query.Order(ent.Desc(priceunit.FieldCreatedAt))
		}
	} else {
		// Default sorting by created_at desc
		query = query.Order(ent.Desc(priceunit.FieldCreatedAt))
	}

	units, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list pricing units").
			Mark(ierr.ErrDatabase)
	}

	return domainPriceUnit.FromEntList(units), nil
}

func (r *priceUnitRepository) Count(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) (int, error) {
	client := r.client.Querier(ctx)

	query := client.PriceUnit.Query().
		Where(
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	if filter.Status != "" {
		query = query.Where(priceunit.StatusEQ(string(filter.Status)))
	} else {
		// By default, exclude archived items
		query = query.Where(priceunit.StatusNEQ(string(types.StatusArchived)))
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count pricing units").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *priceUnitRepository) Update(ctx context.Context, unit *domainPriceUnit.PriceUnit) error {
	client := r.client.Querier(ctx)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(unit.ID),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(unit.Name).
		SetSymbol(unit.Symbol).
		SetPrecision(unit.Precision).
		SetConversionRate(unit.ConversionRate).
		SetStatus(string(unit.Status)).
		SetUpdatedAt(unit.UpdatedAt).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *priceUnitRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(id),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *priceUnitRepository) GetByCode(ctx context.Context, code string, tenantID string, environmentID string, status string) (*domainPriceUnit.PriceUnit, error) {
	client := r.client.Querier(ctx)

	// Validate tenant isolation
	if tenantID != types.GetTenantID(ctx) {
		return nil, ierr.WithError(errors.New("tenant ID does not match context tenant ID")).
			WithHint("Cannot access price unit from different tenant").
			WithReportableDetails(map[string]any{
				"expected_tenant_id": types.GetTenantID(ctx),
				"actual_tenant_id":   tenantID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate environment isolation
	if environmentID != types.GetEnvironmentID(ctx) {
		return nil, ierr.WithError(errors.New("environment ID does not match context environment ID")).
			WithHint("Cannot access price unit from different environment").
			WithReportableDetails(map[string]any{
				"expected_environment_id": types.GetEnvironmentID(ctx),
				"actual_environment_id":   environmentID,
			}).
			Mark(ierr.ErrValidation)
	}

	q := client.PriceUnit.Query().
		Where(
			priceunit.CodeEQ(code),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		)
	if status != "" {
		q = q.Where(priceunit.StatusEQ(status))
	}
	unit, err := q.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get pricing unit").
			Mark(ierr.ErrDatabase)
	}
	return domainPriceUnit.FromEnt(unit), nil
}

func (r *priceUnitRepository) ConvertToBaseCurrency(ctx context.Context, code string, tenantID string, environmentID string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	// amount in fiat currency = amount in custom currency * conversion_rate
	return priceUnitAmount.Mul(unit.ConversionRate), nil
}

func (r *priceUnitRepository) ConvertToPriceUnit(ctx context.Context, code string, tenantID string, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	// amount in custom currency = amount in fiat currency / conversion_rate
	return fiatAmount.Div(unit.ConversionRate), nil
}
