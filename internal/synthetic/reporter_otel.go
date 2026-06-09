package synthetic

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func NewOTELReporter(tracer trace.Tracer) Reporter {
	return &otelReporter{tracer: tracer}
}

type otelReporter struct {
	tracer trace.Tracer
}

func (o *otelReporter) Report(ctx context.Context, r FailureReport) {
	attrs := []attribute.KeyValue{
		attribute.String("synthetic.check.name", r.CheckName),
		attribute.String("synthetic.check.kind", string(r.CheckKind)),
		attribute.String("synthetic.step", r.Step),
		attribute.String("synthetic.run_id", r.RunID),
	}
	for k, v := range r.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	_, span := o.tracer.Start(ctx, "synthetic.check.run",
		trace.WithAttributes(attrs...),
		trace.WithTimestamp(r.OccurredAt),
	)
	if r.Err != nil {
		span.RecordError(r.Err)
		span.SetStatus(codes.Error, r.Err.Error())
	}
	span.End()
}
