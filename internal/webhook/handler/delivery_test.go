package handler

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

func TestDeliverWebhook_Disabled(t *testing.T) {
	t.Parallel()

	h := &handler{
		config: &config.Webhook{Enabled: false},
		logger: testLogger(t),
	}

	err := h.DeliverWebhook(context.Background(), &types.WebhookEvent{
		ID:            "sev_1",
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerCreated,
	})
	require.Error(t, err)
	require.True(t, ierr.IsInvalidOperation(err))
}

func TestDeliverWebhook_NilEvent(t *testing.T) {
	t.Parallel()

	h := &handler{
		config: &config.Webhook{Enabled: true},
		logger: testLogger(t),
	}

	err := h.DeliverWebhook(context.Background(), nil)
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))
}

func TestDeliverNative_TenantNotConfigured(t *testing.T) {
	t.Parallel()

	h := &handler{
		config: &config.Webhook{
			Enabled: true,
			Svix:    config.Svix{Enabled: false},
			Tenants: map[string]config.TenantWebhookConfig{},
		},
		logger: testLogger(t),
	}

	err := h.deliverNative(context.Background(), &types.WebhookEvent{
		ID:            "sev_1",
		TenantID:      "ten_unknown",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerCreated,
		Timestamp:     time.Now().UTC(),
		Payload:       []byte(`{"customer_id":"c1"}`),
	}, "msg-uuid")
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err))
}

func TestAbsorbDeliveryError_NilErrNoPanic(t *testing.T) {
	t.Parallel()

	h := &handler{logger: testLogger(t)}
	require.NotPanics(t, func() {
		h.absorbDeliveryError(context.Background(), "native", nil, &types.WebhookEvent{EventName: "x"}, "mid")
	})
}

func TestAbsorbDeliveryError_MissingEntityUsesSkipLogSemantics(t *testing.T) {
	t.Parallel()

	h := &handler{logger: testLogger(t)}
	missing := ierr.NewError("invoice not found").Mark(ierr.ErrNotFound)
	require.NotPanics(t, func() {
		h.absorbDeliveryError(context.Background(), "native", missing, &types.WebhookEvent{
			TenantID:  "ten_1",
			EventName: types.WebhookEventInvoiceUpdateFinalized,
		}, "mid")
	})
}

func TestAbsorbDeliveryError_RealErrorNoPanicWithoutRepo(t *testing.T) {
	t.Parallel()

	h := &handler{
		// systemEventRepo intentionally nil — guards must handle this safely
		logger: testLogger(t),
	}
	deliveryErr := ierr.NewError("connection refused").Mark(ierr.ErrInternal)
	require.NotPanics(t, func() {
		h.absorbDeliveryError(context.Background(), "native", deliveryErr, &types.WebhookEvent{
			ID:        "sev_1",
			TenantID:  "ten_1",
			EventName: types.WebhookEventCustomerCreated,
		}, "mid")
	})
}
