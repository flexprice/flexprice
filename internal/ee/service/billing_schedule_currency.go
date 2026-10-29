package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func validateScheduleCurrency(ctx context.Context, customers customer.Repository, customerID, currency string) error {
	if customers == nil {
		return ierr.NewError("billing recipient is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	recipient, err := customers.Get(types.WithForceWriter(ctx), customerID)
	if err != nil {
		return err
	}
	if recipient == nil || recipient.ID != customerID || recipient.TenantID != types.GetTenantID(ctx) || recipient.EnvironmentID != types.GetEnvironmentID(ctx) {
		return ierr.NewError("billing recipient not found").Mark(ierr.ErrNotFound)
	}
	if recipient.BillingCurrency != nil && *recipient.BillingCurrency != "" && !types.IsMatchingCurrency(*recipient.BillingCurrency, currency) {
		return ierr.Validation("billing terms require the recipient billing currency to match the commitment currency")
	}
	return nil
}
