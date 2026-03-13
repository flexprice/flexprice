package webhook

import (
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
	"go.uber.org/fx"
)

// Module provides all webhook-related dependencies
var Module = fx.Options(
	fx.Provide(
		// Publisher for sending webhook events (uses shared Kafka producer)
		publisher.NewPublisher,

		// Handler for processing webhook events (creates its own Kafka consumer)
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
