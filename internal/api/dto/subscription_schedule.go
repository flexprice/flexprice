package dto

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionScheduleResponse represents a subscription schedule
// @Description Full details of a subscription schedule
type SubscriptionScheduleResponse struct {
	// id of the schedule
	ID string `json:"id"`

	// subscription_id is the ID of the subscription
	SubscriptionID string `json:"subscription_id"`

	// schedule_type is the type of schedule (plan_change, addon_change, etc.)
	ScheduleType types.SubscriptionScheduleChangeType `json:"schedule_type"`

	// scheduled_at is when the schedule will execute
	ScheduledAt time.Time `json:"scheduled_at"`

	// status is the current status of the schedule
	Status types.ScheduleStatus `json:"status"`

	// configuration contains type-specific configuration (e.g., target_plan_id for plan changes)
	Configuration interface{} `json:"configuration,omitempty"`

	// executed_at is when the schedule was executed
	ExecutedAt *time.Time `json:"executed_at,omitempty"`

	// cancelled_at is when the schedule was cancelled
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`

	// execution_result contains type-specific execution result
	ExecutionResult interface{} `json:"execution_result,omitempty"`

	// error_message contains the error if execution failed
	ErrorMessage *string `json:"error_message,omitempty"`

	// days_until_execution is the number of days until execution
	DaysUntilExecution int `json:"days_until_execution"`

	// can_be_cancelled indicates if the schedule can be cancelled
	CanBeCancelled bool `json:"can_be_cancelled"`

	// metadata from the schedule
	Metadata map[string]string `json:"metadata,omitempty"`

	// created_at timestamp
	CreatedAt time.Time `json:"created_at"`

	// updated_at timestamp
	UpdatedAt time.Time `json:"updated_at"`
}

// GetPendingSchedulesResponse represents a list of pending schedules
// @Description List of pending schedules for a subscription
type GetPendingSchedulesResponse struct {
	// schedules is the list of pending schedules
	Schedules []*SubscriptionScheduleResponse `json:"schedules"`

	// count is the number of pending schedules
	Count int `json:"count"`
}

// CancelScheduleRequest represents the request to cancel a schedule
// @Description Request to cancel a subscription schedule (supports two modes)
type CancelScheduleRequest struct {
	// schedule_id is the ID of the schedule to cancel (optional if subscription_id and schedule_type are provided)
	ScheduleID *string `json:"schedule_id,omitempty"`

	// subscription_id is the ID of the subscription (required if schedule_id is not provided)
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// schedule_type is the type of schedule to cancel (required if schedule_id is not provided)
	ScheduleType *types.SubscriptionScheduleChangeType `json:"schedule_type,omitempty"`
}

// Validate validates the cancel schedule request
func (r *CancelScheduleRequest) Validate() error {
	// Either schedule_id OR (subscription_id + schedule_type) must be provided
	if r.ScheduleID == nil {
		if r.SubscriptionID == nil || r.ScheduleType == nil {
			return fmt.Errorf("either schedule_id or (subscription_id + schedule_type) must be provided")
		}
		// Validate schedule type if provided
		if err := r.ScheduleType.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CancelScheduleResponse represents the response after cancelling a schedule
// @Description Confirmation of schedule cancellation
type CancelScheduleResponse struct {
	// status is the new status (should be "cancelled")
	Status types.ScheduleStatus `json:"status"`

	// message is a confirmation message
	Message string `json:"message"`
}

// SubscriptionScheduleResponseFromDomain converts a domain schedule to a DTO
func SubscriptionScheduleResponseFromDomain(s *subscription.SubscriptionSchedule) *SubscriptionScheduleResponse {
	if s == nil {
		return nil
	}

	response := &SubscriptionScheduleResponse{
		ID:                 s.ID,
		SubscriptionID:     s.SubscriptionID,
		ScheduleType:       types.SubscriptionScheduleChangeType(s.ScheduleType),
		ScheduledAt:        s.ScheduledAt,
		Status:             types.ScheduleStatus(s.Status),
		ExecutedAt:         s.ExecutedAt,
		CancelledAt:        s.CancelledAt,
		ErrorMessage:       s.ErrorMessage,
		DaysUntilExecution: s.DaysUntilExecution(),
		CanBeCancelled:     s.CanBeCancelled(),
		Metadata:           s.Metadata,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}

	// Parse configuration based on type
	if s.ScheduleType == types.SubscriptionScheduleChangeTypePlanChange {
		if config, err := s.GetPlanChangeConfig(); err == nil {
			response.Configuration = config
		}
	}

	// Parse execution result based on type
	if s.ScheduleType == types.SubscriptionScheduleChangeTypePlanChange && s.ExecutionResult != nil {
		if result, err := s.GetPlanChangeResult(); err == nil {
			response.ExecutionResult = result
		}
	}

	return response
}

// SubscriptionScheduleListResponseFromDomain converts a list of domain schedules to DTOs
func SubscriptionScheduleListResponseFromDomain(schedules []*subscription.SubscriptionSchedule) []*SubscriptionScheduleResponse {
	responses := make([]*SubscriptionScheduleResponse, 0, len(schedules))
	for _, s := range schedules {
		responses = append(responses, SubscriptionScheduleResponseFromDomain(s))
	}
	return responses
}
