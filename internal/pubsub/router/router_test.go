package router

import (
	"testing"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRouter builds a Router around a real watermill router and a fake DLQ
// publisher, avoiding any Kafka connection so handler registration can be tested
// in isolation.
func newTestRouter(t *testing.T) (*Router, *fakePublisher) {
	t.Helper()
	wmRouter, err := message.NewRouter(message.RouterConfig{}, watermill.NewStdLogger(false, false))
	require.NoError(t, err)

	log := newTestLogger(t)
	inner := &fakePublisher{}
	fallback := &fakePublisher{}
	dlqRouter := newRoutingDLQPublisher(inner, fallback, "events_dlq", log, nil)

	return &Router{
		router:    wmRouter,
		logger:    log,
		tracing:   nil,
		dlqRouter: dlqRouter,
	}, inner
}

func TestAddNoPublishHandlerWithDLQ_RegistersRoute(t *testing.T) {
	r, _ := newTestRouter(t)

	r.AddNoPublishHandlerWithDLQ(
		"event_consumption_handler",
		"events",
		"v1_event_processing",
		"v1_event_processing.dlq",
		nil,
		func(*message.Message) error { return nil },
	)

	dlqTopic, ok := r.dlqRouter.routes["event_consumption_handler"]
	require.True(t, ok, "handler should be registered in the DLQ routing map")
	assert.Equal(t, "v1_event_processing.dlq", dlqTopic)
	assert.Equal(t, "v1_event_processing", r.dlqRouter.consumerGroups["event_consumption_handler"])
}

func TestAddNoPublishHandler_DoesNotRegisterRoute(t *testing.T) {
	r, _ := newTestRouter(t)

	r.AddNoPublishHandler(
		"plain_handler",
		"events",
		nil,
		func(*message.Message) error { return nil },
	)

	_, ok := r.dlqRouter.routes["plain_handler"]
	assert.False(t, ok, "AddNoPublishHandler callers must not be opted into DLQ routing")
}

func TestAddNoPublishHandlerWithDLQ_EmptyTopicStillRegistered(t *testing.T) {
	r, _ := newTestRouter(t)

	r.AddNoPublishHandlerWithDLQ(
		"event_consumption_replay_handler",
		"events_backfill",
		"v1_event_processing_replay",
		"", // DLQ disabled for this env
		nil,
		func(*message.Message) error { return nil },
	)

	dlqTopic, ok := r.dlqRouter.routes["event_consumption_replay_handler"]
	require.True(t, ok)
	assert.Equal(t, "", dlqTopic)
}
