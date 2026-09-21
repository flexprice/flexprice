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

func (s *fakeCheckoutSessionServiceForStripe) List(_ context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	if s.session == nil || len(filter.CheckoutPaymentIDs) == 0 || filter.CheckoutPaymentIDs[0] != "pay_001" {
		return &dto.ListCheckoutSessionsResponse{}, nil
	}
	return &dto.ListCheckoutSessionsResponse{Items: []*dto.CheckoutSessionResponse{s.session}}, nil
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
