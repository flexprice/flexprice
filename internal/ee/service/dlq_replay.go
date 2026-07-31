package service

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// DLQReplayService drains a dead-letter (poison-queue) topic back to the origin
// topic each message was poisoned from. It is a thin wrapper over the replay
// engine in internal/kafka/dlq, so the same logic backs the `dlq` CLI, the
// Temporal activity, and the trigger below.
type DLQReplayService interface {
	// ReplayDLQ runs a replay synchronously (used by the Temporal activity).
	ReplayDLQ(ctx context.Context, opts dlq.Options) (*dlq.Summary, error)
	// TriggerReplayDLQWorkflow starts the ReplayDLQ Temporal workflow and returns
	// its handle. This is the operator entry point (admin API / Temporal UI).
	TriggerReplayDLQWorkflow(ctx context.Context, req *ReplayDLQRequest) (*workflowModels.TemporalWorkflowResult, error)
}

// ReplayDLQRequest is the operator-facing request to start a DLQ replay.
type ReplayDLQRequest struct {
	SourceTopic    string
	TargetOverride string
	Group          string
	Since          string // RFC3339; empty resumes from the committed offset
	FromStart      bool
	Max            int
	MaxReplays     int
	DryRun         bool
}

type dlqReplayService struct {
	ServiceParams
}

// NewDLQReplayService creates a new DLQReplayService.
func NewDLQReplayService(params ServiceParams) DLQReplayService {
	return &dlqReplayService{ServiceParams: params}
}

func (s *dlqReplayService) ReplayDLQ(ctx context.Context, opts dlq.Options) (*dlq.Summary, error) {
	return dlq.Replay(ctx, s.Config, s.Logger, opts)
}

func (s *dlqReplayService) TriggerReplayDLQWorkflow(ctx context.Context, req *ReplayDLQRequest) (*workflowModels.TemporalWorkflowResult, error) {
	if req.SourceTopic == "" {
		return nil, ierr.NewError("source_topic is required").
			WithHint("Source DLQ topic is required").
			Mark(ierr.ErrValidation)
	}

	var sinceMs int64
	if req.Since != "" {
		t, err := time.Parse(time.RFC3339, req.Since)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid since format. Use RFC3339 (e.g., 2026-08-01T00:00:00Z)").
				Mark(ierr.ErrValidation)
		}
		sinceMs = t.UnixMilli()
	}
	if req.FromStart && sinceMs > 0 {
		return nil, ierr.NewError("from_start and since are mutually exclusive").
			WithHint("Set only one of from_start or since").
			Mark(ierr.ErrValidation)
	}

	workflowInput := map[string]interface{}{
		"source_topic":    req.SourceTopic,
		"target_override": req.TargetOverride,
		"group":           req.Group,
		"since_ms":        sinceMs,
		"from_start":      req.FromStart,
		"max":             req.Max,
		"max_replays":     req.MaxReplays,
		"dry_run":         req.DryRun,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return nil, ierr.NewError("temporal service not available").
			WithHint("DLQ replay workflow requires the Temporal service").
			Mark(ierr.ErrInternal)
	}

	workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, types.TemporalReplayDLQWorkflow, workflowInput)
	if err != nil {
		return nil, err
	}

	return &workflowModels.TemporalWorkflowResult{
		Message:    "DLQ replay workflow started successfully",
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
	}, nil
}
