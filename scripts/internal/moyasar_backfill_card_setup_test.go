package internal

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	moyasarwebhook "github.com/flexprice/flexprice/internal/integration/moyasar/webhook"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type backfillMoyasarClient struct {
	moyasar.MoyasarClient
	charges map[string]*moyasar.MoyasarPayment
	voided  []string
}

func (c *backfillMoyasarClient) GetPayment(_ context.Context, id string) (*moyasar.MoyasarPayment, error) {
	charge, ok := c.charges[id]
	if !ok {
		return nil, ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}
	return charge, nil
}

func (c *backfillMoyasarClient) VoidPayment(_ context.Context, id string) (*moyasar.MoyasarPayment, error) {
	c.voided = append(c.voided, id)
	c.charges[id].Status = string(moyasar.MoyasarPaymentStatusVoided)
	return c.charges[id], nil
}

type backfillPaymentMethodRepo struct {
	paymentmethod.Repository
	methods   []*paymentmethod.PaymentMethod
	createErr error
}

func (r *backfillPaymentMethodRepo) Create(_ context.Context, pm *paymentmethod.PaymentMethod) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.methods = append(r.methods, pm)
	return nil
}

func (r *backfillPaymentMethodRepo) Count(_ context.Context, f *types.PaymentMethodFilter) (int, error) {
	return len(lo.Filter(r.methods, func(pm *paymentmethod.PaymentMethod, _ int) bool {
		return pm.GatewayMethodID == lo.FromPtr(f.GatewayMethodID) && pm.CustomerID == lo.FromPtr(f.CustomerID)
	})), nil
}

type recordingScope struct {
	interfaces.BillingEntityScope
	scoped []string
	err    error
}

func (s *recordingScope) ScopeToPayment(ctx context.Context, p *payment.Payment) (context.Context, error) {
	s.scoped = append(s.scoped, p.ID)
	return ctx, s.err
}

type MoyasarBackfillCardSetupSuite struct {
	testutil.BaseServiceTestSuite
	backfill  *moyasarCardSetupBackfill
	lifecycle *payments.PaymentLifecycle
	client    *backfillMoyasarClient
	pmRepo    *backfillPaymentMethodRepo
	scope     *recordingScope
	customer  *customer.Customer
}

func TestMoyasarBackfillCardSetup(t *testing.T) {
	suite.Run(t, new(MoyasarBackfillCardSetupSuite))
}

func (s *MoyasarBackfillCardSetupSuite) SetupTest() {
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
	paymentSvc := service.NewPaymentService(params)
	s.lifecycle = payments.NewPaymentLifecycle(paymentSvc, service.NewInvoiceService(params), s.GetLogger())

	s.client = &backfillMoyasarClient{charges: map[string]*moyasar.MoyasarPayment{}}
	s.pmRepo = &backfillPaymentMethodRepo{}
	s.scope = &recordingScope{}
	moyasarIntegration := &integration.MoyasarIntegration{
		Client:         s.client,
		WebhookHandler: moyasarwebhook.NewHandler(s.client, nil, nil, s.pmRepo, s.lifecycle, s.GetLogger()),
		Lifecycle:      s.lifecycle,
	}
	s.backfill = &moyasarCardSetupBackfill{
		paymentRepo: stores.PaymentRepo,
		paymentSvc:  paymentSvc,
		scope:       s.scope,
		moyasar: func(context.Context) (*integration.MoyasarIntegration, error) {
			return moyasarIntegration, nil
		},
	}

	s.customer = &customer.Customer{
		ID:         "cust_moyasar_backfill",
		ExternalID: "ext_cust_moyasar_backfill",
		Name:       "Moyasar Backfill Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(stores.CustomerRepo.Create(s.GetContext(), s.customer))
}

// failedSetupPayment is a card-setup payment a decline marked FAILED before this fix, with the
// charge Moyasar later reported for it.
func (s *MoyasarBackfillCardSetupSuite) failedSetupPayment(chargeID string, status moyasar.MoyasarPaymentStatus) string {
	id, err := s.lifecycle.InitiatePayment(s.GetContext(), payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeCustomer,
		DestinationID:     s.customer.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromInt(1),
		Currency:          moyasar.DefaultCurrency,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.lifecycle.RecordPaymentFailure(s.GetContext(), payments.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   chargeID,
		ErrorMessage:       "card declined",
	}))
	s.client.charges[chargeID] = &moyasar.MoyasarPayment{
		ID:       chargeID,
		Status:   string(status),
		Metadata: map[string]string{"flexprice_payment_id": id},
		Source:   &moyasar.PaymentSource{Type: "creditcard", Token: "token_" + chargeID},
	}
	return id
}

func (s *MoyasarBackfillCardSetupSuite) run() moyasarCardSetupBackfillSummary {
	summary, err := s.backfill.run(s.GetContext())
	s.Require().NoError(err)
	return summary
}

func (s *MoyasarBackfillCardSetupSuite) customerPaymentCount() int {
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationID = lo.ToPtr(s.customer.ID)
	list, err := s.GetStores().PaymentRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return len(list)
}

func (s *MoyasarBackfillCardSetupSuite) paymentStatus(id string) types.PaymentStatus {
	p, err := s.GetStores().PaymentRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return p.PaymentStatus
}

func (s *MoyasarBackfillCardSetupSuite) TestPaidCharge_SettledOnReplacement() {
	failedID := s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)

	summary := s.run()

	s.Equal(1, summary.scanned)
	s.Equal(1, summary.settled)
	s.Zero(summary.errors)
	s.Equal([]string{failedID}, s.scope.scoped)
	s.Equal(types.PaymentStatusFailed, s.paymentStatus(failedID))

	keyed, err := s.GetStores().PaymentRepo.GetByIdempotencyKey(s.GetContext(), "mp_paid")
	s.Require().NoError(err)
	replacement, err := s.GetStores().PaymentRepo.Get(s.GetContext(), keyed.ID)
	s.Require().NoError(err)
	s.NotEqual(failedID, replacement.ID)
	s.Equal(s.customer.ID, replacement.DestinationID)
	s.True(decimal.NewFromInt(1).Equal(replacement.Amount))
	s.Equal(types.PaymentStatusVoided, s.paymentStatus(replacement.ID))
	attempts, err := s.GetStores().PaymentRepo.ListAttempts(s.GetContext(), replacement.ID)
	s.Require().NoError(err)
	s.Require().Len(attempts, 1)
	s.Equal(types.PaymentStatusSucceeded, attempts[0].PaymentStatus)

	s.Require().Len(s.pmRepo.methods, 1)
	s.Equal("token_mp_paid", s.pmRepo.methods[0].GatewayMethodID)
	s.Equal([]string{"mp_paid"}, s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestCardSaveFails_KeepsChargeForRerun() {
	s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)
	s.pmRepo.createErr = errors.New("db down")

	summary := s.run()

	s.Zero(summary.settled)
	s.Equal(1, summary.errors)
	s.Empty(s.pmRepo.methods)
	s.Empty(s.client.voided)

	s.pmRepo.createErr = nil
	summary = s.run()

	s.Equal(1, summary.settled)
	s.Zero(summary.errors)
	s.Equal(2, s.customerPaymentCount())
	s.Len(s.pmRepo.methods, 1)
	s.Equal([]string{"mp_paid"}, s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestFailedCharge_Skipped() {
	s.failedSetupPayment("mp_failed", moyasar.MoyasarPaymentStatusFailed)

	summary := s.run()

	s.Equal(1, summary.scanned)
	s.Zero(summary.settled)
	s.Equal(map[string]int{"failed": 1}, summary.skippedByStatus)
	s.Equal(1, s.customerPaymentCount())
	s.Empty(s.pmRepo.methods)
	s.Empty(s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestAlreadyVoidedCharge_Skipped() {
	s.failedSetupPayment("mp_voided", moyasar.MoyasarPaymentStatusVoided)

	summary := s.run()

	s.Equal(map[string]int{"voided": 1}, summary.skippedByStatus)
	s.Equal(1, s.customerPaymentCount())
	s.Empty(s.pmRepo.methods)
	s.Empty(s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestRerun_CreatesNothingNew() {
	s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)
	s.Equal(1, s.run().settled)

	summary := s.run()

	s.Zero(summary.settled)
	s.Zero(summary.errors)
	s.Equal(map[string]int{"voided": 1}, summary.skippedByStatus)
	s.Equal(2, s.customerPaymentCount())
	s.Len(s.pmRepo.methods, 1)
	s.Equal([]string{"mp_paid"}, s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestRerunAfterFailedVoid_ReturnsChargeOnce() {
	s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)
	s.Require().NoError(s.lifecycle.RecordPaymentSuccess(s.GetContext(), payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: lo.Must(s.lifecycle.InitiatePayment(s.GetContext(), payments.InitiatePaymentParams{
			DestinationType:   types.PaymentDestinationTypeCustomer,
			DestinationID:     s.customer.ID,
			PaymentMethodType: types.PaymentMethodTypeCard,
			Gateway:           string(types.PaymentGatewayTypeMoyasar),
			Amount:            decimal.NewFromInt(1),
			Currency:          moyasar.DefaultCurrency,
			IdempotencyKey:    "mp_paid",
		})),
		GatewayPaymentID: "mp_paid",
	}))

	summary := s.run()

	s.Equal(1, summary.settled)
	s.Equal(2, s.customerPaymentCount())
	s.Equal([]string{"mp_paid"}, s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestDryRun_ChangesNothing() {
	s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)
	s.backfill.dryRun = true

	summary := s.run()

	s.Equal(1, summary.wouldSettleCount)
	s.Zero(summary.settled)
	s.Equal(1, s.customerPaymentCount())
	s.Empty(s.pmRepo.methods)
	s.Empty(s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestScopeError_CountedAndSkipped() {
	s.failedSetupPayment("mp_paid", moyasar.MoyasarPaymentStatusPaid)
	s.scope.err = errors.New("payment connection was removed")

	summary := s.run()

	s.Equal(1, summary.errors)
	s.Zero(summary.settled)
	s.Empty(s.client.voided)
}

func (s *MoyasarBackfillCardSetupSuite) TestChargeID_FallsBackToGivenID() {
	p := &payment.Payment{ID: "pay_without_gateway_id"}
	s.Equal(uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.ID)).String(), moyasarChargeID(p))

	p.GatewayPaymentID = lo.ToPtr("mp_stored")
	s.Equal("mp_stored", moyasarChargeID(p))
}
