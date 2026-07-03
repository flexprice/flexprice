package internal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
)

type addEnvironmentScript struct {
	log             *logger.Logger
	tenantRepo      tenant.Repository
	environmentRepo environment.Repository
}

func newAddEnvironmentScript() (*addEnvironmentScript, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}

	client := postgres.NewClient(entClient, log, tracing.NewService(cfg, log))
	repoParams := repository.RepositoryParams{
		EntClient:     client,
		Logger:        log,
		InMemoryCache: cache.GetInMemoryCache(),
	}

	return &addEnvironmentScript{
		log:             log,
		tenantRepo:      repository.NewTenantRepository(repoParams),
		environmentRepo: repository.NewEnvironmentRepository(repoParams),
	}, nil
}

func parseEnvironmentType(value string) (types.EnvironmentType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(types.EnvironmentDevelopment):
		return types.EnvironmentDevelopment, nil
	case string(types.EnvironmentProduction):
		return types.EnvironmentProduction, nil
	default:
		return "", fmt.Errorf("invalid environment type %q, allowed values: development, production", value)
	}
}

func (s *addEnvironmentScript) createEnvironment(ctx context.Context, tenantID, name string, envType types.EnvironmentType) (*environment.Environment, error) {
	env := &environment.Environment{
		ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name: name,
		Type: envType,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Status:    types.StatusPublished,
		},
	}

	if err := s.environmentRepo.Create(ctx, env); err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	return env, nil
}

func AddEnvironmentToTenant() error {
	tenantID := strings.TrimSpace(os.Getenv("TENANT_ID"))
	environmentName := strings.TrimSpace(os.Getenv("ENVIRONMENT_NAME"))
	environmentTypeRaw := strings.TrimSpace(os.Getenv("ENVIRONMENT_TYPE"))

	if tenantID == "" || environmentName == "" || environmentTypeRaw == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_NAME and ENVIRONMENT_TYPE are required")
	}

	environmentType, err := parseEnvironmentType(environmentTypeRaw)
	if err != nil {
		return err
	}

	script, err := newAddEnvironmentScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()

	_, err = script.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get tenant %s: %w", tenantID, err)
	}

	env, err := script.createEnvironment(ctx, tenantID, environmentName, environmentType)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully created environment %s for tenant %s\n", env.Name, tenantID)
	fmt.Printf("Environment ID: %s\n", env.ID)
	fmt.Printf("Environment Type: %s\n", env.Type)

	return nil
}
