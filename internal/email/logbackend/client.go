package logbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flexprice/flexprice/internal/email/emailtypes"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// backend writes each email to disk instead of sending it, for local runs.
type backend struct {
	dir string
	log *logger.Logger
}

func New(log *logger.Logger) emailtypes.Provider {
	return &backend{dir: filepath.Join(os.TempDir(), "flexprice-emails"), log: log}
}

func (b *backend) Name() types.EmailProvider {
	return types.EmailProviderLog
}

func (b *backend) Send(ctx context.Context, msg emailtypes.OutboundEmail) (emailtypes.SendResult, error) {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return emailtypes.SendResult{}, ierr.WithError(err).WithHint("Failed to write email to disk").Mark(ierr.ErrSystem)
	}

	path := filepath.Join(b.dir, filepath.Base(msg.IdempotencyKey)+".html")
	if err := os.WriteFile(path, []byte(msg.HTML), 0o644); err != nil {
		return emailtypes.SendResult{}, ierr.WithError(err).WithHint("Failed to write email to disk").Mark(ierr.ErrSystem)
	}

	b.log.Info(ctx, "email written to disk instead of sent", "email_log_id", msg.IdempotencyKey, "path", path, "attachments", len(msg.Attachments))
	return emailtypes.SendResult{ProviderMessageID: "log_" + msg.IdempotencyKey}, nil
}

type logEvent struct {
	ProviderMessageID string                    `json:"provider_message_id"`
	Region            string                    `json:"region"`
	Status            types.EmailDeliveryStatus `json:"status"`
	Reason            string                    `json:"reason"`
}

// ParseEvents accepts one unsigned event, so local runs can simulate delivery outcomes.
func (b *backend) ParseEvents(_ context.Context, body []byte, _ http.Header) ([]emailtypes.DeliveryEvent, error) {
	var e logEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, ierr.WithError(err).WithHint("Malformed email event").Mark(ierr.ErrValidation)
	}
	if err := e.Status.Validate(); err != nil {
		return nil, err
	}
	return []emailtypes.DeliveryEvent{{
		ProviderMessageID: e.ProviderMessageID,
		Region:            e.Region,
		Status:            e.Status,
		Reason:            e.Reason,
	}}, nil
}
