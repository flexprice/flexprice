package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// Units of the $0.01 meter driven per run: enough to clear a fresh canary,
// headroom past the critical threshold, and a cap against a corrupt balance read.
const (
	alertDriveMinUnits    = 3500
	alertDriveBufferUnits = 200
	alertDriveMaxUnits    = 10_000_000
)

const droppedWebhook = "wallet.ongoing_balance.dropped"

// lowBalanceAlert drives the canary wallet below its critical threshold, waits
// for the dropped webhook and refills the wallet, so one run exercises the whole
// alert path and leaves the canary ready for the next. The wait covers the
// server's debounced usage-alert evaluation.
type lowBalanceAlert struct {
	webhookWait time.Duration
}

func newLowBalanceAlert(scenario.Deps) *lowBalanceAlert {
	return &lowBalanceAlert{webhookWait: 8 * time.Minute}
}

func (*lowBalanceAlert) Name() string               { return "low-balance-alert" }
func (*lowBalanceAlert) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*lowBalanceAlert) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitAlerts} }

func (s *lowBalanceAlert) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	walletID := p.WalletID()
	if walletID == "" {
		return sc.Skip("wallet", "persona has no wallet")
	}
	if gap := p.Gap(e2eprobe.TraitAlerts); gap != "" {
		return sc.Skip("alerts", gap)
	}
	spec := p.Spec().Wallet
	if spec == nil || spec.Balance == "" {
		return sc.Skip("wallet", "persona declares no wallet balance to refill to")
	}
	target, err := decimal.NewFromString(spec.Balance)
	if err != nil {
		return sc.Fail("wallet", fmt.Errorf("declared wallet balance %q: %w", spec.Balance, err))
	}
	sc.Attr("wallet_id", walletID)
	current, state, err := s.balance(ctx, sc, walletID)
	if err != nil {
		return sc.Fail("balance", err)
	}
	sc.Attr("alert_state", string(state))
	sc.Attr("real_time_balance", current.String())
	if state != types.AlertStateOk {
		// A previous run left the canary low; restore it before driving it down again.
		if current, err = refill(ctx, sc, walletID, target, current); err != nil {
			return sc.Fail("top_up", err)
		}
		sc.Attr("refilled_first", "yes")
	}
	units := computeDriveUnits(current.String())
	sc.Attr("drive_units", strconv.Itoa(units))
	t0 := sc.Now()
	if err := s.drive(ctx, sc, units, t0); err != nil {
		return sc.Fail("ingest", err)
	}
	ev, waitErr := sc.Inbox().Await(ctx, inbox.Match{EventType: droppedWebhook, WalletID: walletID, After: t0}, s.webhookWait)
	if waitErr == nil {
		sc.Attr("webhook_at", ev.ReceivedAt.UTC().Format(time.RFC3339))
	}
	// Refill whatever happened, so the next run starts from the declared balance.
	settled, _, err := s.balance(ctx, sc, walletID)
	if err != nil {
		return sc.Fail("balance", err)
	}
	left, err := refill(ctx, sc, walletID, target, settled)
	if err != nil {
		return sc.Fail("top_up", err)
	}
	sc.Attr("refilled_to", left.StringFixed(2))
	switch {
	case errors.Is(waitErr, inbox.ErrTimeout):
		return s.explainMissingWebhook(ctx, sc, walletID, t0)
	case waitErr != nil:
		return sc.Fail("webhook", waitErr)
	}
	return sc.Result("alert")
}

func (*lowBalanceAlert) balance(ctx context.Context, sc *scenario.Context, walletID string) (decimal.Decimal, types.AlertState, error) {
	bal, err := walletBalance(ctx, sc, walletID)
	if err != nil {
		return decimal.Zero, "", err
	}
	state := types.AlertStateOk
	if bal.AlertState != nil {
		state = *bal.AlertState
	}
	if bal.RealTimeBalance == nil {
		return decimal.Zero, state, errors.New("no real_time_balance")
	}
	current, err := decimal.NewFromString(strings.TrimSpace(*bal.RealTimeBalance))
	if err != nil {
		return decimal.Zero, state, fmt.Errorf("real_time_balance %q: %w", *bal.RealTimeBalance, err)
	}
	return current, state, nil
}

// refill credits the wallet up to target and returns the balance it leaves; a
// wallet already at or above target is left alone.
func refill(ctx context.Context, sc *scenario.Context, walletID string, target, current decimal.Decimal) (decimal.Decimal, error) {
	if !current.LessThan(target) {
		return current, nil
	}
	if err := walletTopUp(ctx, sc, walletID, target.Sub(current).StringFixed(2), "e2eprobe alert canary recovery"); err != nil {
		return current, err
	}
	return target, nil
}

// drive ingests one event large enough to cross the critical threshold.
func (*lowBalanceAlert) drive(ctx context.Context, sc *scenario.Context, units int, t0 time.Time) error {
	ext := sc.Persona().ExternalID()
	id := fmt.Sprintf("%s-alert-%d", ext, t0.Unix())
	ts := t0.UTC().Format(time.RFC3339Nano)
	_, err := sc.Client().Events().Ingest(ctx, types.IngestEventRequest{
		EventID:            &id,
		EventName:          walletUsageMeter.EventName,
		ExternalCustomerID: ext,
		Timestamp:          &ts,
		Properties: map[string]string{
			"amount":          strconv.Itoa(units),
			"e2eprobe":        "true",
			"e2eprobe_role":   "alert-canary",
			"e2eprobe_run_id": sc.RunID(),
		},
	})
	return err
}

// explainMissingWebhook separates an alert the server never raised from one
// whose webhook never reached the probe.
func (s *lowBalanceAlert) explainMissingWebhook(ctx context.Context, sc *scenario.Context, walletID string, since time.Time) e2eprobe.Result {
	entity := types.AlertEntityTypeWallet
	logs, err := sc.Client().Alerts().QueryLogs(ctx, types.AlertLogFilter{EntityType: &entity, EntityID: &walletID, StartTime: &since})
	if err != nil {
		return sc.Fail("alert_logs", err)
	}
	if len(logs) == 0 {
		return sc.Fail("alert_not_fired", fmt.Errorf("no low-balance alert was raised within %s after driving the balance below critical", s.webhookWait))
	}
	return sc.Fail("webhook_not_delivered", errors.New("alert raised but no "+droppedWebhook+" webhook reached the probe"))
}

// computeDriveUnits is how many $0.01 units push realTimeBalanceUSD below the
// critical threshold of 0 with headroom; an unreadable or non-positive balance
// gets the minimum so the pipeline still sees a fresh event.
func computeDriveUnits(realTimeBalanceUSD string) int {
	bal, err := decimal.NewFromString(strings.TrimSpace(realTimeBalanceUSD))
	if err != nil || bal.Sign() <= 0 {
		return alertDriveMinUnits
	}
	needed := int(bal.Mul(decimal.NewFromInt(100)).Ceil().IntPart()) + alertDriveBufferUnits
	return min(max(needed, alertDriveMinUnits), alertDriveMaxUnits)
}
