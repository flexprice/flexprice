package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// MoyasarAuthPaymentRefundActivities handles cron reconciliation of Moyasar AUTH payments.
type MoyasarAuthPaymentRefundActivities struct {
	integrationFactory *integration.Factory
	paymentService     interfaces.PaymentService
	logger             *logger.Logger
}

// NewMoyasarAuthPaymentRefundActivities constructs MoyasarAuthPaymentRefundActivities.
func NewMoyasarAuthPaymentRefundActivities(
	integrationFactory *integration.Factory,
	paymentService interfaces.PaymentService,
	log *logger.Logger,
) *MoyasarAuthPaymentRefundActivities {
	return &MoyasarAuthPaymentRefundActivities{
		integrationFactory: integrationFactory,
		paymentService:     paymentService,
		logger:             log,
	}
}

// ReconcilePendingAuthPaymentsActivity fetches all PENDING AUTH payments and reconciles
// each one against Moyasar's current payment status.
func (a *MoyasarAuthPaymentRefundActivities) ReconcilePendingAuthPaymentsActivity(ctx context.Context) error {
	a.logger.Info(ctx, "starting ReconcilePendingAuthPaymentsActivity")

	moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		a.logger.Error(ctx, "moyasar integration not found, skipping activity", "error", err)
		return nil
	}

	destType := string(types.PaymentDestinationTypeCustomer)
	status := string(types.PaymentStatusPending)

	paymentsResp, err := a.paymentService.ListPayments(ctx, &types.PaymentFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		DestinationType: lo.ToPtr(destType),
		PaymentStatus:   lo.ToPtr(status),
	})
	if err != nil {
		a.logger.Error(ctx, "failed to list pending AUTH payments", "error", err)
		return err
	}

	a.logger.Info(ctx, "fetched pending AUTH payments", "count", len(paymentsResp.Items))

	for _, paymentResp := range paymentsResp.Items {
		if paymentResp.GatewayPaymentID == nil || *paymentResp.GatewayPaymentID == "" {
			a.logger.Info(ctx, "skipping pending AUTH payment with no gateway payment ID", "payment_id", paymentResp.ID)
			continue
		}

		statusResp, err := moyasarIntegration.PaymentSvc.GetPaymentStatus(ctx, *paymentResp.GatewayPaymentID)
		if err != nil {
			a.logger.Error(ctx, "failed to get payment status from Moyasar",
				"payment_id", paymentResp.ID,
				"gateway_payment_id", *paymentResp.GatewayPaymentID,
				"error", err,
			)
			continue
		}

		a.reconcilePaymentStatus(ctx, moyasarIntegration, paymentResp.ID, *paymentResp.GatewayPaymentID, statusResp)
	}

	a.logger.Info(ctx, "completed ReconcilePendingAuthPaymentsActivity")
	return nil
}

// reconcilePaymentStatus maps a Moyasar status to a lifecycle transition.
func (a *MoyasarAuthPaymentRefundActivities) reconcilePaymentStatus(
	ctx context.Context,
	moyasarIntegration *integration.MoyasarIntegration,
	flexpricePaymentID string,
	gatewayPaymentID string,
	statusResp *moyasar.PaymentStatusResponse,
) {
	switch statusResp.Status {
	case string(moyasar.MoyasarPaymentStatusPaid), string(moyasar.MoyasarPaymentStatusCaptured):
		if err := moyasarIntegration.Lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   gatewayPaymentID,
			SucceededAt:        time.Now().UTC(),
		}); err != nil {
			a.logger.Error(ctx, "failed to record payment success",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
				"error", err,
			)
		} else {
			a.logger.Info(ctx, "reconciled pending AUTH payment to SUCCEEDED",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
		}

	case string(moyasar.MoyasarPaymentStatusFailed):
		if err := moyasarIntegration.Lifecycle.RecordPaymentFailure(ctx, payments.RecordPaymentFailureParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   gatewayPaymentID,
			FailedAt:           time.Now().UTC(),
			ErrorMessage:       "payment failed (reconciled by cron)",
		}); err != nil {
			a.logger.Error(ctx, "failed to record payment failure",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
				"error", err,
			)
		} else {
			a.logger.Info(ctx, "reconciled pending AUTH payment to FAILED",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
		}

	default:
		a.logger.Info(ctx, "no status transition needed for pending AUTH payment",
			"payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"moyasar_status", statusResp.Status,
		)
	}
}

// VoidOrRefundSucceededAuthPaymentsActivity fetches all SUCCEEDED AUTH payments and voids
// or refunds each one via Moyasar, then records the outcome in the lifecycle.
func (a *MoyasarAuthPaymentRefundActivities) VoidOrRefundSucceededAuthPaymentsActivity(ctx context.Context) error {
	a.logger.Info(ctx, "starting VoidOrRefundSucceededAuthPaymentsActivity")

	moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		a.logger.Error(ctx, "moyasar integration not found, skipping activity", "error", err)
		return nil
	}

	destType := string(types.PaymentDestinationTypeCustomer)
	status := string(types.PaymentStatusSucceeded)

	paymentsResp, err := a.paymentService.ListPayments(ctx, &types.PaymentFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		DestinationType: lo.ToPtr(destType),
		PaymentStatus:   lo.ToPtr(status),
	})
	if err != nil {
		a.logger.Error(ctx, "failed to list succeeded AUTH payments", "error", err)
		return err
	}

	a.logger.Info(ctx, "fetched succeeded AUTH payments", "count", len(paymentsResp.Items))

	for _, paymentResp := range paymentsResp.Items {
		if paymentResp.GatewayPaymentID == nil || *paymentResp.GatewayPaymentID == "" {
			a.logger.Info(ctx, "skipping succeeded AUTH payment with no gateway payment ID", "payment_id", paymentResp.ID)
			continue
		}

		voided, refunded, err := moyasarIntegration.VoidOrRefundAuthPayment(ctx, paymentResp.ID, *paymentResp.GatewayPaymentID)
		if err != nil {
			a.logger.Error(ctx, "failed to void or refund AUTH payment",
				"payment_id", paymentResp.ID,
				"gateway_payment_id", *paymentResp.GatewayPaymentID,
				"error", err,
			)
			continue
		}

		a.logger.Info(ctx, "processed AUTH payment void/refund",
			"payment_id", paymentResp.ID,
			"gateway_payment_id", *paymentResp.GatewayPaymentID,
			"voided", voided,
			"refunded", refunded,
		)
	}

	a.logger.Info(ctx, "completed VoidOrRefundSucceededAuthPaymentsActivity")
	return nil
}
