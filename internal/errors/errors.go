package errors

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

type ErrCode string

const (
	ErrCodeHTTPClient       ErrCode = "http_client_error"
	ErrCodeSystemError      ErrCode = "system_error"
	ErrCodeNotFound         ErrCode = "not_found"
	ErrCodeAlreadyExists    ErrCode = "already_exists"
	ErrCodeVersionConflict  ErrCode = "version_conflict"
	ErrCodeValidation       ErrCode = "validation_error"
	ErrCodeInvalidOperation ErrCode = "invalid_operation"
	ErrCodePermissionDenied ErrCode = "permission_denied"
)

var ErrCodeMap = map[ErrCode]string{
	ErrCodeHTTPClient:       "http_client_error",
	ErrCodeSystemError:      "system_error",
	ErrCodeNotFound:         "not_found",
	ErrCodeAlreadyExists:    "already_exists",
	ErrCodeVersionConflict:  "version_conflict",
	ErrCodeValidation:       "validation_error",
	ErrCodeInvalidOperation: "invalid_operation",
	ErrCodePermissionDenied: "permission_denied",
}

func (e ErrCode) String() string {
	d, ok := ErrCodeMap[e]
	if !ok {
		return ""
	}
	return d
}

func (e ErrCode) IsEmpty() bool {
	_, ok := ErrCodeMap[e]
	return !ok
}

// InternalError represents a domain error
type InternalError struct {
	Code       ErrCode // Machine-readable error code
	Message    string  // Human-readable error message
	Op         string  // Logical operation name
	Err        error   // Underlying error
	LogMessage string  // Internal logging message
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

func ErrorCode(err error) ErrCode {
	if err == nil {
		return ""
	}
	if e, ok := err.(*InternalError); ok && !e.Code.IsEmpty() {
		return e.Code
	} else if ok && e.Err != nil {
		return ErrorCode(e.Err)
	}
	return ErrCodeSystemError
}

func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := err.(*InternalError); ok && e.Message != "" {
		return e.Message
	} else if ok && e.Err != nil {
		return ErrorMessage(e.Err)
	}
	return "an internal error has occured"
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
func New(code ErrCode, message string) *InternalError {
	return &InternalError{
		Code:    code,
		Message: message,
	}
}

func NewWithTrace(ctx context.Context, code ErrCode, message string) *InternalError {
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Error, message)
	span.SetAttributes(
		attribute.String("error.code", string(code)),
	)
	return &InternalError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(err error, code ErrCode, message string) *InternalError {
	if err == nil {
		return nil
	}
	return &InternalError{
		Code:    ErrCode(code),
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

// GetHTTPStatusCode returns the HTTP status code for the given error code.
func GetHTTPStatusCode(errCode ErrCode) int {
	switch errCode {
	case ErrCodeAlreadyExists:
		return http.StatusConflict
	case ErrCodeHTTPClient:
		return http.StatusInternalServerError
	case ErrCodeInvalidOperation:
		return http.StatusBadRequest
	case ErrCodeNotFound:
		return http.StatusNotFound
	case ErrCodePermissionDenied:
		return http.StatusForbidden
	case ErrCodeSystemError:
		return http.StatusInternalServerError
	case ErrCodeValidation:
		return http.StatusBadRequest
	case ErrCodeVersionConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
