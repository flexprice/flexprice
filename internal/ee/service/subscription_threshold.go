package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

func thresholdBillingEligible(ctx context.Context, sub *subscription.Subscription, asOf time.Time) bool {
	return sub != nil && subscriptionUpdateScoped(ctx, sub, sub.ID) &&
		sub.SubscriptionStatus == types.SubscriptionStatusActive && sub.PauseStatus != types.PauseStatusActive &&
		sub.CancelledAt == nil && (sub.CancelAt == nil || sub.CancelAt.After(asOf)) &&
		(sub.EndDate == nil || sub.EndDate.After(asOf)) &&
		sub.AutoInvoiceThreshold != nil && sub.AutoInvoiceThreshold.IsPositive() &&
		asOf.After(sub.CurrentPeriodStart) && asOf.Before(sub.CurrentPeriodEnd)
}

func sameThresholdBillingPeriod(before, current *subscription.Subscription) bool {
	return before.UpdatedAt.Equal(current.UpdatedAt) && before.PlanID == current.PlanID && before.BillingAnchor.Equal(current.BillingAnchor) &&
		before.CurrentPeriodStart.Equal(current.CurrentPeriodStart) && before.CurrentPeriodEnd.Equal(current.CurrentPeriodEnd) &&
		before.Currency == current.Currency && before.GetInvoicingCustomerID() == current.GetInvoicingCustomerID() &&
		before.AutoInvoiceThreshold != nil && current.AutoInvoiceThreshold != nil && before.AutoInvoiceThreshold.Equal(*current.AutoInvoiceThreshold)
}
