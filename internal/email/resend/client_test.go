package resend

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/email/emailtypes"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"
)

// Test-only signing secrets, built at runtime so secret scanners do not flag them.
var (
	testSecret  = "whsec_" + base64.StdEncoding.EncodeToString([]byte("invoice-email-test-signing-key"))
	otherSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("some-other-signing-key"))
)

func signedHeaders(t *testing.T, secret string, body []byte, at time.Time) http.Header {
	wh, err := svix.NewWebhook(secret)
	require.NoError(t, err)
	sig, err := wh.Sign("msg_1", at, body)
	require.NoError(t, err)
	return http.Header{
		"Svix-Id":        {"msg_1"},
		"Svix-Timestamp": {strconv.FormatInt(at.Unix(), 10)},
		"Svix-Signature": {sig},
	}
}

func TestParseEvents(t *testing.T) {
	b, err := New(Config{WebhookSecret: testSecret})
	require.NoError(t, err)

	tests := []struct {
		file   string
		status types.EmailDeliveryStatus
		reason string
	}{
		{"email.sent.json", types.EmailDeliveryStatusSent, ""},
		{"email.delivery_delayed.json", types.EmailDeliveryStatusDeliveryDelayed, ""},
		{"email.delivered.json", types.EmailDeliveryStatusDelivered, ""},
		{"email.bounced.json", types.EmailDeliveryStatusBounced, "550 5.1.1 The email account that you tried to reach does not exist"},
		{"email.complained.json", types.EmailDeliveryStatusComplained, ""},
		{"email.failed.json", types.EmailDeliveryStatusFailed, "reached_daily_quota"},
		{"email.suppressed.json", types.EmailDeliveryStatusSuppressed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", tt.file))
			require.NoError(t, err)

			events, err := b.ParseEvents(context.Background(), body, signedHeaders(t, testSecret, body, time.Now()))
			require.NoError(t, err)
			require.Len(t, events, 1)
			assert.Equal(t, "4ef9a417-02e9-4d39-ad75-9611e0fcc33c", events[0].ProviderMessageID)
			assert.Equal(t, "us", events[0].Region)
			assert.Equal(t, "tenant_01jx", events[0].TenantID)
			assert.Equal(t, "env_01jx", events[0].EnvironmentID)
			assert.Equal(t, "email_01jx", events[0].EmailLogID)
			assert.Equal(t, tt.status, events[0].Status)
			assert.Equal(t, tt.reason, events[0].Reason)
		})
	}
}

func TestParseEvents_Rejects(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "email.delivered.json"))
	require.NoError(t, err)

	b, err := New(Config{WebhookSecret: testSecret})
	require.NoError(t, err)

	t.Run("wrong secret", func(t *testing.T) {
		hdr := signedHeaders(t, otherSecret, body, time.Now())
		_, err := b.ParseEvents(context.Background(), body, hdr)
		assert.True(t, ierr.IsPermissionDenied(err))
	})
	t.Run("stale timestamp", func(t *testing.T) {
		hdr := signedHeaders(t, testSecret, body, time.Now().Add(-10*time.Minute))
		_, err := b.ParseEvents(context.Background(), body, hdr)
		assert.True(t, ierr.IsPermissionDenied(err))
	})
	t.Run("no secret configured", func(t *testing.T) {
		unconfigured, err := New(Config{})
		require.NoError(t, err)
		_, err = unconfigured.ParseEvents(context.Background(), body, signedHeaders(t, testSecret, body, time.Now()))
		assert.True(t, ierr.IsPermissionDenied(err))
	})
	t.Run("unknown event type is ignored", func(t *testing.T) {
		opened := []byte(`{"type":"email.opened","data":{"email_id":"x"}}`)
		events, err := b.ParseEvents(context.Background(), opened, signedHeaders(t, testSecret, opened, time.Now()))
		require.NoError(t, err)
		assert.Empty(t, events)
	})
}

func TestSend_ClassifiesErrors(t *testing.T) {
	tests := []struct {
		status    int
		permanent bool
	}{
		{http.StatusUnprocessableEntity, true},
		{http.StatusForbidden, true},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"nope"}`))
			}))
			defer srv.Close()

			p, err := New(Config{APIKey: "re_test"})
			require.NoError(t, err)
			p.(*backend).client.BaseURL = mustParse(t, srv.URL+"/")

			_, err = p.Send(context.Background(), emailtypes.OutboundEmail{IdempotencyKey: "email_1", From: "a@x.io", To: []string{"b@x.io"}, Subject: "s", HTML: "h"})
			require.Error(t, err)
			assert.Equal(t, tt.permanent, ierr.IsValidation(err))
		})
	}
}

func TestSend_ReturnsProviderID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "email_1", r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"re_123"}`))
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "re_test"})
	require.NoError(t, err)
	p.(*backend).client.BaseURL = mustParse(t, srv.URL+"/")

	res, err := p.Send(context.Background(), emailtypes.OutboundEmail{IdempotencyKey: "email_1", From: "a@x.io", To: []string{"b@x.io"}, Subject: "s", HTML: "h"})
	require.NoError(t, err)
	assert.Equal(t, "re_123", res.ProviderMessageID)
}

func mustParse(t *testing.T, raw string) *url.URL {
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
