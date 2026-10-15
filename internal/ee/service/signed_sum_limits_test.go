package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestValidateSignedSumGrantShape(t *testing.T) {
	m := &meter.Meter{ID: "meter_net", Aggregation: meter.Aggregation{Type: types.AggregationSignedSum}}
	ceiling := func(mutate func(*entitlement.Entitlement)) *entitlement.Entitlement {
		e := &entitlement.Entitlement{
			GrantMeasure:      types.EntitlementGrantMeasureQuantity,
			GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
			AggregationMode:   types.EntitlementAggregationModeAdditive,
		}
		mutate(e)
		return e
	}

	tests := []struct {
		name    string
		e       *entitlement.Entitlement
		wantErr bool
	}{
		{"quantity ceiling per period is allowed", ceiling(func(*entitlement.Entitlement) {}), false},
		{"empty mode defaults to additive", ceiling(func(e *entitlement.Entitlement) { e.AggregationMode = "" }), false},
		{"amount measure is rejected", ceiling(func(e *entitlement.Entitlement) { e.GrantMeasure = types.EntitlementGrantMeasureAmount }), true},
		{"parallel is rejected", ceiling(func(e *entitlement.Entitlement) { e.AggregationMode = types.EntitlementAggregationModeParallel }), true},
		{"day windows are rejected", ceiling(func(e *entitlement.Entitlement) { e.GrantDurationUnit = types.EntitlementGrantDurationUnitDay }), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSignedSumGrantShape(tt.e, m)
			require.Equal(t, tt.wantErr, err != nil, "err = %v", err)
		})
	}
}

// A usage_limit on a SIGNED_SUM feature becomes the grant config's ceiling; a grant shape
// other than a quantity ceiling per period is refused.
func (s *EntitlementServiceSuite) TestCreateEntitlement_SignedSum() {
	ctx := s.GetContext()
	signedSumMeter := &meter.Meter{
		ID:          "meter-signed-sum",
		Name:        "Storage",
		EventName:   "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"},
		ResetUsage:  types.ResetUsageNever,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.service.(*entitlementService).MeterRepo.CreateMeter(ctx, signedSumMeter))
	storage := &feature.Feature{ID: "feat-storage", Name: "Storage", Type: types.FeatureTypeMetered, MeterID: signedSumMeter.ID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, storage))
	s.NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{ID: "plan-signed-sum", Name: "Storage plan", BaseModel: types.GetDefaultBaseModel(ctx)}))

	resp, err := s.service.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		PlanID:      "plan-signed-sum",
		FeatureID:   storage.ID,
		FeatureType: types.FeatureTypeMetered,
		IsEnabled:   true,
		UsageLimit:  lo.ToPtr(int64(100)),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp.GrantQuota)
	s.True(decimal.NewFromInt(100).Equal(*resp.GrantQuota))
	s.Equal(types.EntitlementGrantMeasureQuantity, resp.GrantMeasure)
	s.Equal(types.EntitlementGrantDurationUnitSubscriptionPeriod, resp.GrantDurationUnit)

	s.NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{ID: "plan-signed-sum-2", Name: "Storage plan 2", BaseModel: types.GetDefaultBaseModel(ctx)}))
	_, err = s.service.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		PlanID:             "plan-signed-sum-2",
		FeatureID:          storage.ID,
		FeatureType:        types.FeatureTypeMetered,
		IsEnabled:          true,
		GrantMeasure:       types.EntitlementGrantMeasureQuantity,
		GrantDurationUnit:  types.EntitlementGrantDurationUnitDay,
		GrantDurationValue: lo.ToPtr(1),
		GrantQuota:         lo.ToPtr(decimal.NewFromInt(100)),
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "SIGNED_SUM", "day windows are not a ceiling")
}

func TestGrantEvalMeta_IsSignedSumFeature(t *testing.T) {
	meta := &grantEvalMeta{
		featureByID: map[string]*feature.Feature{
			"feat_net": {ID: "feat_net", MeterID: "meter_net"},
			"feat_sum": {ID: "feat_sum", MeterID: "meter_sum"},
		},
		meterByID: map[string]*meter.Meter{
			"meter_net": {ID: "meter_net", Aggregation: meter.Aggregation{Type: types.AggregationSignedSum}},
			"meter_sum": {ID: "meter_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}},
		},
	}
	require.True(t, meta.isSignedSumFeature("feat_net"))
	require.False(t, meta.isSignedSumFeature("feat_sum"))
	require.False(t, meta.isSignedSumFeature("feat_unknown"))
	require.False(t, (*grantEvalMeta)(nil).isSignedSumFeature("feat_net"))
}

// The usage summary reports a SIGNED_SUM feature's exact usage: the running total for a
// meter that never resets, this period's net otherwise. Negative values are kept.
func (s *BillingServiceSuite) TestSetSignedSumSummaryUsage() {
	ctx := s.GetContext()
	svc := s.service.(*billingService)
	svc.MeterUsageCheckpointRepo = s.GetStores().MeterUsageCheckpointRepo

	sub := s.testData.subscription
	running := &meter.Meter{ID: "meter_running", ResetUsage: types.ResetUsageNever, Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"}}
	periodic := &meter.Meter{ID: "meter_periodic", ResetUsage: types.ResetUsageBillingPeriod, Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"}}
	for _, m := range []*meter.Meter{running, periodic} {
		m.Name, m.EventName, m.BaseModel = m.ID, "storage", types.GetDefaultBaseModel(ctx)
		s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	}

	ext := s.testData.customer.ExternalID
	insert := func(meterID string, ts time.Time, qty int64) {
		s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: ext,
				EventName:          "storage",
				Timestamp:          ts,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromInt(qty),
		}}))
	}
	before := sub.CurrentPeriodStart.Add(-24 * time.Hour)
	during := sub.CurrentPeriodStart.Add(time.Hour)
	insert(running.ID, before, 40)
	insert(running.ID, during, 6)
	insert(periodic.ID, before, 40)
	insert(periodic.ID, during, -3)

	featureMeterMap := map[string]string{"feat_running": running.ID, "feat_periodic": periodic.ID}
	featureSubs := map[string]*subscription.Subscription{"feat_running": sub, "feat_periodic": sub}
	signedSumMeters, err := svc.signedSumMetersByID(ctx, []string{running.ID, periodic.ID, "meter_missing"})
	s.Require().NoError(err)
	s.Require().Len(signedSumMeters, 2)

	usageByFeature := map[string]decimal.Decimal{}
	s.Require().NoError(svc.setSignedSumSummaryUsage(ctx, sub, []string{ext}, signedSumMeters, featureMeterMap, featureSubs, usageByFeature))

	s.True(decimal.NewFromInt(46).Equal(usageByFeature["feat_running"]), "running total, got %s", usageByFeature["feat_running"])
	s.True(decimal.NewFromInt(-3).Equal(usageByFeature["feat_periodic"]), "period net stays exact, got %s", usageByFeature["feat_periodic"])
}
