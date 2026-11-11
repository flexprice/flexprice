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

func (b *commitmentBuilder) WithCommitmentStatus(s types.CommitmentStatus) *commitmentBuilder {
	b.c.CommitmentStatus = s
	return b
}

func (b *commitmentBuilder) WithTerminatedAt(t time.Time) *commitmentBuilder {
	b.c.TerminatedAt = &t
	return b
}

// WithoutTerminatedAt clears terminated_at, as a revoked termination leaves it.
func (b *commitmentBuilder) WithoutTerminatedAt() *commitmentBuilder {
	b.c.TerminatedAt = nil
	return b
}

// WithTotalAmount sets the total the billing terms split into instalments.
func (b *commitmentBuilder) WithTotalAmount(v decimal.Decimal) *commitmentBuilder {
	b.c.TotalAmount = &v
	return b
}

// WithInstalmentInvoicing sets how the commitment's instalments are delivered.
func (b *commitmentBuilder) WithInstalmentInvoicing(v types.InstalmentDelivery) *commitmentBuilder {
	b.c.InstalmentInvoicing = &v
	return b
}

func (b *commitmentBuilder) Build() *Commitment {
	return b.c
}
