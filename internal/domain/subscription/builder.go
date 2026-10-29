package subscription

import (
	"maps"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionBuilder struct {
	subscription *Subscription
}

// NewSubscriptionBuilder copies an existing subscription for field updates.
func NewSubscriptionBuilder(existing *Subscription) *subscriptionBuilder {
	if existing == nil {
		return &subscriptionBuilder{subscription: &Subscription{}}
	}
	copied := *existing
	copied.Metadata = maps.Clone(existing.Metadata)
	return &subscriptionBuilder{subscription: &copied}
}

// WithStatus sets the subscription lifecycle status.
func (b *subscriptionBuilder) WithStatus(status types.SubscriptionStatus) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.SubscriptionStatus = status
	}
	return b
}

// WithParentSubscriptionID sets or clears the parent reference.
func (b *subscriptionBuilder) WithParentSubscriptionID(id *string) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.ParentSubscriptionID = nil
		if id != nil {
			value := *id
			b.subscription.ParentSubscriptionID = &value
		}
	}
	return b
}

// WithSubscriptionType sets the subscription's invoicing membership role.
func (b *subscriptionBuilder) WithSubscriptionType(value types.SubscriptionType) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.SubscriptionType = value
	}
	return b
}

// WithCancelAt sets or clears the scheduled cancellation date.
func (b *subscriptionBuilder) WithCancelAt(date *time.Time) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.CancelAt = nil
		if date != nil {
			value := *date
			b.subscription.CancelAt = &value
		}
	}
	return b
}

// WithCancelledAt sets or clears the effective cancellation timestamp.
func (b *subscriptionBuilder) WithCancelledAt(date *time.Time) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.CancelledAt = nil
		if date != nil {
			value := *date
			b.subscription.CancelledAt = &value
		}
	}
	return b
}

// WithEndDate sets or clears the subscription end date.
func (b *subscriptionBuilder) WithEndDate(date *time.Time) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.EndDate = nil
		if date != nil {
			value := *date
			b.subscription.EndDate = &value
		}
	}
	return b
}

// WithCurrentPeriodEnd sets the current billing period boundary.
func (b *subscriptionBuilder) WithCurrentPeriodEnd(date time.Time) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.CurrentPeriodEnd = date
	}
	return b
}

// WithCurrentPeriodStart sets the current billing period's start.
func (b *subscriptionBuilder) WithCurrentPeriodStart(date time.Time) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.CurrentPeriodStart = date
	}
	return b
}

// WithPauseStatus sets the subscription's pause state.
func (b *subscriptionBuilder) WithPauseStatus(status types.PauseStatus) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.PauseStatus = status
	}
	return b
}

// WithActivePauseID sets or clears the active pause reference.
func (b *subscriptionBuilder) WithActivePauseID(id *string) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.ActivePauseID = nil
		if id != nil {
			value := *id
			b.subscription.ActivePauseID = &value
		}
	}
	return b
}

// WithProrationBehavior sets how partial periods are billed.
func (b *subscriptionBuilder) WithProrationBehavior(behavior types.ProrationBehavior) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.ProrationBehavior = behavior
	}
	return b
}

// WithCancelAtPeriodEnd sets the cancellation boundary flag.
func (b *subscriptionBuilder) WithCancelAtPeriodEnd(cancel bool) *subscriptionBuilder {
	if b != nil && b.subscription != nil {
		b.subscription.CancelAtPeriodEnd = cancel
	}
	return b
}

// Build returns the updated subscription, or nil for a nil builder.
func (b *subscriptionBuilder) Build() *Subscription {
	if b == nil {
		return nil
	}
	return b.subscription
}
