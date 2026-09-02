package analytics

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
)

// fakePublisher is a test double for messagePublisher: it records what was published,
// and can be told to fail. Local to this package (kafka's own fake did not move here).
type fakePublisher struct {
	mu     sync.Mutex
	topics []string
	msgs   []*message.Message
	err    error
}

func (f *fakePublisher) Publish(topic string, messages ...*message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	for _, m := range messages {
		f.topics = append(f.topics, topic)
		f.msgs = append(f.msgs, m)
	}
	return nil
}

func (f *fakePublisher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func (f *fakePublisher) only() *message.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.msgs) != 1 {
		return nil
	}
	return f.msgs[0]
}

func sampleMeterUsage() *events.MeterUsage {
	mu := &events.MeterUsage{
		MeterID:    "meter_1",
		QtyTotal:   decimal.NewFromInt(5),
		UniqueHash: "hash_1",
	}
	mu.ID = "evt_1"
	mu.TenantID = "tenant_1"
	mu.EnvironmentID = "env_1"
	mu.EventName = "api_call"
	mu.Timestamp = time.Unix(1700000000, 0).UTC()
	return mu
}

// newAnalyticsPub builds a MeterUsageSinkPublisher over a fake, on the analytics topic.
func newAnalyticsPub(pub messagePublisher) *meterUsageSinkPublisher {
	return &meterUsageSinkPublisher{
		publisher: pub,
		topic:     "analytics.meter_usage.sink.realtime",
		logger:    logger.NewNoopLogger(),
	}
}

func TestPublishMeterUsage_MarshalsRecordToMainTopic(t *testing.T) {
	fake := &fakePublisher{}
	p := newAnalyticsPub(fake)

	rec := sampleMeterUsage()
	rec.IngestedAt = rec.Timestamp

	p.PublishMeterUsage(context.Background(), []*events.MeterUsage{rec})

	if fake.callCount() != 1 {
		t.Fatalf("expected 1 publish, got %d", fake.callCount())
	}
	if fake.topics[0] != "analytics.meter_usage.sink.realtime" {
		t.Fatalf("expected topic analytics.meter_usage, got %q", fake.topics[0])
	}

	msg := fake.only()
	if msg == nil {
		t.Fatal("expected exactly one message")
	}
	if msg.UUID != "evt_1" {
		t.Fatalf("expected message UUID evt_1 (dedup key), got %q", msg.UUID)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &got); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if got["id"] != "evt_1" {
		t.Errorf("missing/wrong id: %v", got["id"])
	}
	if got["meter_id"] != "meter_1" {
		t.Errorf("missing/wrong meter_id: %v", got["meter_id"])
	}
	if got["ingested_at"] == nil || got["ingested_at"] == "0001-01-01T00:00:00Z" {
		t.Errorf("ingested_at must be stamped for versioning, got %v", got["ingested_at"])
	}
}

// A zero ingested_at is stamped before publish.
func TestPublishMeterUsage_StampsIngestedAt(t *testing.T) {
	fake := &fakePublisher{}
	p := newAnalyticsPub(fake)

	p.PublishMeterUsage(context.Background(), []*events.MeterUsage{sampleMeterUsage()})

	msg := fake.only()
	if msg == nil {
		t.Fatal("expected exactly one message")
	}
	var got map[string]interface{}
	_ = json.Unmarshal(msg.Payload, &got)
	if got["ingested_at"] == nil || got["ingested_at"] == "0001-01-01T00:00:00Z" {
		t.Errorf("ingested_at must be stamped, got %v", got["ingested_at"])
	}
}

// Every record, regardless of ingestion recency, goes to the single main topic.
func TestPublishMeterUsage_AlwaysGoesToMainTopic(t *testing.T) {
	fake := &fakePublisher{}
	p := newAnalyticsPub(fake)

	rec := sampleMeterUsage()
	rec.IngestedAt = rec.Timestamp.Add(48 * time.Hour) // arbitrarily "late"; must not matter

	p.PublishMeterUsage(context.Background(), []*events.MeterUsage{rec})
	if fake.topics[0] != "analytics.meter_usage.sink.realtime" {
		t.Fatalf("record must go to main topic, got %q", fake.topics[0])
	}
}

// Fire-and-forget: a publisher error must NOT propagate out of PublishMeterUsage.
func TestPublishMeterUsage_SwallowsPublisherError(t *testing.T) {
	fake := &fakePublisher{err: context.DeadlineExceeded}
	p := newAnalyticsPub(fake)

	p.PublishMeterUsage(context.Background(), []*events.MeterUsage{sampleMeterUsage()})
}

// A nil (unconfigured) publisher is a no-op, never a panic.
func TestPublishMeterUsage_NilPublisherIsNoop(t *testing.T) {
	var p *meterUsageSinkPublisher
	p.PublishMeterUsage(context.Background(), []*events.MeterUsage{sampleMeterUsage()})
}

// When the feed is disabled the constructor must return an untyped-nil interface (not a
// typed-nil impl), so the service's `if pub != nil` guard is FALSE and PublishMeterUsage is
// never called — a nil interface method call would panic.
func TestNewMeterUsageSinkPublisher_DisabledReturnsNilInterface(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Analytics.Enabled = false

	pub := NewMeterUsageSinkPublisher(nil, cfg, logger.NewNoopLogger())
	if pub != nil {
		t.Fatalf("disabled feed must yield a nil interface, got %#v", pub)
	}
}
