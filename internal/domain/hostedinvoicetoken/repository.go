package hostedinvoicetoken

import (
	"context"
	"time"
)

// Repository is the persistence contract for hosted invoice tokens. A current token is
// one that is not revoked; it may be expired.
type Repository interface {
	// Create inserts a token; a second current token for the invoice returns ErrAlreadyExists.
	Create(ctx context.Context, l *HostedInvoiceToken) error

	// GetUnscoped reads a token by id WITHOUT tenant or environment filters. Only the
	// public hosted invoice middleware may call it: it learns the tenant from the row.
	GetUnscoped(ctx context.Context, id string) (*HostedInvoiceToken, error)

	// GetCurrentByInvoiceID returns the invoice's current (unrevoked) token, or ErrNotFound.
	GetCurrentByInvoiceID(ctx context.Context, invoiceID string) (*HostedInvoiceToken, error)

	// ListCurrentByInvoiceIDs returns the current (unrevoked) tokens of the given invoices.
	ListCurrentByInvoiceIDs(ctx context.Context, invoiceIDs []string) ([]*HostedInvoiceToken, error)

	// UpdateExpiresAt sets the expiry of the invoice's current token, or returns ErrNotFound.
	UpdateExpiresAt(ctx context.Context, invoiceID string, expiresAt time.Time) error

	// Revoke revokes a token; revoking an already revoked token is a no-op.
	Revoke(ctx context.Context, id string) error

	// RevokeByInvoiceID revokes the invoice's current token if it has one.
	RevokeByInvoiceID(ctx context.Context, invoiceID string) error
}
