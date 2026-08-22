package entitlement

import (
	"time"
)

// entitlementBuilder copies an existing entitlement and applies field updates.
type entitlementBuilder struct {
	entitlement *Entitlement
}

// NewEntitlementBuilder returns a builder seeded from an existing entitlement.
func NewEntitlementBuilder(e *Entitlement) *entitlementBuilder {
	if e == nil {
		return &entitlementBuilder{entitlement: &Entitlement{}}
	}

	copied := *e
	if e.ConfigValue != nil {
		copied.ConfigValue = make(map[string]interface{}, len(e.ConfigValue))
		for k, v := range e.ConfigValue {
			copied.ConfigValue[k] = v
		}
	}

	return &entitlementBuilder{entitlement: &copied}
}

func (b *entitlementBuilder) WithEndDate(endDate time.Time) *entitlementBuilder {
	if b == nil || b.entitlement == nil {
		return b
	}
	b.entitlement.EndDate = &endDate
	return b
}

func (b *entitlementBuilder) WithUpdatedBy(userID string) *entitlementBuilder {
	if b == nil || b.entitlement == nil {
		return b
	}
	b.entitlement.UpdatedBy = userID
	b.entitlement.UpdatedAt = time.Now().UTC()
	return b
}

func (b *entitlementBuilder) Build() *Entitlement {
	if b == nil {
		return nil
	}
	return b.entitlement
}
