package subscription

import (
	"maps"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionPauseBuilder struct {
	pause *SubscriptionPause
}

// NewSubscriptionPauseBuilder copies an existing pause for field updates.
func NewSubscriptionPauseBuilder(existing *SubscriptionPause) *subscriptionPauseBuilder {
	if existing == nil {
		return &subscriptionPauseBuilder{pause: &SubscriptionPause{}}
	}
	copied := *existing
	copied.Metadata = maps.Clone(existing.Metadata)
	return &subscriptionPauseBuilder{pause: &copied}
}

// WithStatus sets the pause's lifecycle state.
func (b *subscriptionPauseBuilder) WithStatus(status types.PauseStatus) *subscriptionPauseBuilder {
	if b != nil && b.pause != nil {
		b.pause.PauseStatus = status
	}
	return b
}

// WithResumption records when and how the pause ended.
func (b *subscriptionPauseBuilder) WithResumption(at time.Time, mode types.ResumeMode) *subscriptionPauseBuilder {
	if b != nil && b.pause != nil {
		b.pause.ResumedAt = &at
		b.pause.ResumeMode = mode
	}
	return b
}

// WithUpdatedBy sets the actor responsible for the update.
func (b *subscriptionPauseBuilder) WithUpdatedBy(id string) *subscriptionPauseBuilder {
	if b != nil && b.pause != nil {
		b.pause.UpdatedBy = id
	}
	return b
}

// WithMetadata merges the supplied metadata into the copied pause.
func (b *subscriptionPauseBuilder) WithMetadata(metadata types.Metadata) *subscriptionPauseBuilder {
	if b != nil && b.pause != nil && len(metadata) > 0 {
		if b.pause.Metadata == nil {
			b.pause.Metadata = types.Metadata{}
		}
		for key, value := range metadata {
			b.pause.Metadata[key] = value
		}
	}
	return b
}

// Build returns the copied pause, or nil for a nil builder.
func (b *subscriptionPauseBuilder) Build() *SubscriptionPause {
	if b == nil {
		return nil
	}
	return b.pause
}
