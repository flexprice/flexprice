package billingterms

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type ruleBuilder struct {
	rule *Rule
}

// NewRuleBuilder starts from a detached copy of the existing definition.
func NewRuleBuilder(rule *Rule) *ruleBuilder {
	if rule == nil {
		return &ruleBuilder{rule: &Rule{}}
	}
	cloned := *rule
	cloned.Value = clonePointer(rule.Value)
	cloned.BillOn = clonePointer(rule.BillOn)
	cloned.StartDate = clonePointer(rule.StartDate)
	cloned.EndDate = clonePointer(rule.EndDate)
	cloned.Interval = clonePointer(rule.Interval)
	cloned.IntervalCount = clonePointer(rule.IntervalCount)
	cloned.BillAt = clonePointer(rule.BillAt)
	cloned.Split = clonePointer(rule.Split)
	cloned.Metadata = cloneMetadata(rule.Metadata)
	return &ruleBuilder{rule: &cloned}
}

// WithID assigns the persistent identity of a newly planned rule.
func (b *ruleBuilder) WithID(id string) *ruleBuilder {
	if b == nil || b.rule == nil {
		return b
	}
	b.rule.ID = id
	return b
}

// WithStatus changes source visibility without replacing its definition.
func (b *ruleBuilder) WithStatus(status types.Status) *ruleBuilder {
	if b == nil || b.rule == nil {
		return b
	}
	b.rule.Status = status
	return b
}

// WithMetadata replaces metadata using a detached copy.
func (b *ruleBuilder) WithMetadata(metadata types.Metadata) *ruleBuilder {
	if b == nil || b.rule == nil {
		return b
	}
	b.rule.Metadata = cloneMetadata(metadata)
	return b
}

// Build returns the copied rule.
func (b *ruleBuilder) Build() *Rule {
	if b == nil {
		return nil
	}
	return b.rule
}

type scheduleItemBuilder struct {
	item *ScheduleItem
}

// NewScheduleItemBuilder starts from a detached copy of an existing charge.
func NewScheduleItemBuilder(item *ScheduleItem) *scheduleItemBuilder {
	if item == nil {
		return &scheduleItemBuilder{item: &ScheduleItem{}}
	}
	cloned := *item
	cloned.BillingTermID = clonePointer(item.BillingTermID)
	cloned.SubscriptionID = clonePointer(item.SubscriptionID)
	cloned.BillOn = clonePointer(item.BillOn)
	cloned.InvoiceID = clonePointer(item.InvoiceID)
	cloned.InvoiceLineItemID = clonePointer(item.InvoiceLineItemID)
	cloned.CancellationReason = clonePointer(item.CancellationReason)
	cloned.Metadata = cloneMetadata(item.Metadata)
	return &scheduleItemBuilder{item: &cloned}
}

// WithID assigns the persistent identity of a newly planned charge.
func (b *scheduleItemBuilder) WithID(id string) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.ID = id
	return b
}

// WithBillingTermID links a new charge to its persisted source rule.
func (b *scheduleItemBuilder) WithBillingTermID(id *string) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.BillingTermID = clonePointer(id)
	return b
}

// WithAmount materializes the rounded principal of a newly planned charge.
func (b *scheduleItemBuilder) WithAmount(amount decimal.Decimal) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Amount = amount
	return b
}

// WithStatus changes source visibility while preserving execution history.
func (b *scheduleItemBuilder) WithStatus(status types.Status) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Status = status
	return b
}

// WithBillOn records a resolved manual or fallback date.
func (b *scheduleItemBuilder) WithBillOn(date *time.Time) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.BillOn = clonePointer(date)
	return b
}

// WithScheduleStatus changes execution state without changing principal.
func (b *scheduleItemBuilder) WithScheduleStatus(status types.BillingScheduleStatus) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.ScheduleStatus = status
	return b
}

// WithInvoiceID retains the invoice that claimed this charge.
func (b *scheduleItemBuilder) WithInvoiceID(id *string) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.InvoiceID = clonePointer(id)
	return b
}

// WithInvoiceLineItemID retains the exact invoice line for this charge.
func (b *scheduleItemBuilder) WithInvoiceLineItemID(id *string) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.InvoiceLineItemID = clonePointer(id)
	return b
}

// WithCancellationReason records why collection stopped.
func (b *scheduleItemBuilder) WithCancellationReason(reason *types.BillingScheduleCancellationReason) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.CancellationReason = clonePointer(reason)
	return b
}

// WithMetadata replaces metadata using a detached copy.
func (b *scheduleItemBuilder) WithMetadata(metadata types.Metadata) *scheduleItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Metadata = cloneMetadata(metadata)
	return b
}

// Build returns the copied charge.
func (b *scheduleItemBuilder) Build() *ScheduleItem {
	if b == nil {
		return nil
	}
	return b.item
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMetadata(metadata types.Metadata) types.Metadata {
	if metadata == nil {
		return nil
	}
	cloned := make(types.Metadata, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
