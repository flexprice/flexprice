package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type NewCustomerLifecyclePoll struct {
	Timeout  time.Duration
	Interval time.Duration
}

type NewCustomerLifecycleOpts struct {
	MaxEphemerals int
	AnalyticsPoll NewCustomerLifecyclePoll
}

func defaultLifecycleOpts() NewCustomerLifecycleOpts {
	return NewCustomerLifecycleOpts{
		MaxEphemerals: 20,
		AnalyticsPoll: NewCustomerLifecyclePoll{Timeout: 90 * time.Second, Interval: 5 * time.Second},
	}
}

type NewCustomerLifecycle struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
	opts   NewCustomerLifecycleOpts
}

func NewNewCustomerLifecycle(c synthetic.Client, r synthetic.Registry, runID string, opts NewCustomerLifecycleOpts) *NewCustomerLifecycle {
	if opts.MaxEphemerals == 0 {
		opts = defaultLifecycleOpts()
	}
	return &NewCustomerLifecycle{client: c, reg: r, runID: runID, opts: opts}
}

func (s *NewCustomerLifecycle) Name() string         { return "new-customer-lifecycle" }
func (s *NewCustomerLifecycle) Kind() synthetic.Kind { return synthetic.KindScenario }

func (s *NewCustomerLifecycle) Run(ctx context.Context) error {
	if len(s.reg.Ephemerals("customer")) >= s.opts.MaxEphemerals {
		return nil
	}
	seeds := s.reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		return nil
	}
	planID := seeds.PlanIDs[0]
	now := time.Now().UTC()
	ext := fmt.Sprintf("synthetic-cust-eph-%d", now.UnixNano())

	if _, err := s.client.Customers().Create(ctx, types.DtoCreateCustomerRequest{
		ExternalID: ext,
		Name:       strPtr("Synthetic Ephemeral"),
		Metadata: map[string]string{
			"synthetic":        "true",
			"synthetic_cohort": "ephemeral",
			"synthetic_role":   "ephemeral",
			"synthetic_run_id": s.runID,
		},
	}); err != nil {
		return fmt.Errorf("create customer: %w", err)
	}
	s.reg.RegisterEphemeral("customer", ext, now)

	subResp, err := s.client.Subscriptions().Create(ctx, types.DtoCreateSubscriptionRequest{
		ExternalCustomerID: &ext,
		PlanID:             planID,
		Metadata: map[string]string{
			"synthetic":        "true",
			"synthetic_cohort": "ephemeral",
			"synthetic_run_id": s.runID,
		},
	})
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	subID := extractSubscriptionID(subResp)
	if subID == "" {
		return fmt.Errorf("subscription create returned empty ID for %s", ext)
	}
	s.reg.RegisterEphemeral("subscription", subID, now)

	for i := 0; i < 3; i++ {
		if _, err := s.client.Events().Ingest(ctx, types.DtoIngestEventRequest{
			EventName:          "synthetic_count",
			ExternalCustomerID: ext,
			Properties: map[string]string{
				"synthetic":        "true",
				"synthetic_run_id": s.runID,
			},
		}); err != nil {
			return fmt.Errorf("ingest event %d: %w", i, err)
		}
	}

	if err := s.pollAnalytics(ctx, ext); err != nil {
		return err
	}
	return nil
}

func (s *NewCustomerLifecycle) pollAnalytics(ctx context.Context, ext string) error {
	deadline := time.Now().Add(s.opts.AnalyticsPoll.Timeout)
	for {
		end := time.Now().UTC()
		start := end.Add(-1 * time.Hour)
		startStr, endStr := start.Format(time.RFC3339), end.Format(time.RFC3339)
		// NOTE: ExternalCustomerID is string (not *string) in v2.0.16
		_, err := s.client.Events().GetUsageAnalytics(ctx, types.DtoGetUsageAnalyticsRequest{
			ExternalCustomerID: ext,
			StartTime:          &startStr,
			EndTime:            &endStr,
		})
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("analytics timeout %s after %s: %w", ext, s.opts.AnalyticsPoll.Timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.opts.AnalyticsPoll.Interval):
		}
	}
}

// extractSubscriptionID is a package-level variable so tests can swap it.
// Task 25 replaces this default with a real SDK response getter.
var extractSubscriptionID = func(_ interface{}) string { return "" }
