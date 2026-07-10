package checks

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

type CycleInvoiceProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	cursor int64
}

func NewCycleInvoiceProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *CycleInvoiceProbe {
	return &CycleInvoiceProbe{client: c, reg: r, runID: runID}
}

func (p *CycleInvoiceProbe) Name() string         { return "cycle-invoice-probe" }
func (p *CycleInvoiceProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *CycleInvoiceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentSubIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	subID := seeds.PersistentSubIDs[int(idx)%len(seeds.PersistentSubIDs)]

	subResp, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		// 404 → sub not provisioned yet (first-run race); soft-skip.
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID}, "get sub %s: %w", subID, err)
	}
	cycleLength := extractBillingCycleLength(subResp)
	if cycleLength <= 0 {
		return nil
	}

	// Resolve the internal customer ID from the subscription response so we
	// can query invoices by customer_id. The server-side InvoiceFilter{SubscriptionID}
	// is unreliable (returns 0 results even when invoices exist), so we use
	// customer_id instead and filter client-side for the target subscription.
	custID := extractSubCustomerID(subResp)
	if custID == "" {
		// No customer ID available — skip rather than raise a false alert.
		return nil
	}

	invResp, err := p.client.Invoices().Query(ctx, sdktypes.InvoiceFilter{
		CustomerID: &custID,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID, "customer_id": custID}, "query invoices for customer %s (sub %s): %w", custID, subID, err)
	}

	latest := extractLatestInvoiceForSub(invResp, subID)
	if latest == nil {
		subAge := extractSubAge(subResp)
		if subAge > 0 && subAge > 2*cycleLength {
			return e2eprobe.Errorf(map[string]string{"subscription_id": subID}, "sub %s is %s old (>2 cycles) and has no invoices", subID, subAge)
		}
		return nil
	}
	lag := time.Since(latest.PeriodEnd)
	if lag > 2*cycleLength {
		return e2eprobe.Errorf(map[string]string{
			"subscription_id":   subID,
			"latest_period_end": latest.PeriodEnd.Format(time.RFC3339),
			"lag":               lag.String(),
		}, "invoice freshness: sub=%s latest_period_end=%s lag=%s cycle_length=%s (lag > 2*cycle)",
			subID, latest.PeriodEnd.Format(time.RFC3339), lag, cycleLength)
	}
	return nil
}

type latestInvoice struct {
	PeriodEnd time.Time
}

// billingPeriodDuration maps SDK BillingPeriod + count to a Go duration.
// Conservative default of 30 days for MONTHLY when count is nil or 0.
func billingPeriodDuration(period sdktypes.BillingPeriod, count int64) time.Duration {
	if count <= 0 {
		count = 1
	}
	var unit time.Duration
	switch period {
	case sdktypes.BillingPeriodDaily:
		unit = 24 * time.Hour
	case sdktypes.BillingPeriodWeekly:
		unit = 7 * 24 * time.Hour
	case sdktypes.BillingPeriodMonthly:
		unit = 30 * 24 * time.Hour
	case sdktypes.BillingPeriodQuarterly:
		unit = 91 * 24 * time.Hour
	case sdktypes.BillingPeriodHalfYearly:
		unit = 182 * 24 * time.Hour
	case sdktypes.BillingPeriodAnnual:
		unit = 365 * 24 * time.Hour
	default:
		// Unknown period — use conservative 30-day default so freshness
		// invariant still works for the most common case.
		return 30 * 24 * time.Hour
	}
	return unit * time.Duration(count)
}

// extractBillingCycleLength reads the billing cycle duration from the SDK
// GetSubscriptionResponse. Returns 30d default when fields are missing.
func extractBillingCycleLength(resp interface{}) time.Duration {
	r, ok := resp.(*sdkdtos.GetSubscriptionResponse)
	if !ok || r == nil {
		return 30 * 24 * time.Hour // conservative default
	}
	inner := r.GetSubscriptionResponse()
	if inner == nil {
		return 30 * 24 * time.Hour
	}
	period := inner.GetBillingPeriod()
	if period == nil {
		return 30 * 24 * time.Hour
	}
	var count int64
	if c := inner.GetBillingPeriodCount(); c != nil {
		count = *c
	}
	d := billingPeriodDuration(*period, count)
	if d <= 0 {
		return 30 * 24 * time.Hour
	}
	return d
}

// extractSubCustomerID reads the internal customer_id from the SDK
// GetSubscriptionResponse.
func extractSubCustomerID(resp interface{}) string {
	r, ok := resp.(*sdkdtos.GetSubscriptionResponse)
	if !ok || r == nil {
		return ""
	}
	inner := r.GetSubscriptionResponse()
	if inner == nil || inner.CustomerID == nil {
		return ""
	}
	return *inner.CustomerID
}

// extractLatestInvoiceForSub reads the most recent invoice (by PeriodEnd) from the
// SDK QueryInvoiceResponse, filtering client-side to only include invoices that
// belong to the given subID. Returns nil when no matching invoices are present.
//
// Note: InvoiceResponse.SubscriptionID is populated by the server; if the
// server omits it the filter falls back to returning all invoices for the customer.
func extractLatestInvoiceForSub(resp interface{}, subID string) *latestInvoice {
	r, ok := resp.(*sdkdtos.QueryInvoiceResponse)
	if !ok || r == nil {
		return nil
	}
	inner := r.GetListInvoicesResponse()
	if inner == nil {
		return nil
	}
	items := inner.GetItems()
	if len(items) == 0 {
		return nil
	}
	var best *latestInvoice
	for _, inv := range items {
		// Client-side filter: skip invoices for other subscriptions when the
		// SubscriptionID field is populated by the server.
		if inv.SubscriptionID != nil && *inv.SubscriptionID != subID {
			continue
		}
		if inv.PeriodEnd == nil {
			continue
		}
		t := *inv.PeriodEnd
		if best == nil || t.After(best.PeriodEnd) {
			cp := latestInvoice{PeriodEnd: t}
			best = &cp
		}
	}
	return best
}

// extractSubAge reads how long ago the subscription was created from the
// GetSubscriptionResponse. Returns 0 → probe skips age-based checks when
// the created_at field is absent or unparseable.
func extractSubAge(resp interface{}) time.Duration {
	r, ok := resp.(*sdkdtos.GetSubscriptionResponse)
	if !ok || r == nil {
		return 0
	}
	inner := r.GetSubscriptionResponse()
	if inner == nil || inner.CreatedAt == nil {
		return 0
	}
	age := time.Since(*inner.CreatedAt)
	if age < 0 {
		return 0
	}
	return age
}
