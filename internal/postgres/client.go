package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	_ "github.com/lib/pq"
	"go.uber.org/fx"
)

// IClient defines the interface for postgres client operations
type IClient interface {
	// WithTx wraps the given function in a transaction
	WithTx(ctx context.Context, fn func(context.Context) error) error

	// TxFromContext returns the transaction from context if it exists
	TxFromContext(ctx context.Context) *ent.Tx

	// Querier returns the current transaction client if in a transaction, or the regular client
	Querier(ctx context.Context) *ent.Client
}

// Client wraps ent.Client to provide transaction management
type Client struct {
	entClient *ent.Client
	logger    *logger.Logger
}

// Module provides an fx.Option to integrate Ent client with the application
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			NewEntClient,
			NewClient,
		),
	)
}

// NewEntClient creates a new Ent client
func NewEntClient(config *config.Configuration, logger *logger.Logger) (*ent.Client, error) {
	// Get DSN from config
	dsn := config.Postgres.GetDSN()

	// Open PostgreSQL connection
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.Postgres.MaxOpenConns)
	db.SetMaxIdleConns(config.Postgres.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(config.Postgres.ConnMaxLifetimeMinutes) * time.Minute)

	// Create driver
	drv := entsql.OpenDB(dialect.Postgres, db)

	// Create client with options
	opts := []ent.Option{
		ent.Driver(drv),
		ent.Debug(), // Enable debug logging
	}

	client := ent.NewClient(opts...)

	logger.Debugw("connected to postgres",
		"host", config.Postgres.Host,
		"port", config.Postgres.Port,
		"auto_migrate", config.Postgres.AutoMigrate,
	)
	// Run the auto migration tool if enabled
	if config.Postgres.AutoMigrate {
		logger.Debugw("running auto migration")
		if err := client.Schema.Create(context.Background()); err != nil {
			return nil, fmt.Errorf("failed creating schema resources: %w", err)
		}
	}

	return client, nil
}

// NewClient creates a new ent client wrapper with transaction management
func NewClient(client *ent.Client, logger *logger.Logger) IClient {
	return &Client{
		entClient: client,
		logger:    logger,
	}
}

// WithTx wraps the given function in a transaction
func (c *Client) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	// If we're already in a transaction, reuse it and do not start a new one or commit it
	if tx := c.TxFromContext(ctx); tx != nil {
		return fn(ctx)
	}

	// Start a new transaction
	tx, err := c.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}

	// Ensure transaction is rolled back on panic
	defer func() {
		if v := recover(); v != nil {
			c.logger.Errorw("rolling back transaction due to panic",
				"panic", v,
			)
			_ = tx.Rollback()
			panic(v)
		}
	}()

	// Create new context with transaction
	txCtx := context.WithValue(ctx, types.CtxDBTransaction, tx)

	if err := fn(txCtx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			err = fmt.Errorf("rolling back transaction: %v (original error: %w)", rerr, err)
		}
		c.logger.Errorw("rolling back transaction due to error",
			"error", err,
		)
		return err
	}

	if err := tx.Commit(); err != nil {
		c.logger.Errorw("committing transaction",
			"error", err,
		)
		return fmt.Errorf("committing transaction: %w", err)
	}

	c.logger.Debugw("committed transaction")
	return nil
}

// TxFromContext returns the transaction from context if it exists
func (c *Client) TxFromContext(ctx context.Context) *ent.Tx {
	if tx, ok := ctx.Value(types.CtxDBTransaction).(*ent.Tx); ok {
		return tx
	}
	return nil
}

// Querier returns the current transaction client if in a transaction, or the regular client
func (c *Client) Querier(ctx context.Context) *ent.Client {
	if tx := c.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return c.entClient
}
