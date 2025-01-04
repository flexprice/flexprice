package testutil

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

var _ postgres.IClient = (*MockPostgresClient)(nil) // Ensure MockPostgresClient implements IClient

// MockPostgresClient is a mock implementation of postgres client for testing
type MockPostgresClient struct {
	entClient *ent.Client
	logger    *logger.Logger
}

// NewMockPostgresClient creates a new mock postgres client
func NewMockPostgresClient(logger *logger.Logger) postgres.IClient {
	return &MockPostgresClient{
		logger: logger,
	}
}

// WithTx executes the given function within a transaction
func (c *MockPostgresClient) WithTx(ctx context.Context, fn func(context.Context) error) error {
	// If we're already in a transaction, reuse it
	if tx := c.TxFromContext(ctx); tx != nil {
		return fn(ctx)
	}

	// For testing, we just execute the function without a real transaction
	return fn(ctx)
}

// TxFromContext returns the transaction from context if it exists
func (c *MockPostgresClient) TxFromContext(ctx context.Context) *ent.Tx {
	if tx, ok := ctx.Value(types.CtxDBTransaction).(*ent.Tx); ok {
		return tx
	}
	return nil
}

// Querier returns the ent client
func (c *MockPostgresClient) Querier(ctx context.Context) *ent.Client {
	if tx := c.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return c.entClient
}
