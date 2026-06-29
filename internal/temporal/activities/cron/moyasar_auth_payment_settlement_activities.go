package cron

import (
	"context"
	"time"

	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// MoyasarAuthPaymentSettlementActivities handles cron reconciliation of Moyasar CUSTOMER payments.
type MoyasarAuthPaymentSettlementActivities struct {
	integrationFactory *integration.Factory
	paymentRepo        domainPayment.Repository
	logger             *logger.Logger
}

// NewMoyasarAuthPaymentSettlementActivities constructs MoyasarAuthPaymentSettlementActivities.
func NewMoyasarAuthPaymentSettlementActivities(
	integrationFactory *integration.Factory,
	paymentRepo domainPayment.Repository,
	log *logger.Logger,
) *MoyasarAuthPaymentSettlementActivities {
	return &MoyasarAuthPaymentSettlementActivities{
		integrationFactory: integrationFactory,
		paymentRepo:        paymentRepo,
		logger:             log,
	}
}

// ReconcilePendingAuthPaymentsActivity fetches all PENDING CUSTOMER payments across every
// tenant/environment and reconciles each one against Moyasar's current payment status.
func (a *MoyasarAuthPaymentSettlementActivities) ReconcilePendingAuthPaymentsActivity(ctx context.Context) (*cronModels.MoyasarReconcilePendingResult, error) {
	a.logger.Info(ctx, "starting ReconcilePendingAuthPaymentsActivity")

	result := &cronModels.MoyasarReconcilePendingResult{}

	pendingPayments, err := a.paymentRepo.ListScopedByDestinationStatusGateway(
		ctx,
		types.PaymentDestinationTypeCustomer,
		types.PaymentStatusPending,
		types.PaymentGatewayTypeMoyasar,
	)
	if err != nil {
		a.logger.Error(ctx, "failed to list pending CUSTOMER payments", "error", err)
		return nil, err
	}

	result.Total = len(pendingPayments)
	a.logger.Info(ctx, "fetched pending CUSTOMER payments", "count", result.Total)

	for _, p := range pendingPayments {
		// Re-establish tenant/environment scope so the integration and ledger
		// services resolve the correct connection and payment record.
		scopedCtx := types.SetTenantID(ctx, p.TenantID)
		scopedCtx = types.SetEnvironmentID(scopedCtx, p.EnvironmentID)

		moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(scopedCtx)
		if err != nil {
			a.logger.Error(scopedCtx, "moyasar integration not found, skipping payment",
				"payment_id", p.PaymentID,
				"tenant_id", p.TenantID,
				"environment_id", p.EnvironmentID,
				"error", err,
			)
			result.Errors++
			continue
		}

		statusResp, err := moyasarIntegration.PaymentSvc.GetPaymentStatus(scopedCtx, p.GatewayPaymentID)
		if err != nil {
			a.logger.Error(scopedCtx, "failed to get payment status from Moyasar",
				"payment_id", p.PaymentID,
				"gateway_payment_id", p.GatewayPaymentID,
				"error", err,
			)
			result.Errors++
			continue
		}

		a.reconcilePaymentStatus(scopedCtx, moyasarIntegration, p.PaymentID, p.GatewayPaymentID, statusResp, result)
	}

	a.logger.Info(ctx, "completed ReconcilePendingAuthPaymentsActivity",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)
	return result, nil
}

// reconcilePaymentStatus maps a Moyasar status to a lifecycle transition.
func (a *MoyasarAuthPaymentSettlementActivities) reconcilePaymentStatus(
	ctx context.Context,
	moyasarIntegration *integration.MoyasarIntegration,
	flexpricePaymentID string,
	gatewayPaymentID string,
	statusResp *moyasar.PaymentStatusResponse,
	result *cronModels.MoyasarReconcilePendingResult,
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
			result.Errors++
		} else {
			a.logger.Info(ctx, "reconciled pending CUSTOMER payment to SUCCEEDED",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
			result.Succeeded++
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
			result.Errors++
		} else {
			a.logger.Info(ctx, "reconciled pending CUSTOMER payment to FAILED",
				"payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
			result.Failed++
		}

	default:
		a.logger.Info(ctx, "no status transition needed for pending CUSTOMER payment",
			"payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"moyasar_status", statusResp.Status,
		)
		result.Skipped++
	}
}

// VoidOrRefundSucceededAuthPaymentsActivity fetches all SUCCEEDED CUSTOMER payments across
// every tenant/environment and voids or refunds each one via Moyasar, then records the
// outcome in the lifecycle.
func (a *MoyasarAuthPaymentSettlementActivities) VoidOrRefundSucceededAuthPaymentsActivity(ctx context.Context) (*cronModels.MoyasarVoidOrRefundResult, error) {
	a.logger.Info(ctx, "starting VoidOrRefundSucceededAuthPaymentsActivity")

	result := &cronModels.MoyasarVoidOrRefundResult{}

	succeededPayments, err := a.paymentRepo.ListScopedByDestinationStatusGateway(
		ctx,
		types.PaymentDestinationTypeCustomer,
		types.PaymentStatusSucceeded,
		types.PaymentGatewayTypeMoyasar,
	)
	if err != nil {
		a.logger.Error(ctx, "failed to list succeeded CUSTOMER payments", "error", err)
		return nil, err
	}

	result.Total = len(succeededPayments)
	a.logger.Info(ctx, "fetched succeeded CUSTOMER payments", "count", result.Total)

	for _, p := range succeededPayments {
		scopedCtx := types.SetTenantID(ctx, p.TenantID)
		scopedCtx = types.SetEnvironmentID(scopedCtx, p.EnvironmentID)

		moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(scopedCtx)
		if err != nil {
			a.logger.Error(scopedCtx, "moyasar integration not found, skipping payment",
				"payment_id", p.PaymentID,
				"tenant_id", p.TenantID,
				"environment_id", p.EnvironmentID,
				"error", err,
			)
			result.Errors++
			continue
		}

		voided, refunded, err := moyasarIntegration.VoidOrRefundAuthPayment(scopedCtx, p.PaymentID, p.GatewayPaymentID)
		if err != nil {
			a.logger.Error(scopedCtx, "failed to void or refund CUSTOMER payment",
				"payment_id", p.PaymentID,
				"gateway_payment_id", p.GatewayPaymentID,
				"error", err,
			)
			result.Errors++
			continue
		}

		switch {
		case voided:
			result.Voided++
		case refunded:
			result.Refunded++
		default:
			result.Skipped++
		}

		a.logger.Info(scopedCtx, "processed CUSTOMER payment void/refund",
			"payment_id", p.PaymentID,
			"gateway_payment_id", p.GatewayPaymentID,
			"voided", voided,
			"refunded", refunded,
		)
	}

	a.logger.Info(ctx, "completed VoidOrRefundSucceededAuthPaymentsActivity",
		"total", result.Total,
		"voided", result.Voided,
		"refunded", result.Refunded,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)
	return result, nil
}
