package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPriceGetByGroupIDs_ScopedToTenantAndEnvironment(t *testing.T) {
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	repo := NewPriceRepository(client, log, noopRedisCache{})

	groupID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_GROUP)
	scope := func(tenant, env string) context.Context {
		ctx := types.SetTenantID(context.Background(), tenant)
		ctx = types.SetEnvironmentID(ctx, env)
		return context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	}
	create := func(ctx context.Context) *domainPrice.Price {
		p := &domainPrice.Price{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:             decimal.NewFromInt(1),
			Currency:           "usd",
			PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           "plan_" + types.GenerateUUID(),
			GroupID:            groupID,
			TransformQuantity:  domainPrice.JSONBTransformQuantity{DivideBy: 1},
			EnvironmentID:      types.GetEnvironmentID(ctx),
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		p.DisplayAmount = p.GetDisplayAmount()
		p.Status = types.StatusPublished
		require.NoError(t, repo.Create(ctx, p))
		t.Cleanup(func() {
			_ = client.Writer(context.Background()).Price.DeleteOneID(p.ID).Exec(context.Background())
		})
		return p
	}

	ctxA := scope("tenant_fle1765_a", "env_fle1765_a")
	mine := create(ctxA)
	create(scope("tenant_fle1765_b", "env_fle1765_a"))
	create(scope("tenant_fle1765_a", "env_fle1765_b"))

	got, err := repo.GetByGroupIDs(ctxA, []string{groupID})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, mine.ID, got[0].ID)
}
