package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/samber/lo"
)

// fillBillingEntityIDs sets each item's billing entity from its customer, with one lookup per page.
func fillBillingEntityIDs[T any](ctx context.Context, params ServiceParams, items []T, customerID func(T) string, set func(T, *string)) error {
	if len(items) == 0 {
		return nil
	}
	ids := lo.Compact(lo.Map(items, func(item T, _ int) string { return customerID(item) }))
	byCustomer, err := params.CustomerRepo.BillingEntityIDsByCustomerIDs(ctx, lo.Uniq(ids))
	if err != nil {
		return err
	}
	for _, item := range items {
		set(item, byCustomer[customerID(item)])
	}
	return nil
}

func fillInvoiceBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.InvoiceResponse) error {
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.InvoiceResponse) string { return r.CustomerID },
		func(r *dto.InvoiceResponse, id *string) { r.BillingEntityID = id })
}
