package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
	"github.com/stretchr/testify/suite"
)

// Stores holds all the repository interfaces for testing
type Stores struct {
	CreditGrantRepo            creditgrant.Repository
	CreditGrantApplicationRepo creditgrantapplication.Repository
	SubscriptionRepo           subscription.Repository
	EventRepo                  events.Repository
	PlanRepo                   plan.Repository
	PriceRepo                  price.Repository
	MeterRepo                  meter.Repository
	CustomerRepo               customer.Repository
	InvoiceRepo                invoice.Repository
	WalletRepo                 wallet.Repository
	PaymentRepo                payment.Repository
	AuthRepo                   auth.Repository
	UserRepo                   user.Repository
	TenantRepo                 tenant.Repository
	EnvironmentRepo            environment.Repository
	EntitlementRepo            entitlement.Repository
	FeatureRepo                feature.Repository
	TaskRepo                   task.Repository
	SecretRepo                 secret.Repository
	CreditNoteRepo             creditnote.Repository
	CreditNoteLineItemRepo     creditnote.CreditNoteLineItemRepository
}

// BaseServiceTestSuite provides common functionality for all service test suites
type BaseServiceTestSuite struct {
	suite.Suite
	ctx              context.Context
	stores           Stores
	publisher        publisher.EventPublisher
	webhookPublisher webhookPublisher.WebhookPublisher
	db               postgres.IClient
	logger           *logger.Logger
	config           *config.Configuration
	now              time.Time
	pdfGenerator     pdf.Generator
}

// SetupSuite is called once before running the tests in the suite
func (s *BaseServiceTestSuite) SetupSuite() {
	// Initialize validator
	validator.NewValidator()

	// Initialize logger with test config
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{
			Level: types.LogLevelInfo,
		},
	}
	var err error
	s.config = cfg
	s.logger, err = logger.NewLogger(cfg)
	if err != nil {
		s.T().Fatalf("failed to create logger: %v", err)
	}

	// Initialize cache
	cache.Initialize(s.logger)
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
	s.ctx = context.WithValue(s.ctx, types.CtxTenantID, types.DefaultTenantID)
	s.ctx = context.WithValue(s.ctx, types.CtxUserID, types.DefaultUserID)
	s.ctx = context.WithValue(s.ctx, types.CtxRequestID, types.GenerateUUID())
}

func (s *BaseServiceTestSuite) setupStores() {
	s.stores = Stores{
		SubscriptionRepo:           NewInMemorySubscriptionStore(),
		EventRepo:                  NewInMemoryEventStore(),
		PlanRepo:                   NewInMemoryPlanStore(),
		PriceRepo:                  NewInMemoryPriceStore(),
		MeterRepo:                  NewInMemoryMeterStore(),
		CustomerRepo:               NewInMemoryCustomerStore(),
		InvoiceRepo:                NewInMemoryInvoiceStore(),
		WalletRepo:                 NewInMemoryWalletStore(),
		PaymentRepo:                NewInMemoryPaymentStore(),
		AuthRepo:                   NewInMemoryAuthRepository(),
		UserRepo:                   NewInMemoryUserStore(),
		TenantRepo:                 NewInMemoryTenantStore(),
		EnvironmentRepo:            NewInMemoryEnvironmentStore(),
		EntitlementRepo:            NewInMemoryEntitlementStore(),
		FeatureRepo:                NewInMemoryFeatureStore(),
		TaskRepo:                   NewInMemoryTaskStore(),
		SecretRepo:                 NewInMemorySecretStore(),
		CreditGrantRepo:            NewInMemoryCreditGrantStore(),
		CreditGrantApplicationRepo: NewInMemoryCreditGrantApplicationStore(),
		CreditNoteRepo:             NewInMemoryCreditNoteStore(),
		CreditNoteLineItemRepo:     NewInMemoryCreditNoteLineItemStore(),
	}

	s.db = NewMockPostgresClient(s.logger)
	s.pdfGenerator = NewMockPDFGenerator(s.logger)
	eventStore := s.stores.EventRepo.(*InMemoryEventStore)
	s.publisher = NewInMemoryEventPublisher(eventStore)
	pubsub := NewInMemoryPubSub()
	webhookPublisher, err := webhookPublisher.NewPublisher(pubsub, s.config, s.logger)
	if err != nil {
		s.T().Fatalf("failed to create webhook publisher: %v", err)
	}
	s.webhookPublisher = webhookPublisher
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
	s.stores.PaymentRepo.(*InMemoryPaymentStore).Clear()
	s.stores.AuthRepo.(*InMemoryAuthRepository).Clear()
	s.stores.UserRepo.(*InMemoryUserStore).Clear()
	s.stores.TenantRepo.(*InMemoryTenantStore).Clear()
	s.stores.EnvironmentRepo.(*InMemoryEnvironmentStore).Clear()
	s.stores.EntitlementRepo.(*InMemoryEntitlementStore).Clear()
	s.stores.FeatureRepo.(*InMemoryFeatureStore).Clear()
	s.stores.TaskRepo.(*InMemoryTaskStore).Clear()
	s.stores.SecretRepo.(*InMemorySecretStore).Clear()
	s.stores.CreditGrantRepo.(*InMemoryCreditGrantStore).Clear()
	s.stores.CreditGrantApplicationRepo.(*InMemoryCreditGrantApplicationStore).Clear()
	s.stores.CreditNoteRepo.(*InMemoryCreditNoteStore).Clear()
	s.stores.CreditNoteLineItemRepo.(*InMemoryCreditNoteLineItemStore).Clear()
}

func (s *BaseServiceTestSuite) ClearStores() {
	s.clearStores()
}

// GetContext returns the test context
func (s *BaseServiceTestSuite) GetContext() context.Context {
	return s.ctx
}

// GetConfig returns the test configuration
func (s *BaseServiceTestSuite) GetConfig() *config.Configuration {
	return s.config
}

// GetStores returns all test repositories
func (s *BaseServiceTestSuite) GetStores() Stores {
	return s.stores
}

// GetPublisher returns the test event publisher
func (s *BaseServiceTestSuite) GetPublisher() publisher.EventPublisher {
	return s.publisher
}

// GetWebhookPublisher returns the test webhook publisher
func (s *BaseServiceTestSuite) GetWebhookPublisher() webhookPublisher.WebhookPublisher {
	return s.webhookPublisher
}

// GetDB returns the test database client
func (s *BaseServiceTestSuite) GetDB() postgres.IClient {
	return s.db
}

// GetPDFGenerator returns the test PDF generator
func (s *BaseServiceTestSuite) GetPDFGenerator() pdf.Generator {
	return s.pdfGenerator
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
