package revenuefact

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// revenueFactBuilder copies an existing fact and applies field updates,
// mirroring the invoiceBuilder pattern: nil-safe seed, nil-guarded setters,
// Build returns a fresh copy so the seed is never mutated.
type revenueFactBuilder struct {
	f *RevenueFact
}

// NewRevenueFactBuilder returns a builder seeded from an existing fact.
func NewRevenueFactBuilder(f *RevenueFact) *revenueFactBuilder {
	if f == nil {
		return &revenueFactBuilder{f: &RevenueFact{}}
	}
	copied := *f
	return &revenueFactBuilder{f: &copied}
}

func (b *revenueFactBuilder) WithID(id string) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.ID = id
	return b
}

func (b *revenueFactBuilder) WithStatus(status types.FactStatus) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.Status = status
	return b
}

func (b *revenueFactBuilder) WithIsRevert(isRevert bool) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.IsRevert = isRevert
	return b
}

func (b *revenueFactBuilder) WithInvoiceRef(invoiceID, invoiceLineItemID *string) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.InvoiceID = invoiceID
	b.f.InvoiceLineItemID = invoiceLineItemID
	return b
}

func (b *revenueFactBuilder) WithComputedAt(t time.Time) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.ComputedAt = t
	return b
}

func (b *revenueFactBuilder) WithVersion(v int64) *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.Version = v
	return b
}

// WithNegatedAmounts flips the sign of every amount/quantity column — the
// contra-entry shape a revert row carries.
func (b *revenueFactBuilder) WithNegatedAmounts() *revenueFactBuilder {
	if b == nil || b.f == nil {
		return b
	}
	b.f.UsageAtListRate = b.f.UsageAtListRate.Neg()
	b.f.TierDelta = b.f.TierDelta.Neg()
	b.f.EntitlementAmount = b.f.EntitlementAmount.Neg()
	b.f.LineDiscount = b.f.LineDiscount.Neg()
	b.f.InvoiceDiscount = b.f.InvoiceDiscount.Neg()
	b.f.NetAmount = b.f.NetAmount.Neg()
	b.f.BillableQty = b.f.BillableQty.Neg()
	b.f.EntitlementQty = b.f.EntitlementQty.Neg()
	return b
}

func (b *revenueFactBuilder) Build() *RevenueFact {
	if b == nil || b.f == nil {
		return &RevenueFact{}
	}
	copied := *b.f
	return &copied
}

// NewRevert returns the negating twin of a FINAL fact whose invoice was
// voided: same grain and invoice stamps, negated amounts, is_revert=true.
func NewRevert(f *RevenueFact, computedAt time.Time) *RevenueFact {
	return NewRevenueFactBuilder(f).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REVENUE_FACT)).
		WithNegatedAmounts().
		WithIsRevert(true).
		WithStatus(types.FactFinal).
		WithComputedAt(computedAt).
		WithVersion(1).
		Build()
}
