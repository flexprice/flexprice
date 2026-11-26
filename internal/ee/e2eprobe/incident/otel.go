package incident

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTELNotifier records every incident transition as a span.
func OTELNotifier(tracer trace.Tracer) Notifier {
	return otelNotifier{tracer: tracer}
}

type otelNotifier struct{ tracer trace.Tracer }

func (o otelNotifier) span(ctx context.Context, name string, inc Incident, failed bool) {
	_, span := o.tracer.Start(ctx, name, trace.WithAttributes(
		attribute.String("e2eprobe.incident.id", inc.ID),
		attribute.String("e2eprobe.incident.kind", string(inc.Kind)),
		attribute.String("e2eprobe.scenario", inc.Key.Scenario),
		attribute.String("e2eprobe.persona", inc.Key.Persona),
		attribute.Int("e2eprobe.incident.runs", inc.Runs),
		attribute.String("e2eprobe.incident.summary", inc.Summary),
	))
	if failed {
		span.SetStatus(codes.Error, inc.Summary)
	}
	span.End()
}

func (o otelNotifier) Opened(ctx context.Context, inc Incident, _ BindingState) error {
	o.span(ctx, "e2eprobe.incident.opened", inc, true)
	return nil
}

func (o otelNotifier) Reminded(ctx context.Context, inc Incident, _ BindingState) error {
	o.span(ctx, "e2eprobe.incident.reminded", inc, true)
	return nil
}

func (o otelNotifier) Resolved(ctx context.Context, inc Incident, _ BindingState, _ time.Time) error {
	o.span(ctx, "e2eprobe.incident.resolved", inc, false)
	return nil
}

func (o otelNotifier) Digest(ctx context.Context, d Digest) error {
	_, span := o.tracer.Start(ctx, "e2eprobe.digest", trace.WithAttributes(
		attribute.Int("e2eprobe.open_incidents", len(d.Open)),
		attribute.Int("e2eprobe.bindings", len(d.Bindings)),
	))
	span.End()
	return nil
}
