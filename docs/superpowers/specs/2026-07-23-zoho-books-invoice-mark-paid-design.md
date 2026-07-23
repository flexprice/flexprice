# Design: Sync "Invoice Paid" from FlexPrice to Zoho Books

**Date:** 2026-07-23
**Status:** Proposed
**Scope:** New outbound flow only. Does not change the existing outbound draft-invoice sync (`internal/integration/zoho/invoice.go:SyncInvoiceToZoho`) or the existing inbound "Zoho says paid" webhook flow (`internal/integration/zoho/webhook/handler.go:handleInvoicePaid`).

## Problem

When a FlexPrice invoice is finalized, it's synced to Zoho Books as a draft invoice (existing flow, `SyncInvoiceToZohoBooksIfEnabled` → `ZohoBooksInvoiceSyncWorkflow`). There is no corresponding flow for the other direction: when that invoice is later paid *in FlexPrice*, nothing tells Zoho about it. The Zoho invoice sits in Zoho forever as unpaid/draft, even though FlexPrice knows it's settled.

(The reverse direction — Zoho says paid → FlexPrice reconciles — already exists via `internal/integration/zoho/webhook/handler.go:handleInvoicePaid`. This design only adds the missing FlexPrice → Zoho leg.)

## Precedent: Whop's mark-paid flow

FlexPrice already has this exact pattern for Whop (`internal/temporal/workflows/whop_invoice_sync_workflow.go:WhopInvoiceMarkPaidWorkflow`, dispatched from `internal/ee/service/payment_processor.go:dispatchWhopMarkPaid`). This design mirrors it closely, substituting Zoho's payment-recording API for Whop's status-update API, and adding tax-delta handling that Whop's flow doesn't need.

## Tax discrepancy (confirmed with stakeholder)

Zoho's invoice total is **intentionally** tax-inclusive while FlexPrice's `invoice.Total` is not — every Zoho item FlexPrice creates is `is_taxable=true` with the org's default tax attached (`internal/integration/zoho/item_sync.go:96-104`), and invoice line items never override that (`internal/integration/zoho/invoice.go:254-255`, `TaxID`/`TaxExemptionID` are commented out). So a Zoho invoice's total is routinely higher than the FlexPrice invoice's total, by design.

**Decision:** when FlexPrice marks an invoice fully paid, mark-paid records a payment for Zoho's **entire outstanding balance** (tax delta included), not just the pre-tax amount FlexPrice collected. This was chosen for implementation simplicity over splitting out the tax delta:
- Reading Zoho's own `balance` field and zeroing it out requires no new bookkeeping.
- It's naturally idempotent from Zoho's own state alone (balance already 0 → skip; no separate "have I already recorded this" tracking needed in FlexPrice).
- The alternative (record only what FlexPrice actually collected) would leave Zoho invoices permanently "Partially Paid" whenever there's a tax delta, and would require extra state to avoid double-posting on retries, since the balance never reaches zero on its own.

## Architecture

```
Invoice fully paid in FlexPrice
  (internal/ee/service/payment_processor.go: handleInvoicePostProcessing,
   invoice.PaymentStatus == types.PaymentStatusSucceeded)
        │
        ▼
dispatchZohoMarkPaid(ctx, invoice.ID)   — fires unconditionally, fire-and-forget,
                                           same as dispatchWhopMarkPaid right above it
        │
        ▼
Temporal: ZohoBooksInvoiceMarkPaidWorkflow → MarkZohoBooksInvoicePaid activity
        │
        ▼
zoho.InvoiceService.MarkInvoicePaidInZoho(ctx, flexpriceInvoiceID)
        │
        ├─ look up entity mapping (FlexPrice invoice → Zoho invoice_id, provider=zoho_books)
        │    not found → log + return nil (invoice was never synced out; nothing to mark paid)
        │
        ├─ GET /books/v3/invoices/{zoho_invoice_id}  → read customer_id + balance
        │
        ├─ balance <= 0 → log + return nil (already paid in Zoho; idempotent no-op —
        │                  this also absorbs the case where the payment came in via the
        │                  existing inbound Zoho webhook, since Zoho's own balance is
        │                  already 0 by the time this runs)
        │
        └─ POST /books/v3/customerpayments
             { customer_id, payment_mode: "other", amount: balance, date: today,
               invoices: [{ invoice_id: zoho_invoice_id, amount_applied: balance }] }
```

No `sync_config` gate: fires unconditionally whenever a Zoho-connected invoice becomes fully paid, matching the existing Whop precedent exactly (the connection model has an unused `Payment.Outbound` toggle, but wiring a new gate here would diverge from how Whop already works, so it's left alone).

## Components (new/changed)

1. **`internal/temporal/models/zoho_books_invoice_mark_paid.go`** (new)
   `ZohoBooksInvoiceMarkPaidWorkflowInput{InvoiceID, TenantID, EnvironmentID}` + `Validate()` — identical shape to `models.WhopInvoiceMarkPaidWorkflowInput`.

2. **`internal/temporal/workflows/zoho_books_invoice_sync_workflow.go`**
   Add `WorkflowZohoBooksInvoiceMarkPaid = "ZohoBooksInvoiceMarkPaidWorkflow"` and `ActivityMarkZohoBooksInvoicePaid = "MarkZohoBooksInvoicePaid"` consts, plus `ZohoBooksInvoiceMarkPaidWorkflow(ctx, input)`: validates input, sets a 2-minute `StartToCloseTimeout` / 3-attempt retry policy, executes the mark-paid activity. **No 5-second sleep** — unlike `ZohoBooksInvoiceSyncWorkflow` (which sleeps to let a brand-new invoice commit), this fires after the invoice/payment update has already committed, matching `WhopInvoiceMarkPaidWorkflow`.

3. **`internal/temporal/activities/zoho/invoice_sync_activities.go`**
   Add `MarkZohoBooksInvoicePaid(ctx, input models.ZohoBooksInvoiceMarkPaidWorkflowInput) error` on `InvoiceSyncActivities`: sets tenant/env on ctx, resolves the Zoho integration (non-retryable error if connection missing, matching `SyncInvoiceToZoho`), calls `zohoIntegration.InvoiceSvc.MarkInvoicePaidInZoho(ctx, input.InvoiceID)`.

4. **`internal/integration/zoho/invoice.go`**
   Add `MarkInvoicePaidInZoho(ctx context.Context, flexpriceInvoiceID string) error` to the `ZohoInvoiceService` interface and `InvoiceService` implementation, per the Architecture flow above.

5. **`internal/integration/zoho/client.go`**
   Add to `ZohoClient` interface + `Client`:
   - `GetInvoice(ctx, zohoInvoiceID string) (*InvoiceResponse, error)` — `GET /books/v3/invoices/{invoice_id}`.
   - `CreateCustomerPayment(ctx, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error)` — `POST /books/v3/customerpayments`.

6. **`internal/integration/zoho/dto.go`**
   - Extend `InvoiceResponse` with `CustomerID string \`json:"customer_id,omitempty"\`` and `Balance decimal.Decimal \`json:"balance,omitempty"\``.
   - Add `CustomerPaymentCreateRequest{CustomerID, PaymentMode, Amount decimal.Decimal, Date string, Invoices []CustomerPaymentInvoiceApply}` and `CustomerPaymentInvoiceApply{InvoiceID string, AmountApplied decimal.Decimal}`.
   - Add `CustomerPaymentResponse{PaymentID string}`.
   - `payment_mode` is hardcoded to `"other"` — FlexPrice's payment methods (card/ACH/wallet/offline) don't map cleanly to Zoho's fixed enum, and the field is cosmetic bookkeeping in Zoho, not something downstream logic depends on.

7. **`internal/temporal/registration.go`**, **`internal/types/temporal.go`**, **`internal/temporal/service/service.go`**
   Register the new workflow type, activity, and input builder, mirroring the existing `TemporalWhopInvoiceMarkPaidWorkflow` entries line-for-line (workflow registration, the `TemporalWorkflowType` constant + inclusion in the provider-workflow switch, and a `buildZohoBooksInvoiceMarkPaidInput` builder alongside `buildWhopInvoiceMarkPaidInput`).

8. **`internal/ee/service/payment_processor.go`**
   Add `dispatchZohoMarkPaid(ctx context.Context, invoiceID string)`, structurally identical to `dispatchWhopMarkPaid` (fetches the global Temporal service, builds `ZohoBooksInvoiceMarkPaidWorkflowInput`, executes `TemporalZohoBooksInvoiceMarkPaidWorkflow`, logs on error). Call it from `handleInvoicePostProcessing` right next to the existing `p.dispatchWhopMarkPaid(ctx, invoice.ID)` call.

## Error handling

- Zoho connection not configured → non-retryable `temporal.ApplicationError` (matches `SyncInvoiceToZoho`'s existing handling).
- No entity mapping for the invoice → log + no-op, not an error (invoice was never pushed to Zoho).
- Zoho balance already 0 → log + no-op (idempotent skip; also the mechanism that prevents a loop with the inbound webhook — no payment-source metadata check needed).
- Zoho API failure (GET or POST) → error propagates, retried up to 3× by the Temporal activity retry policy, same as the sync workflow.
- `dispatchZohoMarkPaid` itself never returns an error to its caller — like `dispatchWhopMarkPaid`, a failure to *start* the workflow is logged and swallowed so it can never block payment processing.

## Testing

- Table-driven unit tests for `MarkInvoicePaidInZoho`: no mapping → skip; Zoho balance zero → skip; positive balance → `CreateCustomerPayment` called with `amount = balance` and the invoice/customer IDs from the GET response; Zoho API error (from either the GET or the POST) → propagates.
- Unit test for `ZohoBooksInvoiceMarkPaidWorkflowInput.Validate()`.
- The `zoho` integration package currently has no mocks/tests at all (only `internal/integration/zoho/webhook/verify_test.go` exists anywhere under this package). This design adds minimal hand-written test doubles for `ZohoClient`, `invoice.Repository`, and `entityintegrationmapping.Repository`, scoped to this feature — not a general mock framework for the package.
