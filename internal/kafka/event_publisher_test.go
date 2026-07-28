package kafka

import (
	"context"
	"encoding/json"
	"fmt"
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

// bulkKafkaCfg is testKafkaCfg plus the batch knobs the publish path reads off KafkaConfig.
func bulkKafkaCfg(size, bytes int) *config.KafkaConfig {
	kc := testKafkaCfg()
	kc.TopicBulk = "events_bulk"
	kc.BulkMaxBatchSize = size
	kc.BulkMaxBatchBytes = bytes
	return kc
}

func evt(id, tenant, customer string) *events.Event {
	return &events.Event{ID: id, TenantID: tenant, ExternalCustomerID: customer, EventName: "api_call"}
}

// Customers in one tenant/environment share a batch — that is the point, since more events
// per message means fewer INSERTs. Only scope splits a batch.
func TestGroupForBatching_GroupsByScope(t *testing.T) {
	in := []*events.Event{
		evt("1", "t1", "cust_a"),
		evt("2", "t1", "cust_b"),
		evt("3", "t2", "cust_a"),
	}

	groups, err := groupForBatching(in, bulkKafkaCfg(100, 0))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (one per tenant)", len(groups))
	}
	byTenant := map[string]int{}
	for _, g := range groups {
		byTenant[g.tenantID] = len(g.events)
	}
	if byTenant["t1"] != 2 || byTenant["t2"] != 1 {
		t.Errorf("wrong grouping: %v", byTenant)
	}
}

func TestGroupForBatching_SplitsOnMaxBatchSize(t *testing.T) {
	in := make([]*events.Event, 0, 5)
	for i := 0; i < 5; i++ {
		in = append(in, evt(fmt.Sprintf("e%d", i), "t1", "cust_a"))
	}

	groups, err := groupForBatching(in, bulkKafkaCfg(2, 0))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3 (2+2+1)", len(groups))
	}
	for i, want := range []int{2, 2, 1} {
		if len(groups[i].events) != want {
			t.Errorf("group %d has %d events, want %d", i, len(groups[i].events), want)
		}
	}
}

// The byte bound keeps a message under the topic's max.message.bytes.
func TestGroupForBatching_SplitsOnMaxBatchBytes(t *testing.T) {
	in := []*events.Event{
		evt("1", "t1", "cust_a"),
		evt("2", "t1", "cust_a"),
		evt("3", "t1", "cust_a"),
	}

	// One event marshals well above 40 bytes, so every event should land alone.
	groups, err := groupForBatching(in, bulkKafkaCfg(100, 40))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3 (byte cap forces one event per batch)", len(groups))
	}
}

// An event bigger than the cap must still be published rather than dropped.
func TestGroupForBatching_OversizedSingleEventStillEmitted(t *testing.T) {
	groups, err := groupForBatching([]*events.Event{evt("1", "t1", "cust_a")}, bulkKafkaCfg(100, 1))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if len(groups) != 1 || len(groups[0].events) != 1 {
		t.Fatalf("oversized event was dropped: %+v", groups)
	}
}

func TestGroupForBatching_AssignsMissingEventIDs(t *testing.T) {
	in := []*events.Event{{TenantID: "t1", EventName: "api_call"}}
	groups, err := groupForBatching(in, bulkKafkaCfg(10, 0))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if groups[0].events[0].ID == "" {
		t.Error("event id was left empty; ClickHouse dedup depends on it")
	}
}

// newBulkPub points both clusters at a KafkaConfig carrying the batch knobs.
func newBulkPub(primary, secondary messagePublisher, kc *config.KafkaConfig) *EventPublisher {
	ep := newPub(logger.NewNoopLogger(), primary, secondary)
	ep.primaryCfg = kc
	if secondary != nil {
		sc := *kc
		ep.secondaryCfg = &sc
	}
	return ep
}

func TestPublishBatch_OneMessagePerScope(t *testing.T) {
	primary := &fakePublisher{}
	ep := newBulkPub(primary, nil, bulkKafkaCfg(100, 0))

	// Three events, two tenants: customers do not split a batch, scope does.
	err := ep.PublishBatch(context.Background(), []*events.Event{
		evt("1", "tenant_1", "cust_a"),
		evt("2", "tenant_1", "cust_b"),
		evt("3", "tenant_2", "cust_a"),
	})
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if primary.callCount() != 2 {
		t.Fatalf("published %d messages, want 2 (one per tenant)", primary.callCount())
	}

	sizes := map[string]int{}
	for _, msg := range primary.messages {
		var batch events.EventBatch
		if err := json.Unmarshal(msg.Payload, &batch); err != nil {
			t.Fatalf("payload is not an EventBatch: %v", err)
		}
		if msg.Metadata.Get("tenant_id") != batch.TenantID {
			t.Errorf("metadata tenant_id %q != envelope %q", msg.Metadata.Get("tenant_id"), batch.TenantID)
		}
		sizes[batch.TenantID] = len(batch.Events)
	}
	if sizes["tenant_1"] != 2 || sizes["tenant_2"] != 1 {
		t.Errorf("wrong batch contents: %v", sizes)
	}
}

// Topic names are per cluster, so one literal name would target a topic the secondary may not have.
func TestPublishBatch_ResolvesTopicPerCluster(t *testing.T) {
	primary, secondary := &fakePublisher{}, &fakePublisher{}
	ep := newBulkPub(primary, secondary, bulkKafkaCfg(100, 0))
	ep.primaryCfg.TopicBulk = "local_events_bulk"
	ep.secondaryCfg.TopicBulk = "prod_events_bulk"

	if err := ep.PublishBatch(context.Background(), []*events.Event{evt("1", "tenant_1", "cust_a")}); err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}

	if got := primary.topics[0]; got != "local_events_bulk" {
		t.Errorf("primary topic = %q, want local_events_bulk", got)
	}
	if got := secondary.topics[0]; got != "prod_events_bulk" {
		t.Errorf("secondary topic = %q, want prod_events_bulk", got)
	}
	if string(primary.messages[0].Payload) != string(secondary.messages[0].Payload) {
		t.Error("clusters received different payloads; the streams must stay dedup-identical")
	}
}

// An unset batch topic must fail loudly rather than publishing to "".
func TestPublishBatch_MissingTopicErrors(t *testing.T) {
	primary := &fakePublisher{}
	ep := newBulkPub(primary, nil, bulkKafkaCfg(100, 0))
	ep.primaryCfg.TopicBulk = ""

	if err := ep.PublishBatch(context.Background(), []*events.Event{evt("1", "tenant_1", "cust_a")}); err == nil {
		t.Fatal("expected an error when kafka.topic_bulk is unset")
	}
	if primary.callCount() != 0 {
		t.Errorf("published %d message(s) with no topic configured", primary.callCount())
	}
}

func TestPublishBatch_EmptyInputIsNoop(t *testing.T) {
	primary := &fakePublisher{}
	ep := newBulkPub(primary, nil, bulkKafkaCfg(100, 0))

	if err := ep.PublishBatch(context.Background(), nil); err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if primary.callCount() != 0 {
		t.Errorf("published %d messages for an empty batch, want 0", primary.callCount())
	}
}

// Two environments must not share a batch: the envelope carries one environment_id.
func TestGroupForBatching_NeverMixesEnvironments(t *testing.T) {
	a := evt("1", "t1", "cust_a")
	a.EnvironmentID = "env_prod"
	b := evt("2", "t1", "cust_a")
	b.EnvironmentID = "env_staging"

	groups, err := groupForBatching([]*events.Event{a, b}, bulkKafkaCfg(100, 0))
	if err != nil {
		t.Fatalf("groupForBatching: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d group(s), want 2 — environments must not share a batch", len(groups))
	}
	for _, g := range groups {
		if len(g.events) != 1 {
			t.Errorf("group has %d events, want 1", len(g.events))
		}
		if g.environmentID != g.events[0].EnvironmentID {
			t.Errorf("envelope env %q does not match its event's %q", g.environmentID, g.events[0].EnvironmentID)
		}
	}
}
