package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

// notifyInvoiceFinalized emits the finalized webhook and kicks the revenue-facts
// FINAL flip. Finalization paths call this, never the two pieces separately.
func notifyInvoiceFinalized(ctx context.Context, params ServiceParams, invoiceID string) {
	publishInvoiceWebhook(ctx, params, types.WebhookEventInvoiceUpdateFinalized, invoiceID)
	asyncRevenueFactsUpdate(ctx, params, "final flip", invoiceID,
		func(ctx context.Context, rs interfaces.RevenueService) error {
			return rs.FinalizeSubscriptionPeriod(ctx, invoiceID)
		})
}

// notifyInvoiceVoided is the void-side twin of notifyInvoiceFinalized.
func notifyInvoiceVoided(ctx context.Context, params ServiceParams, invoiceID string) {
	publishInvoiceWebhook(ctx, params, types.WebhookEventInvoiceUpdateVoided, invoiceID)
	asyncRevenueFactsUpdate(ctx, params, "revert", invoiceID,
		func(ctx context.Context, rs interfaces.RevenueService) error {
			return rs.RevertInvoiceFacts(ctx, invoiceID)
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
