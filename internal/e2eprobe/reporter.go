package e2eprobe

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

type Kind string

const (
	KindBootstrap   Kind = "bootstrap"
	KindDriver      Kind = "driver"
	KindProbe       Kind = "probe"
	KindScenario    Kind = "scenario"
	KindListener    Kind = "listener"
	KindMaintenance Kind = "maintenance"
)

type FailureReport struct {
	CheckName  string
	CheckKind  Kind
	Step       string
	Err        error
	RunID      string
	Attributes map[string]string
	OccurredAt time.Time
}

type Reporter interface {
	Report(ctx context.Context, r FailureReport)
}

func NewCompositeReporter(sinks ...Reporter) Reporter {
	cleaned := make([]Reporter, 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			cleaned = append(cleaned, s)
		}
	}
	return &compositeReporter{sinks: cleaned}
}

type compositeReporter struct {
	sinks []Reporter
}

func (c *compositeReporter) Report(ctx context.Context, r FailureReport) {
	for _, s := range c.sinks {
		s.Report(ctx, r)
	}
}

func NewLogReporter(lg *logger.Logger) Reporter {
	return &logReporter{lg: lg}
}

type logReporter struct {
	lg *logger.Logger
}

func (l *logReporter) Report(_ context.Context, r FailureReport) {
	fields := []any{
		"event", "e2eprobe.check.failed",
		"check_name", r.CheckName,
		"check_kind", string(r.CheckKind),
		"step", r.Step,
		"run_id", r.RunID,
		"occurred_at", r.OccurredAt.Format(time.RFC3339Nano),
	}
	for k, v := range r.Attributes {
		fields = append(fields, k, v)
	}
	if r.Err != nil {
		fields = append(fields, "error", r.Err.Error())
	}
	l.lg.Errorw("e2eprobe check failed", fields...)
}
