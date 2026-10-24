package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/integration/moyasar/webhook"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// stubMoyasarClient records void and refund calls; other methods are unused.
type stubMoyasarClient struct {
	moyasar.MoyasarClient
	payment   *moyasar.MoyasarPayment
	voidErr   error
	refundErr error
	voided    []string
	refunded  []string
}

func (c *stubMoyasarClient) GetPayment(_ context.Context, _ string) (*moyasar.MoyasarPayment, error) {
	return c.payment, nil
}

func (c *stubMoyasarClient) VoidPayment(_ context.Context, paymentID string) (*moyasar.MoyasarPayment, error) {
	c.voided = append(c.voided, paymentID)
	if c.voidErr == nil && c.payment != nil {
		c.payment.Status = string(moyasar.MoyasarPaymentStatusVoided)
	}
	return &moyasar.MoyasarPayment{ID: paymentID}, c.voidErr
}

func (c *stubMoyasarClient) RefundPayment(_ context.Context, paymentID string, _ int) (*moyasar.RefundPaymentResponse, error) {
	c.refunded = append(c.refunded, paymentID)
	if c.refundErr == nil && c.payment != nil {
		c.payment.Status = string(moyasar.MoyasarPaymentStatusRefunded)
	}
	return &moyasar.RefundPaymentResponse{}, c.refundErr
}

// stubPaymentMethodRepo keeps created payment methods in memory; other methods are unused.
type stubPaymentMethodRepo struct {
	paymentmethod.Repository
	created []*paymentmethod.PaymentMethod
}

func (r *stubPaymentMethodRepo) Count(_ context.Context, filter *types.PaymentMethodFilter) (int, error) {
	return len(lo.Filter(r.created, func(pm *paymentmethod.PaymentMethod, _ int) bool {
		return pm.GatewayMethodID == lo.FromPtr(filter.GatewayMethodID) && pm.CustomerID == lo.FromPtr(filter.CustomerID)
	})), nil
}

func (r *stubPaymentMethodRepo) Create(_ context.Context, pm *paymentmethod.PaymentMethod) error {
	r.created = append(r.created, pm)
	return nil
}

type OrphanedCardCheckSuite struct {
	testutil.BaseServiceTestSuite
	paymentSvc interfaces.PaymentService
	lifecycle  *payments.PaymentLifecycle
	customerID string
}

func TestOrphanedCardCheck(t *testing.T) {
	suite.Run(t, new(OrphanedCardCheckSuite))
}

func (s *OrphanedCardCheckSuite) SetupTest() {
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
	s.paymentSvc = service.NewPaymentService(params)
	s.lifecycle = payments.NewPaymentLifecycle(s.paymentSvc, service.NewInvoiceService(params), s.GetLogger())

	s.customerID = "cust_card_setup"
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), &customer.Customer{
		ID:         s.customerID,
		ExternalID: "ext_card_setup",
		Name:       "Card Setup Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// declinedAttempt returns a card-setup payment that failed on moy_declined.
func (s *OrphanedCardCheckSuite) declinedAttempt() string {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeCustomer,
		DestinationID:     s.customerID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromInt(1),
		Currency:          "SAR",
		IdempotencyKey:    "setup_attempt_1",
	})
	s.Require().NoError(err)
	s.Require().NoError(s.lifecycle.RecordPaymentFailure(ctx, payments.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "moy_declined",
		ErrorMessage:       "DECLINED",
	}))
	return id
}

// approvedRetry is a paid card check whose metadata still points at an earlier attempt.
func approvedRetry(flexpricePaymentID string) *moyasar.MoyasarPayment {
	return &moyasar.MoyasarPayment{
		ID:       "moy_retry",
		Status:   string(moyasar.MoyasarPaymentStatusPaid),
		Source:   &moyasar.PaymentSource{Token: "token_retry", Company: "visa", Number: "4111-11XX-XXXX-1111"},
		Metadata: map[string]string{"flexprice_payment_id": flexpricePaymentID},
	}
}

var paidEvent = &webhook.MoyasarWebhookEvent{
	ID:   "evt_retry",
	Type: webhook.EventPaymentPaid,
	Data: webhook.PaymentEventData{ID: "moy_retry"},
}

func (s *OrphanedCardCheckSuite) TestApprovedRetryIsRecordedSavedAndReleased() {
	tests := []struct {
		name       string
		voidErr    error
		refundErr  error
		wantStatus types.PaymentStatus
	}{
		{name: "void succeeds", wantStatus: types.PaymentStatusVoided},
		{name: "refund after a failed void", voidErr: errors.New("void window closed"), wantStatus: types.PaymentStatusRefunded},
		// Left SUCCEEDED so the settlement job retries the release.
		{name: "both fail", voidErr: errors.New("void failed"), refundErr: errors.New("refund failed"), wantStatus: types.PaymentStatusSucceeded},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			declinedID := s.declinedAttempt()
			client := &stubMoyasarClient{payment: approvedRetry(declinedID), voidErr: tt.voidErr, refundErr: tt.refundErr}
			methods := &stubPaymentMethodRepo{}
			h := webhook.NewHandler(client, nil, nil, methods, s.lifecycle, s.GetLogger())
			services := &webhook.ServiceDependencies{PaymentService: s.paymentSvc}

			// Delivered twice: the retry must map to one payment and one card.
			for range 2 {
				s.Require().NoError(h.HandleWebhookEvent(ctx, paidEvent, types.GetEnvironmentID(ctx), services))
			}

			created, err := s.GetStores().PaymentRepo.GetByIdempotencyKey(ctx, "moyasar_card_check_moy_retry")
			s.Require().NoError(err)
			adopted, err := s.GetStores().PaymentRepo.Get(ctx, created.ID)
			s.Require().NoError(err)
			s.Equal(types.PaymentDestinationTypeCustomer, adopted.DestinationType)
			s.Equal(s.customerID, adopted.DestinationID)
			s.Equal("moy_retry", lo.FromPtr(adopted.GatewayPaymentID))
			s.Equal(tt.wantStatus, adopted.PaymentStatus)

			declined, err := s.GetStores().PaymentRepo.Get(ctx, declinedID)
			s.Require().NoError(err)
			s.Equal(types.PaymentStatusFailed, declined.PaymentStatus)

			s.Require().Len(methods.created, 1)
			s.Equal("token_retry", methods.created[0].GatewayMethodID)
			s.Equal(s.customerID, methods.created[0].CustomerID)
			s.Contains(client.voided, "moy_retry")
		})
	}
}

// failingPaymentService refuses to create payments; GetPayment serves one record.
type failingPaymentService struct {
	interfaces.PaymentService
	payment *dto.PaymentResponse
}

func (f *failingPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	return f.payment, nil
}

func (f *failingPaymentService) CreatePayment(_ context.Context, _ *dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
	return nil, errors.New("database unavailable")
}

// An orphaned charge that can't get its own payment is released directly.
func TestOrphanedCardCheck_ReleasedWhenItCannotBeRecorded(t *testing.T) {
	tests := []struct {
		name         string
		voidErr      error
		refundErr    error
		wantRefunded bool
		wantErr      bool
	}{
		{name: "void succeeds"},
		{name: "refund after a failed void", voidErr: errors.New("void window closed"), wantRefunded: true},
		// Nothing holds the charge, so the error asks Moyasar to redeliver.
		{name: "both fail", voidErr: errors.New("void failed"), refundErr: errors.New("refund failed"), wantRefunded: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			closed := &dto.PaymentResponse{
				ID:                "pay_declined",
				DestinationType:   types.PaymentDestinationTypeCustomer,
				DestinationID:     "cust_1",
				PaymentStatus:     types.PaymentStatusFailed,
				GatewayPaymentID:  lo.ToPtr("moy_declined"),
				PaymentMethodType: types.PaymentMethodTypeCard,
				Amount:            decimal.NewFromInt(1),
				Currency:          "SAR",
			}
			paymentSvc := &failingPaymentService{payment: closed}
			log := logger.NewNoopLogger()
			client := &stubMoyasarClient{payment: approvedRetry(closed.ID), voidErr: tt.voidErr, refundErr: tt.refundErr}
			h := webhook.NewHandler(client, nil, nil, &stubPaymentMethodRepo{}, payments.NewPaymentLifecycle(paymentSvc, nil, log), log)

			err := h.HandleWebhookEvent(context.Background(), paidEvent, "env_1", &webhook.ServiceDependencies{PaymentService: paymentSvc})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{"moy_retry"}, client.voided)
			if tt.wantRefunded {
				assert.Equal(t, []string{"moy_retry"}, client.refunded)
			} else {
				assert.Empty(t, client.refunded)
			}
		})
	}
}
