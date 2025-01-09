package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

// Stores holds all the repository interfaces for testing
type Stores struct {
	SubscriptionRepo subscription.Repository
	EventRepo        events.Repository
	PlanRepo         plan.Repository
	PriceRepo        price.Repository
	MeterRepo        meter.Repository
	CustomerRepo     customer.Repository
	InvoiceRepo      invoice.Repository
	WalletRepo       wallet.Repository
	AuthRepo         auth.Repository
	UserRepo         user.Repository
	TenantRepo       tenant.Repository
	EnvironmentRepo  environment.Repository
}

// BaseServiceTestSuite provides common functionality for all service test suites
type BaseServiceTestSuite struct {
	suite.Suite
	ctx       context.Context
	stores    Stores
	publisher publisher.EventPublisher
	db        postgres.IClient
	logger    *logger.Logger
	now       time.Time
}

// SetupSuite is called once before running the tests in the suite
func (s *BaseServiceTestSuite) SetupSuite() {
	// Initialize logger with test config
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{
			Level: types.LogLevelDebug,
		},
	}
	var err error
	s.logger, err = logger.NewLogger(cfg)
	if err != nil {
		s.T().Fatalf("failed to create logger: %v", err)
	}
}

// SetupTest is called before each test
func (s *BaseServiceTestSuite) SetupTest() {
	s.setupContext()
	s.setupStores()
	s.now = time.Now().UTC()
}

// TearDownTest is called after each test
func (s *BaseServiceTestSuite) TearDownTest() {
	s.clearStores()
}

func (s *BaseServiceTestSuite) setupContext() {
	s.ctx = context.Background()
	s.ctx = context.WithValue(s.ctx, types.CtxTenantID, "tenant_test")
	s.ctx = context.WithValue(s.ctx, types.CtxUserID, "user_test")
	s.ctx = context.WithValue(s.ctx, types.CtxRequestID, types.GenerateUUID())
}

func (s *BaseServiceTestSuite) setupStores() {
	s.stores = Stores{
		SubscriptionRepo: NewInMemorySubscriptionStore(),
		EventRepo:        NewInMemoryEventStore(),
		PlanRepo:         NewInMemoryPlanStore(),
		PriceRepo:        NewInMemoryPriceStore(),
		MeterRepo:        NewInMemoryMeterStore(),
		CustomerRepo:     NewInMemoryCustomerStore(),
		InvoiceRepo:      NewInMemoryInvoiceStore(),
		WalletRepo:       NewInMemoryWalletStore(),
		AuthRepo:         NewInMemoryAuthRepository(),
		UserRepo:         NewInMemoryUserStore(),
		TenantRepo:       NewInMemoryTenantStore(),
		EnvironmentRepo:  NewInMemoryEnvironmentStore(),
	}

	s.db = NewMockPostgresClient(s.logger)
	eventStore := s.stores.EventRepo.(*InMemoryEventStore)
	s.publisher = NewInMemoryEventPublisher(eventStore)
}

func (s *BaseServiceTestSuite) clearStores() {
	s.stores.SubscriptionRepo.(*InMemorySubscriptionStore).Clear()
	s.stores.EventRepo.(*InMemoryEventStore).Clear()
	s.stores.PlanRepo.(*InMemoryPlanStore).Clear()
	s.stores.PriceRepo.(*InMemoryPriceStore).Clear()
	s.stores.MeterRepo.(*InMemoryMeterStore).Clear()
	s.stores.CustomerRepo.(*InMemoryCustomerStore).Clear()
	s.stores.InvoiceRepo.(*InMemoryInvoiceStore).Clear()
	s.stores.WalletRepo.(*InMemoryWalletStore).Clear()
	s.stores.AuthRepo.(*InMemoryAuthRepository).Clear()
	s.stores.UserRepo.(*InMemoryUserStore).Clear()
	s.stores.TenantRepo.(*InMemoryTenantStore).Clear()
	s.stores.EnvironmentRepo.(*InMemoryEnvironmentStore).Clear()
}

// GetContext returns the test context
func (s *BaseServiceTestSuite) GetContext() context.Context {
	return s.ctx
}

// GetStores returns all test repositories
func (s *BaseServiceTestSuite) GetStores() Stores {
	return s.stores
}

// GetPublisher returns the test event publisher
func (s *BaseServiceTestSuite) GetPublisher() publisher.EventPublisher {
	return s.publisher
}

// GetDB returns the test database client
func (s *BaseServiceTestSuite) GetDB() postgres.IClient {
	return s.db
}

// GetLogger returns the test logger
func (s *BaseServiceTestSuite) GetLogger() *logger.Logger {
	return s.logger
}

// GetNow returns the current test time
func (s *BaseServiceTestSuite) GetNow() time.Time {
	return s.now.UTC()
}

// GetUUID returns a new UUID string
func (s *BaseServiceTestSuite) GetUUID() string {
	return types.GenerateUUID()
}
