package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// linkedCustomerID runs read in a repository span, retries a replica miss on the primary and marks a missing row as not found.
func linkedCustomerID(ctx context.Context, entity, id string, read func(ctx context.Context) (string, error)) (string, error) {
	span := StartRepositorySpan(ctx, entity, "get_linked_customer_id", map[string]interface{}{"id": id})
	defer FinishSpan(span)

	customerID, err := read(ctx)
	if ent.IsNotFound(err) {
		// A just-written row may not have reached the replica yet.
		customerID, err = read(types.WithForceWriter(ctx))
	}
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return "", ierr.WithError(err).WithHintf("Customer of %s %s not found", entity, id).Mark(ierr.ErrNotFound)
		}
		return "", ierr.WithError(err).WithHintf("Failed to read the customer of %s %s", entity, id).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return customerID, nil
}

// invoiceCustomerID reads the customer of an invoice in any status.
func invoiceCustomerID(ctx context.Context, client *ent.Client, id string) (string, error) {
	return client.Invoice.Query().
		Where(invoice.ID(id), invoice.TenantID(types.GetTenantID(ctx)), invoice.EnvironmentID(types.GetEnvironmentID(ctx))).
		Select(invoice.FieldCustomerID).
		String(ctx)
}

// subscriptionCustomerID reads the customer of a subscription in any status.
func subscriptionCustomerID(ctx context.Context, client *ent.Client, id string) (string, error) {
	return client.Subscription.Query().
		Where(subscription.ID(id), subscription.TenantID(types.GetTenantID(ctx)), subscription.EnvironmentID(types.GetEnvironmentID(ctx))).
		Select(subscription.FieldCustomerID).
		String(ctx)
}
