package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/jmoiron/sqlx"
)

// TxKey is the context key type for storing transaction
type TxKey struct{}

// Tx wraps sqlx.Tx to support nested transactions using savepoints
type Tx struct {
	*sqlx.Tx
	savepointID int
	ID          string // Unique ID for tracing
}

// GetTx retrieves a transaction from the context if it exists
func GetTx(ctx context.Context) (*Tx, bool) {
	tx, ok := ctx.Value(TxKey{}).(*Tx)
	return tx, ok
}

// BeginTx starts a new transaction
func (db *DB) BeginTx(ctx context.Context) (context.Context, *Tx, error) {
	if tx, ok := GetTx(ctx); ok {
		// Create a new savepoint for nested transaction
		tx.savepointID++
		savepoint := fmt.Sprintf("sp_%d", tx.savepointID)

		db.logger.Debug("creating savepoint",
			"tx_id", tx.ID,
			"savepoint", savepoint,
			"savepoint_id", tx.savepointID,
		)

		_, err := tx.ExecContext(ctx, fmt.Sprintf("SAVEPOINT %s", savepoint))
		if err != nil {
			return ctx, nil, fmt.Errorf("failed to create savepoint: %w", err)
		}
		return ctx, tx, nil
	}

	// Start a new transaction
	sqlxTx, err := db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
		ReadOnly:  false,
	})
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	tx := &Tx{
		Tx: sqlxTx,
		ID: types.GenerateUUID(),
	}

	db.logger.Debug("starting new transaction",
		"tx_id", tx.ID,
	)

	ctx = context.WithValue(ctx, TxKey{}, tx)
	return ctx, tx, nil
}

// CommitTx commits the current transaction level
func (db *DB) CommitTx(ctx context.Context) error {
	tx, ok := GetTx(ctx)
	if !ok {
		return fmt.Errorf("no transaction in context")
	}

	if tx.savepointID > 0 {
		// Release the current savepoint for nested transaction
		savepoint := fmt.Sprintf("sp_%d", tx.savepointID)

		db.logger.Debug("releasing savepoint",
			"tx_id", tx.ID,
			"savepoint", savepoint,
			"savepoint_id", tx.savepointID,
		)

		_, err := tx.ExecContext(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", savepoint))
		if err != nil {
			return fmt.Errorf("failed to release savepoint: %w", err)
		}
		tx.savepointID--
		return nil
	}

	// Commit the top-level transaction
	db.logger.Debug("committing transaction",
		"tx_id", tx.ID,
	)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// RollbackTx rolls back the current transaction level
func (db *DB) RollbackTx(ctx context.Context) error {
	tx, ok := GetTx(ctx)
	if !ok {
		return fmt.Errorf("no transaction in context")
	}

	if tx.savepointID > 0 {
		// Rollback to the current savepoint for nested transaction
		savepoint := fmt.Sprintf("sp_%d", tx.savepointID)

		db.logger.Debug("rolling back to savepoint",
			"tx_id", tx.ID,
			"savepoint", savepoint,
			"savepoint_id", tx.savepointID,
		)

		_, err := tx.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepoint))
		if err != nil {
			return fmt.Errorf("failed to rollback to savepoint: %w", err)
		}
		tx.savepointID--
		return nil
	}

	// Rollback the top-level transaction
	db.logger.Debug("rolling back transaction",
		"tx_id", tx.ID,
	)

	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}

// WithTx executes a function within a transaction
func (db *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	// Start transaction
	ctx, tx, err := db.BeginTx(ctx)
	if err != nil {
		return err
	}

	// Handle panics by rolling back
	defer func() {
		if r := recover(); r != nil {
			db.logger.Error("panic in transaction",
				"tx_id", tx.ID,
				"panic", r,
			)
			_ = db.RollbackTx(ctx)
			panic(r) // Re-throw panic after rollback
		}
	}()

	// Execute the function
	if err := fn(ctx); err != nil {
		db.logger.Error("transaction failed",
			"tx_id", tx.ID,
			"error", err,
		)
		if rbErr := db.RollbackTx(ctx); rbErr != nil {
			return fmt.Errorf("error rolling back transaction: %v (original error: %v)", rbErr, err)
		}
		return err
	}

	// Commit transaction
	if err := db.CommitTx(ctx); err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	return nil
}
