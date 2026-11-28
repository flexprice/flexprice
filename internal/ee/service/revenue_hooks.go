package service

import (
	"context"
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// notifyInvoiceFinalized emits the finalized webhook and kicks the revenue-facts
// FINAL flip. Finalization paths call this, never the two pieces separately.
func notifyInvoiceFinalized(ctx context.Context, params ServiceParams, invoiceID string) {
	publishInvoiceWebhook(ctx, params, types.WebhookEventInvoiceUpdateFinalized, invoiceID)
	asyncRevenueFactsUpdate(ctx, params, "final flip", invoiceID,
		func(ctx context.Context, rs interfaces.RevenueService) error {
			return rs.FinalizeSubscriptionPeriod(ctx, invoiceID)
		})
	postInvoiceJournal(ctx, params, invoiceID)
}

// notifyInvoiceVoided is the void-side twin of notifyInvoiceFinalized.
func notifyInvoiceVoided(ctx context.Context, params ServiceParams, invoiceID string) {
	publishInvoiceWebhook(ctx, params, types.WebhookEventInvoiceUpdateVoided, invoiceID)
	asyncRevenueFactsUpdate(ctx, params, "revert", invoiceID,
		func(ctx context.Context, rs interfaces.RevenueService) error {
			return rs.RevertInvoiceFacts(ctx, invoiceID)
		})
	postInvoiceJournal(ctx, params, invoiceID)
}

// postInvoiceJournal posts the invoice's journal entries after commit.
func postInvoiceJournal(ctx context.Context, params ServiceParams, invoiceID string) {
	asyncRevRecUpdate(ctx, params, "invoice journal", invoiceID, func(ctx context.Context) error {
		inv, err := params.InvoiceRepo.Get(ctx, invoiceID)
		if err != nil {
			return err
		}
		// The subscription sync runs concurrently and may not have created the lines' POBs yet.
		if subscriptionID := lo.FromPtr(inv.SubscriptionID); subscriptionID != "" {
			if err := syncSubscriptionRevRec(ctx, params, subscriptionID); err != nil {
				return err
			}
		}
		return params.JournalService.PostInvoice(ctx, inv)
	})
}

// postPaymentJournal posts the payment's journal entries after commit.
func postPaymentJournal(ctx context.Context, params ServiceParams, paymentID string) {
	asyncRevRecUpdate(ctx, params, "payment journal", paymentID, func(ctx context.Context) error {
		p, err := params.PaymentRepo.Get(ctx, paymentID)
		if err != nil {
			return err
		}
		return params.JournalService.PostPayment(ctx, p)
	})
}

// postRefundJournal posts the refund's journal entry after commit.
func postRefundJournal(ctx context.Context, params ServiceParams, refundID string) {
	asyncRevRecUpdate(ctx, params, "refund journal", refundID, func(ctx context.Context) error {
		r, err := params.RefundRepo.Get(ctx, refundID)
		if err != nil {
			return err
		}
		return params.JournalService.PostRefund(ctx, r)
	})
}

// postCreditNoteJournal posts the credit note's journal entries after commit.
func postCreditNoteJournal(ctx context.Context, params ServiceParams, creditNoteID string) {
	asyncRevRecUpdate(ctx, params, "credit note journal", creditNoteID, func(ctx context.Context) error {
		cn, err := params.CreditNoteRepo.Get(ctx, creditNoteID)
		if err != nil {
			return err
		}
		return params.JournalService.PostCreditNote(ctx, cn)
	})
}

// asyncRevenueFactsUpdate runs one revenue_facts update detached from the
// caller: an invoice operation must never wait on the shadow path or fail with
// it. Delivery is best-effort — every operation is idempotent, and a lost run
// shows up as a gap the rollup and drift sweep repair.
func asyncRevenueFactsUpdate(ctx context.Context, params ServiceParams, op, invoiceID string, run func(context.Context, interfaces.RevenueService) error) {
	if params.RevenueFacts == nil {
		// Not wired in this deployment/test context.
		return
	}

	// Keep the tenant scope, drop the caller's transaction: sharing its
	// connection past the commit corrupts that connection for everyone.
	detached := types.WithoutDBTransaction(context.WithoutCancel(ctx))

	start := func() {
		asyncCtx, cancel := context.WithTimeout(detached, time.Minute)
		go func() {
			defer cancel()
			// A panic in the shadow path must never crash the process.
			defer func() {
				if r := recover(); r != nil {
					params.Logger.Error(asyncCtx, "panic in revenue facts "+op,
						"error", fmt.Sprintf("%v", r),
						"invoice_id", invoiceID)
				}
			}()

			if err := run(asyncCtx, params.RevenueFacts); err != nil {
				params.Logger.Error(asyncCtx, "revenue facts "+op+" failed",
					"error", err,
					"invoice_id", invoiceID)
			}
		}()
	}

	// The update reads the invoice this transaction is still writing, so wait
	// for the commit to publish it rather than racing ahead of it.
	if !types.RegisterPostCommit(ctx, start) {
		start()
	}
}

// NotifySubscriptionChanged syncs the subscription's revenue recognition records after the caller commits.
// Every subscription lifecycle site calls this one helper.
func NotifySubscriptionChanged(ctx context.Context, params ServiceParams, subscriptionID string) {
	asyncRevRecUpdate(ctx, params, "subscription sync", subscriptionID, func(ctx context.Context) error {
		return syncSubscriptionRevRec(ctx, params, subscriptionID)
	})
}

// syncSubscriptionRevRec creates or updates the subscription's contract and POBs; safe to run concurrently.
func syncSubscriptionRevRec(ctx context.Context, params ServiceParams, subscriptionID string) error {
	sub, _, err := params.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return err
	}

	c, err := params.ContractService.SyncForSubscription(ctx, sub)
	if err != nil {
		return err
	}

	_, err = params.POBService.SyncForSubscription(ctx, sub, c)
	return err
}

// notifySubscriptionArchived archives the revenue recognition records of an abandoned draft subscription.
func notifySubscriptionArchived(ctx context.Context, params ServiceParams, subscriptionID string) {
	asyncRevRecUpdate(ctx, params, "subscription archive", subscriptionID, func(ctx context.Context) error {
		return params.ContractService.ArchiveForSubscription(ctx, subscriptionID)
	})
}

// asyncRevRecUpdate runs one revenue recognition write detached from the caller, after
// it commits, and only if revenue recognition is enabled for the tenant and environment.
func asyncRevRecUpdate(ctx context.Context, params ServiceParams, op, entityID string, run func(context.Context) error) {
	detached := types.WithoutDBTransaction(context.WithoutCancel(ctx))

	start := func() {
		asyncCtx, cancel := context.WithTimeout(detached, time.Minute)
		go func() {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					params.Logger.Error(asyncCtx, "panic in revenue recognition "+op,
						"error", fmt.Sprintf("%v", r),
						"entity_id", entityID)
				}
			}()

			enabled, err := revRecEnabled(asyncCtx, params)
			if err == nil && enabled {
				err = run(asyncCtx)
			}
			if err != nil {
				params.Logger.Error(asyncCtx, "revenue recognition "+op+" failed",
					"error", err,
					"entity_id", entityID)
			}
		}()
	}

	if !types.RegisterPostCommit(ctx, start) {
		start()
	}
}

func revRecEnabled(ctx context.Context, params ServiceParams) (bool, error) {
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return false, nil
	}

	setting, err := params.SettingsRepo.GetByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	if err != nil {
		if ierr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](setting.Value)
	if err != nil {
		return false, err
	}
	return cfg.Enabled, nil
}
