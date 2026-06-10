package checks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type EntitlementAndUsageProbe struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
	cursor int64
}

func NewEntitlementAndUsageProbe(c synthetic.Client, r synthetic.Registry, runID string) *EntitlementAndUsageProbe {
	return &EntitlementAndUsageProbe{client: c, reg: r, runID: runID}
}

func (p *EntitlementAndUsageProbe) Name() string         { return "entitlement-and-usage-probe" }
func (p *EntitlementAndUsageProbe) Kind() synthetic.Kind { return synthetic.KindProbe }

func (p *EntitlementAndUsageProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) == 0 || len(seeds.PersistentSubIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customerExt := seeds.PersistentCustomerIDs[int(idx)%len(seeds.PersistentCustomerIDs)]
	subID := seeds.PersistentSubIDs[int(idx)%len(seeds.PersistentSubIDs)]

	custResp, err := p.client.Customers().GetByExternalID(ctx, customerExt)
	if err != nil {
		return fmt.Errorf("get customer %s: %w", customerExt, err)
	}
	customerID := extractCustomerID(custResp)
	if customerID == "" {
		return nil
	}

	if _, err := p.client.Customers().GetEntitlements(ctx, customerID); err != nil {
		return fmt.Errorf("customer entitlements %s: %w", customerID, err)
	}
	if _, err := p.client.Subscriptions().GetEntitlements(ctx, subID, nil); err != nil {
		return fmt.Errorf("sub entitlements %s: %w", subID, err)
	}

	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	startStr, endStr := start.Format(time.RFC3339), end.Format(time.RFC3339)
	// SDK: GetCustomerUsageSummaryRequest.CustomerID is *string, not string.
	if _, err := p.client.Customers().GetUsageSummary(ctx, dtos.GetCustomerUsageSummaryRequest{
		CustomerID: &customerID,
		SubscriptionIds: []string{},
		FeatureIds:      []string{},
		// StartTime and EndTime are not fields on GetCustomerUsageSummaryRequest;
		// they are passed via startStr/endStr only for context (unused here until Task 25).
	}); err != nil {
		_ = startStr
		_ = endStr
		return fmt.Errorf("customer usage summary %s: %w", customerID, err)
	}

	if _, err := p.client.Subscriptions().GetUsage(ctx, types.DtoGetUsageBySubscriptionRequest{
		SubscriptionID: subID,
	}); err != nil {
		return fmt.Errorf("sub usage %s: %w", subID, err)
	}
	return nil
}

// extractCustomerID reads the internal customer ID from the SDK
// GetCustomerByExternalIDResponse wrapper.
// Returns "" → probe exits early before making the 4 downstream API calls.
func extractCustomerID(resp interface{}) string {
	r, ok := resp.(*dtos.GetCustomerByExternalIDResponse)
	if !ok || r == nil {
		return ""
	}
	inner := r.GetDtoCustomerResponse()
	if inner == nil || inner.ID == nil {
		return ""
	}
	return *inner.ID
}
