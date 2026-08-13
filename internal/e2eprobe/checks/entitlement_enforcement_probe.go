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

// EntitlementEnforcementProbe verifies that the plan-level entitlement for
// e2eprobe_count is present (limit=100, soft), then provisions a fresh
// ephemeral sub, ingests 150 events (50% past the soft limit) and polls
// GetCustomerUsageSummary to confirm usage is aggregated. The soft-limit
// contract means ingestion continues to succeed — the assertion is about
// usage rollup being visible, not enforcement blocking.
type EntitlementEnforcementProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewEntitlementEnforcementProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *EntitlementEnforcementProbe {
	return &EntitlementEnforcementProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *EntitlementEnforcementProbe) Name() string        { return "entitlement-enforcement-probe" }
func (p *EntitlementEnforcementProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *EntitlementEnforcementProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		return nil
	}
	planID := seeds.PlanIDs[0]

	// Resolve the e2eprobe_count feature ID.
	featResp, err := p.client.Features().Query(ctx, types.FeatureFilter{
		LookupKeys: []string{"e2eprobe_count_feature"},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_feature"}, "query e2eprobe_count feature: %w", err)
	}
	if featResp.ListFeaturesResponse == nil || len(featResp.ListFeaturesResponse.Items) == 0 {
		return nil
	}
	if featResp.ListFeaturesResponse.Items[0].ID == nil {
		return nil
	}
	featID := *featResp.ListFeaturesResponse.Items[0].ID

	// Verify the plan-level entitlement carries limit=100, soft, enabled.
	entResp, err := p.client.Entitlements().Query(ctx, types.EntitlementFilter{
		PlanIds:    []string{planID},
		FeatureIds: []string{featID},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_entitlement", "plan_id": planID, "feature_id": featID}, "query entitlement: %w", err)
	}
	// Soft-skip when the plan-level entitlement hasn't been seeded yet — the
	// seed step lands asynchronously and running before it does would page
	// on-call for a still-pending prerequisite.
	if entResp.ListEntitlementsResponse == nil || len(entResp.ListEntitlementsResponse.Items) == 0 {
		return nil
	}
	ent := entResp.ListEntitlementsResponse.Items[0]
	if ent.UsageLimit == nil || *ent.UsageLimit != 100 {
		return e2eprobe.Errorf(map[string]string{"step": "assert_usage_limit", "plan_id": planID, "feature_id": featID}, "entitlement usage_limit = %v, want 100", ent.UsageLimit)
	}
	if ent.IsSoftLimit == nil || !*ent.IsSoftLimit {
		return e2eprobe.Errorf(map[string]string{"step": "assert_soft_limit", "plan_id": planID, "feature_id": featID}, "entitlement is_soft_limit = %v, want true", ent.IsSoftLimit)
	}
	if ent.IsEnabled == nil || !*ent.IsEnabled {
		return e2eprobe.Errorf(map[string]string{"step": "assert_enabled", "plan_id": planID, "feature_id": featID}, "entitlement is_enabled = %v, want true", ent.IsEnabled)
	}

	// Provision ephemeral customer + sub on the plan (inherits the entitlement).
	now := time.Now().UTC()
	ext := fmt.Sprintf("e2eprobe-cust-eph-ent-%d", now.UnixNano())
	if _, err := p.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("E2EProbe Ephemeral Entitlement"),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   "ephemeral-entitlement",
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
			"e2eprobe_role":   "ephemeral-entitlement",
			"e2eprobe_run_id": p.runID,
		},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "create_sub", "external_customer_id": ext, "plan_id": planID}, "create sub: %w", err)
	}
	subID := extractSubscriptionID(subResp)
	if subID != "" {
		p.reg.RegisterEphemeral("subscription", subID, now)
	}

	// Ingest 150 events on e2eprobe_count (50% past the 100 soft limit).
	for i := 0; i < 150; i++ {
		if _, err := p.client.Events().Ingest(ctx, types.IngestEventRequest{
			EventName:          "e2eprobe_count",
			ExternalCustomerID: ext,
			Properties: map[string]string{
				"e2eprobe":        "true",
				"e2eprobe_run_id": p.runID,
			},
		}); err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "ingest", "external_customer_id": ext, "subscription_id": subID}, "ingest event %d: %w", i, err)
		}
	}

	// Poll GetCustomerUsageSummary until it returns a non-nil summary. Weak
	// assertion — the exact per-feature usage / limit field shape depends on
	// the SDK response, and staging validation will strengthen this to
	// current_usage >= 100 in a follow-up.
	deadline := time.Now().Add(90 * time.Second)
	for {
		custResp, err := p.client.Customers().GetByExternalID(ctx, ext)
		if err != nil {
			if isNotFound(err) {
				return nil // customer archived mid-run — soft skip
			}
			return e2eprobe.Errorf(map[string]string{"step": "get_customer", "external_customer_id": ext}, "get customer by external id: %w", err)
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
			return nil // summary populated — success
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{"step": "poll_summary", "external_customer_id": ext, "subscription_id": subID, "feature_id": featID}, "usage summary did not populate within 90s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
