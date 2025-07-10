package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/custompricingunit"
	domainCPU "github.com/flexprice/flexprice/internal/domain/custompricingunit"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// customPricingUnitRepository handles business logic for custom pricing units
type CustomPricingUnitRepository struct {
	client *ent.Client
}

// NewCustomPricingUnitRepository creates a new instance of customPricingUnitRepository
func NewCustomPricingUnitRepository(client *ent.Client) domainCPU.Repository {
	return &CustomPricingUnitRepository{
		client: client,
	}
}

func (r *CustomPricingUnitRepository) GetByID(ctx context.Context, id string) (*ent.CustomPricingUnit, error) {
	return r.client.CustomPricingUnit.Get(ctx, id)
}

func (r *CustomPricingUnitRepository) GetByCode(ctx context.Context, code, tenantID, environmentID, status string) (*ent.CustomPricingUnit, error) {
	q := r.client.CustomPricingUnit.Query().
		Where(
			custompricingunit.CodeEQ(code),
			custompricingunit.TenantIDEQ(tenantID),
			custompricingunit.EnvironmentIDEQ(environmentID),
		)
	if status != "" {
		q = q.Where(custompricingunit.StatusEQ(status))
	}
	return q.Only(ctx)
}

func (r *CustomPricingUnitRepository) GetConversionRate(ctx context.Context, code, tenantID, environmentID string) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	return unit.ConversionRate, nil
}

func (r *CustomPricingUnitRepository) GetSymbol(ctx context.Context, code, tenantID, environmentID string) (string, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return "", err
	}
	return unit.Symbol, nil
}

func (r *CustomPricingUnitRepository) ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, customAmount decimal.Decimal) (decimal.Decimal, error) {
	unit, err := r.GetByCode(ctx, code, tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}
	return customAmount.Mul(unit.ConversionRate), nil
}
