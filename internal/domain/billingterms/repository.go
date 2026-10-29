package billingterms

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Cursor is a stable creation-time/ID boundary within scoped source history.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// RuleFilter bounds a scoped rule lookup; current rows are the default.
type RuleFilter struct {
	Owner           *OwnerRef
	IDs             []string
	IncludeArchived bool
	Limit           int
	After           *Cursor
}

// ScheduleItemFilter selects scoped source history or due charges.
type ScheduleItemFilter struct {
	Owner                   *OwnerRef
	IDs                     []string
	BillingTermIDs          []string
	InvoiceIDs              []string
	SubscriptionIDs         []string
	ScheduleStatuses        []types.BillingScheduleStatus
	Delivery                *types.BillingTermDelivery
	DueBefore               *time.Time
	UnresolvedManualBefore  *time.Time
	CreatedBefore           *time.Time
	RecoverableInvoicesOnly bool
	IncludeArchived         bool
	RelevantOnly            bool
	Limit                   int
	After                   *Cursor
}

// ExecutionStore shares transactions and owner locks with invoice collection.
type ExecutionStore interface {
	WithTx(ctx context.Context, fn func(context.Context) error) error
	InTransaction(ctx context.Context) bool
	// LockEligibility acquires the complete scoped lifecycle set before owner locks.
	LockEligibility(ctx context.Context, scopes []EligibilityScope) error
	LockOwner(ctx context.Context, owner OwnerRef) error
	// BindingStates validates scoped invoice/line/source economics for at most 500 items.
	BindingStates(ctx context.Context, items []*ScheduleItem) ([]BindingState, error)
}

// NonblockingEligibilityStore acquires guards without waiting behind earlier caller locks.
// Any failure requires the caller to roll back its transaction.
type NonblockingEligibilityStore interface {
	TryLockEligibility(ctx context.Context, scopes []EligibilityScope) error
}

// RuleRepository persists definitions without changing their economics in place.
type RuleRepository interface {
	Create(ctx context.Context, rule *Rule) error
	Get(ctx context.Context, id string) (*Rule, error)
	List(ctx context.Context, filter *RuleFilter) ([]*Rule, error)
	// Archive requires the caller transaction and shared owner lock used by collection.
	Archive(ctx context.Context, owner OwnerRef) error
}

// ScheduleItemRepository persists charges and their execution/source references.
type ScheduleItemRepository interface {
	Create(ctx context.Context, item *ScheduleItem) error
	Get(ctx context.Context, id string) (*ScheduleItem, error)
	List(ctx context.Context, filter *ScheduleItemFilter) ([]*ScheduleItem, error)
	ResolveBillOn(ctx context.Context, id string, billOn time.Time) (bool, error)
	// Claim requires a caller transaction holding shared owner and invoice lifecycle locks.
	Claim(ctx context.Context, id, invoiceID, invoiceLineItemID string) (bool, error)
	// Cancel requires the same transaction and lifecycle locks as collection or invoice voiding.
	Cancel(ctx context.Context, id string, reason types.BillingScheduleCancellationReason) (bool, error)
	// Archive requires the caller transaction and shared owner lock used by collection.
	Archive(ctx context.Context, owner OwnerRef) error
}

// OwnerAdapter supplies owner-specific economics and collection capabilities.
type OwnerAdapter interface {
	Type() types.BillingTermOwnerType
	Snapshot(ctx context.Context, owner OwnerRef) (*OwnerSnapshot, error)
}
