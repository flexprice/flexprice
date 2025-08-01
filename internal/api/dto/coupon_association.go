package dto

import (
	"context"

	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateCouponAssociationRequest represents the request to create a new coupon association
type CreateCouponAssociationRequest struct {
	CouponID               string            `json:"coupon_id" validate:"required"`
	SubscriptionID         string            `json:"subscription_id" validate:"required"`
	SubscriptionLineItemID *string           `json:"subscription_line_item_id,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

// CouponAssociationResponse represents the response for coupon association data
type CouponAssociationResponse struct {
	*coupon_association.CouponAssociation `json:",inline"`
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

func (r *CreateCouponAssociationRequest) ToCouponAssociation(ctx context.Context, couponID string, subscriptionID string, subscriptionLineItemID string) *coupon_association.CouponAssociation {
	return &coupon_association.CouponAssociation{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:               couponID,
		SubscriptionID:         r.SubscriptionID,
		SubscriptionLineItemID: r.SubscriptionLineItemID,
		Metadata:               r.Metadata,
		BaseModel:              types.GetDefaultBaseModel(ctx),
		EnvironmentID:          types.GetEnvironmentID(ctx),
	}
}
