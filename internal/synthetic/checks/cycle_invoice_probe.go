package checks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type CycleInvoiceProbe struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
	cursor int64
}

func NewCycleInvoiceProbe(c synthetic.Client, r synthetic.Registry, runID string) *CycleInvoiceProbe {
	return &CycleInvoiceProbe{client: c, reg: r, runID: runID}
}

func (p *CycleInvoiceProbe) Name() string         { return "cycle-invoice-probe" }
func (p *CycleInvoiceProbe) Kind() synthetic.Kind { return synthetic.KindProbe }

func (p *CycleInvoiceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentSubIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	subID := seeds.PersistentSubIDs[int(idx)%len(seeds.PersistentSubIDs)]

	subResp, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return fmt.Errorf("get sub %s: %w", subID, err)
	}
	cycleLength := extractBillingCycleLength(subResp)
	if cycleLength <= 0 {
		return nil
	}

	invResp, err := p.client.Invoices().Query(ctx, types.InvoiceFilter{
		SubscriptionID: &subID,
	})
	if err != nil {
		return fmt.Errorf("query invoices for %s: %w", subID, err)
	}
	latest := extractLatestInvoice(invResp)
	if latest == nil {
		subAge := extractSubAge(subResp)
		if subAge > 0 && subAge > 2*cycleLength {
			return fmt.Errorf("sub %s is %s old (>2 cycles) and has no invoices", subID, subAge)
		}
		return nil
	}
	lag := time.Since(latest.PeriodEnd)
	if lag > 2*cycleLength {
		return fmt.Errorf("invoice freshness: sub=%s latest_period_end=%s lag=%s cycle_length=%s (lag > 2*cycle)",
			subID, latest.PeriodEnd.Format(time.RFC3339), lag, cycleLength)
	}
	return nil
}

type latestInvoice struct {
	PeriodEnd time.Time
}

// extractBillingCycleLength reads the billing cycle duration from the SDK subscription response.
// Filled in by Task 25. Returns 0 → probe soft no-ops when cycle length is unknown.
func extractBillingCycleLength(_ interface{}) time.Duration { return 0 }

// extractLatestInvoice reads the most recent invoice from the SDK invoice query response.
// Filled in by Task 25. Returns nil → probe assumes no invoices yet.
func extractLatestInvoice(_ interface{}) *latestInvoice { return nil }

// extractSubAge reads the subscription age from the SDK subscription response.
// Filled in by Task 25. Returns 0 → probe skips age-based checks.
func extractSubAge(_ interface{}) time.Duration { return 0 }
