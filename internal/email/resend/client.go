package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/email/emailtypes"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/resend/resend-go/v2"
	svix "github.com/svix/svix-webhooks/go"
)

const (
	sendTimeout = 30 * time.Second
	// errConcurrentIdempotentRequests is Resend's 409 for a key whose first request has not finished.
	errConcurrentIdempotentRequests = "concurrent_idempotent_requests"
)

// Config holds what the Resend backend needs; an empty WebhookSecret rejects every delivery event.
type Config struct {
	APIKey        string
	WebhookSecret string
}

type backend struct {
	client  *resend.Client
	apiKey  string
	webhook *svix.Webhook
}

func New(cfg Config) (emailtypes.Provider, error) {
	b := &backend{
		client: resend.NewCustomClient(&http.Client{
			Timeout:   sendTimeout,
			Transport: statusTransport{base: http.DefaultTransport},
		}, cfg.APIKey),
		apiKey: cfg.APIKey,
	}

	if cfg.WebhookSecret != "" {
		wh, err := svix.NewWebhook(cfg.WebhookSecret)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("email.resend.webhook_secret is not a valid Resend signing secret").
				Mark(ierr.ErrValidation)
		}
		b.webhook = wh
	}
	return b, nil
}

func (b *backend) Name() types.EmailProvider {
	return types.EmailProviderResend
}

func (b *backend) Send(ctx context.Context, msg emailtypes.OutboundEmail) (emailtypes.SendResult, error) {
	if b.apiKey == "" {
		return emailtypes.SendResult{}, ierr.NewError("resend api key is not configured").
			WithHint("Set email.resend.api_key to send emails through Resend").
			Mark(ierr.ErrValidation)
	}

	req := &resend.SendEmailRequest{
		From:    msg.From,
		To:      msg.To,
		Cc:      msg.CC,
		ReplyTo: msg.ReplyTo,
		Subject: msg.Subject,
		Html:    msg.HTML,
		Text:    msg.Text,
		Headers: msg.Headers,
		Tags:    toTags(msg.Tags),
	}
	for _, a := range msg.Attachments {
		req.Attachments = append(req.Attachments, &resend.Attachment{Filename: a.Filename, Content: a.Content})
	}

	reply := new(sendResponse)
	ctx = context.WithValue(ctx, statusKey{}, reply)

	sent, err := b.client.Emails.SendWithOptions(ctx, req, &resend.SendEmailOptions{IdempotencyKey: msg.IdempotencyKey})
	if err != nil {
		if reply.status == http.StatusConflict {
			// An earlier send with this key is still running and may yet fail, so retry later.
			if reply.errorName == errConcurrentIdempotentRequests {
				return emailtypes.SendResult{}, ierr.WithError(err).
					WithHint("Resend is still processing an earlier send of this email").
					Mark(ierr.ErrServiceUnavailable)
			}
			// Any other 409: Resend already took a send with this key, so the email went out.
			return emailtypes.SendResult{}, nil
		}
		return emailtypes.SendResult{}, classifySendError(err, reply.status)
	}
	return emailtypes.SendResult{ProviderMessageID: sent.Id}, nil
}

// classifySendError marks 4xx as permanent and 429, 5xx and network failures as retryable.
func classifySendError(err error, status int) error {
	switch {
	case errors.Is(err, resend.ErrRateLimit) || status == http.StatusTooManyRequests:
		return ierr.WithError(err).WithHint("Resend rate limit reached").Mark(ierr.ErrTooManyRequests)
	case status >= 400 && status < 500:
		return ierr.WithError(err).WithHint("Resend rejected the email").Mark(ierr.ErrValidation)
	default:
		return ierr.WithError(err).WithHint("Resend is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
}

// eventStatuses maps Resend webhook types to delivery statuses; other types are ignored.
var eventStatuses = map[string]types.EmailDeliveryStatus{
	"email.sent":             types.EmailDeliveryStatusSent,
	"email.delivery_delayed": types.EmailDeliveryStatusDeliveryDelayed,
	"email.delivered":        types.EmailDeliveryStatusDelivered,
	"email.bounced":          types.EmailDeliveryStatusBounced,
	"email.complained":       types.EmailDeliveryStatusComplained,
	"email.failed":           types.EmailDeliveryStatusFailed,
	"email.suppressed":       types.EmailDeliveryStatusSuppressed,
}

type webhookPayload struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Data      struct {
		EmailID string `json:"email_id"`
		Tags    tagSet `json:"tags"`
		Bounce  struct {
			Message string `json:"message"`
		} `json:"bounce"`
		Failed struct {
			Reason string `json:"reason"`
		} `json:"failed"`
	} `json:"data"`
}

func (b *backend) ParseEvents(_ context.Context, body []byte, hdr http.Header) ([]emailtypes.DeliveryEvent, error) {
	if b.webhook == nil {
		return nil, ierr.NewError("resend webhook secret is not configured").
			WithHint("Set email.resend.webhook_secret to accept Resend delivery events").
			Mark(ierr.ErrPermissionDenied)
	}
	// Verify also rejects timestamps more than 5 minutes off.
	if err := b.webhook.Verify(body, hdr); err != nil {
		return nil, ierr.WithError(err).WithHint("Invalid Resend webhook signature").Mark(ierr.ErrPermissionDenied)
	}

	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, ierr.WithError(err).WithHint("Malformed Resend webhook payload").Mark(ierr.ErrValidation)
	}

	status, ok := eventStatuses[p.Type]
	if !ok || p.Data.EmailID == "" {
		return nil, nil
	}

	reason := p.Data.Bounce.Message
	if reason == "" {
		reason = p.Data.Failed.Reason
	}
	return []emailtypes.DeliveryEvent{{
		ProviderMessageID: p.Data.EmailID,
		TenantID:          p.Data.Tags[emailtypes.TagTenantID],
		EnvironmentID:     p.Data.Tags[emailtypes.TagEnvironmentID],
		EmailLogID:        p.Data.Tags[emailtypes.TagEmailLogID],
		Region:            p.Data.Tags[emailtypes.TagRegion],
		Status:            status,
		Reason:            reason,
		OccurredAt:        p.CreatedAt,
	}}, nil
}

func toTags(m map[string]string) []resend.Tag {
	tags := make([]resend.Tag, 0, len(m))
	for k, v := range m {
		tags = append(tags, resend.Tag{Name: k, Value: v})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return tags
}

// tagSet reads webhook tags sent either as an object or as a list of name/value pairs.
type tagSet map[string]string

func (t *tagSet) UnmarshalJSON(b []byte) error {
	var obj map[string]string
	if err := json.Unmarshal(b, &obj); err == nil {
		*t = obj
		return nil
	}
	var list []resend.Tag
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*t = make(tagSet, len(list))
	for _, tag := range list {
		(*t)[tag.Name] = tag.Value
	}
	return nil
}

type statusKey struct{}

// sendResponse is what statusTransport records about Resend's reply; errorName is set only on a 409.
type sendResponse struct {
	status    int
	errorName string
}

// statusTransport records the response status in the request context, since resend-go
// reports 4xx and 5xx failures as the same untyped error and drops the error name.
type statusTransport struct {
	base http.RoundTripper
}

func (t statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if resp != nil {
		if r, ok := req.Context().Value(statusKey{}).(*sendResponse); ok {
			r.status = resp.StatusCode
			if resp.StatusCode == http.StatusConflict {
				r.errorName = readErrorName(resp)
			}
		}
	}
	return resp, err
}

// readErrorName returns the error name from Resend's JSON body and leaves the body readable for resend-go.
func readErrorName(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var e struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Name
}
