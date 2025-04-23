package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
)

type migrationScript struct {
	cfg             *config.Configuration
	log             *logger.Logger
	tenantRepo      tenant.Repository
	environmentRepo environment.Repository
	entClient       *ent.Client
	pgClient        postgres.IClient
}

func newMigrationScript() (*migrationScript, error) {
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize the database client
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
		return nil, err
	}

	// Create postgres client
	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Initialize repositories
	tenantRepo := entRepo.NewTenantRepository(pgClient, log, cacheClient)
	environmentRepo := entRepo.NewEnvironmentRepository(pgClient, log)

	return &migrationScript{
		cfg:             cfg,
		log:             log,
		tenantRepo:      tenantRepo,
		environmentRepo: environmentRepo,
		entClient:       entClient,
		pgClient:        pgClient,
	}, nil
}

// createEnvironment creates a new environment for a tenant if it doesn't exist
func (s *migrationScript) createEnvironment(ctx context.Context, name string, envType types.EnvironmentType, tenantID string) (*environment.Environment, error) {
	// Check if environment already exists
	existingEnvs, err := s.environmentRepo.List(ctx, types.Filter{})
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}

	// Filter environments by name manually
	for _, env := range existingEnvs {
		if env.Name == name {
			s.log.Infow("Environment already exists", "tenant_id", tenantID, "name", name)
			return env, nil
		}
	}

	// Create new environment
	now := time.Now().UTC()
	env := &environment.Environment{
		ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name: name,
		Type: envType,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
		},
	}

	err = s.environmentRepo.Create(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	s.log.Infow("Created new environment", "tenant_id", tenantID, "name", name, "id", env.ID)
	return env, nil
}

// updateEntitiesWithEnvironmentID updates all entities for a tenant with the given environment ID
func (s *migrationScript) updateEntitiesWithEnvironmentID(ctx context.Context, tenantID, environmentID string) error {
	// List of entities to update
	entities := []string{
		"customers",
		"entitlements",
		"features",
		"invoice_line_items",
		"invoices",
		"meters",
		"payment_attempts",
		"payments",
		"plans",
		"prices",
		"secrets",
		"subscriptions",
		"subscription_line_items",
		"tasks",
		"wallets",
		"wallet_transactions",
	}

	// Update each entity
	for _, entity := range entities {
		query := fmt.Sprintf(
			"UPDATE %s SET environment_id = $1 WHERE tenant_id = $2 AND (environment_id IS NULL OR environment_id = '')",
			entity,
		)

		result, err := s.entClient.QueryContext(ctx, query, environmentID, tenantID)
		if err != nil {
			s.log.Errorw("Failed to update entity", "entity", entity, "error", err)
			continue
		}

		rowsAffected := 0
		if result.Next() {
			rowsAffected = 1
		}
		result.Close()

		s.log.Infow("Updated entity", "entity", entity, "tenant_id", tenantID, "environment_id", environmentID, "rows_affected", rowsAffected)
	}

	return nil
}

// MigrateEnvironments is the main function that migrates all entities to use environment_id
func MigrateEnvironments() error {
	script, err := newMigrationScript()
	if err != nil {
		return fmt.Errorf("failed to initialize migration script: %w", err)
	}

	// Create a base context
	ctx := context.Background()

	// Get all tenants
	tenants, err := script.tenantRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tenants: %w", err)
	}

	script.log.Infow("Starting environment migration", "tenant_count", len(tenants))

	// Process each tenant
	for _, t := range tenants {
		// Create a context with tenant ID
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, t.ID)

		script.log.Infow("Processing tenant", "tenant_id", t.ID, "name", t.Name)

		// Create sandbox environment
		sandboxEnv, err := script.createEnvironment(tenantCtx, "Sandbox", types.EnvironmentDevelopment, t.ID)
		if err != nil {
			script.log.Errorw("Failed to create sandbox environment", "tenant_id", t.ID, "error", err)
			continue
		}

		// Create production environment
		prodEnv, err := script.createEnvironment(tenantCtx, "Production", types.EnvironmentProduction, t.ID)
		if err != nil {
			script.log.Errorw("Failed to create production environment", "tenant_id", t.ID, "error", err)
			continue
		}

		// Update all entities with sandbox environment ID
		err = script.updateEntitiesWithEnvironmentID(tenantCtx, t.ID, sandboxEnv.ID)
		if err != nil {
			script.log.Errorw("Failed to update entities", "tenant_id", t.ID, "error", err)
			continue
		}

		script.log.Infow("Successfully processed tenant",
			"tenant_id", t.ID,
			"sandbox_env_id", sandboxEnv.ID,
			"production_env_id", prodEnv.ID,
		)
	}

	script.log.Infow("Environment migration completed", "tenant_count", len(tenants))
	return nil
}
