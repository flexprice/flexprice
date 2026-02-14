package ent

import (
	"context"

	"github.com/getsentry/sentry-go"
)

func StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) *sentry.Span {
	_ = ctx
	_ = repository
	_ = operation
	_ = params
	return nil
}

// FinishSpan safely finishes a span, handling nil spans
func FinishSpan(span *sentry.Span) {
	if span != nil {
		span.Finish()
	}
}

// SetSpanError marks a span as failed and adds error information
func SetSpanError(span *sentry.Span, err error) {
	if span == nil || err == nil {
		return
	}

	span.Status = sentry.SpanStatusInternalError
	span.SetData("error", err.Error())
}

// SetSpanSuccess marks a span as successful
func SetSpanSuccess(span *sentry.Span) {
	if span != nil {
		span.Status = sentry.SpanStatusOK
	}
}
