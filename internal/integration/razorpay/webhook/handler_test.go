package webhook

// Tests for the checkout-session status branching shared by handlePaymentLinkPaid
// and handlePaymentCaptured: Pending → complete, Expired/Failed → refund, other →
// no-op, not-found → existing standalone fallback (only exercised for
// handlePaymentCaptured, since only that path has a standalone fallback).

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
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ── fake RazorpayClient (RefundPayment/FetchPayment exercised via PaymentService) ─

type webhookTestRazorpayClient struct {
	razorpay.RazorpayClient
	refundCalls []string // razorpayPaymentID per call
}

func (c *webhookTestRazorpayClient) FetchPayment(_ context.Context, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"refunded": false}, nil
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

// ── fake interfaces.InvoiceService — only touched by handlePaymentCaptured's
// standalone (no-checkout-session) reconciliation fallback ──────────────────

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
	completeCalls []string                     // sessionIDs passed to CompleteCheckoutSession
}

func (s *webhookTestCheckoutSessionService) List(_ context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
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
// NOTE: session.ID is reused as the "checkout payment ID" the fake session store
// keys on, purely to keep the fakes above simple (real code correlates via
// CheckoutPaymentIDs, not session.ID — the fakes just don't need that extra field).

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
		payment: &dto.PaymentResponse{ID: "pay_flex_001", PaymentStatus: types.PaymentStatusInitiated},
	}
	s.checkoutSvc = &webhookTestCheckoutSessionService{}
	s.mappingSvc = &webhookTestMappingService{
		entityIDByProviderEntityID: map[string]string{"plink_test001": "pay_flex_001"},
	}

	razorpayPaymentSvc := razorpay.NewPaymentService(
		s.client, nil, nil, webhookTestLocker{}, webhookTestMappingStore{}, logger.NewNoopLogger(),
	)
	s.handler = NewHandler(s.client, razorpayPaymentSvc, webhookTestMappingStore{}, logger.NewNoopLogger())

	s.services = &ServiceDependencies{
		PaymentService:                  s.paymentSvc,
		CheckoutSessionService:          s.checkoutSvc,
		EntityIntegrationMappingService: s.mappingSvc,
		// handlePaymentCaptured's standalone (no-checkout-session) fallback always
		// reconciles the invoice after marking the payment Succeeded — needed so
		// TestPaymentCaptured_NoSessionFound_FallsThroughToStandalone doesn't panic
		// on a nil InvoiceService.
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
	// A payment_link.cancelled/expired webhook marks the session Failed (not
	// Expired — that status is only set by the internal cleanup cron). A late
	// payment.captured for the same payment must still be refunded, not dropped.
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
	// No mapping registered for this payment link — GetEntityIntegrationMappings
	// returns empty, so handlePaymentLinkPaid returns before any session lookup.
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

func (s *WebhookCheckoutBranchingSuite) TestPaymentCaptured_NoSessionFound_FallsThroughToStandalone() {
	// No session at all for this payment — must fall back to the pre-existing
	// standalone reconciliation path (marks the payment Succeeded directly).
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
