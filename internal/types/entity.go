package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type EntityType string

const (
	EntityTypeEvents    EntityType = "EVENTS"
	EntityTypePrices    EntityType = "PRICES"
	EntityTypeCustomers EntityType = "CUSTOMERS"
	EntityTypeInvoices  EntityType = "INVOICES"
	EntityTypePayments  EntityType = "PAYMENTS"
)

func (e EntityType) String() string {
	return string(e)
}

func (e EntityType) Validate() error {
	allowed := []EntityType{
		EntityTypeEvents,
		EntityTypePrices,
		EntityTypeCustomers,
		EntityTypeInvoices,
		EntityTypePayments,
	}
	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid entity type").
			WithHint("Invalid entity type").
			Mark(ierr.ErrValidation)
	}
	return nil
}
