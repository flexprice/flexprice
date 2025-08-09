package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CouponValidationError represents validation errors with structured details
type CouponValidationError struct {
	Code    types.CouponValidationErrorCode `json:"code"`
	Message string                          `json:"message"`
	Details map[string]interface{}          `json:"details,omitempty"`
}

func (e *CouponValidationError) Error() string {
	return e.Message
}

// CouponValidationService defines the interface for coupon validation operations
type CouponValidationService interface {
	// Core validation method used for both subscription and invoice scenarios
	ValidateCoupon(ctx context.Context, couponID string, subscriptionID *string) error
	// Basic coupon validation (status, validity, etc.)
	ValidateCouponBasic(coupon *coupon.Coupon) error
}

// couponValidationService implements CouponValidationService
type couponValidationService struct {
	ServiceParams
}

// NewCouponValidationService creates a new coupon validation service
func NewCouponValidationService(params ServiceParams) CouponValidationService {
	return &couponValidationService{
		ServiceParams: params,
	}
}

// ValidateCouponForSubscription validates a coupon before associating it with a subscription
func (s *couponValidationService) ValidateCoupon(ctx context.Context, couponID string, subscriptionID *string) error {
	s.Logger.Infow("validating coupon for subscription association",
		"coupon_id", couponID,
		"subscription_id", subscriptionID)

	// Get coupon details
	coupon, err := s.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get coupon details").
			Mark(ierr.ErrNotFound)
	}

	// subscription is nil by default if subscriptionID is nil
	var subscription *subscription.Subscription
	if subscriptionID != nil {
		sub, err := s.SubRepo.Get(ctx, *subscriptionID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to get subscription details").
				Mark(ierr.ErrNotFound)
		}
		subscription = sub
	}

	// Priority 1: Basic coupon validation
	if err := s.ValidateCouponBasic(coupon); err != nil {
		return err
	}

	// Priority 2: Business rule validation
	if err := s.validateCouponBusinessRules(coupon, subscription); err != nil {
		return err
	}

	// Priority 3: Redemption validation
	if err := s.validateCouponRedemption(coupon); err != nil {
		return err
	}

	// Priority 4: Subscription-specific validation
	if subscription != nil {
		if err := s.validateCouponCadence(ctx, coupon, subscription); err != nil {
			return err
		}
	}

	s.Logger.Infow("coupon validation for subscription successful",
		"coupon_id", couponID,
		"subscription_id", subscriptionID)

	return nil
}

// ValidateCouponBasic performs basic coupon validation (public method)
func (s *couponValidationService) ValidateCouponBasic(coupon *coupon.Coupon) error {
	// Check if coupon exists
	if coupon == nil {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeNotFound,
			Message: "Coupon not found",
		}
	}

	// Check if coupon is published
	if coupon.Status != types.StatusPublished {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeNotPublished,
			Message: "Coupon is not published",
			Details: map[string]interface{}{
				"coupon_id": coupon.ID,
				"status":    coupon.Status,
			},
		}
	}

	return nil
}

// Priority 2: Business rule validation for subscription
func (s *couponValidationService) validateCouponBusinessRules(coupon *coupon.Coupon, subscription *subscription.Subscription) error {
	// Date range validation
	if err := s.validateCouponDateRange(coupon); err != nil {
		return err
	}

	// Currency validation (if subscription exists and has currency)
	if subscription != nil && subscription.Currency != "" {
		if err := s.validateCouponCurrency(coupon, subscription.Currency); err != nil {
			return err
		}
	}

	return nil
}

// Date range validation
func (s *couponValidationService) validateCouponDateRange(coupon *coupon.Coupon) error {
	now := time.Now()

	// Check redeem_after date
	if coupon.RedeemAfter != nil {
		if now.Before(*coupon.RedeemAfter) {
			return &CouponValidationError{
				Code:    types.CouponValidationErrorCodeNotActive,
				Message: "Coupon is not yet active",
				Details: map[string]interface{}{
					"coupon_id":    coupon.ID,
					"redeem_after": coupon.RedeemAfter,
					"current_time": now,
				},
			}
		}
	}

	// Check redeem_before date
	if coupon.RedeemBefore != nil {
		if now.After(*coupon.RedeemBefore) {
			return &CouponValidationError{
				Code:    types.CouponValidationErrorCodeExpired,
				Message: "Coupon has expired",
				Details: map[string]interface{}{
					"coupon_id":     coupon.ID,
					"redeem_before": coupon.RedeemBefore,
					"current_time":  now,
				},
			}
		}
	}

	return nil
}

// Currency validation
func (s *couponValidationService) validateCouponCurrency(coupon *coupon.Coupon, targetCurrency string) error {
	// If coupon has specific currency, it must match target currency
	if coupon.Currency != "" {
		if coupon.Currency != targetCurrency {
			return &CouponValidationError{
				Code:    types.CouponValidationErrorCodeCurrencyMismatch,
				Message: "Coupon currency does not match target currency",
				Details: map[string]interface{}{
					"coupon_id":       coupon.ID,
					"coupon_currency": coupon.Currency,
					"target_currency": targetCurrency,
				},
			}
		}
	}

	return nil
}

// Priority 5: Redemption validation
func (s *couponValidationService) validateCouponRedemption(coupon *coupon.Coupon) error {
	// Check if coupon has reached max redemptions
	if coupon.MaxRedemptions != nil {
		if coupon.TotalRedemptions >= *coupon.MaxRedemptions {
			return &CouponValidationError{
				Code:    types.CouponValidationErrorCodeRedemptionLimitReached,
				Message: "Coupon has reached maximum redemptions",
				Details: map[string]interface{}{
					"coupon_id":         coupon.ID,
					"max_redemptions":   *coupon.MaxRedemptions,
					"total_redemptions": coupon.TotalRedemptions,
				},
			}
		}
	}

	return nil
}

// validateCouponForInvoiceSpecific implements cadence-specific validation for invoice application
func (s *couponValidationService) validateCouponCadence(ctx context.Context, coupon *coupon.Coupon, subscription *subscription.Subscription) error {
	s.Logger.Debugw("validating coupon cadence for invoice",
		"coupon_id", coupon.ID,
		"cadence", coupon.Cadence)

	// Validate cadence-specific rules
	switch coupon.Cadence {
	case types.CouponCadenceOnce:
		return s.validateOnceCadence(ctx, coupon, subscription)
	case types.CouponCadenceForever:
		return s.validateForeverCadence(coupon, subscription)
	case types.CouponCadenceRepeated:
		return s.validateRepeatedCadence(ctx, coupon, subscription)
	default:
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeInvalidCadence,
			Message: "Invalid coupon cadence",
			Details: map[string]interface{}{
				"coupon_id": coupon.ID,
				"cadence":   coupon.Cadence,
			},
		}
	}
}

// validateOnceCadenceForInvoice validates "once" cadence - coupon should only be applied to first invoice
func (s *couponValidationService) validateOnceCadence(ctx context.Context, coupon *coupon.Coupon, subscription *subscription.Subscription) error {
	s.Logger.Debugw("validating once cadence for invoice",
		"coupon_id", coupon.ID,
		"subscription_id", subscription.ID)

	// Use optimized query to check if this coupon has already been applied to this subscription
	// This is much more efficient than fetching all invoices and counting them
	existingApplicationCount, err := s.CouponApplicationRepo.CountBySubscriptionAndCoupon(ctx, subscription.ID, coupon.ID)
	if err != nil {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeDatabaseError,
			Message: "Failed to count existing applications for once cadence validation",
			Details: map[string]interface{}{
				"subscription_id": subscription.ID,
				"coupon_id":       coupon.ID,
				"error":           err.Error(),
			},
		}
	}

	s.Logger.Debugw("existing applications count for once cadence validation",
		"coupon_id", coupon.ID,
		"subscription_id", subscription.ID,
		"existing_applications", existingApplicationCount)

	// For "once" cadence, this coupon should not have been applied before in this subscription
	if existingApplicationCount > 1 {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeOnceCadenceViolation,
			Message: "Once cadence coupon can only be applied once per subscription",
			Details: map[string]interface{}{
				"coupon_id":             coupon.ID,
				"subscription_id":       subscription.ID,
				"existing_applications": existingApplicationCount,
			},
		}
	}

	s.Logger.Debugw("once cadence validation passed - no previous applications found",
		"coupon_id", coupon.ID,
		"subscription_id", subscription.ID)

	return nil
}

// validateForeverCadence validates "forever" cadence - coupon is always applied
func (s *couponValidationService) validateForeverCadence(coupon *coupon.Coupon, subscription *subscription.Subscription) error {
	s.Logger.Debugw("validating forever cadence for invoice",
		"coupon_id", coupon.ID,
		"subscription_id", subscription.ID)
	// Forever cadence coupons are always valid for application
	// Even if the coupon has expired, it should still be applied to invoices
	// where it was already associated with the subscription
	// This follows the requirement: "coupon expires, it will still be applied on all future invoices"

	return nil
}

// validateRepeatedCadenceForInvoice validates "repeated" cadence - coupon applied for duration_in_periods times
func (s *couponValidationService) validateRepeatedCadence(ctx context.Context, coupon *coupon.Coupon, subscription *subscription.Subscription) error {
	s.Logger.Debugw("validating repeated cadence for invoice",
		"coupon_id", coupon.ID,
		"duration_in_periods", coupon.DurationInPeriods)

	// Check if duration_in_periods is set for repeated cadence
	if coupon.DurationInPeriods == nil || *coupon.DurationInPeriods <= 0 {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeInvalidRepeatedCadence,
			Message: "Repeated cadence requires valid duration_in_periods",
			Details: map[string]interface{}{
				"coupon_id":           coupon.ID,
				"duration_in_periods": coupon.DurationInPeriods,
			},
		}
	}

	// Use optimized query to count existing applications for this coupon and subscription
	// This is much more efficient than the previous approach of getting all invoices and their applications
	existingApplicationCount, err := s.CouponApplicationRepo.CountBySubscriptionAndCoupon(ctx, subscription.ID, coupon.ID)
	if err != nil {
		s.Logger.Warnw("failed to count existing applications for repeated cadence validation",
			"coupon_id", coupon.ID,
			"subscription_id", subscription.ID,
			"error", err)
		// Don't fail validation if we can't count applications - allow the application to proceed
		return nil
	}

	s.Logger.Debugw("existing applications count for repeated cadence validation",
		"coupon_id", coupon.ID,
		"subscription_id", subscription.ID,
		"existing_applications", existingApplicationCount)

	// For repeated cadence, check if we've exceeded the duration_in_periods limit
	if existingApplicationCount >= *coupon.DurationInPeriods {
		return &CouponValidationError{
			Code:    types.CouponValidationErrorCodeRepeatedCadenceLimitReached,
			Message: "Repeated cadence coupon has reached its application limit",
			Details: map[string]interface{}{
				"coupon_id":             coupon.ID,
				"subscription_id":       subscription.ID,
				"duration_in_periods":   *coupon.DurationInPeriods,
				"existing_applications": existingApplicationCount,
			},
		}
	}

	s.Logger.Debugw("repeated cadence validation passed",
		"coupon_id", coupon.ID,
		"existing_applications", existingApplicationCount,
		"duration_in_periods", *coupon.DurationInPeriods)

	return nil
}
