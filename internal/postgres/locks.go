package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/samber/lo"
)

// LockRequest represents a request to acquire an advisory lock
type LockRequest struct {
	// Key is the entity ID (e.g., wallet ID) to lock
	Key string
	// Timeout is the maximum time to wait for the lock.
	// If nil, defaults to 30 seconds. Must be positive.
	Timeout *time.Duration
}

// GetTimeout returns the timeout duration, defaulting to 30 seconds if nil.
// Ensures the returned duration is always positive (minimum 1ms).
func (r *LockRequest) GetTimeout() time.Duration {
	var timeout time.Duration
	if r.Timeout == nil {
		timeout = 30 * time.Second
	} else {
		timeout = lo.FromPtr(r.Timeout)
	}

	// Ensure timeout is always positive
	if timeout <= 0 {
		return time.Millisecond
	}
	return timeout
}

// LockWithWait acquires an advisory lock based on the provided request.
// If Timeout is nil, defaults to 30 seconds.
// The key should be the entity ID (e.g., wallet ID).
// Auto released on tx commit/rollback.
// Must be called inside a transaction.
func (c *Client) LockWithWait(ctx context.Context, req LockRequest) error {
	tx := c.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("LockWithWait must be called inside transaction")
	}

	timeout := req.GetTimeout()

	// Set lock_timeout for this transaction (automatically reset on commit/rollback)
	timeoutMs := int(timeout.Milliseconds())
	_, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL lock_timeout = %d", timeoutMs))
	if err != nil {
		return fmt.Errorf("failed to set lock timeout: %w", err)
	}

	// Acquire the lock (will respect lock_timeout we just set)
	_, err = tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtext($1))
	`, req.Key)
	if err != nil {
		// Check if it's a lock timeout error using PostgreSQL error code
		if isLockTimeoutError(err) {
			return fmt.Errorf("failed to acquire lock within %v: %w", timeout, err)
		}
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	return nil
}

// isLockTimeoutError checks if the error is a PostgreSQL lock timeout error
// Uses PostgreSQL error codes for reliable detection
func isLockTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// Check for pq.Error directly (most common case)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		// PostgreSQL error code 55P03 = lock_not_available (lock timeout)
		return pqErr.Code == "55P03"
	}

	return false
}
