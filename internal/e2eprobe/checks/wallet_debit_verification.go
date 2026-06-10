package checks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type WalletDebitOpts struct {
	EventCount   int
	EventAmount  string
	PollInterval time.Duration
	PollTimeout  time.Duration
}

func defaultWalletDebitOpts() WalletDebitOpts {
	return WalletDebitOpts{
		EventCount:   100,
		EventAmount:  "0.01",
		PollInterval: 5 * time.Second,
		PollTimeout:  120 * time.Second,
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

	// Query all wallets and filter client-side by customer ID (WalletFilter has
	// no customer filter field; the synthetic tenant has a bounded wallet count).
	walletResp, err := v.client.Wallets().Query(ctx, types.WalletFilter{})
	if err != nil {
		return fmt.Errorf("wallet query for %s: %w", customer, err)
	}
	walletIDs := extractWalletIDsForCustomer(walletResp, customer)
	if len(walletIDs) == 0 {
		return nil
	}
	walletID := walletIDs[0]
	startBalance, err := v.readBalance(ctx, walletID)
	if err != nil {
		return fmt.Errorf("read start balance: %w", err)
	}

	amountPerEvent, err := parseFloat(v.opts.EventAmount)
	if err != nil {
		return fmt.Errorf("parse event amount: %w", err)
	}
	expectedDebit := float64(v.opts.EventCount) * amountPerEvent
	if startBalance < expectedDebit*5 {
		topUp := expectedDebit * 10
		topUpStr := fmt.Sprintf("%.4f", topUp)
		if _, err := v.client.Wallets().TopUp(ctx, walletID, types.DtoTopUpWalletRequest{
			Amount: &topUpStr,
		}); err != nil {
			return fmt.Errorf("top up: %w", err)
		}
		startBalance, err = v.readBalance(ctx, walletID)
		if err != nil {
			return fmt.Errorf("read balance after topup: %w", err)
		}
	}

	for i := 0; i < v.opts.EventCount; i++ {
		req := types.DtoIngestEventRequest{
			EventName:          "e2eprobe_sum",
			ExternalCustomerID: customer,
			Properties: map[string]string{
				"amount":           v.opts.EventAmount,
				"e2eprobe": "true",
				"e2eprobe_run_id": v.runID,
				"debit_batch":      fmt.Sprintf("%d", time.Now().UnixNano()),
			},
		}
		if _, err := v.client.Events().Ingest(ctx, req); err != nil {
			return fmt.Errorf("ingest event %d: %w", i, err)
		}
	}

	deadline := time.Now().Add(v.opts.PollTimeout)
	for {
		curr, err := v.readBalance(ctx, walletID)
		if err != nil {
			if time.Now().After(deadline) {
				return fmt.Errorf("read balance timed out: %w", err)
			}
		} else {
			delta := startBalance - curr
			if delta >= expectedDebit*0.99 {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("debit short: expected ~%.4f, observed %.4f (start=%.4f current=%.4f) after %s",
					expectedDebit, delta, startBalance, curr, v.opts.PollTimeout)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(v.opts.PollInterval):
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
	inner := r.GetDtoWalletBalanceResponse()
	if inner == nil || inner.Balance == nil {
		return 0, fmt.Errorf("response missing balance field")
	}
	var f float64
	if _, err := fmt.Sscanf(*inner.Balance, "%f", &f); err != nil {
		return 0, fmt.Errorf("parse balance %q: %w", *inner.Balance, err)
	}
	return f, nil
}

func parseFloat(s string) (float64, error) {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, fmt.Errorf("parse %q as float: %w", s, err)
	}
	return f, nil
}
