package main

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/workflow"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		fx.Provide(
			config.NewConfig,
			logger.NewLogger,
			postgres.NewDB,
			temporal.NewClient,
			repository.NewSubscriptionRepository,
			// Add other required repositories
			service.NewSubscriptionService,
		),
		fx.Invoke(registerWorker),
	)
	app.Run()
}

func registerWorker(
	lc fx.Lifecycle,
	temporalClient client.Client,
	subscriptionService service.SubscriptionService,
	logger *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			w := worker.New(temporalClient, "billing-period-queue", worker.Options{})
			workflow.RegisterWorkflows(w)
			workflow.RegisterActivities(w)

			go func() {
				if err := w.Run(worker.InterruptCh()); err != nil {
					logger.Fatal("Unable to start worker", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Shutting down worker...")
			return nil
		},
	})
}
