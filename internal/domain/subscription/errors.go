package subscription

import (
	"github.com/flexprice/flexprice/internal/errors"
)

// Error codes specific to subscription domain
const (
	ErrCodeNotFound        = "SUBSCRIPTION_NOT_FOUND"
	ErrCodeAlreadyExists   = "SUBSCRIPTION_ALREADY_EXISTS"
	ErrCodeVersionConflict = "SUBSCRIPTION_VERSION_CONFLICT"
	ErrCodeInvalidState    = "SUBSCRIPTION_INVALID_STATE"
)

// Common subscription errors
var (
	ErrNotFound        = errors.New(ErrCodeNotFound, "subscription not found")
	ErrAlreadyExists   = errors.New(ErrCodeAlreadyExists, "subscription already exists")
	ErrVersionConflict = errors.New(ErrCodeVersionConflict, "subscription version conflict")
	ErrInvalidState    = errors.New(ErrCodeInvalidState, "subscription is in invalid state for operation")
)

// NewNotFoundError creates a new not found error with additional context
func NewNotFoundError(id string) error {
	return errors.Wrap(ErrNotFound, ErrCodeNotFound,
		"subscription not found with id: "+id)
}

// NewAlreadyExistsError creates a new already exists error with additional context
func NewAlreadyExistsError(id string) error {
	return errors.Wrap(ErrAlreadyExists, ErrCodeAlreadyExists,
		"subscription already exists with id: "+id)
}

// NewVersionConflictError creates a new version conflict error with additional context
func NewVersionConflictError(id string, currentVersion, expectedVersion int) error {
	return errors.Wrap(ErrVersionConflict, ErrCodeVersionConflict,
		"subscription version conflict: expected %d but got %d for id: %s")
}

// NewInvalidStateError creates a new invalid state error with additional context
func NewInvalidStateError(id string, currentState, expectedState string) error {
	return errors.Wrap(ErrInvalidState, ErrCodeInvalidState,
		"subscription is in invalid state: expected %s but got %s for id: %s")
}
