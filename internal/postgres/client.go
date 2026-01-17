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
	"github.com/flexprice/flexprice/internal/sentry"
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

	// Writer returns the writer client for write operations.
	// Always routes to the primary database (writer endpoint).
	//
	// Routing:
	// - Inside transaction: returns transaction client (writer)
	// - Outside transaction: returns writer client
	//
	// Use for: Create, Update, Delete, Save, Exec operations
	Writer(ctx context.Context) *ent.Client

	// Reader returns the appropriate client for read operations.
	// Intelligently routes based on context to ensure consistency when needed.
	//
	// Routing:
	// - Inside transaction: returns transaction client (writer) for read-your-writes consistency
	// - Force writer flag set: returns writer client for read-after-write consistency
	// - Otherwise: returns reader client (read replica if available)
	//
	// Use for: Get, List, Count, Query operations
	Reader(ctx context.Context) *ent.Client

	// LockWithWait acquires an advisory lock with a default timeout of 30 seconds.
	// The key should be the entity ID (e.g., wallet ID).
	// Must be called inside a transaction. Lock is automatically released on commit/rollback.
	LockWithWait(ctx context.Context, req LockRequest) error

	// Close closes the database connection
	Close() error
}

// Client wraps ent.Client to provide transaction management and read/write routing
type Client struct {
	writerClient *ent.Client // Primary database connection for writes
	readerClient *ent.Client // Read replica connection (may be same as writer)
	logger       *logger.Logger
	sentry       *sentry.Service
	hasReader    bool // Whether a separate reader endpoint is configured
}

// Module provides an fx.Option to integrate Ent client with the application
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			NewEntClients,
			NewClient,
		),
	)
}

// EntClients holds both writer and reader ENT clients
type EntClients struct {
	Writer    *ent.Client
	Reader    *ent.Client
	HasReader bool
}

// NewEntClients creates both writer and reader Ent clients
func NewEntClients(config *config.Configuration, logger *logger.Logger) (*EntClients, error) {
	// Get writer DSN from config
	writerDSN := config.Postgres.GetDSN()

	// Open writer PostgreSQL connection
	writerDB, err := sql.Open("postgres", writerDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres writer: %w", err)
	}

	// Configure writer connection pool
	writerDB.SetMaxOpenConns(config.Postgres.MaxOpenConns)
	writerDB.SetMaxIdleConns(config.Postgres.MaxIdleConns)
	writerDB.SetConnMaxLifetime(time.Duration(config.Postgres.ConnMaxLifetimeMinutes) * time.Minute)

	// Create writer driver
	writerDrv := entsql.OpenDB(dialect.Postgres, writerDB)

	// Create client with options
	writerOpts := []ent.Option{
		ent.Driver(writerDrv),
	}

	if config.Logging.DBLevel == types.LogLevelDebug {
		writerOpts = append(writerOpts,
			ent.Debug(),
			ent.Log(logger.GetEntLogger()),
		)
	}

	writerClient := ent.NewClient(writerOpts...)

	logger.Debugw("connected to postgres writer",
		"host", config.Postgres.Host,
		"port", config.Postgres.Port,
		"auto_migrate", config.Postgres.AutoMigrate,
	)

	// Initialize reader client
	var readerClient *ent.Client
	hasReader := config.Postgres.HasSeparateReader()

	if hasReader {
		// Get reader DSN from config
		readerDSN := config.Postgres.GetReaderDSN()

		// Open reader PostgreSQL connection
		readerDB, err := sql.Open("postgres", readerDSN)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to postgres reader: %w", err)
		}

		readerDB.SetMaxOpenConns(config.Postgres.MaxOpenConns)
		readerDB.SetMaxIdleConns(config.Postgres.MaxIdleConns)
		readerDB.SetConnMaxLifetime(time.Duration(config.Postgres.ConnMaxLifetimeMinutes) * time.Minute)

		// Create reader driver
		readerDrv := entsql.OpenDB(dialect.Postgres, readerDB)

		// Create reader client with options (removing debug logs for reads)
		readerOpts := []ent.Option{
			ent.Driver(readerDrv),
		}

		if config.Logging.DBLevel == types.LogLevelDebug {
			readerOpts = append(readerOpts,
				ent.Debug(),
				ent.Log(logger.GetEntLogger()),
			)
		}

		readerClient = ent.NewClient(readerOpts...)

		logger.Debugw("connected to postgres reader",
			"host", config.Postgres.ReaderHost,
			"port", config.Postgres.ReaderPort,
		)
	} else {
		// Use writer client as reader if no separate reader is configured
		readerClient = writerClient
		logger.Debugw("no separate reader configured, using writer for reads")
	}

	// Run the auto migration tool if enabled (only on writer)
	if config.Postgres.AutoMigrate {
		logger.Debugw("running auto migration")
		if err := writerClient.Schema.Create(context.Background()); err != nil {
			return nil, fmt.Errorf("failed creating schema resources: %w", err)
		}
	}

	return &EntClients{
		Writer:    writerClient,
		Reader:    readerClient,
		HasReader: hasReader,
	}, nil
}

// NewClient creates a new ent client wrapper with transaction management
func NewClient(clients *EntClients, logger *logger.Logger, sentry *sentry.Service) IClient {
	postgresClient := &Client{
		writerClient: clients.Writer,
		readerClient: clients.Reader,
		logger:       logger,
		sentry:       sentry,
		hasReader:    clients.HasReader,
	}

	if sentry != nil {
		return NewSentryClient(postgresClient, sentry, logger)
	}

	return postgresClient
}

// WithTx wraps the given function in a transaction
// Transactions ALWAYS use the writer connection to ensure consistency
func (c *Client) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	// If we're already in a transaction, reuse it and do not start a new one or commit it
	if tx := c.TxFromContext(ctx); tx != nil {
		return fn(ctx)
	}

	// Start a new transaction on the WRITER client
	tx, err := c.writerClient.Tx(ctx)
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

	// also force writer for all queries in this request
	// this is important to prevent issues with read after write consistency
	txCtx = types.WithForceWriter(txCtx)

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

// Writer returns the writer client for write operations.
// Always routes to the primary database.
//
// Use this for: Create, Update, Delete, Save, Exec operations
func (c *Client) Writer(ctx context.Context) *ent.Client {
	// If in a transaction, return the transaction client (which is on writer)
	if tx := c.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}

	// Always return writer for write operations
	return c.writerClient
}

// Reader returns the appropriate client for read operations.
// Intelligently routes to ensure consistency when needed.
//
// Use this for: Get, List, Count, Query operations
func (c *Client) Reader(ctx context.Context) *ent.Client {
	// Priority 1: If in a transaction, use transaction client for read-your-writes consistency
	if tx := c.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}

	// Priority 2: If force writer flag is set, use writer for read-after-write consistency
	if types.ShouldForceWriter(ctx) {
		return c.writerClient
	}

	// Priority 3: Default to reader for scalability
	return c.readerClient
}

// Close closes the database connection
func (c *Client) Close() error {
	err := c.writerClient.Close()
	if err != nil {
		return fmt.Errorf("failed to close postgres writer: %w", err)
	}

	if c.hasReader && c.readerClient != nil {
		err = c.readerClient.Close()
		if err != nil {
			return fmt.Errorf("failed to close postgres reader: %w", err)
		}
	}

	return nil
}
