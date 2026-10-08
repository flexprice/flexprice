package webhook

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCheckoutSessionServiceForStripe struct {
	interfaces.CheckoutSessionService
	session       *dto.CheckoutSessionResponse
	completeCalls []string
	completeErr   error
}

func (s *fakeCheckoutSessionServiceForStripe) GetByPaymentID(_ context.Context, paymentID string) (*dto.CheckoutSessionResponse, error) {
	if s.session == nil || paymentID != "pay_001" {
		return nil, nil
	}
	return s.session, nil
}

func (s *fakeCheckoutSessionServiceForStripe) CompleteCheckoutSession(_ context.Context, sessionID string, _ *types.CheckoutProviderResult) error {
	s.completeCalls = append(s.completeCalls, sessionID)
	return s.completeErr
}

func TestHandler_HandleCheckoutSessionForPayment_CompletesPendingSession(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	handler := &Handler{
		logger: log,
	}

	fakeCheckout := &fakeCheckoutSessionServiceForStripe{
		session: &dto.CheckoutSessionResponse{
			ID:             "cs_test_session_1",
			CheckoutStatus: types.CheckoutStatusPending,
		},
	}
	deps := &ServiceDependencies{
		CheckoutSessionService: fakeCheckout,
	}

	found, err := handler.handleCheckoutSessionForPayment(ctx, "pay_001", "pi_test_123", deps)
	require.NoError(t, err)
	assert.True(t, found)
	require.Len(t, fakeCheckout.completeCalls, 1)
	assert.Equal(t, "cs_test_session_1", fakeCheckout.completeCalls[0])
}

func TestHandler_HandleCheckoutSessionForPayment_IdempotentAlreadyExists(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	handler := &Handler{
		logger: log,
	}

	fakeCheckout := &fakeCheckoutSessionServiceForStripe{
		session: &dto.CheckoutSessionResponse{
			ID:             "cs_test_session_1",
			CheckoutStatus: types.CheckoutStatusPending,
		},
		completeErr: ierr.NewError("already completed").Mark(ierr.ErrAlreadyExists),
	}
	deps := &ServiceDependencies{
		CheckoutSessionService: fakeCheckout,
	}

	found, err := handler.handleCheckoutSessionForPayment(ctx, "pay_001", "pi_test_123", deps)
	require.NoError(t, err)
	assert.True(t, found)
	require.Len(t, fakeCheckout.completeCalls, 1)
}

func TestHandler_HandleCheckoutSessionForPayment_CompleteFailurePropagates(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	handler := &Handler{
		logger: log,
	}

	fakeCheckout := &fakeCheckoutSessionServiceForStripe{
		session: &dto.CheckoutSessionResponse{
			ID:             "cs_test_session_1",
			CheckoutStatus: types.CheckoutStatusPending,
		},
		completeErr: ierr.NewError("transient failure").Mark(ierr.ErrSystem),
	}
	deps := &ServiceDependencies{
		CheckoutSessionService: fakeCheckout,
	}

	// The session is still pending after this failure, so the error must reach the
	// caller and fail the webhook rather than being logged and dropped.
	found, err := handler.handleCheckoutSessionForPayment(ctx, "pay_001", "pi_test_123", deps)
	require.Error(t, err)
	assert.False(t, found)
}

func TestHandler_HandleCheckoutSessionForPayment_NoSession(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	handler := &Handler{
		logger: log,
	}

	fakeCheckout := &fakeCheckoutSessionServiceForStripe{
		session: nil,
	}
	deps := &ServiceDependencies{
		CheckoutSessionService: fakeCheckout,
	}

	found, err := handler.handleCheckoutSessionForPayment(ctx, "pay_unknown", "pi_test_123", deps)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, fakeCheckout.completeCalls)
}

func TestHandler_HandleCheckoutSessionForPayment_TerminalStatusIgnored(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	handler := &Handler{
		logger: log,
	}

	fakeCheckout := &fakeCheckoutSessionServiceForStripe{
		session: &dto.CheckoutSessionResponse{
			ID:             "cs_test_session_1",
			CheckoutStatus: types.CheckoutStatusCompleted,
		},
	}
	deps := &ServiceDependencies{
		CheckoutSessionService: fakeCheckout,
	}

	found, err := handler.handleCheckoutSessionForPayment(ctx, "pay_001", "pi_test_123", deps)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Empty(t, fakeCheckout.completeCalls)
}

func TestHandler_HandleCheckoutSessionForPayment_TerminalSessionIsHandled(t *testing.T) {
	for _, status := range []types.CheckoutStatus{types.CheckoutStatusExpired, types.CheckoutStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			handler := &Handler{logger: logger.NewNoopLogger()}
			fakeCheckout := &fakeCheckoutSessionServiceForStripe{
				session: &dto.CheckoutSessionResponse{ID: "cs_test_session_1", CheckoutStatus: status},
			}

			// No payment intent, so nothing reaches Stripe; the payment must still be
			// reported as handled so the standalone settlement path never runs.
			found, err := handler.handleCheckoutSessionForPayment(context.Background(), "pay_001", "",
				&ServiceDependencies{CheckoutSessionService: fakeCheckout})

			require.NoError(t, err)
			assert.True(t, found)
			assert.Empty(t, fakeCheckout.completeCalls)
		})
	}
}
