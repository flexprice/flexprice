package checks

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type EntitlementAndUsageProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	cursor int64
}

func NewEntitlementAndUsageProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *EntitlementAndUsageProbe {
	return &EntitlementAndUsageProbe{client: c, reg: r, runID: runID}
}

func (p *EntitlementAndUsageProbe) Name() string         { return "entitlement-and-usage-probe" }
func (p *EntitlementAndUsageProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *EntitlementAndUsageProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	// Only iterate customers that receive ingest traffic; the canary has no
	// entitlement usage to check.
	customers := seeds.IngestCustomerIDs
	if len(customers) == 0 {
		customers = seeds.PersistentCustomerIDs
	}
	if len(customers) == 0 || len(seeds.PersistentSubIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customerExt := customers[int(idx)%len(customers)]
	subID := seeds.PersistentSubIDs[int(idx)%len(seeds.PersistentSubIDs)]

	custResp, err := p.client.Customers().GetByExternalID(ctx, customerExt)
	if err != nil {
		// 404 → customer not provisioned yet (first-run race); soft-skip.
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customerExt, "subscription_id": subID}, "get customer %s: %w", customerExt, err)
	}
	customerID := extractCustomerID(custResp)
	if customerID == "" {
		return nil
	}

	if _, err := p.client.Customers().GetEntitlements(ctx, customerID); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customerExt, "internal_customer_id": customerID, "subscription_id": subID}, "customer entitlements %s: %w", customerID, err)
	}
	if _, err := p.client.Subscriptions().GetEntitlements(ctx, subID, nil); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customerExt, "internal_customer_id": customerID, "subscription_id": subID}, "sub entitlements %s: %w", subID, err)
	}

	// SDK: GetCustomerUsageSummaryRequest.CustomerID is *string, not string.
	// Note: StartTime and EndTime are not fields on GetCustomerUsageSummaryRequest.
	if _, err := p.client.Customers().GetUsageSummary(ctx, dtos.GetCustomerUsageSummaryRequest{
		CustomerID:      &customerID,
		SubscriptionIds: []string{},
		FeatureIds:      []string{},
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customerExt, "internal_customer_id": customerID, "subscription_id": subID}, "customer usage summary %s: %w", customerID, err)
	}

	if _, err := p.client.Subscriptions().GetUsage(ctx, types.GetUsageBySubscriptionRequest{
		SubscriptionID: subID,
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customerExt, "internal_customer_id": customerID, "subscription_id": subID}, "sub usage %s: %w", subID, err)
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
	inner := r.GetCustomerResponse()
	if inner == nil || inner.ID == nil {
		return ""
	}
	return *inner.ID
}
