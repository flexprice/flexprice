package invoice

import (
	"errors"
	"fmt"
)

var (
	// ErrInvoiceNotFound is returned when an invoice is not found
	ErrInvoiceNotFound = errors.New("invoice not found")

	// ErrInvoiceLineItemNotFound is returned when an invoice line item is not found
	ErrInvoiceLineItemNotFound = errors.New("invoice line item not found")

	// ErrInvalidInvoiceAmount is returned when invoice amounts are invalid
	ErrInvalidInvoiceAmount = errors.New("invalid invoice amount")

	// ErrInvalidInvoiceStatus is returned when invoice status transition is invalid
	ErrInvalidInvoiceStatus = errors.New("invalid invoice status")

	// ErrInvalidPaymentStatus is returned when payment status transition is invalid
	ErrInvalidPaymentStatus = errors.New("invalid payment status")

	// ErrInvoiceAlreadyPaid indicates that the invoice has already been paid
	ErrInvoiceAlreadyPaid = errors.New("invoice already paid")

	// ErrInvoiceAlreadyVoided indicates that the invoice has already been voided
	ErrInvoiceAlreadyVoided = errors.New("invoice already voided")

	// ErrInvoiceNotFinalized indicates that the invoice is not in finalized status
	ErrInvoiceNotFinalized = errors.New("invoice not finalized")
)

// ValidationError represents an error that occurs during invoice validation
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field %s: %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// IsValidationError checks if an error is a validation error
func IsValidationError(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

// IsNotFoundError checks if an error is a not found error
func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrInvoiceNotFound)
}

// IsInvoiceLineItemNotFoundError checks if an error is an invoice line item not found error
func IsInvoiceLineItemNotFoundError(err error) bool {
	return errors.Is(err, ErrInvoiceLineItemNotFound)
}

// VersionConflictError represents an error that occurs during optimistic locking
type VersionConflictError struct {
	ID            string
	CurrentVersion int
	ExpectedVersion int
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict for invoice %s: current version %d, expected version %d", e.ID, e.CurrentVersion, e.ExpectedVersion)
}

// NewVersionConflictError creates a new version conflict error
func NewVersionConflictError(id string, currentVersion, expectedVersion int) error {
	return &VersionConflictError{
		ID:            id,
		CurrentVersion: currentVersion,
		ExpectedVersion: expectedVersion,
	}
}

// IsVersionConflictError checks if an error is a version conflict error
func IsVersionConflictError(err error) bool {
	_, ok := err.(*VersionConflictError)
	return ok
}
