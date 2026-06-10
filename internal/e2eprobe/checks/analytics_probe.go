package checks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type AnalyticsProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	cursor int64
}

func NewAnalyticsProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *AnalyticsProbe {
	return &AnalyticsProbe{client: c, reg: r, runID: runID}
}

func (p *AnalyticsProbe) Name() string         { return "analytics-probe" }
func (p *AnalyticsProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

var analyticsWindows = []time.Duration{1 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

func (p *AnalyticsProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customer := seeds.PersistentCustomerIDs[int(idx)%len(seeds.PersistentCustomerIDs)]
	window := analyticsWindows[int(idx)%len(analyticsWindows)]
	end := time.Now().UTC()
	start := end.Add(-window)
	startStr, endStr := start.Format(time.RFC3339), end.Format(time.RFC3339)

	req := types.DtoGetUsageAnalyticsRequest{
		ExternalCustomerID: customer,
		StartTime:          &startStr,
		EndTime:            &endStr,
	}
	if _, err := p.client.Events().GetUsageAnalytics(ctx, req); err != nil {
		return fmt.Errorf("analytics %s (window=%s): %w", customer, window, err)
	}
	return nil
}
