package emaillog

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository is the persistence contract for customer email logs.
type Repository interface {
	// Create inserts an email log; a second automatic email for the same entity and template
	// returns ErrAlreadyExists.
	Create(ctx context.Context, e *EmailLog) error

	Get(ctx context.Context, id string) (*EmailLog, error)

	// GetForUpdate reads an email log and locks its row until the surrounding transaction ends.
	GetForUpdate(ctx context.Context, id string) (*EmailLog, error)

	// Update saves the delivery fields: provider message id, status, reason, timestamps and metadata.
	Update(ctx context.Context, e *EmailLog) error

	List(ctx context.Context, filter *types.EmailLogFilter) ([]*EmailLog, error)
	Count(ctx context.Context, filter *types.EmailLogFilter) (int, error)

	// ListEnvironmentsWithQueuedEmails returns, across all tenants, every environment holding a queued email.
	ListEnvironmentsWithQueuedEmails(ctx context.Context) ([]types.TenantEnvironment, error)

	// ListQueued returns up to limit queued emails of the environment in ctx, manual sends first, then oldest first.
	ListQueued(ctx context.Context, limit int) ([]*EmailLog, error)
}
