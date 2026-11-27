package zoho

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/zoho"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/activities/billingentity"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type CreditNoteSyncActivities struct {
	integrationFactory *integration.Factory
	entityScope        interfaces.BillingEntityScope
	creditNoteRepo     creditnote.Repository
	refundRepo         refund.Repository
	logger             *logger.Logger
}

func NewCreditNoteSyncActivities(
	integrationFactory *integration.Factory,
	entityScope interfaces.BillingEntityScope,
	creditNoteRepo creditnote.Repository,
	refundRepo refund.Repository,
	logger *logger.Logger,
) *CreditNoteSyncActivities {
	return &CreditNoteSyncActivities{
		integrationFactory: integrationFactory,
		entityScope:        entityScope,
		creditNoteRepo:     creditNoteRepo,
		refundRepo:         refundRepo,
		logger:             logger,
	}
}

func (a *CreditNoteSyncActivities) SyncCreditNoteToZoho(ctx context.Context, input models.ZohoBooksCreditNoteSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	cn, err := a.creditNoteRepo.Get(ctx, input.CreditNoteID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("credit note not found", "CreditNoteNotFound", err)
		}
		return err
	}

	ctx, err = billingentity.ScopeActivity(ctx, a.entityScope, "", cn.InvoiceID, "")
	if err != nil {
		return err
	}

	zohoIntegration, err := a.integrationFactory.GetZohoBooksIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("Zoho Books connection not configured", ierr.ErrConnectionNotFound, err)
		}
		a.logger.Error(ctx, "SyncCreditNoteToZoho activity failed to get Zoho Books integration",
			"error", err,
			"credit_note_id", input.CreditNoteID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID)
		return err
	}

	refundFilter := types.NewNoLimitRefundFilter()
	refundFilter.CreditNoteIDs = []string{cn.ID}
	refunds, err := a.refundRepo.List(ctx, refundFilter)
	if err != nil {
		return err
	}

	err = zohoIntegration.CreditNoteSvc.SyncCreditNoteToZoho(ctx, zoho.ZohoCreditNoteSyncRequest{
		CreditNote: cn,
		Refunds:    refunds,
	})
	if err != nil {
		a.logger.Error(ctx, "Zoho Books credit note sync activity failed",
			"error", err,
			"credit_note_id", input.CreditNoteID,
			"invoice_id", cn.InvoiceID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID)
		if zoho.IsZohoRequestRejected(err) || ierr.IsValidation(err) {
			return temporal.NewNonRetryableApplicationError("Zoho Books rejected the credit note sync", "ZohoRequestRejected", err)
		}
	}
	return err
}
