package integration_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type configOnlyMoyasarClient struct {
	moyasar.MoyasarClient
}

func (configOnlyMoyasarClient) GetMoyasarConfig(context.Context) (*moyasar.MoyasarConfig, error) {
	return &moyasar.MoyasarConfig{PublishableKey: "pk_test"}, nil
}

type MoyasarTokenizationSuite struct {
	testutil.BaseServiceTestSuite
	moyasar *integration.MoyasarIntegration
}

func TestMoyasarTokenization(t *testing.T) {
	suite.Run(t, new(MoyasarTokenizationSuite))
}

func (s *MoyasarTokenizationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	stores := s.GetStores()
	params := service.ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		CustomerRepo:     stores.CustomerRepo,
		InvoiceRepo:      stores.InvoiceRepo,
		PaymentRepo:      stores.PaymentRepo,
		SettingsRepo:     stores.SettingsRepo,
		ConnectionRepo:   stores.ConnectionRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	}
	s.moyasar = &integration.MoyasarIntegration{
		Client:      configOnlyMoyasarClient{},
		Lifecycle:   payments.NewPaymentLifecycle(service.NewPaymentService(params), service.NewInvoiceService(params), s.GetLogger()),
		PaymentRepo: stores.PaymentRepo,
		Logger:      s.GetLogger(),
	}
	s.Require().NoError(stores.CustomerRepo.Create(s.GetContext(), &customer.Customer{
		ID:         "cust_tokenize",
		ExternalID: "ext_cust_tokenize",
		Name:       "Tokenize Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *MoyasarTokenizationSuite) initiate() string {
	id, key, err := s.moyasar.InitiateTokenization(s.GetContext(), "cust_tokenize")
	s.Require().NoError(err)
	s.Equal("pk_test", key)
	return id
}

func (s *MoyasarTokenizationSuite) TestReusesUntouchedPayment() {
	s.Equal(s.initiate(), s.initiate())
}

func (s *MoyasarTokenizationSuite) TestPaymentWithAttemptIsNotReused() {
	first := s.initiate()
	s.Require().NoError(s.moyasar.Lifecycle.RecordFailedAttempt(s.GetContext(), payments.RecordFailedAttemptParams{
		FlexpricePaymentID: first,
		GatewayPaymentID:   "mp_declined",
		ErrorMessage:       "declined",
	}))

	second := s.initiate()
	s.NotEqual(first, second)
	s.Equal(second, s.initiate())

	p, err := s.GetStores().PaymentRepo.Get(s.GetContext(), second)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusInitiated, p.PaymentStatus)
}

func (s *MoyasarTokenizationSuite) TestRequiresPaymentRepo() {
	s.moyasar.PaymentRepo = nil
	_, _, err := s.moyasar.InitiateTokenization(s.GetContext(), "cust_tokenize")
	s.Error(err)
}
