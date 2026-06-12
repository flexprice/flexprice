package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func TestLowWalletAlertListener_ValidPayload(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := e2eprobe.ListenerEvent{
		Payload: map[string]any{
			"alert_type":  "low_balance",
			"wallet_id":   "wal_1",
			"customer_id": "cust_1",
		},
	}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestLowWalletAlertListener_MissingFields(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := e2eprobe.ListenerEvent{Payload: map[string]any{"alert_type": "low_balance"}}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
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
	ev := e2eprobe.ListenerEvent{
		ReceivedAt: time.Now().Add(-2 * time.Hour),
		Payload: map[string]any{
			"alert_type":  "low_balance",
			"wallet_id":   "wal_1",
			"customer_id": "cust_1",
		},
	}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err == nil {
		t.Errorf("expected error for stale event")
	}
}
