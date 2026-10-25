package journal

import (
	"context"
	"time"

	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// LineSum is the net of matching lines for one period, account, event type and group.
type LineSum struct {
	PeriodStart *time.Time // nil on the opening row (all periods before SumFilter.PeriodStart)
	Account     types.LedgerAccountKey
	EventType   types.JournalEventType // empty on the opening row
	CustomerID  *string                // set only when grouped by it
	ContractID  *string
	POBID       *string
	Net         decimal.Decimal // credit − debit
}

// Repository persists immutable journal entries and their lines.
type Repository interface {
	// CreateWithLines saves the entry and lines; a posted idempotency key returns ErrAlreadyExists.
	CreateWithLines(ctx context.Context, e *domainJournal.Entry) error
	// GetIDByIdempotencyKey returns the ID of the entry posted under the key.
	GetIDByIdempotencyKey(ctx context.Context, key string) (string, error)
	// Get returns the entry with its lines.
	Get(ctx context.Context, id string) (*domainJournal.Entry, error)
	// List returns the matching entries with their lines.
	List(ctx context.Context, filter *Filter) ([]*domainJournal.Entry, error)
	Count(ctx context.Context, filter *Filter) (int, error)
	// SumLines nets matching lines per period, account, event type and group; earlier periods fold into one opening row.
	SumLines(ctx context.Context, f *SumFilter) ([]*LineSum, error)
}
