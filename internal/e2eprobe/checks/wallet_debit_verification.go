package checks

// wallet_debit_verification.go — redesigned to be reliably testable.
//
// The original implementation pre-funded a wallet, ingested 100 × e2eprobe_sum
// events at $0.01 each, then polled for a $1 balance debit for 120s.  But
// Flexprice debits wallets at invoice-finalisation time, not per-event, so the
// poll would never see the debit within the probe's window.
//
// New design — two sequential phases, one combined Check:
//
//   Phase 1 — Direct TopUp read-after-write (synchronous, fast)
//     Read B0, TopUp by a fixed amount T, read B1, assert B1 ≥ B0+T (within
//     rounding tolerance).  Catches regressions in the wallet TopUp → GetBalance
//     synchronous flow.
//
//   Phase 2 — Event aggregation verification (eventual, up to 5m)
//     Ingest N events of e2eprobe_sum with known amount, then poll
//     GetUsageAnalytics for the customer + event name until the aggregated sum
//     ≥ expected value.  Catches regressions in the event→meter aggregation
//     pipeline without depending on async wallet-debit cycles.
//
// The check name remains "wallet-debit-verification" and the env-var prefix
// E2EPROBE_CHECK_WALLET_DEBIT_VERIFICATION_* is preserved for backwards
// compatibility with deployed configurations.

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// WalletDebitOpts controls both phases of the verification. Zero values are
// replaced by defaults in NewWalletDebitVerification.
type WalletDebitOpts struct {
	// Phase 1 — top-up amount ($)
	TopUpAmount string

	// Phase 2 — event ingestion + analytics polling
	EventCount            int
	EventAmount           string
	AnalyticsPollInterval time.Duration
	AnalyticsPollTimeout  time.Duration

	// LandedPollInterval / LandedPollTimeout bound the wait for synchronously-
	// ingested events to show up in the raw events table before the probe
	// starts polling the aggregation pipeline. Short by design — if events
	// haven't landed in 30s the ingestion path is broken, not just slow.
	LandedPollInterval time.Duration
	LandedPollTimeout  time.Duration
}

func defaultWalletDebitOpts() WalletDebitOpts {
	return WalletDebitOpts{
		TopUpAmount:           "5.00",
		EventCount:            10,
		EventAmount:           "1.00",
		AnalyticsPollInterval: 10 * time.Second,
		AnalyticsPollTimeout:  5 * time.Minute,
		LandedPollInterval:    2 * time.Second,
		LandedPollTimeout:     30 * time.Second,
	}
}

type WalletDebitVerification struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	opts   WalletDebitOpts
	cursor int64
}

func NewWalletDebitVerification(c e2eprobe.Client, r e2eprobe.Registry, runID string, opts WalletDebitOpts) *WalletDebitVerification {
	if opts.EventCount == 0 {
		opts = defaultWalletDebitOpts()
	}
	if opts.LandedPollInterval == 0 {
		opts.LandedPollInterval = 2 * time.Second
	}
	if opts.LandedPollTimeout == 0 {
		opts.LandedPollTimeout = 30 * time.Second
	}
	return &WalletDebitVerification{client: c, reg: r, runID: runID, opts: opts}
}

func (v *WalletDebitVerification) Name() string         { return "wallet-debit-verification" }
func (v *WalletDebitVerification) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (v *WalletDebitVerification) Run(ctx context.Context) error {
	seeds := v.reg.Seeds()
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&v.cursor, 1)
	customer := seeds.PreFundedCustomerIDs[int(idx)%len(seeds.PreFundedCustomerIDs)]

	walletIDs, internalCustID, err := lookupWalletIDsAndCustomerForExternalCustomer(ctx, v.client, customer)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": customer}, "lookup wallets for %s: %w", customer, err)
	}
	if len(walletIDs) == 0 {
		return nil
	}
	walletID := walletIDs[0]

	if err := v.phase1TopUp(ctx, customer, internalCustID, walletID); err != nil {
		return err
	}
	if err := v.phase2Analytics(ctx, customer); err != nil {
		return err
	}
	return nil
}

// phase1TopUp verifies that TopUp + GetBalance is consistent: balance after
// top-up must be ≥ balance before top-up + topUpAmount (within 1-cent tolerance).
func (v *WalletDebitVerification) phase1TopUp(ctx context.Context, extCustID, internalCustID, walletID string) error {
	topUpAmount, err := parseFloat(v.opts.TopUpAmount)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"wallet_id":            walletID,
		}, "parse top_up_amount: %w", err)
	}

	b0, err := v.readBalance(ctx, walletID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"internal_customer_id": internalCustID,
			"wallet_id":            walletID,
		}, "read balance before top-up: %w", err)
	}

	topUpStr := v.opts.TopUpAmount
	if _, err := v.client.Wallets().TopUp(ctx, walletID, types.TopUpWalletRequest{
		Amount:            &topUpStr,
		Description:       strPtr("e2eprobe wallet-ops-verification phase1"),
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
	}); err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"internal_customer_id": internalCustID,
			"wallet_id":            walletID,
		}, "top-up wallet: %w", err)
	}

	b1, err := v.readBalance(ctx, walletID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"internal_customer_id": internalCustID,
			"wallet_id":            walletID,
		}, "read balance after top-up: %w", err)
	}

	const tolerance = 0.01
	expected := b0 + topUpAmount
	if b1 < expected-tolerance {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"internal_customer_id": internalCustID,
			"wallet_id":            walletID,
		}, "balance after top-up incorrect: expected ≥%.4f got %.4f (b0=%.4f top_up=%.4f)",
			expected, b1, b0, topUpAmount)
	}
	return nil
}

// phase2Analytics ingests N events and polls GetUsageAnalytics until the
// aggregated sum meets or exceeds the expected total, confirming that the
// events → ClickHouse aggregation pipeline is healthy.
//
// Each event carries a unique probe-prefixed EventID so individual events can
// be grepped in Flexprice consumer logs. After ingestion, we synchronously
// query the raw events table to confirm all N landed before polling the
// aggregation pipeline — that lets failure attributes distinguish three modes:
//   - landed_count < N:                       ingestion path dropped events
//   - landed_count == N, analytics_sum == 0:  aggregation pipeline never processed them
//   - landed_count == N, analytics_sum < N:   partial aggregation drop (the
//                                             real failure mode the probe has
//                                             been catching in production)
func (v *WalletDebitVerification) phase2Analytics(ctx context.Context, extCustID string) error {
	amountPerEvent, err := parseFloat(v.opts.EventAmount)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID}, "parse event_amount: %w", err)
	}
	expectedTotal := float64(v.opts.EventCount) * amountPerEvent
	batchTag := fmt.Sprintf("%d", time.Now().UnixNano())

	eventIDs := make([]string, 0, v.opts.EventCount)
	for i := 0; i < v.opts.EventCount; i++ {
		eventID := fmt.Sprintf("e2eprobe-wdv-%s-%d", batchTag, i)
		eventIDs = append(eventIDs, eventID)
		eid := eventID
		req := types.IngestEventRequest{
			EventID:            &eid,
			EventName:          "e2eprobe_sum",
			ExternalCustomerID: extCustID,
			Properties: map[string]string{
				"amount":          v.opts.EventAmount,
				"e2eprobe":        "true",
				"e2eprobe_run_id": v.runID,
				"debit_batch":     batchTag,
			},
		}
		if _, err := v.client.Events().Ingest(ctx, req); err != nil {
			return e2eprobe.Errorf(map[string]string{
				"external_customer_id": extCustID,
				"event_name":           "e2eprobe_sum",
				"event_id":             eventID,
				"debit_batch":          batchTag,
			}, "ingest event %d: %w", i, err)
		}
	}

	ingestTime := time.Now()

	// Step 1: confirm all N events landed in the raw events table. Allow a
	// brief retry budget — the ingest API returns 2xx before the synchronous
	// write completes in some deployments, so a single ListRaw call right
	// after the loop occasionally undercounts. Retry for up to 30s.
	landedCount := v.confirmEventsLanded(ctx, extCustID, batchTag, ingestTime)
	if landedCount < v.opts.EventCount {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"event_name":           "e2eprobe_sum",
			"debit_batch":          batchTag,
			"expected_count":       fmt.Sprintf("%d", v.opts.EventCount),
			"landed_count":         fmt.Sprintf("%d", landedCount),
			"first_event_id":       eventIDs[0],
			"last_event_id":        eventIDs[len(eventIDs)-1],
		}, "ingest dropped events: only %d of %d landed in events table within 30s",
			landedCount, v.opts.EventCount)
	}

	// Step 2: poll analytics until sum ≥ expectedTotal or timeout.
	deadline := time.Now().Add(v.opts.AnalyticsPollTimeout)
	var lastSum float64
	for {
		end := time.Now().UTC()
		// Look back from the ingest time with a small buffer to ensure coverage.
		start := ingestTime.Add(-1 * time.Minute).UTC()

		resp, err := v.client.Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
			ExternalCustomerID: &extCustID,
			StartTime:          &start,
			EndTime:            &end,
		})
		if err == nil {
			lastSum = extractAnalyticsSum(resp, "e2eprobe_sum")
			if lastSum >= expectedTotal {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return e2eprobe.Errorf(map[string]string{
					"external_customer_id": extCustID,
					"debit_batch":          batchTag,
					"landed_count":         fmt.Sprintf("%d", landedCount),
				}, "analytics poll timed out: %w", err)
			}
			return e2eprobe.Errorf(map[string]string{
				"external_customer_id": extCustID,
				"debit_batch":          batchTag,
				"expected_sum":         fmt.Sprintf("%.4f", expectedTotal),
				"observed_sum":         fmt.Sprintf("%.4f", lastSum),
				"landed_count":         fmt.Sprintf("%d", landedCount),
				"first_event_id":       eventIDs[0],
				"last_event_id":        eventIDs[len(eventIDs)-1],
			}, "analytics sum did not reach expected %.4f within %s (observed %.4f, %d events landed)",
				expectedTotal, v.opts.AnalyticsPollTimeout, lastSum, landedCount)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(v.opts.AnalyticsPollInterval):
		}
	}
}

// confirmEventsLanded returns the number of events for this batch found in
// the raw events table. Polls for up to 30s to allow for ingest-side write
// lag. Errors during the query are treated as 0 landed so the caller can
// alert on ingestion regressions even when ListRaw itself is misbehaving.
func (v *WalletDebitVerification) confirmEventsLanded(ctx context.Context, extCustID, batchTag string, ingestTime time.Time) int {
	deadline := time.Now().Add(v.opts.LandedPollTimeout)
	eventName := "e2eprobe_sum"
	startTime := ingestTime.Add(-1 * time.Minute).UTC()
	var maxSeen int
	for {
		endTime := time.Now().UTC()
		pageSize := int64(v.opts.EventCount * 2)
		resp, err := v.client.Events().ListRaw(ctx, types.GetEventsRequest{
			EventName:          &eventName,
			ExternalCustomerID: &extCustID,
			PropertyFilters:    map[string][]string{"debit_batch": {batchTag}},
			StartTime:          &startTime,
			EndTime:            &endTime,
			PageSize:           &pageSize,
		})
		if err == nil && resp != nil {
			inner := resp.GetGetEventsResponse()
			if inner != nil {
				n := len(inner.Events)
				if n > maxSeen {
					maxSeen = n
				}
				if n >= v.opts.EventCount {
					return n
				}
			}
		}
		if time.Now().After(deadline) {
			return maxSeen
		}
		select {
		case <-ctx.Done():
			return maxSeen
		case <-time.After(v.opts.LandedPollInterval):
		}
	}
}

func (v *WalletDebitVerification) readBalance(ctx context.Context, walletID string) (float64, error) {
	resp, err := v.client.Wallets().GetBalance(ctx, walletID)
	if err != nil {
		return 0, err
	}
	return extractBalanceFloat(resp)
}

// extractBalanceFloat reads the numeric balance from the SDK GetWalletBalanceResponse.
// Uses the Balance field (string-encoded decimal). Returns an error if the field is
// absent or unparseable.
var extractBalanceFloat = func(resp interface{}) (float64, error) {
	r, ok := resp.(*sdkdtos.GetWalletBalanceResponse)
	if !ok || r == nil {
		return 0, fmt.Errorf("unexpected response type %T", resp)
	}
	inner := r.GetWalletBalanceResponse()
	if inner == nil || inner.Balance == nil {
		return 0, fmt.Errorf("response missing balance field")
	}
	var f float64
	if _, err := fmt.Sscanf(*inner.Balance, "%f", &f); err != nil {
		return 0, fmt.Errorf("parse balance %q: %w", *inner.Balance, err)
	}
	return f, nil
}

// extractAnalyticsSum sums TotalUsage across all items matching the given
// event name in the GetUsageAnalyticsResponse. Returns 0 when no matching
// items exist or TotalUsage is missing.
var extractAnalyticsSum = func(resp interface{}, eventName string) float64 {
	r, ok := resp.(*sdkdtos.GetUsageAnalyticsResponse)
	if !ok || r == nil {
		return 0
	}
	inner := r.GetGetUsageAnalyticsResponse()
	if inner == nil {
		return 0
	}
	var total float64
	for _, item := range inner.GetItems() {
		if item.EventName != nil && *item.EventName != eventName {
			continue
		}
		if item.TotalUsage == nil {
			continue
		}
		var f float64
		if _, err := fmt.Sscanf(*item.TotalUsage, "%f", &f); err == nil {
			total += f
		}
	}
	return total
}

func parseFloat(s string) (float64, error) {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, fmt.Errorf("parse %q as float: %w", s, err)
	}
	return f, nil
}
