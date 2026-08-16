package subscription

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
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

func (b *subscriptionLineItemBuilder) WithPlan(planID, planName string) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil {
		return b
	}
	b.item.EntityID = planID
	b.item.EntityType = types.SubscriptionLineItemEntityTypePlan
	b.item.PlanDisplayName = planName
	return b
}

// WithPrice repoints a line item at another price, carrying over everything the
// line item mirrors from it. Quantity, dates and identity are untouched: this is
// for swapping the price a line bills against, not for rebuilding the line.
func (b *subscriptionLineItemBuilder) WithPrice(p *price.Price) *subscriptionLineItemBuilder {
	if b == nil || b.item == nil || p == nil {
		return b
	}

	b.item.PriceID = p.ID
	b.item.PriceType = p.Type
	b.item.BillingPeriod = p.BillingPeriod
	b.item.BillingPeriodCount = p.BillingPeriodCount
	b.item.InvoiceCadence = p.InvoiceCadence
	b.item.PriceUnitID = p.PriceUnitID
	b.item.PriceUnit = p.PriceUnit
	if p.DisplayName != "" {
		b.item.DisplayName = p.DisplayName
	}
	if p.Type == types.PRICE_TYPE_USAGE {
		b.item.MeterID = p.MeterID
	}
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
