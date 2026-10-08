package e2eprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func formatSlack(r FailureReport) string {
	var b strings.Builder
	b.WriteString(":rotating_light: *e2eprobe.check.failed*\n")
	b.WriteString(fmt.Sprintf("check: `%s` (%s)\n", r.CheckName, r.CheckKind))
	if r.Step != "" {
		b.WriteString(fmt.Sprintf("step: `%s`\n", r.Step))
	}
	if r.RunID != "" {
		b.WriteString(fmt.Sprintf("run_id: `%s`\n", r.RunID))
	}
	for k, v := range r.Attributes {
		b.WriteString(fmt.Sprintf("%s: `%s`\n", k, v))
	}
	if r.Err != nil {
		b.WriteString(fmt.Sprintf("error: ```%s```", r.Err.Error()))
	}
	return b.String()
}
