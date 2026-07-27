package subscription

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// subscriptionLineItemBuilder copies an existing line item and applies field updates.
type subscriptionLineItemBuilder struct {
	item *SubscriptionLineItem
}

// NewSubscriptionLineItemBuilder returns a builder seeded from an existing line item.
func NewSubscriptionLineItemBuilder(lineItem *SubscriptionLineItem) *subscriptionLineItemBuilder {
	if lineItem == nil {
		return &subscriptionLineItemBuilder{item: &SubscriptionLineItem{}}
	}

	copied := *lineItem
	if lineItem.Metadata != nil {
		copied.Metadata = make(map[string]string, len(lineItem.Metadata))
		for k, v := range lineItem.Metadata {
			copied.Metadata[k] = v
		}
	}
	if lineItem.CommitmentTimeBuckets != nil {
		copied.CommitmentTimeBuckets = make(types.TimeOfDayBuckets, len(lineItem.CommitmentTimeBuckets))
		copy(copied.CommitmentTimeBuckets, lineItem.CommitmentTimeBuckets)
	}

	return &subscriptionLineItemBuilder{item: &copied}
}

func (b *subscriptionLineItemBuilder) WithID(id string) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.ID = id
	return b
}

func (b *subscriptionLineItemBuilder) WithQuantity(quantity decimal.Decimal) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.Quantity = quantity
	return b
}

func (b *subscriptionLineItemBuilder) WithStartDate(startDate time.Time) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.StartDate = startDate
	return b
}

func (b *subscriptionLineItemBuilder) WithEndDate(endDate time.Time) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.EndDate = endDate
	return b
}

func (b *subscriptionLineItemBuilder) WithBaseModel(baseModel types.BaseModel) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.BaseModel = baseModel
	return b
}

// Build returns the updated line item, or nil if the builder is nil.
func (b *subscriptionLineItemBuilder) Build() *SubscriptionLineItem {
	if b == nil {
		return nil
	}
	return b.item
}
