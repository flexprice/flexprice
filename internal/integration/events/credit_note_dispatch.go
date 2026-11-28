package events

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// DispatchCreditNoteVendorSync starts the credit note sync on each provider that syncs credit
// notes, for a finalized adjustment credit note and for any voided credit note. Used on credit_note.updated.
func DispatchCreditNoteVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	mappingRepo entityintegrationmapping.Repository,
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

	ctx, targets, err := creditNoteSyncTargets(ctx, connRepo, entityScope, cn.InvoiceID)
	if err != nil || len(targets) == 0 {
		return err
	}
	return dispatchCreditNoteSync(ctx, targets, mappingRepo, log, cn, event, false)
}

// DispatchRefundVendorSync starts the credit note sync on each provider that syncs credit notes,
// once every refund of a refund credit note has finished. Used on refund.succeeded.
func DispatchRefundVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	mappingRepo entityintegrationmapping.Repository,
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

	ctx, targets, err := creditNoteSyncTargets(ctx, connRepo, entityScope, settled.InvoiceID)
	if err != nil || len(targets) == 0 {
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
		log.Info(ctx, "integration_events: credit note has refunds still in progress, deferring credit note sync",
			"credit_note_id", cn.ID,
			"refund_id", settled.ID,
		)
		return nil
	}
	return dispatchCreditNoteSync(ctx, targets, mappingRepo, log, cn, event, false)
}

// PushCreditNoteVendorSync starts the credit note sync for a manual push, on the provider named by
// OnlyProvider or on every provider that syncs credit notes. A credit note that cannot be pushed is
// reported as a validation error instead of being skipped.
func PushCreditNoteVendorSync(
	ctx context.Context,
	connRepo connection.Repository,
	mappingRepo entityintegrationmapping.Repository,
	refundRepo refund.Repository,
	entityScope interfaces.BillingEntityScope,
	log *logger.Logger,
	cn *creditnote.CreditNote,
	event *types.WebhookEvent,
	opts ...DispatchOption,
) error {
	if cn.CreditNoteStatus != types.CreditNoteStatusFinalized && cn.CreditNoteStatus != types.CreditNoteStatusVoided {
		return ierr.NewError("credit note cannot be pushed").
			WithHint("Only finalized or voided credit notes can be pushed").
			Mark(ierr.ErrValidation)
	}

	o := resolveDispatchOptions(opts)
	if o.onlyProvider != "" && !lo.ContainsBy(creditNoteSyncTriggers(), func(t creditNoteSyncTrigger) bool { return t.provider == o.onlyProvider }) {
		return ierr.NewError("provider does not sync credit notes").
			WithHintf("Credit notes cannot be pushed to %s", o.onlyProvider).
			Mark(ierr.ErrValidation)
	}

	ctx, targets, err := creditNoteSyncTargets(ctx, connRepo, entityScope, cn.InvoiceID)
	if err != nil {
		return err
	}
	if o.onlyProvider != "" {
		targets = lo.Filter(targets, func(t creditNoteSyncTarget, _ int) bool { return t.trigger.provider == o.onlyProvider })
	}
	if len(targets) == 0 {
		return ierr.NewError("credit note sync is not enabled").
			WithHint("Turn on invoice and credit note sync for the connection to push credit notes").
			Mark(ierr.ErrValidation)
	}

	if cn.CreditNoteType == types.CreditNoteTypeRefund && cn.CreditNoteStatus == types.CreditNoteStatusFinalized {
		filter := types.NewNoLimitRefundFilter()
		filter.CreditNoteIDs = []string{cn.ID}
		refunds, err := refundRepo.List(ctx, filter)
		if err != nil {
			return err
		}
		if lo.ContainsBy(refunds, func(r *refund.Refund) bool { return !r.RefundStatus.IsTerminal() }) {
			return ierr.NewError("credit note refunds are still in progress").
				WithHint("Push the credit note once all its refunds have finished").
				Mark(ierr.ErrValidation)
		}
	}

	return dispatchCreditNoteSync(ctx, targets, mappingRepo, log, cn, event, true)
}

// creditNoteSyncTrigger starts one provider's credit note sync on its connection. A manual
// trigger reports a credit note it skips as a validation error.
type creditNoteSyncTrigger struct {
	provider types.SecretProvider
	start    func(ctx context.Context, conn *connection.Connection, mappingRepo entityintegrationmapping.Repository, log *logger.Logger, cn *creditnote.CreditNote, event *types.WebhookEvent, manual bool) error
}

// creditNoteSyncTriggers lists the providers that sync credit notes.
func creditNoteSyncTriggers() []creditNoteSyncTrigger {
	return []creditNoteSyncTrigger{
		{types.SecretProviderZohoBooks, triggerZohoBooksCreditNoteSync},
	}
}

type creditNoteSyncTarget struct {
	trigger creditNoteSyncTrigger
	conn    *connection.Connection
}

// creditNoteSyncTargets scopes ctx to the invoice's billing entity and returns each provider whose
// connection in that scope syncs invoices and credit notes outbound.
func creditNoteSyncTargets(
	ctx context.Context,
	connRepo connection.Repository,
	entityScope interfaces.BillingEntityScope,
	invoiceID string,
) (context.Context, []creditNoteSyncTarget, error) {
	if entityScope != nil {
		scopedCtx, err := entityScope.ScopeToInvoice(ctx, invoiceID)
		if err != nil {
			return ctx, nil, err
		}
		ctx = scopedCtx
	}

	var targets []creditNoteSyncTarget
	for _, trigger := range creditNoteSyncTriggers() {
		conn, err := getConnectionIfExists(ctx, connRepo, trigger.provider)
		if err != nil {
			return ctx, nil, err
		}
		if conn != nil && conn.IsInvoiceOutboundEnabled() && conn.IsCreditNoteOutboundEnabled() {
			targets = append(targets, creditNoteSyncTarget{trigger: trigger, conn: conn})
		}
	}
	return ctx, targets, nil
}

// dispatchCreditNoteSync starts the credit note sync on each target. A sync already running is
// returned, not skipped, so the event is retried after it.
func dispatchCreditNoteSync(
	ctx context.Context,
	targets []creditNoteSyncTarget,
	mappingRepo entityintegrationmapping.Repository,
	log *logger.Logger,
	cn *creditnote.CreditNote,
	event *types.WebhookEvent,
	manual bool,
) error {
	var errs []error
	for _, target := range targets {
		if err := target.trigger.start(ctx, target.conn, mappingRepo, log, cn, event, manual); err != nil {
			errs = append(errs, err)
		}
	}
	// A single error is returned as is so its hint reaches the caller.
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

// triggerZohoBooksCreditNoteSync starts the Zoho Books credit note sync, skipping a finalized
// credit note already mapped on the connection.
func triggerZohoBooksCreditNoteSync(
	ctx context.Context,
	conn *connection.Connection,
	mappingRepo entityintegrationmapping.Repository,
	log *logger.Logger,
	cn *creditnote.CreditNote,
	event *types.WebhookEvent,
	manual bool,
) error {
	if cn.CreditNoteStatus == types.CreditNoteStatusFinalized {
		filter := types.NewEntityIntegrationMappingFilter()
		filter.EntityType = types.IntegrationEntityTypeCreditNote
		filter.EntityID = cn.ID
		filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
		filter.ConnectionID = conn.ID
		filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
		mappings, err := mappingRepo.List(ctx, filter)
		if err != nil {
			return err
		}
		if len(mappings) > 0 {
			if manual {
				return ierr.NewError("credit note is already synced").
					WithHint("This credit note is already in Zoho Books").
					Mark(ierr.ErrValidation)
			}
			log.Info(ctx, "integration_events: credit note already synced to Zoho Books, skipping",
				"credit_note_id", cn.ID,
				"connection_id", conn.ID)
			return nil
		}
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}
	return startZohoBooksCreditNoteSync(ctx, temporalSvc, log, cn, event)
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
