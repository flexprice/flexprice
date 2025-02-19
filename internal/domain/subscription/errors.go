package subscription

import (
	"github.com/flexprice/flexprice/internal/errors"
)

// NewNotFoundError creates a new not found error with additional context
func NewNotFoundError(id string) error {
	return errors.Newf(errors.ErrNotFound, "subscription not found with id: %v", id)
}

// NewAlreadyExistsError creates a new already exists error with additional context
func NewAlreadyExistsError(id string) error {
	return errors.Newf(errors.ErrAlreadyExists, "subscription already exists with id: %v", id)
}

// NewVersionConflictError creates a new version conflict error with additional context
func NewVersionConflictError(id string, currentVersion, expectedVersion int) error {
	return errors.Newf(
		errors.ErrVersionConflict,
		"subscription version conflict: expected %d but got %d for id: %s",
		expectedVersion, currentVersion, id,
	)
}

// NewInvalidStateError creates a new invalid state error with additional context
func NewInvalidStateError(id string, currentState, expectedState string) error {
	return errors.Newf(
		errors.ErrInvalidState,
		"subscription is in invalid state: expected %s but got %s for id: %s",
		expectedState, currentState, id,
	)
}
