package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// CouponValidationErrorCode represents the type of coupon validation error
type CouponValidationErrorCode string

const (
	// Basic validation errors
	CouponValidationErrorCodeNotFound     CouponValidationErrorCode = "COUPON_NOT_FOUND"
	CouponValidationErrorCodeNotPublished CouponValidationErrorCode = "COUPON_NOT_PUBLISHED"

	// Date range validation errors
	CouponValidationErrorCodeNotActive CouponValidationErrorCode = "COUPON_NOT_ACTIVE"
	CouponValidationErrorCodeExpired   CouponValidationErrorCode = "COUPON_EXPIRED"

	// Environment and context validation errors
	CouponValidationErrorCodeEnvironmentMismatch CouponValidationErrorCode = "ENVIRONMENT_MISMATCH"
	CouponValidationErrorCodeCurrencyMismatch    CouponValidationErrorCode = "CURRENCY_MISMATCH"

	// Redemption validation errors
	CouponValidationErrorCodeRedemptionLimitReached CouponValidationErrorCode = "REDEMPTION_LIMIT_REACHED"

	// Subscription validation errors
	CouponValidationErrorCodeInvalidSubscriptionStatus CouponValidationErrorCode = "INVALID_SUBSCRIPTION_STATUS"

	// Cadence validation errors
	CouponValidationErrorCodeInvalidCadence              CouponValidationErrorCode = "INVALID_CADENCE"
	CouponValidationErrorCodeOnceCadenceViolation        CouponValidationErrorCode = "ONCE_CADENCE_VIOLATION"
	CouponValidationErrorCodeRepeatedCadenceLimitReached CouponValidationErrorCode = "REPEATED_CADENCE_LIMIT_REACHED"
	CouponValidationErrorCodeInvalidRepeatedCadence      CouponValidationErrorCode = "INVALID_REPEATED_CADENCE"

	// Database and system errors
	CouponValidationErrorCodeDatabaseError CouponValidationErrorCode = "DATABASE_ERROR"
)

func (c CouponValidationErrorCode) String() string {
	return string(c)
}

func (c CouponValidationErrorCode) Validate() error {
	allowed := []CouponValidationErrorCode{
		CouponValidationErrorCodeNotFound,
		CouponValidationErrorCodeNotPublished,
		CouponValidationErrorCodeNotActive,
		CouponValidationErrorCodeExpired,
		CouponValidationErrorCodeEnvironmentMismatch,
		CouponValidationErrorCodeCurrencyMismatch,
		CouponValidationErrorCodeRedemptionLimitReached,
		CouponValidationErrorCodeInvalidSubscriptionStatus,
		CouponValidationErrorCodeInvalidCadence,
		CouponValidationErrorCodeOnceCadenceViolation,
		CouponValidationErrorCodeRepeatedCadenceLimitReached,
		CouponValidationErrorCodeInvalidRepeatedCadence,
		CouponValidationErrorCodeDatabaseError,
	}

	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid coupon validation error code").
			WithHint("Please provide a valid coupon validation error code").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
				"code":    c,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// IsUserError returns true if the error code represents a user error (not a system error)
func (c CouponValidationErrorCode) IsUserError() bool {
	systemErrors := []CouponValidationErrorCode{
		CouponValidationErrorCodeDatabaseError,
		CouponValidationErrorCodeEnvironmentMismatch,
	}
	return !lo.Contains(systemErrors, c)
}

// IsCadenceError returns true if the error code is related to cadence validation
func (c CouponValidationErrorCode) IsCadenceError() bool {
	cadenceErrors := []CouponValidationErrorCode{
		CouponValidationErrorCodeInvalidCadence,
		CouponValidationErrorCodeOnceCadenceViolation,
		CouponValidationErrorCodeRepeatedCadenceLimitReached,
		CouponValidationErrorCodeInvalidRepeatedCadence,
	}
	return lo.Contains(cadenceErrors, c)
}

// IsRedemptionError returns true if the error code is related to redemption limits
func (c CouponValidationErrorCode) IsRedemptionError() bool {
	return c == CouponValidationErrorCodeRedemptionLimitReached
}
