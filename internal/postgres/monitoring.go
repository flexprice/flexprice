package postgres

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/logger"
	sentryService "github.com/flexprice/flexprice/internal/sentry"
)

// SentryClient wraps the standard postgres client with Sentry monitoring
type SentryClient struct {
	client IClient
	sentry *sentryService.Service
	logger *logger.Logger
}

// NewSentryClient creates a new Sentry-instrumented Postgres client
func NewSentryClient(client IClient, sentry *sentryService.Service, logger *logger.Logger) IClient {
	return &SentryClient{
		client: client,
		sentry: sentry,
		logger: logger,
	}
}

// WithTx wraps the given function in a transaction with Sentry span tracking
func (c *SentryClient) WithTx(ctx context.Context, fn func(context.Context) error) error {
	span, spanCtx := c.sentry.StartDBSpan(ctx, "postgres.transaction", map[string]interface{}{
		"operation": "transaction",
	})
	if span != nil {
		defer span.Finish()
	}

	// Use the original client's WithTx but with the new span context
	return c.client.WithTx(spanCtx, fn)
}

// TxFromContext returns the transaction from context if it exists
func (c *SentryClient) TxFromContext(ctx context.Context) *ent.Tx {
	return c.client.TxFromContext(ctx)
}

// Querier returns the current transaction client if in a transaction, or the regular client
// This method wraps the client without any span tracking for now as there
// is no value in just getting postgress query client getting called
// we have added a repository layer that will add the span tracking
func (c *SentryClient) Querier(ctx context.Context) *ent.Client {
	return c.client.Querier(ctx)
}
