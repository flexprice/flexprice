package incident

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/logger"
)

// Notifier receives incident transitions and the periodic digest. A returned
// error means the message did not reach its audience; the tracker retries
// opens and reminders on its next tick.
type Notifier interface {
	Opened(ctx context.Context, inc Incident, st BindingState) error
	Reminded(ctx context.Context, inc Incident, st BindingState) error
	Resolved(ctx context.Context, inc Incident, st BindingState, closedAt time.Time) error
	Digest(ctx context.Context, d Digest) error
}

// Multi fans out to every notifier; a panicking sink never blocks the others
// and the joined error reports every sink that rejected the message.
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

func (m multi) each(fn func(Notifier) error) error {
	var errs []error
	for _, s := range m.sinks {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					errs = append(errs, fmt.Errorf("notifier panicked: %v", rec))
				}
			}()
			if err := fn(s); err != nil {
				errs = append(errs, err)
			}
		}()
	}
	return errors.Join(errs...)
}

func (m multi) Opened(ctx context.Context, inc Incident, st BindingState) error {
	return m.each(func(n Notifier) error { return n.Opened(ctx, inc, st) })
}

func (m multi) Reminded(ctx context.Context, inc Incident, st BindingState) error {
	return m.each(func(n Notifier) error { return n.Reminded(ctx, inc, st) })
}

func (m multi) Resolved(ctx context.Context, inc Incident, st BindingState, closedAt time.Time) error {
	return m.each(func(n Notifier) error { return n.Resolved(ctx, inc, st, closedAt) })
}

func (m multi) Digest(ctx context.Context, d Digest) error {
	return m.each(func(n Notifier) error { return n.Digest(ctx, d) })
}

// LogNotifier writes incidents to the structured log.
func LogNotifier(lg *logger.Logger) Notifier {
	return logNotifier{lg: lg}
}

type logNotifier struct{ lg *logger.Logger }

func (l logNotifier) Opened(ctx context.Context, inc Incident, st BindingState) error {
	l.lg.Error(ctx, "e2eprobe incident opened",
		"error", inc.Summary,
		"event", "e2eprobe.incident.opened",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"runs", inc.Runs,
		"step", st.Last.Step,
		"opened_at", inc.OpenedAt.Format(time.RFC3339),
	)
	return nil
}

func (l logNotifier) Reminded(ctx context.Context, inc Incident, _ BindingState) error {
	l.lg.Info(ctx, "e2eprobe incident still open",
		"event", "e2eprobe.incident.reminded",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"runs", inc.Runs,
		"summary", inc.Summary,
		"opened_at", inc.OpenedAt.Format(time.RFC3339),
	)
	return nil
}

func (l logNotifier) Resolved(ctx context.Context, inc Incident, _ BindingState, closedAt time.Time) error {
	l.lg.Info(ctx, "e2eprobe incident resolved",
		"event", "e2eprobe.incident.resolved",
		"binding", inc.Key.String(),
		"kind", string(inc.Kind),
		"duration", closedAt.Sub(inc.OpenedAt).Round(time.Second).String(),
	)
	return nil
}

func (l logNotifier) Digest(ctx context.Context, d Digest) error {
	l.lg.Info(ctx, "e2eprobe digest",
		"event", "e2eprobe.digest",
		"open_incidents", len(d.Open),
		"bindings", len(d.Bindings),
		"digest", FormatDigest(d),
	)
	return nil
}
