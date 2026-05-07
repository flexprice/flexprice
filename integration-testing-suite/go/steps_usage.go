package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runUsageSteps executes Phase 4: Usage Ingestion.
func (r *SanityRunner) runUsageSteps(ctx context.Context) {
	r.setPhase("PHASE 4: Usage Ingestion & Verification")
	r.printPhaseHeader(r.phase)

	if !r.require(r.externalCustID, "Create Customer", "Bulk Ingest Events") ||
		!r.require(r.eventNameA, "Feature A event name", "Bulk Ingest Events") ||
		!r.require(r.eventNameB, "Feature B event name", "Bulk Ingest Events") {
		r.skip("Wait for Processing", "depends on event ingestion")
		return
	}

	// ── Bulk Ingest 50 Events ───────────────────────────────────────────
	r.run("Bulk Ingest 50 Events", "Events.IngestEventsBulk", false, func() error {
		events := make([]types.IngestEventRequest, 0, 50)

		r.totalTokensIngested = 0
		for i := 0; i < 30; i++ {
			tokens := float64(rand.Intn(50) + 1)
			r.totalTokensIngested += tokens
			events = append(events, types.IngestEventRequest{
				EventName:          r.eventNameA,
				ExternalCustomerID: r.externalCustID,
				Properties:         map[string]string{"tokens": fmt.Sprintf("%.0f", tokens)},
				Source:             strPtr("sanity_test"),
				Timestamp:          strPtr(time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339)),
			})
		}

		r.totalGBHoursIngested = 0
		for i := 0; i < 20; i++ {
			gbHours := float64(rand.Intn(10) + 1)
			r.totalGBHoursIngested += gbHours
			events = append(events, types.IngestEventRequest{
				EventName:          r.eventNameB,
				ExternalCustomerID: r.externalCustID,
				Properties:         map[string]string{"gb_hours": fmt.Sprintf("%.0f", gbHours)},
				Source:             strPtr("sanity_test"),
				Timestamp:          strPtr(time.Now().Add(-time.Duration(i) * time.Second).Format(time.RFC3339)),
			})
		}

		_, err := r.client.Events.IngestEventsBulk(ctx, types.BulkIngestEventRequest{Events: events})
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf(
			"30 events (%.0f tokens) + 20 events (%.0f gb_hours)",
			r.totalTokensIngested, r.totalGBHoursIngested,
		)
		return nil
	})

	// ── Wait for Processing (context-aware poll, 30s max) ───────────────
	// Polls usage endpoint until any charge has quantity > 0, then proceeds.
	// No quantity assertions — consumer lag, ClickHouse delay, etc. are all valid.

	if !r.require(r.subscriptionID, "Create Subscription", "Wait for Processing") {
		return
	}

	r.run("Wait for Processing", "-", false, func() error {
		deadline := time.Now().Add(30 * time.Second)
		attempts := 0

		for {
			attempts++
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting for usage processing")
			default:
			}

			resp, err := r.client.Subscriptions.GetSubscriptionUsage(ctx, types.GetUsageBySubscriptionRequest{
				SubscriptionID: r.subscriptionID,
			})
			if err == nil && resp.GetUsageBySubscriptionResponse != nil {
				for _, c := range resp.GetUsageBySubscriptionResponse.Charges {
					if c.Quantity != nil && *c.Quantity > 0 {
						elapsed := time.Since(deadline.Add(-30 * time.Second))
						r.lastResult().Details = fmt.Sprintf("usage appeared after %s (%d poll(s))", elapsed.Round(time.Millisecond), attempts)
						return nil
					}
				}
			}

			if time.Now().After(deadline) {
				r.lastResult().Details = fmt.Sprintf("timed out after 30s (%d poll(s)), proceeding", attempts)
				return nil
			}

			time.Sleep(2 * time.Second)
		}
	})
}
