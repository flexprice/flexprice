package kafka

import (
	"context"
	"sync"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
)

// fakePublisher is a test double for messagePublisher. It records every published message
// and can be configured to fail, so we can exercise the fan-out without a broker.
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

func testKafkaCfg() *config.KafkaConfig {
	return &config.KafkaConfig{
		Topic:                  "events",
		TopicLazy:              "events_lazy",
		RouteTenantsOnLazyMode: []string{"tenant_lazy"},
	}
}

// newPub builds an EventPublisher writing to the given clusters (pass nil to disable one).
func newPub(lg *logger.Logger, primary, secondary messagePublisher) *EventPublisher {
	ep := &EventPublisher{logger: lg}
	if primary != nil {
		ep.primary = primary
		ep.primaryCfg = testKafkaCfg()
	}
	if secondary != nil {
		ep.secondary = secondary
		ep.secondaryCfg = testKafkaCfg()
	}
	return ep
}

func sampleEvent() *events.Event {
	return &events.Event{
		ID:                 "evt_123",
		TenantID:           "tenant_1",
		EnvironmentID:      "env_1",
		EventName:          "api_call",
		ExternalCustomerID: "cust_ext_9",
	}
}

func TestPublish_PrimaryOnly_Success(t *testing.T) {
	primary := &fakePublisher{}
	ep := newPub(logger.NewNoopLogger(), primary, nil)

	if err := ep.Publish(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if primary.callCount() != 1 || primary.topics[0] != "events" {
		t.Fatalf("expected one publish on 'events', got calls=%d topics=%v", primary.callCount(), primary.topics)
	}
}

func TestPublish_BothClusters_ReceiveSamePayloadAndID(t *testing.T) {
	primary := &fakePublisher{}
	secondary := &fakePublisher{}
	ep := newPub(logger.NewNoopLogger(), primary, secondary)

	if err := ep.Publish(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	pm, sm := primary.only(), secondary.only()
	if pm == nil || sm == nil {
		t.Fatalf("expected one message on each cluster; primary=%d secondary=%d", primary.callCount(), secondary.callCount())
	}
	if pm.UUID != sm.UUID || pm.UUID != "evt_123" {
		t.Fatalf("expected matching UUID 'evt_123', got primary=%q secondary=%q", pm.UUID, sm.UUID)
	}
	if string(pm.Payload) != string(sm.Payload) {
		t.Fatalf("expected identical payloads, got primary=%q secondary=%q", pm.Payload, sm.Payload)
	}
	if pm.Metadata.Get("partition_key") != "tenant_1:cust_ext_9" || sm.Metadata.Get("partition_key") != pm.Metadata.Get("partition_key") {
		t.Fatalf("partition keys wrong/mismatched: primary=%q secondary=%q", pm.Metadata.Get("partition_key"), sm.Metadata.Get("partition_key"))
	}
}

func TestPublish_OneClusterFailure_DoesNotBlockOther(t *testing.T) {
	primary := &fakePublisher{err: context.DeadlineExceeded}
	secondary := &fakePublisher{}
	ep := newPub(logger.NewNoopLogger(), primary, secondary)

	// Primary fails, but secondary must still receive the event (independent writes). The
	// failure is surfaced as the returned error (service logs it and returns 202 regardless).
	err := ep.Publish(context.Background(), sampleEvent())
	if err == nil {
		t.Fatal("expected the primary failure to be returned, got nil")
	}
	if secondary.callCount() != 1 {
		t.Fatalf("expected secondary to still receive the event despite primary failure, got %d", secondary.callCount())
	}
}

func TestPublish_AssignsEventIDWhenEmpty(t *testing.T) {
	primary := &fakePublisher{}
	ep := newPub(logger.NewNoopLogger(), primary, nil)

	ev := sampleEvent()
	ev.ID = ""
	if err := ep.Publish(context.Background(), ev); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ev.ID == "" {
		t.Fatal("expected event.ID to be assigned a generated UUID")
	}
	if msg := primary.only(); msg == nil || msg.UUID != ev.ID {
		t.Fatalf("expected message UUID to equal assigned event.ID %q", ev.ID)
	}
}

func TestPublish_RoutesLazyTenantToLazyTopic(t *testing.T) {
	primary := &fakePublisher{}
	ep := newPub(logger.NewNoopLogger(), primary, nil)

	ev := sampleEvent()
	ev.TenantID = "tenant_lazy"
	if err := ep.Publish(context.Background(), ev); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if primary.topics[0] != "events_lazy" {
		t.Fatalf("expected lazy-routed tenant on 'events_lazy', got %q", primary.topics[0])
	}
}
