package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// An override past its end_date drops out and the plan entitlement it replaced returns.
func (s *SubscriptionServiceSuite) TestActiveEntitlements_HonourOverrideDates() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription

	featureID := "feat_dated_override"
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, &feature.Feature{
		ID:        featureID,
		Name:      "Rate limit config",
		Type:      types.FeatureTypeStatic,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	planEnt := &entitlement.Entitlement{
		ID:          "ent_dated_plan",
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    sub.PlanID,
		FeatureID:   featureID,
		FeatureType: types.FeatureTypeStatic,
		IsEnabled:   true,
		StaticValue: "100",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, planEnt)
	s.Require().NoError(err)

	overrideStart := sub.CurrentPeriodStart.Add(24 * time.Hour)
	overrideEnd := sub.CurrentPeriodStart.Add(5 * 24 * time.Hour)
	_, err = s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID:                  "ent_dated_override",
		EntityType:          types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:            sub.ID,
		FeatureID:           featureID,
		FeatureType:         types.FeatureTypeStatic,
		IsEnabled:           true,
		StaticValue:         "500",
		ParentEntitlementID: lo.ToPtr(planEnt.ID),
		StartDate:           &overrideStart,
		EndDate:             &overrideEnd,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	})
	s.Require().NoError(err)

	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"before start_date", sub.CurrentPeriodStart.Add(time.Hour), "100"},
		{"inside window", sub.CurrentPeriodStart.Add(2 * 24 * time.Hour), "500"},
		{"at end_date", overrideEnd, "100"},
		{"after end_date", overrideEnd.Add(24 * time.Hour), "100"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ents, err := subService.GetActiveSubscriptionEntitlementsForSubscription(ctx, sub, tt.at)
			s.Require().NoError(err)
			got := lo.Filter(ents, func(e *dto.EntitlementResponse, _ int) bool { return e.FeatureID == featureID })
			s.Require().Len(got, 1)
			s.Equal(tt.want, got[0].StaticValue)
		})
	}
}

// Customer-facing reads drop an expired plan entitlement; billing still sees it, so an invoice
// run after end_date keeps the allowance that covered the billed period.
func (s *SubscriptionServiceSuite) TestExpiredPlanEntitlement_StaysInBillingView() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription

	featureID := "feat_dated_plan"
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, &feature.Feature{
		ID:        featureID,
		Name:      "Expired allowance",
		Type:      types.FeatureTypeStatic,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	ended := time.Now().UTC().Add(-time.Hour)
	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID:          "ent_dated_plan_expired",
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    sub.PlanID,
		FeatureID:   featureID,
		FeatureType: types.FeatureTypeStatic,
		IsEnabled:   true,
		StaticValue: "on",
		EndDate:     &ended,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	})
	s.Require().NoError(err)

	hasFeature := func(resp *dto.SubscriptionEntitlementsResponse) bool {
		return lo.ContainsBy(resp.Features, func(f *dto.AggregatedFeature) bool {
			return f.Feature != nil && f.Feature.ID == featureID
		})
	}

	active, err := subService.GetActiveAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	s.Require().NoError(err)
	s.False(hasFeature(active), "an expired entitlement must not reach customer-facing reads")

	billing, err := subService.GetAggregatedSubscriptionEntitlementsForSubscription(ctx, sub, nil)
	s.Require().NoError(err)
	s.True(hasFeature(billing), "billing must keep the entitlement that covered the period")
}

// A grant-backed entitlement that ends mid-cycle stays in customer-facing reads until the
// cycle ends, so its grant state still shows; a plain one drops at end_date.
func (s *SubscriptionServiceSuite) TestActiveEntitlements_GrantBackedStayForEndedCycle() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.testData.subscription

	ended := sub.CurrentPeriodStart.Add(2 * 24 * time.Hour)
	at := sub.CurrentPeriodStart.Add(5 * 24 * time.Hour)
	for _, tc := range []struct {
		featureID string
		grant     bool
	}{
		{"feat_ended_grant", true},
		{"feat_ended_plain", false},
	} {
		s.NoError(s.GetStores().FeatureRepo.Create(ctx, &feature.Feature{
			ID:        tc.featureID,
			Name:      tc.featureID,
			Type:      types.FeatureTypeStatic,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}))
		ent := &entitlement.Entitlement{
			ID:          "ent_" + tc.featureID,
			EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			EntityID:    sub.PlanID,
			FeatureID:   tc.featureID,
			FeatureType: types.FeatureTypeStatic,
			IsEnabled:   true,
			StaticValue: "on",
			EndDate:     &ended,
			BaseModel:   types.GetDefaultBaseModel(ctx),
		}
		if tc.grant {
			ent.GrantQuota = lo.ToPtr(decimal.NewFromInt(100))
		}
		_, err := s.GetStores().EntitlementRepo.Create(ctx, ent)
		s.Require().NoError(err)
	}

	ents, err := subService.GetActiveSubscriptionEntitlementsForSubscription(ctx, sub, at)
	s.Require().NoError(err)
	has := func(featureID string) bool {
		return lo.ContainsBy(ents, func(e *dto.EntitlementResponse) bool { return e.FeatureID == featureID })
	}
	s.True(has("feat_ended_grant"), "grant-backed entitlement stays for the cycle it ended in")
	s.False(has("feat_ended_plain"), "plain entitlement drops at end_date")

	next, err := subService.GetActiveSubscriptionEntitlementsForSubscription(ctx, &subscription.Subscription{
		ID: sub.ID, PlanID: sub.PlanID, CurrentPeriodStart: sub.CurrentPeriodEnd, CurrentPeriodEnd: sub.CurrentPeriodEnd.AddDate(0, 1, 0),
		BaseModel: sub.BaseModel, EnvironmentID: sub.EnvironmentID, CustomerID: sub.CustomerID,
	}, sub.CurrentPeriodEnd.Add(time.Hour))
	s.Require().NoError(err)
	s.False(lo.ContainsBy(next, func(e *dto.EntitlementResponse) bool { return e.FeatureID == "feat_ended_grant" }),
		"grant-backed entitlement drops from the next cycle")
}

func TestIsEntitlementActive(t *testing.T) {
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cycleEnd := cycleStart.AddDate(0, 1, 0)
	sub := &subscription.Subscription{CurrentPeriodStart: cycleStart, CurrentPeriodEnd: cycleEnd}
	at := cycleStart.Add(10 * 24 * time.Hour)
	day5 := cycleStart.Add(5 * 24 * time.Hour)
	day15 := cycleStart.Add(15 * 24 * time.Hour)
	quota := lo.ToPtr(decimal.NewFromInt(100))

	tests := []struct {
		name string
		ent  *entitlement.Entitlement
		want bool
	}{
		{"plain, no dates", &entitlement.Entitlement{}, true},
		{"plain, ended this cycle", &entitlement.Entitlement{EndDate: &day5}, false},
		{"plain, starts later", &entitlement.Entitlement{StartDate: &day15}, false},
		{"grant, ended this cycle", &entitlement.Entitlement{GrantQuota: quota, EndDate: &day5}, true},
		{"grant, ended at cycle start", &entitlement.Entitlement{GrantQuota: quota, EndDate: &cycleStart}, false},
		{"grant, starts later this cycle", &entitlement.Entitlement{GrantQuota: quota, StartDate: &day15}, false},
		{"grant, starts next cycle", &entitlement.Entitlement{GrantQuota: quota, StartDate: &cycleEnd}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEntitlementActive(tt.ent, sub, at); got != tt.want {
				t.Errorf("isEntitlementActive() = %v, want %v", got, tt.want)
			}
		})
	}
}
