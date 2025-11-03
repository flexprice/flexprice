package dto

import (
	"context"
	"time"

	couponAssociation "github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateCouponAssociationRequest represents the request to create a new coupon association
type CreateCouponAssociationRequest struct {
	CouponID               string            `json:"coupon_id" validate:"required"`
	SubscriptionID         string            `json:"subscription_id" validate:"required"`
	SubscriptionLineItemID *string           `json:"subscription_line_item_id,omitempty"`
	SubscriptionPhaseID    *string           `json:"subscription_phase_id,omitempty"`
	StartDate              *time.Time        `json:"start_date,omitempty"`
	EndDate                *time.Time        `json:"end_date,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

// CouponAssociationResponse represents the response for coupon association data
type CouponAssociationResponse struct {
	*couponAssociation.CouponAssociation `json:",inline"`
}

// ListCouponAssociationsResponse represents the response for listing coupon associations
type ListCouponAssociationsResponse = types.ListResponse[*CouponAssociationResponse]

// Validate validates the CreateCouponAssociationRequest
func (r *CreateCouponAssociationRequest) Validate() error {
	if r.CouponID == "" {
		return ierr.NewError("coupon_id is required").
			WithHint("Please provide a valid coupon ID").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreateCouponAssociationRequest) ToCouponAssociation(ctx context.Context, couponID string, subscriptionID string, subscriptionLineItemID string) *couponAssociation.CouponAssociation {
	startDate := time.Now()
	if r.StartDate != nil {
		startDate = *r.StartDate
	}

	return &couponAssociation.CouponAssociation{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:               couponID,
		SubscriptionID:         r.SubscriptionID,
		SubscriptionLineItemID: r.SubscriptionLineItemID,
		SubscriptionPhaseID:    r.SubscriptionPhaseID,
		StartDate:              startDate,
		EndDate:                r.EndDate,
		Metadata:               r.Metadata,
		BaseModel:              types.GetDefaultBaseModel(ctx),
		EnvironmentID:          types.GetEnvironmentID(ctx),
	}
}
