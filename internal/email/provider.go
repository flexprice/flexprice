package email

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
	"github.com/flexprice/flexprice/internal/email/logbackend"
	"github.com/flexprice/flexprice/internal/email/resendbackend"
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

// NewProvider builds the email provider named by email.provider; empty means log.
func NewProvider(cfg *config.Configuration, log *logger.Logger) (Provider, error) {
	switch types.EmailProvider(cfg.Email.Provider) {
	case types.EmailProviderResend:
		return resendbackend.New(resendbackend.Config{
			APIKey:        cfg.Email.Resend.APIKey,
			WebhookSecret: cfg.Email.Resend.WebhookSecret,
		})
	case types.EmailProviderLog, "":
		return logbackend.New(log), nil
	default:
		return nil, ierr.NewErrorf("unsupported email provider: %s", cfg.Email.Provider).
			WithHint("Set email.provider to resend or log").
			Mark(ierr.ErrValidation)
	}
}
