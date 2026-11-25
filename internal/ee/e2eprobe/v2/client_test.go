package e2eprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestErrorFromRawHTTPResponse_ClassifiesByStatus(t *testing.T) {
	cases := []struct {
		name  string
		code  int
		body  string
		infra bool
	}{
		{"plain 503", 503, "upstream down", true},
		{"plain 404", 404, "not found", false},
		{"typed 502", 502, `{"http_status_code":502,"message":"bad gateway"}`, true},
		{"typed 400", 400, `{"http_status_code":400,"message":"bad request"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errorFromRawHTTPResponse(tc.code, []byte(tc.body))
			if err == nil || IsInfra(err) != tc.infra {
				t.Fatalf("err = %v, infra = %v, want %v", err, IsInfra(err), tc.infra)
			}
		})
	}
}

func TestAlertOps_QueryLogsDecodesTheServerPayload(t *testing.T) {
	var sent types.AlertLogFilter
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/alerts/search" || r.Header.Get("x-api-key") != "key" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		// value_at_time is a decimal the SDK types as a time; the probe must not depend on that decode.
		_, _ = w.Write([]byte(`{"items":[{"id":"al_1","entity_type":"wallet","entity_id":"w_1","customer_id":"cust_1",` +
			`"alert_type":"low_wallet_balance","alert_status":"in_alarm",` +
			`"alert_info":{"value_at_time":"15","timestamp":"2026-11-25T14:00:00Z"},` +
			`"created_at":"2026-11-25T14:00:00Z"}],"pagination":{"total":1}}`))
	}))
	defer srv.Close()

	entity, walletID := types.AlertEntityTypeWallet, "w_1"
	since := time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC)
	logs, err := NewSDKClient(srv.URL, "key").Alerts().QueryLogs(context.Background(), types.AlertLogFilter{EntityType: &entity, EntityID: &walletID, StartTime: &since})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	want := []AlertLog{{
		ID:          "al_1",
		EntityType:  "wallet",
		EntityID:    "w_1",
		CustomerID:  "cust_1",
		AlertType:   "low_wallet_balance",
		AlertStatus: "in_alarm",
		CreatedAt:   time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	}}
	if !reflect.DeepEqual(logs, want) {
		t.Errorf("logs = %+v, want %+v", logs, want)
	}
	if sent.EntityID == nil || *sent.EntityID != walletID || sent.EntityType == nil || *sent.EntityType != entity || sent.StartTime == nil || !sent.StartTime.Equal(since) {
		t.Errorf("filter sent to the server = %+v", sent)
	}
}
