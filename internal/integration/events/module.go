package events

import (
	"context"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/fx"
)

// Module provides integration-events consumer dependencies into the FX graph.
// It uses an isolated Kafka consumer group on the same topic as webhooks (system_events).
var Module = fx.Options(
	fx.Provide(
		provideIntegrationPubSub,
		provideIntegrationHandler,
		NewIntegrationEventService,
	),
)

func provideIntegrationPubSub(
	cfg *config.Configuration,
	log *logger.Logger,
) types.IntegrationEventsPubSub {
	consumerGroup := cfg.IntegrationEvents.ConsumerGroup
	if consumerGroup == "" {
		consumerGroup = "integration-events-consumer"
	}
	ps, err := kafka.NewPubSubFromConfig(cfg, log, consumerGroup)
	if err != nil {
		log.Fatal(context.Background(), "integration_events: failed to create kafka pubsub", "error", err)
	}
	return types.IntegrationEventsPubSub{PubSub: ps}
}

func provideIntegrationHandler(
	connectionRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	ps types.IntegrationEventsPubSub,
	cfg *config.Configuration,
	log *logger.Logger,
) Handler {
	return NewHandler(Deps{
		ConnectionRepo: connectionRepo,
		EIMRepo:        eimRepo,
		CustomerRepo:   customerRepo,
		InvoiceRepo:    invoiceRepo,
		Logger:         log,
		Config:         cfg,
		PubSub:         ps.PubSub,
	})
}
