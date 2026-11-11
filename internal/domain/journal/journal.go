package journal

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Entry is an immutable, balanced posting of one event.
type Entry struct {
	id                 string
	eventType          types.JournalEventType
	sourceEntityType   types.RevRecEntityType
	sourceEntityID     string
	idempotencyKey     string
	accountingPeriodID string
	businessDate       time.Time
	customerID         string
	contractID         string
	currency           string
	reversalOf         string
	lines              []*Line
	createdAt          time.Time
}

func (e *Entry) ID() string {
	if e == nil {
		return ""
	}
	return e.id
}

func (e *Entry) EventType() types.JournalEventType {
	if e == nil {
		return ""
	}
	return e.eventType
}

func (e *Entry) SourceEntityType() types.RevRecEntityType {
	if e == nil {
		return ""
	}
	return e.sourceEntityType
}

func (e *Entry) SourceEntityID() string {
	if e == nil {
		return ""
	}
	return e.sourceEntityID
}

// IdempotencyKey is empty on recognition entries.
func (e *Entry) IdempotencyKey() string {
	if e == nil {
		return ""
	}
	return e.idempotencyKey
}

func (e *Entry) AccountingPeriodID() string {
	if e == nil {
		return ""
	}
	return e.accountingPeriodID
}

func (e *Entry) BusinessDate() time.Time {
	if e == nil {
		return time.Time{}
	}
	return e.businessDate
}

func (e *Entry) CustomerID() string {
	if e == nil {
		return ""
	}
	return e.customerID
}

// ContractID is empty without a subscription.
func (e *Entry) ContractID() string {
	if e == nil {
		return ""
	}
	return e.contractID
}

func (e *Entry) Currency() string {
	if e == nil {
		return ""
	}
	return e.currency
}

func (e *Entry) ReversalOf() string {
	if e == nil {
		return ""
	}
	return e.reversalOf
}

func (e *Entry) Lines() []*Line {
	if e == nil {
		return nil
	}
	return e.lines
}

func (e *Entry) CreatedAt() time.Time {
	if e == nil {
		return time.Time{}
	}
	return e.createdAt
}

// Builder updates a copy of an entry.
type Builder struct {
	entry Entry
}

// NewBuilder copies e; nil starts empty.
func NewBuilder(e *Entry) *Builder {
	b := &Builder{}
	if e != nil {
		b.entry = *e
	}
	return b
}

func (b *Builder) WithID(id string) *Builder {
	b.entry.id = id
	return b
}

func (b *Builder) WithEventType(eventType types.JournalEventType) *Builder {
	b.entry.eventType = eventType
	return b
}

func (b *Builder) WithSource(entityType types.RevRecEntityType, entityID string) *Builder {
	b.entry.sourceEntityType = entityType
	b.entry.sourceEntityID = entityID
	return b
}

func (b *Builder) WithIdempotencyKey(key string) *Builder {
	b.entry.idempotencyKey = key
	return b
}

func (b *Builder) WithAccountingPeriodID(accountingPeriodID string) *Builder {
	b.entry.accountingPeriodID = accountingPeriodID
	return b
}

func (b *Builder) WithBusinessDate(businessDate time.Time) *Builder {
	b.entry.businessDate = businessDate
	return b
}

func (b *Builder) WithCustomerID(customerID string) *Builder {
	b.entry.customerID = customerID
	return b
}

func (b *Builder) WithContractID(contractID string) *Builder {
	b.entry.contractID = contractID
	return b
}

func (b *Builder) WithCurrency(currency string) *Builder {
	b.entry.currency = currency
	return b
}

func (b *Builder) WithReversalOf(entryID string) *Builder {
	b.entry.reversalOf = entryID
	return b
}

func (b *Builder) WithLines(lines []*Line) *Builder {
	b.entry.lines = append([]*Line(nil), lines...)
	return b
}

func (b *Builder) WithCreatedAt(createdAt time.Time) *Builder {
	b.entry.createdAt = createdAt
	return b
}

func (b *Builder) Build() *Entry {
	e := b.entry
	return &e
}

// Line is one debit or credit of an entry.
type Line struct {
	id        string
	account   types.LedgerAccountKey
	direction types.JournalDirection
	amount    decimal.Decimal
	pobID     string
}

func (l *Line) ID() string {
	if l == nil {
		return ""
	}
	return l.id
}

func (l *Line) Account() types.LedgerAccountKey {
	if l == nil {
		return ""
	}
	return l.account
}

func (l *Line) Direction() types.JournalDirection {
	if l == nil {
		return ""
	}
	return l.direction
}

func (l *Line) Amount() decimal.Decimal {
	if l == nil {
		return decimal.Zero
	}
	return l.amount
}

// POBID is empty on lines outside any POB.
func (l *Line) POBID() string {
	if l == nil {
		return ""
	}
	return l.pobID
}

// Net is the line's amount signed credit-positive.
// e.g. Dr unbilled 100 → −100; Cr revenue 100 → +100.
func (l *Line) Net() decimal.Decimal {
	if l.Direction() == types.JournalDirectionCredit {
		return l.Amount()
	}
	return l.Amount().Neg()
}

// LineBuilder updates a copy of a line.
type LineBuilder struct {
	line Line
}

// NewLineBuilder copies l; nil starts empty.
func NewLineBuilder(l *Line) *LineBuilder {
	b := &LineBuilder{}
	if l != nil {
		b.line = *l
	}
	return b
}

func (b *LineBuilder) WithID(id string) *LineBuilder {
	b.line.id = id
	return b
}

func (b *LineBuilder) WithAccount(account types.LedgerAccountKey) *LineBuilder {
	b.line.account = account
	return b
}

func (b *LineBuilder) WithDirection(direction types.JournalDirection) *LineBuilder {
	b.line.direction = direction
	return b
}

func (b *LineBuilder) WithAmount(amount decimal.Decimal) *LineBuilder {
	b.line.amount = amount
	return b
}

func (b *LineBuilder) WithPOBID(pobID string) *LineBuilder {
	b.line.pobID = pobID
	return b
}

func (b *LineBuilder) Build() *Line {
	l := b.line
	return &l
}
