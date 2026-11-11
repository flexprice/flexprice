package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *SubscriptionChangeV2Suite) createPlanGrant(planID, name string, credits int64) *creditgrant.CreditGrant {
	ctx := s.GetContext()
	cg := &creditgrant.CreditGrant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:           name,
		Scope:          types.CreditGrantScopePlan,
		PlanID:         lo.ToPtr(planID),
		Credits:        decimal.NewFromInt(credits),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().CreditGrantRepo.Create(ctx, cg)
	s.Require().NoError(err)
	return cg
}

// subscriptionGrants returns the subscription-scoped grants, keyed by their source plan.
func (s *SubscriptionChangeV2Suite) subscriptionGrants() []*creditgrant.CreditGrant {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{s.td.sub.ID}
	filter.WithStatus(types.StatusPublished)
	grants, err := s.GetStores().CreditGrantRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return grants
}

func (s *SubscriptionChangeV2Suite) liveGrantsForPlan(planID string, at time.Time) []*creditgrant.CreditGrant {
	live := make([]*creditgrant.CreditGrant, 0)
	for _, g := range s.subscriptionGrants() {
		if lo.FromPtr(g.PlanID) != planID {
			continue
		}
		if g.EndDate != nil && !g.EndDate.After(at) {
			continue
		}
		live = append(live, g)
	}
	return live
}

func (s *SubscriptionChangeV2Suite) grantChanges(resp *dto.SubscriptionChangeV2Response) []*dto.EntityChangeResult {
	return lo.Filter(resp.EntityChanges, func(c *dto.EntityChangeResult, _ int) bool {
		return c.EntityType == types.SubscriptionChangeEntityTypeCreditGrant
	})
}

// The defect this phase exists for: without migration the customer keeps receiving the
// old plan's credits forever and never receives the new plan's.
func (s *SubscriptionChangeV2Suite) TestExecute_MigratesCreditGrantsToTheTargetPlan() {
	ctx := s.GetContext()

	s.createPlanGrant(s.td.starter.ID, "starter credits", 100)
	s.createPlanGrant(s.td.pro.ID, "pro credits", 500)

	// Materialise the starter grant on the subscription the way creation does.
	starterSubGrant := s.materialisePlanGrants(s.td.starter.ID)
	s.Require().Len(starterSubGrant, 1)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	at := resp.EffectiveAt
	s.Empty(s.liveGrantsForPlan(s.td.starter.ID, at), "the outgoing plan's grants stop at the change")

	proGrants := s.liveGrantsForPlan(s.td.pro.ID, at)
	s.Require().Len(proGrants, 1, "the target plan's grants are materialised")
	s.Equal(decimal.NewFromInt(500), proGrants[0].Credits)
	s.Require().NotNil(proGrants[0].StartDate)
	s.True(proGrants[0].StartDate.Equal(at), "the new grant starts at the change instant")

	// The old grant row survives, end-dated — already-granted credits are not clawed back.
	ended := lo.Filter(s.subscriptionGrants(), func(g *creditgrant.CreditGrant, _ int) bool {
		return lo.FromPtr(g.PlanID) == s.td.starter.ID
	})
	s.Require().Len(ended, 1)
	s.Require().NotNil(ended[0].EndDate)
	s.True(ended[0].EndDate.Equal(at))
	s.Equal(types.StatusPublished, ended[0].Status, "the grant is closed, not deleted")
}

// materialisePlanGrants creates the subscription-scoped grants a plan would produce at
// subscription creation, through the same mapping the service uses.
func (s *SubscriptionChangeV2Suite) materialisePlanGrants(planID string) []*creditgrant.CreditGrant {
	return s.materialisePlanGrantsFor(s.td.sub, planID)
}

func (s *SubscriptionChangeV2Suite) materialisePlanGrantsFor(sub *subscription.Subscription, planID string) []*creditgrant.CreditGrant {
	ctx := s.GetContext()
	planGrants, err := NewCreditGrantService(s.serviceParams()).GetCreditGrantsByPlan(ctx, planID)
	s.Require().NoError(err)

	created := make([]*creditgrant.CreditGrant, 0, len(planGrants.Items))
	for _, pg := range planGrants.Items {
		req := dto.NewSubscriptionScopedCreditGrantRequest(pg, sub.ID, planID)
		cg := &creditgrant.CreditGrant{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
			Name:           req.Name,
			Scope:          req.Scope,
			PlanID:         req.PlanID,
			SubscriptionID: req.SubscriptionID,
			Credits:        req.Credits,
			Cadence:        req.Cadence,
			Period:         req.Period,
			PeriodCount:    req.PeriodCount,
			ExpirationType: req.ExpirationType,
			StartDate:      lo.ToPtr(s.td.periodStart),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
		_, err := s.GetStores().CreditGrantRepo.Create(ctx, cg)
		s.Require().NoError(err)
		created = append(created, cg)
	}
	return created
}

// Addon grants have their own provenance and must outlive a plan swap.
func (s *SubscriptionChangeV2Suite) TestExecute_AddonGrantsSurviveAPlanChange() {
	ctx := s.GetContext()

	addonEntity, _, _ := s.attachAddon("extra", 10)
	s.createPlanGrant(s.td.starter.ID, "starter credits", 100)
	s.materialisePlanGrants(s.td.starter.ID)

	addonGrant := &creditgrant.CreditGrant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:           "addon credits",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: lo.ToPtr(s.td.sub.ID),
		AddonID:        lo.ToPtr(addonEntity.ID),
		Credits:        decimal.NewFromInt(25),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		StartDate:      lo.ToPtr(s.td.periodStart),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	_, createErr := s.GetStores().CreditGrantRepo.Create(ctx, addonGrant)
	s.Require().NoError(createErr)

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	stored, err := s.GetStores().CreditGrantRepo.Get(ctx, addonGrant.ID)
	s.Require().NoError(err)
	s.Nil(stored.EndDate, "the addon's grant is untouched by the plan swap")
}

func (s *SubscriptionChangeV2Suite) TestPreview_ReportsGrantMigrationWithoutWriting() {
	ctx := s.GetContext()

	s.createPlanGrant(s.td.starter.ID, "starter credits", 100)
	proGrant := s.createPlanGrant(s.td.pro.ID, "pro credits", 500)
	materialised := s.materialisePlanGrants(s.td.starter.ID)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone))
	s.Require().NoError(err)

	changes := s.grantChanges(preview)
	s.Require().Len(changes, 2)

	dropped, added := changes[0], changes[1]
	s.Equal(types.EntityChangeBehaviourDrop, dropped.Behaviour)
	s.Equal(materialised[0].ID, dropped.EntityID)
	s.Equal(s.td.starter.ID, dropped.ReferenceID)

	s.Equal(types.EntityChangeBehaviourAdd, added.Behaviour)
	s.Equal(proGrant.ID, added.EntityID, "preview names the plan grant it would materialise")
	s.Equal(s.td.pro.ID, added.ReferenceID)

	s.Require().Len(s.subscriptionGrants(), 1, "preview created nothing")
	s.Nil(s.subscriptionGrants()[0].EndDate, "preview cancelled nothing")
}

func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_MigratesGrantsLikeAnImmediateChange() {
	ctx := s.GetContext()
	boundary := s.td.periodEnd

	s.createPlanGrant(s.td.starter.ID, "starter credits", 100)
	s.createPlanGrant(s.td.pro.ID, "pro credits", 500)
	s.materialisePlanGrants(s.td.starter.ID)

	sched, config := s.createV2Schedule(s.td.pro.ID, boundary)
	s.rollPeriodTo(boundary, boundary.AddDate(0, 0, 30))
	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	s.Empty(s.liveGrantsForPlan(s.td.starter.ID, boundary))
	proGrants := s.liveGrantsForPlan(s.td.pro.ID, boundary)
	s.Require().Len(proGrants, 1)
	s.True(proGrants[0].StartDate.Equal(boundary), "the scheduled path anchors grants at the boundary")
}

func (s *SubscriptionChangeV2Suite) createFeature(name string) *feature.Feature {
	ctx := s.GetContext()
	f := &feature.Feature{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:      name,
		Type:      types.FeatureTypeMetered,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))
	return f
}

func (s *SubscriptionChangeV2Suite) createPlanEntitlement(planID, featureID string, limit int64) *entitlement.Entitlement {
	ctx := s.GetContext()
	e := &entitlement.Entitlement{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         planID,
		FeatureID:        featureID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(limit),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, e)
	s.Require().NoError(err)
	return e
}

func (s *SubscriptionChangeV2Suite) createSubscriptionOverride(parent *entitlement.Entitlement, limit int64) *entitlement.Entitlement {
	return s.createSubscriptionOverrideFor(s.td.sub, parent, limit)
}

func (s *SubscriptionChangeV2Suite) createSubscriptionOverrideFor(
	sub *subscription.Subscription,
	parent *entitlement.Entitlement,
	limit int64,
) *entitlement.Entitlement {
	ctx := s.GetContext()
	e := &entitlement.Entitlement{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:          types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:            sub.ID,
		FeatureID:           parent.FeatureID,
		FeatureType:         parent.FeatureType,
		IsEnabled:           true,
		UsageLimit:          lo.ToPtr(limit),
		UsageResetPeriod:    parent.UsageResetPeriod,
		ParentEntitlementID: lo.ToPtr(parent.ID),
		StartDate:           lo.ToPtr(s.td.periodStart),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, e)
	s.Require().NoError(err)
	return e
}

// A v1-era override whose parent belongs to the outgoing plan suppresses nothing after
// the swap and would otherwise stack with the new plan's entitlement on the same feature.
func (s *SubscriptionChangeV2Suite) TestExecute_ClosesStaleEntitlementOverrides() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)
	proEnt := s.createPlanEntitlement(s.td.pro.ID, f.ID, 5000)
	override := s.createSubscriptionOverride(starterEnt, 2000)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	stored, err := s.GetStores().EntitlementRepo.Get(ctx, override.ID)
	s.Require().NoError(err)
	s.Require().NotNil(stored.EndDate, "the stale override is closed")
	s.True(stored.EndDate.Equal(resp.EffectiveAt))

	// Plan entitlements are rows on the plan, shared by every subscription on it.
	for _, planEnt := range []*entitlement.Entitlement{starterEnt, proEnt} {
		live, err := s.GetStores().EntitlementRepo.Get(ctx, planEnt.ID)
		s.Require().NoError(err)
		s.Nil(live.EndDate, "plan entitlements must never be end-dated by a plan change")
		s.Equal(types.StatusPublished, live.Status)
	}

	reported := lo.Filter(resp.EntityChanges, func(c *dto.EntityChangeResult, _ int) bool {
		return c.EntityType == types.SubscriptionChangeEntityTypeEntitlement
	})
	s.Require().Len(reported, 1)
	s.Equal(override.ID, reported[0].EntityID)
	s.Equal(f.ID, reported[0].ReferenceID)
	s.Equal(types.EntityChangeBehaviourDrop, reported[0].Behaviour)
}

// An override parented to a plan the subscription is not leaving is none of our business.
func (s *SubscriptionChangeV2Suite) TestExecute_LeavesUnrelatedOverridesAlone() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	proEnt := s.createPlanEntitlement(s.td.pro.ID, f.ID, 5000)
	override := s.createSubscriptionOverride(proEnt, 2000)

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	stored, err := s.GetStores().EntitlementRepo.Get(ctx, override.ID)
	s.Require().NoError(err)
	s.Nil(stored.EndDate)
}

func (s *SubscriptionChangeV2Suite) createEntitlementGrant(
	sub *subscription.Subscription,
	configID, featureID string,
	validFrom, validTo time.Time,
) *entitlementgrant.EntitlementGrant {
	ctx := s.GetContext()
	g := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT),
		EntitlementConfigID: configID,
		CustomerID:          sub.CustomerID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       featureID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(1000),
		Usage:               decimal.Zero,
		ValidFrom:           validFrom,
		ValidTo:             validTo,
		GrantStatus:         types.EntitlementGrantStatusActive,
		LastComputedAt:      lo.ToPtr(validFrom.Add(time.Second)),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, g)
	s.Require().NoError(err)
	return g
}

func (s *SubscriptionChangeV2Suite) storedGrant(id string) *entitlementgrant.EntitlementGrant {
	g, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return g
}

// An incoming allowance replaces the old window using the shared addon flow.
func (s *SubscriptionChangeV2Suite) TestExecute_ClosesEntitlementGrantsFromTheOutgoingPlan() {
	ctx := s.GetContext()
	f, starterEnt, proEnt := s.planAllowanceFixture(types.ResetUsageBillingPeriod)
	closing := s.createEntitlementGrant(s.td.sub, starterEnt.ID, f.ID, s.td.periodStart, s.td.periodEnd)
	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)
	closed := s.storedGrant(closing.ID)
	s.True(closed.ValidTo.Equal(resp.EffectiveAt))
	s.True(closed.ValidFrom.Equal(s.td.periodStart))
	current, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	live, err := NewEntitlementGrantService(s.serviceParams()).LiveGrantsByFeature(ctx, current, resp.EffectiveAt)
	s.Require().NoError(err)
	s.Require().Len(live[f.ID], 1)
	s.Equal(proEnt.ID, live[f.ID][0].EntitlementConfigID)
}

// Removing the last allowance leaves its granted window to expire, as for addons.
func (s *SubscriptionChangeV2Suite) TestExecute_LeavesGrantOpenWhenOverriddenFeatureIsRemoved() {
	ctx := s.GetContext()

	f, starterEnt, targetEnt := s.planAllowanceFixture(types.ResetUsageBillingPeriod)
	s.Require().NoError(s.GetStores().EntitlementRepo.Delete(ctx, targetEnt.ID))
	override := s.createSubscriptionOverride(starterEnt, 2000)
	override.GrantMeasure = starterEnt.GrantMeasure
	override.GrantDurationUnit = starterEnt.GrantDurationUnit
	override.GrantQuota = lo.ToPtr(decimal.NewFromInt(2000))
	_, err := s.GetStores().EntitlementRepo.Update(ctx, override)
	s.Require().NoError(err)
	grant := s.createEntitlementGrant(s.td.sub, override.ID, f.ID, s.td.periodStart, s.td.periodEnd)

	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	s.True(s.storedGrant(grant.ID).ValidTo.Equal(s.td.periodEnd))
}

// Grant windows are per subscription, and so is the close.
func (s *SubscriptionChangeV2Suite) TestExecute_LeavesAnotherSubscriptionsGrantsAlone() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)

	other := s.createSubscription(s.td.starter.ID)
	otherGrant := s.createEntitlementGrant(other, starterEnt.ID, f.ID, s.td.periodStart, s.td.periodEnd)
	otherOverride := s.createSubscriptionOverrideFor(other, starterEnt, 2000)

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	s.True(s.storedGrant(otherGrant.ID).ValidTo.Equal(s.td.periodEnd),
		"another subscription on the same outgoing plan keeps its window")

	stored, err := s.GetStores().EntitlementRepo.Get(ctx, otherOverride.ID)
	s.Require().NoError(err)
	s.Nil(stored.EndDate, "and keeps its override")
}

// A window that already ended before the change has nothing left to close.
func (s *SubscriptionChangeV2Suite) TestExecute_IgnoresAlreadyClosedGrantWindows() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)
	expiredAt := s.td.periodStart.AddDate(0, 0, 1)
	expired := s.createEntitlementGrant(s.td.sub, starterEnt.ID, f.ID, s.td.periodStart, expiredAt)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	s.True(s.storedGrant(expired.ID).ValidTo.Equal(expiredAt))
	s.Empty(lo.Filter(resp.EntityChanges, func(c *dto.EntityChangeResult, _ int) bool {
		return c.EntityType == types.SubscriptionChangeEntityTypeEntitlementGrant
	}))
}

func (s *SubscriptionChangeV2Suite) TestPreview_DoesNotMutateEntitlementGrants() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)
	grant := s.createEntitlementGrant(s.td.sub, starterEnt.ID, f.ID, s.td.periodStart, s.td.periodEnd)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone))
	s.Require().NoError(err)

	reported := lo.Filter(preview.EntityChanges, func(c *dto.EntityChangeResult, _ int) bool {
		return c.EntityType == types.SubscriptionChangeEntityTypeEntitlementGrant
	})
	s.Empty(reported, "grant windows are not reported as dropped before the shared flow runs")

	s.True(s.storedGrant(grant.ID).ValidTo.Equal(s.td.periodEnd), "preview closed nothing")
}

func (s *SubscriptionChangeV2Suite) planAllowanceFixture(reset types.ResetUsage) (*feature.Feature, *entitlement.Entitlement, *entitlement.Entitlement) {
	ctx := s.GetContext()
	m := &meter.Meter{ID: types.GenerateUUID(), EventName: types.GenerateUUID(), ResetUsage: reset,
		Aggregation: meter.Aggregation{Type: types.AggregationSum}, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	f := &feature.Feature{ID: types.GenerateUUID(), Type: types.FeatureTypeMetered, MeterID: m.ID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))
	createEC := func(planID string, quota int64) *entitlement.Entitlement {
		ec := &entitlement.Entitlement{ID: types.GenerateUUID(), EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: planID,
			FeatureID: f.ID, FeatureType: types.FeatureTypeMetered, IsEnabled: true,
			GrantMeasure: types.EntitlementGrantMeasureQuantity, GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
			GrantQuota: lo.ToPtr(decimal.NewFromInt(quota)), BaseModel: types.GetDefaultBaseModel(ctx)}
		_, err := s.GetStores().EntitlementRepo.Create(ctx, ec)
		s.Require().NoError(err)
		return ec
	}
	return f, createEC(s.td.starter.ID, 100), createEC(s.td.pro.ID, 1000)
}

func (s *SubscriptionChangeV2Suite) TestExecute_PlanAllowancesUseAddonGrantChangeFlow() {
	cases := []struct {
		name                       string
		live, prorate, unevaluated bool
	}{
		{name: "carry remaining and add full incoming quota", live: true},
		{name: "open new allowance without predecessor"},
		{name: "prorate incoming quota only", live: true, prorate: true},
		{name: "unevaluated predecessor follows addon replacement", live: true, unevaluated: true},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			f, oldEC, newEC := s.planAllowanceFixture(types.ResetUsageBillingPeriod)
			sub := s.createSubscription(s.td.starter.ID)
			at := time.Now().UTC().Truncate(time.Second)
			start := at
			var predecessor *entitlementgrant.EntitlementGrant
			if tc.live {
				predecessor = s.createEntitlementGrant(sub, oldEC.ID, f.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
				builder := entitlementgrant.NewEntitlementGrantBuilder(predecessor).
					WithQuota(decimal.NewFromInt(100)).WithUsage(decimal.NewFromInt(80)).WithLastComputedAt(lo.ToPtr(at.Add(-time.Second)))
				if tc.unevaluated {
					builder.WithUsage(decimal.Zero).WithLastComputedAt(nil)
					start = sub.CurrentPeriodStart
				}
				predecessor = builder.Build()
				_, err := s.GetStores().EntitlementGrantRepo.Update(ctx, predecessor)
				s.Require().NoError(err)
			}
			behavior := types.ProrationBehaviorNone
			if tc.prorate {
				behavior = types.ProrationBehaviorCreateProrations
			}
			_, err := s.svc.ExecutePlanChange(ctx, sub.ID, s.changeRequest(s.td.pro.ID, behavior), at)
			s.Require().NoError(err)
			current, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			grants, err := NewEntitlementGrantService(s.serviceParams()).LiveGrantsByFeature(ctx, current, at)
			s.Require().NoError(err)
			s.Require().Len(grants[f.ID], 1)
			g := grants[f.ID][0]
			s.Equal(newEC.ID, g.EntitlementConfigID)
			s.True(g.ValidFrom.Equal(start))
			s.True(g.ValidTo.Equal(current.CurrentPeriodEnd))
			if predecessor != nil {
				if tc.unevaluated {
					_, err := s.GetStores().EntitlementGrantRepo.Get(ctx, predecessor.ID)
					s.Require().Error(err, "the unevaluated predecessor is removed, as for addons")
				} else {
					s.True(s.storedGrant(predecessor.ID).ValidTo.Equal(at))
				}
			}
			switch {
			case tc.unevaluated:
				s.Equal("1100", g.Quota.String(), "the unused 100 carries forward alongside the incoming 1000")
			case tc.prorate:
				s.True(g.Quota.GreaterThan(decimal.NewFromInt(20)))
				s.True(g.Quota.LessThan(decimal.NewFromInt(1020)))
				coefficient, err := decimal.NewFromString(g.Metadata["proration_coefficient"])
				s.Require().NoError(err)
				s.True(g.Quota.Equal(decimal.NewFromInt(1000).Mul(coefficient).Round(entitlementGrantQuotaScale).Add(decimal.NewFromInt(20))))
			case tc.live:
				s.Equal("1020", g.Quota.String())
			default:
				s.Equal("1000", g.Quota.String())
			}
			_, _, err = NewEntitlementGrantService(s.serviceParams()).EnsureGrantsForSubscriptions(ctx, s.td.customer, []*subscription.Subscription{current}, at.Add(time.Minute))
			s.Require().NoError(err)
			grants, err = NewEntitlementGrantService(s.serviceParams()).LiveGrantsByFeature(ctx, current, at.Add(time.Minute))
			s.Require().NoError(err)
			s.Require().Len(grants[f.ID], 1)
			s.Equal(g.ID, grants[f.ID][0].ID)
			s.True(grants[f.ID][0].ValidFrom.Equal(start))
		})
	}
}

// A plan change on a never-reset meter grants the new plan's quota as the change prorates it,
// with no leftover from the old plan: the running total already carries the usage.
func (s *SubscriptionChangeV2Suite) TestExecute_NeverResetPlanAllowanceFollowsProration() {
	for _, behavior := range []types.ProrationBehavior{types.ProrationBehaviorNone, types.ProrationBehaviorCreateProrations} {
		s.Run(string(behavior), func() {
			ctx := s.GetContext()
			f, oldEC, newEC := s.planAllowanceFixture(types.ResetUsageNever)
			sub := s.createSubscription(s.td.starter.ID)
			at := time.Now().UTC().Truncate(time.Second)
			predecessor := entitlementgrant.NewEntitlementGrantBuilder(
				s.createEntitlementGrant(sub, oldEC.ID, f.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)).
				WithQuota(decimal.NewFromInt(100)).WithUsage(decimal.NewFromInt(80)).WithLastComputedAt(lo.ToPtr(at.Add(-time.Second))).
				Build()
			_, err := s.GetStores().EntitlementGrantRepo.Update(ctx, predecessor)
			s.Require().NoError(err)

			_, err = s.svc.ExecutePlanChange(ctx, sub.ID, s.changeRequest(s.td.pro.ID, behavior), at)
			s.Require().NoError(err)
			current, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			grants, err := NewEntitlementGrantService(s.serviceParams()).LiveGrantsByFeature(ctx, current, at)
			s.Require().NoError(err)
			s.Require().Len(grants[f.ID], 1)
			g := grants[f.ID][0]
			s.Equal(newEC.ID, g.EntitlementConfigID)

			want := decimal.NewFromInt(1000)
			if behavior == types.ProrationBehaviorCreateProrations {
				coefficient, err := decimal.NewFromString(g.Metadata["proration_coefficient"])
				s.Require().NoError(err)
				s.True(coefficient.LessThan(decimal.NewFromInt(1)), "a mid-cycle change is scaled, got %s", coefficient)
				want = want.Mul(coefficient).Round(entitlementGrantQuotaScale)
			}
			s.True(g.Quota.Equal(want), "expected %s, got %s", want, g.Quota)
		})
	}
}

func (s *SubscriptionChangeV2Suite) TestExecute_PlanChangeDoesNotCountPreChangeUsageTwice() {
	ctx := s.GetContext()
	f, oldEC, newEC := s.planAllowanceFixture(types.ResetUsageBillingPeriod)
	at := time.Now().UTC()
	old := s.createEntitlementGrant(s.td.sub, oldEC.ID, f.ID, s.td.periodStart, s.td.periodEnd)
	old = entitlementgrant.NewEntitlementGrantBuilder(old).WithQuota(decimal.NewFromInt(100)).WithUsage(decimal.NewFromInt(300)).WithLastComputedAt(lo.ToPtr(at.Add(-time.Second))).Build()
	_, err := s.GetStores().EntitlementGrantRepo.Update(ctx, old)
	s.Require().NoError(err)
	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), at)
	s.Require().NoError(err)
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{Event: events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ExternalCustomerID: s.td.customer.ExternalID, Timestamp: at.Add(-time.Hour)}, MeterID: f.MeterID, QtyTotal: decimal.NewFromInt(300)},
		{Event: events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ExternalCustomerID: s.td.customer.ExternalID, Timestamp: at.Add(time.Second)}, MeterID: f.MeterID, QtyTotal: decimal.NewFromInt(800)},
	}))
	current, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	grantSvc := NewEntitlementGrantService(s.serviceParams())
	evaluatedAt := at.Add(time.Minute)
	grants, meta, err := grantSvc.EnsureGrantsForSubscriptions(ctx, s.td.customer, []*subscription.Subscription{current}, evaluatedAt)
	s.Require().NoError(err)
	s.Require().NoError(NewAlertService(s.serviceParams()).(*alertService).evaluateEntitlementGrantsForCustomer(ctx, s.td.customer, meta, grants, evaluatedAt))
	live, err := grantSvc.LiveGrantsByFeature(ctx, current, evaluatedAt)
	s.Require().NoError(err)
	s.Require().Len(live[f.ID], 1)
	incoming := s.storedGrant(live[f.ID][0].ID)
	s.Equal(newEC.ID, incoming.EntitlementConfigID)
	s.Equal("800", incoming.Usage.String())
	s.Equal("1000", incoming.Quota.String())
	s.Equal("200", s.storedGrant(old.ID).Overage().Add(incoming.Overage()).String())
}
