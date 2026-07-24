package webhook

import (
	"context"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	kafkaProducerPkg "github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	repoent "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
	"go.uber.org/fx"
)

// Module provides all webhook-related dependencies
var Module = fx.Options(
	// Core dependencies
	fx.Provide(
		providePubSub,
	),

	// Webhook components
	fx.Provide(
		provideWebhookPublisher,
		provideEventDeriver,
		handler.NewHandler,
		providePayloadBuilderFactory,
		NewWebhookService,
	),
)

// provideEventDeriver wires the consumer-side event deriver and registers its filters. Adding a
// new derivation is a one-line filter registration here; the deriver statically rejects any
// registration whose source→target edges would form a cycle (panics at boot).
func provideEventDeriver(
	entitlementService service.EntitlementService,
	logger *logger.Logger,
) handler.EventDeriver {
	return handler.NewEventDeriver(
		logger,
		handler.NewEntitlementFilter(entitlementService, logger),
	)
}

// providePayloadBuilderFactory creates a new payload builder factory with all required services
func providePayloadBuilderFactory(
	invoiceService service.InvoiceService,
	planService service.PlanService,
	priceService service.PriceService,
	entitlementService service.EntitlementService,
	featureService service.FeatureService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	customerService service.CustomerService,
	paymentService service.PaymentService,
	tracingSvc *tracing.Service,
	creditNoteService service.CreditNoteService,
	checkoutSessionService interfaces.CheckoutSessionService,
	groupService service.GroupService,
) payload.PayloadBuilderFactory {
	services := payload.NewServices(
		invoiceService,
		planService,
		priceService,
		entitlementService,
		featureService,
		subscriptionService,
		walletService,
		customerService,
		paymentService,
		tracingSvc,
		creditNoteService,
		checkoutSessionService,
		groupService,
	)
	return payload.NewPayloadBuilderFactory(services)
}

func providePubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) pubsub.PubSub {
	pubSub, err := kafka.NewPubSubFromConfig(cfg, logger, cfg.Webhook.ConsumerGroup)
	if err != nil {
		logger.Fatal(context.Background(), "failed to create kafka pubsub for webhooks", "error", err)
	}
	return pubSub
}

// provideWebhookPublisher returns a webhook publisher backed by shared Kafka producer.
func provideWebhookPublisher(
	cfg *config.Configuration,
	logger *logger.Logger,
	producer *kafkaProducerPkg.Producer,
	systemEventRepo *repoent.SystemEventRepository,
) (publisher.WebhookPublisher, error) {
	return publisher.NewPublisherFromProducer(producer, cfg, logger, systemEventRepo)
}
