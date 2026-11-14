package cron

import (
	"context"

	"github.com/samber/lo"

	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

var savedMethodTypes = []types.PaymentMethodType{types.PaymentMethodTypeENACH, types.PaymentMethodTypeUPI, types.PaymentMethodTypeCard}

// RazorpayPaymentStatusSyncActivities settles in-flight Razorpay payments on a schedule.
type RazorpayPaymentStatusSyncActivities struct {
	paymentRepo domainPayment.Repository
	paymentSvc  interfaces.PaymentService
	logger      *logger.Logger
}

// NewRazorpayPaymentStatusSyncActivities constructs RazorpayPaymentStatusSyncActivities.
func NewRazorpayPaymentStatusSyncActivities(paymentRepo domainPayment.Repository, paymentSvc interfaces.PaymentService, log *logger.Logger) *RazorpayPaymentStatusSyncActivities {
	return &RazorpayPaymentStatusSyncActivities{paymentRepo: paymentRepo, paymentSvc: paymentSvc, logger: log}
}

// SyncRazorpayInFlightPaymentsActivity runs the gateway sync GET /payments/:id does for every pending or
// processing Razorpay saved-method debit, across all tenants and environments. Payment links are left to their webhooks.
func (a *RazorpayPaymentStatusSyncActivities) SyncRazorpayInFlightPaymentsActivity(ctx context.Context) (*cronModels.RazorpayPaymentStatusSyncResult, error) {
	result := &cronModels.RazorpayPaymentStatusSyncResult{}
	for _, status := range []types.PaymentStatus{types.PaymentStatusPending, types.PaymentStatusProcessing} {
		inFlight, err := a.paymentRepo.ListScopedByDestinationStatusGateway(ctx, types.PaymentDestinationTypeInvoice, status, types.PaymentGatewayTypeRazorpay)
		if err != nil {
			a.logger.Error(ctx, "failed to list in-flight Razorpay payments", "error", err, "payment_status", status)
			return nil, err
		}

		for _, p := range inFlight {
			if !lo.Contains(savedMethodTypes, p.PaymentMethodType) {
				continue
			}
			result.Total++
			scopedCtx := types.SetEnvironmentID(types.SetTenantID(ctx, p.TenantID), p.EnvironmentID)
			synced, err := a.paymentSvc.GetPayment(scopedCtx, p.PaymentID)
			if err != nil {
				a.logger.Error(scopedCtx, "failed to sync Razorpay payment status", "error", err, "payment_id", p.PaymentID)
				result.Errors++
				continue
			}
			if synced.PaymentStatus != status {
				result.Settled++
			}
		}
	}

	a.logger.Info(ctx, "synced in-flight Razorpay payments", "total", result.Total, "settled", result.Settled, "errors", result.Errors)
	return result, nil
}
