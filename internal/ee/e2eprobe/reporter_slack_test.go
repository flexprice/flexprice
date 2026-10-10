package e2eprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

func TestSlackReporter_Posts(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	r := NewSlackReporter(srv.URL, "#syn", nil, nil)
	r.Report(context.Background(), FailureReport{
		CheckName:  "cycle-invoice-probe",
		CheckKind:  KindProbe,
		Step:       "freshness",
		Err:        errors.New("stale"),
		RunID:      "abc",
		Attributes: map[string]string{"sub_id": "sub_x"},
		OccurredAt: time.Now(),
	})
	var p map[string]any
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, gotBody)
	}
	text, _ := p["text"].(string)
	for _, want := range []string{"cycle-invoice-probe", "probe", "sub_x", "stale"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q: %s", want, text)
		}
	}
	if p["channel"] != "#syn" {
		t.Errorf("channel=%v", p["channel"])
	}
}

func TestSlackBotReporter_PostsWithBearer(t *testing.T) {
	var gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	rep := &slackReporter{endpoint: srv.URL, botToken: "xoxb-test", channel: "#syn", client: srv.Client()}
	rep.Report(context.Background(), FailureReport{CheckName: "cycle-invoice-probe", CheckKind: KindProbe})
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("auth=%q", gotAuth)
	}
	var p map[string]any
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["channel"] != "#syn" {
		t.Errorf("channel=%v", p["channel"])
	}
}

func TestSlackBotReporter_SwallowsAPIErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()
	rep := &slackReporter{endpoint: srv.URL, botToken: "xoxb-test", channel: "#x", client: srv.Client()}
	rep.Report(context.Background(), FailureReport{CheckName: "x"})
}

func TestSlackReporter_SwallowsHTTPErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	r := NewSlackReporter(srv.URL, "", nil, nil)
	r.Report(context.Background(), FailureReport{CheckName: "x"})
}

func TestFormatSlack_Terse(t *testing.T) {
	status, msg := int64(500), "Unable to charge the stored token in Razorpay"
	apiErr := &sdkerrors.ErrorResponse{HTTPStatusCode: &status, Message: &msg}
	got := formatSlack(FailureReport{
		CheckName: "payment-autocharge-probe-razorpay",
		Step:      "run",
		RunID:     "e2eprobe-1",
		Err:       Errorf(map[string]string{"step": "topup_autocharge_start"}, "start top-up: %w", apiErr),
		Attributes: map[string]string{
			"tenant_id":             "gcp-staging-as1",
			"environment_id":        "env_1",
			"step":                  "topup_autocharge_start",
			"external_customer_id":  "e2eprobe-cust-pay-razorpay-mandate",
			"failed_legs":           "wallet_topup",
			"leg.wallet_topup.step": "topup_autocharge_start",
			"error_body":            apiErr.Error(),
			"status_code":           "500",
		},
	})
	want := ":rotating_light: *payment-autocharge-probe-razorpay* failed · gcp-staging-as1\n" +
		"step `topup_autocharge_start` · external_customer_id `e2eprobe-cust-pay-razorpay-mandate`\n" +
		"```500 Unable to charge the stored token in Razorpay```"
	if got != want {
		t.Errorf("formatSlack =\n%s\nwant\n%s", got, want)
	}
}
