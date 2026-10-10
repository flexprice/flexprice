package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/payload"
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

// TestDeliverNative_TenantLookupIsCaseInsensitive guards against a regression where
// Viper lowercases YAML map keys but event.TenantID arrives in its original
// ULID uppercase form, causing the tenant config lookup to miss. We simulate
// Viper's post-load state (lowercase key) and pass an uppercase tenant ID;
// the handler must find the config and fail only on the downstream
// "tenant disabled" check, not with ErrNotFound.
func TestDeliverNative_TenantLookupIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	const (
		upperTenantID = "tenant_01KE93H5A5S0S3DMBJT4YP589M"
		lowerTenantID = "tenant_01ke93h5a5s0s3dmbjt4yp589m"
	)

	h := &handler{
		config: &config.Webhook{
			Enabled: true,
			Svix:    config.Svix{Enabled: false},
			Tenants: map[string]config.TenantWebhookConfig{
				lowerTenantID: {Enabled: false, Endpoint: "http://localhost:8080/health"},
			},
		},
		logger: testLogger(t),
	}

	err := h.deliverNative(context.Background(), &types.WebhookEvent{
		ID:            "sev_1",
		TenantID:      upperTenantID,
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerCreated,
		Timestamp:     time.Now().UTC(),
		Payload:       []byte(`{"customer_id":"c1"}`),
	}, "msg-uuid")

	require.Error(t, err)
	require.False(t, ierr.IsNotFound(err), "lookup must succeed case-insensitively; got ErrNotFound: %v", err)
	require.True(t, ierr.IsInvalidOperation(err), "expected ErrInvalidOperation (tenant disabled), got: %v", err)
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

type stubBuilders struct{ err error }

func (f stubBuilders) GetBuilder(types.WebhookEventName) (payload.PayloadBuilder, error) {
	return nil, f.err
}

// Only events the tenant is subscribed to are counted, labelled by delivery outcome.
func TestProcessMessage_CountsSubscribedDeliveries(t *testing.T) {
	r := metricstest.Install(t)
	tests := []struct {
		name     string
		tenant   *config.TenantWebhookConfig
		buildErr error
		outcome  string
		want     int64
	}{
		{name: "tenant not configured", outcome: "error", want: 0},
		{name: "event excluded", tenant: &config.TenantWebhookConfig{Enabled: true, ExcludedEvents: []types.WebhookEventName{types.WebhookEventCustomerCreated}}, outcome: "error", want: 0},
		{name: "subscribed, entity missing", tenant: &config.TenantWebhookConfig{Enabled: true}, buildErr: ierr.NewError("gone").Mark(ierr.ErrNotFound), outcome: "skipped", want: 1},
		{name: "subscribed, build fails", tenant: &config.TenantWebhookConfig{Enabled: true}, buildErr: errors.New("boom"), outcome: "error", want: 1},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantID := fmt.Sprintf("ten_metrics_%d", i)
			tenants := map[string]config.TenantWebhookConfig{}
			if tt.tenant != nil {
				tenants[tenantID] = *tt.tenant
			}
			h := &handler{
				config:  &config.Webhook{Enabled: true, Tenants: tenants},
				factory: stubBuilders{err: tt.buildErr},
				logger:  testLogger(t),
			}
			event, err := json.Marshal(types.WebhookEvent{
				ID:            "sev_metrics",
				TenantID:      tenantID,
				EnvironmentID: "env_metrics",
				EventName:     types.WebhookEventCustomerCreated,
				Timestamp:     time.Now().UTC(),
			})
			require.NoError(t, err)

			require.NoError(t, h.processMessage(context.Background(), message.NewMessage("sev_metrics", event)))
			require.Equal(t, tt.want, r.Sum("webhook.outbound.deliveries", map[string]string{
				"tenant_id": tenantID,
				"transport": "native",
				"outcome":   tt.outcome,
			}))
		})
	}
}
