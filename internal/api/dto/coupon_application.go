package dto

import (
	coupon_application "github.com/flexprice/flexprice/internal/domain/coupon_application"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreateCouponApplicationRequest represents the request to create a new coupon application
type CreateCouponApplicationRequest struct {
	CouponID            string                 `json:"coupon_id" validate:"required"`
	CouponAssociationID string                 `json:"coupon_association_id,omitempty"`
	InvoiceID           string                 `json:"invoice_id" validate:"required"`
	InvoiceLineItemID   *string                `json:"invoice_line_item_id,omitempty"`
	SubscriptionID      *string                `json:"subscription_id,omitempty"`
	OriginalPrice       decimal.Decimal        `json:"original_price" validate:"required"`
	FinalPrice          decimal.Decimal        `json:"final_price" validate:"required"`
	DiscountedAmount    decimal.Decimal        `json:"discounted_amount" validate:"required"`
	DiscountType        types.CouponType       `json:"discount_type" validate:"required"`
	DiscountPercentage  *decimal.Decimal       `json:"discount_percentage,omitempty"`
	Currency            string                 `json:"currency" validate:"required"`
	CouponSnapshot      map[string]interface{} `json:"coupon_snapshot,omitempty"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
}

// CouponApplicationResponse represents the response for coupon application data
type CouponApplicationResponse struct {
	*coupon_application.CouponApplication `json:",inline"`
}

// ListCouponApplicationsResponse represents the response for listing coupon applications
type ListCouponApplicationsResponse = types.ListResponse[*CouponApplicationResponse]

// Validate validates the CreateCouponApplicationRequest
func (r *CreateCouponApplicationRequest) Validate() error {
	if r.CouponID == "" {
		return ierr.NewError("coupon_id is required").
			WithHint("Please provide a valid coupon ID").
			Mark(ierr.ErrValidation)
	}

	if r.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Please provide a valid invoice ID").
			Mark(ierr.ErrValidation)
	}

	if r.OriginalPrice.LessThan(decimal.Zero) {
		return ierr.NewError("original_price must be greater than zero").
			WithHint("Please provide a valid original price").
			Mark(ierr.ErrValidation)
	}

	if r.FinalPrice.LessThan(decimal.Zero) {
		return ierr.NewError("final_price cannot be negative").
			WithHint("Please provide a valid final price").
			Mark(ierr.ErrValidation)
	}

	if r.DiscountedAmount.LessThan(decimal.Zero) {
		return ierr.NewError("discounted_amount cannot be negative").
			WithHint("Please provide a valid discounted amount").
			Mark(ierr.ErrValidation)
	}

	if r.Currency == "" {
		return ierr.NewError("currency is required").
			WithHint("Please provide a currency code").
			Mark(ierr.ErrValidation)
	}

	return nil
}
