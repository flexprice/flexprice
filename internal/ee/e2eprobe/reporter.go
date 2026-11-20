package e2eprobe

import (
	"context"
	"encoding/json"
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
		func() {
			defer func() {
				_ = recover() // intentionally swallow; sink failures cannot cascade
			}()
			s.Report(ctx, r)
		}()
	}
}

func NewLogReporter(lg *logger.Logger) Reporter {
	return &logReporter{lg: lg}
}

type logReporter struct {
	lg *logger.Logger
}

func (l *logReporter) Report(ctx context.Context, r FailureReport) {
	errMsg := ""
	if r.Err != nil {
		errMsg = r.Err.Error()
	}
	// Attributes flattened into a JSON string so the Error call can keep
	// "error" as a static literal first-position arg (required by LL006).
	attrJSON, _ := json.Marshal(r.Attributes)
	l.lg.Error(ctx, "e2eprobe check failed",
		"error", errMsg,
		"event", "e2eprobe.check.failed",
		"check_name", r.CheckName,
		"check_kind", string(r.CheckKind),
		"step", r.Step,
		"run_id", r.RunID,
		"occurred_at", r.OccurredAt.Format(time.RFC3339Nano),
		"attributes", string(attrJSON),
	)
}
