package checks

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// LowBalanceAlertProbe drives the canary wallet across its low-balance
// alert threshold and asserts the corresponding webhook lands within a
// bounded window. Missing webhook → check error → Slack via reporter.
//
// Flexprice's wallet-alert webhook mapping only fires for the in_alarm and
// ok states of AlertTypeLowOngoingBalance (see internal/ee/service/alertlogs.go
// `alertWebhookMapping`). Landing on info or warning updates the wallet's
// alert_state in the DB but produces no webhook. To trigger the drop event
// we must therefore cross the CRITICAL threshold (0), not just info (25).
//
// The canary wallet's ongoing_balance can drift arbitrarily between ticks —
// prior in-flight events, manual top-ups by operators, other pipelines. So
// the drop leg reads current real_time_balance and computes the ingest
// units needed to push balance below critical with a small buffer, instead
// of a fixed constant. See computeDriveUnits.
//
// Cycle:
//
//	Tick T0 (wallet=ok):    ingest usage large enough to push ongoing_balance
//	                        below the critical threshold (0) → wait for the
//	                        wallet.ongoing_balance.dropped webhook.
//	Tick T1 (wallet=in_alarm): top-up the wallet back to $30 to recover.
//
// One end-to-end verification per two ticks. At default 5m interval, full
// cycle every 10m.
type LowBalanceAlertProbe struct {
	client   e2eprobe.Client
	reg      e2eprobe.Registry
	listener *LowWalletAlertListener
	runID    string
	opts     LowBalanceAlertOpts
	logger   *logger.Logger
}

// LowBalanceAlertOpts are runtime knobs. Zero-value falls through to sane
// defaults set by NewLowBalanceAlertProbe.
type LowBalanceAlertOpts struct {
	// MinUsageUnits is the floor on units per drop event. At $0.01/unit this
	// is $35 by default. The actual amount ingested is
	//   max(MinUsageUnits, ceil(real_time_balance × 100) + DropBufferUnits),
	// so on a "cold" canary at initial-balance $30 we ingest MinUsageUnits
	// (~$35, below critical=0), and on a drifted wallet at $657.7 we ingest
	// ~65 970 units (~$659.70) to still cross critical.
	MinUsageUnits int

	// DropBufferUnits is added on top of the units needed to reach the
	// critical threshold, so a tiny post-fetch usage bump on the server side
	// can't leave us landing exactly on the threshold. $2 buffer default.
	DropBufferUnits int

	// MaxUsageUnits is a safety cap so a corrupted balance read (say a very
	// large positive number) can't have us ingest millions of dollars of
	// usage. Default 10_000_000 = $100_000 max per drop. Well above any
	// realistic canary balance.
	MaxUsageUnits int

	// RecoveryTopUp is the credit re-added when the wallet is found in_alarm.
	// Default matches AlertCanaryInitialBalance ($30).
	RecoveryTopUp string

	// WebhookWait bounds how long we poll SeenThresholds after ingesting
	// the drop event before we Slack-page. Default 2m — comfortably longer
	// than typical Kafka + Svix/native-HTTP propagation so a slow-but-alive
	// pipeline does not false-alert, but short enough to fit inside the 5m
	// tick interval and surface real regressions the same day.
	WebhookWait time.Duration

	// PollInterval controls SeenThresholds polling cadence during WebhookWait.
	// Default 2s.
	PollInterval time.Duration
}

func NewLowBalanceAlertProbe(c e2eprobe.Client, r e2eprobe.Registry, listener *LowWalletAlertListener, runID string, lg *logger.Logger, opts LowBalanceAlertOpts) *LowBalanceAlertProbe {
	if opts.MinUsageUnits == 0 {
		opts.MinUsageUnits = 3500
	}
	if opts.DropBufferUnits == 0 {
		opts.DropBufferUnits = 200
	}
	if opts.MaxUsageUnits == 0 {
		opts.MaxUsageUnits = 10_000_000
	}
	if opts.RecoveryTopUp == "" {
		opts.RecoveryTopUp = AlertCanaryInitialBalance
	}
	if opts.WebhookWait == 0 {
		opts.WebhookWait = 2 * time.Minute
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}
	return &LowBalanceAlertProbe{client: c, reg: r, listener: listener, runID: runID, logger: lg, opts: opts}
}

func (p *LowBalanceAlertProbe) Name() string        { return "low-balance-alert-probe" }
func (p *LowBalanceAlertProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *LowBalanceAlertProbe) Run(ctx context.Context) error {
	ext := p.reg.Seeds().AlertCanaryExternalCustomerID
	if ext == "" {
		return nil // seed hasn't completed yet
	}

	walletIDs, _, err := lookupWalletIDsAndCustomerForExternalCustomer(ctx, p.client, ext)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": ext}, "lookup canary wallet: %w", err)
	}
	if len(walletIDs) == 0 {
		return nil // wallet not yet provisioned
	}
	walletID := walletIDs[0]

	balResp, err := p.client.Wallets().GetBalance(ctx, walletID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": ext, "wallet_id": walletID,
		}, "read canary balance: %w", err)
	}
	if balResp == nil || balResp.WalletBalanceResponse == nil {
		return nil
	}
	b := balResp.WalletBalanceResponse

	state := "unknown"
	if b.AlertState != nil {
		state = string(*b.AlertState)
	}
	rtBal := ""
	if b.RealTimeBalance != nil {
		rtBal = *b.RealTimeBalance
	}
	attrs := map[string]string{
		"external_customer_id": ext,
		"wallet_id":            walletID,
		"alert_state":          state,
	}
	if rtBal != "" {
		attrs["real_time_balance"] = rtBal
	}

	p.logDebug(ctx, "low-balance-alert-probe: fetched wallet balance",
		"wallet_id", walletID, "alert_state", state, "real_time_balance", rtBal,
		"external_customer_id", ext, "run_id", p.runID)

	if state != "ok" {
		// Recovery leg: top-up so the state machine can re-arm for the next
		// alert cycle. The recovery webhook (wallet.updated) is not asserted
		// here — this check owns the drop → alert leg.
		return p.recover(ctx, walletID, ext, attrs)
	}

	// Drop leg: ingest usage to push ongoing_balance below the critical threshold,
	// then wait for the wallet.ongoing_balance.dropped webhook.
	units := p.computeDriveUnits(rtBal)
	return p.driveAndVerify(ctx, walletID, ext, units, attrs)
}

// computeDriveUnits returns the number of units to ingest so real_time_balance
// (in dollars) crosses below the critical threshold (0) with DropBufferUnits
// of headroom. Falls back to MinUsageUnits when the balance can't be parsed
// or is already at/below zero.
func (p *LowBalanceAlertProbe) computeDriveUnits(realTimeBalanceUSD string) int {
	trimmed := strings.TrimSpace(realTimeBalanceUSD)
	if trimmed == "" {
		return p.opts.MinUsageUnits
	}
	bal, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return p.opts.MinUsageUnits
	}
	if bal <= 0 {
		// Balance already ≤ critical yet state reads "ok" — a state desync
		// we can't fix here. Ingest the minimum so the alert pipeline still
		// gets a fresh event to re-evaluate against, but don't blow up the
		// event's dollar value.
		return p.opts.MinUsageUnits
	}
	// At $0.01/unit, dollars × 100 = units.
	needed := int(math.Ceil(bal*100)) + p.opts.DropBufferUnits
	if needed < p.opts.MinUsageUnits {
		return p.opts.MinUsageUnits
	}
	if needed > p.opts.MaxUsageUnits {
		return p.opts.MaxUsageUnits
	}
	return needed
}

func (p *LowBalanceAlertProbe) driveAndVerify(ctx context.Context, walletID, ext string, units int, attrs map[string]string) error {
	// Baseline the newest receipt timestamp per alert_type before ingest.
	// SeenThresholds is keyed by alert_type so len() doesn't grow on repeat
	// cycles — a re-fired "low_ongoing_balance" overwrites the same key.
	// We must compare timestamps to detect a fresh delivery.
	baseline := maxReceipt(p.listener.SeenThresholds(walletID))

	amountStr := strconv.Itoa(units)
	ingestReq := types.IngestEventRequest{
		EventName:          "e2eprobe_sum",
		ExternalCustomerID: ext,
		Properties: map[string]string{
			"amount":          amountStr,
			"e2eprobe":        "true",
			"e2eprobe_role":   "alert-canary",
			"e2eprobe_run_id": p.runID,
		},
	}
	if _, err := p.client.Events().Ingest(ctx, ingestReq); err != nil {
		return e2eprobe.Errorf(attrs, "ingest canary drop event: %w", err)
	}
	p.logDebug(ctx, "low-balance-alert-probe: ingested drop event",
		"wallet_id", walletID, "amount_units", amountStr,
		"expected_debit_usd", decimalDollars(units),
		"baseline_receipt", baseline.Format(time.RFC3339Nano),
		"deadline_sec", int(p.opts.WebhookWait.Seconds()), "run_id", p.runID)

	deadline := time.Now().Add(p.opts.WebhookWait)
	for {
		if newest := maxReceipt(p.listener.SeenThresholds(walletID)); newest.After(baseline) {
			p.logDebug(ctx, "low-balance-alert-probe: webhook received within deadline",
				"wallet_id", walletID,
				"webhook_at", newest.Format(time.RFC3339Nano),
				"elapsed_ms", time.Since(deadline.Add(-p.opts.WebhookWait)).Milliseconds(),
				"run_id", p.runID)
			return nil
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(attrs,
				"no low-balance webhook received within %s after ingesting $%s (rate=$0.01/unit) on wallet %s",
				p.opts.WebhookWait, decimalDollars(units), walletID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.opts.PollInterval):
		}
	}
}

// maxReceipt returns the newest timestamp in a SeenThresholds map, or the
// zero time when the map is empty. Zero baseline is fine: any real receipt
// After(zero) resolves to true on the first cycle.
func maxReceipt(seen map[string]time.Time) time.Time {
	var newest time.Time
	for _, t := range seen {
		if t.After(newest) {
			newest = t
		}
	}
	return newest
}

func (p *LowBalanceAlertProbe) recover(ctx context.Context, walletID, ext string, attrs map[string]string) error {
	topUpReq := types.TopUpWalletRequest{
		Amount:            strPtr(p.opts.RecoveryTopUp),
		Description:       strPtr("e2eprobe alert canary recovery top-up"),
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
	}
	if _, err := p.client.Wallets().TopUp(ctx, walletID, topUpReq); err != nil {
		return e2eprobe.Errorf(attrs, "recovery top-up of canary wallet %s: %w", walletID, err)
	}
	p.logDebug(ctx, "low-balance-alert-probe: recovery top-up applied",
		"wallet_id", walletID, "amount_usd", p.opts.RecoveryTopUp,
		"external_customer_id", ext, "run_id", p.runID)
	return nil
}

// logDebug is a nil-safe wrapper so tests / stub call-sites without a logger
// don't panic. Emits at Debug level so probes stay quiet on default (Info)
// runs; flip E2EPROBE_LOG_LEVEL=debug to surface the per-tick checkpoints.
func (p *LowBalanceAlertProbe) logDebug(ctx context.Context, msg string, kv ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Debug(ctx, msg, kv...)
}

// decimalDollars renders unit-count as dollars given the $0.01/unit price.
// Purely for error messages — no math elsewhere.
func decimalDollars(units int) string {
	return fmt.Sprintf("%.2f", float64(units)*0.01)
}
