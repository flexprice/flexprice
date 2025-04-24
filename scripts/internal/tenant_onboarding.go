package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
	"github.com/samber/lo"
)

type onboardingScript struct {
	cfg             *config.Configuration
	log             *logger.Logger
	tenantRepo      tenant.Repository
	userRepo        user.Repository
	environmentRepo environment.Repository
	authProvider    auth.Provider
	supabaseAuth    *supabase.Client
}

func newOnboardingScript() (*onboardingScript, error) {
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

	// Initialize the other DB
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))

	// Initialize repositories
	repoParams := repository.RepositoryParams{
		EntClient: client,
		Logger:    log,
	}

	// Create auth provider
	authProvider := auth.NewProvider(cfg)

	return &onboardingScript{
		cfg:             cfg,
		log:             log,
		tenantRepo:      repository.NewTenantRepository(repoParams),
		userRepo:        repository.NewUserRepository(repoParams),
		environmentRepo: repository.NewEnvironmentRepository(repoParams),
		authProvider:    authProvider,
		supabaseAuth:    newSupabaseAuth(cfg),
	}, nil
}

func newSupabaseAuth(cfg *config.Configuration) *supabase.Client {
	client := supabase.CreateClient(cfg.Auth.Supabase.BaseURL, cfg.Auth.Supabase.ServiceKey)
	if client == nil {
		log.Fatalf("failed to create Supabase client")
	}

	return client
}

func (s *onboardingScript) createTenant(ctx context.Context, name string) (*tenant.Tenant, error) {
	t := &tenant.Tenant{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT),
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.tenantRepo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	s.log.Infow("created tenant", "id", t.ID, "name", t.Name)
	return t, nil
}

func (s *onboardingScript) createUser(ctx context.Context, email, tenantID string) (*user.User, error) {
	password := os.Getenv("USER_PASSWORD")
	u := user.NewUser(email, tenantID)

	// Check if user already exists in MongoDB
	existingUser, err := s.userRepo.GetByEmail(ctx, u.Email)
	if err == nil && existingUser != nil {
		s.log.Infow("user already exists", "id", existingUser.ID, "email", existingUser.Email, "tenant_id", existingUser.TenantID)
		return existingUser, nil
	}

	// Register the user with Supabase only if UserID is empty
	// Skip the confirmation email step and directly set the user as confirmed
	supabaseUser, err := s.supabaseAuth.Admin.CreateUser(ctx, supabase.AdminUserParams{
		Email:        u.Email,
		Password:     lo.ToPtr(password),
		EmailConfirm: true, // This is the key setting that bypasses email confirmation
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create user with admin API: %w", err)
	}
	s.log.Infof("Supabase registration response : %+v", supabaseUser)

	u.ID = supabaseUser.ID // Set the UserID from the Supabase response

	if err := s.userRepo.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	s.log.Infow("created user", "id", u.ID, "email", u.Email, "tenant_id", u.TenantID)
	return u, nil
}

func (s *onboardingScript) createEnvironment(ctx context.Context, name string, envType types.EnvironmentType, tenantID string) (*environment.Environment, error) {
	e := &environment.Environment{
		ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name: name,
		Type: envType,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	if err := s.environmentRepo.Create(ctx, e); err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	s.log.Infow("created environment", "id", e.ID, "name", e.Name, "type", e.Type, "tenant_id", e.TenantID)
	return e, nil
}

func OnboardNewTenant() error {
	email := os.Getenv("USER_EMAIL")
	tenantName := os.Getenv("TENANT_NAME")
	password := os.Getenv("USER_PASSWORD")

	if email == "" || tenantName == "" || password == "" {
		log, _ := logger.NewLogger(config.GetDefaultConfig())
		log.Fatalf("Usage: go run scripts/local/main.go -user-email=<email> -tenant-name=<tenant_name> -user-password=<password>")
		return nil
	}

	log, _ := logger.NewLogger(config.GetDefaultConfig())
	// Initialize script
	script, err := newOnboardingScript()
	if err != nil {
		log.Fatalf("Failed to initialize script: %v", err)
	}

	ctx := context.Background()

	// Create tenant
	t, err := script.createTenant(ctx, tenantName)
	if err != nil {
		log.Fatalf("Failed to create tenant: %v", err)
	}

	// Create user
	u, err := script.createUser(ctx, email, t.ID)
	if err != nil {
		log.Fatalf("Failed to create user: %v", err)
	}

	// Create default environments (development, staging, production)
	envTypes := []types.EnvironmentType{
		types.EnvironmentDevelopment,
		types.EnvironmentProduction,
	}

	envNameMap := map[types.EnvironmentType]string{
		types.EnvironmentDevelopment: "Sandbox",
		types.EnvironmentProduction:  "Production",
	}

	for _, envType := range envTypes {
		env, err := script.createEnvironment(ctx, envNameMap[envType], envType, t.ID)
		if err != nil {
			log.Fatalf("Failed to create environment %s: %v", envType, err)
		}
		log.Debugf("Created environment %s", env.ID)
	}

	fmt.Printf("Successfully onboarded tenant %s with user %s\n", tenantName, email)
	fmt.Printf("Tenant ID: %s\n", t.ID)
	fmt.Printf("User ID: %s\n", u.ID)

	return nil
}
