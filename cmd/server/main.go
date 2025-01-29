package main

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/api/cron"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/dynamodb"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/fx"

	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/gin-gonic/gin"
)

// @title FlexPrice API
// @version 1.0
// @description FlexPrice API Service
// @BasePath /v1
// @schemes http https
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name x-api-key
// @description Enter your API key in the format *x-api-key &lt;api-key&gt;**

func init() {
	// Set UTC timezone for the entire application
	time.Local = time.UTC
}

func main() {
	var opts []fx.Option
	opts = append(opts,
		fx.Provide(
			// Config
			config.NewConfig,

			// Logger
			logger.NewLogger,

			// Monitoring
			sentry.NewSentryService,

			// DB
			postgres.NewDB,
			postgres.NewEntClient,
			postgres.NewClient,
			clickhouse.NewClickHouseStore,

			// Optional DBs
			dynamodb.NewClient,

			// Producers and Consumers
			kafka.NewProducer,
			kafka.NewConsumer,

			// Event Publisher
			publisher.NewEventPublisher,

			// Repositories
			repository.NewEventRepository,
			repository.NewMeterRepository,
			repository.NewUserRepository,
			repository.NewAuthRepository,
			repository.NewPriceRepository,
			repository.NewCustomerRepository,
			repository.NewPlanRepository,
			repository.NewSubscriptionRepository,
			repository.NewWalletRepository,
			repository.NewTenantRepository,
			repository.NewEnvironmentRepository,
			repository.NewInvoiceRepository,

			// Services
			service.NewMeterService,
			service.NewEventService,
			service.NewUserService,
			service.NewAuthService,
			service.NewPriceService,
			service.NewCustomerService,
			service.NewPlanService,
			service.NewSubscriptionService,
			service.NewWalletService,
			service.NewTenantService,
			service.NewInvoiceService,

			// Handlers
			provideHandlers,

			// Router
			provideRouter,

			// Temporal
			provideTemporalConfig,
			temporal.NewTemporalClient,
			provideTemporalService,
		),
		fx.Invoke(
			startServer,
		),
	)

	app := fx.New(opts...)
	app.Run()
}

func provideHandlers(
	cfg *config.Configuration,
	logger *logger.Logger,
	meterService service.MeterService,
	eventService service.EventService,
	authService service.AuthService,
	userService service.UserService,
	priceService service.PriceService,
	customerService service.CustomerService,
	planService service.PlanService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	tenantService service.TenantService,
	invoiceService service.InvoiceService,
	temporalService *service.TemporalService,
) api.Handlers {
	return api.Handlers{
		Events:       v1.NewEventsHandler(eventService, logger),
		Meter:        v1.NewMeterHandler(meterService, logger),
		Auth:         v1.NewAuthHandler(cfg, authService, logger),
		User:         v1.NewUserHandler(userService, logger),
		Price:        v1.NewPriceHandler(priceService, logger),
		Customer:     v1.NewCustomerHandler(customerService, logger),
		Plan:         v1.NewPlanHandler(planService, logger),
		Subscription: v1.NewSubscriptionHandler(subscriptionService, logger),
		Wallet:       v1.NewWalletHandler(walletService, logger),
		Tenant:       v1.NewTenantHandler(tenantService, logger),
		Cron:         cron.NewSubscriptionHandler(subscriptionService, logger),
		Invoice:      v1.NewInvoiceHandler(invoiceService, logger),
	}
}

func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger)
}

func provideTemporalConfig(cfg *config.Configuration) *config.TemporalConfig {
	return &cfg.Temporal
}

func provideTemporalService(cfg *config.TemporalConfig, log *logger.Logger) (*service.TemporalService, error) {
	return service.NewTemporalService(cfg, log)
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	temporalClient *temporal.TemporalClient,
	temporalService *service.TemporalService,
	log *logger.Logger,
) {
	mode := cfg.Deployment.Mode
	if mode == "" {
		mode = types.ModeLocal
	}

	switch mode {
	case types.ModeLocal:
		startAPIServer(lc, r, cfg, log)
		if consumer != nil {
			startConsumer(lc, consumer, eventRepo, cfg, log)
		}
		startTemporalWorker(lc, temporalClient, &cfg.Temporal, temporalService, log)
		return

	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalClient, &cfg.Temporal, temporalService, log)
		return

	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)
		return

	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

func startTemporalWorker(
	lc fx.Lifecycle,
	temporalClient *temporal.TemporalClient,
	cfg *config.TemporalConfig,
	temporalService *service.TemporalService,
	log *logger.Logger,
) {
	worker := temporal.NewWorker(temporalClient, *cfg)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("Starting temporal worker...")
			if err := worker.Start(); err != nil {
				log.Error("Failed to start worker", "error", err)
				return err
			}
			log.Info("Temporal worker started successfully")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down temporal worker...")
			done := make(chan struct{})
			go func() {
				worker.Stop()
				close(done)
			}()

			select {
			case <-done:
				log.Info("Temporal worker stopped successfully")
			case <-ctx.Done():
				log.Error("Timeout while stopping temporal worker")
			}
			return nil
		},
	})
}

func startAPIServer(
	lc fx.Lifecycle,
	r *gin.Engine,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := r.Run(cfg.Server.Address); err != nil {
					log.Fatalf("Failed to start server: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down server...")
			return nil
		},
	})
}

func startConsumer(
	lc fx.Lifecycle,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				// Simulated message consumption
				log.Info("Kafka consumer started")
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down Kafka consumer...")
			return nil
		},
	})
}
