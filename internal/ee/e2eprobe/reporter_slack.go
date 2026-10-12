package e2eprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

const slackPostMessageURL = "https://slack.com/api/chat.postMessage"

// NewSlackReporter posts failures to an incoming-webhook URL.
func NewSlackReporter(webhookURL, channel string, client *http.Client, lg *logger.Logger) Reporter {
	return &slackReporter{endpoint: webhookURL, channel: channel, client: slackHTTPClient(client), lg: lg}
}

// NewSlackBotReporter posts failures via chat.postMessage using a bot token.
// channel is required: unlike a webhook, a bot token has no default channel.
func NewSlackBotReporter(botToken, channel string, client *http.Client, lg *logger.Logger) Reporter {
	return &slackReporter{endpoint: slackPostMessageURL, botToken: botToken, channel: channel, client: slackHTTPClient(client), lg: lg}
}

func slackHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 5 * time.Second}
	}
	return client
}

type slackReporter struct {
	endpoint string
	botToken string
	channel  string
	client   *http.Client
	lg       *logger.Logger
}

func (s *slackReporter) Report(ctx context.Context, r FailureReport) {
	body := map[string]any{"text": formatSlack(r)}
	if s.channel != "" {
		body["channel"] = s.channel
	}
	buf, err := json.Marshal(body)
	if err != nil {
		s.logErr(ctx, "marshal", err, r.CheckName)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(buf))
	if err != nil {
		s.logErr(ctx, "build_request", err, r.CheckName)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.botToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.botToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.logErr(ctx, "transport", err, r.CheckName)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logErr(ctx, "non_2xx", fmt.Errorf("status %d", resp.StatusCode), r.CheckName)
		return
	}
	// chat.postMessage returns 200 with {"ok":false,"error":...} on logical failure.
	if s.botToken != "" {
		var ack struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
			s.logErr(ctx, "decode_ack", err, r.CheckName)
			return
		}
		if !ack.OK {
			s.logErr(ctx, "slack_api", fmt.Errorf("slack error %q", ack.Error), r.CheckName)
		}
	}
}

func (s *slackReporter) logErr(ctx context.Context, step string, err error, check string) {
	if s.lg == nil {
		return
	}
	s.lg.Error(ctx, "slack reporter delivery failed", "error", err.Error(), "step", step, "check", check)
}

// slackHiddenAttrs stay in logs and OTEL but are noise in an alert.
var slackHiddenAttrs = map[string]bool{
	"run_id": true, "tenant_id": true, "environment_id": true, "step": true, "provider": true,
	"status_code": true, "error_body": true, "failed_legs": true,
}

func formatSlack(r FailureReport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(":rotating_light: *%s* failed", r.CheckName))
	if tenant := r.Attributes["tenant_id"]; tenant != "" {
		b.WriteString(" · " + tenant)
	}
	var parts []string
	step := r.Attributes["step"]
	if step == "" {
		step = r.Step
	}
	if step != "" && step != "run" {
		parts = append(parts, fmt.Sprintf("step `%s`", step))
	}
	keys := make([]string, 0, len(r.Attributes))
	for k := range r.Attributes {
		if !slackHiddenAttrs[k] && !strings.HasPrefix(k, "leg.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s `%s`", k, r.Attributes[k]))
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	if r.Err != nil {
		b.WriteString("\n```" + Brief(r.Err) + "```")
	}
	return b.String()
}
