package addonassociation

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// addonAssociationBuilder copies an existing association and applies field updates,
// so callers never mutate the row they read.
type addonAssociationBuilder struct {
	association *AddonAssociation
}

// NewAddonAssociationBuilder returns a builder seeded from an existing association.
func NewAddonAssociationBuilder(association *AddonAssociation) *addonAssociationBuilder {
	if association == nil {
		return &addonAssociationBuilder{association: &AddonAssociation{}}
	}

	copied := *association
	if association.Metadata != nil {
		copied.Metadata = make(map[string]interface{}, len(association.Metadata))
		for k, v := range association.Metadata {
			copied.Metadata[k] = v
		}
	}

	return &addonAssociationBuilder{association: &copied}
}

// WithCancellation ends the attachment at effectiveAt: the three fields that record
// a cancellation always move together.
func (b *addonAssociationBuilder) WithCancellation(effectiveAt time.Time, reason string) *addonAssociationBuilder {
	if b == nil || b.association == nil {
		return b
	}

	b.association.EndDate = lo.ToPtr(effectiveAt)
	b.association.CancelledAt = lo.ToPtr(effectiveAt)
	b.association.AddonStatus = types.AddonStatusCancelled
	if reason != "" {
		b.association.CancellationReason = reason
	}
	return b
}

func (b *addonAssociationBuilder) Build() *AddonAssociation {
	if b == nil {
		return nil
	}
	return b.association
}
