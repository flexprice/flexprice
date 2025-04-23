package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/pubsub/memory"
	"github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
)

func SyncBillingCustomers() error {
	// Initialize context
	ctx := context.Background()

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Initialize database client
	entClient, err := postgres.NewEntClient(cfg, logger)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer entClient.Close()

	// Create postgres client wrapper
	client := postgres.NewClient(entClient, logger, sentry.NewSentryService(cfg, logger))

	// Initialize cache
	cache := cache.GetInMemoryCache()

	// Initialize repositories
	tenantRepo := ent.NewTenantRepository(client, logger, cache)
	customerRepo := ent.NewCustomerRepository(client, logger, cache)
	subscriptionRepo := ent.NewSubscriptionRepository(client, logger, cache)
	invoiceRepo := ent.NewInvoiceRepository(client, logger, cache)
	walletRepo := ent.NewWalletRepository(client, logger, cache)
	planRepo := ent.NewPlanRepository(client, logger, cache)
	priceRepo := ent.NewPriceRepository(client, logger, cache)
	meterRepo := ent.NewMeterRepository(client, logger, cache)
	entitlementRepo := ent.NewEntitlementRepository(client, logger, cache)
	featureRepo := ent.NewFeatureRepository(client, logger, cache)
	authRepo := ent.NewAuthRepository(client, logger)

	// Initialize pubsub for webhook publisher
	ps := memory.NewPubSub(cfg, logger)

	// Initialize webhook publisher
	webhookPublisher, err := publisher.NewPublisher(ps, cfg, logger)
	if err != nil {
		log.Fatalf("Failed to create webhook publisher: %v", err)
	}
	defer webhookPublisher.Close()

	// Create service params with all dependencies
	serviceParams := service.ServiceParams{
		DB:               client,
		Config:           cfg,
		TenantRepo:       tenantRepo,
		CustomerRepo:     customerRepo,
		SubRepo:          subscriptionRepo,
		InvoiceRepo:      invoiceRepo,
		WalletRepo:       walletRepo,
		PlanRepo:         planRepo,
		PriceRepo:        priceRepo,
		MeterRepo:        meterRepo,
		EntitlementRepo:  entitlementRepo,
		FeatureRepo:      featureRepo,
		AuthRepo:         authRepo,
		Logger:           logger,
		WebhookPublisher: webhookPublisher,
	}

	// Create tenant service
	tenantService := service.NewTenantService(serviceParams)

	// Get all tenants
	tenants, err := tenantService.GetAllTenants(ctx)
	if err != nil {
		log.Fatalf("Failed to get tenants: %v", err)
	}

	// Create billing context
	billingCtx := context.WithValue(ctx, types.CtxTenantID, cfg.Billing.TenantID)
	billingCtx = context.WithValue(billingCtx, types.CtxEnvironmentID, cfg.Billing.EnvironmentID)

	// Create customer service for checking existing customers
	customerService := service.NewCustomerService(serviceParams)

	// Process each tenant
	var tenantsToSync []*tenant.Tenant

	for _, t := range tenants {
		if t.ID == cfg.Billing.TenantID {
			continue
		}

		// Try to find customer by lookup key (tenant ID)
		_, err := customerService.GetCustomerByLookupKey(billingCtx, t.ID)
		if err != nil {
			// If customer not found, add tenant to sync list
			domainTenant := &tenant.Tenant{
				ID:             t.ID,
				Name:           t.Name,
				Status:         types.Status(t.Status),
				BillingDetails: tenant.TenantBillingDetails{},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			tenantsToSync = append(tenantsToSync, domainTenant)
			fmt.Printf("Found tenant without billing customer: %s (%s)\n", t.Name, t.ID)
		}
	}

	fmt.Printf("Tenants to sync: %v\n", tenantsToSync)
	outputTenantsToSync(tenants, tenantsToSync, logger)

	// Process each tenant separately to avoid transaction rollback affecting all tenants
	// for _, t := range tenantsToSync {
	// 	err := client.WithTx(ctx, func(txCtx context.Context) error {
	// 		fmt.Printf("Creating billing customer for tenant: %s (%s)\n", t.Name, t.ID)

	// 		// Create billing context with transaction
	// 		billingTxCtx := context.WithValue(txCtx, types.CtxTenantID, cfg.Billing.TenantID)
	// 		billingTxCtx = context.WithValue(billingTxCtx, types.CtxEnvironmentID, cfg.Billing.EnvironmentID)

	// 		err := tenantService.CreateTenantAsBillingCustomer(billingTxCtx, t)
	// 		if err != nil {
	// 			fmt.Printf("Failed to create billing customer for tenant %s: %v\n", t.ID, err)
	// 			return err
	// 		}
	// 		fmt.Printf("Successfully created billing customer for tenant: %s\n", t.ID)
	// 		return nil
	// 	})
	// 	if err != nil {
	// 		logger.Errorw("Failed to sync tenant", "tenant_id", t.ID, "error", err)
	// 		continue
	// 	}
	// }

	fmt.Printf("\nSummary:\n")
	fmt.Printf("Total tenants processed: %d\n", len(tenants))
	fmt.Printf("Tenants requiring sync: %d\n", len(tenantsToSync))
	return nil
}

func outputTenantsToSync(tenants []*dto.TenantResponse, tenantsToSync []*tenant.Tenant, logger *logger.Logger) {

	// Output tenants to sync to JSON file for review
	output := struct {
		TotalTenantsProcessed int              `json:"total_tenants_processed"`
		TenantsRequiringSync  int              `json:"tenants_requiring_sync"`
		TenantsToSync         []*tenant.Tenant `json:"tenants_to_sync"`
	}{
		TotalTenantsProcessed: len(tenants),
		TenantsRequiringSync:  len(tenantsToSync),
		TenantsToSync:         tenantsToSync,
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		logger.Errorw("Failed to marshal tenants to JSON", "error", err)
	} else {
		err = os.WriteFile("tenants_to_sync.json", jsonData, 0644)
		if err != nil {
			logger.Errorw("Failed to write tenants to file", "error", err)
		} else {
			fmt.Printf("\nTenants to sync have been written to tenants_to_sync.json for review\n")
		}
	}
}
