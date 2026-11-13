package selfbilling

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	maxBulkEvents = 100

	eventNameEventsIngested = "events_ingested"
	eventNameInvoiceValue   = "invoice_value"

	// usageSettleDelay lets an hour's late commits and replica lag land before it is sent; a sent hour is never corrected.
	usageSettleDelay = 15 * time.Minute

	// eventHourFormat keys an event to its UTC hour, so a re-sent hour reuses the same event ID.
	eventHourFormat = "2006010215"

	// usdCurrency is the currency Flexprice Cloud prices invoice value in.
	usdCurrency = "usd"
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

	envIDs := make([]string, 0, len(counts)+len(invoices))
	for _, c := range counts {
		envIDs = append(envIDs, c.EnvironmentID)
	}
	for _, inv := range invoices {
		envIDs = append(envIDs, inv.EnvironmentID)
	}

	// environment_type lets Flexprice Cloud meters bill production usage only; an unknown environment counts as production.
	envTypes, err := s.SelfBillingPostgresRepo.ListEnvironmentTypes(ctx, lo.Uniq(envIDs))
	if err != nil {
		return err
	}

	// Flexprice Cloud's FX rates are read only when a row needs converting, so a USD-only push never depends on them.
	usdRates := map[string]decimal.Decimal{usdCurrency: decimal.NewFromInt(1)}
	needsRates := lo.SomeBy(invoices, func(inv *domainsb.TenantHourInvoices) bool {
		return !types.IsMatchingCurrency(inv.Currency, usdCurrency)
	})
	if needsRates {
		cloudRates, err := s.client.USDRates(ctx)
		if err != nil {
			return err
		}
		maps.Copy(usdRates, cloudRates)
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
				"count":            strconv.FormatUint(c.Count, 10),
				"environment_id":   c.EnvironmentID,
				"environment_type": lo.CoalesceOrEmpty(envTypes[c.EnvironmentID], types.EnvironmentProduction).String(),
				"region":           region,
			},
		})
	}

	// amount repeats usd_amount, the field the invoice_value meter sums; the other amounts stay in the invoice currency.
	var missingRates []string
	for _, inv := range invoices {
		if s.Config.Billing.IsBillingTenant(inv.TenantID) {
			continue
		}

		currency := strings.ToUpper(inv.Currency)
		usdRate, ok := usdRates[strings.ToLower(inv.Currency)]
		if !ok {
			missingRates = append(missingRates, currency)
			continue
		}

		hour := inv.Hour.UTC()
		usdAmount := types.RoundToCurrencyPrecision(inv.Total.Mul(usdRate), usdCurrency).String()
		events = append(events, usageEvent{
			id:        strings.Join([]string{"inv", region, inv.TenantID, inv.EnvironmentID, currency, hour.Format(eventHourFormat)}, ":"),
			name:      eventNameInvoiceValue,
			extID:     s.extID(inv.TenantID),
			timestamp: hour,
			properties: map[string]string{
				"amount":                        usdAmount,
				"usd_amount":                    usdAmount,
				"usd_rate":                      usdRate.String(),
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
				"environment_type":              lo.CoalesceOrEmpty(envTypes[inv.EnvironmentID], types.EnvironmentProduction).String(),
				"region":                        region,
			},
		})
	}

	// A later run inside the lookback sends a skipped row once its rate exists on Flexprice Cloud.
	if len(missingRates) > 0 {
		s.Logger.Error(ctx, "self-billing invoice value skipped for currencies without a USD rate",
			"error", "no fx rate to usd on flexprice cloud",
			"currencies", lo.Uniq(missingRates),
		)
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
