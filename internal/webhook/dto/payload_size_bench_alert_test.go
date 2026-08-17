package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// oldAlertPayload models the counterfactual "naive" webhook payload that embeds the full
// fetched entities directly, instead of extracting just their IDs like
// NewAlertWebhookPayload does. Alert webhooks never actually shipped this shape in
// production -- this exists purely to quantify the cost the flat-ID design avoided.
type oldAlertPayload struct {
	EventType   types.WebhookEventName `json:"event_type"`
	AlertType   types.AlertType        `json:"alert_type"`
	AlertStatus types.AlertState       `json:"alert_status"`
	Feature     *dto.FeatureResponse   `json:"feature,omitempty"`
	Wallet      *dto.WalletResponse    `json:"wallet,omitempty"`
	Customer    *dto.CustomerResponse  `json:"customer,omitempty"`
}

func buildOldAlertPayload() *oldAlertPayload {
	return &oldAlertPayload{
		EventType:   types.WebhookEventFeatureWalletBalanceAlert,
		AlertType:   types.AlertTypeFeatureWalletBalance,
		AlertStatus: types.AlertStateInAlarm,
		Feature:     buildFeatureResponse(),
		Wallet:      buildWalletResponse(),
		Customer:    buildCustomerResponse(3),
	}
}

func BenchmarkAlertPayloadSize(b *testing.B) {
	feat := buildFeatureResponse()
	wlt := buildWalletResponse()
	cust := buildCustomerResponse(3)

	b.Run("fixed/old", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			data, err := json.Marshal(buildOldAlertPayload())
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})

	b.Run("fixed/new", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			payload := NewAlertWebhookPayload(feat, wlt, cust, types.AlertTypeFeatureWalletBalance, types.AlertStateInAlarm, types.WebhookEventFeatureWalletBalanceAlert)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
