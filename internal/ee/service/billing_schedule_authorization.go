package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

type pendingCollectionKey struct{}

// A guarded activation authorizes one paid period, bound to a single draft invoice.
type pendingCollection struct {
	tenantID       string
	environmentID  string
	subscriptionID string
	customerID     string
	currency       string
	periodStart    time.Time
	periodEnd      time.Time
	asOf           time.Time
	invoiceID      string
	reason         types.InvoiceBillingReason
}

func withTrialEndCollection(ctx context.Context, sub *subscription.Subscription, asOf time.Time) context.Context {
	authorization := pendingCollection{}
	if sub != nil && commitmentTermsScope(ctx, sub.TenantID, sub.EnvironmentID, sub.Status) &&
		sub.SubscriptionStatus == types.SubscriptionStatusIncomplete &&
		sub.TrialStart != nil && sub.TrialEnd != nil && sub.TrialStart.Before(*sub.TrialEnd) &&
		commitmentTermsDate(*sub.TrialStart) && commitmentTermsDate(*sub.TrialEnd) && commitmentTermsDate(sub.CurrentPeriodEnd) &&
		commitmentTermsDate(asOf) && !sub.TrialEnd.After(asOf) &&
		sub.CurrentPeriodStart.Equal(*sub.TrialEnd) && sub.CurrentPeriodStart.Before(sub.CurrentPeriodEnd) {
		authorization = pendingCollection{
			tenantID: sub.TenantID, environmentID: sub.EnvironmentID,
			subscriptionID: sub.ID, customerID: sub.GetInvoicingCustomerID(), currency: sub.Currency,
			periodStart: sub.CurrentPeriodStart, periodEnd: sub.CurrentPeriodEnd, asOf: asOf,
			reason: types.InvoiceBillingReasonSubscriptionTrialEnd,
		}
	}
	return context.WithValue(ctx, pendingCollectionKey{}, authorization)
}

func bindPendingCollection(ctx context.Context, inv *invoice.Invoice) context.Context {
	authorization, ok := ctx.Value(pendingCollectionKey{}).(pendingCollection)
	if !ok {
		return ctx
	}
	if !authorization.matchesInvoice(ctx, inv) {
		return context.WithValue(ctx, pendingCollectionKey{}, pendingCollection{})
	}
	authorization.invoiceID = inv.ID
	return context.WithValue(ctx, pendingCollectionKey{}, authorization)
}

func revalidatePendingCollection(ctx context.Context, inv *invoice.Invoice) context.Context {
	authorization, ok := ctx.Value(pendingCollectionKey{}).(pendingCollection)
	if !ok {
		return ctx
	}
	if authorization.invoiceID == "" || !authorization.matchesInvoice(ctx, inv) {
		return context.WithValue(ctx, pendingCollectionKey{}, pendingCollection{})
	}
	return ctx
}

func withoutPendingCollection(ctx context.Context) context.Context {
	if _, ok := ctx.Value(pendingCollectionKey{}).(pendingCollection); !ok {
		return ctx
	}
	return context.WithValue(ctx, pendingCollectionKey{}, pendingCollection{})
}

func (a pendingCollection) matchesInvoice(ctx context.Context, inv *invoice.Invoice) bool {
	return inv != nil && inv.ID != "" && (a.invoiceID == "" || a.invoiceID == inv.ID) &&
		a.tenantID != "" && a.environmentID != "" &&
		a.tenantID == types.GetTenantID(ctx) && a.environmentID == types.GetEnvironmentID(ctx) &&
		inv.TenantID == a.tenantID && inv.EnvironmentID == a.environmentID && inv.Status == types.StatusPublished &&
		inv.InvoiceType == types.InvoiceTypeSubscription && inv.InvoiceStatus == types.InvoiceStatusDraft &&
		inv.BillingReason == string(a.reason) &&
		inv.SubscriptionID != nil && *inv.SubscriptionID == a.subscriptionID &&
		inv.CustomerID == a.customerID && strings.EqualFold(inv.Currency, a.currency) &&
		inv.PeriodStart != nil && inv.PeriodStart.Equal(a.periodStart) &&
		inv.PeriodEnd != nil && inv.PeriodEnd.Equal(a.periodEnd)
}

func pendingCollectionAllowed(ctx context.Context, sub *subscription.Subscription, delivery types.BillingTermDelivery) bool {
	authorization, ok := ctx.Value(pendingCollectionKey{}).(pendingCollection)
	if !ok || authorization.invoiceID == "" || sub == nil || delivery != types.BillingTermSubscriptionInvoice {
		return false
	}
	if sub.PauseStatus == types.PauseStatusActive || sub.ActivePauseID != nil || sub.CancelledAt != nil {
		return false
	}
	if sub.CancelAt != nil && !sub.CancelAt.After(authorization.asOf) || sub.EndDate != nil && !sub.EndDate.After(authorization.asOf) {
		return false
	}
	if !commitmentTermsScope(ctx, sub.TenantID, sub.EnvironmentID, sub.Status) ||
		sub.TenantID != authorization.tenantID || sub.EnvironmentID != authorization.environmentID ||
		sub.ID != authorization.subscriptionID || sub.GetInvoicingCustomerID() != authorization.customerID ||
		!strings.EqualFold(sub.Currency, authorization.currency) ||
		!sub.CurrentPeriodStart.Equal(authorization.periodStart) || !sub.CurrentPeriodEnd.Equal(authorization.periodEnd) {
		return false
	}
	switch authorization.reason {
	case types.InvoiceBillingReasonSubscriptionCreate:
		return checkoutDraftEligible(sub, authorization.asOf) && draftActivationPaidPeriod(sub, authorization.asOf)
	case types.InvoiceBillingReasonSubscriptionTrialEnd:
		return sub.SubscriptionStatus == types.SubscriptionStatusIncomplete &&
			sub.TrialStart != nil && sub.TrialEnd != nil && sub.TrialStart.Before(*sub.TrialEnd) &&
			sub.TrialEnd.Equal(authorization.periodStart) && !sub.TrialEnd.After(authorization.asOf)
	default:
		return false
	}
}

func withDraftActivationCollection(ctx context.Context, sub *subscription.Subscription, asOf time.Time) context.Context {
	authorization := pendingCollection{}
	if sub != nil && commitmentTermsScope(ctx, sub.TenantID, sub.EnvironmentID, sub.Status) &&
		commitmentTermsDate(asOf) && commitmentTermsDate(sub.CurrentPeriodStart) && commitmentTermsDate(sub.CurrentPeriodEnd) &&
		sub.CurrentPeriodStart.Before(sub.CurrentPeriodEnd) && checkoutDraftEligible(sub, asOf) && draftActivationPaidPeriod(sub, asOf) {
		authorization = pendingCollection{
			tenantID: sub.TenantID, environmentID: sub.EnvironmentID,
			subscriptionID: sub.ID, customerID: sub.GetInvoicingCustomerID(), currency: sub.Currency,
			periodStart: sub.CurrentPeriodStart, periodEnd: sub.CurrentPeriodEnd, asOf: asOf,
			reason: types.InvoiceBillingReasonSubscriptionCreate,
		}
	}
	return context.WithValue(ctx, pendingCollectionKey{}, authorization)
}

func draftActivationPaidPeriod(sub *subscription.Subscription, asOf time.Time) bool {
	if sub.TrialStart == nil && sub.TrialEnd == nil {
		return true
	}
	return sub.TrialStart != nil && sub.TrialEnd != nil && commitmentTermsDate(*sub.TrialStart) && commitmentTermsDate(*sub.TrialEnd) &&
		sub.TrialStart.Before(*sub.TrialEnd) && !sub.TrialEnd.After(sub.CurrentPeriodStart) && !sub.TrialEnd.After(asOf)
}
