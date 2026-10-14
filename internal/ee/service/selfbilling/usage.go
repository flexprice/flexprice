package selfbilling

import (
	"context"
	"strconv"
	"strings"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/samber/lo"
)

const (
	maxBulkEvents       = 1000
	eventNameIngested   = "events_ingested"
	eventNameInvoiceVal = "invoice_value"
)

// usageWindow is the closed hours [floor(now)-lookback, floor(now)); the open hour is never sent.
func usageWindow(now time.Time, lookback time.Duration) (time.Time, time.Time) {
	to := now.UTC().Truncate(time.Hour)
	return to.Add(-lookback), to
}

func (s *selfBillingService) pushUsage(ctx context.Context, now time.Time) error {
	from, to := usageWindow(now, s.Config.Billing.UsageLookback())
	counts, err := s.events.CountByTenantHour(ctx, from, to)
	if err != nil {
		return err
	}
	invoices, err := s.invoices.ListFinalized(ctx, from, to)
	if err != nil {
		return err
	}
	events := append(s.countEvents(counts), s.invoiceEvents(invoices)...)
	for _, chunk := range lo.Chunk(events, maxBulkEvents) {
		if err := s.client.IngestBulk(ctx, chunk); err != nil {
			return err
		}
	}
	s.Logger.Info(ctx, "self-billing usage pushed", "events", len(events), "from", from, "to", to)
	return nil
}

// countEvents emits one event per tenant environment hour; id and timestamp are both deterministic so re-sends dedupe.
// A client re-send that lands in a later ingest hour is counted again there (accepted v1 overcount).
func (s *selfBillingService) countEvents(counts []*domainsb.TenantEnvHourCount) []usageEvent {
	out := make([]usageEvent, 0, len(counts))
	for _, c := range counts {
		if s.Config.Billing.IsBillingTenant(c.TenantID) {
			continue
		}
		hour := c.Hour.UTC()
		out = append(out, usageEvent{
			id:        "evt:" + s.region() + ":" + c.TenantID + ":" + c.EnvironmentID + ":" + hour.Format("2006010215"),
			name:      eventNameIngested,
			extID:     externalID(s.region(), c.TenantID),
			timestamp: hour,
			properties: map[string]string{
				"count":          strconv.FormatUint(c.Count, 10),
				"environment_id": c.EnvironmentID,
				"region":         s.region(),
			},
		})
	}
	return out
}

// invoiceEvents sends the invoice breakdown; amount stays equal to total, the field the meter sums today.
func (s *selfBillingService) invoiceEvents(invoices []*domainsb.FinalizedInvoice) []usageEvent {
	out := make([]usageEvent, 0, len(invoices))
	for _, inv := range invoices {
		if s.Config.Billing.IsBillingTenant(inv.TenantID) {
			continue
		}
		out = append(out, usageEvent{
			id:        inv.ID,
			name:      eventNameInvoiceVal,
			extID:     externalID(s.region(), inv.TenantID),
			timestamp: inv.FinalizedAt.UTC(),
			properties: map[string]string{
				"amount":                        inv.Total.String(),
				"total":                         inv.Total.String(),
				"subtotal":                      inv.Subtotal.String(),
				"total_discount":                inv.TotalDiscount.String(),
				"total_prepaid_credits_applied": inv.TotalPrepaidCreditsApplied.String(),
				"total_tax":                     inv.TotalTax.String(),
				"amount_due":                    inv.AmountDue.String(),
				"amount_paid":                   inv.AmountPaid.String(),
				"currency":                      strings.ToUpper(inv.Currency),
				"environment_id":                inv.EnvironmentID,
				"invoice_type":                  inv.InvoiceType,
				"billing_reason":                inv.BillingReason,
				"subscription_id":               inv.SubscriptionID,
				"period_start":                  formatTime(inv.PeriodStart),
				"period_end":                    formatTime(inv.PeriodEnd),
			},
		})
	}
	return out
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
