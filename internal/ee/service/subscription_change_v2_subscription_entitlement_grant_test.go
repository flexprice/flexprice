package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// addSubscriptionEntitlementGrant gives the fixture subscription a $50 subscription entitlement grant.
func (s *SubscriptionChangeV2Suite) addSubscriptionEntitlementGrant() {
	ctx := s.GetContext()
	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:        types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:          s.td.sub.ID,
		GrantScope:        types.EntitlementGrantScopeSubscription,
		GrantMeasure:      types.EntitlementGrantMeasureAmount,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(50)),
		IsEnabled:         true,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	})
	s.Require().NoError(err)
}

func (s *SubscriptionChangeV2Suite) TestPlanChange_SubscriptionEntitlementGrantGuard() {
	cases := []struct {
		name    string
		setup   func() // shapes the target plan or the request
		reset   bool
		wantErr string
	}{
		{name: "compatible target plan is allowed", setup: func() {
			s.createUsagePriceWithSequence(s.td.pro.ID, 0)
		}},
		{name: "tiered usage line on the target plan", wantErr: "tiered", setup: func() {
			p := s.createUsagePriceWithSequence(s.td.pro.ID, 0)
			p.BillingModel = types.BILLING_MODEL_TIERED
			p.TierMode = types.BILLING_TIER_VOLUME
			p.Tiers = price.JSONBTiers{{UnitAmount: decimal.NewFromInt(1)}}
			s.Require().NoError(s.GetStores().PriceRepo.Update(s.GetContext(), p, false))
		}},
		{name: "feature entitlement grant on the target plan", wantErr: "feature entitlement grants", setup: func() {
			s.createPlanEntitlement(s.td.pro.ID, s.createFeature("api_calls").ID, 1000)
		}},
		{name: "billing period reset", reset: true, wantErr: "cannot be reset"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.addSubscriptionEntitlementGrant()
			if tc.setup != nil {
				tc.setup()
			}
			req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
			if tc.reset {
				req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
			}

			_, previewErr := s.svc.PreviewPlanChange(s.GetContext(), s.td.sub.ID, req)
			_, executeErr := s.svc.ExecutePlanChange(s.GetContext(), s.td.sub.ID, req, time.Now().UTC())
			if tc.wantErr == "" {
				s.NoError(previewErr)
				s.NoError(executeErr)
				return
			}
			s.Require().Error(previewErr)
			s.Contains(previewErr.Error(), tc.wantErr)
			s.Require().Error(executeErr)
			s.Contains(executeErr.Error(), tc.wantErr)
			s.Equal(s.td.starter.ID, s.reloadSub().PlanID, "a refused change leaves the plan as it was")
		})
	}
}

func (s *SubscriptionChangeV2Suite) reloadSub() *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.td.sub.ID)
	s.Require().NoError(err)
	return sub
}
