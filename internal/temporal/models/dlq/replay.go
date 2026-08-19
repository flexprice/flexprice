package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ReplayDLQWorkflowInput represents the input for the DLQ replay workflow.
// Fields mirror internal/kafka/dlq.Options (the replay engine).
type ReplayDLQWorkflowInput struct {
	SourceTopic    string `json:"source_topic"`
	TargetOverride string `json:"target_override,omitempty"`
	Group          string `json:"group,omitempty"`
	SinceMs        int64  `json:"since_ms,omitempty"`
	FromStart      bool   `json:"from_start,omitempty"`
	Max            int    `json:"max,omitempty"`
	MaxReplays     int    `json:"max_replays,omitempty"`
	DryRun         bool   `json:"dry_run"`
}

// Validate validates the DLQ replay workflow input.
func (i *ReplayDLQWorkflowInput) Validate() error {
	if i.SourceTopic == "" {
		return ierr.NewError("source_topic is required").
			WithHint("Source DLQ topic is required").
			Mark(ierr.ErrValidation)
	}
	if i.FromStart && i.SinceMs > 0 {
		return ierr.NewError("from_start and since_ms are mutually exclusive").
			WithHint("Set only one of from_start or since_ms").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ReplayDLQWorkflowResult represents the result of the DLQ replay workflow.
// Mirrors internal/kafka/dlq.Summary plus a completion timestamp.
type ReplayDLQWorkflowResult struct {
	Scanned     int            `json:"scanned"`
	Replayed    int            `json:"replayed"`
	Skipped     int            `json:"skipped"`
	Quarantined int            `json:"quarantined"`
	ByTarget    map[string]int `json:"by_target"`
	ByReason    map[string]int `json:"by_reason"`
	CompletedAt time.Time      `json:"completed_at"`
}
