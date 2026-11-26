package incident

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

func TestFormatOpened_HidesTenantAndCapsMismatches(t *testing.T) {
	opened := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	mismatches := []string{"a: want 1, got 2", "b: want 1, got 2", "c: want 1, got 2", "d: want 1, got 2", "e: want 1, got 2", "f: want 1, got 2", "g: want 1, got 2"}
	st := BindingState{
		Key:          Key{Scenario: "usage", Persona: "taxed"},
		Consecutive:  4,
		FailingSince: opened,
		Last:         e2eprobe.Mismatch("assert_usage", mismatches, map[string]string{"tenant_id": "t1", "run_id": "r1", "external_customer_id": "e2eprobe-cust-persistent-0", "subscription_id": "sub_1"}),
	}
	inc := Incident{Key: st.Key, Kind: KindFailure, OpenedAt: opened.Add(25 * time.Minute), Summary: st.Last.Summary(), Runs: 4}
	out := FormatOpened(inc, st)
	for _, want := range []string{":rotating_light:", "*usage · taxed*", "failing for 25m", "(4 runs)", "step `assert_usage`", "• a: want 1, got 2", "• e: want 1, got 2", "+2 more", "external_customer_id=e2eprobe-cust-persistent-0", "subscription_id=sub_1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"tenant_id", "run_id", "• f:", "• g:"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q must be hidden in:\n%s", hidden, out)
		}
	}
}

func TestFormatOpened_CoverageAndInfra(t *testing.T) {
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	cov := FormatOpened(Incident{Key: Key{Scenario: "lifecycle", Persona: "baseline-a"}, Kind: KindCoverage, OpenedAt: now.Add(24 * time.Hour), Summary: "persona: not provisioned"},
		BindingState{SkippingSince: now, Last: e2eprobe.Skip("persona", "not provisioned")})
	if !strings.Contains(cov, ":warning:") || !strings.Contains(cov, "skipping for 24h0m0s") || !strings.Contains(cov, "not provisioned") {
		t.Errorf("coverage text:\n%s", cov)
	}
	infra := FormatOpened(Incident{Key: Key{Scenario: "api"}, Kind: KindInfra, OpenedAt: now, Summary: "3 bindings saw infrastructure errors in the last 15m"}, BindingState{})
	if !strings.Contains(infra, "API infrastructure") || !strings.Contains(infra, "3 bindings") {
		t.Errorf("infra text:\n%s", infra)
	}
}

func TestFormatResolved(t *testing.T) {
	opened := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	out := FormatResolved(Incident{Key: Key{Scenario: "usage", Persona: "taxed"}, Kind: KindFailure, OpenedAt: opened}, BindingState{}, opened.Add(95*time.Minute))
	if !strings.Contains(out, ":white_check_mark:") || !strings.Contains(out, "*usage · taxed*") || !strings.Contains(out, "recovered after 1h35m") {
		t.Errorf("resolved text:\n%s", out)
	}
}

func TestSlackNotifier_PostsToWebhook(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		if r.Header.Get("Authorization") != "" {
			t.Error("webhook delivery must not carry a bearer token")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := SlackNotifier(srv.URL, "", "#probes", srv.Client(), nil)
	n.Opened(context.Background(), Incident{Key: Key{Scenario: "usage", Persona: "taxed"}, Kind: KindFailure, Summary: "assert: x"}, BindingState{})
	if got["channel"] != "#probes" || !strings.Contains(got["text"].(string), "usage · taxed") {
		t.Errorf("posted body = %v", got)
	}
}

func TestSlackNotifier_UsesBotTokenAndChannel(t *testing.T) {
	var auth, channel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		channel, _ = body["channel"].(string)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	n := SlackNotifier("https://hooks.example/ignored", "xoxb-token", "C123", srv.Client(), nil).(*slackNotifier)
	n.endpoint = srv.URL
	n.Resolved(context.Background(), Incident{Key: Key{Scenario: "usage"}, Kind: KindFailure}, BindingState{}, time.Now())
	if auth != "Bearer xoxb-token" || channel != "C123" {
		t.Errorf("auth = %q channel = %q", auth, channel)
	}
}
