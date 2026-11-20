package events

import (
	"context"
	"encoding/json"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// DispatchCreditNoteVendorSync starts the Zoho Books credit note sync for a finalized adjustment
// credit note, and for any voided credit note. Used on credit_note.updated.
func DispatchCreditNoteVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	creditNoteRepo creditnote.Repository,
	entityScope interfaces.BillingEntityScope,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	var payload webhookDto.InternalCreditNoteEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.CreditNoteID == "" {
		log.Error(ctx, "integration_events: invalid credit note payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	// The credit note is read first because its invoice decides the billing entity scope.
	cn, err := creditNoteRepo.Get(ctx, payload.CreditNoteID)
	if err != nil {
		return err
	}
	finalizedAdjustment := cn.CreditNoteType == types.CreditNoteTypeAdjustment && cn.CreditNoteStatus == types.CreditNoteStatusFinalized
	if !finalizedAdjustment && cn.CreditNoteStatus != types.CreditNoteStatusVoided {
		return nil
	}

	ctx, syncEnabled, err := zohoBooksCreditNoteSyncContext(ctx, connRepo, entityScope, cn.InvoiceID)
	if err != nil || !syncEnabled {
		return err
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}
	return startZohoBooksCreditNoteSync(ctx, temporalSvc, log, cn, event)
}

// DispatchRefundVendorSync starts the Zoho Books credit note sync once every refund of a
// refund credit note has finished. Used on refund.succeeded.
func DispatchRefundVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	creditNoteRepo creditnote.Repository,
	refundRepo refund.Repository,
	entityScope interfaces.BillingEntityScope,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	var payload webhookDto.InternalRefundEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.RefundID == "" {
		log.Error(ctx, "integration_events: invalid refund payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	settled, err := refundRepo.Get(ctx, payload.RefundID)
	if err != nil {
		return err
	}
	if settled.CreditNoteID == nil {
		return nil
	}

	ctx, syncEnabled, err := zohoBooksCreditNoteSyncContext(ctx, connRepo, entityScope, settled.InvoiceID)
	if err != nil || !syncEnabled {
		return err
	}

	cn, err := creditNoteRepo.Get(ctx, *settled.CreditNoteID)
	if err != nil {
		return err
	}
	if cn.CreditNoteType != types.CreditNoteTypeRefund {
		return nil
	}

	filter := types.NewNoLimitRefundFilter()
	filter.CreditNoteIDs = []string{cn.ID}
	refunds, err := refundRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if lo.ContainsBy(refunds, func(r *refund.Refund) bool { return !r.RefundStatus.IsTerminal() }) {
		log.Info(ctx, "integration_events: credit note has refunds still in progress, deferring Zoho Books sync",
			"credit_note_id", cn.ID,
			"refund_id", settled.ID,
		)
		return nil
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}
	return startZohoBooksCreditNoteSync(ctx, temporalSvc, log, cn, event)
}

// zohoBooksCreditNoteSyncContext scopes ctx to the invoice's billing entity and reports
// whether that entity's Zoho Books connection syncs credit notes.
func zohoBooksCreditNoteSyncContext(
	ctx context.Context,
	connRepo connection.Repository,
	entityScope interfaces.BillingEntityScope,
	invoiceID string,
) (context.Context, bool, error) {
	if entityScope != nil {
		scopedCtx, err := entityScope.ScopeToInvoice(ctx, invoiceID)
		if err != nil {
			return ctx, false, err
		}
		ctx = scopedCtx
	}

	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderZohoBooks)
	if err != nil {
		return ctx, false, err
	}
	syncEnabled := conn != nil && conn.IsInvoiceOutboundEnabled() && conn.IsCreditNoteOutboundEnabled()
	return ctx, syncEnabled, nil
}

func startZohoBooksCreditNoteSync(
	ctx context.Context,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	cn *creditnote.CreditNote,
	event *types.WebhookEvent,
) error {
	input := &temporalmodels.ZohoBooksCreditNoteSyncWorkflowInput{
		CreditNoteID:  cn.ID,
		TenantID:      event.TenantID,
		EnvironmentID: event.EnvironmentID,
	}
	// A sync already running for this credit note may have listed its refunds before this
	// one settled, so the error is returned and the event retried instead of skipped.
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalZohoBooksCreditNoteSyncWorkflow, input, types.SecretProviderZohoBooks, cn.InvoiceID)
}
