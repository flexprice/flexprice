package email

import (
	"context"
	"net/http"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
	"github.com/flexprice/flexprice/internal/email/local"
	"github.com/flexprice/flexprice/internal/email/resend"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// The provider types live in emailtypes so backends can implement them without importing this package.
type (
	Provider      = emailtypes.Provider
	OutboundEmail = emailtypes.OutboundEmail
	Attachment    = emailtypes.Attachment
	SendResult    = emailtypes.SendResult
	DeliveryEvent = emailtypes.DeliveryEvent
)

// NewProvider builds the email provider named by email.provider. With customer emails off it
// returns a provider that sends nothing and accepts no delivery events.
func NewProvider(cfg *config.Configuration, log *logger.Logger) (Provider, error) {
	if !cfg.Email.IsProviderConfigured() {
		return disabled{}, nil
	}
	switch types.EmailProvider(cfg.Email.Provider) {
	case types.EmailProviderResend:
		return resend.New(resend.Config{
			APIKey:        cfg.Email.Resend.APIKey,
			WebhookSecret: cfg.Email.Resend.WebhookSecret,
		})
	case types.EmailProviderLocal:
		return local.New(log), nil
	default:
		return nil, ierr.NewErrorf("unsupported email provider: %s", cfg.Email.Provider).
			WithHint("Set email.provider to resend or local").
			Mark(ierr.ErrValidation)
	}
}

type disabled struct{}

func (disabled) Name() types.EmailProvider {
	return ""
}

func (disabled) Send(context.Context, OutboundEmail) (SendResult, error) {
	return SendResult{}, errDisabled()
}

func (disabled) ParseEvents(context.Context, []byte, http.Header) ([]DeliveryEvent, error) {
	return nil, errDisabled()
}

func errDisabled() error {
	return ierr.NewError("customer emails are disabled").
		WithHint("Emails to customers are not enabled in this deployment").
		Mark(ierr.ErrInvalidOperation)
}
