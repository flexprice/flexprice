package invoice

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// invoiceBuilder copies an existing invoice and applies field updates, mirroring
// invoiceLineItemBuilder. Line items and coupon applications are separate entities:
// the seed's slices carry over by reference until replaced via their setters.
type invoiceBuilder struct {
	inv *Invoice
}

// NewInvoiceBuilder returns a builder seeded from an existing invoice.
func NewInvoiceBuilder(inv *Invoice) *invoiceBuilder {
	if inv == nil {
		return &invoiceBuilder{inv: &Invoice{}}
	}

	copied := *inv
	if inv.Metadata != nil {
		copied.Metadata = lo.Assign(types.Metadata{}, inv.Metadata)
	}

	return &invoiceBuilder{inv: &copied}
}

func (b *invoiceBuilder) WithID(id string) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.ID = id
	return b
}

func (b *invoiceBuilder) WithInvoiceStatus(status types.InvoiceStatus) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.InvoiceStatus = status
	return b
}

func (b *invoiceBuilder) WithPaymentStatus(status types.PaymentStatus) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.PaymentStatus = status
	return b
}

func (b *invoiceBuilder) WithAmountPaid(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.AmountPaid = amount
	return b
}

func (b *invoiceBuilder) WithTotalPrepaidCreditsApplied(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.TotalPrepaidCreditsApplied = amount
	return b
}

func (b *invoiceBuilder) WithRefundedAmount(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.RefundedAmount = amount
	return b
}

func (b *invoiceBuilder) WithAdjustmentAmount(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.AdjustmentAmount = amount
	return b
}

func (b *invoiceBuilder) WithTotalDiscount(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.TotalDiscount = amount
	return b
}

func (b *invoiceBuilder) WithTotalTax(amount decimal.Decimal) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.TotalTax = amount
	return b
}

func (b *invoiceBuilder) WithInvoiceNumber(number *string) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.InvoiceNumber = number
	return b
}

func (b *invoiceBuilder) WithIdempotencyKey(key *string) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.IdempotencyKey = key
	return b
}

func (b *invoiceBuilder) WithRecalculatedInvoiceID(id *string) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.RecalculatedInvoiceID = id
	return b
}

func (b *invoiceBuilder) WithVoidedAt(t *time.Time) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.VoidedAt = t
	return b
}

func (b *invoiceBuilder) WithFinalizedAt(t *time.Time) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.FinalizedAt = t
	return b
}

func (b *invoiceBuilder) WithPaidAt(t *time.Time) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.PaidAt = t
	return b
}

func (b *invoiceBuilder) WithLastComputedAt(t *time.Time) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.LastComputedAt = t
	return b
}

func (b *invoiceBuilder) WithLineItems(items []*InvoiceLineItem) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.LineItems = items
	return b
}

func (b *invoiceBuilder) WithCouponApplications(apps []*coupon_application.CouponApplication) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.CouponApplications = apps
	return b
}

func (b *invoiceBuilder) WithTaxExemptionReasonCode(code *types.TaxExemptionReasonCode) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.TaxExemptionReasonCode = code
	return b
}

func (b *invoiceBuilder) WithIsManuallyEdited(edited bool) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.IsManuallyEdited = edited
	return b
}

func (b *invoiceBuilder) WithVersion(version int) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.Version = version
	return b
}

func (b *invoiceBuilder) WithBaseModel(baseModel types.BaseModel) *invoiceBuilder {
	if b == nil || b.inv == nil {
		return b
	}
	b.inv.BaseModel = baseModel
	return b
}

// Build returns the updated invoice, or nil if the builder is nil.
func (b *invoiceBuilder) Build() *Invoice {
	if b == nil {
		return nil
	}
	return b.inv
}

// CopyForDraftEdit returns a DRAFT copy for the void-and-recreate edit flow: business
// data (description, billing period/interval, dates, metadata, subtotal) carries over;
// payment state, discount/tax, number, timestamps, and lineage reset. Line items are the caller's.
func (i *Invoice) CopyForDraftEdit(id string, baseModel types.BaseModel) *Invoice {
	return NewInvoiceBuilder(i).
		WithID(id).
		WithInvoiceStatus(types.InvoiceStatusDraft).
		WithPaymentStatus(types.PaymentStatusPending).
		WithAmountPaid(decimal.Zero).
		WithTotalPrepaidCreditsApplied(decimal.Zero).
		WithRefundedAmount(decimal.Zero).
		WithAdjustmentAmount(decimal.Zero).
		WithTotalDiscount(decimal.Zero).
		WithTotalTax(decimal.Zero).
		WithInvoiceNumber(nil).
		// Deterministic key so a retried recreate cannot race a duplicate.
		WithIdempotencyKey(lo.ToPtr("void_recreate-" + i.ID)).
		WithRecalculatedInvoiceID(nil).
		WithVoidedAt(nil).
		WithFinalizedAt(nil).
		WithPaidAt(nil).
		WithLastComputedAt(nil).
		WithLineItems(nil).
		WithCouponApplications(nil).
		WithIsManuallyEdited(true).
		WithVersion(1).
		WithBaseModel(baseModel).
		Build()
}
