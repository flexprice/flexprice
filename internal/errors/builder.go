package errors

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// ErrorBuilder provides a fluent interface for building errors
// but does not implement the error interface. This is intentional.
// Mark must be the last call in the chain when using the builder.
type ErrorBuilder struct {
	err error
}

// NewError starts a new error builder chain
func NewError(msg string) *ErrorBuilder {
	return &ErrorBuilder{err: errors.New(msg)}
}

func NewErrorf(format string, args ...any) *ErrorBuilder {
	return &ErrorBuilder{err: errors.Newf(format, args...)}
}

// WithError starts a builder chain with an existing error
func WithError(err error) *ErrorBuilder {
	return &ErrorBuilder{err: err}
}

// WithMessage adds context to the error
// this is for the internal error messages
func (b *ErrorBuilder) WithMessage(msg string) *ErrorBuilder {
	b.err = errors.WithMessage(b.err, msg)
	return b
}

func (b *ErrorBuilder) WithMessagef(format string, args ...any) *ErrorBuilder {
	b.err = errors.WithMessagef(b.err, format, args...)
	return b
}

// WithHint adds context to the error
// this is for the frontend error messages
func (b *ErrorBuilder) WithHint(hint string) *ErrorBuilder {
	b.err = errors.WithHint(b.err, hint)
	return b
}

// WithHintf is a helper for WithHint that allows for formatting
func (b *ErrorBuilder) WithHintf(format string, args ...any) *ErrorBuilder {
	b.err = errors.WithHintf(b.err, format, args...)
	return b
}

// WithReportableDetails adds structured details
func (b *ErrorBuilder) WithReportableDetails(details map[string]any) *ErrorBuilder {
	if len(details) == 0 {
		return b
	}
	marshaled, err := json.Marshal(details)
	if err != nil {
		return b
	}
	b.err = errors.WithSafeDetails(b.err, "__json__:%s", errors.Safe(string(marshaled)))
	return b
}

// Mark marks the error with a sentinel error
// should be the last call in the chain
func (b *ErrorBuilder) Mark(reference error) error {
	b.err = errors.Mark(b.err, reference)
	return b.err
}

// Err returns the error string
func (b *ErrorBuilder) Error() error {
	return b.err
}
