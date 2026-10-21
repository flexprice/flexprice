package selfbilling

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
)

const (
	maxBulkEvents = 100

	eventNameEventsIngested = "events_ingested"
	eventNameInvoiceValue   = "invoice_value"

	// usageSettleDelay lets an hour's late commits and replica lag land before it is sent; a sent hour is never corrected.
	usageSettleDelay = 15 * time.Minute

	// eventHourFormat keys an event to its UTC hour, so a re-sent hour reuses the same event ID.
	eventHourFormat = "2006010215"
)

// pushUsage re-sends the settled hours [to-lookback, to), to = floor(now-settle delay); deterministic IDs make Flexprice Cloud drop repeats.
func (s *selfBillingService) pushUsage(ctx context.Context, now time.Time) error {
	to := now.UTC().Add(-usageSettleDelay).Truncate(time.Hour)
	from := to.Add(-s.Config.Billing.UsageLookback())
	region := s.Config.Billing.Region

	counts, err := s.SelfBillingClickHouseRepo.CountEventsByTenantHour(ctx, from, to)
	if err != nil {
		return err
	}

	invoices, err := s.SelfBillingPostgresRepo.SumInvoicesByTenantHour(ctx, from, to)
	if err != nil {
		return err
	}

	events := make([]usageEvent, 0, len(counts)+len(invoices))
	for _, c := range counts {
		if s.Config.Billing.IsBillingTenant(c.TenantID) {
			continue
		}

		hour := c.Hour.UTC()
		events = append(events, usageEvent{
			id:        strings.Join([]string{"evt", region, c.TenantID, c.EnvironmentID, hour.Format(eventHourFormat)}, ":"),
			name:      eventNameEventsIngested,
			extID:     s.extID(c.TenantID),
			timestamp: hour,
			properties: map[string]string{
				"count":          strconv.FormatUint(c.Count, 10),
				"environment_id": c.EnvironmentID,
				"region":         region,
			},
		})
	}

	// amount stays equal to total, the field the invoice_value meter sums.
	for _, inv := range invoices {
		if s.Config.Billing.IsBillingTenant(inv.TenantID) {
			continue
		}

		hour := inv.Hour.UTC()
		currency := strings.ToUpper(inv.Currency)
		events = append(events, usageEvent{
			id:        strings.Join([]string{"inv", region, inv.TenantID, inv.EnvironmentID, currency, hour.Format(eventHourFormat)}, ":"),
			name:      eventNameInvoiceValue,
			extID:     s.extID(inv.TenantID),
			timestamp: hour,
			properties: map[string]string{
				"amount":                        inv.Total.String(),
				"total":                         inv.Total.String(),
				"subtotal":                      inv.Subtotal.String(),
				"total_discount":                inv.TotalDiscount.String(),
				"total_prepaid_credits_applied": inv.TotalPrepaidCreditsApplied.String(),
				"total_tax":                     inv.TotalTax.String(),
				"amount_due":                    inv.AmountDue.String(),
				"amount_paid":                   inv.AmountPaid.String(),
				"invoice_count":                 strconv.FormatUint(inv.InvoiceCount, 10),
				"currency":                      currency,
				"environment_id":                inv.EnvironmentID,
				"region":                        region,
			},
		})
	}

	for _, chunk := range lo.Chunk(events, maxBulkEvents) {
		if err := s.client.IngestBulk(ctx, chunk); err != nil {
			return err
		}
	}

	s.Logger.Info(ctx, "self-billing usage pushed",
		"event_counts", len(counts),
		"invoice_sums", len(invoices),
		"events", len(events),
		"from", from,
		"to", to,
	)
	return nil
}
