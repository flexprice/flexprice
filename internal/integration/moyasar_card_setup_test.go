package integration_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	moyasarwebhook "github.com/flexprice/flexprice/internal/integration/moyasar/webhook"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type fakeMoyasarClient struct {
	moyasar.MoyasarClient
	charges  map[string]*moyasar.MoyasarPayment
	voided   []string
	refunded []string
}

func (c *fakeMoyasarClient) GetMoyasarConfig(context.Context) (*moyasar.MoyasarConfig, error) {
	return &moyasar.MoyasarConfig{PublishableKey: "pk_test"}, nil
}

func (c *fakeMoyasarClient) GetPayment(_ context.Context, id string) (*moyasar.MoyasarPayment, error) {
	charge, ok := c.charges[id]
	if !ok {
		return nil, ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}
	return charge, nil
}

func (c *fakeMoyasarClient) VoidPayment(_ context.Context, id string) (*moyasar.MoyasarPayment, error) {
	c.voided = append(c.voided, id)
	return c.charges[id], nil
}

func (c *fakeMoyasarClient) RefundPayment(_ context.Context, id string, _ int) (*moyasar.RefundPaymentResponse, error) {
	c.refunded = append(c.refunded, id)
	return &moyasar.RefundPaymentResponse{}, nil
}

type fakePaymentMethodRepo struct {
	paymentmethod.Repository
	methods []*paymentmethod.PaymentMethod
}

func (r *fakePaymentMethodRepo) Create(_ context.Context, pm *paymentmethod.PaymentMethod) error {
	r.methods = append(r.methods, pm)
	return nil
}

func (r *fakePaymentMethodRepo) Count(_ context.Context, f *types.PaymentMethodFilter) (int, error) {
	return len(lo.Filter(r.methods, func(pm *paymentmethod.PaymentMethod, _ int) bool {
		return pm.GatewayMethodID == lo.FromPtr(f.GatewayMethodID) && pm.CustomerID == lo.FromPtr(f.CustomerID)
	})), nil
}

type MoyasarCardSetupSuite struct {
	testutil.BaseServiceTestSuite
	moyasar  *integration.MoyasarIntegration
	client   *fakeMoyasarClient
	pmRepo   *fakePaymentMethodRepo
	deps     *moyasarwebhook.ServiceDependencies
	customer *customer.Customer
}

func TestMoyasarCardSetup(t *testing.T) {
	suite.Run(t, new(MoyasarCardSetupSuite))
}

func (s *MoyasarCardSetupSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	stores := s.GetStores()
	params := service.ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		SubRepo:             stores.SubscriptionRepo,
		PlanRepo:            stores.PlanRepo,
		PriceRepo:           stores.PriceRepo,
		EventRepo:           stores.EventRepo,
		MeterRepo:           stores.MeterRepo,
		CustomerRepo:        stores.CustomerRepo,
		InvoiceRepo:         stores.InvoiceRepo,
		EntitlementRepo:     stores.EntitlementRepo,
		SettingsRepo:        stores.SettingsRepo,
		FeatureRepo:         stores.FeatureRepo,
		TenantRepo:          stores.TenantRepo,
		UserRepo:            stores.UserRepo,
		AuthRepo:            stores.AuthRepo,
		WalletRepo:          stores.WalletRepo,
		PaymentRepo:         stores.PaymentRepo,
		ConnectionRepo:      stores.ConnectionRepo,
		CheckoutSessionRepo: stores.CheckoutSessionRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}
	paymentSvc := service.NewPaymentService(params)
	invoiceSvc := service.NewInvoiceService(params)
	lifecycle := payments.NewPaymentLifecycle(paymentSvc, invoiceSvc, s.GetLogger())

	s.client = &fakeMoyasarClient{charges: map[string]*moyasar.MoyasarPayment{}}
	s.pmRepo = &fakePaymentMethodRepo{}
	moyasarPaymentSvc := moyasar.NewPaymentService(s.client, nil, nil, s.GetLogger())
	s.moyasar = &integration.MoyasarIntegration{
		Client:            s.client,
		PaymentSvc:        moyasarPaymentSvc,
		WebhookHandler:    moyasarwebhook.NewHandler(s.client, moyasarPaymentSvc, nil, s.pmRepo, lifecycle, s.GetLogger()),
		Lifecycle:         lifecycle,
		PaymentMethodRepo: s.pmRepo,
		PaymentRepo:       stores.PaymentRepo,
		Logger:            s.GetLogger(),
	}
	s.deps = &moyasarwebhook.ServiceDependencies{PaymentService: paymentSvc, InvoiceService: invoiceSvc}

	s.customer = &customer.Customer{
		ID:         "cust_moyasar_setup",
		ExternalID: "ext_cust_moyasar_setup",
		Name:       "Moyasar Setup Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(stores.CustomerRepo.Create(s.GetContext(), s.customer))
}

func (s *MoyasarCardSetupSuite) initiateSetup() string {
	id, key, err := s.moyasar.InitiateTokenization(s.GetContext(), s.customer.ID)
	s.Require().NoError(err)
	s.Equal("pk_test", key)
	return id
}

func (s *MoyasarCardSetupSuite) deliver(eventType moyasarwebhook.MoyasarEventType, moyasarPaymentID string) error {
	return s.moyasar.WebhookHandler.HandleWebhookEvent(s.GetContext(), &moyasarwebhook.MoyasarWebhookEvent{
		ID:   "evt_" + moyasarPaymentID,
		Type: eventType,
		Data: moyasarwebhook.PaymentEventData{ID: moyasarPaymentID},
	}, types.GetEnvironmentID(s.GetContext()), s.deps)
}

func (s *MoyasarCardSetupSuite) decline(flexpricePaymentID, moyasarPaymentID string) {
	s.client.charges[moyasarPaymentID] = &moyasar.MoyasarPayment{
		ID:       moyasarPaymentID,
		Status:   string(moyasar.MoyasarPaymentStatusFailed),
		Metadata: map[string]string{"flexprice_payment_id": flexpricePaymentID},
		Source:   &moyasar.PaymentSource{Type: "creditcard", Message: "card declined"},
	}
	s.Require().NoError(s.deliver(moyasarwebhook.EventPaymentFailed, moyasarPaymentID))
}

func (s *MoyasarCardSetupSuite) approve(flexpricePaymentID, moyasarPaymentID string) {
	s.client.charges[moyasarPaymentID] = &moyasar.MoyasarPayment{
		ID:       moyasarPaymentID,
		Status:   string(moyasar.MoyasarPaymentStatusPaid),
		Metadata: map[string]string{"flexprice_payment_id": flexpricePaymentID},
		Source:   &moyasar.PaymentSource{Type: "creditcard", Token: "token_" + moyasarPaymentID},
	}
	s.Require().NoError(s.deliver(moyasarwebhook.EventPaymentPaid, moyasarPaymentID))
}

func (s *MoyasarCardSetupSuite) paymentStatus(id string) types.PaymentStatus {
	p, err := s.GetStores().PaymentRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return p.PaymentStatus
}

func (s *MoyasarCardSetupSuite) customerPaymentCount() int {
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationID = lo.ToPtr(s.customer.ID)
	list, err := s.GetStores().PaymentRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return len(list)
}

func (s *MoyasarCardSetupSuite) TestSetupIntent_ReusesUntouchedPayment() {
	first := s.initiateSetup()
	s.Equal(first, s.initiateSetup())
	s.Equal(1, s.customerPaymentCount())
}

func (s *MoyasarCardSetupSuite) TestDecline_LeavesPaymentOpenWithFailedAttempt() {
	id := s.initiateSetup()
	s.decline(id, "mp_declined")

	s.Equal(types.PaymentStatusInitiated, s.paymentStatus(id))
	attempts, err := s.GetStores().PaymentRepo.ListAttempts(s.GetContext(), id)
	s.Require().NoError(err)
	s.Require().Len(attempts, 1)
	s.Equal(types.PaymentStatusFailed, attempts[0].PaymentStatus)
	s.Equal("mp_declined", lo.FromPtr(attempts[0].GatewayAttemptID))
}

func (s *MoyasarCardSetupSuite) TestApprovalAfterDecline_SavesCardAndVoidsCharge() {
	id := s.initiateSetup()
	s.decline(id, "mp_declined")
	s.approve(id, "mp_approved")

	s.Equal(types.PaymentStatusVoided, s.paymentStatus(id))
	s.Require().Len(s.pmRepo.methods, 1)
	s.Equal("token_mp_approved", s.pmRepo.methods[0].GatewayMethodID)
	s.Equal([]string{"mp_approved"}, s.client.voided)
}

func (s *MoyasarCardSetupSuite) TestApprovalOnDeclinedGatewayPayment_SavesCardAndVoidsCharge() {
	id := s.initiateSetup()
	s.decline(id, "mp_resubmitted")
	s.approve(id, "mp_resubmitted")

	s.Equal(types.PaymentStatusVoided, s.paymentStatus(id))
	s.Len(s.pmRepo.methods, 1)
	s.Equal([]string{"mp_resubmitted"}, s.client.voided)
}

func (s *MoyasarCardSetupSuite) TestSetupIntentAfterDecline_ReturnsNewPayment() {
	id := s.initiateSetup()
	s.decline(id, "mp_declined")

	next := s.initiateSetup()
	s.NotEqual(id, next)
	s.Equal(next, s.initiateSetup())
}

func (s *MoyasarCardSetupSuite) TestSetupIntentAfterCardSaved_ReturnsNewPayment() {
	id := s.initiateSetup()
	s.approve(id, "mp_approved")

	s.NotEqual(id, s.initiateSetup())
}

func (s *MoyasarCardSetupSuite) TestLateFailedEventAfterSuccess_LeavesStatusUnchanged() {
	id := s.initiateSetup()
	s.approve(id, "mp_approved")
	s.decline(id, "mp_late_declined")

	s.Equal(types.PaymentStatusVoided, s.paymentStatus(id))
}

func (s *MoyasarCardSetupSuite) legacyFailedSetupPayment() string {
	id, err := s.moyasar.Lifecycle.InitiatePayment(s.GetContext(), payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeCustomer,
		DestinationID:     s.customer.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromInt(1),
		Currency:          moyasar.DefaultCurrency,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.moyasar.Lifecycle.RecordPaymentFailure(s.GetContext(), payments.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "mp_legacy_declined",
		ErrorMessage:       "card declined",
	}))
	return id
}

func (s *MoyasarCardSetupSuite) TestChargeOnLegacyFailedPayment_RecoveredOnReplacement() {
	failedID := s.legacyFailedSetupPayment()
	s.approve(failedID, "mp_legacy_paid")

	s.Equal(types.PaymentStatusFailed, s.paymentStatus(failedID))
	keyed, err := s.GetStores().PaymentRepo.GetByIdempotencyKey(s.GetContext(), "mp_legacy_paid")
	s.Require().NoError(err)
	replacement, err := s.GetStores().PaymentRepo.Get(s.GetContext(), keyed.ID)
	s.Require().NoError(err)
	s.NotEqual(failedID, replacement.ID)
	s.Equal(types.PaymentStatusVoided, replacement.PaymentStatus)
	s.Equal("mp_legacy_paid", lo.FromPtr(replacement.GatewayPaymentID))
	s.Require().Len(s.pmRepo.methods, 1)
	s.Equal([]string{"mp_legacy_paid"}, s.client.voided)
}

func (s *MoyasarCardSetupSuite) TestChargeOnLegacyFailedPayment_RedeliveryDoesNotDuplicate() {
	failedID := s.legacyFailedSetupPayment()
	s.approve(failedID, "mp_legacy_paid")
	s.Require().NoError(s.deliver(moyasarwebhook.EventPaymentPaid, "mp_legacy_paid"))

	s.Equal(2, s.customerPaymentCount())
	s.Len(s.pmRepo.methods, 1)
	s.Equal([]string{"mp_legacy_paid"}, s.client.voided)
}

func (s *MoyasarCardSetupSuite) TestInvoiceDecline_MarksPaymentFailed() {
	inv := &invoice.Invoice{
		ID:              "inv_moyasar_setup",
		CustomerID:      s.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "sar",
		AmountDue:       decimal.NewFromInt(100),
		AmountRemaining: decimal.NewFromInt(100),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	id, err := s.moyasar.Lifecycle.InitiatePayment(s.GetContext(), payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromInt(100),
		Currency:          moyasar.DefaultCurrency,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.moyasar.Lifecycle.ConfirmGatewayPayment(s.GetContext(), id, "mp_invoice_declined"))

	s.decline(id, "mp_invoice_declined")

	s.Equal(types.PaymentStatusFailed, s.paymentStatus(id))
}
