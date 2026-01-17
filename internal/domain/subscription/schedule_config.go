package subscription

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// PlanChangeConfiguration represents the configuration for a plan change schedule
type PlanChangeConfiguration struct {
	TargetPlanID       string                  `json:"target_plan_id"`
	ProrationBehavior  types.ProrationBehavior `json:"proration_behavior"`
	BillingCadence     types.BillingCadence    `json:"billing_cadence"`
	BillingPeriod      types.BillingPeriod     `json:"billing_period"`
	BillingPeriodCount int                     `json:"billing_period_count"`
	BillingCycle       types.BillingCycle      `json:"billing_cycle"`
	ChangeMetadata     map[string]string       `json:"change_metadata,omitempty"`
}

// PlanChangeResult represents the result of a plan change execution
type PlanChangeResult struct {
	OldSubscriptionID string    `json:"old_subscription_id"`
	NewSubscriptionID string    `json:"new_subscription_id"`
	ChangeType        string    `json:"change_type"`
	EffectiveDate     time.Time `json:"effective_date"`
}

// GetPlanChangeConfig parses and returns the plan change configuration
func (s *SubscriptionSchedule) GetPlanChangeConfig() (*PlanChangeConfiguration, error) {
	if s.ScheduleType != types.SubscriptionScheduleChangeTypePlanChange {
		return nil, ErrInvalidScheduleType
	}

	var config PlanChangeConfiguration
	if err := json.Unmarshal(s.Configuration, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SetPlanChangeConfig serializes and sets the plan change configuration
func (s *SubscriptionSchedule) SetPlanChangeConfig(config *PlanChangeConfiguration) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	s.Configuration = data
	return nil
}

// GetPlanChangeResult parses and returns the plan change result
func (s *SubscriptionSchedule) GetPlanChangeResult() (*PlanChangeResult, error) {
	if s.ScheduleType != types.SubscriptionScheduleChangeTypePlanChange {
		return nil, ErrInvalidScheduleType
	}

	if s.ExecutionResult == nil {
		return nil, nil
	}

	var result PlanChangeResult
	if err := json.Unmarshal(s.ExecutionResult, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetPlanChangeResult serializes and sets the plan change result
func (s *SubscriptionSchedule) SetPlanChangeResult(result *PlanChangeResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	s.ExecutionResult = data
	return nil
}

// CancellationConfiguration represents the configuration for a subscription cancellation schedule
type CancellationConfiguration struct {
	CancellationType  types.CancellationType  `json:"cancellation_type"`
	Reason            string                  `json:"reason"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior"`
	// Original subscription state to restore if cancelled
	OriginalCancelAtPeriodEnd bool       `json:"original_cancel_at_period_end"`
	OriginalCancelAt          *time.Time `json:"original_cancel_at"` // No omitempty - need to store null
	OriginalEndDate           *time.Time `json:"original_end_date"`  // No omitempty - need to store null
}

// CancellationResult represents the result of a cancellation execution
type CancellationResult struct {
	SubscriptionID string    `json:"subscription_id"`
	CancelledAt    time.Time `json:"cancelled_at"`
	EffectiveDate  time.Time `json:"effective_date"`
	Reason         string    `json:"reason,omitempty"`
}

// GetCancellationConfig parses and returns the cancellation configuration
func (s *SubscriptionSchedule) GetCancellationConfig() (*CancellationConfiguration, error) {
	if s.ScheduleType != types.SubscriptionScheduleChangeTypeCancellation {
		return nil, ErrInvalidScheduleType
	}

	var config CancellationConfiguration
	if err := json.Unmarshal(s.Configuration, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SetCancellationConfig serializes and sets the cancellation configuration
func (s *SubscriptionSchedule) SetCancellationConfig(config *CancellationConfiguration) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	s.Configuration = data
	return nil
}

// GetCancellationResult parses and returns the cancellation result
func (s *SubscriptionSchedule) GetCancellationResult() (*CancellationResult, error) {
	if s.ScheduleType != types.SubscriptionScheduleChangeTypeCancellation {
		return nil, ErrInvalidScheduleType
	}

	if s.ExecutionResult == nil {
		return nil, nil
	}

	var result CancellationResult
	if err := json.Unmarshal(s.ExecutionResult, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetCancellationResult serializes and sets the cancellation result
func (s *SubscriptionSchedule) SetCancellationResult(result *CancellationResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	s.ExecutionResult = data
	return nil
}

// Future: Add more configuration types here as needed
// e.g., AddonChangeConfiguration, PriceChangeConfiguration, etc.
