package dto

import "github.com/flexprice/flexprice/internal/validator"

// ReplayDLQRequest is the admin request to start a DLQ replay workflow.
type ReplayDLQRequest struct {
	// SourceTopic is the dead-letter topic to drain.
	SourceTopic string `json:"source_topic" validate:"required" binding:"required" example:"production_event_processing_dlq"`
	// TargetOverride routes every message to this topic instead of its
	// per-message topic_poisoned header. Leave empty for normal routing.
	TargetOverride string `json:"target_override" example:"events"`
	// Group is the consumer group used to track the resume offset.
	Group string `json:"group" example:"dlq-replay-tool"`
	// Since (RFC3339) ignores the resume point and replays from the first
	// message appended at/after this time. Mutually exclusive with from_start.
	Since string `json:"since" example:"2026-08-01T00:00:00Z"`
	// FromStart ignores the resume point and re-drains from the oldest retained
	// offset. Mutually exclusive with since.
	FromStart bool `json:"from_start"`
	// Max caps the number of messages replayed (0 = no cap).
	Max int `json:"max" example:"0"`
	// MaxReplays quarantines a message once replayed this many times (0 = default 3).
	MaxReplays int `json:"max_replays" example:"3"`
	// DryRun previews routing without producing or committing anything.
	DryRun bool `json:"dry_run" example:"true"`
}

// Validate validates the DLQ replay request.
func (r *ReplayDLQRequest) Validate() error {
	return validator.ValidateRequest(r)
}
