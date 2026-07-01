package checks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

const lowWalletAlertMaxAge = 5 * time.Minute

// Wallet-alert event types Flexprice dispatches when a threshold is crossed.
// Anything else on /webhook is ignored (not an error) so this listener can
// safely share the endpoint with unrelated webhook traffic.
var walletAlertEventTypes = map[string]struct{}{
	"wallet.credit_balance.dropped":    {},
	"wallet.credit_balance.recovered":  {},
	"wallet.ongoing_balance.dropped":   {},
	"wallet.ongoing_balance.recovered": {},
}

type LowWalletAlertListener struct {
	runID string

	mu       sync.Mutex
	lastSeen map[string]time.Time // key: "<wallet_id>:<alert_type>"
}

func NewLowWalletAlertListener(runID string) *LowWalletAlertListener {
	return &LowWalletAlertListener{runID: runID, lastSeen: map[string]time.Time{}}
}

func (l *LowWalletAlertListener) Name() string        { return "low-wallet-alert-listener" }
func (l *LowWalletAlertListener) Kind() e2eprobe.Kind { return e2eprobe.KindListener }

func (l *LowWalletAlertListener) Run(ctx context.Context) error {
	ev := e2eprobe.EventFromContext(ctx)
	if ev == nil {
		return nil
	}

	eventType, _ := ev.Payload["event_type"].(string)
	if _, ok := walletAlertEventTypes[eventType]; !ok {
		// Not a wallet-alert webhook (e.g. invoice.created); ignore.
		return nil
	}

	walletID := nestedString(ev.Payload, "wallet", "id")
	customerID := nestedString(ev.Payload, "customer", "id")
	alertType := nestedString(ev.Payload, "alert", "alert_type")
	alertState := nestedString(ev.Payload, "alert", "state")
	currentBalance := nestedString(ev.Payload, "alert", "current_balance")

	attrs := map[string]string{
		"event_type":      eventType,
		"wallet_id":       walletID,
		"customer_id":     customerID,
		"alert_type":      alertType,
		"alert_state":     alertState,
		"current_balance": currentBalance,
	}

	if !ev.ReceivedAt.IsZero() && time.Since(ev.ReceivedAt) > lowWalletAlertMaxAge {
		return e2eprobe.Errorf(attrs,
			"low-wallet alert delivered late: received_at=%s age=%s",
			ev.ReceivedAt.Format(time.RFC3339), time.Since(ev.ReceivedAt))
	}

	var missing []string
	if walletID == "" {
		missing = append(missing, "wallet.id")
	}
	if customerID == "" {
		missing = append(missing, "customer.id")
	}
	if alertType == "" {
		missing = append(missing, "alert.alert_type")
	}
	if len(missing) > 0 {
		return e2eprobe.Errorf(attrs, "low-wallet alert missing fields: %v", missing)
	}

	l.recordReceipt(walletID, alertType)
	return nil
}

func (l *LowWalletAlertListener) recordReceipt(walletID, alertType string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastSeen[walletID+":"+strings.ToLower(alertType)] = time.Now()
}

// SeenThresholds returns the alert_type → last-seen timestamp map for a wallet.
// Consumed by heartbeat / debug tooling to report which of {info, warning,
// critical} have been observed for each configured wallet.
func (l *LowWalletAlertListener) SeenThresholds(walletID string) map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]time.Time{}
	prefix := walletID + ":"
	for k, v := range l.lastSeen {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out
}

// nestedString reads payload[keys[0]][keys[1]]... as a string; returns "" if
// any hop is missing or the leaf isn't a string.
func nestedString(payload map[string]any, keys ...string) string {
	var cur any = payload
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	default:
		return ""
	}
}
