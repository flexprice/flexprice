package errors

import (
	"errors"
	"fmt"
)

// Common error types that can be used across the application
var (
	ErrNotFound         = New(ErrCodeNotFound, "resource not found")
	ErrAlreadyExists    = New(ErrCodeAlreadyExists, "resource already exists")
	ErrVersionConflict  = New(ErrCodeVersionConflict, "version conflict")
	ErrValidation       = New(ErrCodeValidation, "validation error")
	ErrInvalidOperation = New(ErrCodeInvalidOperation, "invalid operation")
	ErrPermissionDenied = New(ErrCodePermissionDenied, "permission denied")
	ErrHTTPClient       = New(ErrCodeHTTPClient, "http client error")
)

const (
	ErrCodeHTTPClient       = "http_client_error"
	ErrCodeNotFound         = "not_found"
	ErrCodeAlreadyExists    = "already_exists"
	ErrCodeVersionConflict  = "version_conflict"
	ErrCodeValidation       = "validation_error"
	ErrCodeInvalidOperation = "invalid_operation"
	ErrCodePermissionDenied = "permission_denied"
)

// InternalError represents a domain error
type InternalError struct {
	Code    string // Machine-readable error code
	Message string // Human-readable error message
	Op      string // Logical operation name
	Err     error  // Underlying error
}

func (e *InternalError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *InternalError) Unwrap() error {
	return e.Err
}

// Is implements error matching for wrapped errors
func (e *InternalError) Is(target error) bool {
	if target == nil {
		return false
	}

	t, ok := target.(*InternalError)
	if !ok {
		return errors.Is(e.Err, target)
	}

	return e.Code == t.Code
}

// New creates a new InternalError
func New(code string, message string) *InternalError {
	return &InternalError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(err error, code string, message string) *InternalError {
	if err == nil {
		return nil
	}
	return &InternalError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// WithOp adds operation information to an error
func WithOp(err error, op string) *InternalError {
	if err == nil {
		return nil
	}

	e, ok := err.(*InternalError)
	if !ok {
		return &InternalError{
			Message: err.Error(),
			Op:      op,
			Err:     err,
		}
	}

	e.Op = op
	return e
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsAlreadyExists checks if an error is an already exists error
func IsAlreadyExists(err error) bool {
	return errors.Is(err, ErrAlreadyExists)
}

// IsVersionConflict checks if an error is a version conflict error
func IsVersionConflict(err error) bool {
	return errors.Is(err, ErrVersionConflict)
}

// IsValidation checks if an error is a validation error
func IsValidation(err error) bool {
	return errors.Is(err, ErrValidation)
}

// IsInvalidOperation checks if an error is an invalid operation error
func IsInvalidOperation(err error) bool {
	return errors.Is(err, ErrInvalidOperation)
}

// IsPermissionDenied checks if an error is a permission denied error
func IsPermissionDenied(err error) bool {
	return errors.Is(err, ErrPermissionDenied)
}

// IsHTTPClient checks if an error is an http client error
func IsHTTPClient(err error) bool {
	return errors.Is(err, ErrHTTPClient)
}
