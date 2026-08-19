package webhook

// Tests for the checkout-session status branching shared by handlePaymentLinkPaid
// and handlePaymentCaptured: Pending → complete, Expired/Failed → refund, other → no-op.

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ── fake RazorpayClient (RefundPayment/FetchPayment exercised via PaymentService) ─

type webhookTestRazorpayClient struct {
	razorpay.RazorpayClient
	refundCalls []string // razorpayPaymentID per call
}

func (c *webhookTestRazorpayClient) FetchPayment(_ context.Context, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"refund_status": nil}, nil
}

func (c *webhookTestRazorpayClient) RefundPayment(_ context.Context, paymentID string, _ int64) (map[string]interface{}, error) {
	c.refundCalls = append(c.refundCalls, paymentID)
	return map[string]interface{}{"id": "rfnd_test001"}, nil
}

// ── fake cache.Locker — always acquires ──────────────────────────────────────

type webhookTestLock struct{}

func (webhookTestLock) AcquiredSuccessfully() bool      { return true }
func (webhookTestLock) Release(_ context.Context) error { return nil }

type webhookTestLocker struct{}

func (webhookTestLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	return webhookTestLock{}, nil
}

// ── inline entityintegrationmapping.Repository fake ──────────────────────────

type webhookTestMappingStore struct{}

func (webhookTestMappingStore) Create(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (webhookTestMappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (webhookTestMappingStore) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, nil
}
func (webhookTestMappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (webhookTestMappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (webhookTestMappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

// ── fake interfaces.PaymentService ───────────────────────────────────────────

type webhookTestPaymentService struct {
	interfaces.PaymentService
	payment     *dto.PaymentResponse
	updateCalls []dto.UpdatePaymentRequest
	attempts    []dto.RecordAttemptRequest
}

func (s *webhookTestPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	return s.payment, nil
}

func (s *webhookTestPaymentService) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	s.updateCalls = append(s.updateCalls, req)
	if req.PaymentStatus != nil {
		s.payment.PaymentStatus = types.PaymentStatus(*req.PaymentStatus)
	}
	return s.payment, nil
}

func (s *webhookTestPaymentService) RecordAttempt(_ context.Context, _ string, req dto.RecordAttemptRequest) error {
	s.attempts = append(s.attempts, req)
	return nil
}

// failedAttempts returns only the declined attempts, so assertions stay focused on
// charge failures rather than every attempt the handler records.
func (s *webhookTestPaymentService) failedAttempts() []dto.RecordAttemptRequest {
	var out []dto.RecordAttemptRequest
	for _, a := range s.attempts {
		if a.PaymentStatus == types.PaymentStatusFailed {
			out = append(out, a)
		}
	}
	return out
}

// ── fake interfaces.InvoiceService — used only by handlePaymentCaptured's standalone fallback ──

type webhookTestInvoiceService struct {
	interfaces.InvoiceService
}

func (webhookTestInvoiceService) GetInvoice(_ context.Context, id string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{
		Invoice: invoice.Invoice{ID: id, AmountPaid: decimal.Zero, AmountDue: decimal.Zero},
	}, nil
}

func (webhookTestInvoiceService) ReconcilePaymentStatus(_ context.Context, _ string, _ types.PaymentStatus, _ *decimal.Decimal) error {
	return nil
}

// ── fake interfaces.CheckoutSessionService ───────────────────────────────────

type webhookTestCheckoutSessionService struct {
	interfaces.CheckoutSessionService
	session       *dto.CheckoutSessionResponse // returned by List, matched by session.ID (see note on suite below)
	listErr       error
	completeCalls []string
}

func (s *webhookTestCheckoutSessionService) List(_ context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.session == nil || len(filter.CheckoutPaymentIDs) == 0 || filter.CheckoutPaymentIDs[0] != s.session.ID {
		return &dto.ListCheckoutSessionsResponse{}, nil
	}
	return &dto.ListCheckoutSessionsResponse{Items: []*dto.CheckoutSessionResponse{s.session}}, nil
}

func (s *webhookTestCheckoutSessionService) CompleteCheckoutSession(_ context.Context, sessionID string, _ *types.CheckoutProviderResult) error {
	s.completeCalls = append(s.completeCalls, sessionID)
	return nil
}

// ── fake interfaces.EntityIntegrationMappingService ──────────────────────────

type webhookTestMappingService struct {
	interfaces.EntityIntegrationMappingService
	entityIDByProviderEntityID map[string]string
}

func (s *webhookTestMappingService) GetEntityIntegrationMappings(_ context.Context, filter *types.EntityIntegrationMappingFilter) (*dto.ListEntityIntegrationMappingsResponse, error) {
	if len(filter.ProviderEntityIDs) == 0 {
		return &dto.ListEntityIntegrationMappingsResponse{}, nil
	}
	entityID, ok := s.entityIDByProviderEntityID[filter.ProviderEntityIDs[0]]
	if !ok {
		return &dto.ListEntityIntegrationMappingsResponse{}, nil
	}
	return &dto.ListEntityIntegrationMappingsResponse{
		Items: []*dto.EntityIntegrationMappingResponse{{EntityID: entityID}},
	}, nil
}

// ── test suite ───────────────────────────────────────────────────────────────
//
// NOTE: the fakes key on session.ID as the "checkout payment ID" for simplicity;
// real code correlates via CheckoutPaymentIDs, not session.ID.

type WebhookCheckoutBranchingSuite struct {
	suite.Suite
	ctx         context.Context
	handler     *Handler
	client      *webhookTestRazorpayClient
	paymentSvc  *webhookTestPaymentService
	checkoutSvc *webhookTestCheckoutSessionService
	mappingSvc  *webhookTestMappingService
	services    *ServiceDependencies
}

func TestWebhookCheckoutBranching(t *testing.T) {
	suite.Run(t, new(WebhookCheckoutBranchingSuite))
}

func (s *WebhookCheckoutBranchingSuite) SetupTest() {
	s.ctx = types.SetTenantID(context.Background(), "tenant_test")
	s.ctx = types.SetEnvironmentID(s.ctx, "env_test")

	s.client = &webhookTestRazorpayClient{}
	s.paymentSvc = &webhookTestPaymentService{
		payment: &dto.PaymentResponse{
			ID:                "pay_flex_001",
			PaymentStatus:     types.PaymentStatusInitiated,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			GatewayTrackingID: lo.ToPtr("plink_test001"),
		},
	}
	s.checkoutSvc = &webhookTestCheckoutSessionService{}
	s.mappingSvc = &webhookTestMappingService{
		entityIDByProviderEntityID: map[string]string{"plink_test001": "pay_flex_001"},
	}

	razorpayPaymentSvc := razorpay.NewPaymentService(
		s.client, nil, nil, webhookTestLocker{}, logger.NewNoopLogger(),
	)
	lifecycle := payments.NewPaymentLifecycle(s.paymentSvc, webhookTestInvoiceService{}, logger.NewNoopLogger())
	s.handler = NewHandler(s.client, razorpayPaymentSvc, webhookTestMappingStore{}, lifecycle, logger.NewNoopLogger())

	s.services = &ServiceDependencies{
		PaymentService:                  s.paymentSvc,
		CheckoutSessionService:          s.checkoutSvc,
		EntityIntegrationMappingService: s.mappingSvc,
		// Needed by handlePaymentCaptured's standalone fallback, which reconciles
		// the invoice; a nil InvoiceService would panic here.
		InvoiceService: webhookTestInvoiceService{},
	}
}

func (s *WebhookCheckoutBranchingSuite) makeEvent(paymentLinkID, razorpayPaymentID string) *RazorpayWebhookEvent {
	event := &RazorpayWebhookEvent{Event: string(EventPaymentLinkPaid)}
	event.Payload.PaymentLink.Entity.ID = paymentLinkID
	event.Payload.Payment.Entity.ID = razorpayPaymentID
	return event
}

func (s *WebhookCheckoutBranchingSuite) TestPendingSession_Completes() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusPending},
	}

	err := s.handler.handlePaymentLinkPaid(s.ctx, s.makeEvent("plink_test001", "pay_rzp_001"), s.services)

	s.NoError(err)
	s.Require().Len(s.checkoutSvc.completeCalls, 1)
	s.Empty(s.client.refundCalls, "pending session must not trigger a refund")
}

func (s *WebhookCheckoutBranchingSuite) TestExpiredSession_Refunds() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusExpired},
	}

	err := s.handler.handlePaymentLinkPaid(s.ctx, s.makeEvent("plink_test001", "pay_rzp_001"), s.services)

	s.NoError(err)
	s.Empty(s.checkoutSvc.completeCalls, "expired session must not be completed")
	s.Require().Len(s.client.refundCalls, 1)
	s.Equal("pay_rzp_001", s.client.refundCalls[0])
	s.Equal(types.PaymentStatusRefunded, s.paymentSvc.payment.PaymentStatus)
}

func (s *WebhookCheckoutBranchingSuite) TestFailedSession_Refunds() {
	// payment_link.cancelled/expired marks the session Failed, not Expired
	// (Expired is set only by the cleanup cron).
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusFailed},
	}

	err := s.handler.handlePaymentLinkPaid(s.ctx, s.makeEvent("plink_test001", "pay_rzp_001"), s.services)

	s.NoError(err)
	s.Empty(s.checkoutSvc.completeCalls)
	s.Require().Len(s.client.refundCalls, 1)
	s.Equal("pay_rzp_001", s.client.refundCalls[0])
	s.Equal(types.PaymentStatusRefunded, s.paymentSvc.payment.PaymentStatus)
}

func (s *WebhookCheckoutBranchingSuite) TestCompletedSession_NoOp() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusCompleted},
	}

	err := s.handler.handlePaymentLinkPaid(s.ctx, s.makeEvent("plink_test001", "pay_rzp_001"), s.services)

	s.NoError(err)
	s.Empty(s.checkoutSvc.completeCalls)
	s.Empty(s.client.refundCalls)
}

func (s *WebhookCheckoutBranchingSuite) TestNoSessionFound_NoOp() {
	err := s.handler.handlePaymentLinkPaid(s.ctx, s.makeEvent("plink_unknown", "pay_rzp_001"), s.services)

	s.NoError(err)
	s.Empty(s.checkoutSvc.completeCalls)
	s.Empty(s.client.refundCalls)
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_ExpiredSession_Refunds() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusExpired},
	}
	event := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	event.Payload.Payment.Entity.ID = "pay_rzp_001"
	event.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}

	err := s.handler.handlePaymentCaptured(s.ctx, event, s.services)

	s.NoError(err)
	s.Require().Len(s.client.refundCalls, 1)
	s.Equal("pay_rzp_001", s.client.refundCalls[0])
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_FailedSession_Refunds() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusFailed},
	}
	event := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	event.Payload.Payment.Entity.ID = "pay_rzp_001"
	event.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}

	err := s.handler.handlePaymentCaptured(s.ctx, event, s.services)

	s.NoError(err)
	s.Require().Len(s.client.refundCalls, 1)
	s.Equal("pay_rzp_001", s.client.refundCalls[0])
}

// ── payment.failed: a decline is one attempt, not our decision to stop ───────

func (s *WebhookCheckoutBranchingSuite) makeFailedEvent() *RazorpayWebhookEvent {
	event := &RazorpayWebhookEvent{Event: string(EventPaymentFailed)}
	event.Payload.Payment.Entity.ID = "pay_rzp_001"
	event.Payload.Payment.Entity.ErrorDescription = "card declined"
	event.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}
	return event
}

func (s *WebhookCheckoutBranchingSuite) pendingSession() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusPending},
	}
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_PendingSession_RecordsAttemptAndLeavesPaymentOpen() {
	s.pendingSession()
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Require().Len(s.paymentSvc.failedAttempts(), 1)
	s.Equal("card declined", s.paymentSvc.failedAttempts()[0].ErrorMessage)
	s.Equal("pay_rzp_001", s.paymentSvc.failedAttempts()[0].GatewayAttemptID)
	s.Equal(types.PaymentStatusPending, s.paymentSvc.payment.PaymentStatus,
		"a decline on an open checkout session must not seal the payment")
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_RepeatedDeclines_RecordEachAttempt() {
	s.pendingSession()
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	s.NoError(s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services))
	s.NoError(s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services))

	s.Len(s.paymentSvc.failedAttempts(), 2)
	s.Equal(types.PaymentStatusPending, s.paymentSvc.payment.PaymentStatus)
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_StandaloneLink_LeavesPaymentOpen() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Require().Len(s.paymentSvc.failedAttempts(), 1)
	s.Equal(types.PaymentStatusPending, s.paymentSvc.payment.PaymentStatus,
		"a standalone payment link stays open too — the customer can retry on the same link")
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_NonLinkPayment_SealsPayment() {
	s.paymentSvc.payment.PaymentMethodType = types.PaymentMethodTypeCard
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Empty(s.paymentSvc.failedAttempts())
	s.Equal(types.PaymentStatusFailed, s.paymentSvc.payment.PaymentStatus,
		"a one-shot card charge has no retry vehicle, so a decline is final")
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_NonPendingSession_SealsPayment() {
	s.checkoutSvc.session = &dto.CheckoutSessionResponse{
		CheckoutSession: &domainCheckout.CheckoutSession{ID: "pay_flex_001", CheckoutStatus: types.CheckoutStatusExpired},
	}
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Empty(s.paymentSvc.failedAttempts())
	s.Equal(types.PaymentStatusFailed, s.paymentSvc.payment.PaymentStatus)
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_SessionLookupError_DoesNotSeal() {
	s.checkoutSvc.listErr = ierr.NewError("db unavailable").Mark(ierr.ErrDatabase)
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Empty(s.paymentSvc.failedAttempts())
	s.Equal(types.PaymentStatusPending, s.paymentSvc.payment.PaymentStatus,
		"an unreadable session must fail open: sealing a live payment loses the capture")
}

// A card charge is finished when it declines, so a checkout-session outage must not
// stop it sealing. Only payment links need the lookup at all.
func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_NonLinkSealsEvenIfSessionLookupFails() {
	s.checkoutSvc.listErr = ierr.NewError("db unavailable").Mark(ierr.ErrDatabase)
	s.paymentSvc.payment.PaymentMethodType = types.PaymentMethodTypeCard
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	err := s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services)

	s.NoError(err)
	s.Equal(types.PaymentStatusFailed, s.paymentSvc.payment.PaymentStatus,
		"a card decline must seal regardless of checkout-session availability")
}

// If the lookup fails we cannot tell whether this payment belongs to a dead checkout.
// Falling through to the standalone branch would settle it and reconcile an archived
// invoice instead of refunding the late capture; leaving it PENDING is recoverable.
func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_SessionLookupError_DoesNotSettle() {
	s.checkoutSvc.listErr = ierr.NewError("db unavailable").Mark(ierr.ErrDatabase)
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	captured := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	captured.Payload.Payment.Entity.ID = "pay_rzp_late"
	captured.Payload.Payment.Entity.Amount = 50000
	captured.Payload.Payment.Entity.Currency = "INR"
	captured.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}

	err := s.handler.handlePaymentCaptured(s.ctx, captured, s.services)

	s.Error(err, "an unreadable session is surfaced, not swallowed")
	s.Equal(types.PaymentStatusPending, s.paymentSvc.payment.PaymentStatus,
		"an unreadable session must not be treated as 'no session' and settled")
	s.Empty(s.client.refundCalls)
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentFailed_ThenCaptured_Succeeds() {
	s.pendingSession()
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	s.NoError(s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services))

	captured := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	captured.Payload.Payment.Entity.ID = "pay_rzp_002"
	captured.Payload.Payment.Entity.Amount = 50000
	captured.Payload.Payment.Entity.Currency = "INR"
	captured.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}

	s.NoError(s.handler.handlePaymentCaptured(s.ctx, captured, s.services))

	s.Require().Len(s.checkoutSvc.completeCalls, 1,
		"the retry must still complete the checkout session")
}

// The winning charge belongs in the ledger too: without it the attempt history shows
// only declines and never says which transaction actually collected the money.
func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_Standalone_RecordsSucceededAttempt() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPending

	s.NoError(s.handler.handlePaymentFailed(s.ctx, s.makeFailedEvent(), s.services))

	captured := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	captured.Payload.Payment.Entity.ID = "pay_rzp_002"
	captured.Payload.Payment.Entity.Amount = 50000
	captured.Payload.Payment.Entity.Currency = "INR"
	captured.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}
	s.NoError(s.handler.handlePaymentCaptured(s.ctx, captured, s.services))

	s.Require().Len(s.paymentSvc.attempts, 2, "one decline then one capture")
	s.Equal(types.PaymentStatusFailed, s.paymentSvc.attempts[0].PaymentStatus)
	s.Equal(types.PaymentStatusSucceeded, s.paymentSvc.attempts[1].PaymentStatus)
	s.Equal("pay_rzp_002", s.paymentSvc.attempts[1].GatewayAttemptID,
		"the succeeded attempt names the transaction that collected")
	s.Equal(types.PaymentStatusSucceeded, s.paymentSvc.payment.PaymentStatus)
}

func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_NoSessionFound_FallsThroughToStandalone() {
	event := &RazorpayWebhookEvent{Event: string(EventPaymentCaptured)}
	event.Payload.Payment.Entity.ID = "pay_rzp_001"
	event.Payload.Payment.Entity.Amount = 50000
	event.Payload.Payment.Entity.Currency = "INR"
	event.Payload.Payment.Entity.Notes = map[string]interface{}{"flexprice_payment_id": "pay_flex_001"}

	err := s.handler.handlePaymentCaptured(s.ctx, event, s.services)

	s.NoError(err)
	s.Empty(s.client.refundCalls)
	s.Require().NotEmpty(s.paymentSvc.updateCalls, "standalone path must still update the payment to Succeeded")
	s.Equal(types.PaymentStatusSucceeded, s.paymentSvc.payment.PaymentStatus)
}
