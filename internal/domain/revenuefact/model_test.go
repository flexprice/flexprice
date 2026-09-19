package revenuefact

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestRevenueFact_Fields(t *testing.T) {
	now := time.Now().UTC()
	priceID := "price_123"

	f := &RevenueFact{
		ID:                "rf_1",
		TenantID:          "tenant_1",
		EnvironmentID:     "env_1",
		CustomerID:        "cust_1",
		SubscriptionID:    "sub_1",
		PriceID:           &priceID,
		RevenueSource:     types.RevenueSourceUsage,
		PeriodStart:       now,
		PeriodEnd:         now,
		Day:               now,
		NetAmount:         decimal.NewFromInt(100),
		DecompositionMode: types.Marginal,
		Currency:          "usd",
		Status:            types.FactProvisional,
		ComputedAt:        now,
		Version:           1,
	}

	assert.NoError(t, f.RevenueSource.Validate())
	assert.NoError(t, f.DecompositionMode.Validate())
	assert.NoError(t, f.Status.Validate())
	assert.Equal(t, "price_123", *f.PriceID)

	assert.Equal(t, "tenant_1", f.TenantID)
	assert.Equal(t, types.FactProvisional, f.Status)
	assert.Equal(t, now, f.ComputedAt)
	assert.Equal(t, int64(1), f.Version)
}
