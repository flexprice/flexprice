package emailtypes

import (
	"context"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Tag keys every outbound email carries; delivery events echo them back.
const (
	TagTenantID      = "tenant_id"
	TagEnvironmentID = "environment_id"
	TagEmailLogID    = "email_log_id"
	TagRegion        = "region"
)

// Provider delivers emails and turns its delivery webhooks into status events.
type Provider interface {
	// Send hands the email to the provider. Errors are marked ierr.ErrValidation when
	// retrying cannot help, and ErrTooManyRequests or ErrServiceUnavailable when it can.
	Send(ctx context.Context, msg OutboundEmail) (SendResult, error)

	// ParseEvents verifies a delivery webhook and returns its events.
	ParseEvents(ctx context.Context, body []byte, hdr http.Header) ([]DeliveryEvent, error)

	Name() types.EmailProvider
}

type OutboundEmail struct {
	IdempotencyKey string
	From           string
	ReplyTo        string
	To             []string
	CC             []string
	Subject        string
	HTML           string
	Text           string
	Attachments    []Attachment
	Headers        map[string]string
	Tags           map[string]string
}

type Attachment struct {
	Filename string
	Content  []byte
}

type SendResult struct {
	ProviderMessageID string
}

type DeliveryEvent struct {
	ProviderMessageID string
	Region            string
	Status            types.EmailDeliveryStatus
	Reason            string
	OccurredAt        time.Time
}
