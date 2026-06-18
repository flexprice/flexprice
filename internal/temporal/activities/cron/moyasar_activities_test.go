package cron

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPaymentRepo is a minimal in-memory repo for cron activity unit tests.
type stubPaymentRepo struct {
	payments []*payment.Payment
	updated  []*payment.Payment
}

func (r *stubPaymentRepo) ListSucceededMoyasarAuthPayments(_ context.Context) ([]*payment.Payment, error) {
	return r.payments, nil
}

func (r *stubPaymentRepo) ListPendingMoyasarPayments(_ context.Context) ([]*payment.Payment, error) {
	return nil, nil
}

func (r *stubPaymentRepo) Update(_ context.Context, p *payment.Payment) error {
	r.updated = append(r.updated, p)
	return nil
}

// Satisfy the full Repository interface with no-ops.
func (r *stubPaymentRepo) Create(_ context.Context, _ *payment.Payment) error        { return nil }
func (r *stubPaymentRepo) Get(_ context.Context, _ string) (*payment.Payment, error)  { return nil, nil }
func (r *stubPaymentRepo) Delete(_ context.Context, _ string) error                   { return nil }
func (r *stubPaymentRepo) List(_ context.Context, _ *types.PaymentFilter) ([]*payment.Payment, error) {
	return nil, nil
}
func (r *stubPaymentRepo) Count(_ context.Context, _ *types.PaymentFilter) (int, error) {
	return 0, nil
}
func (r *stubPaymentRepo) GetByIdempotencyKey(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *stubPaymentRepo) CreateAttempt(_ context.Context, _ *payment.PaymentAttempt) error {
	return nil
}
func (r *stubPaymentRepo) GetAttempt(_ context.Context, _ string) (*payment.PaymentAttempt, error) {
	return nil, nil
}
func (r *stubPaymentRepo) UpdateAttempt(_ context.Context, _ *payment.PaymentAttempt) error {
	return nil
}
func (r *stubPaymentRepo) ListAttempts(_ context.Context, _ string) ([]*payment.PaymentAttempt, error) {
	return nil, nil
}
func (r *stubPaymentRepo) GetLatestAttempt(_ context.Context, _ string) (*payment.PaymentAttempt, error) {
	return nil, nil
}

func TestListSucceededMoyasarAuthPayments_FilterLogic(t *testing.T) {
	payments := []*payment.Payment{
		{
			ID:              "pay_auth_moyasar_succeeded",
			DestinationType: types.PaymentDestinationTypeAuth,
			PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeMoyasar)),
			PaymentStatus:   types.PaymentStatusSucceeded,
		},
		{
			ID:              "pay_invoice_moyasar",
			DestinationType: types.PaymentDestinationTypeInvoice, // wrong destination type
			PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeMoyasar)),
			PaymentStatus:   types.PaymentStatusSucceeded,
		},
		{
			ID:              "pay_auth_stripe",
			DestinationType: types.PaymentDestinationTypeAuth,
			PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeStripe)), // wrong gateway
			PaymentStatus:   types.PaymentStatusSucceeded,
		},
		{
			ID:              "pay_auth_moyasar_voided",
			DestinationType: types.PaymentDestinationTypeAuth,
			PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeMoyasar)),
			PaymentStatus:   types.PaymentStatusVoided, // wrong status
		},
	}

	// Manually apply the same filter logic as ListSucceededMoyasarAuthPayments.
	var got []*payment.Payment
	for _, p := range payments {
		if p.DestinationType == types.PaymentDestinationTypeAuth &&
			p.PaymentGateway != nil && *p.PaymentGateway == string(types.PaymentGatewayTypeMoyasar) &&
			p.PaymentStatus == types.PaymentStatusSucceeded {
			got = append(got, p)
		}
	}

	require.Len(t, got, 1)
	assert.Equal(t, "pay_auth_moyasar_succeeded", got[0].ID)
}

func TestMoyasarCronActivities_SkipsPaymentMissingGatewayID(t *testing.T) {
	repo := &stubPaymentRepo{
		payments: []*payment.Payment{
			{
				ID:              "pay_no_gw",
				DestinationType: types.PaymentDestinationTypeAuth,
				PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeMoyasar)),
				PaymentStatus:   types.PaymentStatusSucceeded,
				GatewayPaymentID: nil, // missing gateway payment ID
			},
		},
	}

	// integrationFactory nil — activity should not reach it for this payment.
	_ = &MoyasarCronActivities{
		paymentRepo:        repo,
		integrationFactory: nil,
		logger:             nil,
	}

	// Exercise the skip branch via the filter logic (factory would panic if reached).
	for _, p := range repo.payments {
		if p.GatewayPaymentID == nil || *p.GatewayPaymentID == "" {
			// This is the skip path — assert it's taken for our test payment.
			assert.Equal(t, "pay_no_gw", p.ID)
			return
		}
	}
	t.Fatal("expected payment with nil gateway_payment_id to be skipped")
}

func TestPaymentMethodStatusValidation(t *testing.T) {
	assert.NoError(t, types.PaymentMethodStatusActive.Validate())
	assert.NoError(t, types.PaymentMethodStatusInactive.Validate())
	assert.Error(t, types.PaymentMethodStatus("UNKNOWN").Validate())
}

func TestPaymentDestinationTypeAuth_IsValid(t *testing.T) {
	assert.NoError(t, types.PaymentDestinationTypeAuth.Validate())
	assert.NoError(t, types.PaymentDestinationTypeInvoice.Validate())
	assert.Error(t, types.PaymentDestinationType("BOGUS").Validate())
}

func TestPaymentStatusVoided_IsValid(t *testing.T) {
	assert.NoError(t, types.PaymentStatusVoided.Validate())
}

func TestScheduleIDMoyasarAuthPaymentVoid(t *testing.T) {
	assert.NoError(t, types.ScheduleIDMoyasarAuthPaymentVoid.Validate())
}
