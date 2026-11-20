package incident

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

// Notifier receives incident transitions and the periodic digest.
type Notifier interface {
	Opened(ctx context.Context, inc Incident, st BindingState)
	Reminded(ctx context.Context, inc Incident, st BindingState)
	Resolved(ctx context.Context, inc Incident, st BindingState, closedAt time.Time)
	Digest(ctx context.Context, d Digest)
}

// Multi fans out to every notifier; a panicking sink never blocks the others.
func Multi(n ...Notifier) Notifier {
	sinks := make([]Notifier, 0, len(n))
	for _, s := range n {
		if s != nil {
			sinks = append(sinks, s)
		}
	}
	return multi{sinks: sinks}
}

type multi struct{ sinks []Notifier }

func (m multi) each(fn func(Notifier)) {
	for _, s := range m.sinks {
		func() {
			defer func() { _ = recover() }()
			fn(s)
		}()
	}
}

func (m multi) Opened(ctx context.Context, inc Incident, st BindingState) {
	m.each(func(n Notifier) { n.Opened(ctx, inc, st) })
}

func (m multi) Reminded(ctx context.Context, inc Incident, st BindingState) {
	m.each(func(n Notifier) { n.Reminded(ctx, inc, st) })
}

func (m multi) Resolved(ctx context.Context, inc Incident, st BindingState, closedAt time.Time) {
	m.each(func(n Notifier) { n.Resolved(ctx, inc, st, closedAt) })
}

func (m multi) Digest(ctx context.Context, d Digest) {
	m.each(func(n Notifier) { n.Digest(ctx, d) })
}

// LogNotifier writes incidents to the structured log.
func LogNotifier(lg *logger.Logger) Notifier {
	return logNotifier{lg: lg}
}

type logNotifier struct{ lg *logger.Logger }

func (l logNotifier) Opened(ctx context.Context, inc Incident, st BindingState) {
	l.lg.Error(ctx, "e2eprobe incident opened",
		"error", inc.Summary,
		"event", "e2eprobe.incident.opened",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"runs", inc.Runs,
		"step", st.Last.Step,
		"opened_at", inc.OpenedAt.Format(time.RFC3339),
	)
}

func (l logNotifier) Reminded(ctx context.Context, inc Incident, _ BindingState) {
	l.lg.Info(ctx, "e2eprobe incident still open",
		"event", "e2eprobe.incident.reminded",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"runs", inc.Runs,
		"summary", inc.Summary,
		"opened_at", inc.OpenedAt.Format(time.RFC3339),
	)
}

func (l logNotifier) Resolved(ctx context.Context, inc Incident, _ BindingState, closedAt time.Time) {
	l.lg.Info(ctx, "e2eprobe incident resolved",
		"event", "e2eprobe.incident.resolved",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"duration", closedAt.Sub(inc.OpenedAt).Round(time.Second).String(),
	)
}

func (l logNotifier) Digest(ctx context.Context, d Digest) {
	l.lg.Info(ctx, "e2eprobe digest",
		"event", "e2eprobe.digest",
		"open_incidents", len(d.Open),
		"bindings", len(d.Bindings),
		"digest", FormatDigest(d),
	)
}
