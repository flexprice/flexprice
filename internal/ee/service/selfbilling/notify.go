package selfbilling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/types"
)

// alertTimeout bounds the detached delivery of one Zapier alert.
const alertTimeout = 30 * time.Second

type alertEvent string

const (
	alertTrialStarted    alertEvent = "trial_started"
	alertTenantActivated alertEvent = "tenant_activated"
	alertTenantSuspended alertEvent = "tenant_suspended"
)

// alert is the JSON body posted to the Zapier catch hook.
type alert struct {
	Event      alertEvent                 `json:"event"`
	TenantID   string                     `json:"tenant_id"`
	TenantName string                     `json:"tenant_name"`
	Region     string                     `json:"region"`
	FromStatus types.TenantInternalStatus `json:"from_status,omitempty"`
	ToStatus   types.TenantInternalStatus `json:"to_status,omitempty"`
	Reason     suspensionReason           `json:"reason,omitempty"`
	OccurredAt time.Time                  `json:"occurred_at"`
}

// notify posts the alert to the Zapier webhook in the background; delivery failures are only logged.
func (s *selfBillingService) notify(ctx context.Context, a alert) {
	hookURL := s.Config.Billing.ZapierWebhookURL
	if hookURL == "" {
		return
	}

	if !s.Config.Billing.HasSecureZapierWebhookURL() {
		s.Logger.Error(ctx, "self-billing alert skipped", "error", "zapier_webhook_url must be https", "event", a.Event, "tenant_id", a.TenantID)
		return
	}

	a.Region = s.Config.Billing.Region
	a.OccurredAt = types.Now()

	detached := context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(detached, alertTimeout)
		defer cancel()

		defer func() {
			if r := recover(); r != nil {
				s.Logger.Error(ctx, "self-billing alert panicked", "error", fmt.Errorf("panic: %v", r), "event", a.Event, "tenant_id", a.TenantID)
			}
		}()

		if err := postAlert(ctx, hookURL, a); err != nil {
			s.Logger.Error(ctx, "failed to send self-billing alert", "error", err, "event", a.Event, "tenant_id", a.TenantID)
		}
	}()
}

// postAlert sends with a plain, untraced client: the hook URL is a secret, and traced spans record url.full.
// Errors never carry the URL.
func postAlert(ctx context.Context, hookURL string, a alert) error {
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid zapier_webhook_url")
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout:       alertTimeout,
		CheckRedirect: httpclient.RejectRedirects,
	}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return urlErr.Err
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("zapier responded with status %d", resp.StatusCode)
	}
	return nil
}
