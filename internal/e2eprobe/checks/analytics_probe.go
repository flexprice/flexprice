package checks

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type AnalyticsProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	logger *logger.Logger
	cursor int64
}

func NewAnalyticsProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *AnalyticsProbe {
	return &AnalyticsProbe{client: c, reg: r, runID: runID, logger: lg}
}

func (p *AnalyticsProbe) Name() string        { return "analytics-probe" }
func (p *AnalyticsProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

var analyticsWindows = []time.Duration{1 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

func (p *AnalyticsProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	// Only poll customers that actually receive ingest traffic — the canary
	// is deliberately isolated and would report zero usage otherwise.
	customers := seeds.IngestCustomerIDs
	if len(customers) == 0 {
		customers = seeds.PersistentCustomerIDs
	}
	if len(customers) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customer := customers[int(idx)%len(customers)]
	window := analyticsWindows[int(idx)%len(analyticsWindows)]
	end := time.Now().UTC()
	start := end.Add(-window)

	req := types.GetUsageAnalyticsRequest{
		ExternalCustomerID: &customer,
		StartTime:          &start,
		EndTime:            &end,
	}
	resp, err := p.client.Events().GetUsageAnalytics(ctx, req)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": customer,
			"window":               window.String(),
		}, "analytics %s (window=%s): %w", customer, window, err)
	}

	itemCount := 0
	if resp != nil {
		if inner := resp.GetGetUsageAnalyticsResponse(); inner != nil {
			itemCount = len(inner.GetItems())
		}
	}
	p.logDebug(ctx, "analytics-probe: usage analytics fetched ok",
		"external_customer_id", customer,
		"window", window.String(),
		"item_count", itemCount,
		"run_id", p.runID)
	return nil
}

func (p *AnalyticsProbe) logDebug(ctx context.Context, msg string, kv ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Debug(ctx, msg, kv...)
}
