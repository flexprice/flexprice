package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func (s *invoiceService) projectSubscriptionBillingSchedule(ctx context.Context, sub *subscription.Subscription, request *dto.CreateInvoiceRequest) error {
	if sub == nil || request == nil || !finiteScheduleDate(request.PeriodStart) || !finiteScheduleDate(request.PeriodEnd) || !request.PeriodStart.Before(*request.PeriodEnd) {
		return ierr.Validation("billing preview requires a subscription and finite ordered period")
	}
	if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing || sub.SubscriptionType == types.SubscriptionTypeInherited || s.CommitmentRepo == nil {
		return nil
	}
	destination, err := request.ToInvoice(ctx)
	if err != nil {
		return err
	}
	cutoff, err := invoiceCollectionCutoff(destination)
	if err != nil {
		return err
	}
	discovered, err := s.discoverInvoiceCollection(ctx, destination)
	if err != nil {
		return err
	}
	projection, err := projectBillingSchedule(ctx, s.ServiceParams, discovered.owners, destination.CustomerID, destination.Currency, time.Now().UTC())
	if err != nil {
		return err
	}
	total := decimal.Zero
	for _, item := range projection.due {
		if item.Delivery != types.BillingTermSubscriptionInvoice || item.BillOn.After(cutoff) {
			continue
		}
		request.LineItems = append(request.LineItems, schedulePreviewRequest(item))
		total = total.Add(item.Amount)
	}
	invoices := make(map[string]*invoice.Invoice)
	for _, item := range projection.assigned {
		if item.Delivery != types.BillingTermSubscriptionInvoice {
			continue
		}
		inv, line, err := projectedScheduleLine(ctx, s.ServiceParams, item, invoices)
		if err != nil {
			return err
		}
		if line == nil || inv.InvoiceType != types.InvoiceTypeSubscription || !sameInvoicePeriodDate(inv.PeriodStart, request.PeriodStart) || !sameInvoicePeriodDate(inv.PeriodEnd, request.PeriodEnd) {
			continue
		}
		request.LineItems = append(request.LineItems, schedulePreviewRequest(item))
		total = total.Add(item.Amount)
	}
	request.Subtotal = request.Subtotal.Add(total)
	request.Total = request.Total.Add(total)
	request.AmountDue = request.AmountDue.Add(total)
	return nil
}
