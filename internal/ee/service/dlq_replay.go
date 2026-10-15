package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/kafka/dlq"
)

// DLQReplayService drains a dead-letter topic back to the topic each message was poisoned from.
type DLQReplayService interface {
	ReplayDLQ(ctx context.Context, opts dlq.Options) (*dlq.Summary, error)
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
