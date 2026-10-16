package admin

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ReplayDLQRequest selects a bounded replay of a regional dead-letter topic.
type ReplayDLQRequest struct {
	SourceTopic    string `json:"source_topic"`
	TargetOverride string `json:"target_override,omitempty"`
	Group          string `json:"group,omitempty"`
	Since          string `json:"since,omitempty"`
	FromStart      bool   `json:"from_start,omitempty"`
	Max            int    `json:"max,omitempty"`
	MaxReplays     int    `json:"max_replays,omitempty"`
	DryRun         bool   `json:"dry_run"`
}

// Validate checks replay bounds before connecting to Kafka.
func (r *ReplayDLQRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}
	r.SourceTopic = strings.TrimSpace(r.SourceTopic)
	if r.SourceTopic == "" {
		return ierr.NewError("source_topic is required").Mark(ierr.ErrValidation)
	}
	if r.FromStart && r.Since != "" {
		return ierr.NewError("from_start and since are mutually exclusive").Mark(ierr.ErrValidation)
	}
	if r.Max < 0 || r.MaxReplays < 0 {
		return ierr.NewError("max and max_replays must not be negative").Mark(ierr.ErrValidation)
	}
	if r.Since != "" {
		ts, err := time.Parse(time.RFC3339, r.Since)
		if err != nil || ts.UnixMilli() <= 0 {
			return ierr.NewError("since must be an RFC3339 timestamp after the Unix epoch").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// ReplayDLQResponse reports completed or partial replay progress.
type ReplayDLQResponse struct {
	Scanned     int            `json:"scanned"`
	Replayed    int            `json:"replayed"`
	Skipped     int            `json:"skipped"`
	Quarantined int            `json:"quarantined"`
	ByTarget    map[string]int `json:"by_target"`
	ByReason    map[string]int `json:"by_reason"`
}
