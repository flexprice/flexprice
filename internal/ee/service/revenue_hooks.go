package service

import (
	"context"
	"fmt"
	"time"
)

// asyncRevenueFactsUpdate runs one revenue_facts update (final flip, revert)
// detached from the caller's request: revenue_facts is a shadow write-path,
// so an invoice operation must never wait on it or fail because of it.
//
// Delivery is deliberately best-effort. Every operation is idempotent, and a
// run lost to a crash surfaces as a reconciliation gap the periodic rollup /
// drift sweep repairs — promote this to a Temporal workflow if facts ever
// need guaranteed delivery ahead of that sweep.
func asyncRevenueFactsUpdate(ctx context.Context, params ServiceParams, op, invoiceID string, run func(context.Context, RevenueService) error) {
	if params.RevenueFactRepo == nil {
		// Not wired in this deployment/test context.
		return
	}

	// WithoutCancel keeps tenant/environment values while surviving the
	// request; the timeout stops a stuck write from leaking the goroutine.
	asyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
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

		if err := run(asyncCtx, NewRevenueService(params)); err != nil {
			params.Logger.Error(asyncCtx, "revenue facts "+op+" failed",
				"error", err,
				"invoice_id", invoiceID)
		}
	}()
}
