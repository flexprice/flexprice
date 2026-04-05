package errors

import (
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
)

// errorMapping holds both the HTTP status and machine-readable code for a
// sentinel error. Single source of truth — one iteration resolves both.
type errorMapping struct {
	Status int
	Code   string
}

// Common error types that can be used across the application
var (
	ErrNotFound           = new(ErrCodeNotFound, "resource not found")
	ErrAlreadyExists      = new(ErrCodeAlreadyExists, "resource already exists")
	ErrVersionConflict    = new(ErrCodeVersionConflict, "version conflict")
	ErrValidation         = new(ErrCodeValidation, "validation error")
	ErrInvalidOperation   = new(ErrCodeInvalidOperation, "invalid operation")
	ErrPermissionDenied   = new(ErrCodePermissionDenied, "permission denied")
	ErrHTTPClient         = new(ErrCodeHTTPClient, "http client error")
	ErrDatabase           = new(ErrCodeDatabase, "database error")
	ErrSystem             = new(ErrCodeSystemError, "system error")
	ErrInternal           = new(ErrCodeInternalError, "internal error")
	ErrServiceUnavailable = new(ErrCodeServiceUnavailable, "service unavailable")

	// errMappings is the single map that ties sentinel → (HTTP status, error code).
	// ResolveError iterates this once to get both values.
	errMappings = map[error]errorMapping{
		ErrNotFound:           {http.StatusNotFound, ErrCodeNotFound},
		ErrAlreadyExists:      {http.StatusConflict, ErrCodeAlreadyExists},
		ErrVersionConflict:    {http.StatusConflict, ErrCodeVersionConflict},
		ErrValidation:         {http.StatusBadRequest, ErrCodeValidation},
		ErrInvalidOperation:   {http.StatusBadRequest, ErrCodeInvalidOperation},
		ErrPermissionDenied:   {http.StatusForbidden, ErrCodePermissionDenied},
		ErrHTTPClient:         {http.StatusInternalServerError, ErrCodeHTTPClient},
		ErrDatabase:           {http.StatusInternalServerError, ErrCodeDatabase},
		ErrSystem:             {http.StatusInternalServerError, ErrCodeSystemError},
		ErrInternal:           {http.StatusInternalServerError, ErrCodeInternalError},
		ErrServiceUnavailable: {http.StatusServiceUnavailable, ErrCodeServiceUnavailable},
	}
)

const (
	ErrCodeHTTPClient       = "http_client_error"
	ErrCodeSystemError      = "system_error"
	ErrCodeInternalError    = "internal_error"
	ErrCodeNotFound         = "not_found"
	ErrCodeAlreadyExists    = "already_exists"
	ErrCodeVersionConflict  = "version_conflict"
	ErrCodeValidation       = "validation_error"
	ErrCodeInvalidOperation = "invalid_operation"
	ErrCodePermissionDenied = "permission_denied"
	ErrCodeDatabase         = "database_error"
	ErrCodeServiceUnavailable = "service_unavailable"
)

// InternalError represents a domain error
type InternalError struct {
	Code    string // Machine-readable error code
	Message string // Human-readable error message
	Op      string // Logical operation name
	Err     error  // Underlying error
}

func (e *InternalError) Error() string {
	if e.Err == nil {
		return e.DisplayError()
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Err.Error())
}

func (e *InternalError) DisplayError() string {
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
func new(code string, message string) *InternalError {
	return &InternalError{
		Code:    code,
		Message: message,
	}
}

func As(err error, target any) bool {
	return errors.As(err, target)
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func IsDatabase(err error) bool {
	return errors.Is(err, ErrDatabase)
}

func IsSystem(err error) bool {
	return errors.Is(err, ErrSystem)
}

func IsInternal(err error) bool {
	return errors.Is(err, ErrInternal)
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

// IsServiceUnavailable checks if an error is a service unavailable error
func IsServiceUnavailable(err error) bool {
	return errors.Is(err, ErrServiceUnavailable)
}

// ResolveError returns both the HTTP status code and machine-readable error
// code for the given error in a single pass over errMappings.
func ResolveError(err error) (httpStatus int, code string) {
	for sentinel, m := range errMappings {
		if errors.Is(err, sentinel) {
			return m.Status, m.Code
		}
	}
	return http.StatusInternalServerError, ErrCodeInternalError
}

