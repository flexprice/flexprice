package webhook

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
)

// WebhookService orchestrates webhook operations
type WebhookService struct {
	config    *config.Configuration
	publisher publisher.WebhookPublisher
	handler   handler.Handler
	factory   payload.PayloadBuilderFactory
	client    httpclient.Client
	logger    *logger.Logger
}

// NewWebhookService creates a new webhook service
func NewWebhookService(
	cfg *config.Configuration,
	publisher publisher.WebhookPublisher,
	h handler.Handler,
	f payload.PayloadBuilderFactory,
	c httpclient.Client,
	l *logger.Logger,
) *WebhookService {
	return &WebhookService{
		config:    cfg,
		publisher: publisher,
		handler:   h,
		factory:   f,
		client:    c,
		logger:    l,
	}
}

// RegisterHandler registers the webhook handler with the router (consumer side)
func (s *WebhookService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	s.handler.RegisterHandler(router, cfg)
}

// Stop stops the webhook service
func (s *WebhookService) Stop() error {
	s.logger.Debug("stopping webhook service")

	// Then close the publisher
	if err := s.publisher.Close(); err != nil {
		s.logger.Errorw("failed to close webhook publisher", "error", err)
		return fmt.Errorf("failed to close webhook publisher: %w", err)
	}

	s.logger.Info("webhook service stopped successfully")
	return nil
}
