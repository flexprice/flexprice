package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PriceSubscriptionScopeSuite is a regression suite for a cross-subscription
// price leak: PriceFilter.SubscriptionID was validated and set by
// GetPricesBySubscriptionID but never applied as a query predicate, so the
// method returned every subscription-scoped price in the tenant/environment
// instead of only the requested subscription's.
type PriceSubscriptionScopeSuite struct {
	testutil.BaseServiceTestSuite
	service PriceService
}

func TestPriceSubscriptionScope(t *testing.T) {
	suite.Run(t, new(PriceSubscriptionScopeSuite))
}

func (s *PriceSubscriptionScopeSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPriceService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *PriceSubscriptionScopeSuite) createSubscriptionPrice(id, subscriptionID string, amount int64) {
	p := &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromInt(amount),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           subscriptionID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
}

func (s *PriceSubscriptionScopeSuite) TestGetPricesBySubscriptionIDScopedToSubscription() {
	// Two subscriptions with their own override prices in the same
	// tenant/environment — the leak returned all of them for any subscription.
	s.createSubscriptionPrice("price_sub_a_1", "sub_a", 10)
	s.createSubscriptionPrice("price_sub_a_2", "sub_a", 20)
	s.createSubscriptionPrice("price_sub_b_1", "sub_b", 30)

	testCases := []struct {
		name           string
		subscriptionID string
		expectedIDs    []string
	}{
		{
			name:           "returns_only_prices_of_the_requested_subscription",
			subscriptionID: "sub_a",
			expectedIDs:    []string{"price_sub_a_1", "price_sub_a_2"},
		},
		{
			name:           "other_subscription_sees_only_its_own_price",
			subscriptionID: "sub_b",
			expectedIDs:    []string{"price_sub_b_1"},
		},
		{
			name:           "subscription_without_prices_gets_empty_result",
			subscriptionID: "sub_without_overrides",
			expectedIDs:    []string{},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetPricesBySubscriptionID(s.GetContext(), tc.subscriptionID)
			s.NoError(err)

			gotIDs := lo.Map(resp.Items, func(p *dto.PriceResponse, _ int) string { return p.ID })
			s.ElementsMatch(tc.expectedIDs, gotIDs)
		})
	}
}

func (s *PriceSubscriptionScopeSuite) TestGetPricesBySubscriptionIDValidation() {
	resp, err := s.service.GetPricesBySubscriptionID(s.GetContext(), "")
	s.Error(err)
	s.Nil(resp)
}
