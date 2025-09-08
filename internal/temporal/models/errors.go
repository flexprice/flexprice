package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Error types for Temporal operations using existing error patterns
var (
	ErrInvalidTenantContext = ierr.NewError("invalid tenant context").Mark(ierr.ErrValidation)
	ErrInvalidTaskQueue     = ierr.NewError("invalid task queue").Mark(ierr.ErrValidation)
	ErrInvalidWorkflow      = ierr.NewError("invalid workflow").Mark(ierr.ErrValidation)
	ErrInvalidActivity      = ierr.NewError("invalid activity").Mark(ierr.ErrValidation)
	ErrWorkerNotFound       = ierr.NewError("worker not found").Mark(ierr.ErrNotFound)
	ErrWorkerAlreadyExists  = ierr.NewError("worker already exists").Mark(ierr.ErrAlreadyExists)
	ErrClientNotInitialized = ierr.NewError("temporal client not initialized").Mark(ierr.ErrSystem)
)

// NewTemporalError creates a new temporal error using the existing error system
func NewTemporalError(message string) error {
	return ierr.NewError(message).Mark(ierr.ErrInternal)
}

// NewTemporalValidationError creates a new temporal validation error
func NewTemporalValidationError(message string) error {
	return ierr.NewError(message).Mark(ierr.ErrValidation)
}
