package dto

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

func planChangeScheduleRow() *subscription.SubscriptionSchedule {
	return &subscription.SubscriptionSchedule{
		ID:             "sched_1",
		SubscriptionID: "subs_1",
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Status:         types.ScheduleStatusPending,
	}
}

func TestSubscriptionScheduleResponse_RendersV2Config(t *testing.T) {
	row := planChangeScheduleRow()
	if err := row.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{
		TargetPlanID: "plan_target",
		EntityPolicies: &subscription.EntityChangePoliciesConfig{
			Addons: &subscription.EntityChangePolicyConfig{
				DefaultBehaviour: types.EntityChangeBehaviourDrop,
			},
		},
		ChangeMetadata: map[string]string{"source": "api"},
	}); err != nil {
		t.Fatalf("SetPlanChangeV2Config: %v", err)
	}

	resp := SubscriptionScheduleResponseFromDomain(row)

	config, ok := resp.Configuration.(*subscription.PlanChangeV2Configuration)
	if !ok {
		t.Fatalf("Configuration = %T, want *PlanChangeV2Configuration", resp.Configuration)
	}
	if config.TargetPlanID != "plan_target" {
		t.Fatalf("target plan not rendered: %+v", config)
	}
	if config.EntityPolicies == nil || config.EntityPolicies.Addons == nil ||
		config.EntityPolicies.Addons.DefaultBehaviour != types.EntityChangeBehaviourDrop {
		t.Fatalf("entity policies not rendered: %+v", config.EntityPolicies)
	}
	if config.ChangeMetadata["source"] != "api" {
		t.Fatalf("metadata not rendered: %+v", config.ChangeMetadata)
	}
	if resp.ExecutionResult != nil {
		t.Fatalf("a pending schedule has no execution result, got %+v", resp.ExecutionResult)
	}
}

func TestSubscriptionScheduleResponse_RendersV2Result(t *testing.T) {
	row := planChangeScheduleRow()
	if err := row.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{TargetPlanID: "plan_b"}); err != nil {
		t.Fatalf("SetPlanChangeV2Config: %v", err)
	}
	effective := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := row.SetPlanChangeV2Result(&subscription.PlanChangeV2Result{
		SubscriptionID: "subs_1",
		FromPlanID:     "plan_a",
		ToPlanID:       "plan_b",
		ChangeType:     "upgrade",
		EffectiveDate:  effective,
	}); err != nil {
		t.Fatalf("SetPlanChangeV2Result: %v", err)
	}
	row.Status = types.ScheduleStatusExecuted

	resp := SubscriptionScheduleResponseFromDomain(row)

	result, ok := resp.ExecutionResult.(*subscription.PlanChangeV2Result)
	if !ok {
		t.Fatalf("ExecutionResult = %T, want *PlanChangeV2Result", resp.ExecutionResult)
	}
	if result.FromPlanID != "plan_a" || result.ToPlanID != "plan_b" || result.ChangeType != "upgrade" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.EffectiveDate.Equal(effective) {
		t.Fatalf("effective date = %v, want %v", result.EffectiveDate, effective)
	}
}

// A v1 row must keep rendering through the v1 shape, including its old/new
// subscription ids, which have no equivalent in the v2 result.
func TestSubscriptionScheduleResponse_V1RowUnchanged(t *testing.T) {
	row := planChangeScheduleRow()
	if err := row.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
		TargetPlanID:       "plan_target",
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
	}); err != nil {
		t.Fatalf("SetPlanChangeConfig: %v", err)
	}
	if err := row.SetPlanChangeResult(&subscription.PlanChangeResult{
		OldSubscriptionID: "subs_old",
		NewSubscriptionID: "subs_new",
		ChangeType:        "upgrade",
	}); err != nil {
		t.Fatalf("SetPlanChangeResult: %v", err)
	}

	resp := SubscriptionScheduleResponseFromDomain(row)

	config, ok := resp.Configuration.(*subscription.PlanChangeConfiguration)
	if !ok {
		t.Fatalf("Configuration = %T, want *PlanChangeConfiguration", resp.Configuration)
	}
	if config.BillingPeriod != types.BILLING_PERIOD_MONTHLY ||
		config.ProrationBehavior != types.ProrationBehaviorCreateProrations {
		t.Fatalf("v1 fields lost: %+v", config)
	}

	result, ok := resp.ExecutionResult.(*subscription.PlanChangeResult)
	if !ok {
		t.Fatalf("ExecutionResult = %T, want *PlanChangeResult", resp.ExecutionResult)
	}
	if result.OldSubscriptionID != "subs_old" || result.NewSubscriptionID != "subs_new" {
		t.Fatalf("unexpected v1 result: %+v", result)
	}
}
