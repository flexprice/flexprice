package e2eprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

func NewSlackReporter(webhookURL, channel string, client *http.Client, lg *logger.Logger) Reporter {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &slackReporter{webhookURL: webhookURL, channel: channel, client: client, lg: lg}
}

type slackReporter struct {
	webhookURL string
	channel    string
	client     *http.Client
	lg         *logger.Logger
}

func (s *slackReporter) Report(ctx context.Context, r FailureReport) {
	body := map[string]any{"text": formatSlack(r)}
	if s.channel != "" {
		body["channel"] = s.channel
	}
	buf, err := json.Marshal(body)
	if err != nil {
		s.logWarn("marshal", err, r.CheckName)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(buf))
	if err != nil {
		s.logWarn("build_request", err, r.CheckName)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.logWarn("transport", err, r.CheckName)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logWarn("non_2xx", fmt.Errorf("status %d", resp.StatusCode), r.CheckName)
	}
}

func (s *slackReporter) logWarn(step string, err error, check string) {
	if s.lg == nil {
		return
	}
	s.lg.Warnw("slack reporter delivery failed", "step", step, "error", err.Error(), "check", check)
}

func formatSlack(r FailureReport) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, ":rotating_light: *e2eprobe.check.failed*\n")
	fmt.Fprintf(&b, "check: `%s` (%s)\n", r.CheckName, r.CheckKind)
	if r.Step != "" {
		fmt.Fprintf(&b, "step: `%s`\n", r.Step)
	}
	if r.RunID != "" {
		fmt.Fprintf(&b, "run_id: `%s`\n", r.RunID)
	}
	for k, v := range r.Attributes {
		fmt.Fprintf(&b, "%s: `%s`\n", k, v)
	}
	if r.Err != nil {
		fmt.Fprintf(&b, "error: ```%s```", r.Err.Error())
	}
	return b.String()
}
