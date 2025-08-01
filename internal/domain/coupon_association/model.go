package coupon_association

import (
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
