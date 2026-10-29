package billingterms

import (
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/types"
)

func collectionSummary(owner *domain.OwnerSnapshot, items []*domain.ScheduleItem, bindings map[string]domain.BindingState, asOf time.Time) (*domain.CollectionSummary, error) {
	summary := &domain.CollectionSummary{
		AsOf:         asOf.UTC(),
		Currency:     owner.Currency,
		TargetAmount: owner.CollectionTarget,
	}
	for _, item := range items {
		if item.ScheduleStatus == types.BillingScheduleInvoiced {
			summary.AssignedAmount = summary.AssignedAmount.Add(item.Amount)
			if bindings[item.ID].InvoiceStatus == types.InvoiceStatusFinalized {
				summary.InvoicedAmount = summary.InvoicedAmount.Add(item.Amount)
			}
			continue
		}
		summary.ScheduledAmount = summary.ScheduledAmount.Add(item.Amount)
		if pendingContextDrift(item, owner) {
			summary.RequiresReplan = true
		}
		if item.BillOn == nil {
			continue
		}
		if !item.BillOn.After(asOf) {
			summary.DueAmount = summary.DueAmount.Add(item.Amount)
		}
		if summary.NextBillOn == nil || item.BillOn.Before(*summary.NextBillOn) {
			date := item.BillOn.UTC()
			summary.NextBillOn = &date
		}
	}
	summary.UnallocatedAmount = summary.TargetAmount.Sub(summary.AssignedAmount).Sub(summary.ScheduledAmount)
	if summary.UnallocatedAmount.IsNegative() {
		return nil, integrity("billing schedule overallocates its collection target")
	}
	summary.RequiresReplan = summary.RequiresReplan || summary.UnallocatedAmount.IsPositive()
	reason := owner.DeferredReason
	if summary.RequiresReplan && (reason == "" || reason == "delivery_disabled") {
		reason = "requires_replan"
	}
	if reason == "" && !owner.CanCollect {
		reason = "delivery_disabled"
	}
	if reason != "" {
		summary.DeferredReason = &reason
	}
	return summary, nil
}

func pendingContextDrift(item *domain.ScheduleItem, owner *domain.OwnerSnapshot) bool {
	if item.CustomerID != owner.CustomerID || item.Delivery != owner.Delivery || (item.SubscriptionID == nil) != (owner.SubscriptionID == nil) {
		return true
	}
	return item.SubscriptionID != nil && *item.SubscriptionID != *owner.SubscriptionID
}
