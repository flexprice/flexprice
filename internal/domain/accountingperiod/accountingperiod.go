package accountingperiod

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// AccountingPeriod is one calendar month of an environment's ledger that journal entries post to.
type AccountingPeriod struct {
	id           string
	periodStart  time.Time
	periodStatus types.AccountingPeriodStatus
	status       types.Status
	createdAt    time.Time
	updatedAt    time.Time
}

func (p *AccountingPeriod) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *AccountingPeriod) PeriodStart() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.periodStart
}

// PeriodEnd is the exclusive end: the next month start.
func (p *AccountingPeriod) PeriodEnd() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.periodStart.AddDate(0, 1, 0)
}

func (p *AccountingPeriod) PeriodStatus() types.AccountingPeriodStatus {
	if p == nil {
		return ""
	}
	return p.periodStatus
}

func (p *AccountingPeriod) Status() types.Status {
	if p == nil {
		return ""
	}
	return p.status
}

func (p *AccountingPeriod) CreatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.createdAt
}

func (p *AccountingPeriod) UpdatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.updatedAt
}

// Builder copies an existing accounting period and applies field updates to the copy.
type Builder struct {
	period AccountingPeriod
}

// NewBuilder returns a builder seeded from p; a nil p starts empty.
func NewBuilder(p *AccountingPeriod) *Builder {
	b := &Builder{}
	if p != nil {
		b.period = *p
	}
	return b
}

func (b *Builder) WithID(id string) *Builder {
	b.period.id = id
	return b
}

func (b *Builder) WithPeriodStart(periodStart time.Time) *Builder {
	b.period.periodStart = periodStart
	return b
}

func (b *Builder) WithPeriodStatus(periodStatus types.AccountingPeriodStatus) *Builder {
	b.period.periodStatus = periodStatus
	return b
}

func (b *Builder) WithStatus(status types.Status) *Builder {
	b.period.status = status
	return b
}

func (b *Builder) WithCreatedAt(createdAt time.Time) *Builder {
	b.period.createdAt = createdAt
	return b
}

func (b *Builder) WithUpdatedAt(updatedAt time.Time) *Builder {
	b.period.updatedAt = updatedAt
	return b
}

func (b *Builder) Build() *AccountingPeriod {
	p := b.period
	return &p
}
