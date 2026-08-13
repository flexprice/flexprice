package checks

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// CommitmentTrueUpProbe creates a fresh ephemeral customer + sub with a
// $5/mo commitment (1.5× overage), ingests a deterministic amount of usage
// on e2eprobe_sum ($0.01/unit), then calls GetInvoicePreview and asserts
// the resulting math. The plan carries a $19.99 monthly fixed base fee
// (`e2eprobe_base_price` in seed_ensure.go); expected preview totals below
// bake that in.
//
// Alternates legs per run:
//   - Odd runs (cursor%2 == 1): under-commitment (100 events × $0.01 = $1.00).
//     Preview total must be at least commitment + base ($5 + $19.99 = $24.99).
//     True-up bumps usage up to the commitment.
//   - Even runs (cursor%2 == 0): over-commitment (700 events × $0.01 = $7.00).
//     Preview total must equal commitment + (usage - commitment) × 1.5 + base
//     = $5 + $2 × 1.5 + $19.99 = $27.99 (epsilon $0.01).
type CommitmentTrueUpProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
	cursor int64
}

func NewCommitmentTrueUpProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *CommitmentTrueUpProbe {
	return &CommitmentTrueUpProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *CommitmentTrueUpProbe) Name() string        { return "commitment-true-up-probe" }
func (p *CommitmentTrueUpProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *CommitmentTrueUpProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		return nil
	}
	planID := seeds.PlanIDs[0]

	idx := atomic.AddInt64(&p.cursor, 1)
	overLeg := idx%2 == 0

	now := time.Now().UTC()
	ext := fmt.Sprintf("e2eprobe-cust-eph-commit-%d", now.UnixNano())
	if _, err := p.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("E2EProbe Ephemeral Commitment"),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-commitment",
			"e2eprobe_run_id": p.runID,
		},
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_customer", "external_customer_id": ext, "plan_id": planID}, "create customer: %w", err)
	}
	p.reg.RegisterEphemeral("customer", ext, now)

	commitAmount := "5.00"
	commitDuration := types.BillingPeriodMonthly
	overageFactor := "1.5"
	billingCycle := types.BillingCycleAnniversary
	subResp, err := p.client.Subscriptions().Create(ctx, types.CreateSubscriptionRequest{
		ExternalCustomerID: &ext,
		PlanID:             planID,
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: int64Ptr(1),
		BillingCycle:       &billingCycle,
		StartDate:          &now,
		CommitmentAmount:   &commitAmount,
		CommitmentDuration: &commitDuration,
		OverageFactor:      &overageFactor,
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-commitment",
			"e2eprobe_run_id": p.runID,
		},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_sub", "external_customer_id": ext, "plan_id": planID}, "create sub: %w", err)
	}
	subID := extractSubscriptionID(subResp)
	if subID == "" {
		return e2eprobe.Errorf(map[string]string{"step": "create_sub", "external_customer_id": ext, "plan_id": planID}, "empty sub id")
	}
	p.reg.RegisterEphemeral("subscription", subID, now)

	// Ingest e2eprobe_sum events (priced at $0.01/unit); amount=1 per event.
	n := 100
	expectedUsage := 1.00
	if overLeg {
		n = 700
		expectedUsage = 7.00
	}
	for i := 0; i < n; i++ {
		if _, err := p.client.Events().Ingest(ctx, types.IngestEventRequest{
			EventName:          "e2eprobe_sum",
			ExternalCustomerID: ext,
			Properties: map[string]string{
				"amount":          "1",
				"e2eprobe":        "true",
				"e2eprobe_run_id": p.runID,
			},
		}); err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "ingest", "external_customer_id": ext, "subscription_id": subID}, "ingest event %d: %w", i, err)
		}
	}

	// Poll GetUsage until aggregation reflects the ingested amount. A successful
	// response can still report zero usage while the pipeline catches up.
	if err := p.pollSubUsage(ctx, subID, ext, expectedUsage); err != nil {
		return err
	}

	previewResp, err := p.client.Invoices().GetPreview(ctx, types.GetPreviewInvoiceRequest{SubscriptionID: subID})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "preview", "external_customer_id": ext, "subscription_id": subID}, "get invoice preview: %w", err)
	}
	if previewResp.InvoiceResponse == nil {
		return e2eprobe.Errorf(map[string]string{"step": "assert_preview_present", "external_customer_id": ext, "subscription_id": subID}, "empty preview response")
	}
	return p.assertPreview(previewResp.InvoiceResponse, overLeg, ext, subID)
}

func (p *CommitmentTrueUpProbe) pollSubUsage(ctx context.Context, subID, ext string, expectedAmount float64) error {
	deadline := time.Now().Add(90 * time.Second)
	usageEpsilon := 0.005
	var lastErr error
	var lastAmount float64
	for {
		resp, err := p.client.Subscriptions().GetUsage(ctx, types.GetUsageBySubscriptionRequest{SubscriptionID: subID})
		if err == nil && resp != nil && resp.GetUsageBySubscriptionResponse != nil && resp.GetUsageBySubscriptionResponse.Amount != nil {
			lastAmount = *resp.GetUsageBySubscriptionResponse.Amount
			if lastAmount+usageEpsilon >= expectedAmount {
				return nil
			}
		} else if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			fields := map[string]string{
				"step":                 "poll_sub_usage",
				"external_customer_id": ext,
				"subscription_id":      subID,
				"expected_amount":      fmt.Sprintf("%.2f", expectedAmount),
				"last_amount":          fmt.Sprintf("%.2f", lastAmount),
			}
			if lastErr != nil {
				return e2eprobe.Errorf(fields, "sub usage did not reach expected amount %.2f (last=%.2f): %w", expectedAmount, lastAmount, lastErr)
			}
			return e2eprobe.Errorf(fields, "sub usage did not reach expected amount %.2f (last=%.2f)", expectedAmount, lastAmount)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (p *CommitmentTrueUpProbe) assertPreview(inv *types.InvoiceResponse, overLeg bool, ext, subID string) error {
	commitment := decimal.NewFromFloat(5.00)
	// e2eprobe_base_price ($19.99 monthly fixed) is on seeds.PlanIDs[0] and
	// always appears on the preview, so bake it into the expected totals.
	baseFee := decimal.NewFromFloat(19.99)
	epsilon := decimal.NewFromFloat(0.01)

	total := invoiceTotal(inv)

	if !overLeg {
		// Under leg: total must be at least commitment + base fee (true-up bumps up).
		expectedMin := commitment.Add(baseFee)
		if total.LessThan(expectedMin.Sub(epsilon)) {
			return e2eprobe.Errorf(map[string]string{
				"step": "assert_commitment_under", "external_customer_id": ext, "subscription_id": subID,
				"preview_total": total.String(), "expected_min": expectedMin.String(),
			}, "under-leg preview total %s < commitment+base %s; expected true-up to bring total to at least %s", total, expectedMin, expectedMin)
		}
		if !hasCommitmentTrueUpLine(inv) {
			return e2eprobe.Errorf(map[string]string{
				"step": "assert_true_up_line", "external_customer_id": ext, "subscription_id": subID,
				"preview_total": total.String(),
			}, "preview total OK but no identifiable commitment true-up line item found")
		}
		return nil
	}

	// Over leg: total ≈ commitment + (usage_past_commitment × 1.5) + base
	//         = $5 + $2×1.5 + $19.99 = $27.99.
	expected := decimal.NewFromFloat(8.00).Add(baseFee)
	if total.Sub(expected).Abs().GreaterThan(epsilon) {
		return e2eprobe.Errorf(map[string]string{
			"step": "assert_commitment_over", "external_customer_id": ext, "subscription_id": subID,
			"preview_total": total.String(), "expected": expected.String(),
		}, "over-leg preview total %s differs from expected %s by more than %s", total, expected, epsilon)
	}
	return nil
}

// invoiceTotal reads the invoice total, preferring the Total field and
// falling back to AmountDue. Returns zero decimal on both nil / parse failure.
func invoiceTotal(inv *types.InvoiceResponse) decimal.Decimal {
	pick := inv.Total
	if pick == nil {
		pick = inv.AmountDue
	}
	if pick == nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(*pick)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// hasCommitmentTrueUpLine scans the invoice line items for anything that
// looks like a commitment true-up marker. The exact response shape is
// stabilised over multiple SDK versions; the probe accepts either:
//   - metadata["kind"] == "commitment_true_up"
//   - display name contains "commitment" or "true-up"/"true up"
//
// If neither matches, the caller reports the full preview so on-call can
// adapt the matcher.
func hasCommitmentTrueUpLine(inv *types.InvoiceResponse) bool {
	for _, li := range inv.LineItems {
		if li.Metadata != nil {
			if v, ok := li.Metadata["kind"]; ok && v == "commitment_true_up" {
				return true
			}
		}
		if li.DisplayName != nil {
			lower := strings.ToLower(*li.DisplayName)
			if strings.Contains(lower, "commitment") || strings.Contains(lower, "true-up") || strings.Contains(lower, "true up") {
				return true
			}
		}
	}
	return false
}
