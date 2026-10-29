package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

type trialEndCollectionKey struct{}

// The transition authorizes one paid period; invoice creation binds it to one draft.
type trialEndCollection struct {
	tenantID       string
	environmentID  string
	subscriptionID string
	customerID     string
	currency       string
	periodStart    time.Time
	periodEnd      time.Time
	asOf           time.Time
	invoiceID      string
}

func withTrialEndCollection(ctx context.Context, sub *subscription.Subscription, asOf time.Time) context.Context {
	authorization := trialEndCollection{}
	if sub != nil && commitmentTermsScope(ctx, sub.TenantID, sub.EnvironmentID, sub.Status) &&
		sub.SubscriptionStatus == types.SubscriptionStatusIncomplete &&
		sub.TrialStart != nil && sub.TrialEnd != nil && sub.TrialStart.Before(*sub.TrialEnd) &&
		commitmentTermsDate(*sub.TrialStart) && commitmentTermsDate(*sub.TrialEnd) && commitmentTermsDate(sub.CurrentPeriodEnd) &&
		commitmentTermsDate(asOf) && !sub.TrialEnd.After(asOf) &&
		sub.CurrentPeriodStart.Equal(*sub.TrialEnd) && sub.CurrentPeriodStart.Before(sub.CurrentPeriodEnd) {
		authorization = trialEndCollection{
			tenantID: sub.TenantID, environmentID: sub.EnvironmentID,
			subscriptionID: sub.ID, customerID: sub.GetInvoicingCustomerID(), currency: sub.Currency,
			periodStart: sub.CurrentPeriodStart, periodEnd: sub.CurrentPeriodEnd, asOf: asOf,
		}
	}
	return context.WithValue(ctx, trialEndCollectionKey{}, authorization)
}

func bindTrialEndCollection(ctx context.Context, inv *invoice.Invoice) context.Context {
	authorization, ok := ctx.Value(trialEndCollectionKey{}).(trialEndCollection)
	if !ok {
		return ctx
	}
	if !authorization.matchesInvoice(ctx, inv) {
		return context.WithValue(ctx, trialEndCollectionKey{}, trialEndCollection{})
	}
	authorization.invoiceID = inv.ID
	return context.WithValue(ctx, trialEndCollectionKey{}, authorization)
}

func revalidateTrialEndCollection(ctx context.Context, inv *invoice.Invoice) context.Context {
	authorization, ok := ctx.Value(trialEndCollectionKey{}).(trialEndCollection)
	if !ok {
		return ctx
	}
	if authorization.invoiceID == "" || !authorization.matchesInvoice(ctx, inv) {
		return context.WithValue(ctx, trialEndCollectionKey{}, trialEndCollection{})
	}
	return ctx
}

func withoutTrialEndCollection(ctx context.Context) context.Context {
	if _, ok := ctx.Value(trialEndCollectionKey{}).(trialEndCollection); !ok {
		return ctx
	}
	return context.WithValue(ctx, trialEndCollectionKey{}, trialEndCollection{})
}

func (a trialEndCollection) matchesInvoice(ctx context.Context, inv *invoice.Invoice) bool {
	return inv != nil && inv.ID != "" && (a.invoiceID == "" || a.invoiceID == inv.ID) &&
		a.tenantID != "" && a.environmentID != "" &&
		a.tenantID == types.GetTenantID(ctx) && a.environmentID == types.GetEnvironmentID(ctx) &&
		inv.TenantID == a.tenantID && inv.EnvironmentID == a.environmentID && inv.Status == types.StatusPublished &&
		inv.InvoiceType == types.InvoiceTypeSubscription && inv.InvoiceStatus == types.InvoiceStatusDraft &&
		inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialEnd) &&
		inv.SubscriptionID != nil && *inv.SubscriptionID == a.subscriptionID &&
		inv.CustomerID == a.customerID && strings.EqualFold(inv.Currency, a.currency) &&
		inv.PeriodStart != nil && inv.PeriodStart.Equal(a.periodStart) &&
		inv.PeriodEnd != nil && inv.PeriodEnd.Equal(a.periodEnd)
}

func trialEndCollectionAllowed(ctx context.Context, sub *subscription.Subscription, delivery types.BillingTermDelivery) bool {
	authorization, ok := ctx.Value(trialEndCollectionKey{}).(trialEndCollection)
	if !ok || authorization.invoiceID == "" || sub == nil || delivery != types.BillingTermSubscriptionInvoice {
		return false
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusIncomplete || sub.PauseStatus == types.PauseStatusActive || sub.CancelledAt != nil {
		return false
	}
	if sub.CancelAt != nil && !sub.CancelAt.After(authorization.asOf) || sub.EndDate != nil && !sub.EndDate.After(authorization.asOf) {
		return false
	}
	return commitmentTermsScope(ctx, sub.TenantID, sub.EnvironmentID, sub.Status) &&
		sub.TenantID == authorization.tenantID && sub.EnvironmentID == authorization.environmentID &&
		sub.ID == authorization.subscriptionID && sub.GetInvoicingCustomerID() == authorization.customerID &&
		strings.EqualFold(sub.Currency, authorization.currency) &&
		sub.TrialStart != nil && sub.TrialEnd != nil && sub.TrialStart.Before(*sub.TrialEnd) &&
		sub.TrialEnd.Equal(authorization.periodStart) && !sub.TrialEnd.After(authorization.asOf) &&
		sub.CurrentPeriodStart.Equal(authorization.periodStart) && sub.CurrentPeriodEnd.Equal(authorization.periodEnd)
}
