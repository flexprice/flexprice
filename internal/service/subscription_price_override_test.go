package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSubscriptionWithPriceOverrides(t *testing.T) {
	// This test demonstrates the price override functionality
	// Note: This is a unit test template - actual implementation would need proper test setup

	t.Run("should create subscription with price overrides", func(t *testing.T) {
		// Arrange - Mock setup would go here
		// Sample override request
		overrideAmount := decimal.NewFromFloat(15.99)
		overrideQuantity := decimal.NewFromInt(2)

		req := dto.CreateSubscriptionRequest{
			CustomerID:         "test_customer_123",
			PlanID:             "test_plan_456",
			Currency:           "USD",
			StartDate:          lo.ToPtr(time.Now()),
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
			OverrideLineItems: []dto.OverrideLineItemRequest{
				{
					PriceID:  "test_price_789",
					Amount:   &overrideAmount,
					Quantity: &overrideQuantity,
				},
			},
		}

		// Validate the override request structure
		assert.NotEmpty(t, req.OverrideLineItems)
		assert.Equal(t, "test_price_789", req.OverrideLineItems[0].PriceID)
		assert.Equal(t, overrideAmount, *req.OverrideLineItems[0].Amount)
		assert.Equal(t, overrideQuantity, *req.OverrideLineItems[0].Quantity)

		// Validate override line item validation
		err := req.OverrideLineItems[0].Validate()
		require.NoError(t, err)

		// Test validation logic
		err = req.Validate()
		require.NoError(t, err)
	})

	t.Run("should reject invalid override line items", func(t *testing.T) {
		// Test empty price ID
		override := dto.OverrideLineItemRequest{
			PriceID: "",
			Amount:  &decimal.Decimal{},
		}
		err := override.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "price_id is required")

		// Test negative amount
		negativeAmount := decimal.NewFromFloat(-10.00)
		override = dto.OverrideLineItemRequest{
			PriceID: "test_price",
			Amount:  &negativeAmount,
		}
		err = override.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be non-negative")

		// Test no override fields provided
		override = dto.OverrideLineItemRequest{
			PriceID: "test_price",
		}
		err = override.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one override field")
	})

	t.Run("should reject duplicate price IDs in overrides", func(t *testing.T) {
		amount1 := decimal.NewFromFloat(10.00)
		amount2 := decimal.NewFromFloat(20.00)

		req := dto.CreateSubscriptionRequest{
			CustomerID:         "test_customer",
			PlanID:             "test_plan",
			Currency:           "USD",
			StartDate:          lo.ToPtr(time.Now()),
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
			OverrideLineItems: []dto.OverrideLineItemRequest{
				{
					PriceID: "duplicate_price",
					Amount:  &amount1,
				},
				{
					PriceID: "duplicate_price", // Duplicate!
					Amount:  &amount2,
				},
			},
		}

		err := req.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate price_id in override line items")
	})
}

func TestPriceScopeFiltering(t *testing.T) {
	t.Run("should filter by price scope", func(t *testing.T) {
		// Test that PriceFilter correctly handles scope filtering
		filter := types.NewPriceFilter()

		// Test plan scope filtering
		filter = filter.WithEntityType(types.PRICE_ENTITY_TYPE_PLAN)
		assert.Equal(t, types.PRICE_ENTITY_TYPE_PLAN, *filter.EntityType)

		// Test subscription scope filtering
		filter = filter.WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION)
		assert.Equal(t, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, *filter.EntityType)

		// Test subscription ID filtering
		subscriptionID := "test_subscription_123"
		filter = filter.WithSubscriptionID(subscriptionID)
		assert.Equal(t, subscriptionID, *filter.SubscriptionID)

		// Test parent price ID filtering
		parentPriceID := "test_parent_price_456"
		filter = filter.WithParentPriceID(parentPriceID)
		assert.Equal(t, parentPriceID, *filter.ParentPriceID)

		// Test validation
		err := filter.Validate()
		assert.NoError(t, err)
	})
}
