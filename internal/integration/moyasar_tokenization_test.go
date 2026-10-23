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

// stubMoyasarConfigClient serves the publishable key; other methods are unused.
type stubMoyasarConfigClient struct {
	moyasar.MoyasarClient
}

func (stubMoyasarConfigClient) GetMoyasarConfig(_ context.Context) (*moyasar.MoyasarConfig, error) {
	return &moyasar.MoyasarConfig{PublishableKey: "pk_test_1"}, nil
}

type MoyasarTokenizationSuite struct {
	testutil.BaseServiceTestSuite
	moyasar    *integration.MoyasarIntegration
	customerID string
}

func TestMoyasarTokenization(t *testing.T) {
	suite.Run(t, new(MoyasarTokenizationSuite))
}

func (s *MoyasarTokenizationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := service.ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	}
	paymentSvc := service.NewPaymentService(params)
	s.moyasar = &integration.MoyasarIntegration{
		Client:    stubMoyasarConfigClient{},
		Lifecycle: payments.NewPaymentLifecycle(paymentSvc, service.NewInvoiceService(params), s.GetLogger()),
		Logger:    s.GetLogger(),
	}

	s.customerID = "cust_tokenization"
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), &customer.Customer{
		ID:         s.customerID,
		ExternalID: "ext_tokenization",
		Name:       "Tokenization Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// Each card-setup attempt needs its own payment: a shared one, once declined,
// can't record a later approved card.
func (s *MoyasarTokenizationSuite) TestEachAttemptGetsItsOwnPayment() {
	ctx := s.GetContext()

	first, publishableKey, err := s.moyasar.InitiateTokenization(ctx, s.customerID)
	s.Require().NoError(err)
	s.Equal("pk_test_1", publishableKey)

	second, _, err := s.moyasar.InitiateTokenization(ctx, s.customerID)
	s.Require().NoError(err)
	s.NotEqual(first, second)

	for _, id := range []string{first, second} {
		p, err := s.GetStores().PaymentRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.PaymentDestinationTypeCustomer, p.DestinationType)
		s.Equal(s.customerID, p.DestinationID)
		s.Equal(types.PaymentStatusInitiated, p.PaymentStatus)
	}
}
