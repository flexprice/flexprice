package billingterm

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// TermRepository is the storage surface for billing terms.
type TermRepository interface {
	CreateMany(ctx context.Context, terms []*Term) ([]*Term, error)

	// List returns the owner's published terms ordered by id.
	List(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*Term, error)

	// Archive archives the owner's published terms and returns them.
	Archive(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*Term, error)

	// GetLinkedCustomerID resolves the customer of the term's owner, reading rows in any status.
	GetLinkedCustomerID(ctx context.Context, id string) (string, error)
}

// InstalmentRepository is the storage surface for billing instalments.
type InstalmentRepository interface {
	CreateMany(ctx context.Context, instalments []*Instalment) ([]*Instalment, error)
	Get(ctx context.Context, id string) (*Instalment, error)

	// List returns the matching instalments ordered by bill_on, then id.
	List(ctx context.Context, filter *InstalmentFilter) ([]*Instalment, error)

	// Update writes amount, bill_on, triggered_at and instalment_status.
	Update(ctx context.Context, instalment *Instalment) (*Instalment, error)

	// Cancel cancels the owner's instalments in the given statuses, which must be non-empty, and keeps invoice_id.
	Cancel(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string, statuses []types.InstalmentStatus) ([]*Instalment, error)

	// Claim moves the given scheduled instalments to invoiced on the invoice and returns only the rows it changed.
	Claim(ctx context.Context, invoiceID string, ids []string) ([]*Instalment, error)

	// Reschedule moves the invoice's invoiced instalments back to scheduled and clears invoice_id.
	Reschedule(ctx context.Context, invoiceID string) ([]*Instalment, error)
}
