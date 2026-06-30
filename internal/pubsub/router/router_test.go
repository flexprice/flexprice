package router

import (
	"errors"
	"sync"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/logger"
)

// fakePublisher is a thread-safe test double for message.Publisher.
type fakePublisher struct {
	mu       sync.Mutex
	err      error
	calls    int
	topics   []string
	messages []*message.Message
}

func (f *fakePublisher) Publish(topic string, msgs ...*message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.topics = append(f.topics, topic)
	f.messages = append(f.messages, msgs...)
	return nil
}

func (f *fakePublisher) Close() error { return nil }

func newMsg() *message.Message {
	return message.NewMessage("test-uuid", []byte(`{}`))
}

func noopLog() *logger.Logger { return logger.NewNoopLogger() }

// --- DynamicPoisonQueue ---

func TestDynamicPoisonQueue_SuccessPassthrough(t *testing.T) {
	dlq := &fakePublisher{}
	mw := DynamicPoisonQueue(dlq, noopLog())

	_, err := mw(func(msg *message.Message) ([]*message.Message, error) {
		return nil, nil
	})(newMsg())

	if err != nil {
		t.Fatalf("expected nil on success, got %v", err)
	}
	if dlq.calls != 0 {
		t.Fatalf("expected no DLQ publish on success, got %d calls", dlq.calls)
	}
}

func TestDynamicPoisonQueue_NoDLQTopic_AcksSilently(t *testing.T) {
	dlq := &fakePublisher{}
	mw := DynamicPoisonQueue(dlq, noopLog())

	// No dlq_topic metadata → should ack silently
	_, err := mw(func(msg *message.Message) ([]*message.Message, error) {
		return nil, errors.New("handler error")
	})(newMsg())

	if err != nil {
		t.Fatalf("expected nil (silent ack), got %v", err)
	}
	if dlq.calls != 0 {
		t.Fatalf("expected no DLQ publish when dlq_topic unset, got %d calls", dlq.calls)
	}
}

func TestDynamicPoisonQueue_WithDLQTopic_PublishesToTopic(t *testing.T) {
	dlq := &fakePublisher{}
	mw := DynamicPoisonQueue(dlq, noopLog())

	handlerErr := errors.New("processing failed")
	msg := newMsg()
	msg.Metadata.Set("dlq_topic", "events_consumer_dlq")

	_, err := mw(func(msg *message.Message) ([]*message.Message, error) {
		return nil, handlerErr
	})(msg)

	if err != nil {
		t.Fatalf("expected nil after DLQ publish, got %v", err)
	}

	dlq.mu.Lock()
	defer dlq.mu.Unlock()

	if dlq.calls != 1 {
		t.Fatalf("expected 1 DLQ publish, got %d", dlq.calls)
	}
	if dlq.topics[0] != "events_consumer_dlq" {
		t.Fatalf("expected topic 'events_consumer_dlq', got %q", dlq.topics[0])
	}
}

func TestDynamicPoisonQueue_PoisonMetadataStamped(t *testing.T) {
	dlq := &fakePublisher{}
	mw := DynamicPoisonQueue(dlq, noopLog())

	handlerErr := errors.New("boom")
	msg := newMsg()
	msg.Metadata.Set("dlq_topic", "events_consumer_dlq")

	mw(func(msg *message.Message) ([]*message.Message, error) { //nolint:errcheck
		return nil, handlerErr
	})(msg) //nolint:errcheck

	dlq.mu.Lock()
	defer dlq.mu.Unlock()

	if len(dlq.messages) == 0 {
		t.Fatal("expected a message in DLQ")
	}
	if got := dlq.messages[0].Metadata.Get(middleware.ReasonForPoisonedKey); got != handlerErr.Error() {
		t.Fatalf("ReasonForPoisonedKey: want %q, got %q", handlerErr.Error(), got)
	}
}

func TestDynamicPoisonQueue_PublishFails_PropagatesError(t *testing.T) {
	dlq := &fakePublisher{err: errors.New("kafka unavailable")}
	mw := DynamicPoisonQueue(dlq, noopLog())

	msg := newMsg()
	msg.Metadata.Set("dlq_topic", "events_consumer_dlq")

	_, err := mw(func(msg *message.Message) ([]*message.Message, error) {
		return nil, errors.New("handler error")
	})(msg)

	if err == nil {
		t.Fatal("expected error when DLQ publish fails, got nil")
	}
}

// --- stamp middleware (AddNoPublishHandlerWithDLQ) ---

func TestStampMiddleware_SetsDLQTopicBeforeHandlerRuns(t *testing.T) {
	const dlqTopic = "events_consumer_dlq"

	var capturedTopic string
	handler := func(msg *message.Message) ([]*message.Message, error) {
		capturedTopic = msg.Metadata.Get("dlq_topic")
		return nil, nil
	}

	// Mirror the closure created inside AddNoPublishHandlerWithDLQ.
	stamp := func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			msg.Metadata.Set("dlq_topic", dlqTopic)
			return h(msg)
		}
	}

	if _, err := stamp(handler)(newMsg()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTopic != dlqTopic {
		t.Fatalf("expected dlq_topic=%q inside handler, got %q", dlqTopic, capturedTopic)
	}
}

func TestDynamicPoisonQueue_ReadsTopicSetByStampMiddleware(t *testing.T) {
	const dlqTopic = "events_consumer_dlq"

	dlq := &fakePublisher{}

	// Chain: DynamicPoisonQueue(outermost) → stamp → alwaysFails
	// This mirrors the real runtime chain after retries are exhausted.
	poison := DynamicPoisonQueue(dlq, noopLog())
	stamp := func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			msg.Metadata.Set("dlq_topic", dlqTopic)
			return h(msg)
		}
	}
	alwaysFails := func(msg *message.Message) ([]*message.Message, error) {
		return nil, errors.New("boom")
	}

	_, err := poison(stamp(alwaysFails))(newMsg())

	if err != nil {
		t.Fatalf("expected nil (acked after DLQ publish), got %v", err)
	}

	dlq.mu.Lock()
	defer dlq.mu.Unlock()

	if dlq.calls != 1 {
		t.Fatalf("expected 1 DLQ publish, got %d", dlq.calls)
	}
	if dlq.topics[0] != dlqTopic {
		t.Fatalf("expected topic %q, got %q", dlqTopic, dlq.topics[0])
	}
}
