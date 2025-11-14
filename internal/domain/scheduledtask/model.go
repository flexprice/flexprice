package scheduledtask

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// ScheduledTask represents a scheduled export task
type ScheduledTask struct {
	ID                 string
	TenantID           string
	EnvironmentID      string
	ConnectionID       string
	EntityType         types.ScheduledTaskEntityType
	Interval           types.ScheduledTaskInterval
	Enabled            bool
	JobConfig          *types.S3JobConfig
	TemporalScheduleID string // Temporal schedule ID
	Status             types.Status
	CreatedAt          time.Time
	UpdatedAt          time.Time
	CreatedBy          string
	UpdatedBy          string
}

// IsEnabled returns whether the task is enabled
func (j *ScheduledTask) IsEnabled() bool {
	return j.Enabled && j.Status == types.StatusPublished
}

// GetS3JobConfig returns the S3 job configuration
func (j *ScheduledTask) GetS3JobConfig() (*types.S3JobConfig, error) {
	if j.JobConfig == nil {
		return nil, nil
	}

	// Set defaults if needed
	j.JobConfig.SetDefaults()

	return j.JobConfig, j.JobConfig.Validate()
}
