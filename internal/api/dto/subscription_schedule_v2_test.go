package dto

import (
	"reflect"
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

	config := resp.Configuration
	if config == nil {
		t.Fatalf("Configuration is nil, want rendered v2 config")
	}
	if config["target_plan_id"] != "plan_target" {
		t.Fatalf("target plan not rendered: %+v", config)
	}
	addons, _ := digMap(config, "entity_policies", "addons")
	if addons == nil || addons["default_behaviour"] != string(types.EntityChangeBehaviourDrop) {
		t.Fatalf("entity policies not rendered: %+v", config["entity_policies"])
	}
	meta, _ := config["change_metadata"].(map[string]interface{})
	if meta == nil || meta["source"] != "api" {
		t.Fatalf("metadata not rendered: %+v", config["change_metadata"])
	}
	if resp.ExecutionResult != nil {
		t.Fatalf("a pending schedule has no execution result, got %+v", resp.ExecutionResult)
	}
}

// digMap walks nested map[string]interface{} keys.
func digMap(m map[string]interface{}, keys ...string) (map[string]interface{}, bool) {
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
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

	result := resp.ExecutionResult
	if result == nil {
		t.Fatalf("ExecutionResult is nil, want rendered v2 result")
	}
	if result["from_plan_id"] != "plan_a" || result["to_plan_id"] != "plan_b" || result["change_type"] != "upgrade" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result["effective_date"] != effective.Format(time.RFC3339) {
		t.Fatalf("effective date = %v, want %v", result["effective_date"], effective.Format(time.RFC3339))
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

	config := resp.Configuration
	if config == nil {
		t.Fatalf("Configuration is nil, want rendered v1 config")
	}
	if config["billing_period"] != string(types.BILLING_PERIOD_MONTHLY) ||
		config["proration_behavior"] != string(types.ProrationBehaviorCreateProrations) {
		t.Fatalf("v1 fields lost: %+v", config)
	}

	result := resp.ExecutionResult
	if result == nil {
		t.Fatalf("ExecutionResult is nil, want rendered v1 result")
	}
	if result["old_subscription_id"] != "subs_old" || result["new_subscription_id"] != "subs_new" {
		t.Fatalf("unexpected v1 result: %+v", result)
	}
}

func TestSubscriptionScheduleResponse_TypedPlanChange(t *testing.T) {
	effective := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		row        func(t *testing.T) *subscription.SubscriptionSchedule
		wantConfig *ConfigurationDetails
		wantExec   *ExecutionDetails
	}{
		{
			name: "v2 pending",
			row: func(t *testing.T) *subscription.SubscriptionSchedule {
				row := planChangeScheduleRow()
				if err := row.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{
					TargetPlanID:           "plan_target",
					BillingPeriodBehaviour: types.BillingPeriodBehaviourUnchanged,
					EntityPolicies: &subscription.EntityChangePoliciesConfig{
						Addons: &subscription.EntityChangePolicyConfig{DefaultBehaviour: types.EntityChangeBehaviourDrop},
					},
					ChangeMetadata: map[string]string{"source": "api"},
				}); err != nil {
					t.Fatalf("SetPlanChangeV2Config: %v", err)
				}
				return row
			},
			wantConfig: &ConfigurationDetails{PlanChange: &PlanChangeScheduleDetails{
				TargetPlanID:           "plan_target",
				BillingPeriodBehaviour: types.BillingPeriodBehaviourUnchanged,
				EntityPolicies: &SubscriptionChangeEntityPolicies{
					Addons: &EntityChangePolicy{DefaultBehaviour: types.EntityChangeBehaviourDrop},
				},
			}},
		},
		{
			name: "v2 executed",
			row: func(t *testing.T) *subscription.SubscriptionSchedule {
				row := planChangeScheduleRow()
				if err := row.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{TargetPlanID: "plan_b"}); err != nil {
					t.Fatalf("SetPlanChangeV2Config: %v", err)
				}
				if err := row.SetPlanChangeV2Result(&subscription.PlanChangeV2Result{
					SubscriptionID: "subs_1", FromPlanID: "plan_a", ToPlanID: "plan_b",
					ChangeType: "downgrade", EffectiveDate: effective,
				}); err != nil {
					t.Fatalf("SetPlanChangeV2Result: %v", err)
				}
				return row
			},
			wantConfig: &ConfigurationDetails{PlanChange: &PlanChangeScheduleDetails{TargetPlanID: "plan_b"}},
			wantExec: &ExecutionDetails{PlanChange: &PlanChangeScheduleResult{
				SubscriptionID: "subs_1", FromPlanID: "plan_a", ToPlanID: "plan_b",
				ChangeType: "downgrade", EffectiveDate: effective,
			}},
		},
		{
			name: "v1 executed",
			row: func(t *testing.T) *subscription.SubscriptionSchedule {
				row := planChangeScheduleRow()
				if err := row.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
					TargetPlanID:   "plan_target",
					ChangeMetadata: map[string]string{"source": "v1"},
				}); err != nil {
					t.Fatalf("SetPlanChangeConfig: %v", err)
				}
				if err := row.SetPlanChangeResult(&subscription.PlanChangeResult{
					OldSubscriptionID: "subs_old", NewSubscriptionID: "subs_new",
					ChangeType: "upgrade", EffectiveDate: effective,
				}); err != nil {
					t.Fatalf("SetPlanChangeResult: %v", err)
				}
				return row
			},
			wantConfig: &ConfigurationDetails{PlanChange: &PlanChangeScheduleDetails{
				TargetPlanID: "plan_target",
			}},
			wantExec: &ExecutionDetails{PlanChange: &PlanChangeScheduleResult{
				SubscriptionID: "subs_new", PreviousSubscriptionID: "subs_old",
				ChangeType: "upgrade", EffectiveDate: effective,
			}},
		},
		{
			name: "cancellation has no plan change",
			row: func(t *testing.T) *subscription.SubscriptionSchedule {
				row := planChangeScheduleRow()
				row.ScheduleType = types.SubscriptionScheduleChangeTypeCancellation
				return row
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := SubscriptionScheduleResponseFromDomain(tt.row(t))
			if !reflect.DeepEqual(resp.ConfigurationDetails, tt.wantConfig) {
				t.Fatalf("ConfigurationDetails = %+v, want %+v", resp.ConfigurationDetails, tt.wantConfig)
			}
			if !reflect.DeepEqual(resp.ExecutionDetails, tt.wantExec) {
				t.Fatalf("ExecutionDetails = %+v, want %+v", resp.ExecutionDetails, tt.wantExec)
			}
		})
	}
}
