package coupon

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Coupon represents a discount coupon entity
type Coupon struct {
	ID                string                  `json:"id" db:"id"`
	Name              string                  `json:"name" db:"name"`
	RedeemAfter       *time.Time              `json:"redeem_after" db:"redeem_after"`
	RedeemBefore      *time.Time              `json:"redeem_before" db:"redeem_before"`
	MaxRedemptions    *int                    `json:"max_redemptions" db:"max_redemptions"`
	TotalRedemptions  int                     `json:"total_redemptions" db:"total_redemptions"`
	Rules             *map[string]interface{} `json:"rules" db:"rules"`
	AmountOff         *decimal.Decimal        `json:"amount_off" db:"amount_off" swaggertype:"string"`
	PercentageOff     *decimal.Decimal        `json:"percentage_off" db:"percentage_off" swaggertype:"string"`
	Type              types.CouponType        `json:"type" db:"type"`
	Cadence           types.CouponCadence     `json:"cadence" db:"cadence"`
	DurationInPeriods *int                    `json:"duration_in_periods" db:"duration_in_periods"`
	Currency          string                  `json:"currency" db:"currency"`
	Metadata          *map[string]string      `json:"metadata" db:"metadata"`
	EnvironmentID     string                  `json:"environment_id" db:"environment_id"`
	types.BaseModel
}

// IsValid checks if the coupon is valid for redemption
func (c *Coupon) IsValid() bool {
	now := time.Now()

	// Check if coupon is within valid date range
	if c.RedeemAfter != nil && now.Before(*c.RedeemAfter) {
		return false
	}

	if c.RedeemBefore != nil && now.After(*c.RedeemBefore) {
		return false
	}

	// Check if coupon has reached maximum redemptions
	if c.MaxRedemptions != nil && c.TotalRedemptions >= *c.MaxRedemptions {
		return false
	}

	// Check if coupon is expired
	if c.Status != types.StatusPublished {
		return false
	}

	return true
}

// DiscountResult holds the result of applying a discount
type DiscountResult struct {
	Discount   decimal.Decimal // The discount amount applied
	FinalPrice decimal.Decimal // The final price after discount
}

// ApplyDiscount calculates and applies the discount in a single operation,
// returning both the discount amount and final price. This is more efficient than
func (c *Coupon) ApplyDiscount(originalPrice decimal.Decimal) DiscountResult {
	var discount decimal.Decimal

	switch c.Type {
	case types.CouponTypeFixed:
		discount = *c.AmountOff
	case types.CouponTypePercentage:
		discount = originalPrice.Mul(*c.PercentageOff).Div(decimal.NewFromInt(100))
	default:
		discount = decimal.Zero
	}

	finalPrice := originalPrice.Sub(discount)

	// Ensure final price doesn't go below zero
	if finalPrice.LessThan(decimal.Zero) {
		// If discount exceeds price, adjust discount to match original price
		discount = originalPrice
		finalPrice = decimal.Zero
	}

	return DiscountResult{
		Discount:   discount,
		FinalPrice: finalPrice,
	}
}

func FromEnt(e *ent.Coupon) *Coupon {
	if e == nil {
		return nil
	}

	return &Coupon{
		ID:                e.ID,
		Name:              e.Name,
		RedeemAfter:       e.RedeemAfter,
		RedeemBefore:      e.RedeemBefore,
		MaxRedemptions:    e.MaxRedemptions,
		TotalRedemptions:  e.TotalRedemptions,
		Rules:             &e.Rules,
		AmountOff:         &e.AmountOff,
		PercentageOff:     &e.PercentageOff,
		Type:              types.CouponType(e.Type),
		Cadence:           types.CouponCadence(e.Cadence),
		DurationInPeriods: e.DurationInPeriods,
		Currency:          *e.Currency,
		EnvironmentID:     e.EnvironmentID,
		Metadata:          &e.Metadata,
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

// FromEntList converts a list of ent.Coupon to domain Coupons
func FromEntList(list []*ent.Coupon) []*Coupon {
	if list == nil {
		return nil
	}
	coupons := make([]*Coupon, len(list))
	for i, item := range list {
		coupons[i] = FromEnt(item)
	}
	return coupons
}
