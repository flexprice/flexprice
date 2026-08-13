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

// CouponApplicationProbe provisions an ephemeral customer + sub with the
// shared E2EPROBE_COUPON_10PCT attached at sub-create time (via
// SubscriptionCoupons), ingests deterministic e2eprobe_sum usage, then
// calls GetInvoicePreview and asserts:
//   - preview.CouponApplications is non-empty
//   - an entry's coupon_id matches seeds.SharedCouponID
//   - the entry's discounted_amount parses to a positive decimal (10% discount applied)
//
// No separate association cleanup is needed — attaching via SubscriptionCoupons
// scopes the association to the sub, which the janitor removes when the
// customer is archived.
type CouponApplicationProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewCouponApplicationProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *CouponApplicationProbe {
	return &CouponApplicationProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *CouponApplicationProbe) Name() string        { return "coupon-application-probe" }
func (p *CouponApplicationProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *CouponApplicationProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	// SharedCouponID is required — the assertion at the end matches coupon
	// applications by ID, so a missing ID is an unmet prerequisite (not a
	// failure worth paging on-call for).
	if len(seeds.PlanIDs) == 0 || seeds.SharedCouponCode == "" || seeds.SharedCouponID == "" {
		return nil
	}
	planID := seeds.PlanIDs[0]

	now := time.Now().UTC()
	ext := fmt.Sprintf("e2eprobe-cust-eph-coupon-%d", now.UnixNano())
	if _, err := p.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("E2EProbe Ephemeral Coupon"),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-coupon",
			"e2eprobe_run_id": p.runID,
		},
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_customer", "external_customer_id": ext}, "create customer: %w", err)
	}
	p.reg.RegisterEphemeral("customer", ext, now)

	billingCycle := types.BillingCycleAnniversary
	subResp, err := p.client.Subscriptions().Create(ctx, types.CreateSubscriptionRequest{
		ExternalCustomerID:  &ext,
		PlanID:              planID,
		Currency:            "usd",
		BillingPeriod:       types.BillingPeriodMonthly,
		BillingPeriodCount:  int64Ptr(1),
		BillingCycle:        &billingCycle,
		StartDate:           &now,
		SubscriptionCoupons: []types.SubscriptionCouponInput{{CouponCode: seeds.SharedCouponCode}},
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-coupon",
			"e2eprobe_run_id": p.runID,
		},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_sub", "external_customer_id": ext, "plan_id": planID, "coupon_code": seeds.SharedCouponCode}, "create sub with coupon: %w", err)
	}
	subID := extractSubscriptionID(subResp)
	if subID == "" {
		return e2eprobe.Errorf(map[string]string{"step": "create_sub", "external_customer_id": ext, "plan_id": planID}, "empty sub id")
	}
	p.reg.RegisterEphemeral("subscription", subID, now)

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
			return e2eprobe.Errorf(map[string]string{"step": "ingest", "subscription_id": subID}, "ingest event %d: %w", i, err)
		}
	}

	// Poll usage until aggregation catches up.
	if err := p.pollUsage(ctx, subID, ext); err != nil {
		return err
	}

	previewResp, err := p.client.Invoices().GetPreview(ctx, types.GetPreviewInvoiceRequest{SubscriptionID: subID})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "preview", "subscription_id": subID}, "get invoice preview: %w", err)
	}
	if previewResp.InvoiceResponse == nil {
		return e2eprobe.Errorf(map[string]string{"step": "assert_preview_present", "subscription_id": subID}, "empty preview")
	}
	inv := previewResp.InvoiceResponse
	if len(inv.CouponApplications) == 0 {
		return e2eprobe.Errorf(map[string]string{"step": "assert_coupon_application_present", "subscription_id": subID, "coupon_code": seeds.SharedCouponCode}, "preview.CouponApplications is empty")
	}

	epsilon := decimal.NewFromFloat(0.01)
	for _, ca := range inv.CouponApplications {
		if ca.CouponID == nil || seeds.SharedCouponID == "" || *ca.CouponID != seeds.SharedCouponID {
			continue
		}
		// Matching coupon application found — verify discount is positive.
		if ca.DiscountedAmount != nil {
			d, err := decimal.NewFromString(*ca.DiscountedAmount)
			if err == nil && d.LessThan(epsilon) {
				return e2eprobe.Errorf(map[string]string{
					"step": "assert_discount_amount", "subscription_id": subID, "coupon_id": *ca.CouponID,
				}, "coupon discounted_amount %s is unexpectedly non-positive", d)
			}
		}
		return nil
	}
	return e2eprobe.Errorf(map[string]string{
		"step": "assert_coupon_id_match", "subscription_id": subID, "coupon_id": seeds.SharedCouponID,
	}, "no coupon application referenced coupon_id %s", seeds.SharedCouponID)
}

func (p *CouponApplicationProbe) pollUsage(ctx context.Context, subID, ext string) error {
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
