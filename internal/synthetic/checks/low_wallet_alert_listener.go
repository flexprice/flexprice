package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

const lowWalletAlertMaxAge = 5 * time.Minute

type LowWalletAlertListener struct {
	runID string
}

func NewLowWalletAlertListener(runID string) *LowWalletAlertListener {
	return &LowWalletAlertListener{runID: runID}
}

func (l *LowWalletAlertListener) Name() string         { return "low-wallet-alert-listener" }
func (l *LowWalletAlertListener) Kind() synthetic.Kind { return synthetic.KindListener }

var requiredFields = []string{"alert_type", "wallet_id", "customer_id"}

func (l *LowWalletAlertListener) Run(ctx context.Context) error {
	ev := synthetic.EventFromContext(ctx)
	if ev == nil {
		return nil
	}
	if !ev.ReceivedAt.IsZero() && time.Since(ev.ReceivedAt) > lowWalletAlertMaxAge {
		return fmt.Errorf("low-wallet alert delivered late: received_at=%s age=%s",
			ev.ReceivedAt.Format(time.RFC3339), time.Since(ev.ReceivedAt))
	}
	missing := []string{}
	for _, f := range requiredFields {
		if _, ok := ev.Payload[f]; !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("low-wallet alert missing fields: %v", missing)
	}
	return nil
}
