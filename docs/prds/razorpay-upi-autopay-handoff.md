# Razorpay UPI Autopay (Charge-Automatically) — Handoff Doc

Status: **Design agreed, implementation not started.**
Last updated: 2026-07-07

## 1. Problem statement

Today, every Razorpay invoice — one-off or subscription — is sent for manual payment via a Payment Link, regardless of a subscription's `collection_method`. There is no support for `charge_automatically`: no mandate/token registration, no auto-charge on invoice finalize, and incoming webhooks for mandate/token events are dropped.

We want to support UPI Autopay (Razorpay's recurring-payment / "Charge-At-Will" product) so that once a customer authorizes a mandate on their first invoice, future one-off invoices and subscription renewal invoices can be auto-charged against that mandate up to its authorized ceiling, falling back to manual payment where auto-charge isn't possible (no mandate, mandate not confirmed, invoice exceeds the mandate's `max_amount`, or the tenant hasn't opted in).

## 2. Current state (verified against code)

### 2.1 Checkout flow today (Payment Links only)
- No file literally named `checkout.go`. Logic lives in `internal/integration/razorpay/payment.go` (payload building) + `checkout_adapter.go` (interface glue).
- Flow: `POST /v1/checkout/sessions` (`internal/api/router.go:416`) → `CheckoutSessionHandler.Create` (`internal/api/v1/checkout_session.go:35`) → `checkoutSessionService.Create` → `executeCheckoutAction` (`internal/ee/service/checkout_session.go:27`, `checkout_session_actions.go:14`) → `CheckoutAdapter.CreatePaymentLink` (`checkout_adapter.go:17`) → `PaymentService.CreatePaymentLink` (`payment.go:42-369`) → Razorpay SDK `PaymentLink.Create` (`client.go:276`) → `POST /v1/payment_link`.
- Payload has **no `method` key** — fully generic, hosted page offers whatever methods are enabled on the merchant account. No forcing of UPI.
- Completion: webhook `POST /v1/webhooks/razorpay/:tenant_id/:environment_id` (`router.go:652`) → `webhook/handler.go:64` → `handlePaymentLinkPaid` (`:322`).
- Razorpay customer creation is already idempotent and reusable: `EnsureCustomerSyncedToRazorpay` (`customer.go:48-117`) checks `Customer.Metadata["razorpay_customer_id"]`, then `entity_integration_mapping`, before creating a new Razorpay customer. **This pattern should be reused for token/mandate tracking (see §4.2).**

### 2.2 Invoice sync to Razorpay today (no autopay, no type filtering)
- Finalize path (all invoice origins converge here): `CreateOneOffInvoice` (`invoice.go:91`) → `CreateInvoice` (`invoice.go:282`) → `FinalizeInvoice` (`invoice.go:861`) → **`performFinalizeInvoiceActions` (`invoice.go:874`)** — this is the single hook point; Temporal renewal workflows and the synchronous `CreateSubscriptionInvoice` (`invoice.go:1794`) → `ProcessDraftInvoice` (`invoice.go:1178`) path also terminate here.
- `performFinalizeInvoiceActions` publishes `WebhookEventInvoiceUpdateFinalized` unconditionally (no `InvoiceType`, `SubscriptionID`, `billing_reason`, or currency gate).
- Dispatch: `internal/integration/events/handler.go:58-68` → `DispatchInvoiceVendorSync` (`dispatch.go:100-160`) → `triggerRazorpayIfEnabled` (`dispatch.go:423-449`) — only checks connection existence, `conn.IsInvoiceOutboundEnabled()`, and idempotency (`invoiceAlreadySynced`).
- `SyncInvoiceToRazorpayIfEnabled` (`invoice.go:~1307`) has **no `collectionMethod` parameter**, unlike its Stripe counterpart (`SyncInvoiceToStripeIfEnabled(ctx, invoiceID, collectionMethod string)`), which does branch on it (`internal/integration/stripe/invoice_sync.go:164-183`).
- A separate legacy function `SyncInvoiceToExternalVendors` (`invoice.go:4180-4206`) does skip invoices with `SubscriptionID == nil`, but its only caller is **commented out** in `internal/temporal/registration.go:453` — dead code, not in the live path. Don't be misled by it.
- `internal/integration/razorpay/invoice.go` (`buildInvoiceData`/`buildLineItems`, ~144-261) never references `SubscriptionID`, `period_start/end`, or `billing_period` — blank `billing_period` (as on one-off invoices) is harmless.

### 2.3 Webhook handling gaps
- Recognized events today (`internal/integration/razorpay/webhook/types.go:11-21`): `payment.captured`, `payment.failed`, `payment.authorized`, `payment_link.paid/cancelled/expired`.
- Dispatch switch (`webhook/handler.go:64-87`) handles captured/failed/payment_link — **`payment.authorized` falls through to `default` and is only logged, not acted on.**
- Nothing handles `token.confirmed`, `token.rejected`, `token.cancelled`, or any mandate lifecycle event.

### 2.4 Existing config/mapping patterns to reuse
- **Per-provider sync config**: `internal/types/sync_config.go:7-22` — `SyncConfig{Plan, Subscription, Invoice, Customer, Payment, Deal, Quote *EntitySyncConfig}`, each `EntitySyncConfig{Inbound, Outbound bool}`. Stored as jsonb on `Connection.sync_config` (`ent/schema/connection.go:47-48`); exposed via `base_config`/`current_config` in `internal/api/dto/integration.go:106-114`. `IsInvoiceOutboundEnabled()` lives on `*connection.Connection` (`internal/domain/connection/model.go:366-370`).
- **Generic catch-all field**: `Connection.metadata` (`ent/schema/connection.go:45-46`, untyped `map[string]interface{}`) — an alternative landing spot if we don't want to extend the typed `SyncConfig` struct.
- **entity_integration_mapping**: `ent/schema/entityintegrationmapping.go:27-59` — fields `entity_id`, `entity_type`, `provider_type`, `provider_entity_id`, optional `metadata` jsonb; unique index on `(tenant_id, environment_id, entity_type, entity_id, provider_type)`. `entity_type` enum (`internal/types/entityintegrationmapping.go:11-22`) is a **plain Go string const**, not a Postgres enum — currently `Customer, Plan, Invoice, Subscription, Payment, CreditNote, Addon, Item, ItemPrice, Price`. Adding `PaymentMethod`/`Mandate` is a one-line addition, no migration needed.
- **CollectionMethod**: `internal/types/subscription.go:163-177` — `charge_automatically` / `send_invoice`, DB-defaults to `charge_automatically` (`ent/schema/subscription.go:152-158`). Lives **only on Subscription today** — not on Invoice, and not on Customer.
- **PaymentMethod domain model**: `internal/domain/paymentmethod/model.go:8-28` (`IsDefault`, `GatewayMethodID`, etc.) — currently populated **only from Moyasar** (`internal/integration/moyasar/webhook/handler.go`). `types.PaymentMethodProvider` doesn't list Razorpay yet.

## 3. Razorpay-side research (external API, verified via docs.razorpay.com)

### 3.1 Two products, pick one
- **Razorpay Subscriptions API** (plan-based, fixed amount+interval) — current "Supported Payment Methods" doc page lists **cards only**; older docs/tags mention UPI Autopay under Subscriptions, but this is unconfirmed/possibly stale. **Do not build against this without confirming with Razorpay support first.**
- **Recurring Payments API ("Charge-At-Will")** — token-based, variable amount, we control timing/amount per charge. Razorpay's own docs recommend this for usage-based billing. **This is the one we're building against.**

### 3.2 Two ways to register a mandate
- **Raw Order + Payment API (2 calls)**: `POST /v1/orders` (with `token{max_amount, expire_at, frequency}` block, `method: "upi"`) → `POST /v1/payments/create/upi` (`recurring: "1"`, `upi.flow: "intent"|"collect"`) referencing that order. We own Customer + Order lifecycle and full UX control.
- **Registration Link API (1 call)**: `POST /v1/subscription_registration/auth_links` — Razorpay creates the customer/order internally, returns a `short_url` (same shape as today's Payment Link response). `method` is fixed per request (e.g. `upi`), no cross-method picker, no `callback_url` support (completion tracked via webhook only). **Chosen direction: this is the closer drop-in replacement for our existing Payment Link flow** — minimal restructuring, same "one call → redirect → done" UX.

### 3.3 Constraints that shape the design
- Max amount per unattended UPI autopay debit: **~₹99,999** (regular MCC) or **₹2,00,000** (lending/investment MCC) — set at mandate registration as `max_amount`; every future debit must stay under it.
- **INR only.**
- **One successful debit per token per billing cycle** (NPCI rule) — no retry-charging the same token twice in a cycle.
- Pre-debit notification: customer must be notified ~24h before a debit; actual debit fires ~25h after notification — **not instant/on-demand**.
- Mandates can't be edited once registered (only cancelled + re-registered); customer can pause/cancel anytime from their UPI app.

### 3.4 Subsequent charge (same regardless of registration method)
`POST /v1/orders` (new order) → `POST /v1/payments/create/recurring` with `token`, `customer_id`, `order_id`, `recurring: true`, variable `amount` (must be ≤ token's `max_amount`).

### 3.5 Go SDK (`github.com/razorpay/razorpay-go`)
Has `Order.Create`, `Payment.CreateUpi`, `Token.Fetch`/`Token.All`/`Token.Delete` — covers most of this without raw HTTP. Gaps to verify directly against SDK source before implementing: no documented "cancel token" helper, and unclear if there's a named method for `/payments/create/recurring` vs. a generic request call.

### 3.6 Reference links
- Overview: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/
- Mandate registration: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi-intent/initiate-mandate-registration/
- Subsequent charges: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi-intent/execute-subsequent-payments/
- Tokens: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/tokens/
- Webhooks: https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/webhooks/
- API reference root: https://razorpay.com/docs/api/ · Orders: https://razorpay.com/docs/api/orders/ · Payments: https://razorpay.com/docs/api/payments/
- Postman workspace: https://www.postman.com/razorpaydev/workspace/razorpay-public-workspace
- Go SDK: https://github.com/razorpay/razorpay-go
- Existing (aspirational, unimplemented) internal PRD: `docs/prds/integrations/payments/lifecycle.md:144-218`

## 4. Agreed decision direction

1. **Provider-agnostic config, not Razorpay-specific.** Extend `SyncConfig` (`internal/types/sync_config.go`) with a new sibling section, e.g.:
   ```go
   type PaymentProviderConfig struct {
       AutoChargeEnabled bool   `json:"auto_charge_enabled"`
       PreferredMethod   string `json:"preferred_method,omitempty"` // "upi", "emandate", "card"
       MaxAmount         int64  `json:"max_amount,omitempty"`
   }
   ```
   Added as `Payment *PaymentProviderConfig` on `SyncConfig`, defaulted off in `ProviderBaseSyncConfig()`. Backward compatible for free — jsonb column, missing key unmarshals to zero value, no migration/backfill needed. Any future provider gets this field automatically.

2. **Token/mandate tracking reuses `entity_integration_mapping` + `PaymentMethod`.** Extend `PaymentMethod` domain model (today Moyasar-only) to also be populated from Razorpay (`GatewayMethodID = token_id`, metadata carries `max_amount`/`frequency`/status). Add a new `entity_type` constant (e.g. `PaymentMethod`) to `internal/types/entityintegrationmapping.go` — no migration needed, plain string enum. Dual-write pattern mirrors `EnsureCustomerSyncedToRazorpay`: on `token.confirmed` webhook, look up the customer via `razorpay_customer_id` mapping, then create the `PaymentMethod` + `entity_integration_mapping` row for the token.

3. **Registration flow = Registration Link API (1 call)**, swapped in as a new `CheckoutAdapter` method (`CreateRegistrationLink`) alongside the existing `CreatePaymentLink` — not a replacement. Response shape (`short_url` under `NextAction`) stays compatible with the frontend's existing handling.

4. **Single enforcement hook**: add the auto-charge branch inside `performFinalizeInvoiceActions` (`invoice.go:874`) — this one location already covers one-off invoices, subscription renewal invoices (Temporal), and the synchronous draft-processing path, so no need to duplicate logic per invoice origin.

5. **`CollectionMethod` needs to move (partially) onto Invoice.** Today it only exists on Subscription, which works for renewal invoices (read off the parent subscription) but not one-off invoices (no subscription to read from). Decision: add an optional `CollectionMethod` field to Invoice itself, defaulting to `send_invoice` for backward compatibility on one-off invoices unless explicitly overridden; for renewal invoices, copy the subscription's `CollectionMethod` down onto the invoice at creation time (so the invoice is self-describing about which path was taken, useful for audit/debugging).

6. **Routing logic** inside the finalize hook:
   - If `invoice.CollectionMethod == charge_automatically` AND connection's `PaymentProviderConfig.AutoChargeEnabled` AND a confirmed, non-expired token exists for the customer AND `invoice.Total <= token.MaxAmount` → create Order + `POST /payments/create/recurring` with the stored `token_id`.
   - Otherwise (no token, over cap, feature disabled, `send_invoice`) → unchanged existing behavior (Payment Link / Registration Link send).

7. **Webhook handler additions** (`internal/integration/razorpay/webhook/handler.go`): add cases for `token.confirmed` / `token.rejected` / `token.cancelled`; stop dropping `payment.authorized`; route `payment.captured` payloads containing a `token_id` to invoice-payment-recording logic (distinguishing recurring charges from one-off/manual payments).

## 5. Explicit open questions / risks not yet resolved

- Confirm with Razorpay support whether Subscriptions API genuinely dropped UPI Autopay support, or if that's just a stale docs page — affects whether Recurring Payments (CAW) is definitely the only viable path.
- Decide the UX/business rule for invoices that exceed the mandate's `max_amount` — block auto-charge and fall back to manual send (current default assumption), or prompt for step-up re-authorization.
- Verify exact `razorpay-go` SDK method for `/payments/create/recurring` and for cancelling a token directly against SDK source (`resources/payment.go`, `resources/token.go`) before writing code — docs didn't confirm a named helper for either.
- Decide whether `PreferredMethod` in `PaymentProviderConfig` should support fallback ordering (e.g. try UPI, fall back to emandate) or just a single fixed method for v1.
- No decision yet on how a customer's mandate gets *set up* if their first invoice is a subscription renewal rather than a checkout session (i.e., do we require mandate registration to always happen via checkout first, or do we need a standalone "set up autopay" flow independent of checkout?).

## 6. Next steps (in order)

1. Write/confirm the exact Go struct changes: `PaymentProviderConfig` on `SyncConfig`, new `entity_type` constant, optional `CollectionMethod` field on Invoice domain model + Ent schema + migration.
2. Implement `CreateRegistrationLink` on the Razorpay `CheckoutAdapter`/`PaymentService`, gated by `PaymentProviderConfig.AutoChargeEnabled`.
3. Implement webhook handling for `token.confirmed`/`token.rejected`/`token.cancelled` and the `PaymentMethod` + `entity_integration_mapping` dual-write on confirmation.
4. Implement the auto-charge branch inside `performFinalizeInvoiceActions`, including the Order + `payments/create/recurring` call and fallback to existing Payment Link logic.
5. Update `payment.captured`/`payment.authorized` handling to record recurring-charge outcomes against the invoice.
6. Resolve the open questions in §5 before finalizing behavior for over-cap invoices and non-checkout mandate setup.
7. Write tests covering: mandate registration success/failure, auto-charge success/failure/over-cap, fallback-to-manual behavior, and backward compatibility (existing tenants with no `PaymentProviderConfig` set see zero behavior change).

## 7. Where things live (for continuity)

- This doc: `docs/prds/razorpay-upi-autopay-handoff.md` (repo root `docs/prds/`, consistent with existing feature design docs like `docs/prds/STRIPE_INTEGRATION_DOCUMENTATION.md` and `docs/prds/payment_system_design.md`).
- Related existing (unimplemented) PRD: `docs/prds/integrations/payments/lifecycle.md:144-218`.
- Key files to touch: `internal/types/sync_config.go`, `internal/types/entityintegrationmapping.go`, `internal/domain/paymentmethod/model.go`, `internal/domain/invoice/model.go` (+ `ent/schema/invoice.go`), `internal/integration/razorpay/{payment.go,checkout_adapter.go,invoice.go,client.go}`, `internal/integration/razorpay/webhook/{handler.go,types.go}`, `internal/ee/service/invoice.go` (`performFinalizeInvoiceActions`, `SyncInvoiceToRazorpayIfEnabled`).
