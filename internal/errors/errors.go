package errors

import (
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
)

// Common error types that can be used across the application
// TODO: move to errors.New from cockroachdb/errors
var (
	ErrNotFound         = new(ErrCodeNotFound, "resource not found")
	ErrAlreadyExists    = new(ErrCodeAlreadyExists, "resource already exists")
	ErrVersionConflict  = new(ErrCodeVersionConflict, "version conflict")
	ErrValidation       = new(ErrCodeValidation, "validation error")
	ErrInvalidOperation = new(ErrCodeInvalidOperation, "invalid operation")
	ErrPermissionDenied = new(ErrCodePermissionDenied, "permission denied")
	ErrHTTPClient       = new(ErrCodeHTTPClient, "http client error")
	ErrDatabase         = new(ErrCodeDatabase, "database error")
	ErrSystem           = new(ErrCodeSystemError, "system error")
	ErrInternal         = new(ErrCodeInternalError, "internal error")
	// maps errors to http status codes
	statusCodeMap = map[error]int{
		ErrHTTPClient:       http.StatusInternalServerError,
		ErrDatabase:         http.StatusInternalServerError,
		ErrNotFound:         http.StatusNotFound,
		ErrAlreadyExists:    http.StatusConflict,
		ErrVersionConflict:  http.StatusConflict,
		ErrValidation:       http.StatusBadRequest,
		ErrInvalidOperation: http.StatusBadRequest,
		ErrPermissionDenied: http.StatusForbidden,
		ErrSystem:           http.StatusInternalServerError,
		ErrInternal:         http.StatusInternalServerError,
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

func HTTPStatusFromErr(err error) int {
	for e, status := range statusCodeMap {
		if errors.Is(err, e) {
			return status
		}
	}
	return http.StatusInternalServerError
}
