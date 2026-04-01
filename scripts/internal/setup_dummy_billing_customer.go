package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
	"github.com/shopspring/decimal"
)

const dummyBillingEventCount = 10

type noopWebhookPublisher struct{}

func (noopWebhookPublisher) PublishWebhook(context.Context, *types.WebhookEvent) error { return nil }

func (noopWebhookPublisher) Close() error { return nil }

// kafkaUsageEventTopic matches internal/kafka.EventPublisher.determineTopic for standard ingested events.
func kafkaUsageEventTopic(cfg *config.Configuration, tenantID string) string {
	if slices.Contains(cfg.Kafka.RouteTenantsOnLazyMode, tenantID) {
		return cfg.Kafka.TopicLazy
	}
	return cfg.Kafka.Topic
}

const dummyBillingMaxCustomers = 50000

// SetupDummyBillingCustomer creates CUSTOMER_COUNT demo customers (default 1), each with a subscription,
// prepaid wallet with a $100 top-up, and dummyBillingEventCount usage events for the given meter.
// Kafka must be available per config.event.publish_destination.
func SetupDummyBillingCustomer() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	planID := os.Getenv("PLAN_ID")
	meterID := os.Getenv("METER_ID")
	startDateRaw := os.Getenv("START_DATE")
	billingCycleRaw := os.Getenv("BILLING_CYCLE")
	customerCountStr := os.Getenv("CUSTOMER_COUNT")
	if strings.TrimSpace(customerCountStr) == "" {
		customerCountStr = "1"
	}
	customerCount, err := strconv.Atoi(strings.TrimSpace(customerCountStr))
	if err != nil || customerCount < 1 || customerCount > dummyBillingMaxCustomers {
		return fmt.Errorf("CUSTOMER_COUNT must be an integer between 1 and %d", dummyBillingMaxCustomers)
	}

	if tenantID == "" || environmentID == "" || planID == "" || meterID == "" || startDateRaw == "" || billingCycleRaw == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID, PLAN_ID, METER_ID, START_DATE, and BILLING_CYCLE are required")
	}

	startDate, err := parseStartDate(startDateRaw)
	if err != nil {
		return err
	}

	billingCycle := types.BillingCycle(strings.TrimSpace(billingCycleRaw))
	if err := billingCycle.Validate(); err != nil {
		return fmt.Errorf("invalid BILLING_CYCLE (use anniversary or calendar): %w", err)
	}

	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	appLogger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}

	sentrySvc := sentry.NewSentryService(cfg, appLogger)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentrySvc)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}

	entClient, err := postgres.NewEntClients(cfg, appLogger)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	client := postgres.NewClient(entClient, appLogger, sentrySvc)
	cacheClient := cache.NewInMemoryCache()

	customerRepo := entRepo.NewCustomerRepository(client, appLogger, cacheClient)
	planRepo := entRepo.NewPlanRepository(client, appLogger, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, appLogger, cacheClient)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(client, appLogger, cacheClient)
	subscriptionPhaseRepo := entRepo.NewSubscriptionPhaseRepository(client, appLogger, cacheClient)
	subscriptionScheduleRepo := entRepo.NewSubscriptionScheduleRepository(client, appLogger)
	priceRepo := entRepo.NewPriceRepository(client, appLogger, cacheClient)
	priceUnitRepo := entRepo.NewPriceUnitRepository(client, appLogger, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, appLogger, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, appLogger, cacheClient)
	invoiceLineItemRepo := entRepo.NewInvoiceLineItemRepository(client, appLogger, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(client, appLogger, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(client, appLogger, cacheClient)
	walletRepo := entRepo.NewWalletRepository(client, appLogger, cacheClient)
	tenantRepo := entRepo.NewTenantRepository(client, appLogger, cacheClient)
	environmentRepo := entRepo.NewEnvironmentRepository(client, appLogger)
	creditGrantRepo := entRepo.NewCreditGrantRepository(client, appLogger, cacheClient)
	creditGrantApplicationRepo := entRepo.NewCreditGrantApplicationRepository(client, appLogger, cacheClient)
	taxRateRepo := entRepo.NewTaxRateRepository(client, appLogger, cacheClient)
	taxAssociationRepo := entRepo.NewTaxAssociationRepository(client, appLogger, cacheClient)
	taxAppliedRepo := entRepo.NewTaxAppliedRepository(client, appLogger, cacheClient)
	paymentRepo := entRepo.NewPaymentRepository(client, appLogger, cacheClient)
	secretRepo := entRepo.NewSecretRepository(client, appLogger, cacheClient)
	creditNoteRepo := entRepo.NewCreditNoteRepository(client, appLogger, cacheClient)
	creditNoteLineItemRepo := entRepo.NewCreditNoteLineItemRepository(client, appLogger, cacheClient)
	couponRepo := entRepo.NewCouponRepository(client, appLogger, cacheClient)
	couponAssociationRepo := entRepo.NewCouponAssociationRepository(client, appLogger, cacheClient)
	couponApplicationRepo := entRepo.NewCouponApplicationRepository(client, appLogger, cacheClient)
	addonRepo := entRepo.NewAddonRepository(client, appLogger, cacheClient)
	addonAssociationRepo := entRepo.NewAddonAssociationRepository(client, appLogger, cacheClient)
	connectionRepo := entRepo.NewConnectionRepository(client, appLogger, cacheClient)
	entityIntegrationMappingRepo := entRepo.NewEntityIntegrationMappingRepository(client, appLogger, cacheClient)
	settingsRepo := entRepo.NewSettingsRepository(client, appLogger, cacheClient)
	taskRepo := entRepo.NewTaskRepository(client, appLogger)
	costSheetRepo := entRepo.NewCostsheetRepository(client, appLogger, cacheClient)
	alertLogsRepo := entRepo.NewAlertLogsRepository(client, appLogger, cacheClient)
	groupRepo := entRepo.NewGroupRepository(client, appLogger, cacheClient)
	scheduledTaskRepo := entRepo.NewScheduledTaskRepository(client, appLogger)
	planPriceSyncRepo := entRepo.NewPlanPriceSyncRepository(client, appLogger)
	workflowExecutionRepo := entRepo.NewWorkflowExecutionRepository(client, appLogger, cacheClient)
	authRepo := entRepo.NewAuthRepository(client, appLogger)
	userRepo := entRepo.NewUserRepository(client, appLogger)

	eventRepo := chRepo.NewEventRepository(chStore, appLogger)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, appLogger)
	featureUsageRepo := chRepo.NewFeatureUsageRepository(chStore, appLogger)
	rawEventRepo := chRepo.NewRawEventRepository(chStore, appLogger)
	costSheetUsageRepo := chRepo.NewCostSheetUsageRepository(chStore, appLogger)

	kafkaProducer, err := kafka.NewProducer(cfg)
	if err != nil {
		return fmt.Errorf("kafka producer: %w", err)
	}
	defer func() {
		if err := kafkaProducer.Close(); err != nil {
			appLogger.Warnw("kafka producer close", "error", err)
		}
	}()

	eventPublisher, err := publisher.NewEventPublisher(cfg, appLogger, kafkaProducer, nil)
	if err != nil {
		return fmt.Errorf("event publisher: %w", err)
	}

	encService, err := security.NewEncryptionService(cfg, appLogger)
	if err != nil {
		return fmt.Errorf("encryption: %w", err)
	}

	integrationFactory := integration.NewFactory(
		cfg,
		appLogger,
		connectionRepo,
		customerRepo,
		subscriptionRepo,
		invoiceRepo,
		paymentRepo,
		priceRepo,
		entityIntegrationMappingRepo,
		meterRepo,
		featureRepo,
		encService,
	)

	typstCompiler := typst.DefaultCompiler(appLogger)
	pdfGen := pdf.NewGenerator(cfg, typstCompiler)

	params := service.ServiceParams{
		Logger:                       appLogger,
		Config:                       cfg,
		DB:                           client,
		PDFGenerator:                 pdfGen,
		AuthRepo:                     authRepo,
		UserRepo:                     userRepo,
		EventRepo:                    eventRepo,
		CostSheetUsageRepo:           costSheetUsageRepo,
		ProcessedEventRepo:           processedEventRepo,
		FeatureUsageRepo:             featureUsageRepo,
		RawEventRepo:                 rawEventRepo,
		MeterRepo:                    meterRepo,
		PriceRepo:                    priceRepo,
		PriceUnitRepo:                priceUnitRepo,
		CustomerRepo:                 customerRepo,
		PlanRepo:                     planRepo,
		SubRepo:                      subscriptionRepo,
		SubscriptionLineItemRepo:     subscriptionLineItemRepo,
		SubscriptionPhaseRepo:        subscriptionPhaseRepo,
		SubScheduleRepo:              subscriptionScheduleRepo,
		WalletRepo:                   walletRepo,
		TenantRepo:                   tenantRepo,
		InvoiceRepo:                  invoiceRepo,
		InvoiceLineItemRepo:          invoiceLineItemRepo,
		FeatureRepo:                  featureRepo,
		EntitlementRepo:              entitlementRepo,
		PaymentRepo:                  paymentRepo,
		SecretRepo:                   secretRepo,
		EnvironmentRepo:              environmentRepo,
		CreditGrantRepo:              creditGrantRepo,
		CostSheetRepo:                costSheetRepo,
		CreditNoteRepo:               creditNoteRepo,
		CreditNoteLineItemRepo:       creditNoteLineItemRepo,
		CreditGrantApplicationRepo:   creditGrantApplicationRepo,
		TaskRepo:                     taskRepo,
		TaxRateRepo:                  taxRateRepo,
		TaxAssociationRepo:           taxAssociationRepo,
		TaxAppliedRepo:               taxAppliedRepo,
		CouponRepo:                   couponRepo,
		CouponAssociationRepo:        couponAssociationRepo,
		CouponApplicationRepo:        couponApplicationRepo,
		AddonRepo:                    addonRepo,
		AddonAssociationRepo:         addonAssociationRepo,
		ConnectionRepo:               connectionRepo,
		EntityIntegrationMappingRepo: entityIntegrationMappingRepo,
		SettingsRepo:                 settingsRepo,
		AlertLogsRepo:                alertLogsRepo,
		GroupRepo:                    groupRepo,
		ScheduledTaskRepo:            scheduledTaskRepo,
		PlanPriceSyncRepo:            planPriceSyncRepo,
		WorkflowExecutionRepo:        workflowExecutionRepo,
		EventPublisher:               eventPublisher,
		WebhookPublisher:             noopWebhookPublisher{},
		Client:                       httpclient.NewDefaultClient(),
		ProrationCalculator:          proration.NewCalculator(appLogger),
		IntegrationFactory:           integrationFactory,
	}

	customerSvc := service.NewCustomerService(params)
	subscriptionSvc := service.NewSubscriptionService(params)
	walletSvc := service.NewWalletService(params)
	eventSvc := service.NewEventService(eventRepo, meterRepo, eventPublisher, appLogger, cfg)

	ctx := context.Background()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	ctx = types.SetUserID(ctx, "system")

	m, err := meterRepo.GetMeter(ctx, meterID)
	if err != nil {
		return fmt.Errorf("get meter: %w", err)
	}
	if m.TenantID != tenantID {
		return fmt.Errorf("meter %s does not belong to tenant %s", meterID, tenantID)
	}
	if m.EnvironmentID != "" && m.EnvironmentID != environmentID {
		return fmt.Errorf("meter %s is not in environment %s", meterID, environmentID)
	}

	p, err := planRepo.Get(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if p.TenantID != tenantID {
		return fmt.Errorf("plan does not belong to the specified tenant")
	}
	if p.EnvironmentID != "" && p.EnvironmentID != environmentID {
		return fmt.Errorf("plan %s is not in environment %s", planID, environmentID)
	}

	rand.Seed(time.Now().UnixNano())

	sendInvoice := types.CollectionMethodSendInvoice
	subReqBase := dto.CreateSubscriptionRequest{
		PlanID:             planID,
		Currency:           "usd",
		StartDate:          &startDate,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       billingCycle,
		CollectionMethod:   &sendInvoice,
	}

	log.Printf("Creating %d customer(s), each with subscription, $100 wallet top-up, and %d events\n",
		customerCount, dummyBillingEventCount)
	eventTopic := kafkaUsageEventTopic(cfg, tenantID)
	log.Printf("Usage events publish to Kafka topic %q (kafka.topic=%q, kafka.topic_lazy=%q; lazy used if tenant is in kafka.route_tenants_on_lazy_mode)\n",
		eventTopic, cfg.Kafka.Topic, cfg.Kafka.TopicLazy)

	for n := 0; n < customerCount; n++ {
		prefix := fmt.Sprintf("[%d/%d]", n+1, customerCount)
		externalID := fmt.Sprintf("dummy_%s", types.GenerateUUIDWithPrefix("tmp"))
		log.Printf("%s Creating customer external_id=%s\n", prefix, externalID)

		custResp, err := customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
			ExternalID:             externalID,
			Name:                   fmt.Sprintf("Dummy billing customer %d", n+1),
			Email:                  "",
			SkipOnboardingWorkflow: true,
		})
		if err != nil {
			return fmt.Errorf("%s create customer: %w", prefix, err)
		}
		cust := custResp.Customer
		log.Printf("%s Created customer id=%s\n", prefix, cust.ID)

		subReq := subReqBase
		subReq.CustomerID = cust.ID
		subResp, err := subscriptionSvc.CreateSubscription(ctx, subReq)
		if err != nil {
			return fmt.Errorf("%s create subscription: %w", prefix, err)
		}
		log.Printf("%s Created subscription id=%s\n", prefix, subResp.ID)

		walletResp, err := walletSvc.CreateWallet(ctx, &dto.CreateWalletRequest{
			CustomerID:     cust.ID,
			Currency:       "usd",
			ConversionRate: decimal.NewFromInt(1),
			WalletType:     types.WalletTypePrePaid,
		})
		if err != nil {
			return fmt.Errorf("%s create wallet: %w", prefix, err)
		}
		log.Printf("%s Created wallet id=%s\n", prefix, walletResp.ID)

		topUp := &dto.TopUpWalletRequest{
			CreditsToAdd:      decimal.Zero,
			Amount:            decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonFreeCredit,
			Description:       fmt.Sprintf("setup-dummy-billing-customer (%s)", prefix),
		}
		if _, err := walletSvc.TopUpWallet(ctx, walletResp.ID, topUp); err != nil {
			return fmt.Errorf("%s top up wallet: %w", prefix, err)
		}
		log.Printf("%s Topped up wallet with $100 (currency amount)\n", prefix)

		for i := 0; i < dummyBillingEventCount; i++ {
			ev := &dto.IngestEventRequest{
				EventID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
				ExternalCustomerID: cust.ExternalID,
				EventName:          m.EventName,
				Timestamp:          time.Now().UTC(),
				Properties:         eventPropertiesForMeter(m),
				Source:             "setup_dummy_billing_script",
			}
			if err := eventSvc.CreateEvent(ctx, ev); err != nil {
				return fmt.Errorf("%s create event %d: %w", prefix, i+1, err)
			}
		}
		log.Printf("%s Published %d events for event_name=%s\n", prefix, dummyBillingEventCount, m.EventName)
	}

	log.Printf("Done: %d customer(s), subscriptions, wallet top-ups, and events (ensure Kafka consumer is running for ClickHouse).\n", customerCount)
	return nil
}

func parseStartDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.UTC); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("parse START_DATE %q: use RFC3339 or 2006-01-02", raw)
}

func eventPropertiesForMeter(m *meter.Meter) map[string]interface{} {
	properties := make(map[string]interface{})
	if m.Aggregation.Type == types.AggregationSum ||
		m.Aggregation.Type == types.AggregationAvg {
		if m.Aggregation.Field != "" {
			properties[m.Aggregation.Field] = rand.Int63n(1000) + 1
		}
	}
	for _, filter := range m.Filters {
		if len(filter.Values) > 0 {
			properties[filter.Key] = filter.Values[rand.Intn(len(filter.Values))]
		}
	}
	return properties
}
