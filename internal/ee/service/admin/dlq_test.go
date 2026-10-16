package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func TestDLQReplayValidation(t *testing.T) {
	for _, req := range []admindto.ReplayDLQRequest{
		{}, {SourceTopic: " "}, {SourceTopic: "events_dlq", Max: -1},
		{SourceTopic: "events_dlq", MaxReplays: -1},
		{SourceTopic: "events_dlq", Since: "invalid"},
		{SourceTopic: "events_dlq", Since: "1970-01-01T00:00:00Z"},
		{SourceTopic: "events_dlq", Since: "2026-10-01T00:00:00Z", FromStart: true},
	} {
		svc := &dlqService{replay: func(context.Context, *config.Configuration, *logger.Logger, dlq.Options) (*dlq.Summary, error) {
			t.Fatal("invalid request reached Kafka")
			return nil, nil
		}}
		_, err := svc.Replay(context.Background(), req)
		require.Error(t, err)
	}
}

func TestDLQReplayOptionsAndPartialProgress(t *testing.T) {
	req := admindto.ReplayDLQRequest{SourceTopic: "events_dlq", TargetOverride: "events", Group: "replay", Since: "2026-10-01T00:00:00Z", Max: 10, MaxReplays: 2, DryRun: true}
	svc := &dlqService{replay: func(ctx context.Context, _ *config.Configuration, _ *logger.Logger, opts dlq.Options) (*dlq.Summary, error) {
		require.Equal(t, dlq.Options{SourceTopic: req.SourceTopic, TargetOverride: req.TargetOverride, Group: req.Group, SinceMs: 1790812800000, Max: 10, MaxReplays: 2, DryRun: true}, opts)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(10*time.Minute), deadline, time.Second)
		return &dlq.Summary{Scanned: 5, Replayed: 3}, errors.New("broker unavailable")
	}}
	resp, err := svc.Replay(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, 5, resp.Scanned)
	require.Equal(t, 3, resp.Replayed)
}

func TestDLQReplayCancellationAndSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &dlqService{replay: func(ctx context.Context, _ *config.Configuration, _ *logger.Logger, opts dlq.Options) (*dlq.Summary, error) {
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.True(t, opts.FromStart)
		return &dlq.Summary{Replayed: 2}, nil
	}}
	resp, err := svc.Replay(ctx, admindto.ReplayDLQRequest{SourceTopic: "events_dlq", FromStart: true})
	require.NoError(t, err)
	require.Equal(t, 2, resp.Replayed)
}
