package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// TaxApplicationProbe provisions an ephemeral customer + sub, attaches the
// shared E2EPROBE_TAX_10PCT tax rate as a fresh subscription-scoped tax
// association, ingests a deterministic amount of e2eprobe_sum usage, then
// calls GetInvoicePreview and asserts:
//   - preview.Taxes is non-empty and references the shared tax rate ID
//   - tax_amount ≈ taxable_amount × 0.10 (epsilon $0.01) on the matching entry
//
// The created tax association is best-effort-deleted at the end of the run
// (janitor's Phase-2 sweep cleans up any that slip through).
type TaxApplicationProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewTaxApplicationProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *TaxApplicationProbe {
	return &TaxApplicationProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *TaxApplicationProbe) Name() string        { return "tax-application-probe" }
func (p *TaxApplicationProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *TaxApplicationProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PlanIDs) == 0 || seeds.SharedTaxRateCode == "" {
		return nil
	}
	planID := seeds.PlanIDs[0]

	now := time.Now().UTC()
	ext := fmt.Sprintf("e2eprobe-cust-eph-tax-%d", now.UnixNano())
	if _, err := p.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("E2EProbe Ephemeral Tax"),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-tax",
			"e2eprobe_run_id": p.runID,
		},
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_customer", "external_customer_id": ext}, "create customer: %w", err)
	}
	p.reg.RegisterEphemeral("customer", ext, now)

	billingCycle := types.BillingCycleAnniversary
	subResp, err := p.client.Subscriptions().Create(ctx, types.CreateSubscriptionRequest{
		ExternalCustomerID: &ext,
		PlanID:             planID,
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: int64Ptr(1),
		BillingCycle:       &billingCycle,
		StartDate:          &now,
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-tax",
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

	// Attach the shared tax rate to this ephemeral sub.
	autoApply := true
	entityType := types.TaxRateEntityTypeSubscription
	taCreate, err := p.client.TaxAssociations().Create(ctx, types.CreateTaxAssociationRequest{
		TaxRateCode: seeds.SharedTaxRateCode,
		EntityID:    &subID,
		EntityType:  &entityType,
		AutoApply:   &autoApply,
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_role":   "ephemeral-tax",
			"e2eprobe_run_id": p.runID,
		},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_tax_assoc", "subscription_id": subID, "tax_rate_code": seeds.SharedTaxRateCode}, "create tax association: %w", err)
	}
	taID := ""
	if taCreate.TaxAssociationResponse != nil && taCreate.TaxAssociationResponse.ID != nil {
		taID = *taCreate.TaxAssociationResponse.ID
	}
	// Best-effort delete at end of run so the persistent tax_associations
	// table doesn't grow unbounded. Janitor also sweeps orphans (Task 14).
	defer func() {
		if taID == "" {
			return
		}
		if _, delErr := p.client.TaxAssociations().Delete(context.Background(), taID); delErr != nil && !isNotFound(delErr) && p.lg != nil {
			p.lg.Info(context.Background(), "tax-application-probe: delete tax association deferred (janitor will retry)",
				"tax_association_id", taID,
				"subscription_id", subID,
				"error", delErr.Error(),
			)
		}
	}()

	// Ingest 100 events × amount=1 on e2eprobe_sum → $1.00 usage.
	for i := 0; i < 100; i++ {
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

	// Poll GetUsage until aggregation catches up.
	if err := p.pollUsage(ctx, subID, ext); err != nil {
		return err
	}

	previewResp, err := p.client.Invoices().GetPreview(ctx, types.GetPreviewInvoiceRequest{SubscriptionID: subID})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "preview", "subscription_id": subID}, "get invoice preview: %w", err)
	}
	if previewResp.InvoiceResponse == nil {
		return e2eprobe.Errorf(map[string]string{"step": "assert_preview_present", "subscription_id": subID}, "empty preview response")
	}
	inv := previewResp.InvoiceResponse
	if len(inv.Taxes) == 0 {
		return e2eprobe.Errorf(map[string]string{"step": "assert_taxes_present", "subscription_id": subID, "tax_rate_code": seeds.SharedTaxRateCode}, "preview.Taxes is empty; expected at least one entry for %s", seeds.SharedTaxRateCode)
	}

	epsilon := decimal.NewFromFloat(0.01)
	found := false
	for _, tx := range inv.Taxes {
		// Prefer strong match by tax_rate_id when available; fall back to
		// nested TaxRate.Code == SharedTaxRateCode. Seeds.SharedTaxRateID is
		// empty when the SDK's broken GetTaxRates list + our create-only
		// idempotency workaround couldn't recover the ID.
		matches := tx.TaxRateID != nil && seeds.SharedTaxRateID != "" && *tx.TaxRateID == seeds.SharedTaxRateID
		if !matches && tx.TaxRate != nil && tx.TaxRate.Code != nil && *tx.TaxRate.Code == seeds.SharedTaxRateCode {
			matches = true
		}
		if !matches {
			// A different tax entry — e.g. a rate applied through a customer-level
			// association. Not ours to assert 10% math on.
			continue
		}
		found = true
		if tx.TaxableAmount != nil && tx.TaxAmount != nil {
			taxable, err1 := decimal.NewFromString(*tx.TaxableAmount)
			amt, err2 := decimal.NewFromString(*tx.TaxAmount)
			if err1 == nil && err2 == nil {
				expected := taxable.Mul(decimal.NewFromFloat(0.10))
				if amt.Sub(expected).Abs().GreaterThan(epsilon) {
					return e2eprobe.Errorf(map[string]string{
						"step": "assert_tax_math", "subscription_id": subID,
						"taxable_amount": taxable.String(), "tax_amount": amt.String(),
					}, "tax_amount %s deviates from expected %s (10%% of %s) by more than $0.01", amt, expected, taxable)
				}
			}
		}
	}
	if !found {
		return e2eprobe.Errorf(map[string]string{
			"step": "assert_tax_rate_match", "subscription_id": subID,
			"tax_rate_id": seeds.SharedTaxRateID, "tax_rate_code": seeds.SharedTaxRateCode,
		}, "preview.Taxes did not include our tax rate (checked id %q AND code %q)", seeds.SharedTaxRateID, seeds.SharedTaxRateCode)
	}
	return nil
}

func (p *TaxApplicationProbe) pollUsage(ctx context.Context, subID, ext string) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		_, err := p.client.Subscriptions().GetUsage(ctx, types.GetUsageBySubscriptionRequest{SubscriptionID: subID})
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{"step": "poll_usage", "subscription_id": subID, "external_customer_id": ext}, "sub usage poll: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
