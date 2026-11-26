package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

const (
	moyasarBackfillActionSettle = "settle"
	moyasarBackfillActionSkip   = "skip"
)

// moyasarCardSetupBackfill settles Moyasar charges that landed on a FAILED card-setup payment
// before the webhook learned to recover them: the card was never saved and the 1 SAR never returned.
type moyasarCardSetupBackfill struct {
	paymentRepo payment.Repository
	paymentSvc  interfaces.PaymentService
	scope       interfaces.BillingEntityScope
	moyasar     func(ctx context.Context) (*integration.MoyasarIntegration, error)
	dryRun      bool
}

type moyasarCardSetupBackfillSummary struct {
	scanned          int
	settled          int
	skippedByStatus  map[string]int
	errors           int
	wouldSettleCount int
}

func (s moyasarCardSetupBackfillSummary) String() string {
	statuses := lo.Keys(s.skippedByStatus)
	sort.Strings(statuses)
	out := fmt.Sprintf("scanned=%d paid_and_settled=%d", s.scanned, s.settled)
	if s.wouldSettleCount > 0 {
		out += fmt.Sprintf(" would_settle=%d", s.wouldSettleCount)
	}
	for _, status := range statuses {
		out += fmt.Sprintf(" skipped_%s=%d", status, s.skippedByStatus[status])
	}
	return out + fmt.Sprintf(" errors=%d", s.errors)
}

// moyasarChargeID is the Moyasar charge the decline webhook stored on the payment; payments the
// webhook never reached fall back to the given_id the setup intent derived from the payment ID.
func moyasarChargeID(p *payment.Payment) string {
	if id := lo.FromPtr(p.GatewayPaymentID); id != "" {
		return id
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.ID)).String()
}

func (b *moyasarCardSetupBackfill) run(ctx context.Context) (moyasarCardSetupBackfillSummary, error) {
	summary := moyasarCardSetupBackfillSummary{skippedByStatus: map[string]int{}}

	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeCustomer))
	filter.PaymentGateway = lo.ToPtr(string(types.PaymentGatewayTypeMoyasar))
	filter.PaymentMethodType = lo.ToPtr(string(types.PaymentMethodTypeCard))
	filter.PaymentStatus = lo.ToPtr(string(types.PaymentStatusFailed))

	failed, err := b.paymentRepo.List(ctx, filter)
	if err != nil {
		return summary, fmt.Errorf("list failed card setup payments: %w", err)
	}

	for _, p := range failed {
		summary.scanned++
		if err := b.process(ctx, p, &summary); err != nil {
			summary.errors++
			log.Printf("ERROR payment_id=%s customer_id=%s error=%v", p.ID, p.DestinationID, err)
		}
	}
	return summary, nil
}

func (b *moyasarCardSetupBackfill) process(ctx context.Context, p *payment.Payment, summary *moyasarCardSetupBackfillSummary) error {
	chargeID := moyasarChargeID(p)

	scopedCtx, err := b.scope.ScopeToPayment(ctx, p)
	if err != nil {
		return fmt.Errorf("scope to payment: %w", err)
	}
	moyasarIntegration, err := b.moyasar(scopedCtx)
	if err != nil {
		return fmt.Errorf("get moyasar integration: %w", err)
	}
	charge, err := moyasarIntegration.Client.GetPayment(scopedCtx, chargeID)
	if err != nil {
		return fmt.Errorf("get moyasar charge %s: %w", chargeID, err)
	}

	action := moyasarBackfillActionSkip
	if charge.Status == string(moyasar.MoyasarPaymentStatusPaid) || charge.Status == string(moyasar.MoyasarPaymentStatusCaptured) {
		action = moyasarBackfillActionSettle
	}
	log.Printf("payment_id=%s customer_id=%s moyasar_charge_id=%s moyasar_status=%s action=%s dry_run=%t",
		p.ID, p.DestinationID, chargeID, charge.Status, action, b.dryRun)

	if action == moyasarBackfillActionSkip {
		summary.skippedByStatus[charge.Status]++
		return nil
	}
	if b.dryRun {
		summary.wouldSettleCount++
		return nil
	}

	if err := moyasarIntegration.WebhookHandler.SettleChargeOnFailedSetup(scopedCtx, charge, dto.NewPaymentResponse(p), b.paymentSvc); err != nil {
		return fmt.Errorf("settle charge %s: %w", chargeID, err)
	}

	// Settlement logs rather than returns its later steps, so confirm the charge was returned.
	keyed, err := b.paymentRepo.GetByIdempotencyKey(scopedCtx, charge.ID)
	if err != nil {
		return fmt.Errorf("find replacement payment for charge %s: %w", charge.ID, err)
	}
	replacement, err := b.paymentRepo.Get(scopedCtx, keyed.ID)
	if err != nil {
		return fmt.Errorf("get replacement payment %s: %w", keyed.ID, err)
	}
	if replacement.PaymentStatus != types.PaymentStatusVoided && replacement.PaymentStatus != types.PaymentStatusRefunded {
		return fmt.Errorf("charge %s recorded on payment %s but not returned (status %s)", charge.ID, replacement.ID, replacement.PaymentStatus)
	}
	log.Printf("settled payment_id=%s replacement_payment_id=%s moyasar_charge_id=%s status=%s",
		p.ID, replacement.ID, charge.ID, replacement.PaymentStatus)
	summary.settled++
	return nil
}

// MoyasarBackfillCardSetup settles Moyasar card-setup charges that were paid on a payment an earlier
// decline marked FAILED. Dry run unless -dry-run=false.
func MoyasarBackfillCardSetup() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	dryRun := os.Getenv("DRY_RUN") != "false"
	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	appLogger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	entClient, err := postgres.NewEntClients(cfg, appLogger)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	client := postgres.NewClient(entClient, appLogger, tracing.NewService(cfg, appLogger))
	cacheClient := cache.NewInMemoryCache()
	redisCache := cache.NewRedisCache()

	customerRepo := entRepo.NewCustomerRepository(client, appLogger, redisCache)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, appLogger, redisCache)
	planRepo := entRepo.NewPlanRepository(client, appLogger, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, appLogger)
	paymentRepo := entRepo.NewPaymentRepository(client, appLogger, redisCache)
	paymentMethodRepo := entRepo.NewPaymentMethodRepository(client, appLogger, redisCache)
	priceRepo := entRepo.NewPriceRepository(client, appLogger, redisCache)
	meterRepo := entRepo.NewMeterRepository(client, appLogger, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(client, appLogger, cacheClient, redisCache)
	connectionRepo := entRepo.NewConnectionRepository(client, appLogger, redisCache)
	entityIntegrationMappingRepo := entRepo.NewEntityIntegrationMappingRepository(client, appLogger, redisCache)
	settingsRepo := entRepo.NewSettingsRepository(client, appLogger, cacheClient)

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
		planRepo,
		invoiceRepo,
		paymentRepo,
		paymentMethodRepo,
		priceRepo,
		entityIntegrationMappingRepo,
		meterRepo,
		featureRepo,
		encService,
		nil, // TemporalService — not available in script context
		nil, // Locker — not needed in script context
		settingsRepo,
		nil, // taxAppliedRepo — not needed in script context
	)

	params := service.ServiceParams{
		Logger:                       appLogger,
		Config:                       cfg,
		DB:                           client,
		CustomerRepo:                 customerRepo,
		SubRepo:                      subscriptionRepo,
		PlanRepo:                     planRepo,
		InvoiceRepo:                  invoiceRepo,
		InvoiceLineItemRepo:          entRepo.NewInvoiceLineItemRepository(client, appLogger),
		PaymentRepo:                  paymentRepo,
		PriceRepo:                    priceRepo,
		MeterRepo:                    meterRepo,
		FeatureRepo:                  featureRepo,
		TenantRepo:                   entRepo.NewTenantRepository(client, appLogger, cacheClient, redisCache),
		EnvironmentRepo:              entRepo.NewEnvironmentRepository(client, appLogger, cacheClient),
		WalletRepo:                   entRepo.NewWalletRepository(client, appLogger, redisCache),
		ConnectionRepo:               connectionRepo,
		EntityIntegrationMappingRepo: entityIntegrationMappingRepo,
		SettingsRepo:                 settingsRepo,
		BillingEntityRepo:            entRepo.NewBillingEntityRepository(client, appLogger),
		WebhookPublisher:             noopWebhookPublisher{},
		Client:                       httpclient.NewDefaultClient(),
		IntegrationFactory:           integrationFactory,
	}
	paymentSvc := service.NewPaymentService(params)
	integrationFactory.SetServices(paymentSvc, service.NewInvoiceService(params))

	ctx := context.Background()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	ctx = types.SetUserID(ctx, "system")

	backfill := &moyasarCardSetupBackfill{
		paymentRepo: paymentRepo,
		paymentSvc:  paymentSvc,
		scope:       service.NewBillingEntityService(params),
		moyasar:     integrationFactory.GetMoyasarIntegration,
		dryRun:      dryRun,
	}
	summary, err := backfill.run(ctx)
	log.Printf("summary dry_run=%t %s", dryRun, summary)
	return err
}
