package commitment

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type commitmentBuilder struct {
	c *Commitment
}

// NewCommitmentBuilder starts from a copy of c (or an empty commitment when c is nil).
func NewCommitmentBuilder(c *Commitment) *commitmentBuilder {
	if c == nil {
		return &commitmentBuilder{c: &Commitment{}}
	}

	cp := *c
	cp.ActiveWindows = append(types.ActiveWindows(nil), c.ActiveWindows...)
	cp.Metadata = lo.Assign(types.Metadata{}, c.Metadata)

	return &commitmentBuilder{c: &cp}
}

func (b *commitmentBuilder) WithName(v string) *commitmentBuilder {
	b.c.Name = v
	return b
}

func (b *commitmentBuilder) WithInvoiceLabel(v string) *commitmentBuilder {
	b.c.InvoiceLabel = v
	return b
}

func (b *commitmentBuilder) WithInvoiceDescription(v *string) *commitmentBuilder {
	b.c.InvoiceDescription = v
	return b
}

func (b *commitmentBuilder) WithMetadata(m types.Metadata) *commitmentBuilder {
	b.c.Metadata = m
	return b
}

func (b *commitmentBuilder) WithStatus(s types.CommitmentStatus) *commitmentBuilder {
	b.c.CommitmentStatus = s
	return b
}

func (b *commitmentBuilder) WithTerminatedAt(t time.Time) *commitmentBuilder {
	b.c.TerminatedAt = &t
	return b
}

// WithTotalAmount sets the currency-rounded contractual collection target.
func (b *commitmentBuilder) WithTotalAmount(amount decimal.Decimal) *commitmentBuilder {
	b.c.TotalAmount = &amount
	return b
}

// WithInvoiceDelivery maps collection delivery onto the existing storage field.
func (b *commitmentBuilder) WithInvoiceDelivery(delivery types.BillingTermDelivery) *commitmentBuilder {
	b.c.InstalmentInvoicing = lo.ToPtr(string(delivery))
	return b
}

func (b *commitmentBuilder) Build() *Commitment {
	return b.c
}
