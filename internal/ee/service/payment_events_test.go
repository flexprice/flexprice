package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

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
			p := &paymentProcessor{paymentService: &paymentService{ServiceParams: ServiceParams{Logger: log, DB: testutil.NewMockPostgresClient(log), SettingsRepo: testutil.NewInMemorySettingsStore(), PaymentRepo: store, WebhookPublisher: wh}}}

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
