package billingentity

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
)

// ScopeActivity puts the billing entity of the activity's customer, else invoice, else subscription in ctx.
func ScopeActivity(ctx context.Context, scope interfaces.BillingEntityScope, customerID, invoiceID, subscriptionID string) (context.Context, error) {
	switch {
	case scope == nil:
		return ctx, nil
	case customerID != "":
		return scope.ScopeToCustomer(ctx, customerID)
	case invoiceID != "":
		return scope.ScopeToInvoice(ctx, invoiceID)
	case subscriptionID != "":
		return scope.ScopeToSubscription(ctx, subscriptionID)
	default:
		return ctx, nil
	}
}
