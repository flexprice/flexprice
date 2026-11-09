package billingterm

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// TermRepository is the storage surface for billing terms.
type TermRepository interface {
	CreateMany(ctx context.Context, terms []*Term) ([]*Term, error)

	// List returns the owner's published terms ordered by id.
	List(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*Term, error)

	// ListByIDs returns the terms with these ids in any status, so an invoiced instalment still finds an archived term.
	ListByIDs(ctx context.Context, ids []string) ([]*Term, error)

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

	// ListInvoiced returns the owners' instalments invoiced in [period.Start, period.End), ordered by invoiced_at, then id.
	ListInvoiced(ctx context.Context, ownerType types.BillingTermOwnerType, ownerIDs []string, period types.Period) ([]*Instalment, error)

	// Update writes bill_on and triggered_at of a scheduled instalment. It fails with ErrInvalidOperation when the
	// instalment is no longer scheduled, e.g. claimed by a finalizing invoice.
	Update(ctx context.Context, instalment *Instalment) (*Instalment, error)

	// Cancel cancels the owner's instalments in the given statuses, which must be non-empty, and keeps invoice_id.
	Cancel(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string, statuses []types.InstalmentStatus) ([]*Instalment, error)

	// Claim moves the given scheduled instalments to invoiced on the invoice, finalized at invoicedAt, and returns only
	// the rows it changed.
	Claim(ctx context.Context, invoiceID string, invoicedAt time.Time, ids []string) ([]*Instalment, error)

	// Reschedule moves the invoice's invoiced instalments back to scheduled and clears invoice_id and invoiced_at.
	Reschedule(ctx context.Context, invoiceID string) ([]*Instalment, error)

	// CancelInvoiced cancels the given instalments invoiced on the invoice, clears invoice_id and invoiced_at, and
	// returns only the rows it changed.
	CancelInvoiced(ctx context.Context, invoiceID string, ids []string) ([]*Instalment, error)
}

// InstalmentFilter selects instalments for List; empty fields do not constrain.
type InstalmentFilter struct {
	OwnerType types.BillingTermOwnerType
	OwnerIDs  []string

	SubscriptionIDs []string
	Statuses        []types.InstalmentStatus
	Deliveries      []types.InstalmentDelivery

	// BillOnOrBefore selects instalments whose bill_on is at or before it.
	BillOnOrBefore *time.Time

	InvoiceIDs []string
	IDs        []string
}
