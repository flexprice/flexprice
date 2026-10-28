// Package webhookscope checks that a record an inbound provider event names belongs to the billing entity of the connection that sent it.
package webhookscope

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Customer reports whether the customer belongs to ctx's billing entity; true when ctx has no entity.
// It logs when the event is ignored or the entity cannot be read, so callers just skip the event on false.
func Customer(ctx context.Context, scope interfaces.BillingEntityScope, log *logger.Logger, customerID string) bool {
	if types.GetBillingEntityID(ctx) == "" {
		return true
	}
	return inScope(ctx, scope, log, customerID, "customer_id", customerID)
}

// Invoice is Customer for the invoice's customer.
func Invoice(ctx context.Context, scope interfaces.BillingEntityScope, invoices interfaces.InvoiceService, log *logger.Logger, invoiceID string) bool {
	if types.GetBillingEntityID(ctx) == "" {
		return true
	}
	inv, err := invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		logLookupError(ctx, log, err, "invoice_id", invoiceID)
		return false
	}
	return inScope(ctx, scope, log, inv.CustomerID, "invoice_id", invoiceID)
}

// Payment is Customer for the payment's customer: the invoice's for an invoice payment, else its destination.
func Payment(ctx context.Context, scope interfaces.BillingEntityScope, payments interfaces.PaymentService, invoices interfaces.InvoiceService, log *logger.Logger, paymentID string) bool {
	if types.GetBillingEntityID(ctx) == "" {
		return true
	}
	p, err := payments.GetPayment(ctx, paymentID)
	if err != nil {
		logLookupError(ctx, log, err, "payment_id", paymentID)
		return false
	}
	customerID := p.DestinationID
	if p.DestinationType == types.PaymentDestinationTypeInvoice {
		inv, err := invoices.GetInvoice(ctx, p.DestinationID)
		if err != nil {
			logLookupError(ctx, log, err, "payment_id", paymentID)
			return false
		}
		customerID = inv.CustomerID
	}
	return inScope(ctx, scope, log, customerID, "payment_id", paymentID)
}

// Subscription is Customer for the subscription's customer.
func Subscription(ctx context.Context, scope interfaces.BillingEntityScope, subscriptions interfaces.SubscriptionService, log *logger.Logger, subscriptionID string) bool {
	if types.GetBillingEntityID(ctx) == "" {
		return true
	}
	sub, err := subscriptions.GetSubscription(ctx, subscriptionID)
	if err != nil {
		logLookupError(ctx, log, err, "subscription_id", subscriptionID)
		return false
	}
	return inScope(ctx, scope, log, sub.CustomerID, "subscription_id", subscriptionID)
}

func inScope(ctx context.Context, scope interfaces.BillingEntityScope, log *logger.Logger, customerID, recordKey, recordID string) bool {
	ok, err := scope.InScope(ctx, customerID)
	if err != nil {
		logLookupError(ctx, log, err, recordKey, recordID)
		return false
	}
	if !ok {
		log.Info(ctx, "ignoring webhook event for another billing entity",
			"tenant_id", types.GetTenantID(ctx),
			"billing_entity_id", types.GetBillingEntityID(ctx),
			"connection_id", types.GetConnectionID(ctx),
			"customer_id", customerID,
			recordKey, recordID)
	}
	return ok
}

func logLookupError(ctx context.Context, log *logger.Logger, err error, recordKey, recordID string) {
	log.Error(ctx, "failed to read the billing entity of a webhook record, skipping event",
		"error", err,
		"tenant_id", types.GetTenantID(ctx),
		"billing_entity_id", types.GetBillingEntityID(ctx),
		"connection_id", types.GetConnectionID(ctx),
		recordKey, recordID)
}
