package invoice

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// invoiceLineItemBuilder copies an existing line item and applies field updates.
type invoiceLineItemBuilder struct {
	item *InvoiceLineItem
}

// NewInvoiceLineItemBuilder returns a builder seeded from an existing line item.
func NewInvoiceLineItemBuilder(lineItem *InvoiceLineItem) *invoiceLineItemBuilder {
	if lineItem == nil {
		return &invoiceLineItemBuilder{item: &InvoiceLineItem{}}
	}

	copied := *lineItem
	if lineItem.Metadata != nil {
		copied.Metadata = lo.Assign(types.Metadata{}, lineItem.Metadata)
	}

	return &invoiceLineItemBuilder{item: &copied}
}

func (b *invoiceLineItemBuilder) WithID(id string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.ID = id
	return b
}

func (b *invoiceLineItemBuilder) WithInvoiceID(invoiceID string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.InvoiceID = invoiceID
	return b
}

func (b *invoiceLineItemBuilder) WithCustomerID(customerID string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.CustomerID = customerID
	return b
}

func (b *invoiceLineItemBuilder) WithEnvironmentID(environmentID string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.EnvironmentID = environmentID
	return b
}

func (b *invoiceLineItemBuilder) WithDisplayName(displayName *string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.DisplayName = displayName
	return b
}

// WithDescription stores the description in Metadata["description"] — the convention
// the billing engine writes and the invoice PDF renderer reads.
func (b *invoiceLineItemBuilder) WithDescription(description *string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil || description == nil {
		return b
	}
	if b.item.Metadata == nil {
		b.item.Metadata = types.Metadata{}
	}
	b.item.Metadata["description"] = *description
	return b
}

func (b *invoiceLineItemBuilder) WithPeriodStart(periodStart *time.Time) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.PeriodStart = periodStart
	return b
}

func (b *invoiceLineItemBuilder) WithPeriodEnd(periodEnd *time.Time) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.PeriodEnd = periodEnd
	return b
}

func (b *invoiceLineItemBuilder) WithAmount(amount decimal.Decimal) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Amount = amount
	return b
}

func (b *invoiceLineItemBuilder) WithQuantity(quantity decimal.Decimal) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Quantity = quantity
	return b
}

func (b *invoiceLineItemBuilder) WithCurrency(currency string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Currency = currency
	return b
}

func (b *invoiceLineItemBuilder) WithParentLineItemID(parentLineItemID *string) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.ParentLineItemID = parentLineItemID
	return b
}

func (b *invoiceLineItemBuilder) WithBaseModel(baseModel types.BaseModel) *invoiceLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.BaseModel = baseModel
	return b
}

// Build returns the updated line item, or nil if the builder is nil.
func (b *invoiceLineItemBuilder) Build() *InvoiceLineItem {
	if b == nil {
		return nil
	}
	return b.item
}
