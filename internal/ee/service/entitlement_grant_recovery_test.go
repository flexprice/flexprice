package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *EntitlementGrantSuite) recoveryFixture(reset types.ResetUsage) (*customer.Customer, *subscription.Subscription, *meter.Meter) {
	ctx := s.GetContext()
	m := &meter.Meter{
		ID: "meter-recovery", Name: "Recovery", EventName: "usage", ResetUsage: reset,
		Aggregation: meter.Aggregation{Type: types.AggregationSum},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.meterStore.CreateMeter(ctx, m))
	f := s.simpleFeature("feature-recovery", m.ID)
	p := s.simplePlan("plan-recovery")
	ec := s.newTimeBoxedEC("ec-recovery", f.ID, 0, types.EntitlementGrantDurationUnitSubscriptionPeriod, decimal.NewFromInt(100))
	ec.EntityType = types.ENTITLEMENT_ENTITY_TYPE_PLAN
	ec.EntityID = p.ID
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ec)
	s.Require().NoError(err)
	cust := s.simpleCustomer("customer-recovery")
	sub := s.simpleSubscription("sub-recovery", cust.ID, p.ID)
	return cust, sub, m
}

func (s *EntitlementGrantSuite) TestEvaluate_SignedUsageRecovery() {
	s.checkSignedUsageRecovery(types.ResetUsageBillingPeriod)
}

func (s *EntitlementGrantSuite) TestEvaluate_NeverBalanceRecovery() {
	s.checkSignedUsageRecovery(types.ResetUsageNever)
}

func (s *EntitlementGrantSuite) checkSignedUsageRecovery(reset types.ResetUsage) {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(reset)
	// Prior-period events affect NEVER only; checkpointing prevents a history-only test from passing accidentally.
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(-time.Hour), 40)
	firstDelta := int64(120)
	if reset == types.ResetUsageNever {
		_, err := NewMeterUsageService(s.buildServiceParams()).SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
			CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
			AggregationType: types.AggregationSum, Until: sub.CurrentPeriodStart,
		}, nil)
		s.Require().NoError(err)
		firstDelta = 80
	}
	publisher := testutil.NewInMemoryWebhookPublisher()
	params := s.buildServiceParams()
	params.WebhookPublisher = publisher
	evaluator := NewAlertService(params).(*alertService)
	var original *entitlementgrant.EntitlementGrant
	var crossedAt *time.Time
	for i, step := range []struct {
		delta, usage, remaining int64
		status                  types.EntitlementGrantStatus
		logs, webhooks          int
	}{
		{firstDelta, 120, 0, types.EntitlementGrantStatusExhausted, 1, 1},
		{0, 120, 0, types.EntitlementGrantStatusExhausted, 1, 1},
		{-40, 80, 20, types.EntitlementGrantStatusActive, 2, 1},
		{30, 110, 0, types.EntitlementGrantStatusExhausted, 3, 2},
		{-130, -20, 100, types.EntitlementGrantStatusActive, 4, 2},
		{50, 30, 70, types.EntitlementGrantStatusActive, 4, 2},
	} {
		eventAt := sub.CurrentPeriodStart.Add(time.Duration(i+1) * time.Hour)
		at := eventAt.Add(time.Minute)
		s.seedMeterUsage(cust.ExternalID, m.ID, eventAt, step.delta)
		grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, at)
		s.Require().NoError(err)
		s.Require().Len(grants, 1)
		if original == nil {
			snapshot := *grants[0]
			original = &snapshot
		}
		s.Equal(original.ID, grants[0].ID)
		if step.status == types.EntitlementGrantStatusActive {
			crossedAt = nil
		} else if crossedAt == nil {
			crossedAt = &at
		}
		for retry := 0; retry < 2; retry++ {
			s.Require().NoError(evaluator.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, at))
		}
		stored, err := s.GetStores().EntitlementGrantRepo.Get(ctx, original.ID)
		s.Require().NoError(err)
		s.Equal(step.status, stored.GrantStatus)
		s.True(stored.Usage.Equal(decimal.Max(decimal.NewFromInt(step.usage), decimal.Zero)), "step %d: consumption %s", i, stored.Usage)
		rawUsage, err := evaluator.refreshEntitlementGrantUsage(ctx, stored, m, sub, []string{cust.ExternalID}, at)
		s.Require().NoError(err)
		s.True(rawUsage.Equal(decimal.NewFromInt(step.usage)), "raw usage stays signed")
		s.True(stored.Quota.Equal(original.Quota))
		s.True(stored.ValidFrom.Equal(original.ValidFrom))
		s.True(stored.ValidTo.Equal(original.ValidTo))
		if crossedAt == nil {
			s.Nil(stored.QuotaCrossedAt, "recovery clears the crossing")
		} else {
			s.Require().NotNil(stored.QuotaCrossedAt)
			s.True(stored.QuotaCrossedAt.Equal(*crossedAt), "retain crossing until recovery; re-exhaustion records a new crossing")
		}
		s.NoError(stored.Validate())
		state, err := s.grantService.GrantStateByFeature(ctx, sub, at)
		s.Require().NoError(err)
		allowance := state["feature-recovery"].Allowances[0]
		s.True(allowance.IsActive)
		s.Require().NotNil(allowance.Remaining)
		s.True(allowance.Remaining.Equal(decimal.NewFromInt(step.remaining)))
		s.True(allowance.Usage.Equal(stored.Usage))
		logs, err := s.GetStores().AlertLogsRepo.ListByEntity(ctx, types.AlertEntityTypeEntitlementGrant, original.ID, 20)
		s.Require().NoError(err)
		s.Len(logs, step.logs, "retries must not duplicate transitions")
		s.Len(publisher.Events(), step.webhooks, "recovery records OK without a new public webhook")
	}
}

func (s *EntitlementGrantSuite) TestEvaluate_EndedGrantRefreshesStatusButRemainsInactive() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageBillingPeriod)
	eventAt := sub.CurrentPeriodStart.Add(time.Hour)
	s.seedMeterUsage(cust.ExternalID, m.ID, eventAt, 120)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, eventAt.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, eventAt.Add(time.Minute)))
	g := grants[0]
	end := eventAt.Add(time.Hour)
	s.Require().NoError(s.GetStores().EntitlementGrantRepo.CloseWindow(ctx, g.ID, end))
	s.seedMeterUsage(cust.ExternalID, m.ID, end.Add(-time.Minute), -40)
	s.seedMeterUsage(cust.ExternalID, m.ID, end, -500) // Exclusive end: belongs to a different window.
	g, err = s.GetStores().EntitlementGrantRepo.Get(ctx, g.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(ctx, cust, meta, []*entitlementgrant.EntitlementGrant{g}, end))
	stored, err := s.GetStores().EntitlementGrantRepo.Get(ctx, g.ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(80)))
	s.Equal(types.EntitlementGrantStatusActive, stored.GrantStatus, "status reflects quota exhaustion, not validity")
	s.Nil(stored.QuotaCrossedAt, "the final refresh clears a recovered crossing")
	state, err := s.grantService.GrantStateByFeature(ctx, sub, end)
	s.Require().NoError(err)
	s.False(state["feature-recovery"].Allowances[0].IsActive)
	logs, err := s.GetStores().AlertLogsRepo.ListByEntity(ctx, types.AlertEntityTypeEntitlementGrant, g.ID, 10)
	s.Require().NoError(err)
	s.Len(logs, 1, "ended grants do not emit recovery transitions")
	pending, err := s.GetStores().EntitlementGrantRepo.ListOpenOrUnfinalized(ctx, cust.ID, end, sub.CurrentPeriodStart)
	s.Require().NoError(err)
	s.Empty(pending, "the final usage refresh completes the ended window")
}

func (s *EntitlementGrantSuite) TestEvaluate_NeverQuietPeriodUsesCheckpoint() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageNever)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(-time.Hour), 80)
	_, err := NewMeterUsageService(s.buildServiceParams()).SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: sub.CurrentPeriodStart,
	}, nil)
	s.Require().NoError(err)
	at := sub.CurrentPeriodStart
	params := s.buildServiceParams()
	usageStore := &recordingGrantUsageStore{MeterUsageRepository: params.MeterUsageRepo}
	params.MeterUsageRepo = usageStore
	grants, meta, err := NewEntitlementGrantService(params).EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.Empty(usageStore.queries, "automatic opening leaves usage to the immediate evaluation")
	s.Require().NoError(NewAlertService(params).(*alertService).evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, at))
	s.Len(usageStore.queries, 1, "opening and evaluation should read usage only once")
	stored, err := s.GetStores().EntitlementGrantRepo.Get(ctx, grants[0].ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(80)))
	remaining, bounded := stored.Remaining()
	s.True(bounded)
	s.True(remaining.Equal(decimal.NewFromInt(20)))
	s.Equal(types.EntitlementGrantStatusActive, stored.GrantStatus)
	s.Nil(stored.QuotaCrossedAt)
}

type recordingGrantUsageStore struct {
	events.MeterUsageRepository
	queries []*events.MeterUsageQueryParams
}

func (r *recordingGrantUsageStore) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	r.queries = append(r.queries, params)
	return r.MeterUsageRepository.GetUsage(ctx, params)
}

func (s *EntitlementGrantSuite) TestRefreshNeverGrantReadsCheckpointTailOnce() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageNever)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(-time.Hour), 100)
	params := s.buildServiceParams()
	_, err := NewMeterUsageService(params).SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: sub.CurrentPeriodStart,
	}, nil)
	s.Require().NoError(err)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(time.Hour), -20)
	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	recorder := &recordingGrantUsageStore{MeterUsageRepository: params.MeterUsageRepo}
	params.MeterUsageRepo = recorder
	usage, err := NewAlertService(params).(*alertService).refreshEntitlementGrantUsage(ctx, grants[0], m, sub, []string{cust.ExternalID}, at)
	s.Require().NoError(err)
	s.True(usage.Equal(decimal.NewFromInt(80)))
	s.Require().Len(recorder.queries, 1, "checkpoint helper reads the tail once; caller must not query the period again")
	s.True(recorder.queries[0].StartTime.Equal(sub.CurrentPeriodStart))
	s.True(recorder.queries[0].EndTime.Equal(at))
}

func (s *EntitlementGrantSuite) TestReissue_NeverCycleGrantUsesFullQuotaAndRecovers() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageNever)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(-time.Hour), 40)
	_, err := NewMeterUsageService(s.buildServiceParams()).SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: sub.CurrentPeriodStart,
	}, nil)
	s.Require().NoError(err)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(time.Hour), 80)
	before := sub.CurrentPeriodStart.Add(90 * time.Minute)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, before)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	evaluator := s.alertService.(*alertService)
	s.Require().NoError(evaluator.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, before))
	original, err := s.GetStores().EntitlementGrantRepo.Get(ctx, grants[0].ID)
	s.Require().NoError(err)
	s.Equal("120", original.Usage.String())
	s.Equal(types.EntitlementGrantStatusExhausted, original.GrantStatus)

	ec, err := s.GetStores().EntitlementRepo.Get(ctx, "ec-recovery")
	s.Require().NoError(err)
	replacement := *ec
	replacement.GrantQuota = lo.ToPtr(decimal.NewFromInt(150))
	_, err = s.GetStores().EntitlementRepo.Update(ctx, &replacement)
	s.Require().NoError(err)
	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	opened, err := s.grantService.ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
		SubscriptionID: sub.ID, FeatureID: ec.FeatureID, Delta: decimal.NewFromInt(50), At: at,
	})
	s.Require().NoError(err)
	s.Require().Len(opened, 1)
	successor := opened[0]
	s.Equal("150", successor.Quota.String(), "full replacement, not exhausted remaining + delta")
	s.True(successor.ValidFrom.Equal(at))
	s.True(successor.ValidTo.Equal(sub.CurrentPeriodEnd))

	for i, step := range []struct{ delta, usage, remaining int64 }{
		{0, 120, 30}, {-40, 80, 70}, {100, 180, 0}, {-110, 70, 80},
	} {
		evaluatedAt := at.Add(time.Duration(i+1) * time.Hour)
		s.seedMeterUsage(cust.ExternalID, m.ID, evaluatedAt.Add(-time.Minute), step.delta)
		grants, meta, err = s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, evaluatedAt)
		s.Require().NoError(err)
		s.Require().NoError(evaluator.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, evaluatedAt))
		stored, err := s.GetStores().EntitlementGrantRepo.Get(ctx, successor.ID)
		s.Require().NoError(err)
		s.Equal(decimal.NewFromInt(step.usage).String(), stored.Usage.String())
		remaining, _ := stored.Remaining()
		s.Equal(decimal.NewFromInt(step.remaining).String(), remaining.String())
		state, err := s.grantService.GrantStateByFeature(ctx, sub, evaluatedAt)
		s.Require().NoError(err)
		s.Require().Len(state[ec.FeatureID].Allowances, 2)
		figures, ok := grantUsageFigures(state[ec.FeatureID])
		s.True(ok)
		s.Equal(stored.Usage.String(), figures.usage.String(), "summary must not sum predecessor and successor usage")
		s.Equal("150", figures.quota.String())
	}
	closed, err := s.GetStores().EntitlementGrantRepo.Get(ctx, original.ID)
	s.Require().NoError(err)
	s.True(closed.ValidTo.Equal(at))
	s.Equal("100", closed.Quota.String())
	s.Equal("120", closed.Usage.String(), "later corrections don't rewrite a finalized predecessor")
}

func (s *EntitlementGrantSuite) TestReissue_NeverUnevaluatedGrantKeepsChangeBoundary() {
	ctx := s.GetContext()
	cust, sub, _ := s.recoveryFixture(types.ResetUsageNever)
	at := sub.CurrentPeriodStart.Add(time.Hour)
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	ec, err := s.GetStores().EntitlementRepo.Get(ctx, "ec-recovery")
	s.Require().NoError(err)
	replacement := *ec
	replacement.GrantQuota = lo.ToPtr(decimal.NewFromInt(120))
	_, err = s.GetStores().EntitlementRepo.Update(ctx, &replacement)
	s.Require().NoError(err)
	opened, err := s.grantService.ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
		SubscriptionID: sub.ID, FeatureID: ec.FeatureID, Delta: decimal.NewFromInt(20), At: at,
	})
	s.Require().NoError(err)
	s.Require().Len(opened, 1)
	s.Equal("120", opened[0].Quota.String())
	s.True(opened[0].ValidFrom.Equal(at), "do not backdate to an unevaluated predecessor's start")
	closed, err := s.GetStores().EntitlementGrantRepo.Get(ctx, grants[0].ID)
	s.Require().NoError(err)
	s.True(closed.ValidTo.Equal(at))
}

func (s *EntitlementGrantSuite) TestReissue_PeriodResetKeepsRemainingPlusDelta() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageBillingPeriod)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(time.Hour), 80)
	before := sub.CurrentPeriodStart.Add(90 * time.Minute)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, before)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	evaluator := s.alertService.(*alertService)
	s.Require().NoError(evaluator.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, before))
	ec, err := s.GetStores().EntitlementRepo.Get(ctx, "ec-recovery")
	s.Require().NoError(err)
	replacement := *ec
	replacement.GrantQuota = lo.ToPtr(decimal.NewFromInt(120))
	_, err = s.GetStores().EntitlementRepo.Update(ctx, &replacement)
	s.Require().NoError(err)
	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	opened, err := s.grantService.ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
		SubscriptionID: sub.ID, FeatureID: ec.FeatureID, Delta: decimal.NewFromInt(20), At: at,
	})
	s.Require().NoError(err)
	s.Require().Len(opened, 1)
	s.Equal("40", opened[0].Quota.String())
	s.seedMeterUsage(cust.ExternalID, m.ID, at.Add(time.Minute), 10)
	after := at.Add(2 * time.Minute)
	grants, meta, err = s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, after)
	s.Require().NoError(err)
	s.Require().NoError(evaluator.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, after))
	stored, err := s.GetStores().EntitlementGrantRepo.Get(ctx, opened[0].ID)
	s.Require().NoError(err)
	s.Equal("10", stored.Usage.String())
	remaining, _ := stored.Remaining()
	s.Equal("30", remaining.String())
}

func (s *EntitlementGrantSuite) TestOverride_NeverCycleQuotaEditsUseFullQuota() {
	ctx := s.GetContext()
	cust, sub, m := s.recoveryFixture(types.ResetUsageNever)
	sub.StartDate = sub.StartDate.Add(-2 * time.Hour)
	sub.CurrentPeriodStart = sub.StartDate
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(-time.Hour), 40)
	_, err := NewMeterUsageService(s.buildServiceParams()).SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: sub.CurrentPeriodStart,
	}, nil)
	s.Require().NoError(err)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(time.Hour), 40)
	// Give the live window an evaluated snapshot; each API update owns a new boundary.
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	original := grants[0]
	original = entitlementgrant.NewEntitlementGrantBuilder(original).
		WithUsage(decimal.NewFromInt(80)).WithLastComputedAt(lo.ToPtr(time.Now().UTC())).Build()
	s.Require().NoError(s.GetStores().EntitlementGrantRepo.UpdateSnapshot(ctx, original))
	created, err := s.entService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: sub.ID,
		FeatureID: "feature-recovery", FeatureType: types.FeatureTypeMetered, IsEnabled: true,
		ParentEntitlementID: lo.ToPtr("ec-recovery"), GrantMeasure: types.EntitlementGrantMeasureQuantity,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod, GrantQuota: lo.ToPtr(decimal.NewFromInt(120)),
	})
	s.Require().NoError(err)
	assertQuota := func(want, usage string, status types.EntitlementGrantStatus) {
		live, err := s.grantService.LiveGrantsByFeature(ctx, sub, time.Now().UTC())
		s.Require().NoError(err)
		s.Require().Len(live["feature-recovery"], 1)
		g := live["feature-recovery"][0]
		s.Equal(want, g.Quota.String())
		s.Equal(usage, g.Usage.String())
		s.Equal(status, g.GrantStatus)
		s.NotNil(g.LastComputedAt)
		if status == types.EntitlementGrantStatusExhausted {
			s.NotNil(g.QuotaCrossedAt)
		} else {
			s.Nil(g.QuotaCrossedAt)
		}
	}
	assertQuota("120", "80", types.EntitlementGrantStatusActive)
	_, err = s.entService.UpdateEntitlement(ctx, created.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(50)),
	})
	s.Require().NoError(err)
	assertQuota("50", "80", types.EntitlementGrantStatusExhausted)
	_, err = s.entService.UpdateEntitlement(ctx, created.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(150)),
	})
	s.Require().NoError(err)
	assertQuota("150", "80", types.EntitlementGrantStatusActive)
	s.seedMeterUsage(cust.ExternalID, m.ID, time.Now().UTC().Add(-time.Minute), -100)
	s.Require().NoError(s.entService.DeleteEntitlement(ctx, created.ID))
	assertQuota("100", "0", types.EntitlementGrantStatusActive)
}

func (s *EntitlementGrantSuite) TestOverride_NeverBeforeFirstEvaluationOpensAtChange() {
	ctx := s.GetContext()
	_, sub, _ := s.recoveryFixture(types.ResetUsageNever)
	before := time.Now().UTC()
	_, err := s.entService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: sub.ID,
		FeatureID: "feature-recovery", FeatureType: types.FeatureTypeMetered, IsEnabled: true,
		ParentEntitlementID: lo.ToPtr("ec-recovery"), GrantMeasure: types.EntitlementGrantMeasureQuantity,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod, GrantQuota: lo.ToPtr(decimal.NewFromInt(120)),
	})
	s.Require().NoError(err)
	live, err := s.grantService.LiveGrantsByFeature(ctx, sub, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Len(live["feature-recovery"], 1)
	s.Equal("120", live["feature-recovery"][0].Quota.String())
	s.False(live["feature-recovery"][0].ValidFrom.Before(before))
}

func (s *EntitlementGrantSuite) TestOpenGrants_UsesFinalQuotaWithoutAddingCarryForward() {
	ctx := s.GetContext()
	cust, sub, _ := s.recoveryFixture(types.ResetUsageNever)
	ec, err := s.GetStores().EntitlementRepo.Get(ctx, "ec-recovery")
	s.Require().NoError(err)
	// The caller supplies the final quota, including any carry-forward.
	ec.GrantQuota = lo.ToPtr(decimal.NewFromInt(120))
	at := sub.CurrentPeriodStart.Add(time.Hour)
	closed := entitlementgrant.NewEntitlementGrantBuilder(nil).
		WithEntitlementConfigID(ec.ID).WithCustomerID(cust.ID).WithSubscriptionID(sub.ID).
		WithScope(types.EntitlementGrantScopeFeature, ec.FeatureID).
		WithMeasure(types.EntitlementGrantMeasureQuantity).
		WithQuota(decimal.NewFromInt(100)).WithUsage(decimal.NewFromInt(80)).
		WithWindow(sub.CurrentPeriodStart, at).WithBaseModel(types.GetDefaultBaseModel(ctx)).Build()
	opened, err := s.grantService.OpenFeatureBasedEntitlementGrants(ctx, []OpenFeatureBasedEntitlementGrantsRequest{{
		FeatureID: ec.FeatureID, Closed: closed,
		New: entitlementgrant.NewEntitlementGrantBuilder(closed).
			WithQuota(decimal.NewFromInt(40)).WithWindow(at, sub.CurrentPeriodEnd).Build(),
		ExistingECs: []*entitlement.Entitlement{ec},
	}})
	s.Require().NoError(err)
	s.Require().Len(opened, 1)
	s.Equal("40", opened[0].Quota.String())
}

func (s *EntitlementGrantSuite) TestSubscriptionOverride_ValidatesNeverGrantShapeAtSetup() {
	_, sub, _ := s.recoveryFixture(types.ResetUsageNever)
	err := NewSubscriptionService(s.buildServiceParams()).(*subscriptionService).ProcessSubscriptionEntitlementOverrides(s.GetContext(), sub, []dto.OverrideEntitlementRequest{{
		EntitlementID: "ec-recovery", GrantConfigPatch: dto.GrantConfigPatch{GrantMeasure: lo.ToPtr(types.EntitlementGrantMeasureAmount)},
	}})
	s.Require().Error(err)
	s.Contains(err.Error(), "quantity measure")
}
