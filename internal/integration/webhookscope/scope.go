// Package webhookscope checks that a record an inbound provider event names belongs to the billing entity of the connection that sent it.
package webhookscope

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Customer reports whether the customer belongs to ctx's billing entity; true when ctx has no entity.
// It logs when the event is ignored; a lookup error is returned for the caller to handle as any other lookup failure.
func Customer(ctx context.Context, scope interfaces.BillingEntityScope, log *logger.Logger, customerID string) (bool, error) {
	if types.GetBillingEntityID(ctx) == "" {
		return true, nil
	}
	return inScope(ctx, scope, log, customerID, "customer_id", customerID)
}

// CustomerByEmail returns the first customer with the email in ctx's billing entity, or nil when there is none.
// An enabling tenant refuses entity filters; every customer then belongs to the default entity, so any match is in scope.
func CustomerByEmail(ctx context.Context, customers interfaces.CustomerService, email string) (*dto.CustomerResponse, error) {
	filter := &types.CustomerFilter{
		Email:           email,
		QueryFilter:     types.NewDefaultQueryFilter(),
		BillingEntityID: types.GetBillingEntityID(ctx),
	}
	list, err := customers.GetCustomers(ctx, filter)
	if errors.Is(err, ierr.ErrBillingEntitiesNotEnabled) {
		filter.BillingEntityID = ""
		list, err = customers.GetCustomers(ctx, filter)
	}
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		return nil, nil
	}
	return list.Items[0], nil
}

// Payment is Customer for the payment's customer: the invoice's for an invoice payment, else its destination.
func Payment(ctx context.Context, scope interfaces.BillingEntityScope, payments interfaces.PaymentService, invoices interfaces.InvoiceService, log *logger.Logger, paymentID string) (bool, error) {
	if types.GetBillingEntityID(ctx) == "" {
		return true, nil
	}
	p, err := payments.GetPayment(ctx, paymentID)
	if err != nil {
		return false, err
	}
	customerID := p.DestinationID
	if p.DestinationType == types.PaymentDestinationTypeInvoice {
		inv, err := invoices.GetInvoice(ctx, p.DestinationID)
		if err != nil {
			return false, err
		}
		customerID = inv.CustomerID
	}
	return inScope(ctx, scope, log, customerID, "payment_id", paymentID)
}

// Subscription is Customer for the subscription's customer.
func Subscription(ctx context.Context, scope interfaces.BillingEntityScope, subscriptions interfaces.SubscriptionService, log *logger.Logger, subscriptionID string) (bool, error) {
	if types.GetBillingEntityID(ctx) == "" {
		return true, nil
	}
	sub, err := subscriptions.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return false, err
	}
	return inScope(ctx, scope, log, sub.CustomerID, "subscription_id", subscriptionID)
}

func inScope(ctx context.Context, scope interfaces.BillingEntityScope, log *logger.Logger, customerID, recordKey, recordID string) (bool, error) {
	ok, err := scope.InScope(ctx, customerID)
	if err != nil || ok {
		return ok, err
	}
	log.Info(ctx, "ignoring webhook event for another billing entity",
		"tenant_id", types.GetTenantID(ctx),
		"billing_entity_id", types.GetBillingEntityID(ctx),
		"connection_id", types.GetConnectionID(ctx),
		"customer_id", customerID,
		recordKey, recordID)
	return false, nil
}
