package payments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type fakeRefundPaymentService struct {
	interfaces.PaymentService
	payment *dto.PaymentResponse
}

func (f *fakeRefundPaymentService) GetPayment(context.Context, string) (*dto.PaymentResponse, error) {
	return f.payment, nil
}

type fakeRefundInvoiceService struct {
	interfaces.InvoiceService
	status types.InvoiceStatus
}

func (f *fakeRefundInvoiceService) GetInvoice(context.Context, string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{Invoice: invoice.Invoice{InvoiceStatus: f.status}}, nil
}

type fakeRefunder struct {
	calls [][2]string
	err   error
}

func (f *fakeRefunder) RefundLateCapturedPayment(_ context.Context, payID, gwID string, _ interfaces.PaymentService) error {
	f.calls = append(f.calls, [2]string{payID, gwID})
	return f.err
}

func runRefundGuard(payStatus types.PaymentStatus, dest types.PaymentDestinationType, invStatus types.InvoiceStatus, refunder *fakeRefunder, log *logger.Logger) bool {
	return payments.RefundIfInvoiceVoided(context.Background(), payments.RefundIfInvoiceVoidedParams{
		PaymentService: &fakeRefundPaymentService{payment: &dto.PaymentResponse{
			ID: "pay_1", DestinationType: dest, DestinationID: "inv_1", PaymentStatus: payStatus,
		}},
		InvoiceService:     &fakeRefundInvoiceService{status: invStatus},
		Refunder:           refunder,
		FlexpricePaymentID: "pay_1",
		GatewayPaymentID:   "gw_1",
		Logger:             log,
	})
}

func TestRefundIfInvoiceVoided_RefundsOnVoidedInvoice(t *testing.T) {
	r := &fakeRefunder{}
	handled := runRefundGuard(types.PaymentStatusPending, types.PaymentDestinationTypeInvoice, types.InvoiceStatusVoided, r, logger.NewNoopLogger())
	require.True(t, handled)
	require.Equal(t, [][2]string{{"pay_1", "gw_1"}}, r.calls)
}

func TestRefundIfInvoiceVoided_RefundsVoidedPayment(t *testing.T) {
	r := &fakeRefunder{}
	handled := runRefundGuard(types.PaymentStatusVoided, types.PaymentDestinationTypeInvoice, types.InvoiceStatusVoided, r, logger.NewNoopLogger())
	require.True(t, handled)
	require.Len(t, r.calls, 1)
}

func TestRefundIfInvoiceVoided_SettlesOnFinalizedInvoice(t *testing.T) {
	r := &fakeRefunder{}
	handled := runRefundGuard(types.PaymentStatusPending, types.PaymentDestinationTypeInvoice, types.InvoiceStatusFinalized, r, logger.NewNoopLogger())
	require.False(t, handled)
	require.Empty(t, r.calls)
}

func TestRefundIfInvoiceVoided_CustomerDestinationNotHandled(t *testing.T) {
	r := &fakeRefunder{}
	handled := runRefundGuard(types.PaymentStatusPending, types.PaymentDestinationTypeCustomer, types.InvoiceStatusVoided, r, logger.NewNoopLogger())
	require.False(t, handled)
	require.Empty(t, r.calls)
}

func TestRefundIfInvoiceVoided_RefundErrorStillHandled(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	r := &fakeRefunder{err: errors.New("gateway down")}
	handled := runRefundGuard(types.PaymentStatusPending, types.PaymentDestinationTypeInvoice, types.InvoiceStatusVoided, r, logger.NewFromSugared(zap.New(core).Sugar()))
	require.True(t, handled)
	require.Len(t, r.calls, 1)
	require.Equal(t, 1, logs.Len())
	require.Contains(t, logs.All()[0].Message, "manual reconciliation required")
}
