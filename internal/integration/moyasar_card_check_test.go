package integration_test

import (
	"context"
	"errors"
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

// stubMoyasarClient serves config and one payment's status, and records releases.
type stubMoyasarClient struct {
	moyasar.MoyasarClient
	status   moyasar.MoyasarPaymentStatus
	getErr   error
	voidErr  error
	voided   int
	refunded int
}

func (c *stubMoyasarClient) GetMoyasarConfig(_ context.Context) (*moyasar.MoyasarConfig, error) {
	return &moyasar.MoyasarConfig{PublishableKey: "pk_test_1"}, nil
}

func (c *stubMoyasarClient) GetPayment(_ context.Context, paymentID string) (*moyasar.MoyasarPayment, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	return &moyasar.MoyasarPayment{ID: paymentID, Status: string(c.status)}, nil
}

func (c *stubMoyasarClient) VoidPayment(_ context.Context, paymentID string) (*moyasar.MoyasarPayment, error) {
	c.voided++
	return &moyasar.MoyasarPayment{ID: paymentID}, c.voidErr
}

func (c *stubMoyasarClient) RefundPayment(_ context.Context, _ string, _ int) (*moyasar.RefundPaymentResponse, error) {
	c.refunded++
	return &moyasar.RefundPaymentResponse{}, nil
}

type MoyasarCardCheckSuite struct {
	testutil.BaseServiceTestSuite
	client     *stubMoyasarClient
	moyasar    *integration.MoyasarIntegration
	customerID string
}

func TestMoyasarCardCheck(t *testing.T) {
	suite.Run(t, new(MoyasarCardCheckSuite))
}

func (s *MoyasarCardCheckSuite) SetupTest() {
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
	s.client = &stubMoyasarClient{}
	s.moyasar = &integration.MoyasarIntegration{
		Client:    s.client,
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
func (s *MoyasarCardCheckSuite) TestEachAttemptGetsItsOwnPayment() {
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

// A retried release must settle what Moyasar already did instead of releasing twice.
func (s *MoyasarCardCheckSuite) TestVoidOrRefundAuthPayment() {
	tests := []struct {
		name         string
		status       moyasar.MoyasarPaymentStatus
		voidErr      error
		wantVoided   bool
		wantRefunded bool
		wantCalls    int
		wantStatus   types.PaymentStatus
	}{
		{name: "held charge is voided", status: moyasar.MoyasarPaymentStatusPaid, wantVoided: true, wantCalls: 1, wantStatus: types.PaymentStatusVoided},
		{name: "refund when the void fails", status: moyasar.MoyasarPaymentStatusCaptured, voidErr: errors.New("void window closed"), wantRefunded: true, wantCalls: 2, wantStatus: types.PaymentStatusRefunded},
		{name: "already voided is only recorded", status: moyasar.MoyasarPaymentStatusVoided, wantVoided: true, wantStatus: types.PaymentStatusVoided},
		{name: "already refunded is only recorded", status: moyasar.MoyasarPaymentStatusRefunded, wantRefunded: true, wantStatus: types.PaymentStatusRefunded},
		{name: "failed charge is closed without a release", status: moyasar.MoyasarPaymentStatusFailed, wantStatus: types.PaymentStatusVoided},
		{name: "unfinished charge is left for the next run", status: moyasar.MoyasarPaymentStatusInitiated, wantStatus: types.PaymentStatusSucceeded},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			s.client.status = tt.status
			s.client.voidErr = tt.voidErr

			paymentID, _, err := s.moyasar.InitiateTokenization(ctx, s.customerID)
			s.Require().NoError(err)
			s.Require().NoError(s.moyasar.Lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
				FlexpricePaymentID: paymentID,
				GatewayPaymentID:   "moy_card_check",
			}))

			voided, refunded, err := s.moyasar.VoidOrRefundAuthPayment(ctx, paymentID, "moy_card_check")
			s.Require().NoError(err)
			s.Equal(tt.wantVoided, voided)
			s.Equal(tt.wantRefunded, refunded)
			s.Equal(tt.wantCalls, s.client.voided+s.client.refunded)

			p, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
			s.Require().NoError(err)
			s.Equal(tt.wantStatus, p.PaymentStatus)
		})
	}
}

// Lookup and recording failures are returned, so the settlement job tries again.
func (s *MoyasarCardCheckSuite) TestVoidOrRefundAuthPaymentErrors() {
	tests := []struct {
		name      string
		status    moyasar.MoyasarPaymentStatus
		getErr    error
		paymentID string
	}{
		{name: "Moyasar lookup fails", getErr: errors.New("moyasar unavailable"), paymentID: "pay_any"},
		{name: "recording an existing void fails", status: moyasar.MoyasarPaymentStatusVoided, paymentID: "pay_missing"},
		{name: "recording an existing refund fails", status: moyasar.MoyasarPaymentStatusRefunded, paymentID: "pay_missing"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.client.status = tt.status
			s.client.getErr = tt.getErr

			voided, refunded, err := s.moyasar.VoidOrRefundAuthPayment(s.GetContext(), tt.paymentID, "moy_card_check")
			s.Error(err)
			s.False(voided)
			s.False(refunded)
			s.Zero(s.client.voided + s.client.refunded)
		})
	}
}
