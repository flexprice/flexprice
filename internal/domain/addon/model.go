package addon

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Addon represents an addon in the domain
type Addon struct {
	ID            string                 `json:"id,omitempty"`
	LookupKey     string                 `json:"lookup_key,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Type          types.AddonType        `json:"type,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	EnvironmentID string                 `json:"environment_id,omitempty"`
	types.BaseModel
}

// SubscriptionAddon represents the relationship between a subscription and an addon
type SubscriptionAddon struct {
	ID             string     `db:"id" json:"id"`
	SubscriptionID string     `db:"subscription_id" json:"subscription_id"`
	AddonID        string     `db:"addon_id" json:"addon_id"`
	PriceID        string     `db:"price_id" json:"price_id"`
	Quantity       int        `db:"quantity" json:"quantity"`
	StartDate      *time.Time `db:"start_date" json:"start_date,omitempty"`
	EndDate        *time.Time `db:"end_date" json:"end_date,omitempty"`

	// Lifecycle management
	AddonStatus        types.AddonStatus `db:"addon_status" json:"addon_status"`
	CancellationReason string            `db:"cancellation_reason" json:"cancellation_reason,omitempty"`
	CancelledAt        *time.Time        `db:"cancelled_at" json:"cancelled_at,omitempty"`

	// Proration support
	ProrationBehavior types.ProrationBehavior `db:"proration_behavior" json:"proration_behavior"`
	ProratedAmount    *decimal.Decimal        `db:"prorated_amount" json:"prorated_amount,omitempty"`

	// Usage tracking
	UsageLimit       *decimal.Decimal `db:"usage_limit" json:"usage_limit,omitempty"`
	UsageResetPeriod string           `db:"usage_reset_period" json:"usage_reset_period,omitempty"`
	UsageResetDate   *time.Time       `db:"usage_reset_date" json:"usage_reset_date,omitempty"`

	Metadata      map[string]interface{} `db:"metadata" json:"metadata,omitempty"`
	EnvironmentID string                 `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt creates domain model from ent model
func (a *Addon) FromEnt(entAddon *ent.Addon) *Addon {
	addon := &Addon{
		ID:            entAddon.ID,
		LookupKey:     entAddon.LookupKey,
		Name:          entAddon.Name,
		Description:   entAddon.Description,
		Type:          types.AddonType(entAddon.Type),
		Metadata:      entAddon.Metadata,
		EnvironmentID: entAddon.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  entAddon.TenantID,
			Status:    types.Status(entAddon.Status),
			CreatedAt: entAddon.CreatedAt,
			UpdatedAt: entAddon.UpdatedAt,
			CreatedBy: entAddon.CreatedBy,
			UpdatedBy: entAddon.UpdatedBy,
		},
	}

	return addon
}

func (a *Addon) FromEntList(entAddons []*ent.Addon) []*Addon {
	return lo.Map(entAddons, func(entAddon *ent.Addon, _ int) *Addon {
		return a.FromEnt(entAddon)
	})
}
