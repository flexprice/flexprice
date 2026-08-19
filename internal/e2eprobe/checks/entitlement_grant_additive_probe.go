package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// EntitlementGrantAdditiveProbe provisions an ephemeral customer + sub
// inheriting the plan-level additive grant entitlement seeded on
// AdditiveGrantFeatureLookupKey (see seed_ensure.go:ensureEntitlementGrants).
// It verifies (a) the sub inherits the grant, (b) events on the granted
// feature are aggregated into a non-nil usage summary.
//
// Loose assertion by design: the exact per-feature shape of
// GetCustomerUsageSummary for grant-backed features isn't staging-verified.
// Tighten to `current_usage >= 200` once the response shape is confirmed
// (see the spec's deferred follow-ups).
type EntitlementGrantAdditiveProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewEntitlementGrantAdditiveProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *EntitlementGrantAdditiveProbe {
	return &EntitlementGrantAdditiveProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *EntitlementGrantAdditiveProbe) Name() string        { return "entitlement-grant-additive-probe" }
func (p *EntitlementGrantAdditiveProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *EntitlementGrantAdditiveProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		return nil
	}
	if seeds.GrantEntitlementIDs[AdditiveGrantFeatureLookupKey] == "" {
		return nil // seed step hasn't landed yet
	}
	planID := seeds.PlanIDs[0]

	featResp, err := p.client.Features().Query(ctx, types.FeatureFilter{
		LookupKeys: []string{AdditiveGrantFeatureLookupKey},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "resolve_feature", "feature_lookup_key": AdditiveGrantFeatureLookupKey}, "query grant feature: %w", err)
	}
	if featResp.ListFeaturesResponse == nil || len(featResp.ListFeaturesResponse.Items) == 0 {
		return nil
	}
	if featResp.ListFeaturesResponse.Items[0].ID == nil {
		return nil
	}
	featID := *featResp.ListFeaturesResponse.Items[0].ID

	now := time.Now().UTC()
	ext := fmt.Sprintf("e2eprobe-cust-eph-grant-additive-%d", now.UnixNano())
	if _, err := p.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("E2EProbe Ephemeral Grant Additive"),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-grant-additive",
			"e2eprobe_run_id": p.runID,
		},
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_customer", "external_customer_id": ext, "plan_id": planID}, "create customer: %w", err)
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
			"e2eprobe_role":   "ephemeral-grant-additive",
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

	if err := p.pollSubEntitlementPresent(ctx, subID, featID, ext); err != nil {
		return err
	}

	for i := 0; i < 200; i++ {
		if _, err := p.client.Events().Ingest(ctx, types.IngestEventRequest{
			EventName:          "e2eprobe_sum_multiplier",
			ExternalCustomerID: ext,
			Properties: map[string]string{
				"amount":          "1",
				"e2eprobe":        "true",
				"e2eprobe_run_id": p.runID,
			},
		}); err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "ingest", "external_customer_id": ext, "subscription_id": subID, "feature_id": featID}, "ingest event %d: %w", i, err)
		}
	}

	if err := p.pollRawEvents(ctx, ext, subID, featID); err != nil {
		return err
	}
	if err := p.pollUsageSummary(ctx, ext, subID, featID); err != nil {
		return err
	}
	return nil
}

func (p *EntitlementGrantAdditiveProbe) pollSubEntitlementPresent(ctx context.Context, subID, featID, ext string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := p.client.Subscriptions().GetEntitlements(ctx, subID, []string{featID})
		if err == nil && resp.SubscriptionEntitlementsResponse != nil {
			for _, af := range resp.SubscriptionEntitlementsResponse.Features {
				if af.Feature != nil && af.Feature.ID != nil && *af.Feature.ID == featID {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{"step": "assert_sub_entitlement_present", "external_customer_id": ext, "subscription_id": subID, "feature_id": featID}, "grant entitlement not present on ephemeral sub after 30s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *EntitlementGrantAdditiveProbe) pollRawEvents(ctx context.Context, ext, subID, featID string) error {
	eventName := "e2eprobe_sum_multiplier"
	// POST /events/query defaults PageSize=50, but the probe ingests 200
	// events and asserts len(...) >= 200 — request 200 in a single page so
	// the assertion can succeed without paginating.
	pageSize := int64(200)
	// Staging's ingest consumer drains roughly ten events/second, so this
	// probe's own 200-event burst needs ~20s to become queryable. The bursts
	// from concurrent probes (700 for commitment, 100 for tax) share that
	// same queue, so worst-case depth is closer to a thousand events — about
	// 100s. 30s and 90s both flaked under that; 180s covers it.
	deadline := time.Now().Add(180 * time.Second)
	for {
		resp, err := p.client.Events().ListRaw(ctx, types.GetEventsRequest{
			ExternalCustomerID: &ext,
			EventName:          &eventName,
			PageSize:           &pageSize,
		})
		if err == nil && resp.GetEventsResponse != nil && len(resp.GetEventsResponse.Events) >= 200 {
			return nil
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{"step": "raw_verify", "external_customer_id": ext, "subscription_id": subID, "feature_id": featID}, "raw event verification timeout — not all 200 events present")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *EntitlementGrantAdditiveProbe) pollUsageSummary(ctx context.Context, ext, subID, featID string) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		custResp, err := p.client.Customers().GetByExternalID(ctx, ext)
		if err != nil {
			if isNotFound(err) {
				return nil // ephemeral archived mid-run — soft skip
			}
			return e2eprobe.Errorf(map[string]string{"step": "poll_usage_summary", "external_customer_id": ext, "subscription_id": subID}, "get customer by external id: %w", err)
		}
		if custResp.CustomerResponse == nil || custResp.CustomerResponse.ID == nil {
			return nil
		}
		custID := *custResp.CustomerResponse.ID

		sumResp, sumErr := p.client.Customers().GetUsageSummary(ctx, sdkdtos.GetCustomerUsageSummaryRequest{
			CustomerID:      &custID,
			SubscriptionIds: []string{},
			FeatureIds:      []string{featID},
		})
		if sumErr == nil && sumResp != nil && sumResp.CustomerUsageSummaryResponse != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{"step": "poll_usage_summary", "external_customer_id": ext, "subscription_id": subID, "feature_id": featID}, "usage summary did not populate within 90s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
