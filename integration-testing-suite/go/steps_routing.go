package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runRoutingSteps executes Phase 0: DB Routing Validation.
// Must run before all other phases. If the lag probe shows reader == writer,
// routing assertions in subsequent phases are disabled (functional tests continue).
func (r *SanityRunner) runRoutingSteps(ctx context.Context) {
	r.setPhase("PHASE 0: DB Routing Validation")
	r.printPhaseHeader(r.phase)

	// ── Scenario 0: Lag probe ─────────────────────────────────────────────
	lagDetected := false
	r.run("Lag probe (reader is real replica)", "POST /internal/debug/lag-probe", true, func() error {
		resp, _, err := r.raw.Post(ctx, "/internal/debug/lag-probe", nil)
		if err != nil {
			return fmt.Errorf("lag probe failed (is FLEXPRICE_DB_ROUTING_DEBUG=true on server?): %w", err)
		}
		isDistinct, _ := resp["is_distinct"].(bool)
		readerIsReplica, _ := resp["reader_is_replica"].(bool)
		writerReachable, _ := resp["writer_reachable"].(bool)

		r.lastResult().Details = fmt.Sprintf(
			"reader_is_replica=%v is_distinct=%v writer_reachable=%v",
			readerIsReplica, isDistinct, writerReachable,
		)

		if !isDistinct {
			if warning, _ := resp["warning"].(string); warning != "" {
				r.lastResult().Details += " | WARNING: " + warning
			}
			return nil
		}
		lagDetected = true
		return nil
	})

	if !lagDetected {
		fmt.Println()
		fmt.Println("  ⚠  LAG PROBE: reader and writer are the same instance.")
		fmt.Println("     Routing assertions DISABLED — functional tests continue.")
		fmt.Println("     Point the server at an Aurora cluster with a real reader endpoint")
		fmt.Println("     to enable full routing validation.")
		fmt.Println()
		r.lagProbeOK = false
	} else {
		r.lagProbeOK = true
	}

	// ── Scenario 1: Pure GET routes to replica (over-pinning guard) ───────
	r.run("Pure GET routes to replica (over-pinning guard)", "GET /customers", true, func() error {
		_, _, err := r.raw.Get(ctx, "/customers")
		if err != nil {
			return fmt.Errorf("list customers: %w", err)
		}
		r.lastResult().Details = "list customers OK (no write in this request)"
		return nil
	})
	r.assertRouting("Pure GET: no writer_pinned reads", RoutingExpectation{
		WriterPinnedMax: 0, // must be exactly 0
		ReaderMin:       1, // must use replica
	})

	// ── Scenario 2: Create customer → immediately get by external ID ──────
	extID := fmt.Sprintf("routing-test-cust-%d", ts())
	var routingCustID string

	r.run("Create customer (write)", "Customers.CreateCustomer", false, func() error {
		req := types.CreateCustomerRequest{
			ExternalID: extID,
			Name:       strPtr(fmt.Sprintf("Routing Test Customer %d", ts())),
			Email:      strPtr(fmt.Sprintf("routing-%d@test.fp", ts())),
		}
		resp, err := r.client.Customers.CreateCustomer(ctx, req)
		if err != nil {
			return err
		}
		customer := resp.CustomerResponse
		if customer == nil || customer.ID == nil {
			return fmt.Errorf("create customer returned no body")
		}
		routingCustID = *customer.ID
		r.lastResult().EntityID = *customer.ID
		return nil
	})
	r.assertRouting("Create customer: writer called", RoutingExpectation{
		WriterCallsMin: 1,
	})

	if routingCustID != "" {
		r.run("Get customer by external ID immediately (read-after-write)", "raw GET /customers", true, func() error {
			rawResp, _, err := r.raw.Get(ctx, fmt.Sprintf("/customers/external/%s", extID))
			if err != nil {
				return fmt.Errorf("get by external_id failed (stale read?): %w", err)
			}
			id, _ := rawResp["id"].(string)
			if id == "" {
				return fmt.Errorf("customer not found by external_id=%s (stale replica read?)", extID)
			}
			r.lastResult().Details = fmt.Sprintf("found id=%s by external_id", id)
			return nil
		})
		r.assertRouting("Get by external ID: writer_pinned (pin propagated to read)", RoutingExpectation{
			WriterPinnedMin: 1,
		})

		r.raw.Delete(ctx, fmt.Sprintf("/customers/%s", routingCustID)) //nolint:errcheck
	}

	// ── Scenario 3: Concurrent request pin isolation ───────────────────────
	r.run("Concurrent requests have isolated pins", "concurrent POSTs", true, func() error {
		const n = 5
		type result struct {
			extID  string
			custID string
			err    error
		}
		results := make([]result, n)
		var wg sync.WaitGroup

		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				eid := fmt.Sprintf("routing-concurrent-%d-%d", i, ts())
				req := types.CreateCustomerRequest{
					ExternalID: eid,
					Name:       strPtr(fmt.Sprintf("Concurrent Test Customer %d-%d", i, ts())),
					Email:      strPtr(fmt.Sprintf("concurrent-%d-%d@test.fp", i, ts())),
				}
				resp, err := r.client.Customers.CreateCustomer(ctx, req)
				if err != nil {
					results[i] = result{err: err}
					return
				}
				customer := resp.CustomerResponse
				var id string
				if customer != nil && customer.ID != nil {
					id = *customer.ID
				}
				results[i] = result{extID: eid, custID: id}
			}(i)
		}
		wg.Wait()

		failed := 0
		for _, res := range results {
			if res.err != nil {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d/%d concurrent creates failed", failed, n)
		}

		// Verify each customer can be read back immediately.
		for _, res := range results {
			if res.custID == "" {
				continue
			}
			rawResp, _, err := r.raw.Get(ctx, fmt.Sprintf("/customers/external/%s", res.extID))
			if err != nil {
				return fmt.Errorf("concurrent customer %s not found after create: %w", res.extID, err)
			}
			if id, _ := rawResp["id"].(string); id == "" {
				return fmt.Errorf("concurrent customer %s returned empty id", res.extID)
			}
			r.raw.Delete(ctx, fmt.Sprintf("/customers/%s", res.custID)) //nolint:errcheck
		}

		r.lastResult().Details = fmt.Sprintf("%d concurrent creates+reads all succeeded", n)
		return nil
	})
}
