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
		h.absorbDeliveryError("native", nil, &types.WebhookEvent{EventName: "x"}, "mid")
	})
}

func TestAbsorbDeliveryError_MissingEntityUsesSkipLogSemantics(t *testing.T) {
	t.Parallel()

	h := &handler{logger: testLogger(t)}
	missing := ierr.NewError("invoice not found").Mark(ierr.ErrNotFound)
	require.NotPanics(t, func() {
		h.absorbDeliveryError("native", missing, &types.WebhookEvent{
			TenantID:  "ten_1",
			EventName: types.WebhookEventInvoiceUpdateFinalized,
		}, "mid")
	})
}
