package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
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

// invoiceCustomers maps invoice IDs to their customers in one lookup.
func invoiceCustomers(ctx context.Context, params ServiceParams, invoiceIDs []string) (map[string]string, error) {
	return params.InvoiceRepo.CustomerIDsByInvoiceIDs(ctx, lo.Uniq(lo.Compact(invoiceIDs)))
}

func fillInvoiceBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.InvoiceResponse) error {
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.InvoiceResponse) string { return r.CustomerID },
		func(r *dto.InvoiceResponse, id *string) { r.BillingEntityID = id })
}

func fillSubscriptionBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.SubscriptionResponse) error {
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.SubscriptionResponse) string { return r.CustomerID },
		func(r *dto.SubscriptionResponse, id *string) { r.BillingEntityID = id })
}

func fillWalletBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.WalletResponse) error {
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.WalletResponse) string { return r.CustomerID },
		func(r *dto.WalletResponse, id *string) { r.BillingEntityID = id })
}

func fillCreditNoteBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.CreditNoteResponse) error {
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.CreditNoteResponse) string { return r.CustomerID },
		func(r *dto.CreditNoteResponse, id *string) { r.BillingEntityID = id })
}

// fillPaymentBillingEntityIDs resolves an invoice payment through its invoice and a customer payment directly.
func fillPaymentBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.PaymentResponse) error {
	invoiceIDs := lo.FilterMap(items, func(p *dto.PaymentResponse, _ int) (string, bool) {
		return p.DestinationID, p.DestinationType == types.PaymentDestinationTypeInvoice
	})
	customerByInvoice, err := invoiceCustomers(ctx, params, invoiceIDs)
	if err != nil {
		return err
	}
	return fillBillingEntityIDs(ctx, params, items,
		func(p *dto.PaymentResponse) string {
			if p.DestinationType == types.PaymentDestinationTypeInvoice {
				return customerByInvoice[p.DestinationID]
			}
			return p.DestinationID
		},
		func(p *dto.PaymentResponse, id *string) { p.BillingEntityID = id })
}

func fillRefundBillingEntityIDs(ctx context.Context, params ServiceParams, items ...*dto.RefundResponse) error {
	customerByInvoice, err := invoiceCustomers(ctx, params, lo.Map(items, func(r *dto.RefundResponse, _ int) string { return r.InvoiceID }))
	if err != nil {
		return err
	}
	return fillBillingEntityIDs(ctx, params, items,
		func(r *dto.RefundResponse) string { return customerByInvoice[r.InvoiceID] },
		func(r *dto.RefundResponse, id *string) { r.BillingEntityID = id })
}
