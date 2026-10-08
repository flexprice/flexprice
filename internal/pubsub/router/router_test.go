package router

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func TestAddNoPublishHandler_DeadLettering(t *testing.T) {
	tests := []struct {
		name         string
		handlerErr   error
		loseSession  bool
		detachMsgCtx bool
		wantDLQ      bool
		wantAck      bool
	}{
		{
			name:        "session lost during retry backoff is nacked, not dead-lettered",
			handlerErr:  errors.New("transient failure"),
			loseSession: true,
			wantDLQ:     false,
			wantAck:     false,
		},
		{
			name:         "session lost is still detected when the handler replaces msg ctx",
			handlerErr:   errors.New("transient failure"),
			loseSession:  true,
			detachMsgCtx: true,
			wantDLQ:      false,
			wantAck:      false,
		},
		{
			name:       "handler returning its own context.Canceled is dead-lettered after retries",
			handlerErr: context.Canceled,
			wantDLQ:    true,
			wantAck:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dlq := &recordingPublisher{}
			sub := &singleMessageSubscriber{ch: make(chan *message.Message, 1)}
			r := newTestRouter(t, dlq)

			var attempts atomic.Int32
			firstAttemptDone := make(chan struct{})
			r.AddNoPublishHandler("test_handler", "events", "events_dlq", sub,
				func(ctx context.Context, msg *message.Message) error {
					if tt.detachMsgCtx {
						msg.SetContext(context.Background())
					}
					if attempts.Add(1) == 1 {
						close(firstAttemptDone)
					}
					return tt.handlerErr
				},
			)
			runRouter(t, r)

			msgCtx, revokeSession := context.WithCancel(context.Background())
			defer revokeSession()
			msg := message.NewMessage(watermill.NewUUID(), []byte(`{}`))
			msg.SetContext(msgCtx)
			sub.ch <- msg

			<-firstAttemptDone
			if tt.loseSession {
				// Retry is now in its ~1s backoff; a rebalance/shutdown cancels the message ctx.
				revokeSession()
			}

			select {
			case <-msg.Acked():
				require.True(t, tt.wantAck, "message was acked")
			case <-msg.Nacked():
				require.False(t, tt.wantAck, "message was nacked")
			case <-time.After(30 * time.Second):
				t.Fatal("message was neither acked nor nacked")
			}
			require.Equal(t, tt.wantDLQ, dlq.count() > 0, "dead-lettered")
		})
	}
}

func newTestRouter(t *testing.T, dlq message.Publisher) *Router {
	t.Helper()
	wr, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
	require.NoError(t, err)
	wr.AddMiddleware(consumerContextMiddleware(nil), middleware.Recoverer, middleware.CorrelationID)
	return &Router{router: wr, logger: logger.NewNoopLogger(), dlqPublisher: dlq}
}

func runRouter(t *testing.T, r *Router) {
	t.Helper()
	go func() { _ = r.Run() }()
	t.Cleanup(func() { _ = r.Close() })
	<-r.router.Running()
}

type recordingPublisher struct {
	mu   sync.Mutex
	msgs []*message.Message
}

func (p *recordingPublisher) Publish(_ string, msgs ...*message.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msgs...)
	return nil
}

func (p *recordingPublisher) Close() error { return nil }

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.msgs)
}

type singleMessageSubscriber struct {
	ch    chan *message.Message
	close sync.Once
}

func (s *singleMessageSubscriber) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return s.ch, nil
}

func (s *singleMessageSubscriber) Close() error {
	s.close.Do(func() { close(s.ch) })
	return nil
}
