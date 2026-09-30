# L3-A — Conversion at finalize + customer billing currency (go-live) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL at execution: superpowers:executing-plans (or subagent-driven-development), TDD each step. This is a **scoping plan** — before implementing, run a fresh exploration pass of the finalize/customer/subscription paths and fill the per-step failing tests + real code. The task boundaries, files, and interfaces below are grounded in verified flows and are the contract.

**Goal:** Turn conversion on. A customer with a `billing_currency` that differs from a subscription/one-off charge currency gets an invoice converted once at finalize, at a frozen rate, with tax in the billing currency. Customers with no billing currency (or one equal to the charge currency) are byte-for-byte unaffected.

**Architecture:** Add `customers.billing_currency`, `invoices.fx_conversion`, and `invoice_line_items.original_currency/original_amount`. Insert a **shared convert-then-retax step** into finalize that runs for *any* invoice type with a billing currency (not just subscriptions), calling the FX-base `FXRateService.ResolveRate`. Gate everything on `billing_currency` set **and** different — no billing currency ⇒ zero FX code runs.

**Tech stack:** Go, Ent, Postgres, `shopspring/decimal`. Builds on the FX-base branch (`fxrate` domain/service already present).

**Spec:** `docs/design/2026-09-26-FLE-1383-adaptive-multi-currency-erd.md` — §1.5, §3.3, §3.5, §3.6, §5.1–§5.4, §8.2, §8.3, §8.4, §9.2, §9.3 (fx_conversion/originals only).

**Stacked on:** `feat/FLE-1383-fx-rates` (FX base). PR base = that branch (fork-internal) until it merges, then rebase onto `develop`.

## Global Constraints

- **Existing customers unaffected (the invariant):** if `customer.billing_currency` is empty or equals the charge currency, finalize must issue **no `fx_rates` query** and produce an identical invoice. This is the headline regression test (§1.5, Task 8).
- Conversion runs **inside the existing finalize transaction**, under the invoice row lock (verified: `performFinalizeInvoiceActions`, `invoice.go:1066` tx, `:1068` `GetForUpdate`).
- Order: prepaid credits + discounts (already before tax) → **convert** → **recompute tax** on converted amounts. Tax base stays `subtotal − discount` before prepaid credits (verified `taxableAmount`, `tax.go:1118`).
- Frozen rate: resolved once via `FXRateService.ResolveRate`, written to `invoices.fx_conversion` in the same tx; step is skipped if `fx_conversion` is already set (checkout drafts, retries).
- No rate ⇒ invoice stays DRAFT, nothing written, marked invalid-operation (Temporal does not retry); the scheduled finalizer retries once a rate exists.
- Multi-tenancy, loglint (LL006 `"error"` key), builders for domain mutations, table-driven tests — as in the FX base.

## Review Focus

- **No-billing-currency path is a true no-op** — no `fx_rates` query, identical invoice (Task 8 test, with a query spy).
- **One-off / wallet-top-up invoices** compute tax in `ComputeInvoice`, and finalize *skips* the subscription-only block (gated `InvoiceType == Subscription`, `invoice.go:1111`). The convert+retax step must live **outside** that gate and **recompute tax** for one-offs (Task 5).
- **Rounding with negative lines** — residual to the largest **positive** line; never a proration credit/discount line (Task 4).
- **`amount_paid` = 0 at finalize** for every path except pay-first checkout (out of scope here — L3-B). Convert only when `amount_paid == 0` on the non-checkout path.
- **Grouped invoicing is already single-currency** (attach-time check `subscription_grouped_invoicing.go:117`); no per-child split needed (§8.4, #4) — just confirm the group converts as a whole.
- **One-off created pre-paid** for a cross-currency customer is **rejected** (today the payment fields are silently dropped) (Task 6, #5).

## File Structure

- `ent/schema/customer.go`, `ent/schema/invoice.go`, `ent/schema/invoice_line_item.go` — new columns; `make generate-ent`; versioned migration.
- `internal/types/fx_conversion.go` (new) — `FxConversion` struct (charge/billing currency, rate, rate_id, scope, converted_at, `source{subtotal,total_discount,total_prepaid_credits_applied,net}`, rounding_adjustment, rounding_line_item_id).
- `internal/domain/customer/` + `internal/domain/invoice/` — struct fields + `FromEnt`; invoice repo plumbing (`internal/repository/ent/invoice.go` Create/CreateWithLineItems/AddLineItems/Update; `invoice_line_item.go`; `internal/testutil/inmemory_invoice_store.go` `copyInvoice` + `UpdateLineItem`).
- `internal/api/dto/customer.go` — `billing_currency` on create/update/response.
- `internal/ee/service/customer.go` — set/update guardrails (§8.2).
- `internal/ee/service/invoice_conversion.go` (new) — `ConvertInvoice(inv, resolution)` (net-once + rounding) + retax helper; called from `performFinalizeInvoiceActions` (`invoice.go:~1150`, outside the subscription gate) and the one-off finalize path.
- `internal/ee/service/subscription.go` — create-time rate check + inline `fx_rate` (§8.3).

## Tasks (task-level; fill TDD steps at execution)

### Task 1 — Schema + repo plumbing (inert)
Add the three columns (mirror `custom_currency`), `make generate-ent`, versioned migration. Thread `fx_conversion` + line originals through every invoice repo write site and the in-memory `copyInvoice`/`UpdateLineItem` (the "custom_currency was lost twice" sites). Round-trip test. `billing_currency` through customer model + repo + `FromEnt`. **No behavior change.**

### Task 2 — `FxConversion` type + `ConvertInvoice` skeleton
Add `types.FxConversion`. Implement `ConvertInvoice(inv, resolution)`: convert net once (`round(net_charge × rate)`), convert each line/discount/credit, stamp line originals, save `fx_conversion`. Unit-test amounts + snapshot.

### Task 3 — Customer `billing_currency` + guardrails (§8.2)
DTO field (null clears), response, domain model. `CustomerService.Create/Update`: validate fiat, rates/factors exist for every active/trialing/paused subscription + every wallet, block during an open checkout session, custom-currency factor check, clear-to-null allowed. `customer.updated` payload. Table-driven guardrail tests.

### Task 4 — Rounding (§5.3)
Residual to the largest **positive** line (fallback largest-abs; tie-break lowest line id; single-line takes it all); record `rounding_adjustment`/`rounding_line_item_id`. Tests incl. JPY (0-dp), a negative proration line, all-zero, single-line.

### Task 5 — Finalize integration (the switch)
Insert the shared convert+retax step in `performFinalizeInvoiceActions` **outside** the `InvoiceType == Subscription` gate: after credits/discounts, if `billing_currency` set & different & `fx_conversion` unset & `amount_paid == 0` → resolve rate (missing ⇒ stay DRAFT, invalid-op), `ConvertInvoice`, **recompute tax** on converted amounts, then finalize. For one-off invoices ensure tax is recomputed post-conversion (they don't tax at finalize today). Tests: subscription convert, one-off convert (+retax), missing-rate-stays-draft, convert-once/retry-skip.

### Task 6 — One-off + guardrails (§8.4)
One-off invoice follows the billing currency (a USD request for an INR customer → INR at finalize; missing rate fails create). Reject a one-off created pre-paid for a cross-currency customer (add the guard in `CreateInvoiceRequest.Validate`/`CreateOneOffInvoice` — today silently dropped). Confirm grouped invoicing needs no new code (single-currency by attach-time). No-payment-before-conversion guard (`validateInvoicePaymentEligibility`).

### Task 7 — Subscription create check + inline rate (§8.3)
In `createSubscription`, next to the existing currency check: reject cross-currency create with no tenant rate (fiat) or no custom factor (custom currency); inline `fx_rate` → create a subscription-scope rate in the same tx; reject `fx_rate` when there is nothing to convert.

### Task 8 — The "unaffected" regression test (§1.5)
A test proving finalize issues **no `fx_rates` query** and produces an identical invoice for: (a) no billing currency, (b) billing currency == charge currency. Use a repo/query spy.

## Verification

- `go test ./...` for touched packages + `make lint-ci` green.
- `make migrate-local` applies the new columns on the shared volume.
- Dockerized end-to-end: set a customer `billing_currency`, run a subscription invoice → finalize → assert INR amounts, `fx_conversion` snapshot, INR tax; a no-billing-currency customer → identical to today.

## Deferred to later layer-3 PRs

Pay-first checkout conversion (L3-B); wallet top-up / void / credit-note conversion + custom-currency drafts (L3-C); PDF/portal/estimate/filter display (L3-D).
