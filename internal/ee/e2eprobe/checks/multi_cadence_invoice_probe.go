package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

// multiCadenceInvoiceProbe verifies that the persistent quarterly subscription
// seeded by ensureMultiCadenceSubscription produces invoices with per-month
// line items (fan-out). If the latest invoice for the seed sub has fewer line
// items than the sub-cadence / price-cadence ratio, we regressed the fan-out.
type multiCadenceInvoiceProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewMultiCadenceInvoiceProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *multiCadenceInvoiceProbe {
	return &multiCadenceInvoiceProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *multiCadenceInvoiceProbe) Name() string        { return "multi-cadence-invoice-probe" }
func (p *multiCadenceInvoiceProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *multiCadenceInvoiceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if seeds.MultiCadenceSubID == "" {
		// Seed hasn't run yet or persistent-seed disabled; soft-skip.
		return nil
	}
	subID := seeds.MultiCadenceSubID

	subResp, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		if isNotFound(err) {
			return nil // sub not yet visible; soft-skip.
		}
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID}, "get sub %s: %w", subID, err)
	}

	custID := extractSubCustomerID(subResp)
	if custID == "" {
		return nil
	}

	invResp, err := p.client.Invoices().Query(ctx, sdktypes.InvoiceFilter{CustomerID: &custID})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID, "customer_id": custID}, "query invoices: %w", err)
	}

	latest := extractLatestFullInvoiceForSub(invResp, subID)
	if latest == nil {
		// No invoice yet — soft-skip until the first cycle produces one.
		return nil
	}

	// Group line items by subscription_line_item_id and count those whose
	// [PeriodStart, PeriodEnd) span is monthly (28-31d) rather than quarterly
	// (89-92d). Requiring EACH source line item to have ≥3 monthly windows
	// catches the case where one price's fan-out regresses while another
	// price on the same invoice still produces enough total line items.
	//
	// Seed plan attaches one FIXED and one USAGE monthly price, so expect
	// at least 2 distinct source line items × 3 monthly windows = 6 total.
	const monthlySpanUpper = 45 * 24 * time.Hour
	sourceIDs := map[string]struct{}{}
	monthlyPerSource := map[string]int{}
	for _, li := range latest.LineItems {
		if li.SubscriptionLineItemID == nil {
			continue
		}
		sourceIDs[*li.SubscriptionLineItemID] = struct{}{}
		if li.PeriodStart == nil || li.PeriodEnd == nil {
			continue
		}
		span := li.PeriodEnd.Sub(*li.PeriodStart)
		if span > 24*time.Hour && span < monthlySpanUpper {
			monthlyPerSource[*li.SubscriptionLineItemID]++
		}
	}

	if len(sourceIDs) == 0 {
		return e2eprobe.Errorf(map[string]string{
			"subscription_id": subID,
			"invoice_id":      derefStr(latest.ID),
			"line_item_count": fmt.Sprintf("%d", len(latest.LineItems)),
		}, "quarterly sub %s: no line items with subscription_line_item_id on latest invoice", subID)
	}
	// Every source must have ≥3 monthly windows. A source with 0 monthly items
	// (only a single quarter-wide item, e.g.) is the exact regression this probe
	// catches — do NOT skip it just because it doesn't appear in monthlyPerSource.
	for srcID := range sourceIDs {
		count := monthlyPerSource[srcID]
		if count < 3 {
			return e2eprobe.Errorf(map[string]string{
				"subscription_id":           subID,
				"invoice_id":                derefStr(latest.ID),
				"subscription_line_item_id": srcID,
				"monthly_window_count":      fmt.Sprintf("%d", count),
			}, "quarterly sub %s: source line item %s produced only %d monthly windows (expected >=3, fan-out regression)",
				subID, srcID, count)
		}
	}
	return nil
}

// extractLatestFullInvoiceForSub returns the most recent invoice (by PeriodEnd)
// that belongs to the given subscription, as a full InvoiceResponse so callers
// can inspect LineItems. Returns nil when no matching invoices are present.
func extractLatestFullInvoiceForSub(resp interface{}, subID string) *sdktypes.InvoiceResponse {
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
	var best *sdktypes.InvoiceResponse
	for i := range items {
		inv := &items[i]
		// Require an explicit subscription-id match. A nil SubscriptionID is
		// treated as non-match: the customer-scoped query can return standalone
		// invoices (no linked sub) that would otherwise be selected as "latest".
		if inv.SubscriptionID == nil || *inv.SubscriptionID != subID {
			continue
		}
		if inv.PeriodEnd == nil {
			continue
		}
		if best == nil || inv.PeriodEnd.After(*best.PeriodEnd) {
			cp := items[i]
			best = &cp
		}
	}
	return best
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
