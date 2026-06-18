package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/payment"
	domainPM "github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/integration"
	moyasardto "github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"
)

// MoyasarCronActivities handles Moyasar-specific cron jobs.
type MoyasarCronActivities struct {
	paymentRepo        payment.Repository
	paymentMethodRepo  domainPM.Repository
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

func NewMoyasarCronActivities(
	paymentRepo payment.Repository,
	paymentMethodRepo domainPM.Repository,
	integrationFactory *integration.Factory,
	log *logger.Logger,
) *MoyasarCronActivities {
	return &MoyasarCronActivities{
		paymentRepo:        paymentRepo,
		paymentMethodRepo:  paymentMethodRepo,
		integrationFactory: integrationFactory,
		logger:             log,
	}
}

// VoidMoyasarAuthPaymentsActivity fetches all SUCCEEDED Moyasar AUTH payments across all
// tenants/environments and voids them (falling back to refund if void fails).
func (a *MoyasarCronActivities) VoidMoyasarAuthPaymentsActivity(ctx context.Context) (*cronModels.MoyasarAuthPaymentVoidWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting VoidMoyasarAuthPaymentsActivity")

	payments, err := a.paymentRepo.ListSucceededMoyasarAuthPayments(ctx)
	if err != nil {
		a.logger.Error(ctx, "failed to list Moyasar auth payments", "error", err)
		return nil, err
	}

	result := &cronModels.MoyasarAuthPaymentVoidWorkflowResult{Total: len(payments)}

	for i, payment := range payments {
		if i%50 == 0 {
			activity.RecordHeartbeat(ctx, i)
		}

		if payment.GatewayPaymentID == nil || *payment.GatewayPaymentID == "" {
			a.logger.Error(ctx, "auth payment missing gateway_payment_id, skipping", "payment_id", payment.ID)
			result.Failed++
			continue
		}

		envCtx := types.SetTenantID(ctx, payment.TenantID)
		envCtx = types.SetEnvironmentID(envCtx, payment.EnvironmentID)

		moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(envCtx)
		if err != nil {
			a.logger.Error(envCtx, "failed to get Moyasar integration",
				"payment_id", payment.ID, "tenant_id", payment.TenantID, "error", err)
			result.Failed++
			continue
		}

		gatewayPaymentID := *payment.GatewayPaymentID

		_, voidErr := moyasarIntegration.Client.VoidPayment(envCtx, gatewayPaymentID)
		if voidErr == nil {
			a.logger.Info(envCtx, "voided auth payment", "payment_id", payment.ID, "gateway_payment_id", gatewayPaymentID)
			payment.PaymentStatus = types.PaymentStatusVoided
			_ = a.paymentRepo.Update(envCtx, payment)
			result.Voided++
			continue
		}

		a.logger.Error(envCtx, "void failed, attempting refund",
			"payment_id", payment.ID, "gateway_payment_id", gatewayPaymentID, "void_error", voidErr)

		_, refundErr := moyasarIntegration.Client.RefundPayment(envCtx, gatewayPaymentID, moyasardto.SetupIntentAmount)
		if refundErr == nil {
			a.logger.Info(envCtx, "refunded auth payment", "payment_id", payment.ID, "gateway_payment_id", gatewayPaymentID)
			payment.PaymentStatus = types.PaymentStatusRefunded
			_ = a.paymentRepo.Update(envCtx, payment)
			result.Refunded++
			continue
		}

		a.logger.Error(envCtx, "both void and refund failed",
			"payment_id", payment.ID, "gateway_payment_id", gatewayPaymentID, "refund_error", refundErr)
		result.Failed++
	}

	log.Info("Completed VoidMoyasarAuthPaymentsActivity",
		"total", result.Total, "voided", result.Voided,
		"refunded", result.Refunded, "failed", result.Failed)
	return result, nil
}

// ReconcilePendingMoyasarPaymentsActivity pulls all PENDING Moyasar payments, re-fetches
// their status from Moyasar, and advances them to SUCCEEDED or FAILED.
// For AUTH payments that succeed, it also activates the associated payment method (token).
func (a *MoyasarCronActivities) ReconcilePendingMoyasarPaymentsActivity(ctx context.Context) (*cronModels.MoyasarReconcilePendingWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting ReconcilePendingMoyasarPaymentsActivity")

	payments, err := a.paymentRepo.ListPendingMoyasarPayments(ctx)
	if err != nil {
		a.logger.Error(ctx, "failed to list pending Moyasar payments", "error", err)
		return nil, err
	}

	result := &cronModels.MoyasarReconcilePendingWorkflowResult{Total: len(payments)}

	for i, p := range payments {
		if i%50 == 0 {
			activity.RecordHeartbeat(ctx, i)
		}

		if p.GatewayPaymentID == nil || *p.GatewayPaymentID == "" {
			a.logger.Error(ctx, "pending payment missing gateway_payment_id, skipping", "payment_id", p.ID)
			result.Skipped++
			continue
		}

		envCtx := types.SetTenantID(ctx, p.TenantID)
		envCtx = types.SetEnvironmentID(envCtx, p.EnvironmentID)

		moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(envCtx)
		if err != nil {
			a.logger.Error(envCtx, "failed to get Moyasar integration",
				"payment_id", p.ID, "error", err)
			result.Skipped++
			continue
		}

		gatewayPaymentID := *p.GatewayPaymentID

		moyasarPayment, err := moyasarIntegration.Client.GetPayment(envCtx, gatewayPaymentID)
		if err != nil {
			a.logger.Error(envCtx, "failed to fetch payment from Moyasar",
				"payment_id", p.ID, "gateway_payment_id", gatewayPaymentID, "error", err)
			result.Skipped++
			continue
		}

		switch moyasardto.MoyasarPaymentStatus(moyasarPayment.Status) {
		case moyasardto.MoyasarPaymentStatusPaid, moyasardto.MoyasarPaymentStatusCaptured:
			now := time.Now().UTC()
			p.PaymentStatus = types.PaymentStatusSucceeded
			p.SucceededAt = lo.ToPtr(now)
			if updateErr := a.paymentRepo.Update(envCtx, p); updateErr != nil {
				a.logger.Error(envCtx, "failed to update payment to succeeded",
					"payment_id", p.ID, "error", updateErr)
				result.Failed++
				continue
			}
			// For AUTH payments, activate the token (payment method).
			if p.DestinationType == types.PaymentDestinationTypeAuth {
				a.activatePaymentMethod(envCtx, p)
			}
			result.Succeeded++

		case moyasardto.MoyasarPaymentStatusFailed:
			p.PaymentStatus = types.PaymentStatusFailed
			if updateErr := a.paymentRepo.Update(envCtx, p); updateErr != nil {
				a.logger.Error(envCtx, "failed to update payment to failed",
					"payment_id", p.ID, "error", updateErr)
			}
			result.Failed++

		default:
			// Still in-flight (initiated, etc.) — leave as PENDING for next run.
			result.Skipped++
		}
	}

	log.Info("Completed ReconcilePendingMoyasarPaymentsActivity",
		"total", result.Total, "succeeded", result.Succeeded,
		"failed", result.Failed, "skipped", result.Skipped)
	return result, nil
}

// activatePaymentMethod looks up the token saved alongside an AUTH payment and activates it.
func (a *MoyasarCronActivities) activatePaymentMethod(ctx context.Context, p *payment.Payment) {
	tokenID := ""
	if p.Metadata != nil {
		tokenID = p.Metadata["token_id"]
	}
	if tokenID == "" {
		a.logger.Error(ctx, "auth payment has no token_id in metadata, cannot activate payment method",
			"payment_id", p.ID)
		return
	}

	inactiveStatus := types.PaymentMethodStatusInactive
	methods, err := a.paymentMethodRepo.List(ctx, &types.PaymentMethodFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  p.DestinationID,
		Status:      &inactiveStatus,
	})
	if err != nil {
		a.logger.Error(ctx, "failed to list inactive payment methods for activation",
			"customer_id", p.DestinationID, "error", err)
		return
	}

	for _, pm := range methods {
		if pm.GatewayMethodID == tokenID {
			pm.PaymentMethodStatus = types.PaymentMethodStatusActive
			if updateErr := a.paymentMethodRepo.Update(ctx, pm); updateErr != nil {
				a.logger.Error(ctx, "failed to activate payment method",
					"payment_method_id", pm.ID, "token_id", tokenID, "error", updateErr)
			} else {
				a.logger.Info(ctx, "activated payment method",
					"payment_method_id", pm.ID, "token_id", tokenID, "customer_id", p.DestinationID)
			}
			return
		}
	}

	a.logger.Error(ctx, "no inactive payment method found for token",
		"token_id", tokenID, "customer_id", p.DestinationID)
}
