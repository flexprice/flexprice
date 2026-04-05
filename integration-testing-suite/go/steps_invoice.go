package main

import (
	"context"
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runInvoiceSteps executes Phase 5: Cancellation & Invoice.
func (r *SanityRunner) runInvoiceSteps(ctx context.Context) {
	r.setPhase("PHASE 5: Cancellation & Invoice Generation")
	r.printPhaseHeader(r.phase)

	if !r.require(r.subscriptionID, "Create Subscription", "Cancel Subscription") {
		r.skip("Find Generated Invoice", "depends on cancellation")
		r.skip("Fetch Invoice", "depends on cancellation")
		r.skip("Mark Invoice Paid", "depends on cancellation")
		return
	}

	// ── Cancel Subscription ──────────────────────────────────────────────
	r.run("Cancel Subscription (immediate)", "Subscriptions.CancelSubscription", false, func() error {
		invoicePolicy := types.CancelImmediatelyInvoicePolicyGenerateInvoice
		req := types.CancelSubscriptionRequest{
			CancellationType:               types.CancellationTypeImmediate,
			CancelImmediatelyInovicePolicy: &invoicePolicy,
			Reason:                         strPtr("Integration test cancellation"),
		}

		resp, err := r.client.Subscriptions.CancelSubscription(ctx, r.subscriptionID, req)
		if err != nil {
			return err
		}
		cancelResp := resp.CancelSubscriptionResponse
		if cancelResp == nil {
			return fmt.Errorf("cancel subscription returned no body")
		}

		if cancelResp.ProrationInvoice != nil && cancelResp.ProrationInvoice.ID != nil {
			r.invoiceID = *cancelResp.ProrationInvoice.ID
		}

		details := "cancelled"
		if cancelResp.SubscriptionID != nil {
			details += fmt.Sprintf(", sub=%s", *cancelResp.SubscriptionID)
		}
		if cancelResp.Status != nil {
			details += fmt.Sprintf(", status=%s", string(*cancelResp.Status))
		}
		r.lastResult().Details = details
		r.subscriptionCancelled = true
		return nil
	})

	// ── Find Invoice ─────────────────────────────────────────────────────
	// If cancel didn't return the invoice inline, query for it.
	if r.invoiceID == "" {
		r.run("Find Generated Invoice", "Invoices.QueryInvoice", false, func() error {
			invoiceType := types.InvoiceTypeSubscription
			resp, err := r.client.Invoices.QueryInvoice(ctx, types.InvoiceFilter{
				SubscriptionID: strPtr(r.subscriptionID),
				InvoiceType:    &invoiceType,
				Sort:           []types.SortCondition{{Field: strPtr("created_at"), Direction: types.SortDirectionDesc.ToPointer()}},
				Limit:          int64Ptr(5),
			})
			if err != nil {
				return err
			}
			list := resp.ListInvoicesResponse
			if list == nil || len(list.Items) == 0 {
				return fmt.Errorf("no invoices found for subscription %s", r.subscriptionID)
			}
			inv := list.Items[0]
			if inv.ID == nil {
				return fmt.Errorf("invoice has no ID")
			}
			r.invoiceID = *inv.ID
			r.lastResult().Details = fmt.Sprintf("invoice_id=%s, %d found", r.invoiceID, len(list.Items))
			return nil
		})
	}

	if !r.require(r.invoiceID, "Generated Invoice", "Fetch Invoice") {
		r.skip("Mark Invoice Paid", "depends on invoice")
		return
	}

	// ── Fetch Invoice ────────────────────────────────────────────────────
	// Sanity check: SDK can fetch the invoice and it has line items + a total.
	r.run("Fetch Invoice", "Invoices.GetInvoice", false, func() error {
		resp, err := r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
		if err != nil {
			return err
		}
		inv := resp.Invoice
		if inv == nil {
			return fmt.Errorf("get invoice returned no body")
		}

		details := fmt.Sprintf("invoice_id=%s, %d line items", r.invoiceID, len(inv.LineItems))
		for i, li := range inv.LineItems {
			name := derefStr(li.DisplayName)
			amount := derefStr(li.Amount)
			qty := derefStr(li.Quantity)
			line := fmt.Sprintf("  line[%d]: %s", i, name)
			if qty != "" {
				line += fmt.Sprintf(", qty=%s", qty)
			}
			if amount != "" {
				line += fmt.Sprintf(", amount=$%s", amount)
			}
			details += "\n        " + line
		}

		status := ""
		if inv.InvoiceStatus != nil {
			status = string(*inv.InvoiceStatus)
		}
		details += fmt.Sprintf("\n        subtotal=$%s, total=$%s, status=%s",
			derefStr(inv.Subtotal), derefStr(inv.Total), status)

		r.lastResult().Details = details
		return nil
	})

	// ── Mark Invoice Paid ────────────────────────────────────────────────
	r.run("Mark Invoice Paid", "Invoices.UpdateInvoicePaymentStatus", false, func() error {
		// Re-fetch to get current status and amount.
		getResp, err := r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
		if err != nil {
			return fmt.Errorf("get invoice pre-payment: %w", err)
		}
		inv := getResp.Invoice

		// Finalize if still draft.
		if inv != nil && inv.InvoiceStatus != nil {
			if s := string(*inv.InvoiceStatus); s == "draft" || s == "DRAFT" {
				if _, err := r.client.Invoices.FinalizeInvoice(ctx, r.invoiceID); err != nil {
					return fmt.Errorf("finalize invoice: %w", err)
				}
				// Re-fetch after finalize.
				getResp, err = r.client.Invoices.GetInvoice(ctx, r.invoiceID, nil, nil)
				if err != nil {
					return fmt.Errorf("re-fetch after finalize: %w", err)
				}
				inv = getResp.Invoice
			}
		}

		amount := "0.00"
		if inv != nil {
			if inv.AmountDue != nil && *inv.AmountDue != "" {
				amount = *inv.AmountDue
			} else if inv.Total != nil && *inv.Total != "" {
				amount = *inv.Total
			}
		}

		_, err = r.client.Invoices.UpdateInvoicePaymentStatus(ctx, r.invoiceID, types.UpdatePaymentStatusRequest{
			PaymentStatus: types.PaymentStatusSucceeded,
			Amount:        strPtr(amount),
		})
		if err != nil {
			return err
		}

		r.lastResult().Details = fmt.Sprintf("invoice=%s, amount=$%s, payment_status=SUCCEEDED", r.invoiceID, amount)
		return nil
	})
}
