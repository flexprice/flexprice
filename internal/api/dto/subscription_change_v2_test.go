package dto

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func TestSubscriptionChangeV2Request_Validate_ChangeAt(t *testing.T) {
	tests := []struct {
		name              string
		changeAt          *types.ScheduleType
		prorationBehavior types.ProrationBehavior
		wantErr           string
	}{
		{
			name:              "end_of_period with create_prorations is rejected",
			changeAt:          lo.ToPtr(types.ScheduleTypePeriodEnd),
			prorationBehavior: types.ProrationBehaviorCreateProrations,
			wantErr:           "proration is not applicable for an end_of_period plan change",
		},
		{
			name:              "end_of_period with none passes",
			changeAt:          lo.ToPtr(types.ScheduleTypePeriodEnd),
			prorationBehavior: types.ProrationBehaviorNone,
		},
		{
			name:              "immediate with create_prorations passes",
			changeAt:          lo.ToPtr(types.ScheduleTypeImmediate),
			prorationBehavior: types.ProrationBehaviorCreateProrations,
		},
		{
			name:              "nil change_at with create_prorations passes",
			prorationBehavior: types.ProrationBehaviorCreateProrations,
		},
		{
			name:              "invalid change_at is rejected",
			changeAt:          lo.ToPtr(types.ScheduleType("tomorrow")),
			prorationBehavior: types.ProrationBehaviorNone,
			wantErr:           "invalid schedule type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &SubscriptionChangeV2Request{
				TargetPlanID:      "plan_123",
				ProrationBehavior: tt.prorationBehavior,
				ChangeAt:          tt.changeAt,
			}

			err := req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSubscriptionChangeV2Request_IsDeferred(t *testing.T) {
	tests := []struct {
		name     string
		req      *SubscriptionChangeV2Request
		expected bool
	}{
		{"nil request", nil, false},
		{"nil change_at", &SubscriptionChangeV2Request{}, false},
		{"immediate", &SubscriptionChangeV2Request{ChangeAt: lo.ToPtr(types.ScheduleTypeImmediate)}, false},
		{"end_of_period", &SubscriptionChangeV2Request{ChangeAt: lo.ToPtr(types.ScheduleTypePeriodEnd)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.IsDeferred(); got != tt.expected {
				t.Fatalf("IsDeferred() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// nil already means "unset"; an explicit empty string is malformed input that
// ScheduleType.Validate() would otherwise let through.
func TestSubscriptionChangeV2Request_Validate_RejectsEmptyChangeAt(t *testing.T) {
	req := &SubscriptionChangeV2Request{
		TargetPlanID:      "plan_123",
		ProrationBehavior: types.ProrationBehaviorNone,
		ChangeAt:          lo.ToPtr(types.ScheduleType("")),
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected an error for an empty change_at, got nil")
	}
	if !strings.Contains(err.Error(), "schedule type cannot be empty string") {
		t.Fatalf("unexpected error: %v", err)
	}
}
