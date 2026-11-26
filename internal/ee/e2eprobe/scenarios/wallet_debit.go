package scenarios

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

const walletDebitEvents = 500

var (
	walletRefillFloor   = decimal.RequireFromString("20.00")
	walletRefillAmount  = decimal.RequireFromString("100.00")
	walletDebitExpected = decimal.RequireFromString("5.00") // 500 units at $0.01
	walletCent          = decimal.RequireFromString("0.01")
	// walletUsageMeter carries no entitlement, so every unit is billed at $0.01.
	walletUsageMeter, _ = e2eprobe.MeterByKey("sum_commit")
)

// registerWallet adds the wallet scenarios; the orchestrator calls it from Register.
func registerWallet(cat *scenario.Catalog) {
	for _, r := range []registration{
		entry("wallet-debit", e2eprobe.KindProbe, 30*time.Minute, func(d scenario.Deps) scenario.Scenario { return newWalletDebit(d) }, e2eprobe.TraitWallet).ingests(walletDebitEvents).checks("a top-up is visible on read-back and 500 units of the entitlement-free meter debit exactly $5.00; a low wallet is refilled first"),
		entry("low-balance-alert", e2eprobe.KindProbe, 30*time.Minute, func(d scenario.Deps) scenario.Scenario { return newLowBalanceAlert(d) }, e2eprobe.TraitAlerts).ingests(1).checks("driving the canary below its critical threshold raises the low-balance alert and its webhook reaches the probe; the canary is refilled"),
	} {
		cat.Register(r.info, r.build)
	}
}

// walletDebit refills the persona's wallet when it runs low, then drives usage
// on a meter without entitlement and waits for the real-time balance to fall by
// exactly what that usage costs.
type walletDebit struct {
	pollEvery  time.Duration
	refillWait time.Duration
	debitWait  time.Duration
}

func newWalletDebit(scenario.Deps) *walletDebit {
	return &walletDebit{pollEvery: 10 * time.Second, refillWait: time.Minute, debitWait: 3 * time.Minute}
}

func (*walletDebit) Name() string               { return "wallet-debit" }
func (*walletDebit) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*walletDebit) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitWallet} }

func (s *walletDebit) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	walletID := sc.Persona().WalletID()
	if walletID == "" {
		return sc.Skip("wallet", "persona has no wallet")
	}
	sc.Attr("wallet_id", walletID)
	before, err := walletRealTimeBalance(ctx, sc, walletID)
	if err != nil {
		return sc.Fail("balance", err)
	}
	if before.LessThan(walletRefillFloor) {
		if err := walletTopUp(ctx, sc, walletID, walletRefillAmount.StringFixed(2), "e2eprobe refill"); err != nil {
			return sc.Fail("top_up", err)
		}
		want := before.Add(walletRefillAmount)
		got, err := s.pollBalance(ctx, sc, walletID, s.refillWait, func(b decimal.Decimal) bool { return b.Sub(want).Abs().LessThanOrEqual(walletCent) })
		if err != nil {
			return sc.Fail("balance", err)
		}
		sc.Money("top-up read-after-write", want, got)
		sc.Attr("refill", walletRefillAmount.StringFixed(2))
		before = want
	}
	sc.Attr("balance_before", before.StringFixed(2))

	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: s.batch(sc)}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	floor := before.Sub(walletDebitExpected).Add(walletCent)
	got, err := s.pollBalance(ctx, sc, walletID, s.debitWait, func(b decimal.Decimal) bool { return b.LessThanOrEqual(floor) })
	if err != nil {
		return sc.Fail("balance", err)
	}
	sc.Attr("balance_after", got.StringFixed(2))
	sc.Money("usage debit", walletDebitExpected, before.Sub(got))
	return sc.Result("assert_debit")
}

// batch is one unit per event on the no-entitlement meter, ids keyed by the run instant.
func (*walletDebit) batch(sc *scenario.Context) []types.IngestEventRequest {
	ext, now := sc.Persona().ExternalID(), sc.Now()
	ts := now.UTC().Format(time.RFC3339Nano)
	out := make([]types.IngestEventRequest, 0, walletDebitEvents)
	for i := 0; i < walletDebitEvents; i++ {
		id := fmt.Sprintf("%s-wallet-debit-%d-%d", ext, now.Unix(), i)
		out = append(out, types.IngestEventRequest{
			EventID:            &id,
			EventName:          walletUsageMeter.EventName,
			ExternalCustomerID: ext,
			Timestamp:          &ts,
			Properties:         map[string]string{"amount": "1", "e2eprobe": "true", "e2eprobe_run_id": sc.RunID()},
		})
	}
	return out
}

// pollBalance reads the balance every pollEvery until accept holds or timeout
// passes; the last read is returned either way so the caller can report it.
func (s *walletDebit) pollBalance(ctx context.Context, sc *scenario.Context, walletID string, timeout time.Duration, accept func(decimal.Decimal) bool) (decimal.Decimal, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		got, err := walletRealTimeBalance(ctx, sc, walletID)
		if err != nil || accept(got) {
			return got, err
		}
		select {
		case <-ctx.Done():
			return got, ctx.Err()
		case <-deadline.C:
			return got, nil
		case <-time.After(s.pollEvery):
		}
	}
}

// walletBalance reads a wallet's live balance. A cached fallback means the
// server could not compute it: a degraded platform, not a balance to act on.
func walletBalance(ctx context.Context, sc *scenario.Context, walletID string) (*types.WalletBalanceResponse, error) {
	resp, err := sc.Client().Wallets().GetBalance(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.WalletBalanceResponse == nil {
		return nil, errors.New("empty balance response")
	}
	if fallback := resp.WalletBalanceResponse.IsCachedFallback; fallback != nil && *fallback {
		return nil, sdkerrors.NewAPIError("wallet balance served from cache fallback", 503, "", nil)
	}
	return resp.WalletBalanceResponse, nil
}

func walletRealTimeBalance(ctx context.Context, sc *scenario.Context, walletID string) (decimal.Decimal, error) {
	bal, err := walletBalance(ctx, sc, walletID)
	if err != nil {
		return decimal.Zero, err
	}
	if bal.RealTimeBalance == nil {
		return decimal.Zero, errors.New("no real_time_balance")
	}
	return decimal.NewFromString(*bal.RealTimeBalance)
}

func walletTopUp(ctx context.Context, sc *scenario.Context, walletID, amount, description string) error {
	_, err := sc.Client().Wallets().TopUp(ctx, walletID, types.TopUpWalletRequest{
		Amount:            &amount,
		Description:       &description,
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
	})
	return err
}
