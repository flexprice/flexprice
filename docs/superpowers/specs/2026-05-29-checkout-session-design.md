# Checkout Session — Design Spec
**Date:** 2026-05-29  
**Status:** Draft  
**Scope — v1:** `new_subscription` + `plan_change` (upgrade/downgrade)  
**Scope — v2 (deferred):** trials, addons, quantity change, payment collection, credit purchase, reactivation

---

## 1. Problem Statement

The current subscription upgrade flow is broken in a fundamental way: when a user initiates a plan upgrade, the old subscription is **cancelled immediately** before payment is confirmed. If payment fails or the invoice is never paid, the old subscription is gone — leaving an orphaned pending invoice and no active plan. Recovery requires a human to manually void the invoice and re-assign the old plan.

Beyond upgrades, there is no unified payment collection abstraction. Stripe sync, Paddle sync, and invoice generation are scattered across individual services. There is no hosted checkout URL concept, no clean way to gate subscription changes on payment, and no way to support multiple payment providers consistently.

**Root causes:**
1. Cancel-old → create-new happens atomically regardless of payment outcome
2. No "pending change" window where the old plan stays active until payment is confirmed
3. No rollback mechanism — once the old sub is gone, it is gone
4. No unified entry point for payment collection across providers and intent types
5. Provider sync (Stripe, Paddle) scattered across services with no common abstraction

---

## 2. Goals

- A single `/checkout` endpoint that handles subscription lifecycle actions requiring payment
- Old subscription stays **fully active** during any plan-change pending window — zero orphan risk
- **Built-in automatic rollback**: if payment is not received within the expiry window, pending invoice is voided and old plan silently continues — no human intervention required
- **Auto-apply on payment**: when a payment webhook fires, the subscription change is applied atomically via Temporal
- **Provider-agnostic**: Stripe, Paddle, Moyasar, Razorpay, Flexprice-native, and future providers all return the same response shape
- Payment action URL is **always returned synchronously** — no polling, no second call
- Works with and without payment integrations configured
- Enums and field names stay aligned with Stripe/Chargebee/Paddle so the model is familiar

## 2a. Non-Goals (v1)

- Trials, addon purchase/removal, quantity change, payment collection, credit purchase, subscription reactivation — all deferred to v2
- Refactoring existing renewal/dunning flows (those stay as-is for now)
- Replacing the existing `/subscriptions/change` endpoint immediately (it becomes a thin internal alias)
- Supporting partial payments or instalment plans

---

## 3. Core Concept: CheckoutSession as Universal Wrapper

`CheckoutSession` is a first-class entity that wraps every subscription lifecycle action that touches money. It is the single source of truth for:

- **What** is being done (`intent`)
- **To what** (`entity_type` + `entity_id`, `target_entity_type` + `target_entity_id`)
- **How** payment is being collected (`collection_method`, `checkout_mode`, `gateway`)
- **What happened** (`result`, applied changes)
- **When it expires** (`expires_at` — automatic rollback timer)

The entity is provider-agnostic by design. Provider-specific session IDs (Stripe `cs_xxx`, Paddle `txn_xxx`) live in the existing `entity_integration_mappings` table — the same pattern used for subscriptions and invoices today. No provider-specific data lives in `checkout_sessions`.

```
POST /checkout
       │
       ├─ Validate entities (plan exists, sub exists, customer exists)
       ├─ Create pending invoice in DRAFT (no payment attempt, no finalization)
       ├─ Resolve gateway: request → customer connection → tenant default → FlexpriceNative
       ├─ provider.CreateSession() ← synchronous call to provider API
       │       returns { checkout_url, provider_session_id } immediately
       ├─ Persist CheckoutSession
       ├─ entity_mappings.Create(checkout_session → provider_session_id)
       ├─ Temporal.StartWorkflow(CheckoutExpiryWorkflow, expires_at)
       └─ Return { checkout_session, subscription, invoice }
                          ↑
              payment_action.url ALWAYS present for redirect/embed/invoice types
```

---

## 4. Enum Validation Against Existing Providers

Before defining the field values, every enum is cross-checked against Stripe, Chargebee, and Paddle to ensure naming alignment.

### 4a. `payment_behavior`

| Value | Flexprice (existing) | Stripe | Chargebee | Notes |
|-------|---------------------|--------|-----------|-------|
| `allow_incomplete` | ✅ | ✅ default since 2019 | — | Try charge; if fails → past_due/incomplete |
| `default_incomplete` | ✅ | ✅ (new subs) | — | Create INCOMPLETE without charging |
| `error_if_incomplete` | ✅ | ✅ | — | Return 402 if charge fails; sub unchanged |
| `default_active` | ✅ (Flexprice-only) | ❌ | `no_action` closest | Skip charge, create ACTIVE immediately |
| `pending_if_incomplete` | ❌ **ADD** | ✅ (updates only) | — | Gate change on payment; old state preserved |

**Decision:** Add `pending_if_incomplete` to the existing `PaymentBehavior` enum in `internal/types/subscription.go`. This is the core enabler of the orphan-free upgrade flow. Stripe uses it only for subscription updates; we use it for both `new_subscription` (creates DRAFT, activates on payment) and `plan_change` (old sub stays active, change applies on payment).

**Full v1 enum:**
```go
// In internal/types/subscription.go — add to existing PaymentBehavior const block
PaymentBehaviorAllowIncomplete    PaymentBehavior = "allow_incomplete"     // existing
PaymentBehaviorDefaultIncomplete  PaymentBehavior = "default_incomplete"   // existing
PaymentBehaviorErrorIfIncomplete  PaymentBehavior = "error_if_incomplete"  // existing
PaymentBehaviorDefaultActive      PaymentBehavior = "default_active"       // existing
PaymentBehaviorPendingIfIncomplete PaymentBehavior = "pending_if_incomplete" // ADD
```

### 4b. `collection_method` — existing enum, no changes

Flexprice's existing values match Stripe and Paddle exactly. **Do not add `hosted`, `embedded`, or `none` here** — those are checkout UI concerns, not payment mechanics.

| Value | Flexprice (existing) | Stripe | Chargebee | Paddle |
|-------|---------------------|--------|-----------|--------|
| `charge_automatically` | ✅ | ✅ | `charge_immediately` | `automatic` |
| `send_invoice` | ✅ | ✅ | `send_invoice` | `manual` |

`collection_method` answers: *how does money move after the invoice is finalized?*

### 4c. `checkout_mode` — new field, CheckoutSession-only

`checkout_mode` answers a different question: *how does the customer interact with the payment UI?* This is separate from `collection_method` — just as Stripe separates their Checkout Session (UI) from the subscription's `collection_method` (payment mechanics).

| Value | Meaning | payment_action.type | URL returned? |
|-------|---------|---------------------|--------------|
| `hosted` | Redirect to provider-hosted payment page (Stripe Checkout, Paddle Checkout) | `redirect` | ✅ always |
| `embedded` | Mount provider SDK widget in caller's UI (Stripe Elements, Moyasar embedded) | `embed` | ✅ (embed token) |
| `none` (default) | No payment UI — charge on file or send invoice directly | `charge` or `invoice` | depends |

**How `collection_method` × `checkout_mode` compose:**

| `collection_method` | `checkout_mode` | What happens | `payment_action.type` | URL? |
|--------------------|----------------|--------------|----------------------|------|
| `charge_automatically` | `none` (default) | Charge saved card directly | `charge` | ❌ (immediate) |
| `charge_automatically` | `hosted` | Hosted page → card collected → charge | `redirect` | ✅ |
| `charge_automatically` | `embedded` | Embed widget → card → charge | `embed` | ✅ |
| `send_invoice` | `none` (default) | Finalize invoice, email it | `invoice` | ✅ (invoice link) |
| `send_invoice` | `hosted` | Invoice + payment link via hosted page | `redirect` | ✅ |

**Edge cases for `charge_automatically` + `none`:**

| Scenario | `payment_behavior` | Outcome | `payment_action.type` |
|----------|-------------------|---------|----------------------|
| Card on file, charge succeeds | any | Session → COMPLETED inline | `charge` (no URL) |
| Card on file, charge fails | `error_if_incomplete` | HTTP 402, session not created | — |
| Card on file, charge fails | `pending_if_incomplete` | Session → PENDING, expiry timer starts | `charge` (no URL, wait for retry) |
| Card on file, charge fails | `allow_incomplete` | Change applied anyway, sub → past_due | `charge` (no URL) |
| No card on file | any | Fall back to `hosted` automatically | `redirect` (URL generated) |

---

## 5. Intent Taxonomy — v1

### v1 Intents

| Intent | entity_type | entity_id | target_entity_type | target_entity_id | Description |
|--------|------------|-----------|-------------------|-----------------|-------------|
| `new_subscription` | `plan` | plan ID | — | — | Create a new subscription to a plan |
| `plan_change` | `subscription` | existing sub ID | `plan` | new plan ID | Upgrade or downgrade an existing subscription |

`plan_change` covers both upgrade and downgrade. Direction is inferred from price comparison; timing is controlled by `intent_params.effective`.

**`intent_params` for `plan_change`:**
```json
{
  "effective":           "immediate",          // "immediate" | "period_end" (default: immediate)
  "proration_behavior":  "create_prorations"   // "create_prorations" | "none" | "always_invoice"
}
```

### v2 Intents (deferred — schema already supports them via entity_type/entity_id generics)

| Intent | entity_type | target_entity_type | Description |
|--------|------------|-------------------|-------------|
| `trial_activation` | `plan` | — | Start a free or paid trial |
| `trial_to_paid` | `subscription` | — | Convert trial to paid |
| `subscription_reactivation` | `subscription` | `plan` | Reactivate a cancelled subscription |
| `addon_purchase` | `subscription` | `addon` | Add an addon |
| `addon_removal` | `subscription` | `addon` | Remove an addon |
| `quantity_change` | `subscription` | — | Change seat count / quantity |
| `payment_collection` | `invoice` | — | Collect on outstanding invoice |
| `payment_method_update` | `customer` | — | Update card on file |
| `one_time_charge` | `customer` | — | Ad-hoc charge |
| `credit_purchase` | `wallet` | — | Top up prepaid credits |

---

## 6. Customer Use Cases — v1

**Case 1 — New subscription, card on file, charge immediately**
```
POST /checkout
  intent:            new_subscription
  entity_type:       plan
  entity_id:         plan_starter
  collection_method: charge_automatically
  checkout_mode:     none           ← default: charge on file, no redirect
  payment_behavior:  pending_if_incomplete

→ Sub created in DRAFT
→ Invoice finalized (not paid yet)
→ Stripe/FlexpriceNative attempts charge on saved card
→ Success:  payment_action.type = "charge" (no URL)
            session → COMPLETED immediately
            DRAFT sub → ACTIVE
→ Failure:  session stays PENDING, expiry timer running (24h default)
            DRAFT sub stays DRAFT; discarded on expiry
```

**Case 2 — New subscription, no card on file, Stripe hosted checkout**
```
POST /checkout
  intent:            new_subscription
  entity_type:       plan
  entity_id:         plan_pro
  collection_method: charge_automatically
  checkout_mode:     hosted
  gateway:           stripe
  payment_behavior:  pending_if_incomplete
  success_url:       https://app.com/success
  cancel_url:        https://app.com/cancel

→ Sub created in DRAFT
→ Invoice finalized
→ Stripe Checkout Session created synchronously → URL returned immediately
→ payment_action: { type: "redirect", url: "https://checkout.stripe.com/cs_live_..." }
→ Customer completes payment on Stripe page
→ Stripe fires: checkout.session.completed
→ entity_mappings lookup (stripe, cs_stripe_xxx) → our checkout session id
→ Temporal: ApplyCheckoutSessionWorkflow → ActivateDraftSubscription → DRAFT sub → ACTIVE
→ session → COMPLETED
```

**Case 3 — New subscription, Paddle hosted checkout**
```
POST /checkout
  intent:            new_subscription
  entity_type:       plan
  entity_id:         plan_pro
  collection_method: charge_automatically
  checkout_mode:     hosted
  gateway:           paddle
  success_url:       https://app.com/success
  cancel_url:        https://app.com/cancel

→ Identical flow to Case 2; only the provider differs
→ PaddleCheckoutProvider.CreateSession() → Paddle Checkout URL
→ Paddle fires: transaction.completed
→ Same Temporal apply flow
→ payment_action: { type: "redirect", url: "https://buy.paddle.com/...", gateway: "paddle" }
```

**Case 4 — New subscription, no integration (Flexprice-native hosted)**
```
POST /checkout
  intent:            new_subscription
  entity_type:       plan
  entity_id:         plan_basic
  collection_method: charge_automatically
  checkout_mode:     hosted
  (no gateway; no connection configured on customer or tenant)

→ Gateway resolved to: flexprice (FlexpriceNativeProvider)
→ Flexprice generates signed hosted payment page URL
→ payment_action: { type: "redirect", url: "https://pay.flexprice.io/cs_xxx" }
→ Customer pays on Flexprice-hosted page → same webhook apply flow
```

**Case 5 — New subscription, B2B send invoice (net30)**
```
POST /checkout
  intent:              new_subscription
  entity_type:         plan
  entity_id:           plan_enterprise
  collection_method:   send_invoice
  checkout_mode:       none
  payment_behavior:    pending_if_incomplete
  expires_in_hours:    720     (30 days)

→ Sub created in DRAFT
→ Invoice finalized and emailed to customer
→ payment_action: { type: "invoice", url: "https://flexprice.io/invoices/inv_xxx/pay" }
→ Customer pays via invoice link within 30 days
→ Invoice payment webhook → session → COMPLETED → DRAFT sub → ACTIVE
→ After 30 days with no payment: session → EXPIRED, invoice voided, DRAFT sub discarded
```

**Case 6 — Plan change (upgrade), pending until paid (no orphan)**
```
POST /checkout
  intent:              plan_change
  entity_type:         subscription
  entity_id:           sub_123          (currently ACTIVE on plan_starter)
  target_entity_type:  plan
  target_entity_id:    plan_pro
  intent_params:       { effective: "immediate", proration_behavior: "create_prorations" }
  collection_method:   charge_automatically
  checkout_mode:       none
  payment_behavior:    pending_if_incomplete
  expires_in_hours:    24

→ Old sub stays ACTIVE on plan_starter — untouched
→ Proration invoice created in DRAFT for the upgrade delta
→ Charge attempted on saved card
→ Success:  session → COMPLETED
            Temporal: ExecuteSubscriptionChangeInternal(sub_123, plan_pro)
            Old sub cancelled, new plan activated atomically
→ Failure:  session stays PENDING, old sub still ACTIVE
→ expires_at reached: session → EXPIRED, invoice voided, old plan continues silently
```

**Case 7 — Plan change (upgrade), Stripe hosted checkout**
```
POST /checkout
  intent:              plan_change
  entity_type:         subscription
  entity_id:           sub_123
  target_entity_type:  plan
  target_entity_id:    plan_pro
  intent_params:       { effective: "immediate", proration_behavior: "create_prorations" }
  collection_method:   charge_automatically
  checkout_mode:       hosted
  gateway:             stripe
  payment_behavior:    pending_if_incomplete
  success_url:         https://app.com/success
  cancel_url:          https://app.com/cancel

→ Old sub stays ACTIVE on plan_starter
→ Proration invoice created
→ Stripe Checkout Session created synchronously
→ payment_action: { type: "redirect", url: "https://checkout.stripe.com/..." }
→ Customer pays on Stripe page
→ Stripe webhook → Temporal apply → old sub cancelled, plan_pro activated
→ Customer redirected to success_url
→ If customer hits cancel_url: DELETE /checkout/:id → session CANCELLED, old plan continues
```

**Case 8 — Plan change (downgrade), at period end, no payment needed**
```
POST /checkout
  intent:              plan_change
  entity_type:         subscription
  entity_id:           sub_123
  target_entity_type:  plan
  target_entity_id:    plan_starter
  intent_params:       { effective: "period_end", proration_behavior: "none" }
  collection_method:   charge_automatically
  checkout_mode:       none
  payment_behavior:    allow_incomplete   ← downgrade: apply regardless

→ No proration charge (downgrade at period end)
→ payment_action: { type: "none" }
→ Session → COMPLETED immediately (no payment required)
→ Temporal: ScheduleSubscriptionChangeAtPeriodEnd(sub_123, plan_starter)
→ At period end: plan_starter activates via existing schedule execution
```

**Case 9 — Plan change (upgrade), B2B invoice-based**
```
POST /checkout
  intent:              plan_change
  entity_type:         subscription
  entity_id:           sub_ent_456
  target_entity_type:  plan
  target_entity_id:    plan_enterprise
  intent_params:       { effective: "immediate", proration_behavior: "always_invoice" }
  collection_method:   send_invoice
  checkout_mode:       none
  payment_behavior:    pending_if_incomplete
  expires_in_hours:    720   (30 days)

→ Old sub stays ACTIVE
→ Proration invoice finalized and emailed
→ payment_action: { type: "invoice", url: "https://flexprice.io/invoices/inv_xxx/pay" }
→ Customer pays via invoice link
→ Invoice webhook → session COMPLETED → upgrade applied
→ After 30 days: session EXPIRED, invoice voided, old plan continues
```

---

## 7. DB Schema

```sql
CREATE TABLE checkout_sessions (

  -- ── Identity ─────────────────────────────────────────────────────────────
  id                         VARCHAR        NOT NULL,
  tenant_id                  VARCHAR        NOT NULL,
  environment_id             VARCHAR        NOT NULL,

  idempotency_key            VARCHAR,
  -- Caller-supplied dedup key (unique per tenant+env).
  -- Same key returns the existing session rather than creating a new one.
  -- Protects against network-retry duplicates.

  -- ── Intent & Lifecycle ───────────────────────────────────────────────────
  intent                     VARCHAR        NOT NULL,
  -- v1 values:  new_subscription | plan_change
  -- v2 values:  trial_activation | trial_to_paid | subscription_reactivation |
  --             addon_purchase | addon_removal | quantity_change |
  --             payment_collection | payment_method_update |
  --             one_time_charge | credit_purchase

  status                     VARCHAR        NOT NULL DEFAULT 'pending',
  -- pending    : created, awaiting payment
  -- processing : payment webhook received; Temporal apply in-flight (idempotency guard,
  --              prevents duplicate webhook deliveries from double-applying)
  -- completed  : payment received and change applied successfully
  -- expired    : expires_at passed without payment; rollback complete, old state intact
  -- cancelled  : explicitly cancelled via DELETE /checkout/:id; rollback complete
  -- failed     : terminal failure during the Temporal apply phase

  -- ── Subject ───────────────────────────────────────────────────────────────
  customer_id                VARCHAR        NOT NULL,

  entity_type                VARCHAR        NOT NULL,
  -- What is being acted on.
  -- v1 values: plan (for new_subscription) | subscription (for plan_change)
  -- v2 values: invoice | addon | wallet | customer

  entity_id                  VARCHAR        NOT NULL,
  -- ID of the entity above.

  target_entity_type         VARCHAR,
  -- What it is being changed to. Null for intents with no target.
  -- v1 values: plan (for plan_change) | null (for new_subscription)

  target_entity_id           VARCHAR,
  -- ID of the target entity above.

  -- ── Intent Parameters ────────────────────────────────────────────────────
  intent_params              JSONB,
  -- Intent-specific overflow. Strongly-typed per intent at the application layer.
  --
  -- plan_change:
  --   { "effective": "immediate" | "period_end",
  --     "proration_behavior": "create_prorations" | "none" | "always_invoice" }
  --
  -- new_subscription:
  --   (currently no params required; reserved for future use e.g. billing_anchor)

  -- ── Invoice ───────────────────────────────────────────────────────────────
  checkout_invoice_id        VARCHAR,
  -- The Flexprice invoice created at session open for the amount to be charged.
  -- Status: DRAFT at creation; FINALIZED on completion; VOIDED on expiry/cancel.
  -- For payment_collection intent (v2): this equals entity_id (the existing invoice).
  -- Null for zero-amount sessions (e.g. period_end downgrade with no proration).

  -- ── Payment Config ────────────────────────────────────────────────────────
  collection_method          VARCHAR        NOT NULL DEFAULT 'charge_automatically',
  -- HOW money moves after invoice finalization. Aligned with Stripe / Paddle / Chargebee.
  -- charge_automatically : attempt charge against saved payment method on file
  -- send_invoice         : finalize invoice and email it; customer pays via link

  payment_behavior           VARCHAR        NOT NULL DEFAULT 'pending_if_incomplete',
  -- WHAT to do when immediate charge fails or is deferred.
  -- Aligned with Stripe's payment_behavior parameter (with pending_if_incomplete added).
  --
  -- pending_if_incomplete  : gate the change on payment; old sub/state untouched until paid
  --                          → new_subscription: sub stays DRAFT until session completes
  --                          → plan_change: old sub stays ACTIVE until session completes
  -- allow_incomplete       : apply change regardless of payment outcome; sub → past_due on failure
  -- default_incomplete     : create in INCOMPLETE state without attempting charge
  -- error_if_incomplete    : return HTTP 402 if charge fails; no session created
  -- default_active         : skip charge entirely; create sub as ACTIVE immediately (B2B net terms)

  checkout_mode              VARCHAR        NOT NULL DEFAULT 'none',
  -- HOW the customer interacts with the payment UI. Separate from collection_method.
  -- Modelled after Stripe Checkout (session UI) vs subscription collection_method (mechanics).
  --
  -- none     : no payment UI — charge on file or send invoice directly (default)
  -- hosted   : redirect to provider-hosted payment page (Stripe Checkout, Paddle Checkout)
  -- embedded : mount provider SDK widget in caller's app (Stripe Elements, Moyasar embedded)

  gateway                    VARCHAR,
  -- Snapshot of the resolved payment provider at session creation time.
  -- Values: stripe | paddle | moyasar | razorpay | flexprice | none
  -- Resolution order: request.gateway → customer connection → tenant default → flexprice
  -- Stored as a snapshot so the session record is self-describing even if connections change.

  -- ── Resolved Payment Action ───────────────────────────────────────────────
  -- Populated synchronously before the POST /checkout response is returned.
  -- The caller never needs to poll — URL is always present when applicable.

  payment_action_type        VARCHAR,
  -- redirect : open payment_action_url in browser (checkout_mode=hosted)
  -- embed    : mount SDK widget using payment_action_url/embed_token (checkout_mode=embedded)
  -- charge   : charged on file; no URL (collection_method=charge_automatically, checkout_mode=none)
  -- invoice  : invoice sent; payment_action_url = invoice payment link (collection_method=send_invoice)
  -- none     : no payment required (zero-amount change, period_end downgrade)

  payment_action_url         VARCHAR,
  -- Present for: redirect, embed, invoice types.
  -- Absent for: charge, none types.

  payment_action_embed_token VARCHAR,
  -- Provider SDK initialisation token for embedded flows.
  -- e.g. Stripe Elements: publishable_key + client_secret composite

  payment_action_amount      NUMERIC(20,8),
  payment_action_currency    VARCHAR(3),

  -- ── Redirect Config ───────────────────────────────────────────────────────
  success_url                VARCHAR,
  -- Provider redirects here after successful payment (hosted/embedded flows).
  cancel_url                 VARCHAR,
  -- Provider redirects here if customer abandons (hosted flows).

  -- ── Expiry ────────────────────────────────────────────────────────────────
  expires_at                 TIMESTAMPTZ    NOT NULL,
  -- Default: NOW() + INTERVAL '24 hours'. Configurable via expires_in_hours on request.
  -- B2B send_invoice flows typically use 720h (30 days).
  -- A Temporal timer workflow fires exactly at this timestamp.

  -- ── Resolution Output ────────────────────────────────────────────────────
  result                     JSONB,
  -- Populated when status → completed.
  -- { "subscription_id": "sub_xxx", "invoice_id": "inv_xxx", "applied_at": "2026-05-29T..." }

  -- ── Failure Info ──────────────────────────────────────────────────────────
  failed_at                  TIMESTAMPTZ,
  failure_reason             VARCHAR,  -- human-readable description
  failure_code               VARCHAR,  -- provider error code for debugging/display

  -- ── Timestamps & Audit ────────────────────────────────────────────────────
  completed_at               TIMESTAMPTZ,
  created_by                 VARCHAR,
  metadata                   JSONB,
  created_at                 TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ    NOT NULL DEFAULT NOW(),

  PRIMARY KEY (id)
);

-- ── Indexes ──────────────────────────────────────────────────────────────────

-- Dedup: same idempotency_key within a tenant+env returns the existing session
CREATE UNIQUE INDEX idx_cs_idempotency
  ON checkout_sessions (tenant_id, environment_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- Customer history: "show all checkout sessions for cust_xxx"
CREATE INDEX idx_cs_customer
  ON checkout_sessions (tenant_id, environment_id, customer_id);

-- Entity lookup: "does sub_123 have a pending upgrade session?"
CREATE INDEX idx_cs_entity
  ON checkout_sessions (entity_type, entity_id);

-- Target lookup: "any pending sessions targeting plan_pro?"
CREATE INDEX idx_cs_target_entity
  ON checkout_sessions (target_entity_type, target_entity_id)
  WHERE target_entity_id IS NOT NULL;

-- Expiry sweep: Temporal polls or cron for pending sessions past expires_at
CREATE INDEX idx_cs_expiry_sweep
  ON checkout_sessions (expires_at)
  WHERE status = 'pending';

-- Invoice linkage: "which session owns inv_xxx?"
CREATE INDEX idx_cs_invoice
  ON checkout_sessions (checkout_invoice_id)
  WHERE checkout_invoice_id IS NOT NULL;

-- Provider session ID → our session ID lookup lives in entity_integration_mappings, NOT here.
-- entity_type='checkout_session', entity_id=our cs_xxx, provider='stripe', provider_id='cs_stripe_xxx'
-- Webhook router: SELECT entity_id FROM entity_integration_mappings
--                 WHERE provider='stripe' AND provider_id='cs_stripe_xxx'
--                   AND entity_type='checkout_session'
```

---

## 8. Domain Model (Go Types)

```go
// ── Enums ─────────────────────────────────────────────────────────────────────

// CheckoutIntent — what action is being taken
type CheckoutIntent string

const (
    // v1
    CheckoutIntentNewSubscription CheckoutIntent = "new_subscription"
    CheckoutIntentPlanChange      CheckoutIntent = "plan_change"

    // v2 (defined for completeness; not implemented in v1)
    CheckoutIntentTrialActivation          CheckoutIntent = "trial_activation"
    CheckoutIntentTrialToPaid              CheckoutIntent = "trial_to_paid"
    CheckoutIntentSubscriptionReactivation CheckoutIntent = "subscription_reactivation"
    CheckoutIntentAddonPurchase            CheckoutIntent = "addon_purchase"
    CheckoutIntentAddonRemoval             CheckoutIntent = "addon_removal"
    CheckoutIntentQuantityChange           CheckoutIntent = "quantity_change"
    CheckoutIntentPaymentCollection        CheckoutIntent = "payment_collection"
    CheckoutIntentPaymentMethodUpdate      CheckoutIntent = "payment_method_update"
    CheckoutIntentOneTimeCharge            CheckoutIntent = "one_time_charge"
    CheckoutIntentCreditPurchase           CheckoutIntent = "credit_purchase"
)

// CheckoutSessionStatus — lifecycle state machine
type CheckoutSessionStatus string

const (
    CheckoutSessionStatusPending    CheckoutSessionStatus = "pending"
    CheckoutSessionStatusProcessing CheckoutSessionStatus = "processing" // idempotency guard during apply
    CheckoutSessionStatusCompleted  CheckoutSessionStatus = "completed"
    CheckoutSessionStatusExpired    CheckoutSessionStatus = "expired"
    CheckoutSessionStatusCancelled  CheckoutSessionStatus = "cancelled"
    CheckoutSessionStatusFailed     CheckoutSessionStatus = "failed"
)

// CheckoutEntityType — type of the primary entity being acted on
type CheckoutEntityType string

const (
    CheckoutEntityPlan         CheckoutEntityType = "plan"         // new_subscription
    CheckoutEntitySubscription CheckoutEntityType = "subscription" // plan_change + v2 intents
    CheckoutEntityInvoice      CheckoutEntityType = "invoice"      // v2: payment_collection
    CheckoutEntityAddon        CheckoutEntityType = "addon"        // v2: addon_*
    CheckoutEntityWallet       CheckoutEntityType = "wallet"       // v2: credit_purchase
    CheckoutEntityCustomer     CheckoutEntityType = "customer"     // v2: payment_method_update
)

// CheckoutMode — how the customer interacts with the payment UI (session-level concern)
// Separate from CollectionMethod (payment mechanics, subscription-level concern).
type CheckoutMode string

const (
    CheckoutModeNone     CheckoutMode = "none"     // default: charge on file or send invoice
    CheckoutModeHosted   CheckoutMode = "hosted"   // redirect to provider-hosted page
    CheckoutModeEmbedded CheckoutMode = "embedded" // mount provider widget in caller's app
)

// PaymentActionType — what the caller should do with the response
type PaymentActionType string

const (
    PaymentActionTypeRedirect PaymentActionType = "redirect" // open payment_action_url
    PaymentActionTypeEmbed    PaymentActionType = "embed"    // mount SDK widget
    PaymentActionTypeCharge   PaymentActionType = "charge"   // charged on file, no URL
    PaymentActionTypeInvoice  PaymentActionType = "invoice"  // invoice sent, URL = payment link
    PaymentActionTypeNone     PaymentActionType = "none"     // no payment required
)

// PaymentBehavior — ADD to existing types.PaymentBehavior in internal/types/subscription.go
// PaymentBehaviorPendingIfIncomplete PaymentBehavior = "pending_if_incomplete"
// (all other existing values remain unchanged)

// ── Domain Entity ─────────────────────────────────────────────────────────────

type CheckoutSession struct {
    ID              string
    TenantID        string
    EnvironmentID   string
    IdempotencyKey  *string

    Intent          CheckoutIntent
    Status          CheckoutSessionStatus

    CustomerID      string
    EntityType      CheckoutEntityType
    EntityID        string
    TargetEntityType *CheckoutEntityType
    TargetEntityID   *string

    IntentParams        map[string]interface{} // validated per-intent at service layer
    CheckoutInvoiceID   *string

    CollectionMethod    types.CollectionMethod  // charge_automatically | send_invoice
    PaymentBehavior     types.PaymentBehavior   // + pending_if_incomplete (new)
    CheckoutMode        CheckoutMode            // none | hosted | embedded
    Gateway             *types.PaymentGateway

    PaymentAction   *CheckoutPaymentAction
    SuccessURL      *string
    CancelURL       *string

    ExpiresAt       time.Time
    CompletedAt     *time.Time
    FailedAt        *time.Time
    FailureReason   *string
    FailureCode     *string
    Result          *CheckoutResult

    Metadata        map[string]interface{}
    CreatedBy       string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type CheckoutPaymentAction struct {
    Type        PaymentActionType
    URL         *string          // nil for charge/none
    EmbedToken  *string          // for embedded flows
    Amount      decimal.Decimal
    Currency    string
    Gateway     *types.PaymentGateway
}

type CheckoutResult struct {
    SubscriptionID *string   `json:"subscription_id,omitempty"`
    InvoiceID      *string   `json:"invoice_id,omitempty"`
    AppliedAt      time.Time `json:"applied_at"`
}

// ── Intent Params (strongly typed, validated at service layer) ────────────────

type PlanChangeIntentParams struct {
    Effective          string `json:"effective"`           // "immediate" | "period_end"
    ProrationBehavior  string `json:"proration_behavior"`  // "create_prorations" | "none" | "always_invoice"
}
```

---

## 9. Provider Abstraction Layer

All payment providers implement a single `CheckoutProvider` interface. `CheckoutService` calls only this interface — it never imports Stripe or Paddle packages directly.

```go
// CheckoutProvider — one implementation per payment gateway
type CheckoutProvider interface {
    // CreateSession creates a provider-side payment session synchronously.
    // Always returns before the POST /checkout response is sent.
    CreateSession(ctx context.Context, req CreateCheckoutSessionRequest) (*ProviderSessionResult, error)

    // GetSession fetches current state from the provider.
    // Used for reconciliation only — normal flow is webhook-driven.
    GetSession(ctx context.Context, providerSessionID string) (*ProviderSessionResult, error)

    // CancelSession cancels the provider-side session.
    // Called during CheckoutExpiryWorkflow and CancelCheckoutSessionWorkflow.
    CancelSession(ctx context.Context, providerSessionID string) error

    // ParseWebhook normalises a raw provider webhook payload into a unified WebhookEvent.
    // Handles signature verification. Returns ErrUnknownEvent for non-checkout events
    // so the webhook router can fall through to existing handlers.
    ParseWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)
}

type CreateCheckoutSessionRequest struct {
    CustomerID       string
    Amount           decimal.Decimal
    Currency         string
    Description      string
    CollectionMethod types.CollectionMethod
    CheckoutMode     CheckoutMode
    SuccessURL       *string
    CancelURL        *string
    ExpiresAt        time.Time
    Metadata         map[string]string
}

type ProviderSessionResult struct {
    ProviderSessionID string
    URL               *string           // nil for charge/none types
    EmbedToken        *string
    PaymentActionType PaymentActionType
}

type WebhookEvent struct {
    ProviderSessionID string
    Provider          types.PaymentGateway
    Type              WebhookEventType
    // payment_succeeded | payment_failed | session_expired | session_cancelled
    Amount            decimal.Decimal
    Currency          string
    RawPayload        []byte
}

// Provider file locations
// internal/integration/stripe/checkout_provider.go    → StripeCheckoutProvider
// internal/integration/paddle/checkout_provider.go    → PaddleCheckoutProvider
// internal/integration/moyasar/checkout_provider.go   → MoyasarCheckoutProvider
// internal/integration/razorpay/checkout_provider.go  → RazorpayCheckoutProvider
// internal/integration/flexprice/checkout_provider.go → FlexpriceNativeProvider
// internal/integration/noop/checkout_provider.go      → NoopProvider (send_invoice/none)
```

### Gateway Resolution

```
CheckoutService.resolveGateway(req, customer, tenant)
  1. req.Gateway explicitly set            → use it
  2. customer has active payment connection → use customer's configured gateway
  3. tenant has default payment connection  → use tenant default
  4. checkout_mode = none + send_invoice    → NoopProvider (no external call needed)
  5. fallback                               → FlexpriceNativeProvider
```

### Provider Session ID → Checkout Session Lookup (Webhook Routing)

Provider session IDs are stored in the **existing** `entity_integration_mappings` table — not in `checkout_sessions`. This reuses existing infrastructure and keeps the checkout session table provider-agnostic.

```
entity_type = "checkout_session"
entity_id   = "cs_fp_01J..."     ← our ID
provider    = "stripe"
provider_id = "cs_stripe_xxx"   ← Stripe's ID
```

Webhook router flow:
```
POST /v1/webhooks/stripe
  → StripeCheckoutProvider.ParseWebhook(payload, sig)
       → WebhookEvent { provider_id: "cs_stripe_xxx", type: payment_succeeded }
  → entity_mappings.GetByProviderID("stripe", "cs_stripe_xxx", "checkout_session")
       → entity_id: "cs_fp_01J..."
  → CheckoutService.HandlePaymentWebhook("cs_fp_01J...", event)
```

---

## 10. API Surface

### POST /v1/checkout

Creates a checkout session. All v1 subscription payment actions flow through this endpoint.

**Request:**
```json
{
  "intent":              "plan_change",
  "customer_id":         "cust_xxx",
  "entity_type":         "subscription",
  "entity_id":           "sub_xxx",
  "target_entity_type":  "plan",
  "target_entity_id":    "plan_pro",
  "intent_params": {
    "effective":          "immediate",
    "proration_behavior": "create_prorations"
  },
  "collection_method":   "charge_automatically",
  "checkout_mode":       "hosted",
  "payment_behavior":    "pending_if_incomplete",
  "gateway":             "stripe",
  "success_url":         "https://app.com/success",
  "cancel_url":          "https://app.com/cancel",
  "expires_in_hours":    24,
  "idempotency_key":     "upgrade-sub-xxx-to-plan-pro-2026-05-29",
  "metadata":            {}
}
```

**Response (unified — same shape for all providers and intents):**
```json
{
  "checkout_session": {
    "id":                  "cs_01J...",
    "intent":              "plan_change",
    "status":              "pending",
    "customer_id":         "cust_xxx",
    "entity_type":         "subscription",
    "entity_id":           "sub_xxx",
    "target_entity_type":  "plan",
    "target_entity_id":    "plan_pro",
    "collection_method":   "charge_automatically",
    "checkout_mode":       "hosted",
    "payment_behavior":    "pending_if_incomplete",
    "payment_action": {
      "type":     "redirect",
      "url":      "https://checkout.stripe.com/pay/cs_live_...",
      "amount":   4900,
      "currency": "USD",
      "gateway":  "stripe"
    },
    "success_url":  "https://app.com/success",
    "cancel_url":   "https://app.com/cancel",
    "expires_at":   "2026-05-30T12:00:00Z",
    "created_at":   "2026-05-29T12:00:00Z"
  },
  "subscription": { "...current subscription (old plan, still active)..." },
  "invoice":       { "...pending proration invoice in DRAFT..." }
}
```

### GET /v1/checkout/:id

Retrieve a checkout session. Returns the same shape as POST.  
Use for polling status after a webhook-driven completion (e.g. polling from `success_url` page).

### DELETE /v1/checkout/:id

Cancel a pending session. Voids the invoice, cancels the provider-side session, marks CANCELLED.  
Old subscription is unaffected. Returns 409 if session is not in `pending` status.

### POST /v1/webhooks/:gateway

One endpoint per payment provider. Routes all provider events to `CheckoutService`.

```
POST /v1/webhooks/stripe
POST /v1/webhooks/paddle
POST /v1/webhooks/moyasar
POST /v1/webhooks/razorpay
```

Each calls `provider.ParseWebhook()` → if it returns `ErrUnknownEvent` (non-checkout event), falls through to existing provider-specific webhook handlers (renewal, dunning, etc.) so existing flows are unaffected.

---

## 11. Session State Machine

```
                 ┌──────────────────────────────────────────┐
                 │               PENDING                     │
                 │  Old sub untouched. Invoice in DRAFT.     │
                 │  Temporal expiry timer running.           │
                 └──────┬─────────────────────┬─────────────┘
                        │                     │
          payment webhook fires          expires_at reached
          (before expires_at)            OR explicit cancel
                        │                     │
                        ▼                     ▼
               ┌──────────────┐      ┌──────────────────────┐
               │  PROCESSING  │      │  EXPIRED / CANCELLED │
               │              │      │                      │
               │  CAS guard:  │      │  Invoice → VOIDED    │
               │  prevents    │      │  Provider session    │
               │  duplicate   │      │    cancelled         │
               │  webhooks    │      │  Old sub intact,     │
               └──────┬───────┘      │  silently continues  │
                      │              └──────────────────────┘
              apply succeeds / fails
                      │
              ┌───────┴────────┐
              ▼                ▼
         COMPLETED           FAILED
         result JSONB set    failure_reason set
         invoice → PAID      invoice → VOIDED
         change applied      old sub intact
```

**PROCESSING state** is critical:
- Set atomically (CAS `WHERE status='pending'`) when the first payment webhook is received
- Any duplicate webhook delivery for the same session sees `status != pending` and exits early
- Temporal's `ApplyCheckoutSessionWorkflow` checks this before any apply activity runs

---

## 12. Temporal Workflows

### CheckoutExpiryWorkflow

Started at session creation. Uses a single Temporal timer — fires exactly once at `expires_at`. Not a cron sweep.

```
Workflow ID (deterministic): "checkout_expiry_{session_id}"

Activities:
  1. CheckSessionStatusActivity(session_id)
     → if status != pending: exit (already completed or cancelled — idempotent guard)
  2. VoidInvoiceActivity(checkout_invoice_id)
  3. ProviderCancelSessionActivity(gateway, provider_session_id via entity_mappings)
  4. UpdateCheckoutSessionStatusActivity(session_id, EXPIRED)

Cancellation:
  ApplyCheckoutSessionWorkflow sends RequestCancellation to
  "checkout_expiry_{session_id}" on payment receipt.
  Temporal cancels the timer gracefully.
```

### ApplyCheckoutSessionWorkflow

Triggered by `CheckoutService.HandlePaymentWebhook()`.

```
Workflow ID: "checkout_apply_{session_id}"

Activities:
  1. AtomicSetProcessingActivity(session_id)
     CAS update: WHERE status='pending' SET status='processing'
     Return early if rows_affected=0 (duplicate webhook guard)

  2. RequestCancellationActivity("checkout_expiry_{session_id}")

  3. Intent-based apply:
     new_subscription →
       ActivateDraftSubscriptionActivity(sub_id)
       sub: DRAFT → ACTIVE

     plan_change, effective=immediate →
       ExecuteSubscriptionChangeInternalActivity(old_sub_id, new_plan_id, intent_params)
       old sub: CANCELLED, new plan: ACTIVE (atomic)

     plan_change, effective=period_end →
       ScheduleSubscriptionChangeAtPeriodEndActivity(sub_id, new_plan_id)
       existing schedule execution handles the actual switch at period end

  4. FinalizeInvoiceActivity(checkout_invoice_id)
     invoice: DRAFT → FINALIZED → PAID

  5. RecordResultActivity(session_id, { subscription_id, invoice_id, applied_at })
     session: PROCESSING → COMPLETED
```

### CancelCheckoutSessionWorkflow

Triggered by `DELETE /v1/checkout/:id`.

```
Activities:
  1. AtomicSetCancellingActivity(session_id)  (guard: only if pending)
  2. RequestCancellationActivity("checkout_expiry_{session_id}")
  3. VoidInvoiceActivity(checkout_invoice_id)
  4. ProviderCancelSessionActivity(gateway, provider_session_id)
  5. UpdateCheckoutSessionStatusActivity(session_id, CANCELLED)
```

---

## 13. Current System Blockers & Migration Strategy

### Blockers

| # | Blocker | File | Change Required |
|---|---------|------|-----------------|
| 1 | `PaymentBehavior` enum missing `pending_if_incomplete` | `internal/types/subscription.go` | Add `PaymentBehaviorPendingIfIncomplete = "pending_if_incomplete"` to existing const block |
| 2 | `CreateSubscription` immediately activates and charges — no DRAFT-only path | `internal/service/subscription.go` | Add `CreateDraftSubscription()` that creates sub in `DRAFT` without invoicing. Checkout calls this; `ActivateDraftSubscriptionActivity` activates it on payment. |
| 3 | `ProcessDraftInvoice` finalizes AND charges atomically — no "finalize without paying" path | `internal/service/invoice.go:1861` | Add `FinalizeDraftInvoice()` that finalizes to FINALIZED status but makes no payment attempt. Checkout sessions use this path; payment is handled by the provider. |
| 4 | `ExecuteSubscriptionChangeInternal` cancels old sub immediately — no pending window | `internal/service/subscription_change.go:717` | Extract cancel+create into `ApplySubscriptionChange(old_sub_id, new_plan_id, params)`. Existing `/subscriptions/change` endpoint creates a CheckoutSession and returns; Temporal activity calls `ApplySubscriptionChange` only after payment. |
| 5 | No unified webhook router | `internal/api/v1/` | New `POST /v1/webhooks/:gateway` handler → `provider.ParseWebhook()` → `CheckoutService.HandlePaymentWebhook()`. Returns `ErrUnknownEvent` passthrough so existing Stripe/Paddle webhook handlers remain unchanged. |
| 6 | Race: duplicate webhook may apply change twice | none | `PROCESSING` CAS guard in `AtomicSetProcessingActivity`. First webhook wins; all subsequent are no-ops. |
| 7 | No per-session Temporal expiry mechanism | none | `CheckoutExpiryWorkflow` with deterministic workflow ID. Cancelled by `ApplyCheckoutSessionWorkflow` on payment. |
| 8 | `checkout_session` not a known entity type in `entity_integration_mappings` | `internal/integration/` | Add `"checkout_session"` as a valid entity type constant. |

### Migration Strategy — Zero Breaking Changes

All changes are purely additive. Nothing existing is modified or removed in v1.

1. New `checkout_sessions` Ent schema + Postgres migration (additive table)
2. Add `PaymentBehaviorPendingIfIncomplete` to existing `PaymentBehavior` enum (additive const)
3. Add `DRAFT` as a valid `SubscriptionStatus` value if not already present (additive const)
4. New `CheckoutService`, `CheckoutProvider` interface, provider adapters — all new files
5. New `POST /v1/checkout`, `GET /v1/checkout/:id`, `DELETE /v1/checkout/:id` (additive routes)
6. New `POST /v1/webhooks/:gateway` (additive route; existing webhook routes unchanged)
7. Existing `POST /v1/subscriptions/change` preserved as-is — callers see no change
   - Internally it will eventually call `CheckoutService.Create()` with `checkout_mode: none`
   - For v1 this internal wiring is not required; the existing code path is kept intact
8. `FinalizeDraftInvoice()` added alongside existing `ProcessDraftInvoice()` — not a replacement

---

## 14. Open Questions (Deferred to Implementation)

1. **`default_active` + `pending_if_incomplete` interaction**: for B2B net-terms new subscriptions, callers typically set `payment_behavior=default_active` (activate without charging). Should checkout sessions with `default_active` complete immediately with `payment_action.type=none`, bypassing the pending window entirely?

2. **Stripe Checkout + pending_update**: Stripe's hosted Checkout cannot be linked to a `pending_update` hash on an existing subscription. For `plan_change` + `checkout_mode=hosted`, we manage the entire pending window ourselves (old sub untouched, change applied via Temporal) rather than delegating to Stripe's native pending_update. This is correct and intentional.

3. **Multi-currency**: if customer's billing currency differs from plan currency, conversion is handled at invoice creation (existing logic). The checkout session amount reflects the invoiced amount in the customer's currency.

4. **B2B send_invoice expiry**: 30-day expiry windows create Temporal timers running for a month. Temporal durable timers handle this correctly (not in-memory). Confirm acceptable at scale before shipping.

5. **`plan_change` + `effective=period_end` + `payment_behavior=pending_if_incomplete`**: for zero-proration downgrades at period end, the session can complete immediately with `payment_action.type=none` since no payment is required. The actual plan switch is a scheduled change, not payment-gated.

---

## 15. Summary

| Dimension | Decision |
|-----------|----------|
| **v1 scope** | `new_subscription` + `plan_change` only |
| **Data model** | `CheckoutSession` as first-class entity; provider IDs in `entity_integration_mappings` |
| **Subject fields** | `entity_type` + `entity_id` + `target_entity_type` + `target_entity_id` + `intent_params` JSONB |
| **Provider coupling** | Zero — `checkout_sessions` table has no provider-specific fields |
| **`collection_method`** | Unchanged from existing enum (`charge_automatically` \| `send_invoice`); Stripe/Paddle-aligned |
| **`checkout_mode`** | New field, session-level only (`none` \| `hosted` \| `embedded`); separates payment UI from payment mechanics |
| **`payment_behavior`** | Existing enum + add `pending_if_incomplete`; all other values unchanged |
| **Gateway resolution** | Request → customer connection → tenant default → FlexpriceNative |
| **URL delivery** | Always synchronous in creation response; no polling ever required |
| **Rollback** | Temporal timer per session (configurable expiry) + explicit cancel API |
| **Auto-apply** | `ApplyCheckoutSessionWorkflow` triggered by normalised webhook event |
| **Backward compat** | All changes additive; existing `/subscriptions/change` untouched in v1 |
| **Orphan problem** | Eliminated — old sub untouched until `COMPLETED`; `EXPIRED`/`CANCELLED` leaves old sub intact |
