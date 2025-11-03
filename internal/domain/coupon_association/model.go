package coupon_association

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CouponAssociation represents a coupon association with a subscription or subscription line item
// subscription_id is mandatory, subscription_line_item_id is optional
type CouponAssociation struct {
	ID                     string            `json:"id" db:"id"`
	CouponID               string            `json:"coupon_id" db:"coupon_id"`
	SubscriptionID         string            `json:"subscription_id" db:"subscription_id"`                               // Mandatory
	SubscriptionLineItemID *string           `json:"subscription_line_item_id,omitempty" db:"subscription_line_item_id"` // Optional
	SubscriptionPhaseID    *string           `json:"subscription_phase_id,omitempty" db:"subscription_phase_id"`         // Optional
	StartDate              time.Time         `json:"start_date" db:"start_date"`
	EndDate                *time.Time        `json:"end_date,omitempty" db:"end_date"` // Optional
	Metadata               map[string]string `json:"metadata,omitempty" db:"metadata"`
	EnvironmentID          string            `json:"environment_id" db:"environment_id"`
	Coupon                 *coupon.Coupon    `json:"coupon,omitempty" db:"coupon"`
	types.BaseModel
}

// Write a validate method for the coupon association

func (ca *CouponAssociation) Validate() error {
	if ca.CouponID == "" {
		return ierr.NewError("coupon validation failed").WithHint("coupon is required").Mark(ierr.ErrValidation)
	}

	if ca.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").WithHint("subscription_id is required").Mark(ierr.ErrValidation)
	}

	return nil
}

func FromEnt(e *ent.CouponAssociation) *CouponAssociation {
	if e == nil {
		return nil
	}

	coupon := coupon.FromEnt(e.Edges.Coupon)
	return &CouponAssociation{
		ID:                     e.ID,
		CouponID:               e.CouponID,
		SubscriptionID:         e.SubscriptionID,
		SubscriptionLineItemID: e.SubscriptionLineItemID,
		SubscriptionPhaseID:    e.SubscriptionPhaseID,
		StartDate:              e.StartDate,
		EndDate:                e.EndDate,
		Metadata:               e.Metadata,
		EnvironmentID:          e.EnvironmentID,
		Coupon:                 coupon,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

func FromEntList(list []*ent.CouponAssociation) []*CouponAssociation {
	if list == nil {
		return nil
	}
	couponAssociations := make([]*CouponAssociation, len(list))
	for i, item := range list {
		couponAssociations[i] = FromEnt(item)
	}
	return couponAssociations
}

// Filter represents filter criteria for querying coupon associations
type Filter struct {
	// SubscriptionID filters by subscription ID
	SubscriptionID string `json:"subscription_id"`

	// ActivePeriodStart is the start of the period to check if associations are active
	// Used when ActiveOnly is true
	ActivePeriodStart *time.Time `json:"active_period_start,omitempty"`

	// ActivePeriodEnd is the end of the period to check if associations are active
	// Used when ActiveOnly is true
	ActivePeriodEnd *time.Time `json:"active_period_end,omitempty"`

	// IncludeLineItems when true, includes both line item-level and subscription-level associations
	// When false (default), includes only subscription-level associations (those without SubscriptionLineItemID)
	IncludeLineItems bool `json:"include_line_items"`

	// ActiveOnly when true, filters to only return associations active during ActivePeriodStart/ActivePeriodEnd
	// An association is active during a period if:
	// - start_date <= active_period_end (association started before or during the period)
	// - AND (end_date IS NULL OR end_date >= active_period_start) (association hasn't ended before the period or is indefinite)
	ActiveOnly bool `json:"active_only"`

	// WithCoupon when true, includes the coupon relation in the response
	WithCoupon bool `json:"with_coupon"`
}
