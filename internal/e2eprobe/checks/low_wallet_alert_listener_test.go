package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func validAlertPayload() map[string]any {
	return map[string]any{
		"event_type":   "wallet.credit_balance.dropped",
		"alert_type":   "info",
		"alert_status": "in_alarm",
		"wallet":       map[string]any{"id": "wal_1"},
		"customer":     map[string]any{"id": "cust_1"},
		"alert": map[string]any{
			"state":           "critical",
			"alert_type":      "critical",
			"current_balance": "5.00",
			"credit_balance":  "5.00",
		},
	}
}

func TestLowWalletAlertListener_ValidPayload(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := e2eprobe.ListenerEvent{Payload: validAlertPayload()}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err != nil {
		t.Errorf("Run: %v", err)
	}
	seen := l.SeenThresholds("wal_1")
	if _, ok := seen["critical"]; !ok {
		t.Errorf("expected critical threshold recorded, got %v", seen)
	}
}

func TestLowWalletAlertListener_MissingFields(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	// alert.alert_type / wallet.id / customer.id all missing.
	ev := e2eprobe.ListenerEvent{Payload: map[string]any{
		"event_type": "wallet.credit_balance.dropped",
	}}
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

func TestLowWalletAlertListener_IgnoresUnrelatedEventTypes(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := e2eprobe.ListenerEvent{Payload: map[string]any{
		"event_type": "invoice.created",
	}}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err != nil {
		t.Errorf("non-wallet-alert events must be no-ops, got %v", err)
	}
}

func TestLowWalletAlertListener_StaleEventRejected(t *testing.T) {
	l := NewLowWalletAlertListener("run-1")
	ev := e2eprobe.ListenerEvent{
		ReceivedAt: time.Now().Add(-2 * time.Hour),
		Payload:    validAlertPayload(),
	}
	ctx := e2eprobe.ContextWithEvent(context.Background(), ev)
	if err := l.Run(ctx); err == nil {
		t.Errorf("expected error for stale event")
	}
}
