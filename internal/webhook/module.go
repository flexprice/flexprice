package webhook

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/memory"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
	"go.uber.org/fx"
)

// Module provides all webhook-related dependencies
var Module = fx.Options(
	// Core dependencies
	fx.Provide(
		// PubSub for sending webhook events
		providePubSub,
	),

	// Webhook components
	fx.Provide(
		// Publisher for sending webhook events
		publisher.NewPublisher,

		// Handler for processing webhook events
		handler.NewHandler,

		// Payload builder factory and services
		providePayloadBuilderFactory,

		// Main webhook service
		NewWebhookService,
	),
)

// providePayloadBuilderFactory creates a new payload builder factory with all required services
func providePayloadBuilderFactory(
	invoiceService service.InvoiceService,
	planService service.PlanService,
	priceService service.PriceService,
	entitlementService service.EntitlementService,
	featureService service.FeatureService,
	subscriptionService service.SubscriptionService,
) payload.PayloadBuilderFactory {
	services := payload.NewServices(
		invoiceService,
		planService,
		priceService,
		entitlementService,
		featureService,
		subscriptionService,
	)
	return payload.NewPayloadBuilderFactory(services)
}

func providePubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) pubsub.PubSub {
	switch cfg.Webhook.PubSub {
	case types.MemoryPubSub:
		return memory.NewPubSub(cfg, logger)
	case types.KafkaPubSub:
		// TODO: implement
	}
	panic("unsupported pubsub type")
}
