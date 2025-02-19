package httpclient

import (
	goerrors "errors"
	"fmt"
)

// Error represents an HTTP client error
type Error struct {
	StatusCode int
	Response   []byte
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d - %v", e.StatusCode, e.Response)
}

// NewError creates a new HTTP client error
func NewError(statusCode int, response []byte) *Error {
	return &Error{
		StatusCode: statusCode,
		Response:   response,
	}
}

// IsHTTPError checks if an error is an HTTP client error
func IsHTTPError(err error) (*Error, bool) {
	var httpErr *Error
	if goerrors.As(err, &httpErr) {
		return httpErr, true
	}
	return nil, false
}
