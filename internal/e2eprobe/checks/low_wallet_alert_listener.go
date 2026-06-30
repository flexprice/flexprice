package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

const lowWalletAlertMaxAge = 5 * time.Minute

type LowWalletAlertListener struct {
	runID string
}

func NewLowWalletAlertListener(runID string) *LowWalletAlertListener {
	return &LowWalletAlertListener{runID: runID}
}

func (l *LowWalletAlertListener) Name() string         { return "low-wallet-alert-listener" }
func (l *LowWalletAlertListener) Kind() e2eprobe.Kind { return e2eprobe.KindListener }

var requiredFields = []string{"alert_type", "wallet_id", "customer_id"}

func (l *LowWalletAlertListener) Run(ctx context.Context) error {
	ev := e2eprobe.EventFromContext(ctx)
	if ev == nil {
		return nil
	}
	// Build attributes from payload fields that are present.
	alertAttrs := map[string]string{}
	for _, f := range requiredFields {
		if v, ok := ev.Payload[f]; ok {
			alertAttrs[f] = fmt.Sprintf("%v", v)
		}
	}

	if !ev.ReceivedAt.IsZero() && time.Since(ev.ReceivedAt) > lowWalletAlertMaxAge {
		return e2eprobe.Errorf(alertAttrs,
			"low-wallet alert delivered late: received_at=%s age=%s",
			ev.ReceivedAt.Format(time.RFC3339), time.Since(ev.ReceivedAt))
	}
	missing := []string{}
	for _, f := range requiredFields {
		if _, ok := ev.Payload[f]; !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return e2eprobe.Errorf(alertAttrs, "low-wallet alert missing fields: %v", missing)
	}
	return nil
}
