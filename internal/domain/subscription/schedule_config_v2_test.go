package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func newPlanChangeSchedule() *SubscriptionSchedule {
	return &SubscriptionSchedule{
		ID:           "sched_123",
		ScheduleType: types.SubscriptionScheduleChangeTypePlanChange,
	}
}

func TestPlanChangeV2Config_RoundTrip(t *testing.T) {
	s := newPlanChangeSchedule()
	config := &PlanChangeV2Configuration{
		TargetPlanID: "plan_target",
		EntityPolicies: &EntityChangePoliciesConfig{
			Addons: &EntityChangePolicyConfig{
				DefaultBehaviour: types.EntityChangeBehaviourCarry,
				Overrides: map[string]types.EntityChangeBehaviour{
					"addon_assoc_1": types.EntityChangeBehaviourDrop,
				},
			},
		},
		IdempotencyKey: lo.ToPtr("idem_1"),
		ChangeMetadata: map[string]string{"source": "api"},
	}

	if err := s.SetPlanChangeV2Config(config); err != nil {
		t.Fatalf("SetPlanChangeV2Config: %v", err)
	}
	if config.Version != PlanChangeConfigVersionV2 {
		t.Fatalf("expected version to be set to %q, got %q", PlanChangeConfigVersionV2, config.Version)
	}

	got, err := s.GetPlanChangeV2Config()
	if err != nil {
		t.Fatalf("GetPlanChangeV2Config: %v", err)
	}
	if got.Version != PlanChangeConfigVersionV2 {
		t.Fatalf("version = %q, want %q", got.Version, PlanChangeConfigVersionV2)
	}
	if got.TargetPlanID != "plan_target" {
		t.Fatalf("unexpected config: %+v", got)
	}
	if got.EntityPolicies == nil || got.EntityPolicies.Addons == nil {
		t.Fatalf("unexpected entity policies: %+v", got.EntityPolicies)
	}
	if got.EntityPolicies.Addons.DefaultBehaviour != types.EntityChangeBehaviourCarry {
		t.Fatalf("unexpected addon policy: %+v", got.EntityPolicies.Addons)
	}
	if got.EntityPolicies.Addons.Overrides["addon_assoc_1"] != types.EntityChangeBehaviourDrop {
		t.Fatalf("unexpected overrides: %+v", got.EntityPolicies.Addons.Overrides)
	}
	if lo.FromPtr(got.IdempotencyKey) != "idem_1" || got.ChangeMetadata["source"] != "api" {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestPlanChangeV2Result_RoundTrip(t *testing.T) {
	s := newPlanChangeSchedule()

	result, err := s.GetPlanChangeV2Result()
	if err != nil || result != nil {
		t.Fatalf("expected nil result with no error, got %+v / %v", result, err)
	}

	effective := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	if err := s.SetPlanChangeV2Result(&PlanChangeV2Result{
		SubscriptionID: "sub_1",
		FromPlanID:     "plan_a",
		ToPlanID:       "plan_b",
		ChangeType:     "upgrade",
		EffectiveDate:  effective,
	}); err != nil {
		t.Fatalf("SetPlanChangeV2Result: %v", err)
	}

	got, err := s.GetPlanChangeV2Result()
	if err != nil {
		t.Fatalf("GetPlanChangeV2Result: %v", err)
	}
	if got.SubscriptionID != "sub_1" || got.FromPlanID != "plan_a" || got.ToPlanID != "plan_b" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if !got.EffectiveDate.Equal(effective) {
		t.Fatalf("effective date = %v, want %v", got.EffectiveDate, effective)
	}
}

func TestPlanChangeV2Config_WrongScheduleType(t *testing.T) {
	s := &SubscriptionSchedule{ScheduleType: types.SubscriptionScheduleChangeTypeCancellation}

	if err := s.SetPlanChangeV2Config(&PlanChangeV2Configuration{}); err != ErrInvalidScheduleType {
		t.Fatalf("SetPlanChangeV2Config err = %v, want ErrInvalidScheduleType", err)
	}
	if _, err := s.GetPlanChangeV2Config(); err != ErrInvalidScheduleType {
		t.Fatalf("GetPlanChangeV2Config err = %v, want ErrInvalidScheduleType", err)
	}
	if err := s.SetPlanChangeV2Result(&PlanChangeV2Result{}); err != ErrInvalidScheduleType {
		t.Fatalf("SetPlanChangeV2Result err = %v, want ErrInvalidScheduleType", err)
	}
	if _, err := s.GetPlanChangeV2Result(); err != ErrInvalidScheduleType {
		t.Fatalf("GetPlanChangeV2Result err = %v, want ErrInvalidScheduleType", err)
	}
}

func TestPlanChangeV2Config_IsV2(t *testing.T) {
	v1 := newPlanChangeSchedule()
	if err := v1.SetPlanChangeConfig(&PlanChangeConfiguration{
		TargetPlanID:       "plan_target",
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
	}); err != nil {
		t.Fatalf("SetPlanChangeConfig: %v", err)
	}
	v1Config, err := v1.GetPlanChangeV2Config()
	if err != nil {
		t.Fatalf("a v1 blob must still decode into the v2 struct, got %v", err)
	}
	if v1Config.IsV2() {
		t.Fatal("IsV2() = true for a v1 config blob")
	}

	v2 := newPlanChangeSchedule()
	if err := v2.SetPlanChangeV2Config(&PlanChangeV2Configuration{TargetPlanID: "plan_target"}); err != nil {
		t.Fatalf("SetPlanChangeV2Config: %v", err)
	}
	v2Config, err := v2.GetPlanChangeV2Config()
	if err != nil {
		t.Fatalf("GetPlanChangeV2Config: %v", err)
	}
	if !v2Config.IsV2() {
		t.Fatal("IsV2() = false for a v2 config blob")
	}

	var nilConfig *PlanChangeV2Configuration
	if nilConfig.IsV2() {
		t.Fatal("IsV2() = true for a nil config")
	}

	malformed := newPlanChangeSchedule()
	malformed.Configuration = []byte("not json")
	if _, err := malformed.GetPlanChangeV2Config(); err == nil {
		t.Fatal("expected a decode error for a malformed configuration")
	}
}

func TestSubscriptionSchedule_IsStaleFor(t *testing.T) {
	boundary := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		scheduledAt time.Time
		periodStart time.Time
		expected    bool
	}{
		{
			name:        "on time: the roll set period start to the boundary itself",
			scheduledAt: boundary,
			periodStart: boundary,
			expected:    false,
		},
		{
			name:        "not yet due: boundary is still ahead of the current period start",
			scheduledAt: boundary,
			periodStart: boundary.AddDate(0, -1, 0),
			expected:    false,
		},
		{
			name:        "missed: a catch-up roll jumped past the boundary",
			scheduledAt: boundary,
			periodStart: boundary.AddDate(0, 1, 0),
			expected:    true,
		},
		{
			name:        "missed by a moment",
			scheduledAt: boundary,
			periodStart: boundary.Add(time.Nanosecond),
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SubscriptionSchedule{ScheduledAt: tt.scheduledAt}
			sub := &Subscription{CurrentPeriodStart: tt.periodStart}
			if got := s.IsStaleFor(sub); got != tt.expected {
				t.Fatalf("IsStaleFor() = %v, want %v", got, tt.expected)
			}
		})
	}

	if (&SubscriptionSchedule{ScheduledAt: boundary}).IsStaleFor(nil) {
		t.Fatal("IsStaleFor(nil) = true; a missing subscription is not evidence of staleness")
	}
}

func TestNewPendingScheduleBuilder_FillsBoilerplate(t *testing.T) {
	ctx := context.Background()
	sub := &Subscription{ID: "subs_1", EnvironmentID: "env_1", BaseModel: types.BaseModel{TenantID: "tenant_1"}}
	scheduledAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	schedule := NewPendingScheduleBuilder(
		ctx, sub, types.SubscriptionScheduleChangeTypePlanChange, scheduledAt,
	).Build()
	if err := schedule.SetPlanChangeV2Config(&PlanChangeV2Configuration{TargetPlanID: "plan_b"}); err != nil {
		t.Fatalf("SetPlanChangeV2Config: %v", err)
	}

	if schedule.ID == "" || schedule.CreatedAt.IsZero() || schedule.UpdatedAt.IsZero() {
		t.Fatalf("boilerplate not filled: %+v", schedule)
	}
	if schedule.SubscriptionID != "subs_1" || schedule.TenantID != "tenant_1" || schedule.EnvironmentID != "env_1" {
		t.Fatalf("tenancy not copied from the subscription: %+v", schedule)
	}
	if schedule.Status != types.ScheduleStatusPending || schedule.StatusColumn != types.StatusPublished {
		t.Fatalf("unexpected status: %v / %v", schedule.Status, schedule.StatusColumn)
	}
	if !schedule.ScheduledAt.Equal(scheduledAt) {
		t.Fatalf("scheduled_at = %v, want %v", schedule.ScheduledAt, scheduledAt)
	}

	config, err := schedule.GetPlanChangeV2Config()
	if err != nil || !config.IsV2() || config.TargetPlanID != "plan_b" {
		t.Fatalf("config not applied: %+v (%v)", config, err)
	}
}

func TestNewSubscriptionScheduleBuilder_CopiesRatherThanAliases(t *testing.T) {
	original := &SubscriptionSchedule{
		ID:           "sched_1",
		ScheduleType: types.SubscriptionScheduleChangeTypePlanChange,
		Status:       types.ScheduleStatusPending,
		Metadata:     types.Metadata{"k": "v"},
	}

	updated := NewSubscriptionScheduleBuilder(original).
		WithStatus(types.ScheduleStatusExecuted).
		WithErrorMessage("boom").
		Build()

	if original.Status != types.ScheduleStatusPending {
		t.Fatal("the builder mutated the original schedule")
	}
	if original.ErrorMessage != nil {
		t.Fatal("the builder wrote an error message onto the original")
	}
	if updated.Status != types.ScheduleStatusExecuted || *updated.ErrorMessage != "boom" {
		t.Fatalf("updates not applied: %+v", updated)
	}

	updated.Metadata["k"] = "changed"
	if original.Metadata["k"] != "v" {
		t.Fatal("metadata is aliased between the original and the copy")
	}
}
