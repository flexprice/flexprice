package incident

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

// SlackNotifier posts to an incoming webhook, or via chat.postMessage when a
// bot token is set (the token wins and needs a channel).
func SlackNotifier(webhookURL, botToken, channel string, client *http.Client, lg *logger.Logger) Notifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	n := &slackNotifier{endpoint: webhookURL, botToken: botToken, channel: channel, client: client, lg: lg}
	if botToken != "" {
		n.endpoint = slackPostMessageURL
	}
	return n
}

type slackNotifier struct {
	endpoint string
	botToken string
	channel  string
	client   *http.Client
	lg       *logger.Logger
}

func (s *slackNotifier) Opened(ctx context.Context, inc Incident, st BindingState) {
	s.post(ctx, FormatOpened(inc, st))
}

func (s *slackNotifier) Reminded(ctx context.Context, inc Incident, st BindingState) {
	s.post(ctx, FormatReminded(inc, st))
}

func (s *slackNotifier) Resolved(ctx context.Context, inc Incident, st BindingState, closedAt time.Time) {
	s.post(ctx, FormatResolved(inc, st, closedAt))
}

func (s *slackNotifier) Digest(ctx context.Context, d Digest) {
	s.post(ctx, FormatDigest(d))
}

func (s *slackNotifier) post(ctx context.Context, text string) {
	body := map[string]any{"text": text}
	if s.channel != "" {
		body["channel"] = s.channel
	}
	buf, err := json.Marshal(body)
	if err != nil {
		s.logErr(ctx, "marshal", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(buf))
	if err != nil {
		s.logErr(ctx, "build_request", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.botToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.botToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.logErr(ctx, "transport", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logErr(ctx, "non_2xx", fmt.Errorf("status %d", resp.StatusCode))
		return
	}
	if s.botToken == "" {
		return
	}
	// chat.postMessage answers 200 with {"ok":false,"error":...} on a logical failure.
	var ack struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		s.logErr(ctx, "decode_ack", err)
		return
	}
	if !ack.OK {
		s.logErr(ctx, "slack_api", fmt.Errorf("slack error %q", ack.Error))
	}
}

func (s *slackNotifier) logErr(ctx context.Context, step string, err error) {
	if s.lg == nil {
		return
	}
	s.lg.Error(ctx, "slack delivery failed", "error", err.Error(), "step", step)
}

// hiddenAttrs stay in logs but are noise in an alert.
var hiddenAttrs = map[string]bool{"run_id": true, "tenant_id": true, "environment_id": true, "step": true}

const maxMismatchLines = 5

func FormatOpened(inc Incident, st BindingState) string {
	var b strings.Builder
	switch inc.Kind {
	case KindCoverage:
		b.WriteString(fmt.Sprintf(":warning: *%s* skipping for %s · %s", inc.Key, humanDuration(inc.OpenedAt.Sub(st.SkippingSince)), st.Last.Reason))
		return b.String()
	case KindInfra:
		b.WriteString(fmt.Sprintf(":rotating_light: *API infrastructure* · %s", inc.Summary))
		return b.String()
	}
	b.WriteString(fmt.Sprintf(":rotating_light: *%s* failing for %s (%d runs)", inc.Key, humanDuration(inc.OpenedAt.Sub(st.FailingSince)), inc.Runs))
	if st.Last.Step != "" {
		b.WriteString(fmt.Sprintf(" · step `%s`", st.Last.Step))
	}
	lines := st.Last.Mismatches
	if len(lines) == 0 && st.Last.Reason != "" {
		lines = []string{st.Last.Reason}
	}
	for i, line := range lines {
		if i == maxMismatchLines {
			b.WriteString(fmt.Sprintf("\n• +%d more", len(lines)-maxMismatchLines))
			break
		}
		b.WriteString("\n• " + line)
	}
	if attrs := formatAttrs(st.Last.Attrs); attrs != "" {
		b.WriteString("\n" + attrs)
	}
	return b.String()
}

func FormatReminded(inc Incident, _ BindingState) string {
	return fmt.Sprintf(":hourglass_flowing_sand: *%s* still open since %s (%s, %d runs) · %s",
		inc.Key, inc.OpenedAt.UTC().Format(time.RFC3339), humanDuration(inc.LastNotified.Sub(inc.OpenedAt)), inc.Runs, inc.Summary)
}

func FormatResolved(inc Incident, _ BindingState, closedAt time.Time) string {
	return fmt.Sprintf(":white_check_mark: *%s* recovered after %s", inc.Key, humanDuration(closedAt.Sub(inc.OpenedAt)))
}

func formatAttrs(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		if !hiddenAttrs[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+attrs[k])
	}
	return strings.Join(parts, " · ")
}

// humanDuration rounds to minutes once past a minute, to seconds below it.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
