package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/payment"
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

				ctx, cancel := context.WithCancel(context.Background())
				if sc.inTx {
					ctx = types.WithPostCommitHooks(ctx)
					ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
				}
				for i := 0; i < sc.calls; i++ {
					publish(ctx)
				}
				assert.Len(t, wh.Events(), sc.wantBefore)

				cancel()
				if sc.finish != nil {
					sc.finish(ctx)
				}
				assert.Len(t, wh.Events(), sc.wantAfter)

				for _, got := range wh.ctxs {
					_, hasTx := got.Value(types.CtxDBTransaction).(*ent.Tx)
					assert.False(t, hasTx, "publisher ctx must not carry a finished tx")
					assert.NoError(t, got.Err(), "publisher ctx must outlive the request")
				}
			})
		}
	}
}

type flakyPaymentStore struct {
	*testutil.InMemoryPaymentStore
	updates  int
	failOnNo int
}

func (s *flakyPaymentStore) Update(ctx context.Context, p *payment.Payment) error {
	s.updates++
	if s.updates == s.failOnNo {
		return errors.New("update failed")
	}
	return s.InMemoryPaymentStore.Update(ctx, p)
}

type statusRecordingPublisher struct {
	*testutil.InMemoryWebhookPublisher
	store    *flakyPaymentStore
	statuses []types.PaymentStatus
}

func (p *statusRecordingPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	stored, err := p.store.InMemoryPaymentStore.Get(ctx, event.EntityID)
	if err != nil {
		return err
	}
	p.statuses = append(p.statuses, stored.PaymentStatus)
	return p.InMemoryWebhookPublisher.PublishWebhook(ctx, event)
}

func TestProcessPaymentPublishesOutcomeAfterUpdate(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	assert.NoError(t, err)

	tests := []struct {
		name         string
		method       types.PaymentMethodType
		failOnNo     int
		wantEvents   []types.WebhookEventName
		wantStatuses []types.PaymentStatus
	}{
		{
			name:         "payment link pending publishes after store",
			method:       types.PaymentMethodTypePaymentLink,
			wantEvents:   []types.WebhookEventName{types.WebhookEventPaymentPending},
			wantStatuses: []types.PaymentStatus{types.PaymentStatusPending},
		},
		{
			name:     "payment link failed update publishes nothing",
			method:   types.PaymentMethodTypePaymentLink,
			failOnNo: 1,
		},
		{
			name:         "offline success publishes after store",
			method:       types.PaymentMethodTypeOffline,
			wantEvents:   []types.WebhookEventName{types.WebhookEventPaymentPending, types.WebhookEventPaymentSuccess},
			wantStatuses: []types.PaymentStatus{types.PaymentStatusProcessing, types.PaymentStatusSucceeded},
		},
		{
			name:         "offline failed outcome update skips success",
			method:       types.PaymentMethodTypeOffline,
			failOnNo:     2,
			wantEvents:   []types.WebhookEventName{types.WebhookEventPaymentPending},
			wantStatuses: []types.PaymentStatus{types.PaymentStatusProcessing},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := &flakyPaymentStore{InMemoryPaymentStore: testutil.NewInMemoryPaymentStore(), failOnNo: tt.failOnNo}
			assert.NoError(t, store.Create(ctx, &payment.Payment{
				ID:                "pay_1",
				PaymentMethodType: tt.method,
				PaymentStatus:     types.PaymentStatusPending,
			}))
			wh := &statusRecordingPublisher{InMemoryWebhookPublisher: testutil.NewInMemoryWebhookPublisher(), store: store}
			p := &paymentProcessor{ServiceParams: ServiceParams{Logger: log, PaymentRepo: store, WebhookPublisher: wh}}

			_, err := p.ProcessPayment(ctx, "pay_1")
			if tt.failOnNo > 0 {
				assert.Error(t, err)
			}

			var gotEvents []types.WebhookEventName
			for _, e := range wh.Events() {
				gotEvents = append(gotEvents, e.EventName)
			}
			assert.Equal(t, tt.wantEvents, gotEvents)
			assert.Equal(t, tt.wantStatuses, wh.statuses)
		})
	}
}
