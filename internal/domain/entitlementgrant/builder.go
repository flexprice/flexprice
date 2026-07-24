package entitlementgrant

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// entitlementGrantBuilder copies an existing grant and applies field updates.
type entitlementGrantBuilder struct {
	grant *EntitlementGrant
}

// NewEntitlementGrantBuilder returns a builder seeded from an existing grant,
// or an empty one when nil (fresh construction).
func NewEntitlementGrantBuilder(g *EntitlementGrant) *entitlementGrantBuilder {
	if g == nil {
		return &entitlementGrantBuilder{grant: &EntitlementGrant{}}
	}
	copied := *g
	if g.LastComputedAt != nil {
		t := *g.LastComputedAt
		copied.LastComputedAt = &t
	}
	return &entitlementGrantBuilder{grant: &copied}
}

func (b *entitlementGrantBuilder) WithID(id string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.ID = id
	return b
}

func (b *entitlementGrantBuilder) WithEntitlementConfigID(id string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.EntitlementConfigID = id
	return b
}

func (b *entitlementGrantBuilder) WithCustomerID(id string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.CustomerID = id
	return b
}

func (b *entitlementGrantBuilder) WithSubscriptionID(id string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.SubscriptionID = id
	return b
}

func (b *entitlementGrantBuilder) WithScope(t types.EntitlementGrantScopeEntityType, entityID string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.ScopeEntityType = t
	b.grant.ScopeEntityID = entityID
	return b
}

func (b *entitlementGrantBuilder) WithMeasure(m types.EntitlementGrantMeasure) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.Measure = m
	return b
}

func (b *entitlementGrantBuilder) WithQuota(q decimal.Decimal) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.Quota = q
	return b
}

func (b *entitlementGrantBuilder) WithUsage(u decimal.Decimal) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.Usage = u
	return b
}

func (b *entitlementGrantBuilder) WithWindow(validFrom, validTo time.Time) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.ValidFrom = validFrom
	b.grant.ValidTo = validTo
	return b
}

func (b *entitlementGrantBuilder) WithGrantStatus(s types.EntitlementGrantStatus) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.GrantStatus = s
	return b
}

func (b *entitlementGrantBuilder) WithLastComputedAt(t *time.Time) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.LastComputedAt = t
	return b
}

func (b *entitlementGrantBuilder) WithEnvironmentID(id string) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.EnvironmentID = id
	return b
}

func (b *entitlementGrantBuilder) WithBaseModel(m types.BaseModel) *entitlementGrantBuilder {
	if b == nil || b.grant == nil {
		return b
	}
	b.grant.BaseModel = m
	return b
}

// Build returns the updated grant, or nil if the builder is nil.
func (b *entitlementGrantBuilder) Build() *EntitlementGrant {
	if b == nil {
		return nil
	}
	return b.grant
}
