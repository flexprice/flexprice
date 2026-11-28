package publisher

import (
	"context"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type recordingProducer struct {
	messages []*message.Message
}

func (r *recordingProducer) Publish(_ string, msgs ...*message.Message) error {
	r.messages = append(r.messages, msgs...)
	return nil
}

func TestPublishWebhook_WaitsForCommit(t *testing.T) {
	tests := []struct {
		name          string
		inTransaction bool
		finish        func(ctx context.Context)
		wantBefore    int
		wantAfter     int
	}{
		{
			name:          "sends after the transaction commits",
			inTransaction: true,
			finish:        types.RunPostCommitHooks,
			wantBefore:    0,
			wantAfter:     1,
		},
		{
			name:          "drops the message when the transaction rolls back",
			inTransaction: true,
			finish:        types.DiscardPostCommitHooks,
			wantBefore:    0,
			wantAfter:     0,
		},
		{
			name:       "sends immediately outside a transaction",
			finish:     func(context.Context) {},
			wantBefore: 1,
			wantAfter:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			producer := &recordingProducer{}
			p, err := NewPublisherFromProducer(producer, &config.Configuration{}, logger.NewNoopLogger(), nil)
			require.NoError(t, err)

			ctx := context.Background()
			if tt.inTransaction {
				ctx = types.WithPostCommitHooks(ctx)
			}

			event := &types.WebhookEvent{ID: "sys_event_test", EventName: types.WebhookEventWalletTransactionCreated}
			require.NoError(t, p.PublishWebhook(ctx, event))
			require.Len(t, producer.messages, tt.wantBefore)

			tt.finish(ctx)
			require.Len(t, producer.messages, tt.wantAfter)
		})
	}
}
