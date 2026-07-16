package kafka

import (
	"context"
	"sync"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/logger"
)

// fakePublisher is a test double for messagePublisher. It records every published message and
// can be configured to fail, so we exercise the dual-write fan-out without a broker.
type fakePublisher struct {
	mu       sync.Mutex
	err      error
	calls    int
	topics   []string
	messages []*message.Message
}

func (f *fakePublisher) Publish(topic string, messages ...*message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.topics = append(f.topics, topic)
	f.messages = append(f.messages, messages...)
	return nil
}

func (f *fakePublisher) Close() error { return nil }

func (f *fakePublisher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakePublisher) only() *message.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) != 1 {
		return nil
	}
	return f.messages[0]
}

// newPubSub builds a PubSub over the given fake clusters (pass nil secondary to disable it).
func newPubSub(lg *logger.Logger, primary, secondary messagePublisher) *PubSub {
	ps := &PubSub{logger: lg, producer: primary}
	if secondary != nil {
		ps.secondary = secondary
	}
	return ps
}

func sampleMsg() *message.Message {
	m := message.NewMessage("wh_123", []byte(`{"event":"invoice.finalized"}`))
	m.Metadata.Set("tenant_id", "tenant_1")
	return m
}

func TestPubSubPublish_PrimaryOnly(t *testing.T) {
	primary := &fakePublisher{}
	ps := newPubSub(logger.NewNoopLogger(), primary, nil)

	if err := ps.Publish(context.Background(), "onboarding_events", sampleMsg()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if primary.callCount() != 1 || primary.topics[0] != "onboarding_events" {
		t.Fatalf("expected one publish on 'onboarding_events', got calls=%d topics=%v", primary.callCount(), primary.topics)
	}
}

func TestPubSubPublish_BothClusters_SameMessageDistinctCopy(t *testing.T) {
	primary := &fakePublisher{}
	secondary := &fakePublisher{}
	ps := newPubSub(logger.NewNoopLogger(), primary, secondary)

	if err := ps.Publish(context.Background(), "balance_alert", sampleMsg()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	pm, sm := primary.only(), secondary.only()
	if pm == nil || sm == nil {
		t.Fatalf("expected one message on each cluster; primary=%d secondary=%d", primary.callCount(), secondary.callCount())
	}
	if pm.UUID != sm.UUID || pm.UUID != "wh_123" {
		t.Fatalf("expected matching UUID 'wh_123', got primary=%q secondary=%q", pm.UUID, sm.UUID)
	}
	if string(pm.Payload) != string(sm.Payload) {
		t.Fatalf("expected identical payloads, got primary=%q secondary=%q", pm.Payload, sm.Payload)
	}
	// The watermill publisher consumes the message it is handed, so the secondary must get a copy.
	if pm == sm {
		t.Fatal("expected secondary to receive a distinct copy, got the same *message.Message pointer")
	}
}

// TestPubSubPublish_SecondaryFailure_Swallowed locks in the deliberate divergence from
// EventPublisher: pubsub callers fail the whole operation on a returned error, so a SECONDARY
// (GMK) failure must be logged but NOT returned — a GMK blip cannot fail an MSK-successful op.
func TestPubSubPublish_SecondaryFailure_Swallowed(t *testing.T) {
	primary := &fakePublisher{}
	secondary := &fakePublisher{err: context.DeadlineExceeded}
	ps := newPubSub(logger.NewNoopLogger(), primary, secondary)

	err := ps.Publish(context.Background(), "balance_alert", sampleMsg())
	if err != nil {
		t.Fatalf("secondary failure must NOT be returned when primary succeeds, got %v", err)
	}
	if primary.callCount() != 1 {
		t.Fatalf("expected primary to receive the message, got %d", primary.callCount())
	}
	if secondary.callCount() != 1 {
		t.Fatalf("expected the secondary publish to be attempted, got %d", secondary.callCount())
	}
}

// A PRIMARY failure IS returned (the caller decides what to do), and the secondary still
// receives the write — writes are independent.
func TestPubSubPublish_PrimaryFailure_Returned(t *testing.T) {
	primary := &fakePublisher{err: context.DeadlineExceeded}
	secondary := &fakePublisher{}
	ps := newPubSub(logger.NewNoopLogger(), primary, secondary)

	err := ps.Publish(context.Background(), "balance_alert", sampleMsg())
	if err == nil {
		t.Fatal("expected the primary failure to be returned, got nil")
	}
	if secondary.callCount() != 1 {
		t.Fatalf("expected secondary to still receive the message despite primary failure, got %d", secondary.callCount())
	}
}
