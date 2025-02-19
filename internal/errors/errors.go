package errors

import (
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
)

// Error codes
const (
	ErrCodeNotFound         = "not_found"
	ErrCodeAlreadyExists    = "already_exists"
	ErrCodeVersionConflict  = "version_conflict"
	ErrCodeValidation       = "validation_error"
	ErrCodeInvalidOperation = "invalid_operation"
	ErrCodePermissionDenied = "permission_denied"
	ErrCodeHTTPClient       = "http_client_error"
	ErrCodeSystem           = "system_error"
	ErrCodeInvalidState     = "invalid_state"
	ErrCodeTimeout          = "timeout"
	ErrCodeDatabase         = "database_error"
	ErrCodeConfiguration    = "configuration_error"
	ErrCodeRateLimit        = "rate_limit_exceeded"
)

// Sentinel errors for marking
var (
	ErrNotFound         = errors.New(ErrCodeNotFound)
	ErrAlreadyExists    = errors.New(ErrCodeAlreadyExists)
	ErrVersionConflict  = errors.New(ErrCodeVersionConflict)
	ErrValidation       = errors.New(ErrCodeValidation)
	ErrInvalidOperation = errors.New(ErrCodeInvalidOperation)
	ErrPermissionDenied = errors.New(ErrCodePermissionDenied)
	ErrHTTPClient       = errors.New(ErrCodeHTTPClient)
	ErrSystem           = errors.New(ErrCodeSystem)
	ErrInvalidState     = errors.New(ErrCodeInvalidState)
	BaseErrors          = []error{
		ErrNotFound,
		ErrAlreadyExists,
		ErrVersionConflict,
		ErrValidation,
		ErrInvalidOperation,
		ErrPermissionDenied,
		ErrHTTPClient,
		ErrSystem,
		ErrInvalidState,
	}
)

// statusCodeMap maps errors to HTTP status codes
var statusCodeMap = map[error]int{
	ErrNotFound:         http.StatusNotFound,
	ErrAlreadyExists:    http.StatusConflict,
	ErrVersionConflict:  http.StatusConflict,
	ErrValidation:       http.StatusBadRequest,
	ErrInvalidOperation: http.StatusInternalServerError,
	ErrPermissionDenied: http.StatusForbidden,
	ErrHTTPClient:       http.StatusBadGateway,
	ErrSystem:           http.StatusInternalServerError,
	ErrInvalidState:     http.StatusUnprocessableEntity,
}

// InternalError represents a domain error with operation context
type InternalError struct {
	msg   string // Human-readable error message
	op    string // Logical operation name
	cause error  // Underlying error
}

func (e *InternalError) Error() string {
	if e.op != "" {
		if e.cause != nil {
			return fmt.Sprintf("%s: %s: %v", e.op, e.msg, e.cause)
		}
		return fmt.Sprintf("%s: %s", e.op, e.msg)
	}
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.msg, e.cause)
	}
	return e.msg
}

func (e *InternalError) Unwrap() error {
	return e.cause
}

// Op returns the operation name
func (e *InternalError) Op() string {
	return e.op
}

// Mark returns the error marker from the error chain using cockroachdb/errors
func (e *InternalError) Mark(sentinel error) error {
	return errors.Mark(e, sentinel)
}

// New creates a new error with a type marker
func New(sentinel error, msg string) error {
	return errors.Mark(&InternalError{msg: msg}, sentinel)
}

// New creates a new error with a type marker
func Newf(sentinel error, format string, args ...any) error {
	return errors.Mark(&InternalError{msg: fmt.Sprintf(format, args...)}, sentinel)
}

// Wrap wraps an error with additional context and default marker
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}

	// If it's already an InternalError, mark will be preserved
	var ierr *InternalError
	if errors.As(err, &ierr) {
		return errors.Wrap(err, msg)
	}

	return errors.Mark(&InternalError{msg: msg, cause: err}, ErrSystem)
}

// WrapAs wraps an error with a message and marks it with the given sentinel error
func WrapAs(err error, sentinel error, msg string) error {
	if err == nil {
		return nil
	}
	return errors.Mark(
		&InternalError{msg: msg, cause: err},
		sentinel,
	)
}

// WithOp wraps an error with operation context
func WithOp(err error, op string) *InternalError {
	if err == nil {
		return nil
	}

	// If it's already an InternalError, just update the op
	var ierr *InternalError
	if errors.As(err, &ierr) {
		newErr := *ierr // Make a copy
		newErr.op = op
		return &newErr
	}

	return &InternalError{
		msg:   err.Error(),
		op:    op,
		cause: err,
	}
}

func NewInternal(msg string) *InternalError {
	return &InternalError{msg: msg}
}

// HttpStatusFromErr returns the appropriate HTTP status code for an error
func HttpStatusFromErr(err error) int {
	for sentinel, code := range statusCodeMap {
		if errors.Is(err, sentinel) {
			return code
		}
	}
	return http.StatusInternalServerError
}

// Error type checks
func IsNotFound(err error) bool         { return errors.Is(err, ErrNotFound) }
func IsAlreadyExists(err error) bool    { return errors.Is(err, ErrAlreadyExists) }
func IsVersionConflict(err error) bool  { return errors.Is(err, ErrVersionConflict) }
func IsValidation(err error) bool       { return errors.Is(err, ErrValidation) }
func IsInvalidOperation(err error) bool { return errors.Is(err, ErrInvalidOperation) }
func IsPermissionDenied(err error) bool { return errors.Is(err, ErrPermissionDenied) }
func IsHTTPClient(err error) bool       { return errors.Is(err, ErrHTTPClient) }
