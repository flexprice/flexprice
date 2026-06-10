package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestLowWalletAlertListener_ValidPayload(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := synthetic.ListenerEvent{
		Payload: map[string]any{
			"alert_type":  "low_balance",
			"wallet_id":   "wal_1",
			"customer_id": "cust_1",
		},
	}
	ctx := synthetic.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestLowWalletAlertListener_MissingFields(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := synthetic.ListenerEvent{Payload: map[string]any{"alert_type": "low_balance"}}
	ctx := synthetic.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestLowWalletAlertListener_NoEventInContextIsNoOp(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	if err := l.Run(context.Background()); err != nil {
		t.Errorf("Run with no event in context should be no-op, got %v", err)
	}
}

func TestLowWalletAlertListener_StaleEventRejected(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := synthetic.ListenerEvent{
		ReceivedAt: time.Now().Add(-2 * time.Hour),
		Payload: map[string]any{
			"alert_type":  "low_balance",
			"wallet_id":   "wal_1",
			"customer_id": "cust_1",
		},
	}
	ctx := synthetic.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err == nil {
		t.Errorf("expected error for stale event")
	}
}
