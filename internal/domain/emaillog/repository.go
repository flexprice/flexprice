package emaillog

import "context"

// Repository is the persistence contract for customer email logs.
type Repository interface {
	// Create inserts an email log; a second automatic email for the same entity and template
	// returns ErrAlreadyExists.
	Create(ctx context.Context, e *EmailLog) error

	Get(ctx context.Context, id string) (*EmailLog, error)

	// Update saves the delivery fields: provider message id, status, reason, timestamps and metadata.
	Update(ctx context.Context, e *EmailLog) error
}
