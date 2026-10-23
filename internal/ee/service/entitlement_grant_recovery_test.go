package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
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
	at := sub.CurrentPeriodStart.Add(time.Hour)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, at))
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
