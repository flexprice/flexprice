package admin

import (
	"context"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	"github.com/flexprice/flexprice/internal/logger"
)

// DLQService replays regional dead letters for authenticated operators.
type DLQService interface {
	Replay(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error)
}

type dlqService struct {
	service.ServiceParams
	replay func(context.Context, *config.Configuration, *logger.Logger, dlq.Options) (*dlq.Summary, error)
}

// NewDLQService constructs the admin replay service.
func NewDLQService(params service.ServiceParams) DLQService {
	return &dlqService{ServiceParams: params, replay: dlq.Replay}
}

func (s *dlqService) Replay(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	opts := dlq.Options{SourceTopic: req.SourceTopic, TargetOverride: req.TargetOverride, Group: req.Group, FromStart: req.FromStart, Max: req.Max, MaxReplays: req.MaxReplays, DryRun: req.DryRun}
	if req.Since != "" {
		since, _ := time.Parse(time.RFC3339, req.Since) // Validated above.
		opts.SinceMs = since.UnixMilli()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	summary, err := s.replay(ctx, s.Config, s.Logger, opts)
	var resp *admindto.ReplayDLQResponse
	if summary != nil {
		resp = &admindto.ReplayDLQResponse{Scanned: summary.Scanned, Replayed: summary.Replayed, Skipped: summary.Skipped, Quarantined: summary.Quarantined, ByTarget: summary.ByTarget, ByReason: summary.ByReason}
	}
	if err != nil {
		return resp, ierr.WithError(err).WithHint("Replay stopped; retry to resume from committed offsets").WithReportableDetails(map[string]interface{}{"source_topic": req.SourceTopic, "summary": resp}).Mark(ierr.ErrInternal)
	}
	return resp, nil
}
