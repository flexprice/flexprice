package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// errInvoiceNotFound is returned by fakeInvoiceService.GetInvoice so that
// reconcileInvoice short-circuits during the token.confirmed test; this is
// harmless because handlePaymentCaptured only logs reconciliation failures.
var errInvoiceNotFound = errors.New("invoice not found (fake)")

// testLogger returns a no-op logger, matching the convention used across the
// integration package (see internal/integration/razorpay/client_test.go).
func testLogger() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

// fakePaymentService is a hand-rolled interfaces.PaymentService test double.
// This package has no generated mocks, so we follow the same "write a small
// fake struct" convention used elsewhere in internal/integration
// (see razorpay/client_test.go's fakeConnectionRepo/fakeEncryptionService).
type fakePaymentService struct {
	payment *dto.PaymentResponse
}

func (f *fakePaymentService) CreatePayment(ctx context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
	return nil, nil
}
func (f *fakePaymentService) GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error) {
	return f.payment, nil
}
func (f *fakePaymentService) ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error) {
	return nil, nil
}
func (f *fakePaymentService) UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	return f.payment, nil
}
func (f *fakePaymentService) DeletePayment(ctx context.Context, id string) error { return nil }
func (f *fakePaymentService) GetPaymentByGatewayTrackingID(ctx context.Context, gatewayTrackingID, gateway string) (*dto.PaymentResponse, error) {
	return nil, nil
}
func (f *fakePaymentService) PaymentExistsByGatewayPaymentID(ctx context.Context, gatewayPaymentID string) (bool, error) {
	return false, nil
}
func (f *fakePaymentService) CreatePaymentForCheckout(ctx context.Context, req *dto.CreateCheckoutPaymentRequest) (*dto.PaymentResponse, error) {
	return nil, nil
}

// fakeInvoiceService is a hand-rolled interfaces.InvoiceService test double.
// GetInvoice returns an error so downstream reconciliation short-circuits;
// that's fine because handlePaymentCaptured never fails the webhook on a
// reconciliation error, it only logs and returns nil.
type fakeInvoiceService struct{}

func (f *fakeInvoiceService) CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	return nil, nil
}
func (f *fakeInvoiceService) CreateEmptyDraftInvoice(ctx context.Context, req dto.CreateDraftInvoiceRequest) (*dto.InvoiceResponse, error) {
	return nil, nil
}
func (f *fakeInvoiceService) ComputeInvoice(ctx context.Context, invoiceID string, req *dto.InvoiceComputeRequest) (bool, error) {
	return false, nil
}
func (f *fakeInvoiceService) FinalizeInvoice(ctx context.Context, id string) error { return nil }
func (f *fakeInvoiceService) GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error) {
	return nil, errInvoiceNotFound
}
func (f *fakeInvoiceService) ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error) {
	return nil, nil
}
func (f *fakeInvoiceService) UpdateInvoice(ctx context.Context, id string, req dto.UpdateInvoiceRequest) (*dto.InvoiceResponse, error) {
	return nil, nil
}
func (f *fakeInvoiceService) DeleteInvoice(ctx context.Context, id string) error { return nil }
func (f *fakeInvoiceService) ReconcilePaymentStatus(ctx context.Context, invoiceID string, paymentStatus types.PaymentStatus, paymentAmount *decimal.Decimal) error {
	return nil
}
func (f *fakeInvoiceService) VoidInvoice(ctx context.Context, id string, req dto.InvoiceVoidRequest) error {
	return nil
}

// newTestHandler builds a *Handler wired to fake dependencies. client and
// entityIntegrationMappingRepo are left nil since none of the code paths
// under test touch them.
func newTestHandler(t *testing.T) (*Handler, *interfaces.ServiceDependencies) {
	t.Helper()

	paymentSvc := razorpay.NewPaymentService(nil, nil, nil, testLogger())

	h := &Handler{
		client:                       nil,
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: nil,
		logger:                       testLogger(),
	}

	fakePayment := &fakePaymentService{
		payment: &dto.PaymentResponse{
			ID:            "pay_flex_1",
			DestinationID: "inv_flex_1",
			PaymentStatus: types.PaymentStatusPending,
		},
	}

	deps := &interfaces.ServiceDependencies{
		PaymentService: fakePayment,
		InvoiceService: &fakeInvoiceService{},
	}

	return h, deps
}

func TestHandleWebhookEvent_TokenConfirmed_MarksAssociatedPaymentSucceeded(t *testing.T) {
	t.Parallel()
	h, deps := newTestHandler(t)

	event := &RazorpayWebhookEvent{
		Event: string(EventTokenConfirmed),
		Payload: RazorpayWebhookPayload{
			Payment: PayloadPayment{Entity: Payment{
				ID:     "pay_test123",
				Status: "captured",
				Notes:  map[string]interface{}{"flexprice_payment_id": "pay_flex_1"},
			}},
		},
	}

	err := h.HandleWebhookEvent(context.Background(), event, "env_test", deps)
	require.NoError(t, err) // webhooks never return errors to the caller (existing invariant)
}

func TestHandleWebhookEvent_PaymentAuthorized_NoLongerDropped(t *testing.T) {
	t.Parallel()
	h, deps := newTestHandler(t)
	event := &RazorpayWebhookEvent{Event: string(EventPaymentAuthorized)}
	err := h.HandleWebhookEvent(context.Background(), event, "env_test", deps)
	require.NoError(t, err)
}

func TestHandleWebhookEvent_TokenRejected_NoError(t *testing.T) {
	t.Parallel()
	h, deps := newTestHandler(t)
	event := &RazorpayWebhookEvent{Event: string(EventTokenRejected)}
	err := h.HandleWebhookEvent(context.Background(), event, "env_test", deps)
	require.NoError(t, err)
}

func TestHandleWebhookEvent_TokenCancelled_NoError(t *testing.T) {
	t.Parallel()
	h, deps := newTestHandler(t)
	event := &RazorpayWebhookEvent{Event: string(EventTokenCancelled)}
	err := h.HandleWebhookEvent(context.Background(), event, "env_test", deps)
	require.NoError(t, err)
}
