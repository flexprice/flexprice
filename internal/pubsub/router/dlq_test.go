package router

import (
	"sync"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePublisher records every Publish call so tests can assert routing behavior.
type fakePublisher struct {
	mu         sync.Mutex
	calls      []publishCall
	closed     bool
	closes     int
	publishErr error
}

type publishCall struct {
	topic string
	msgs  []*message.Message
}

func (f *fakePublisher) Publish(topic string, msgs ...*message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, publishCall{topic: topic, msgs: msgs})
	return f.publishErr
}

func (f *fakePublisher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.closes++
	return nil
}

func (f *fakePublisher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePublisher) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelError},
	})
	require.NoError(t, err)
	return log
}

// poisonedMsg builds a message stamped the way the PoisonQueue middleware stamps
// it before invoking the publisher.
func poisonedMsg(handlerName string) *message.Message {
	msg := message.NewMessage("uuid-1", []byte("payload"))
	msg.Metadata.Set(middleware.PoisonedHandlerKey, handlerName)
	msg.Metadata.Set(middleware.ReasonForPoisonedKey, "boom")
	msg.Metadata.Set("tenant_id", "tenant_123")
	msg.Metadata.Set("environment_id", "env_456")
	return msg
}

const testFallbackTopic = "events_dlq"

func TestRoutingDLQPublisher_RoutesToConfiguredTopic(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	pub.Register("event_consumption_handler", "v1_event_processing", "v1_event_processing.dlq")

	err := pub.Publish("_dlq_placeholder", poisonedMsg("event_consumption_handler"))
	require.NoError(t, err)

	require.Equal(t, 1, inner.callCount())
	assert.Equal(t, "v1_event_processing.dlq", inner.calls[0].topic)
	require.Len(t, inner.calls[0].msgs, 1)
	assert.Equal(t, "uuid-1", inner.calls[0].msgs[0].UUID)
	assert.Equal(t, 0, fallback.callCount(), "configured handler must not use the fallback DLQ")
}

func TestRoutingDLQPublisher_HandlerNotRegistered_UsesFallback(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	// No Register call for this handler.

	err := pub.Publish("_dlq_placeholder", poisonedMsg("some_other_handler"))
	require.NoError(t, err)

	assert.Equal(t, 0, inner.callCount(), "unregistered handler must not hit a per-consumer DLQ topic")
	require.Equal(t, 1, fallback.callCount(), "unregistered handler must fall back to the shared DLQ")
	assert.Equal(t, testFallbackTopic, fallback.calls[0].topic)
}

func TestRoutingDLQPublisher_EmptyDLQTopic_UsesFallback(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	pub.Register("event_consumption_handler", "v1_event_processing", "") // per-consumer DLQ not configured

	err := pub.Publish("_dlq_placeholder", poisonedMsg("event_consumption_handler"))
	require.NoError(t, err)

	assert.Equal(t, 0, inner.callCount())
	require.Equal(t, 1, fallback.callCount(), "empty DLQ topic must fall back to the shared DLQ")
	assert.Equal(t, testFallbackTopic, fallback.calls[0].topic)
}

func TestRoutingDLQPublisher_MultipleMessages_RoutedIndividually(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	pub.Register("event_consumption_handler", "v1_event_processing", "v1_event_processing.dlq")
	pub.Register("event_consumption_lazy_handler", "v1_event_processing_lazy", "v1_event_processing_lazy.dlq")

	err := pub.Publish("_dlq_placeholder",
		poisonedMsg("event_consumption_handler"),
		poisonedMsg("event_consumption_lazy_handler"),
		poisonedMsg("unregistered_handler"),
	)
	require.NoError(t, err)

	require.Equal(t, 2, inner.callCount())
	assert.Equal(t, "v1_event_processing.dlq", inner.calls[0].topic)
	assert.Equal(t, "v1_event_processing_lazy.dlq", inner.calls[1].topic)

	require.Equal(t, 1, fallback.callCount(), "unregistered handler goes to the fallback DLQ")
	assert.Equal(t, testFallbackTopic, fallback.calls[0].topic)
}

func TestRoutingDLQPublisher_PublishError_Propagated(t *testing.T) {
	inner := &fakePublisher{publishErr: assert.AnError}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	pub.Register("event_consumption_handler", "v1_event_processing", "v1_event_processing.dlq")

	err := pub.Publish("_dlq_placeholder", poisonedMsg("event_consumption_handler"))
	assert.Error(t, err)
}

func TestRoutingDLQPublisher_FallbackError_Propagated(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{publishErr: assert.AnError}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)
	// Unregistered handler → fallback path → error must propagate.

	err := pub.Publish("_dlq_placeholder", poisonedMsg("unregistered_handler"))
	assert.Error(t, err)
}

func TestRoutingDLQPublisher_Close_ClosesInnerAndDistinctFallback(t *testing.T) {
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	pub := newRoutingDLQPublisher(inner, fallback, testFallbackTopic, newTestLogger(t), nil)

	require.NoError(t, pub.Close())
	assert.True(t, inner.closed)
	assert.True(t, fallback.closed, "distinct fallback publisher must also be closed")
}

func TestRoutingDLQPublisher_Close_SharedFallbackClosedOnce(t *testing.T) {
	inner := &fakePublisher{}
	// fallback == inner (the kafka.topic_dlq case reuses the Kafka publisher).
	pub := newRoutingDLQPublisher(inner, inner, testFallbackTopic, newTestLogger(t), nil)

	require.NoError(t, pub.Close())
	assert.Equal(t, 1, inner.closeCount(), "shared publisher must be closed exactly once")
}
