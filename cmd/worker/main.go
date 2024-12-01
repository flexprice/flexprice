package main

import (
	"context"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/integrations"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/workflows/stripe_sync"
)

func main() {
	app := fx.New(
		fx.Provide(
			// Core dependencies
			config.NewConfig,
			logger.NewLogger,
			NewTemporalClient,

			// Databases
			clickhouse.NewClickHouseStore,
			postgres.NewDB,

			// Kafka setup
			kafka.GetSaramaConfig,
			kafka.NewProducer,
			kafka.NewConsumer,

			// Repositories
			repository.NewEventRepository,
			repository.NewMeterRepository,

			// Services
			service.NewEventService,

			// Provide the Stripe secret key
			func(cfg *config.Configuration) string {
				if cfg.Stripe.SecretKey == "" {
					panic("Stripe secret key is not set in the configuration")
				}
				return cfg.Stripe.SecretKey
			},

			// Integrations
			integrations.NewStripeIntegration,

			// Worker
			NewTemporalWorker,
		),
		fx.Invoke(StartWorker),
	)

	app.Run()
}

type WorkerDependencies struct {
	fx.In

	Lifecycle      fx.Lifecycle
	Config         *config.Configuration
	Logger         *logger.Logger
	TemporalClient client.Client
	Integration    *integrations.StripeIntegration
}

func NewTemporalClient(config *config.Configuration) (client.Client, error) {
	return client.NewClient(client.Options{
		HostPort: config.Temporal.HostPort,
	})
}

func NewTemporalWorker(deps WorkerDependencies) worker.Worker {
	// Create worker with basic options
	w := worker.New(deps.TemporalClient, "stripe-sync", worker.Options{
		MaxConcurrentActivityExecutionSize: 1,
	})

	// Register workflow
	w.RegisterWorkflow(stripe_sync.SyncUsageWorkflow)

	// Create an instance of the activity
	syncUsageActivity := stripe_sync.NewSyncUsageActivity(deps.Integration)

	// Register activity with its method name
	w.RegisterActivity(syncUsageActivity.Execute)

	return w
}

func StartWorker(
	lifecycle fx.Lifecycle,
	w worker.Worker,
	logger *logger.Logger,
) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("Starting Temporal worker")
			go func() {
				if err := w.Run(worker.InterruptCh()); err != nil {
					logger.Error("Worker failed", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Shutting down worker")
			w.Stop()
			return nil
		},
	})
}
