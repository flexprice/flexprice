package subscription

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Errors
var (
	ErrInvalidScheduleType = errors.New("invalid schedule type for this operation")
)

// SubscriptionSchedule represents a scheduled change to a subscription
type SubscriptionSchedule struct {
	ID              string
	SubscriptionID  string
	ScheduleType    types.SubscriptionScheduleChangeType
	ScheduledAt     time.Time // When the schedule should execute
	Status          types.ScheduleStatus
	Configuration   json.RawMessage
	ExecutedAt      *time.Time
	CancelledAt     *time.Time
	ExecutionResult json.RawMessage
	ErrorMessage    *string
	Metadata        types.Metadata
	TenantID        string
	EnvironmentID   string
	CreatedAt       time.Time // When the schedule was requested
	UpdatedAt       time.Time
	CreatedBy       string
	UpdatedBy       string
	StatusColumn    types.Status
}

// IsPending returns true if the schedule is pending
func (s *SubscriptionSchedule) IsPending() bool {
	return s.Status == types.ScheduleStatusPending
}

// CanBeCancelled returns true if the schedule can be cancelled
func (s *SubscriptionSchedule) CanBeCancelled() bool {
	return s.Status == types.ScheduleStatusPending && time.Now().Before(s.ScheduledAt)
}

// IsExpired returns true if the execution time has passed but schedule is still pending
func (s *SubscriptionSchedule) IsExpired() bool {
	return s.Status == types.ScheduleStatusPending && time.Now().After(s.ScheduledAt)
}

// DaysUntilExecution returns the number of days until execution
func (s *SubscriptionSchedule) DaysUntilExecution() int {
	if s.ExecutedAt != nil {
		return 0
	}
	duration := time.Until(s.ScheduledAt)
	return int(duration.Hours() / 24)
}

// SubscriptionScheduleFromEnt converts an Ent entity to domain model
func SubscriptionScheduleFromEnt(e *ent.SubscriptionSchedule) *SubscriptionSchedule {
	if e == nil {
		return nil
	}

	schedule := &SubscriptionSchedule{
		ID:             e.ID,
		SubscriptionID: e.SubscriptionID,
		ScheduleType:   e.ScheduleType,
		ScheduledAt:    e.ScheduledAt,
		Status:         types.ScheduleStatus(e.Status),
		Metadata:       e.Metadata,
		TenantID:       e.TenantID,
		EnvironmentID:  e.EnvironmentID,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
		CreatedBy:      e.CreatedBy,
		UpdatedBy:      e.UpdatedBy,
		StatusColumn:   types.Status(e.Status),
	}

	// Handle configuration JSONB
	if e.Configuration != nil {
		configBytes, err := json.Marshal(e.Configuration)
		if err == nil {
			schedule.Configuration = configBytes
		}
	}

	// Handle executed at
	if e.ExecutedAt != nil {
		schedule.ExecutedAt = e.ExecutedAt
	}

	// Handle cancelled at
	if e.CancelledAt != nil {
		schedule.CancelledAt = e.CancelledAt
	}

	// Handle execution result JSONB
	if e.ExecutionResult != nil {
		resultBytes, err := json.Marshal(e.ExecutionResult)
		if err == nil {
			schedule.ExecutionResult = resultBytes
		}
	}

	// Handle error message
	if e.ErrorMessage != nil {
		schedule.ErrorMessage = e.ErrorMessage
	}

	return schedule
}

// SubscriptionScheduleListFromEnt converts a list of Ent entities to domain models
func SubscriptionScheduleListFromEnt(entities []*ent.SubscriptionSchedule) []*SubscriptionSchedule {
	schedules := make([]*SubscriptionSchedule, 0, len(entities))
	for _, e := range entities {
		schedules = append(schedules, SubscriptionScheduleFromEnt(e))
	}
	return schedules
}
