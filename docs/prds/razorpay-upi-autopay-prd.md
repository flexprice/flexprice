# Razorpay UPI Autopay — Product Requirements & Data Model

Status: **Design agreed, implementation not started.**
Date: 2026-07-07

Diagrams: `docs/prds/diagrams/razorpay-autocharge-flow.excalidraw` (checkout + auto-charge flows), `docs/prds/diagrams/razorpay-autocharge-erd.excalidraw` (data model).

---

## 1. Overview & Problem Statement

Today, every Razorpay invoice — one-off or subscription — is sent for manual payment via a Payment Link, regardless of a subscription's `collection_method`. There is no support for `charge_automatically`: no mandate/token registration, no auto-charge on invoice finalize, and incoming mandate-lifecycle webhooks are dropped entirely.

We're adding UPI Autopay (Razorpay's token-based "Charge-At-Will" recurring payment product) so that once a customer authorizes a mandate on their first invoice, future one-off and subscription-renewal invoices auto-charge against that mandate — up to its authorized ceiling — falling back to manual payment whenever auto-charge isn't possible (no mandate, mandate not confirmed/expired/cancelled, invoice exceeds the ceiling, or the tenant hasn't opted in).

## 2. Goals & Non-Goals

**Goals (v1):**
- Support UPI Autopay mandate registration at checkout, combined with the first invoice payment in one step.
- Auto-charge all subsequent invoices (one-off, subscription renewal, draft-processed) against a confirmed mandate.
- Guarantee an invoice is never charged twice, under any combination of retries, crashes, or concurrent webhook delivery.
- Fall back to today's manual Payment Link flow whenever auto-charge isn't possible, with zero behavior change for tenants who don't opt in.

**Explicit non-goals (v1):**
- Card-based mandate registration (design anticipates it structurally; not implemented).
- A dunning state machine, grace-period tracking, or auto-cancellation on repeated failure — simple fallback only.
- Proactive "mandate expiring soon" reminder notifications.
- Customer-facing method picker — the method (UPI) is fixed per request, matching a real Razorpay API constraint, not a design choice we're free to change.
- Reconciliation between credit notes/refunds and already-succeeded charges beyond normal invoice total computation.

## 3. User Stories / Scenarios

- **New customer, autopay-enabled tenant**: customer completes checkout → authorizes a UPI mandate → that same action pays the first invoice → all future invoices auto-charge silently.
- **Existing mandate, new invoice**: subscription renews → invoice finalizes → auto-charges against the existing mandate with no customer action.
- **Mandate expires mid-subscription**: an invoice arrives after `token.expire_at` → falls back to manual Payment Link for that invoice + a fresh Registration Link is sent inviting re-authorization.
- **Mandate rejected or cancelled**: customer declines authorization, or cancels the mandate later from their UPI app → all falls back to manual, no special handling beyond routing.
- **Invoice exceeds the mandate ceiling**: falls back to manual for that invoice only (existing over-cap behavior, unchanged).
- **Tenant hasn't opted in**: zero behavior change — today's Payment Link flow, untouched.

## 4. Current State (verified against code)

### 4.1 Checkout flow today (Payment Links only)
- No file literally named `checkout.go`. Logic lives in `internal/integration/razorpay/payment.go` (payload building) + `checkout_adapter.go` (interface glue).
- Flow: `POST /v1/checkout/sessions` (`internal/api/router.go:416`) → `CheckoutSessionHandler.Create` (`internal/api/v1/checkout_session.go:35`) → `checkoutSessionService.Create` → `executeCheckoutAction` (`internal/ee/service/checkout_session.go:27`, `checkout_session_actions.go:14`) → `CheckoutAdapter.CreatePaymentLink` (`checkout_adapter.go:17`) → `PaymentService.CreatePaymentLink` (`payment.go:42-369`) → Razorpay SDK `PaymentLink.Create` (`client.go:276`) → `POST /v1/payment_link`.
- Payload has **no `method` key** — fully generic, hosted page offers whatever methods are enabled on the merchant account. No forcing of UPI.
- Completion: webhook `POST /v1/webhooks/razorpay/:tenant_id/:environment_id` (`router.go:652`) → `webhook/handler.go:64` → `handlePaymentLinkPaid` (`:322`).
- Razorpay customer creation is already idempotent and reusable: `EnsureCustomerSyncedToRazorpay` (`customer.go:48-117`) checks `Customer.Metadata["razorpay_customer_id"]`, then `entity_integration_mapping`, before creating a new Razorpay customer. This dual-write pattern is reused for token/mandate tracking (§6.2 below).

### 4.2 Invoice sync to Razorpay today (no autopay, no type filtering)
- Finalize path (all invoice origins converge here): `CreateOneOffInvoice` (`invoice.go:91`) → `CreateInvoice` (`invoice.go:282`) → `FinalizeInvoice` (`invoice.go:861`) → **`performFinalizeInvoiceActions` (`invoice.go:874`)** — this is the single hook point; Temporal renewal workflows and the synchronous `CreateSubscriptionInvoice` (`invoice.go:1794`) → `ProcessDraftInvoice` (`invoice.go:1178`) path also terminate here.
- `performFinalizeInvoiceActions` publishes `WebhookEventInvoiceUpdateFinalized` unconditionally (no `InvoiceType`, `SubscriptionID`, `billing_reason`, or currency gate).
- Dispatch: `internal/integration/events/handler.go:58-68` → `DispatchInvoiceVendorSync` (`dispatch.go:100-160`) → `triggerRazorpayIfEnabled` (`dispatch.go:423-449`) — only checks connection existence, `conn.IsInvoiceOutboundEnabled()`, and idempotency (`invoiceAlreadySynced`).
- `SyncInvoiceToRazorpayIfEnabled` (`invoice.go:~1307`) has **no `collectionMethod` parameter**, unlike its Stripe counterpart (`SyncInvoiceToStripeIfEnabled(ctx, invoiceID, collectionMethod string)`), which does branch on it (`internal/integration/stripe/invoice_sync.go:164-183`).
- A separate legacy function `SyncInvoiceToExternalVendors` (`invoice.go:4180-4206`) does skip invoices with `SubscriptionID == nil`, but its only caller is **commented out** in `internal/temporal/registration.go:453` — dead code, not in the live path.
- `internal/integration/razorpay/invoice.go` (`buildInvoiceData`/`buildLineItems`, ~144-261) never references `SubscriptionID`, `period_start/end`, or `billing_period` — blank `billing_period` (as on one-off invoices) is harmless.

### 4.3 Webhook handling gaps
- Recognized events today (`internal/integration/razorpay/webhook/types.go:11-21`): `payment.captured`, `payment.failed`, `payment.authorized`, `payment_link.paid/cancelled/expired`.
- Dispatch switch (`webhook/handler.go:64-87`) handles captured/failed/payment_link — **`payment.authorized` falls through to `default` and is only logged, not acted on.**
- Nothing handles `token.confirmed`, `token.rejected`, `token.cancelled`, or any mandate lifecycle event.

### 4.4 Existing config/mapping patterns reused by this design
- **Per-provider sync config**: `internal/types/sync_config.go:7-22` — `SyncConfig{Plan, Subscription, Invoice, Customer, Payment, Deal, Quote *EntitySyncConfig}`, each `EntitySyncConfig{Inbound, Outbound bool}`. Stored as jsonb on `Connection.sync_config` (`ent/schema/connection.go:47-48`); exposed via `base_config`/`current_config` in `internal/api/dto/integration.go:106-114`. `IsInvoiceOutboundEnabled()` lives on `*connection.Connection` (`internal/domain/connection/model.go:366-370`).
- **Generic catch-all field**: `Connection.metadata` (`ent/schema/connection.go:45-46`, untyped `map[string]interface{}`) — considered as an alternative landing spot for the new config but rejected in favor of a dedicated typed field (§7).
- **entity_integration_mapping**: `ent/schema/entityintegrationmapping.go:27-59` — fields `entity_id`, `entity_type`, `provider_type`, `provider_entity_id`, optional `metadata` jsonb; unique index on `(tenant_id, environment_id, entity_type, entity_id, provider_type)`. `entity_type` enum (`internal/types/entityintegrationmapping.go:11-22`) is a **plain Go string const**, not a Postgres enum — currently `Customer, Plan, Invoice, Subscription, Payment, CreditNote, Addon, Item, ItemPrice, Price`. Adding `PaymentMethod`/`InvoiceCharge` is a one-line addition, no migration needed.
- **CollectionMethod**: `internal/types/subscription.go:163-177` — `charge_automatically` / `send_invoice`, DB-defaults to `charge_automatically` (`ent/schema/subscription.go:152-158`). Lives **only on Subscription today** — not on Invoice, and not on Customer.
- **PaymentMethod domain model**: `internal/domain/paymentmethod/model.go:8-28` (`IsDefault`, `GatewayMethodID`, etc.) — currently populated **only from Moyasar** (`internal/integration/moyasar/webhook/handler.go`). `types.PaymentMethodProvider` doesn't list Razorpay yet.
- **CheckoutSession domain model** (`internal/domain/checkout/model.go:104-172`): has an `Configuration JSONBCheckoutConfiguration` jsonb field (`types.CheckoutConfiguration`, `internal/types/checkout_configuration.go`) currently holding only `CreateSubscriptionParams` — this is the field §7.2 extends. `CreateCheckoutSessionRequest` (`internal/api/dto/checkout_session.go:14-25`) already has a **required top-level `payment_provider` field**. `CollectionMethod` is **not** currently read/passed anywhere in the checkout flow (`checkout_session.go`, `checkout_session_actions.go`, `checkout_adapter.go` all confirmed to not reference it) — checkout currently creates a draft subscription without populating it.

## 5. Razorpay-Side API Research

### 5.1 Two products, one chosen
- **Razorpay Subscriptions API** (plan-based, fixed amount+interval) — current "Supported Payment Methods" doc page lists **cards only**; older docs/tags mention UPI Autopay under Subscriptions, but this is unconfirmed/possibly stale. Do not build against this without confirming with Razorpay support first (open question, §14).
- **Recurring Payments API ("Charge-At-Will")** — token-based, variable amount, we control timing/amount per charge. Razorpay's own docs recommend this for usage-based billing. **This is the product this PRD builds against.**

### 5.2 Two ways to register a mandate
- **Raw Order + Payment API (2 calls)**: `POST /v1/orders` (with `token{max_amount, expire_at, frequency}` block, `method: "upi"`) → `POST /v1/payments/create/upi` (`recurring: "1"`, `upi.flow: "intent"|"collect"`) referencing that order.
- **Registration Link API (1 call, chosen)**: `POST /v1/subscription_registration/auth_links` — Razorpay creates the customer/order internally, returns a `short_url` (same shape as today's Payment Link response). `method` is fixed per request (e.g. `upi`), **no cross-method picker on the hosted page**, no `callback_url` support (completion tracked via webhook only). This is the closer drop-in replacement for the existing Payment Link flow — minimal restructuring, same "one call → redirect → done" UX. **Confirmed via Razorpay docs: the registration link's `amount` field is charged as the authorization payment itself** — mandate setup and the first invoice payment happen together, in one call.

### 5.3 Constraints that shape the design
- Max amount per unattended UPI autopay debit: current Razorpay published limits are ~₹15,000 default for unattended debits (higher for specific categories like insurance premiums, mutual fund SIPs, credit card bills — up to ₹1,00,000 for lending/investment categories); older reference points cited ~₹99,999/₹2,00,000 by MCC — **the exact current cap should be re-verified against Razorpay's live docs at implementation time**, since published limits have changed over time.
- **INR only.**
- **One successful debit per token per billing cycle** (NPCI rule) — no retry-charging the same token twice in a cycle.
- Pre-debit notification: customer must be notified ~24h before a debit; actual debit fires ~25h after notification — **not instant/on-demand**.
- Mandates can't be edited once registered (only cancelled + re-registered); customer can pause/cancel anytime from their UPI app.
- Cards, by contrast, have **no practical RBI-imposed ceiling** — card e-mandates are the default choice for high-value recurring collections where UPI's cap is insufficient. Same pre-debit-notification requirement applies to both methods.

### 5.4 Subsequent charge (same regardless of registration method)
`POST /v1/orders` (new order) → `POST /v1/payments/create/recurring` with `token`, `customer_id`, `order_id`, `recurring: true`, variable `amount` (must be ≤ token's `max_amount`).

### 5.5 No idempotency-key protection on the calls we need
Razorpay's idempotency-key headers (`X-Payout-Idempotency`, `X-Transfer-Idempotency`, `X-Refund-Idempotency`) exist only for Payouts/Transfers/Refunds — **not** for `POST /v1/orders` or `POST /v1/payments/create/recurring`, the exact two calls auto-charge needs. Idempotency must therefore be owned entirely on our side (§9).

### 5.6 Go SDK (`github.com/razorpay/razorpay-go`)
Has `Order.Create`, `Payment.CreateUpi`, `Token.Fetch`/`Token.All`/`Token.Delete` — covers most of this without raw HTTP. Gaps to verify directly against SDK source before implementing: no documented "cancel token" helper, and unclear if there's a named method for `/payments/create/recurring` vs. a generic request call.

### 5.7 Reference links
- Overview: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/
- Mandate registration: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi-intent/initiate-mandate-registration/
- Subsequent charges: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi-intent/execute-subsequent-payments/
- Tokens: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/tokens/
- Webhooks: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/webhooks/
- API reference root: https://razorpay.com/docs/api/ · Orders: https://razorpay.com/docs/api/orders/ · Payments: https://razorpay.com/docs/api/payments/
- Test UPI details (for QA, §13): https://razorpay.com/docs/payments/payments/test-upi-details/ — use UPI ID `success@razorpay` to simulate an approved payment/mandate, `failure@razorpay` to simulate a decline.
- Webhook testing: https://razorpay.com/docs/webhooks/validate-test/ — test-mode webhooks fire from real test-mode transactions; OTP `754081` is required to add/edit/delete a test-mode webhook.
- Postman workspace: https://www.postman.com/razorpaydev/workspace/razorpay-public-workspace
- Go SDK: https://github.com/razorpay/razorpay-go

## 6. Payment Method Support: UPI and Cards

Razorpay's Recurring Payments ("Charge-At-Will") API family supports both UPI Autopay and card tokenization on the same underlying primitives (`token`, `POST /v1/orders`, `POST /v1/payments/create/recurring`). This means the entire idempotent auto-charge flow, claim/lock mechanism, per-cycle dedup, and reconciliation sweep (§9–§11) are all method-agnostic and need zero changes to support cards later — they operate on "a token," not "a UPI token" specifically.

What differs is registration, and it's not a config-flag swap:

- **UPI**: `POST /v1/subscription_registration/auth_links` with `type: "upi"` — one call, returns a `short_url` for redirect, completion tracked via webhook only.
- **Cards**: the customer completes a hosted Checkout form (card entry + AFA/OTP) as the authorization transaction — a different integration surface entirely, not the Registration Link API. This needs its own adapter method (e.g. `CreateCardMandateCheckout`) as a **future** iteration, not part of this PRD's implementation.
- The registration method is fixed per request — Razorpay's Registration Link API has no cross-method picker on its hosted page; the caller decides UPI vs. card before generating the link. This is why `preferred_payment_method` is declared by the checkout session request (§7.2), not resolved by the customer at Razorpay's hosted page.
- UPI's debit cap vs. cards having no practical RBI-imposed ceiling means the over-cap fallback (mandate usability check step 4, §8) is realistically UPI-specific — it simply won't trigger for card mandates in practice, no special-casing needed.

**v1 scope: UPI only.** Card support is a known, designed-for extension point — the config structure below already has a slot for it — not a future re-architecture.

## 7. Configuration Structure

Two distinct layers, solving two different problems: a tenant-wide policy gate/ceiling, vs. a per-checkout-request declaration of intent.

### 7.1 Connection-level: tenant-wide gate + safety ceiling

A **new field on `Connection`**, sibling to the existing `sync_config` column — deliberately **not** nested inside `SyncConfig`. An earlier version of this design proposed adding a field named `Payment *PaymentProviderConfig` onto `SyncConfig`, but `SyncConfig` already has a field named `Payment *EntitySyncConfig` (§4.4 — the inbound/outbound sync toggle for the Payment entity type). Two same-named fields of different types on one struct doesn't compile, and even renamed, it conflates two unrelated concepts (entity-sync toggle vs. auto-charge behavior). This design corrects that by giving auto-charge config its own column entirely.

```go
// new field on Connection (new jsonb column, e.g. payment_provider_config)
type PaymentProviderConfig struct {
    Razorpay *RazorpayPaymentProviderConfig `json:"razorpay,omitempty"`
    Stripe   *StripePaymentProviderConfig   `json:"stripe,omitempty"`
    // new provider = new optional field, zero migration
}

type RazorpayPaymentProviderConfig struct {
    AutoChargeEnabled bool  `json:"auto_charge_enabled"`
    MaxAmount         int64 `json:"max_amount,omitempty"` // paise; tenant-wide safety ceiling used when registering new mandates
}

type StripePaymentProviderConfig struct {
    AutoChargeEnabled bool `json:"auto_charge_enabled"`
    // no MaxAmount/PreferredMethod — meaningless for Stripe's off-session model
}
```

This answers: *is auto-charge even available for this tenant+provider* and *what ceiling do we register new mandates with*. Each provider gets its own shape rather than being forced into one generic struct — evaluated against Stripe and Moyasar, a flat shared struct would carry dead fields (`MaxAmount`/`PreferredMethod` mean nothing for Stripe's off-session charging model).

### 7.2 CheckoutSession-level: per-request declaration

A **new field inside the existing `CheckoutSession.Configuration` JSONB column** (`types.CheckoutConfiguration` — zero migration, the column already exists and today only holds `CreateSubscriptionParams`, per §4.4):

```go
type CheckoutConfiguration struct {
    CreateSubscriptionParams *CreateSubscriptionParams      `json:"create_subscription_params,omitempty"`
    PaymentProviderConfig    *CheckoutPaymentProviderConfig `json:"payment_provider_config,omitempty"`
}

type CheckoutPaymentProviderConfig struct {
    CollectionMethod types.CollectionMethod  `json:"collection_method,omitempty"` // shared across providers — not Razorpay-specific
    Razorpay         *RazorpayCheckoutConfig `json:"razorpay,omitempty"`
    Stripe           *StripeCheckoutConfig   `json:"stripe,omitempty"`
}

type RazorpayCheckoutConfig struct {
    PreferredPaymentMethod string `json:"preferred_payment_method,omitempty"` // "upi" | "card"
}
```

Example request body:
```json
{
  "customer_external_id": "...",
  "payment_provider": "razorpay",
  "configuration": {
    "payment_provider_config": {
      "collection_method": "charge_automatically",
      "razorpay": { "preferred_payment_method": "upi" }
    }
  }
}
```

This answers *what does THIS checkout request want*: `collection_method` reuses the existing `types.CollectionMethod` enum (§4.4) and is the source of truth copied down onto the newly-created draft Subscription (§8.2) — this is where that value originates for a checkout-initiated subscription, since checkout is often the first touchpoint before the subscription exists. `preferred_payment_method` picks which registration adapter to call (Registration Link for `"upi"`; the future card-Checkout adapter for `"card"`).

`collection_method` is hoisted to the shared level (not nested per-provider) because it's a generic billing concept both Stripe and Razorpay share — only `preferred_payment_method`, which is genuinely provider-specific, stays nested.

**Validation rule**: reject the checkout request (400) if a provider sub-object other than the one matching the top-level, required `payment_provider` field is populated — e.g. `payment_provider: "stripe"` with a populated `razorpay` sub-object is invalid. The per-provider keying exists for clean Go/JSON typing (a discriminated-union-style shape avoids hand-rolled `UnmarshalJSON` logic), not because multiple providers can genuinely apply to one session — `payment_provider` alone already disambiguates that.

## 8. Proposed Architecture

### 8.1 Checkout / mandate registration flow (Diagram 2 in the `.excalidraw` file)

`POST /v1/checkout/sessions` gates in this order:
1. Connection-level `AutoChargeEnabled` (§7.1)? No → baseline: `CreatePaymentLink` (unchanged existing flow).
2. `PreferredPaymentMethod == "upi"` (v1 scope, §6)? No → fallback to manual (unsupported method).
3. Request's `CollectionMethod == charge_automatically` (§7.2)? No (`send_invoice`) → fallback to manual.
4. Does the customer already have a confirmed, non-expired token for this connection? Yes → skip registration entirely, route this invoice straight into §9's auto-charge flow.
5. No token → compute the mandate ceiling (`min(config.MaxAmount, Razorpay's current published cap)`) → call `POST /v1/subscription_registration/auth_links` with `{customer_id, type: "upi", amount: <this invoice's amount>, max_amount: <ceiling>, expire_at, token: {frequency}}` → returns `short_url`, `order_id`.
6. Present `short_url` to the customer (checkout redirect — identical UX to today's Payment Link `NextAction`).
7. Customer authorizes via UPI app.
8. Terminal outcomes:
   - `token.confirmed` + `payment.captured` together (same `order_id` covers both) → create `PaymentMethod` + `entity_integration_mapping` row (`token_id`, `max_amount`, `expire_at`, `status=confirmed`); mark this invoice PAID from that same payment; all future invoices route through §9.
   - `token.rejected` or `payment.failed` → mandate not confirmed, invoice unpaid → fallback: send manual Payment Link for this invoice (as if `AutoChargeEnabled=off`).
   - Link expires unused (customer never completes) → equivalent to today's `payment_link.expired` — resend Payment Link or a new Registration Link per existing retry/reminder behavior.

### 8.2 Mandate lifecycle & max auto-charge window

- **Max expiry** is a value **we choose** at registration time (the `expire_at` field in the Order's `token` block), not a Razorpay-imposed ceiling. Default to a long window (e.g. 3 years) covering the expected subscription lifetime, so expiry is an exception path, not something routine renewals hit.
- No proactive renewal-reminder sweep in v1 — that would require a scheduled job scanning for near-expiry mandates, out of scope per the "simple fallback, no dunning state" decision below. We only react at invoice-finalize time when we discover the mandate is unusable.
- **Mandate usability check**, evaluated fresh per invoice at finalize time (never cached):
  1. Token exists for customer? No → manual Payment Link (unchanged today).
  2. Token status is `confirmed` (not `rejected`/`cancelled`)? No → manual Payment Link.
  3. `now() < token.expire_at`? No → manual Payment Link **+ fire a new Registration Link**.
  4. `invoice.Total <= token.MaxAmount`? No → manual Payment Link (existing over-cap fallback, unchanged).
  5. All pass → proceed to idempotent auto-charge (§9).
- **Customer-initiated mandate cancellation** (pause/cancel from their UPI app) is only observable via the `token.cancelled` webhook updating the `PaymentMethod`/mapping status — check #2 above then naturally routes future invoices to manual. No polling needed.
- `CollectionMethod` moves (partially) onto Invoice: today it only exists on Subscription, which works for renewal invoices (read off the parent subscription) but not one-off invoices. Decision: add an optional `CollectionMethod` field to Invoice itself, defaulting to `send_invoice` for backward compatibility unless explicitly overridden; for renewal invoices, copy the subscription's `CollectionMethod` down onto the invoice at creation time (self-describing, useful for audit/debugging). For checkout-originated invoices, the value originates from the checkout request's `collection_method` (§7.2, §8.1).

### 8.3 Expired-mandate policy (industry-validated)

Researched behavior across Stripe (Smart Retries + dunning/update-payment-method emails), Razorpay Subscriptions, and Chargebee's Razorpay UPI Autopay integration: **none of them block invoice finalization waiting for re-authorization.** The invoice still finalizes; failed/unavailable automatic collection falls into a dunning-style manual path with a re-auth call-to-action, with a grace period before any punitive action (e.g. subscription cancellation) — never a hard stop on billing.

Decision for v1: **simple fallback, no dunning state machine.**
- Invoice finalizes normally.
- Auto-charge is skipped; the existing manual Payment Link is sent (unchanged path).
- A new Registration Link is additionally fired, inviting the customer to re-authorize a fresh mandate.
- No new subscription/customer state (`mandate_expired_since`, grace-period counters, escalation) is introduced in v1 — deliberately deferred; the fallback + re-auth link reuses paths that already exist, with no new state machine to test or reason about.

### 8.4 Invoice enumeration — what gets auto-charged

All invoice origins already converge on one hook point, `performFinalizeInvoiceActions` (`invoice.go:874`, §4.2):
- One-off invoices (`CreateOneOffInvoice` → `CreateInvoice` → `FinalizeInvoice`)
- Temporal-driven subscription renewal invoices
- The synchronous `CreateSubscriptionInvoice` → `ProcessDraftInvoice` path

No new fan-out is needed — the auto-charge branch is a single conditional inserted at this one hook, firing identically regardless of which path produced the invoice. The only per-origin difference is where `CollectionMethod` is read from (§8.2). Credit notes/refunds are out of scope for auto-charge logic itself — they reduce `invoice.Total` during normal invoice computation, before the charge decision is evaluated, not after. Wallet top-up and addon invoices converge on the same finalize path — no additional handling required.

### 8.5 Routing logic inside the finalize hook

- If `invoice.CollectionMethod == charge_automatically` AND connection's `PaymentProviderConfig.Razorpay.AutoChargeEnabled` AND a confirmed, non-expired token exists for the customer AND `invoice.Total <= token.MaxAmount` → create Order + `POST /payments/create/recurring` with the stored `token_id` (§9).
- Otherwise (no token, over cap, feature disabled, `send_invoice`) → unchanged existing behavior (Payment Link / Registration Link send).

### 8.6 Webhook handler additions

`internal/integration/razorpay/webhook/handler.go`: add cases for `token.confirmed` / `token.rejected` / `token.cancelled`; stop dropping `payment.authorized`; route `payment.captured` payloads containing a `token_id` to invoice-payment-recording logic (distinguishing recurring charges from one-off/manual payments).

## 9. Idempotent Auto-Charge Flow

The core reliability guarantee of this feature: **an invoice must never be charged twice**, and Razorpay provides no idempotency-key protection on the two calls we need (`POST /v1/orders`, `POST /v1/payments/create/recurring` — §5.5). Two layers, used together — Redis for fast in-flight mutual exclusion, Postgres for crash-safe durability:

- **Redis lock**: key `razorpay:autocharge:{tenant_id}:{environment_id}:{invoice_id}` (tenant/env included even though invoice IDs are already globally unique, as defense in depth against future ID-generation changes). Acquired via `SETNX` with a short TTL (~60s, renewed while the Razorpay call is in flight). Held until the invoice reaches a terminal state (confirmed by webhook) or the TTL expires.
- **Postgres claim row**: a new `entity_type` constant `InvoiceCharge` on `entity_integration_mapping` (`internal/types/entityintegrationmapping.go` — plain string enum, no migration needed). Row key: `(tenant_id, environment_id, entity_type=InvoiceCharge, entity_id=invoice_id, provider_type=razorpay)`. The table's existing unique index on that tuple (`ent/schema/entityintegrationmapping.go:27-59`) makes the insert an atomic compare-and-swap. Metadata carries `{status: "claimed"|"succeeded"|"failed", token_id, payment_id}`.

```
performFinalizeInvoiceActions(invoice)
  └─ mandate usability check passes (§8.2)
       └─ AutoChargeInvoice(ctx, invoice):
            1. Acquire Redis lock razorpay:autocharge:{tenant}:{env}:{invoice_id}
               → already held? return (another attempt owns this invoice now)
            2. Insert DB claim row (status: "claimed")
               → unique violation, existing row status "succeeded"?
                    → release lock, return (already charged — crash-recovery no-op)
               → unique violation, existing row status "claimed"?
                    → release lock, return (another attempt/crash-recovery owns it;
                      reconciliation sweep — §11 — will resolve if truly stuck)
               → unique violation, existing row status "failed"?
                    → update row back to "claimed", continue (safe retry)
            3. Per-cycle NPCI dedup check (renewal invoices only, §10 #4)
               → already succeeded this cycle for this token_id? release lock,
                 fall back to manual for this invoice
            4. Create Razorpay Order (POST /v1/orders)
            5. Create Razorpay Payment (POST /v1/payments/create/recurring),
               referencing token_id + order_id
            6. On success: update claim row → status "succeeded", payment_id set
               (Redis lock stays held — only released on webhook confirmation)
            7. Await payment.captured / payment.failed webhook:
               → captured: mark invoice paid, release lock
               → failed: mark claim row "failed", mark invoice payment failed
                 (falls back to manual for this invoice), release lock
               → neither arrives before TTL: lock expires; claim row stays
                 "claimed" — resolved only by the reconciliation sweep (§11),
                 never by a second finalize attempt racing the lock again
```

**Never charge twice, structurally**: the DB claim insert is the single source of truth for "was this invoice ever charged" — even with Redis completely unavailable, the unique constraint alone makes double-charging impossible (throughput degrades to serialized-by-DB-row instead of Redis-fast-rejected, but correctness holds).

**Ambiguous outcomes are never treated as failure.** A timeout/5xx/network error on the Payment call leaves the claim row `claimed`, not `failed` — only an explicit `payment.failed` webhook moves it to `failed` and permits retry. This is the one deliberate exception to "resolve everything inline"; ambiguous states are handled exclusively by the reconciliation sweep, not by inline retry logic, to avoid a second concurrent charge attempt racing an unresolved first one.

**Orphaned Razorpay Orders are an accepted, harmless byproduct.** If a crash happens after step 4 (Order created) but before step 6 (claim marked succeeded), a retry attempt could in theory create a second Order — but Orders don't move money; only the Payment call does, and that's gated by the claim+lock guard already covering step 4 onward. An extra unused Order is clutter, not a financial risk.

## 10. Edge Cases

| # | Scenario | Handling |
|---|---|---|
| 1 | `payment.captured` webhook delivered twice | Webhook handler checks invoice payment status before recording (idempotent-handler invariant) — second delivery is a no-op |
| 2 | Temporal activity retries `AutoChargeInvoice` after the first attempt actually succeeded | Claim row already `succeeded` → step 2 short-circuits |
| 3 | Two invoices for the same customer finalize concurrently | Lock/claim keys are per-invoice-id — both proceed independently; the shared-token cycle constraint is enforced separately (#4) |
| 4 | NPCI: one successful debit per token per billing cycle | Before charging, query `InvoiceCharge` claims for the same `token_id` where the linked invoice's `period_start`/`period_end` overlaps the current cycle and status is `succeeded`. Found → skip charge, fall back to manual for this invoice. One-off invoices have no "cycle" in the NPCI sense — this check applies to renewal invoices only |
| 5 | Invoice total changes between mandate-check and charge (e.g. concurrent credit note) | Non-issue given both operate on the same invoice snapshot passed into `performFinalizeInvoiceActions`; invoice-mutation-after-finalize is guarded by existing invariants outside this design's scope |
| 6 | Razorpay Payment call returns ambiguous result (timeout/5xx/network error) | Claim stays `claimed`, never auto-marked `failed`; resolved only by reconciliation sweep (§11) or an eventual webhook |
| 7 | Webhook never arrives (dropped, Razorpay outage) | Redis lock TTL expires; DB claim row stays `claimed` — intentionally a stuck-but-safe state, resolved by the reconciliation sweep (§11) |
| 8 | Subscription cancelled mid-cycle after invoice finalized but before charge completes | Out of scope — existing subscription-cancellation invariants already govern invoice mutation after finalize; auto-charge needs no special handling beyond what non-autocharge invoices have |
| 9 | Tenant disables `AutoChargeEnabled` mid-flight | Config is read once at the top of the finalize hook for that invoice; a mid-flight flip doesn't affect an already-dispatched attempt — the next invoice picks up the new config |
| 10 | Invoice re-finalized twice due to an unrelated bug elsewhere | Same claim+lock guard handles this identically to any other concurrent-attempt scenario |

## 11. Reconciliation Sweep (resolves edge cases #6/#7)

A periodic job (Temporal workflow or existing cron mechanism, `internal/api/cron/`) running every 15–30 minutes:
1. Query `InvoiceCharge` claim rows with `status = "claimed"` and `created_at` older than a threshold (e.g. 1 hour — comfortably longer than any real in-flight webhook delay).
2. For each: call Razorpay's `Payment.Fetch` (read-only) using the `payment_id` recorded on the claim, if one exists. If no `payment_id` was ever recorded (crash before step 5 completed), treat as abandoned/failed.
3. Resolve the claim to `succeeded` or `failed` based on that authoritative read; update the invoice's payment status accordingly; release any lingering Redis lock defensively (it likely already expired via TTL).

This sweep is the **only** place a stuck claim gets resolved — no invoice-finalize-triggered code path attempts cleanup, keeping the finalize hook single-purpose (charge-initiator only, never reconciler).

## 12. API Changes

- `POST /v1/checkout/sessions` request body gains an optional `configuration.payment_provider_config` block (§7.2). No breaking change — omitted entirely, behavior is identical to today.
- No new endpoints. All new behavior is either inside the existing checkout-session creation path or the existing webhook endpoint (new event cases, not a new route).

## 13. Testing Strategy

Table-driven, real DB (no Ent-client mocking):
- Unit tests: mandate-usability check matrix (all 5 gate conditions, pass/fail combinations, §8.2).
- Unit tests: claim-row state machine (`claimed`→`succeeded`; `claimed`→`failed`→retry-`claimed`; concurrent-insert-rejected).
- Integration test: concurrent `AutoChargeInvoice` calls for the same invoice (goroutines racing against real Postgres + real/miniredis) — assert exactly one Payment call is made.
- Integration test: per-cycle dedup — two renewal invoices in the same cycle sharing a token; second one falls back to manual.
- Integration test: reconciliation sweep resolving a manually-seeded stuck `claimed` row via a stubbed `Payment.Fetch`.
- Webhook idempotency test: duplicate `payment.captured` delivery is a no-op on the second call.
- Backward-compatibility test: tenants with no `PaymentProviderConfig` set see zero behavior change.
- Unit tests: checkout request validation rule (§7.2) — mismatched provider sub-object vs. top-level `payment_provider` is rejected.
- Integration test: checkout session flow (§8.1) — mandate registration + first payment succeeding together, failing together, and expiring unused.

**Manual/Postman verification against Razorpay test mode** (mapped to §8.1/§9's stages): Customer creation (`POST /v1/customers`) → Registration Link (`POST /v1/subscription_registration/auth_links`) → complete auth using test UPI ID `success@razorpay`/`failure@razorpay` (§5.7) → fetch token (`GET /v1/tokens/{token_id}`) to confirm status/max_amount/expire_at → Order (`POST /v1/orders`) → recurring Payment (`POST /v1/payments/create/recurring`) → fetch Payment (`GET /v1/payments/{payment_id}`, used by the reconciliation sweep) → deliberately call the recurring-payment endpoint twice against the same token+cycle to observe Razorpay's own NPCI rejection. Idempotency/race conditions (concurrent claim inserts, Redis lock contention, crash-recovery) cannot be tested via Postman — these require the Go integration tests above.

## 14. Rollout, Backward Compatibility, and Open Questions

**Rollout & backward compatibility:**
- All new config is additive and jsonb-backed — a tenant with no `PaymentProviderConfig` set sees zero behavior change (existing Payment Link flow, byte-for-byte).
- No existing field is renamed or removed; the one structural fix (avoiding the `SyncConfig.Payment` naming collision, §7.1) results in a new field, not a changed one.
- New `entity_type` constant (`InvoiceCharge`) is a plain Go string const addition — no migration.

**Open questions / risks not yet resolved:**
- Confirm with Razorpay support whether the Subscriptions API genuinely dropped UPI Autopay support, or if that's just a stale docs page — affects nothing in this design (we're building against Recurring Payments regardless) but worth closing out for completeness.
- Verify exact `razorpay-go` SDK method for `/payments/create/recurring` and for cancelling a token directly against SDK source (`resources/payment.go`, `resources/token.go`) before writing code — docs didn't confirm a named helper for either.
- Re-verify the exact current UPI unattended-debit cap against Razorpay's live docs at implementation time (§5.3 notes some ambiguity between older and current published figures).
- Fallback ordering across multiple preferred methods (e.g. try UPI, offer card) — deferred past v1; single fixed method per tenant for now.
- No decision yet on how a customer's mandate gets set up if their first invoice is a subscription renewal rather than a checkout session — i.e., do we require mandate registration to always happen via checkout first, or do we need a standalone "set up autopay" flow independent of checkout? (Not blocking v1, since v1's primary path is checkout-originated registration, but worth flagging for a fast-follow.)

## 15. Where Things Live (Continuity)

- This doc: `docs/prds/razorpay-upi-autopay-prd.md`.
- Flow diagrams (Diagram 1: invoice finalize/auto-charge; Diagram 2: checkout session/mandate registration): `docs/prds/diagrams/razorpay-autocharge-flow.excalidraw`.
- Data model ERD: `docs/prds/diagrams/razorpay-autocharge-erd.excalidraw`.
- Related existing (unimplemented) PRD: `docs/prds/integrations/payments/lifecycle.md:144-218`.

**New/modified code surfaces this design introduces:**
- `internal/types/sync_config.go` — no change (avoiding the naming collision, §7.1).
- New `PaymentProviderConfig` type + new jsonb column on `Connection` (`ent/schema/connection.go`) — exact Go file TBD at implementation time, likely a new `internal/types/payment_provider_config.go` alongside `sync_config.go`.
- `internal/types/entityintegrationmapping.go` — new `InvoiceCharge` (and, if not already planned, `PaymentMethod`) `entity_type` constants.
- `internal/domain/paymentmethod/model.go` — extend population to Razorpay (`GatewayMethodID = token_id`, metadata carries `max_amount`/`frequency`/status).
- `internal/domain/invoice/model.go` + `ent/schema/invoice.go` — new optional `CollectionMethod` field + migration.
- `internal/types/checkout_configuration.go` — new `PaymentProviderConfig`/`CheckoutPaymentProviderConfig`/`RazorpayCheckoutConfig` types on `CheckoutConfiguration`.
- `internal/api/dto/checkout_session.go` or `internal/ee/service/checkout_session.go` — checkout request validation for the provider-match rule (§7.2).
- `internal/integration/razorpay/checkout_adapter.go` + `payment.go` — new `CreateRegistrationLink` method alongside existing `CreatePaymentLink`.
- `internal/integration/razorpay/webhook/{handler.go,types.go}` — new cases for `token.confirmed`/`token.rejected`/`token.cancelled`; stop dropping `payment.authorized`; route token-bearing `payment.captured` to auto-charge completion logic.
- `internal/ee/service/invoice.go` — the `AutoChargeInvoice` flow alongside `performFinalizeInvoiceActions`/`SyncInvoiceToRazorpayIfEnabled`.
- New reconciliation sweep — new Temporal workflow or cron handler (`internal/api/cron/` or `internal/temporal/workflows/`).
- Redis lock helper — wherever existing Redis client usage lives in this codebase (verify exact location at implementation time).
