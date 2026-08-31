package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
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
}

func NewMultiCadenceInvoiceProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *multiCadenceInvoiceProbe {
	return &multiCadenceInvoiceProbe{client: c, reg: r, runID: runID}
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
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
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

	// Expected line item count = subMonths / priceMonths. The seed plan uses
	// monthly prices (seed_ensure.go:803, 836). Sub cadence is quarterly.
	// Expect at least 3 line items whose per-line period spans ~1 month.
	// (The plan has 2 monthly prices — one fixed, one usage — so realistic
	// count is 6; requiring >= 3 keeps the probe robust to plan-price additions.)
	if len(latest.LineItems) < 3 {
		return e2eprobe.Errorf(map[string]string{
			"subscription_id": subID,
			"invoice_id":      derefStr(latest.ID),
			"line_item_count": fmt.Sprintf("%d", len(latest.LineItems)),
		}, "quarterly sub %s: latest invoice has %d line items, expected >=3 (fan-out regression?)",
			subID, len(latest.LineItems))
	}

	// Assert at least one line item's [PeriodStart, PeriodEnd) is ~30 days,
	// NOT ~90 days — confirms per-month fan-out rather than one quarter-wide line.
	hasMonthlyLine := false
	for _, li := range latest.LineItems {
		if li.PeriodStart == nil || li.PeriodEnd == nil {
			continue
		}
		span := li.PeriodEnd.Sub(*li.PeriodStart)
		// Monthly span: 28-31 days; quarterly span: 89-92 days. Use 45 days as
		// the discriminator threshold.
		if span > 24*time.Hour && span < 45*24*time.Hour {
			hasMonthlyLine = true
			break
		}
	}
	if !hasMonthlyLine {
		return e2eprobe.Errorf(map[string]string{
			"subscription_id": subID,
			"invoice_id":      derefStr(latest.ID),
		}, "quarterly sub %s: no line item has a monthly period span (fan-out regression)", subID)
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
		if inv.SubscriptionID != nil && *inv.SubscriptionID != subID {
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
