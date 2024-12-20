package errors

import (
	"errors"
	"fmt"
)

// Common error types that can be used across the application
var (
	ErrNotFound          = errors.New("resource not found")
	ErrAlreadyExists     = errors.New("resource already exists")
	ErrVersionConflict   = errors.New("version conflict")
	ErrValidation        = errors.New("validation error")
	ErrInvalidOperation  = errors.New("invalid operation")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrDependencyMissing = errors.New("dependency missing")
)

// Error represents a domain error
type Error struct {
	Code    string // Machine-readable error code
	Message string // Human-readable error message
	Op      string // Logical operation name
	Err     error  // Underlying error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Is implements error matching for wrapped errors
func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}

	t, ok := target.(*Error)
	if !ok {
		return errors.Is(e.Err, target)
	}

	return e.Code == t.Code
}

// New creates a new Error
func New(code string, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(err error, code string, message string) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// WithOp adds operation information to an error
func WithOp(err error, op string) *Error {
	if err == nil {
		return nil
	}

	e, ok := err.(*Error)
	if !ok {
		return &Error{
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

// IsDependencyMissing checks if an error is a dependency missing error
func IsDependencyMissing(err error) bool {
	return errors.Is(err, ErrDependencyMissing)
}
