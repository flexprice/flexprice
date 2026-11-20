package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// Units of the $0.01 meter driven per drop leg: enough to clear a fresh canary,
// headroom past the critical threshold, and a cap against a corrupt balance read.
const (
	alertDriveMinUnits    = 3500
	alertDriveBufferUnits = 200
	alertDriveMaxUnits    = 10_000_000
)

const droppedWebhook = "wallet.ongoing_balance.dropped"

// lowBalanceAlert alternates two legs on the canary wallet: drive its balance
// below the critical threshold and wait for the dropped webhook, then, finding
// it in alarm on the next run, refill it so the cycle can repeat.
type lowBalanceAlert struct {
	webhookWait time.Duration
}

func newLowBalanceAlert(scenario.Deps) *lowBalanceAlert {
	return &lowBalanceAlert{webhookWait: 2 * time.Minute}
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
	sc.Attr("wallet_id", walletID)
	resp, err := sc.Client().Wallets().GetBalance(ctx, walletID)
	if err != nil {
		return sc.Fail("balance", err)
	}
	if resp == nil || resp.WalletBalanceResponse == nil {
		return sc.Fail("balance", errors.New("empty balance response"))
	}
	bal := resp.WalletBalanceResponse
	state := types.AlertStateOk
	if bal.AlertState != nil {
		state = *bal.AlertState
	}
	realTime := ""
	if bal.RealTimeBalance != nil {
		realTime = *bal.RealTimeBalance
	}
	sc.Attr("alert_state", string(state))
	sc.Attr("real_time_balance", realTime)
	if state != types.AlertStateOk {
		return s.refill(ctx, sc, walletID)
	}
	return s.drop(ctx, sc, walletID, computeDriveUnits(realTime))
}

// refill restores the declared balance; the recovery webhook is not asserted here.
func (*lowBalanceAlert) refill(ctx context.Context, sc *scenario.Context, walletID string) e2eprobe.Result {
	sc.Attr("leg", "recover")
	spec := sc.Persona().Spec().Wallet
	if spec == nil || spec.Balance == "" {
		return sc.Skip("recover", "persona declares no wallet balance to refill to")
	}
	if err := walletTopUp(ctx, sc, walletID, spec.Balance, "e2eprobe alert canary recovery"); err != nil {
		return sc.Fail("top_up", err)
	}
	return sc.Result("recover")
}

// drop ingests one event large enough to cross the critical threshold and waits
// for its webhook; a miss is attributed to the alert pipeline or to delivery.
func (s *lowBalanceAlert) drop(ctx context.Context, sc *scenario.Context, walletID string, units int) e2eprobe.Result {
	sc.Attr("leg", "drop")
	sc.Attr("drive_units", strconv.Itoa(units))
	ext, t0 := sc.Persona().ExternalID(), sc.Now()
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
	if err != nil {
		return sc.Fail("ingest", err)
	}
	ev, err := sc.Inbox().Await(ctx, inbox.Match{EventType: droppedWebhook, WalletID: walletID, After: t0}, s.webhookWait)
	switch {
	case errors.Is(err, inbox.ErrTimeout):
		return s.explainMissingWebhook(ctx, sc, walletID, t0)
	case err != nil:
		return sc.Fail("webhook", err)
	}
	sc.Attr("webhook_at", ev.ReceivedAt.UTC().Format(time.RFC3339))
	return sc.Result("drop")
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
