//go:build e2eprobe_integration

package integration

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/temporal/models"
)

func TestE2EProbe_LocalTemporal(t *testing.T) {
	host, key := os.Getenv("E2EPROBE_API_HOST"), os.Getenv("E2EPROBE_API_KEY")
	if host == "" || key == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must both be set")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	raw := e2eprobe.NewSDKClient(host, key).Raw()
	call := func(ctx context.Context, method, path string, body, out any) {
		t.Helper()
		if err := raw.Do(ctx, method, path, body, out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	create := func(path string, body map[string]any) string {
		t.Helper()
		var created struct {
			ID string `json:"id"`
		}
		call(ctx, http.MethodPost, path, body, &created)
		if created.ID == "" {
			t.Fatalf("POST %s returned no ID", path)
		}
		return created.ID
	}
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	customerID := create("/customers", map[string]any{
		"external_id": "local-sync-" + tag, "name": "Local price sync", "skip_onboarding_workflow": true,
	})
	planID := create("/plans", map[string]any{"name": "Local price sync"})
	price := map[string]any{
		"amount": "2.00", "currency": "usd", "entity_type": "PLAN", "entity_id": planID,
		"price_unit_type": "FIAT", "billing_period": "MONTHLY", "billing_period_count": 1,
		"invoice_cadence": "ARREAR", "billing_model": "FLAT_FEE", "type": "FIXED",
		"start_date": time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339),
	}
	basePriceID := create("/prices", price)
	subscriptionID := create("/subscriptions", map[string]any{
		"customer_id": customerID, "plan_id": planID, "currency": "usd",
		"billing_period": "MONTHLY", "billing_period_count": 1, "billing_cycle": "anniversary",
	})
	meterID := create("/meters", map[string]any{
		"name": "Local price sync meter", "event_name": "local_sync_" + tag,
		"aggregation": map[string]string{"type": "COUNT"}, "reset_usage": "BILLING_PERIOD",
	})
	meteredPrice := maps.Clone(price)
	meteredPrice["type"] = "USAGE"
	meteredPrice["meter_id"] = meterID
	meteredPriceID := create("/prices", meteredPrice)

	linePrices := func() []string {
		t.Helper()
		var subscription struct {
			LineItems []struct {
				PriceID string `json:"price_id"`
			} `json:"line_items"`
		}
		call(ctx, http.MethodGet, "/subscriptions/"+subscriptionID, nil, &subscription)
		prices := make([]string, 0, len(subscription.LineItems))
		for _, item := range subscription.LineItems {
			prices = append(prices, item.PriceID)
		}
		return prices
	}
	before := linePrices()
	if !slices.Contains(before, basePriceID) || slices.Contains(before, meteredPriceID) {
		t.Fatal("expected the fixed price and no metered price before sync")
	}

	var run models.TemporalWorkflowResult
	call(ctx, http.MethodPost, "/plans/"+planID+"/sync/subscriptions", map[string]any{}, &run)
	if run.WorkflowID == "" || run.RunID == "" {
		t.Fatal("price sync trigger did not return a workflow ID and run ID")
	}
	t.Logf("Triggered plan price sync (run %s)", run.RunID)

	waitForPriceSync(t, ctx, raw, run.WorkflowID, run.RunID)

	var summary dto.WorkflowSummaryResponse
	call(ctx, http.MethodGet, "/workflows/"+run.WorkflowID+"/"+run.RunID+"/summary", nil, &summary)
	if summary.Status != "Completed" || summary.ActivityCount == 0 || summary.FailedActivities != 0 {
		t.Fatalf("unexpected workflow summary: status=%s activities=%d failed=%d",
			summary.Status, summary.ActivityCount, summary.FailedActivities)
	}
	if !slices.Contains(linePrices(), meteredPriceID) {
		t.Fatal("completed price sync did not attach the metered price")
	}
	t.Log("Completed price sync attached the metered price to the subscription")
}

func waitForPriceSync(t *testing.T, ctx context.Context, raw e2eprobe.RawOps, workflowID, runID string) {
	t.Helper()
	poll, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	filter := map[string]any{"workflow_id": workflowID, "limit": 10}
	status := "absent"
	for {
		var workflows dto.ListWorkflowsResponse
		if err := raw.Do(poll, http.MethodPost, "/workflows/search", filter, &workflows); err != nil {
			if poll.Err() != nil {
				t.Fatalf("plan price sync last seen as %s before poll deadline", status)
			}
			t.Fatalf("search for plan price sync: %v", err)
		}
		for _, row := range workflows.Items {
			if row == nil || row.RunID != runID {
				continue
			}
			status = row.Status
			switch status {
			case "Completed":
				if row.WorkflowType != "PriceSyncV2Workflow" {
					t.Fatalf("unexpected workflow type %q", row.WorkflowType)
				}
				return
			case "Running":
			default:
				t.Fatalf("plan price sync ended with status %q", status)
			}
		}
		select {
		case <-poll.Done():
			t.Fatalf("plan price sync last seen as %s before poll deadline", status)
		case <-ticker.C:
		}
	}
}
