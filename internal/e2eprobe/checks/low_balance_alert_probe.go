package checks

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// LowBalanceAlertProbe drives the canary wallet across its low-balance
// alert threshold and asserts the corresponding webhook lands within a
// bounded window. Missing webhook → check error → Slack via reporter.
//
// Flexprice's wallet alert-state machine is binary (ok / in_alarm), so we
// cannot exercise info / warning / critical in one tick. The probe cycles:
//
//   Tick T0 (wallet=ok):    ingest usage → drop projected balance below
//                           the info threshold (25) → wait for the webhook.
//   Tick T1 (wallet=in_alarm): top-up the wallet back to $30 to recover.
//
// One end-to-end verification per two ticks. At default 5m interval,
// full cycle every 10m.
type LowBalanceAlertProbe struct {
	client   e2eprobe.Client
	reg      e2eprobe.Registry
	listener *LowWalletAlertListener
	runID    string
	opts     LowBalanceAlertOpts
}

// LowBalanceAlertOpts are runtime knobs. Zero-value falls through to sane
// defaults set by NewLowBalanceAlertProbe.
type LowBalanceAlertOpts struct {
	// UsageAmount is the value of the "amount" property on the driver event.
	// Combined with the $0.01 e2eprobe_sum unit price and the 30/25/10/0
	// threshold spread, a single event with amount=600 pushes ongoing_balance
	// from $30 to $24 (below info=25). Default 600.
	UsageAmount int

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

func NewLowBalanceAlertProbe(c e2eprobe.Client, r e2eprobe.Registry, listener *LowWalletAlertListener, runID string, opts LowBalanceAlertOpts) *LowBalanceAlertProbe {
	if opts.UsageAmount == 0 {
		opts.UsageAmount = 600
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
	return &LowBalanceAlertProbe{client: c, reg: r, listener: listener, runID: runID, opts: opts}
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
	attrs := map[string]string{
		"external_customer_id": ext,
		"wallet_id":            walletID,
		"alert_state":          state,
	}
	if b.RealTimeBalance != nil {
		attrs["real_time_balance"] = *b.RealTimeBalance
	}

	if state != "ok" {
		// Recovery leg: top-up so the state machine can re-arm for the next
		// alert cycle. The recovery webhook (wallet.updated) is not asserted
		// here — this check owns the drop → alert leg.
		return p.recover(ctx, walletID, ext, attrs)
	}

	// Drop leg: ingest usage to push ongoing_balance below the info threshold,
	// then wait for the webhook to reach the listener.
	return p.driveAndVerify(ctx, walletID, ext, attrs)
}

func (p *LowBalanceAlertProbe) driveAndVerify(ctx context.Context, walletID, ext string, attrs map[string]string) error {
	// Baseline the newest receipt timestamp per alert_type before ingest.
	// SeenThresholds is keyed by alert_type so len() doesn't grow on repeat
	// cycles — a re-fired "low_ongoing_balance" overwrites the same key.
	// We must compare timestamps to detect a fresh delivery.
	baseline := maxReceipt(p.listener.SeenThresholds(walletID))

	amountStr := strconv.Itoa(p.opts.UsageAmount)
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

	deadline := time.Now().Add(p.opts.WebhookWait)
	for {
		if newest := maxReceipt(p.listener.SeenThresholds(walletID)); newest.After(baseline) {
			return nil
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(attrs,
				"no low-balance webhook received within %s after ingesting $%s (rate=$0.01/unit) on wallet %s",
				p.opts.WebhookWait, decimalDollars(p.opts.UsageAmount), walletID)
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
	return nil
}

// decimalDollars renders unit-count as dollars given the $0.01/unit price.
// Purely for error messages — no math elsewhere.
func decimalDollars(units int) string {
	return fmt.Sprintf("%.2f", float64(units)*0.01)
}
