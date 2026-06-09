package synthetic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPWebhookListener_DeliversEvents(t *testing.T) {
	l := NewHTTPWebhookListener(0) // port 0 -> ephemeral
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := l.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	port := l.BoundPort()
	if port == 0 {
		t.Fatal("port not bound")
	}
	go func() {
		_, _ = http.Post(fmt.Sprintf("http://127.0.0.1:%d/webhook", port), "application/json", bytes.NewReader([]byte(`{"alert_type":"low_balance","wallet_id":"w1"}`)))
	}()
	select {
	case ev := <-ch:
		if ev.Payload["alert_type"] != "low_balance" {
			t.Errorf("payload=%+v", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received")
	}
}

func TestListenerScheduler_InvokesCheckPerEvent(t *testing.T) {
	src := &channelListener{events: make(chan ListenerEvent, 4)}
	var seenPayloads int32
	c := &checkFn{name: "lst", kind: KindListener, fn: func(ctx context.Context) error {
		ev := EventFromContext(ctx)
		if ev != nil && ev.Payload["k"] == "v" {
			atomic.AddInt32(&seenPayloads, 1)
		}
		return nil
	}}
	s := NewListenerScheduler(c, src)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) })
	src.events <- ListenerEvent{Payload: map[string]any{"k": "v"}}
	src.events <- ListenerEvent{Payload: map[string]any{"k": "v"}}
	time.Sleep(40 * time.Millisecond)
	cancel()
	if got := atomic.LoadInt32(&seenPayloads); got != 2 {
		t.Errorf("seen=%d, want 2", got)
	}
}

// helpers

type channelListener struct{ events chan ListenerEvent }

func (c *channelListener) Name() string { return "chan" }
func (c *channelListener) Subscribe(_ context.Context) (<-chan ListenerEvent, error) {
	return c.events, nil
}

type checkFn struct {
	name string
	kind Kind
	fn   func(ctx context.Context) error
}

func (c *checkFn) Name() string                  { return c.name }
func (c *checkFn) Kind() Kind                    { return c.kind }
func (c *checkFn) Run(ctx context.Context) error { return c.fn(ctx) }
