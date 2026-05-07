package webhook

import (
	"github.com/flexprice/flexprice/internal/config"
	kafkaProducerPkg "github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	repoent "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
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
		handler.NewHandler,
		providePayloadBuilderFactory,
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
	walletService service.WalletService,
	customerService service.CustomerService,
	paymentService service.PaymentService,
	sentry *sentry.Service,
	creditNoteService service.CreditNoteService,
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
		sentry,
		creditNoteService,
	)
	return payload.NewPayloadBuilderFactory(services)
}

func providePubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) pubsub.PubSub {
	pubSub, err := kafka.NewPubSubFromConfig(cfg, logger, cfg.Webhook.ConsumerGroup)
	if err != nil {
		logger.Fatalw("failed to create kafka pubsub for webhooks", "error", err)
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
