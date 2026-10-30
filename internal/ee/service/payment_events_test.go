package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

type ctxCapturingPublisher struct {
	*testutil.InMemoryWebhookPublisher
	ctxs []context.Context
}

func (p *ctxCapturingPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	p.ctxs = append(p.ctxs, ctx)
	return p.InMemoryWebhookPublisher.PublishWebhook(ctx, event)
}

func TestPaymentEventsPublishAfterCommit(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	assert.NoError(t, err)

	publishers := map[string]func(ServiceParams) func(context.Context){
		"service": func(p ServiceParams) func(context.Context) {
			s := &paymentService{ServiceParams: p}
			return func(ctx context.Context) {
				s.publishSystemEvent(ctx, types.WebhookEventPaymentCreated, "pay_1")
			}
		},
		"processor": func(p ServiceParams) func(context.Context) {
			s := &paymentProcessor{ServiceParams: p}
			return func(ctx context.Context) {
				s.publishSystemEvent(ctx, types.WebhookEventPaymentSuccess, "pay_1")
			}
		},
	}

	scenarios := []struct {
		name       string
		inTx       bool
		calls      int
		finish     func(context.Context)
		wantBefore int
		wantAfter  int
	}{
		{name: "no tx publishes immediately", calls: 1, wantBefore: 1, wantAfter: 1},
		{name: "commit publishes after commit", inTx: true, calls: 1, finish: types.RunPostCommitHooks, wantAfter: 1},
		{name: "rollback publishes nothing", inTx: true, calls: 1, finish: types.DiscardPostCommitHooks},
		{name: "multiple events share one commit", inTx: true, calls: 2, finish: types.RunPostCommitHooks, wantAfter: 2},
	}

	for pubName, build := range publishers {
		for _, sc := range scenarios {
			t.Run(pubName+"/"+sc.name, func(t *testing.T) {
				wh := &ctxCapturingPublisher{InMemoryWebhookPublisher: testutil.NewInMemoryWebhookPublisher()}
				publish := build(ServiceParams{Logger: log, WebhookPublisher: wh})

				ctx := context.Background()
				if sc.inTx {
					ctx = types.WithPostCommitHooks(ctx)
					ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
				}
				for i := 0; i < sc.calls; i++ {
					publish(ctx)
				}
				assert.Len(t, wh.Events(), sc.wantBefore)

				if sc.finish != nil {
					sc.finish(ctx)
				}
				assert.Len(t, wh.Events(), sc.wantAfter)

				for _, got := range wh.ctxs {
					_, hasTx := got.Value(types.CtxDBTransaction).(*ent.Tx)
					assert.False(t, hasTx, "publisher ctx must not carry a finished tx")
				}
			})
		}
	}
}
