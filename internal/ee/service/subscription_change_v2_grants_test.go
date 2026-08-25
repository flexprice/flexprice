package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/feature"
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

// A window left open keeps granting the outgoing plan's quota after the swap, and billing
// reads its overage as [quota_crossed_at, valid_to] — so it would charge overage on the old
// plan's terms for time already spent on the new one.
func (s *SubscriptionChangeV2Suite) TestExecute_ClosesEntitlementGrantsFromTheOutgoingPlan() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)
	proEnt := s.createPlanEntitlement(s.td.pro.ID, f.ID, 5000)

	closing := s.createEntitlementGrant(s.td.sub, starterEnt.ID, f.ID, s.td.periodStart, s.td.periodEnd)
	surviving := s.createEntitlementGrant(s.td.sub, proEnt.ID, f.ID, s.td.periodStart, s.td.periodEnd)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	closed := s.storedGrant(closing.ID)
	s.True(closed.ValidTo.Equal(resp.EffectiveAt), "the outgoing plan's window ends at the swap")
	s.True(closed.ValidFrom.Equal(s.td.periodStart), "closing must not move the window start")

	untouched := s.storedGrant(surviving.ID)
	s.True(untouched.ValidTo.Equal(s.td.periodEnd), "a window on the target plan's config is not ours to close")

	reported := lo.Filter(resp.EntityChanges, func(c *dto.EntityChangeResult, _ int) bool {
		return c.EntityType == types.SubscriptionChangeEntityTypeEntitlementGrant
	})
	s.Require().Len(reported, 1)
	s.Equal(closing.ID, reported[0].EntityID)
	s.Equal(f.ID, reported[0].ReferenceID)
	s.Equal(types.EntityChangeBehaviourDrop, reported[0].Behaviour)
}

// Grants key off whichever entitlement the subscription actually resolves to, so an
// overridden feature's window carries the override's id rather than the plan entitlement's.
func (s *SubscriptionChangeV2Suite) TestExecute_ClosesGrantsKeyedByAClosingOverride() {
	ctx := s.GetContext()

	f := s.createFeature("api_calls")
	starterEnt := s.createPlanEntitlement(s.td.starter.ID, f.ID, 1000)
	override := s.createSubscriptionOverride(starterEnt, 2000)
	grant := s.createEntitlementGrant(s.td.sub, override.ID, f.ID, s.td.periodStart, s.td.periodEnd)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	s.True(s.storedGrant(grant.ID).ValidTo.Equal(resp.EffectiveAt))
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

func (s *SubscriptionChangeV2Suite) TestPreview_ReportsGrantClosureWithoutWriting() {
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
	s.Require().Len(reported, 1)
	s.Equal(grant.ID, reported[0].EntityID)

	s.True(s.storedGrant(grant.ID).ValidTo.Equal(s.td.periodEnd), "preview closed nothing")
}
