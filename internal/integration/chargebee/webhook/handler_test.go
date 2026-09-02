package webhook

// Tests the routing decision in handlePaymentSucceeded: a payment that belongs to a
// checkout session must complete the session and must NOT also run
// ReconcileInvoicePayment, which double-counts and never credits the wallet.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/shopspring/decimal"

	transactionModel "github.com/chargebee/chargebee-go/v3/models/transaction"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/chargebee"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

const (
	testChargebeeInvoiceID = "cb_inv_001"
	testFlexpriceInvoiceID = "inv_flex_001"
	testChargebeeTxnID     = "txn_001"
	testFlexpricePaymentID = "pay_flex_001"
)

// ── entityintegrationmapping.Repository fake: maps one Chargebee invoice ─────

type mappingStore struct {
	entityIDByProviderEntityID map[string]string
}

func (mappingStore) Create(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (mappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (s mappingStore) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	if len(filter.ProviderEntityIDs) == 0 {
		return nil, nil
	}
	entityID, ok := s.entityIDByProviderEntityID[filter.ProviderEntityIDs[0]]
	if !ok {
		return nil, nil
	}
	return []*entityintegrationmapping.EntityIntegrationMapping{{EntityID: entityID}}, nil
}
func (mappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (mappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (mappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

// ── interfaces.CheckoutSessionService fake ───────────────────────────────────

type fakeCheckoutSessionService struct {
	interfaces.CheckoutSessionService
	session        *dto.CheckoutSessionResponse
	listErr        error
	completeCalls  []string
	completeErr    error
	cleanupCalls   []string
	cleanupReasons []error
}

func (s *fakeCheckoutSessionService) List(_ context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.session == nil || len(filter.CheckoutPaymentIDs) == 0 || filter.CheckoutPaymentIDs[0] != testFlexpricePaymentID {
		return &dto.ListCheckoutSessionsResponse{}, nil
	}
	return &dto.ListCheckoutSessionsResponse{Items: []*dto.CheckoutSessionResponse{s.session}}, nil
}

func (s *fakeCheckoutSessionService) CompleteCheckoutSession(_ context.Context, sessionID string, _ *types.CheckoutProviderResult) error {
	s.completeCalls = append(s.completeCalls, sessionID)
	return s.completeErr
}

func (s *fakeCheckoutSessionService) CleanupCheckoutSession(_ context.Context, sessionID string, reason error) error {
	s.cleanupCalls = append(s.cleanupCalls, sessionID)
	s.cleanupReasons = append(s.cleanupReasons, reason)
	return nil
}

// ── chargebee.ChargebeeClient fake: only the refund endpoints are exercised ──

type refundClient struct {
	chargebee.ChargebeeClient
	amount         int64
	amountRefunded int64
	refundCalls    []int64
	refundErr      error
}

func (c *refundClient) RetrieveTransaction(_ context.Context, _ string) (*transactionModel.Transaction, error) {
	txn := &transactionModel.Transaction{Id: testChargebeeTxnID, Amount: c.amount}
	if c.amountRefunded > 0 {
		txn.LinkedRefunds = []*transactionModel.LinkedRefund{{
			TxnId:     "txn_refund_000",
			TxnStatus: transactionEnum.StatusSuccess,
			TxnAmount: c.amountRefunded,
		}}
	}
	return txn, nil
}

func (c *refundClient) RefundTransaction(_ context.Context, _ string, amountMinor int64, _ string) (*transactionModel.Transaction, error) {
	c.refundCalls = append(c.refundCalls, amountMinor)
	if c.refundErr != nil {
		return nil, c.refundErr
	}
	return &transactionModel.Transaction{Id: "txn_refund_001"}, nil
}

// ── interfaces.PaymentService fake ───────────────────────────────────────────

type fakePaymentService struct {
	interfaces.PaymentService
	payment    *dto.PaymentResponse
	getErr     error
	updateReqs []dto.UpdatePaymentRequest
}

func (s *fakePaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.payment, nil
}

func (s *fakePaymentService) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	s.updateReqs = append(s.updateReqs, req)
	return s.payment, nil
}

// ── cache.Locker fake: always grants the lock ────────────────────────────────

type grantingLocker struct{}

func (grantingLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	return grantedLock{}, nil
}

type grantedLock struct{}

func (grantedLock) AcquiredSuccessfully() bool      { return true }
func (grantedLock) Release(_ context.Context) error { return nil }

// ── suite ────────────────────────────────────────────────────────────────────

type ChargebeeWebhookCheckoutSuite struct {
	suite.Suite
	ctx         context.Context
	handler     *Handler
	checkoutSvc *fakeCheckoutSessionService
	paymentSvc  *fakePaymentService
	client      *refundClient
	services    *ServiceDependencies
}

func TestChargebeeWebhookCheckout(t *testing.T) {
	suite.Run(t, new(ChargebeeWebhookCheckoutSuite))
}

func (s *ChargebeeWebhookCheckoutSuite) SetupTest() {
	s.ctx = types.SetTenantID(context.Background(), "tenant_test")
	s.ctx = types.SetEnvironmentID(s.ctx, "env_test")

	log := logger.NewNoopLogger()

	invoiceSvc := &chargebee.InvoiceService{
		InvoiceServiceParams: chargebee.InvoiceServiceParams{
			EntityIntegrationMappingRepo: mappingStore{
				entityIDByProviderEntityID: map[string]string{
					testChargebeeInvoiceID: testFlexpriceInvoiceID,
				},
			},
			Logger: log,
		},
	}

	s.client = &refundClient{amount: 10000}
	s.handler = NewHandler(s.client, invoiceSvc, chargebee.NewPaymentService(s.client, grantingLocker{}, log), log)
	s.checkoutSvc = &fakeCheckoutSessionService{}
	s.paymentSvc = &fakePaymentService{payment: &dto.PaymentResponse{
		ID:            testFlexpricePaymentID,
		Amount:        decimal.NewFromInt(100),
		Currency:      "USD",
		PaymentStatus: types.PaymentStatusSucceeded,
	}}
	s.services = &ServiceDependencies{CheckoutSessionService: s.checkoutSvc, PaymentService: s.paymentSvc}
}

func (s *ChargebeeWebhookCheckoutSuite) seedSession(status types.CheckoutStatus) *dto.CheckoutSessionResponse {
	session := &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{
			ID:             "cs_001",
			CheckoutStatus: status,
		},
	}
	s.checkoutSvc.session = session
	return session
}

// contentForInvoice builds event content against a specific Chargebee invoice id,
// so a test can choose whether that id has a FlexPrice mapping.
func (s *ChargebeeWebhookCheckoutSuite) contentForInvoice(chargebeeInvoiceID string) json.RawMessage {
	content, err := json.Marshal(map[string]any{
		"transaction": map[string]any{"id": testChargebeeTxnID, "amount": 10000, "currency_code": "USD"},
		"invoice":     map[string]any{"id": chargebeeInvoiceID, "po_number": testFlexpricePaymentID},
	})
	s.Require().NoError(err)
	return content
}

func (s *ChargebeeWebhookCheckoutSuite) event(eventType ChargebeeEventType) *ChargebeeWebhookEvent {
	content, err := json.Marshal(map[string]any{
		"transaction": map[string]any{"id": testChargebeeTxnID, "amount": 10000, "currency_code": "USD"},
		"invoice":     map[string]any{"id": testChargebeeInvoiceID, "po_number": testFlexpricePaymentID},
	})
	s.Require().NoError(err)
	return &ChargebeeWebhookEvent{ID: "ev_001", EventType: string(eventType), Content: content}
}

// The whole point of Phase 4: a checkout payment completes the session.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_PendingSessionIsCompleted() {
	session := s.seedSession(types.CheckoutStatusPending)

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled, "a checkout payment must not fall through to invoice reconciliation")
	s.Equal([]string{session.ID}, s.checkoutSvc.completeCalls)
}

// At-least-once delivery: the second webhook must be a quiet no-op, not an error.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_AlreadyCompletedIsNotAnError() {
	s.seedSession(types.CheckoutStatusPending)
	s.checkoutSvc.completeErr = ierr.NewError("already completed").Mark(ierr.ErrAlreadyExists)

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled)
}

// An ordinary invoice payment has no session and must reconcile as before.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_NoSessionFallsThrough() {
	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, "pay_no_session", testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.False(handled, "a standalone payment must still reach ReconcileInvoicePayment")
	s.Empty(s.checkoutSvc.completeCalls)
}

// A late payment on a terminal session must not be completed — the invoice and
// payment are archived by then, so the money is refunded instead.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_ExpiredSessionIsRefunded() {
	s.seedSession(types.CheckoutStatusExpired)

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled, "must still not fall through: reconciling would credit an archived invoice")
	s.Empty(s.checkoutSvc.completeCalls)
	s.Equal([]int64{10000}, s.client.refundCalls, "the full payment amount, in minor units")

	s.Require().Len(s.paymentSvc.updateReqs, 1)
	s.Equal(string(types.PaymentStatusRefunded), *s.paymentSvc.updateReqs[0].PaymentStatus)
	s.Require().NotNil(s.paymentSvc.updateReqs[0].RefundedAt)
	s.Equal("txn_refund_001", (*s.paymentSvc.updateReqs[0].Metadata)["chargebee_refund_id"])
}

// A redelivered webhook must not refund twice.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_AlreadyRefundedPaymentIsNotRefundedAgain() {
	s.seedSession(types.CheckoutStatusFailed)
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusRefunded

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled)
	s.Empty(s.client.refundCalls)
	s.Empty(s.paymentSvc.updateReqs)
}

// Chargebee itself is the second guard: a transaction with nothing left to refund
// must not receive another refund submission.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_FullyRefundedTransactionIsNotResubmitted() {
	s.seedSession(types.CheckoutStatusExpired)
	s.client.amountRefunded = 10000

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled)
	s.Empty(s.client.refundCalls)
	s.Require().Len(s.paymentSvc.updateReqs, 1, "the FlexPrice payment still has to be marked refunded")
	s.Nil(s.paymentSvc.updateReqs[0].Metadata, "no refund id to record when nothing was submitted")
}

// A lookup failure must not silently fall through to the double-counting path.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_LookupErrorDoesNotFallThrough() {
	s.checkoutSvc.listErr = ierr.NewError("db down").Mark(ierr.ErrDatabase)

	handled, err := s.handler.handleCheckoutSessionForPayment(s.ctx, testFlexpricePaymentID, testFlexpriceInvoiceID, testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().Error(err)
	s.False(handled)
	s.Empty(s.checkoutSvc.completeCalls)
}

func (s *ChargebeeWebhookCheckoutSuite) TestPaymentFailed_CleansUpPendingSession() {
	session := s.seedSession(types.CheckoutStatusPending)

	s.Require().NoError(s.handler.HandleWebhookEvent(s.ctx, s.event(EventPaymentFailed), "env_test", s.services))

	s.Equal([]string{session.ID}, s.checkoutSvc.cleanupCalls)
	s.Require().Len(s.checkoutSvc.cleanupReasons, 1)
	s.Require().NotNil(s.checkoutSvc.cleanupReasons[0], "a failure must be recorded as failed, not expired")
	s.Contains(s.checkoutSvc.cleanupReasons[0].Error(), testChargebeeTxnID)
}

// Cleanup archives the session's invoice and payment, so it must never run against
// a session that already settled.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentFailed_CompletedSessionUntouched() {
	s.seedSession(types.CheckoutStatusCompleted)

	s.Require().NoError(s.handler.HandleWebhookEvent(s.ctx, s.event(EventPaymentFailed), "env_test", s.services))

	s.Empty(s.checkoutSvc.cleanupCalls)
}

func (s *ChargebeeWebhookCheckoutSuite) TestUnhandledEventTypeIsIgnored() {
	s.seedSession(types.CheckoutStatusPending)

	s.Require().NoError(s.handler.HandleWebhookEvent(s.ctx, s.event(EventInvoiceUpdated), "env_test", s.services))

	s.Empty(s.checkoutSvc.completeCalls)
	s.Empty(s.checkoutSvc.cleanupCalls)
}

// A hosted checkout page creates its own Chargebee invoice with no entity mapping.
// po_number carries the Flexprice payment id — verified live against the test site.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_HostedPageRoutesByPONumber() {
	session := s.seedSession(types.CheckoutStatusPending)

	handled, err := s.handler.handleCheckoutSessionForPayment(
		s.ctx, testFlexpricePaymentID, "", testChargebeeInvoiceID, testChargebeeTxnID, s.services)

	s.Require().NoError(err)
	s.True(handled, "an unmapped hosted-page invoice must still find its session")
	s.Equal([]string{session.ID}, s.checkoutSvc.completeCalls)
}

// End to end through HandleWebhookEvent: no invoice mapping exists, so routing must
// come from po_number alone.
func (s *ChargebeeWebhookCheckoutSuite) TestHostedPageWebhook_UnmappedInvoiceCompletesSession() {
	session := s.seedSession(types.CheckoutStatusPending)

	content, err := json.Marshal(map[string]any{
		"transaction": map[string]any{"id": testChargebeeTxnID, "amount": 1000, "currency_code": "USD"},
		"invoice":     map[string]any{"id": "cb_inv_unmapped", "po_number": testFlexpricePaymentID},
	})
	s.Require().NoError(err)

	s.Require().NoError(s.handler.HandleWebhookEvent(s.ctx, &ChargebeeWebhookEvent{
		ID: "ev_hosted", EventType: string(EventPaymentSucceeded), Content: content,
	}, "env_test", s.services))

	s.Equal([]string{session.ID}, s.checkoutSvc.completeCalls)
}

// ── interfaces.InvoiceService fake: records reconciliations ──────────────────

type fakeInvoiceService struct {
	interfaces.InvoiceService
	reconciled []string
}

func (s *fakeInvoiceService) ReconcilePaymentStatus(_ context.Context, id string, _ types.PaymentStatus, _ *decimal.Decimal) error {
	s.reconciled = append(s.reconciled, id)
	return nil
}

// A payment link issued for an existing invoice has no checkout session, and the
// hosted page invoices the charge on a Chargebee invoice of its own — so neither
// route that predates it can settle the money. po_number is the only thread back.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_LinkPaymentIsSettledViaPONumber() {
	invoiceSvc := &fakeInvoiceService{}
	s.services.InvoiceService = invoiceSvc
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending
	s.paymentSvc.payment.DestinationType = types.PaymentDestinationTypeInvoice
	s.paymentSvc.payment.DestinationID = testFlexpriceInvoiceID

	event := s.event(EventPaymentSucceeded)
	event.Content = s.contentForInvoice("cb_inv_unmapped")

	s.Require().NoError(s.handler.handlePaymentSucceeded(s.ctx, event, s.services))

	s.Require().Len(s.paymentSvc.updateReqs, 1, "the payment itself must move to SUCCEEDED")
	s.Equal(string(types.PaymentStatusSucceeded), *s.paymentSvc.updateReqs[0].PaymentStatus)
	s.Equal(testChargebeeTxnID, *s.paymentSvc.updateReqs[0].GatewayPaymentID)
	s.Equal([]string{testFlexpriceInvoiceID}, invoiceSvc.reconciled)
}

// po_number is free text a tenant can set on any Chargebee invoice, so anything
// that is not one of our payment ids must be left alone.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_UnknownPONumberIsIgnored() {
	invoiceSvc := &fakeInvoiceService{}
	s.services.InvoiceService = invoiceSvc
	s.paymentSvc.getErr = ierr.NewError("not found").Mark(ierr.ErrNotFound)

	event := s.event(EventPaymentSucceeded)
	event.Content = s.contentForInvoice("cb_inv_unmapped")

	s.Require().NoError(s.handler.handlePaymentSucceeded(s.ctx, event, s.services))

	s.Empty(s.paymentSvc.updateReqs)
	s.Empty(invoiceSvc.reconciled)
}

// A checkout session still wins: settling the payment directly would skip the
// wallet credit the session completion performs.
func (s *ChargebeeWebhookCheckoutSuite) TestPaymentSucceeded_SessionWinsOverLinkSettlement() {
	invoiceSvc := &fakeInvoiceService{}
	s.services.InvoiceService = invoiceSvc
	session := s.seedSession(types.CheckoutStatusPending)
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	event := s.event(EventPaymentSucceeded)
	event.Content = s.contentForInvoice("cb_inv_unmapped")

	s.Require().NoError(s.handler.handlePaymentSucceeded(s.ctx, event, s.services))

	s.Equal([]string{session.ID}, s.checkoutSvc.completeCalls)
	s.Empty(s.paymentSvc.updateReqs, "the session owns settlement")
	s.Empty(invoiceSvc.reconciled)
}
