# Plan Change v2 — Swap-In-Place — ERD

Status: **Proposed** — v0
Date: 2026-08-11

---

## 1. Scope

Mutate `subscriptions.plan_id` in place instead of cancel-and-recreate. Slice line items at the
effective date, leave the billing anchor alone.

**In v0:** feature parity with today's v1 plan change, delivered by swap, plus **addon dispositions**
(carry / drop) and **attaching addons** at change time — which together express replace.

**Out of v0, structure kept open for each:**


| Deferred                                                                  | Note                                   |
| ------------------------------------------------------------------------- | -------------------------------------- |
| `subscription_associations` table                                         | §7                                     |
| `addendum_config` — coupons / overrides at change time (addons ARE in v0) | §8                                     |
| Dispositions for coupons, tax, credit grants, price/entitlement overrides | all default to CARRY = zero operations |
| Interval / cadence / currency change, hierarchy subs, phases              | 4xx, hint points at v1                 |


---



## 2. API contract

```
POST /v1/subscriptions/{id}/change/v2/preview     @x-scope "read"
POST /v1/subscriptions/{id}/change/v2/execute     @x-scope "write"
```

Preview and execute take the **identical** request type — that is what guarantees quote parity, and
it is what v1 gets wrong (two independent credit-netting implementations; preview ignores usage, tax,
coupons and the cancel invoice).

```go
type SubscriptionChangeV2Request struct {
    TargetPlanID      string                  `json:"target_plan_id" validate:"required"`
    ChangeAt          *types.ScheduleType     `json:"change_at,omitempty"`   // immediate (default) | end_of_period
    ProrationBehavior types.ProrationBehavior `json:"proration_behavior" validate:"required"`

    // What happens to what is already attached. Wrapper kept so new entity types
    // are added without a breaking change; v0 populates exactly one field.
    EntityPolicies *SubscriptionChangeEntityPolicies `json:"entity_policies,omitempty"`

    // What to attach as part of the change. Same wrapper reasoning.
    AddendumConfig *SubscriptionAddendumConfig `json:"addendum_config,omitempty"`

    Checkout       *CheckoutParams   `json:"checkout,omitempty"`
    IdempotencyKey *string           `json:"idempotency_key,omitempty"`
    Metadata       map[string]string `json:"metadata,omitempty"`
}

// SubscriptionChangeEntityPolicies is the extension point. Each entity type gets
// one field of the same shape, so adding coupons or tax later is additive.
type SubscriptionChangeEntityPolicies struct {
    Addons *EntityChangePolicy `json:"addons,omitempty"`
    // v1+, all *EntityChangePolicy:
    //   Coupons, TaxAssociations, CreditGrants, PriceOverrides, EntitlementOverrides
}

// SubscriptionChangeConfig is the subscription's configuration on the new plan.
// Field names, JSON tags and types mirror SubscriptionCreationConfig so the payload
// looks identical to a caller — but it is a DISTINCT type, not that one reused:
//
//   - Update semantics need tri-state. SubscriptionCreationConfig.EnableTrueUp is a
//     plain bool, where absent and false are the same thing. Correct at create;
//     on a change it would silently disable true-up on every request that omits it.
//     Anything settable here must be a pointer.
//   - Phases is a blocked precondition, and Coupons / LineItemCoupons are deprecated;
//     neither belongs on a new endpoint.
//   - Sharing the type would put all 16 creation fields in the OpenAPI schema while
//     most 400, and would leak future creation-only fields into the generated SDKs
//     without anyone deciding they work here.
//
// Growth is deliberate: copy a field over when it is implemented.
type SubscriptionAddendumConfig struct {
    Addons []AddAddonToSubscriptionRequest `json:"addons,omitempty" validate:"omitempty,dive"`
    // v1+, pointer-typed where "leave unchanged" must be expressible:
    //   SubscriptionCoupons, OverrideLineItems, OverrideEntitlements, CreditGrants,
    //   TaxRateOverrides, LineItemCommitments, CommitmentAmount, OverageFactor,
    //   EnableTrueUp *bool
}

// EntityChangePolicy declares what happens to one entity type's existing
// attachments. Generic on purpose — the same shape serves every entity.
type EntityChangePolicy struct {
    // Applied to every active attachment of this entity type.
    // Empty means the entity's built-in default, which is "carry" for everything in v0.
    Default types.EntityDisposition `json:"default,omitempty"`

    // Per-instance override, keyed by the INSTANCE id for that entity type —
    // addon_associations.id, coupon_associations.id, credit_grants.id — never the
    // catalogue id, because one subscription can hold two attachments of one addon (§5).
    Overrides map[string]types.EntityDisposition `json:"overrides,omitempty"`
}
```

`EntityDisposition` is an enum in `internal/types/`, alongside the other change enums:

```go
// EntityDisposition is the shared vocabulary for what a plan change does to an
// existing attachment. Not every value is legal for every entity type — each policy
// field validates against its own allowed set via ValidateFor, and the error names
// the allowed values, so an unsupported combination is a 4xx, not a silent no-op.
type EntityDisposition string

const (
    // Leave the attachment untouched. Zero operations. Built-in default for everything in v0.
    EntityDispositionCarry EntityDisposition = "carry"
    // Close the attachment at the change's effective_at, settling money per the
    // change's proration_behavior.
    EntityDispositionDrop EntityDisposition = "drop"
    // v1+: "rederive" — close and recreate from the target plan. Takes no parameter;
    // there are only two plans in this API. Meaningless until plan-bundled addons exist.
)

var EntityDispositionValues = []EntityDisposition{
    EntityDispositionCarry,
    EntityDispositionDrop,
}

func (d EntityDisposition) String() string { return string(d) }

// ValidateFor checks d against the subset an entity type accepts.
// v0: addon -> {carry, drop}.
func (d EntityDisposition) ValidateFor(e SubscriptionChangeEntityType, allowed []EntityDisposition) error
```

```json
"entity_policies": { "addons": { "overrides": { "addon_assoc_A": "drop" } } }
```



### Response

```go
type SubscriptionChangeV2Response struct {
    Subscription     *SubscriptionResponse `json:"subscription"`
    ChangedResources ChangedResources      `json:"changed_resources"`  // reused as-is

    ChangeType  types.SubscriptionChangeType `json:"change_type"`
    EffectiveAt time.Time                    `json:"effective_at"`
    FromPlan    PlanSummary                  `json:"from_plan"`        // existing type
    ToPlan      PlanSummary                  `json:"to_plan"`

    // One entry per active attachment the change touched or considered, any entity
    // type. v0 emits addon rows only. Same shape as the request policy, so a caller
    // can round-trip a preview result back as explicit overrides.
    EntityDispositions []EntityDispositionResult `json:"entity_dispositions,omitempty"`

    IsScheduled bool       `json:"is_scheduled"`
    ScheduleID  *string    `json:"schedule_id,omitempty"`
    ScheduledAt *time.Time `json:"scheduled_at,omitempty"`

    CheckoutSession *CheckoutSessionResponse `json:"checkout_session,omitempty"`
    Warnings        []string                 `json:"warnings,omitempty"`
    Metadata        map[string]string        `json:"metadata,omitempty"`
}

type EntityDispositionResult struct {
    EntityType types.SubscriptionChangeEntityType `json:"entity_type"`
    // The instance id — the key an Overrides entry would use.
    ReferenceID string `json:"reference_id"`
    // The catalogue id: addon_id, coupon_id, …
    EntityID string `json:"entity_id"`
    // The resolved value — identical to what an Overrides entry accepts, so a
    // preview result round-trips back as an explicit override unchanged.
    Disposition types.EntityDisposition `json:"disposition"`
    Reason      types.DispositionReason `json:"reason"`
    // Free text, set only when Reason alone is not self-explanatory.
    Detail string `json:"detail,omitempty"`
}
```

The two enums live in `internal/types/`, next to the existing `SubscriptionChangeType`:

```go
// SubscriptionChangeEntityType names an entity kind a plan change can act on.
// Singular — one result row describes one attachment. It corresponds to the
// same-named plural field on SubscriptionChangeEntityPolicies: addon -> Addons.
type SubscriptionChangeEntityType string

const (
    SubscriptionChangeEntityTypeAddon SubscriptionChangeEntityType = "addon"
    // v1+: coupon, tax_association, credit_grant, price_override, entitlement_override
)

var SubscriptionChangeEntityTypeValues = []SubscriptionChangeEntityType{
    SubscriptionChangeEntityTypeAddon,
}

func (e SubscriptionChangeEntityType) String() string { return string(e) }
func (e SubscriptionChangeEntityType) Validate() error { /* mirrors SubscriptionChangeType.Validate */ }

// DispositionReason explains why a disposition was chosen, so a preview is
// auditable without the caller re-deriving the precedence rules.
type DispositionReason string

const (
    // No per-instance override matched; the effective default applied. Covers both
    // the built-in default and a caller-supplied entity_policies.<entity>.default —
    // the two are not split, because the caller can see which they sent and the
    // resolved Disposition is echoed back either way.
    DispositionReasonDefault DispositionReason = "default"
    // entity_policies.<entity>.overrides[<reference_id>] named this attachment.
    DispositionReasonExplicitOverride DispositionReason = "explicit_override"
    // The server overrode policy. The only reason a consumer cannot derive from
    // its own request, so it is the one that has to be on the wire. Detail says why.
    DispositionReasonForced DispositionReason = "forced"
)

var DispositionReasonValues = []DispositionReason{
    DispositionReasonDefault,
    DispositionReasonExplicitOverride,
    DispositionReasonForced,
}
```

**Line-item continuity is reported through** `ChangedLineItem`**, not a parallel array.** That type
already exists and is already returned for every line item created or ended
([dto/subscription_modification.go:409](../../internal/api/dto/subscription_modification.go#L409));
it simply does not say which `created` entry succeeds which `ended` one. One additive optional field
fixes that:

```go
type ChangedLineItem struct {
    ID           string                `json:"id"`
    PriceID      string                `json:"price_id"`
    Quantity     decimal.Decimal       `json:"quantity" swaggertype:"string"`
    StartDate    *time.Time            `json:"start_date,omitempty"`
    EndDate      *time.Time            `json:"end_date,omitempty"`
    ChangeAction ChangedLineItemAction `json:"change_action" enums:"created,updated,ended"`

    // NEW. Two entries in the SAME response sharing a non-empty line_key — one
    // "ended", one "created" — are the same service continuing. An "ended" entry
    // whose key matches no "created" entry is a service that stopped; a "created"
    // entry matching no "ended" one is new. Unpaired is normal, not an error.
    //
    // The value is the line key defined in §6 — the same function the engine uses
    // to resolve successors, so the response cannot disagree with what was billed.
    // A plan change never emits "updated".
    LineKey string `json:"line_key,omitempty"`
}
```



### Preconditions — 4xx before any write

Interval / cadence / period-count / billing-cycle mismatch · currency mismatch ·
`subscription_type ∈ {parent, grouped_invoicing, inherited}` · subscription has phases ·
`pause_status ∈ {active, scheduled}` · pending cancellation or plan_change schedule ·
pending checkout session · `subscription_status ∉ {active, trialing}` ·
`target_plan_id == subscription.plan_id`. Every hint names the v1 endpoint as the fallback.

**v0 is not a superset of v1.** v1 requires `billing_cadence`, `billing_period`,
`billing_period_count` and `billing_cycle` and can change all four, because it recreates the
subscription ([subscription_change.go:29-38](../../internal/api/dto/subscription_change.go#L29)); v2
4xxs on any of them. v1 also carries the internal-only `OpeningInvoiceAdjustmentAmount`, which the
Stripe inbound path uses and v2 has no equivalent for. So v1 stays supported and callable — it is the
documented fallback — and it must **not** be marked deprecated in Swagger until interval change
lands. One behaviour difference to expect: for anniversary billing v1 deliberately moves the anchor
to the effective date to avoid a short first period ([subscription_change.go:855](../../internal/ee/service/subscription_change.go#L855));
v2 always keeps the anchor, which is the point of swap-in-place, but it means the same operation
produces different invoice dates than v1 did.

---



## 3. Dispositions

> **Everything keyed on** `subscription_id` **carries, because the row survives. v0 rederives exactly one
> thing — plan-derived line items — and makes exactly one thing configurable — addons.**

**UNTOUCHED, zero operations.** `invoices`, `invoice_line_items`, `credit_notes`, `payments`,
`wallets`, `wallet_transactions`, `coupon_applications`, `tax_applieds`, `usage_records`, ClickHouse
`events_processed` / `feature_usage` / `usage_benchmark`, `entity_integration_mappings`,
`workflow_executions`, `system_events`, `alert_logs`, `parent_subscription_id` and children.
`billing_sequences` in particular — unique on `(tenant_id, subscription_id)` — so the cycle counter
no longer restarts at 1.

**CARRY, not configurable in v0.** `coupon_associations`, `tax_associations`, plan-sourced and custom
`credit_grants`, `entitlement_grants` **including the usage counter**, subscription-scoped `prices`,
`alert_settings`, `trial_start`/`trial_end`, and the subscription columns v1 silently dropped
(`commitment_*`, `overage_factor`, `enable_true_up`, `payment_behavior`, `collection_method`,
`gateway_payment_method_id`, `payment_terms`, `timezone`, `lookup_key`, `auto_invoice_threshold`,
`invoicing_customer_id`).

Carrying `entitlement_grants` means the target plan's **quota** applies from the next reset, not at
`effective_at`. That is a deliberate v0 choice, not an oversight — see §6.

**REDERIVE.** `subscription_line_items` where `entity_type = 'plan'` — close at `effective_at`, open
successors from the target plan's prices, per the line key in §6. A line whose successor carries an
identical price is left alone rather than closed and reopened.

### Addons

CARRY is zero operations. DROP is the semantics `RemoveAddonFromSubscription` already implements,
with `EffectiveDate = effective_at`:


| Addon shape             | On drop                                                                                                                                                                                                                | Status                                                                      |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Fixed, advance          | Prorated credit for the unused window                                                                                                                                                                                  | works                                                                       |
| Usage, arrear           | **Not prorated.** Closing the line item clips the ClickHouse window; the next regular invoice bills `[period_start, effective_at)`                                                                                     | works — **must be tested**, this is the usage restart silently never billed |
| Fixed, arrear           | *Should* be a prorated charge for the consumed fraction, but `shouldIssueCharge` ([calculator.go:122](../../internal/domain/proration/calculator.go#L122)) is never true for remove actions, so today it bills nothing | **open — confirm and close**                                                |
| Credit grant, ONETIME   | Nothing. Already delivered, expiry frozen. Never clawed back — credits may be spent                                                                                                                                    | works                                                                       |
| Credit grant, RECURRING | `CancelFutureSubscriptionGrants` scoped by `AddonID` ([subscription.go:5643](../../internal/ee/service/subscription.go#L5643)). Already-applied periods are not clawed back                                            | works                                                                       |
| Entitlements            | No write. Addon entitlements are templates (`entity_type = ADDON`, [addon.go:243](../../internal/ee/service/addon.go#L243)) resolved through the active association                                                    | works                                                                       |


Two changes the drop path needs inside a plan change:

1. **One invoice.** Today the credit is a wallet top-up issued *after* the transaction, best-effort — [subscription.go:5672](../../internal/ee/service/subscription.go#L5672) logs *"removal was persisted and the credit is UNISSUED"*. Use `LineItemProrationService.Compute`, not `Apply`, and fold the entries into the plan change's single settlement.
2. **Default to** `effective_at`**.** `RemoveAddonFromSubscription` defaults to `sub.CurrentPeriodEnd`.

**Open — arrear fixed charge on drop.** Adding `ProrationActionRemoveItem` to `shouldIssueCharge` is
not enough: the branch computes `NewPricePerUnit × NewQuantity × coefficient`, and a remove has no
`New*` values, so it would still produce zero. Charging the consumed fraction needs a distinct branch
on `Old* × (1 − coefficient)`. A design task, not a flag.

**Guard — two attachments of one addon.** `entitlement_grants` is UNIQUE on
`(tenant_id, environment_id, entitlement_config_id, customer_id, subscription_id, valid_from)`
([entitlement_grant.go:118](../../ent/schema/entitlement_grant.go#L118)), so two attachments of the
same addon share **one** grant row whenever their windows align — the index only separates them when
`valid_from` differs. Dropping one attachment must therefore
not close the entitlement grant while another active attachment of the same `addon_id` remains. Same
shape as the `credit_grants` case in §5, and the reason `credit_grants.addon_association_id` alone
does not cover it.

---



## 4. ERD

```mermaid
erDiagram
    CUSTOMER  ||--o{ SUBSCRIPTION : owns
    CUSTOMER  ||--o{ WALLET : holds
    PLAN      ||--o{ PRICE : defines
    ADDON     ||--o{ PRICE : defines
    ADDON     ||--o{ ENTITLEMENT : "templates, entity_type=ADDON"

    SUBSCRIPTION }o--|| PLAN : "plan_id MUTATED"
    SUBSCRIPTION ||--o{ SUBSCRIPTION_LINE_ITEM : "REDERIVE plan lines"
    SUBSCRIPTION ||--o{ ADDON_ASSOCIATION : "POLICY carry or drop"
    SUBSCRIPTION ||--o{ CREDIT_GRANT : "follows its addon"
    SUBSCRIPTION ||--o{ ENTITLEMENT_GRANT : "CARRY usage counter"
    SUBSCRIPTION ||--o{ COUPON_ASSOCIATION : CARRY
    SUBSCRIPTION ||--o{ ALERT_SETTINGS : CARRY
    SUBSCRIPTION ||--o{ TAX_ASSOCIATION : CARRY
    SUBSCRIPTION ||--o{ INVOICE : UNTOUCHED
    SUBSCRIPTION ||--|| BILLING_SEQUENCE : "UNTOUCHED, counter continues"
    SUBSCRIPTION ||--o{ SUBSCRIPTION_SCHEDULE : "deferred change lives here"

    SUBSCRIPTION_LINE_ITEM }o--|| PRICE : "price_id, money resolved live"
    SUBSCRIPTION_LINE_ITEM }o--o| ADDON_ASSOCIATION : "addon_association_id"
    ADDON_ASSOCIATION ||--o{ CREDIT_GRANT : "NEW addon_association_id FK"
    CREDIT_GRANT ||--o{ CREDIT_GRANT_APPLICATION : "lazy chain"
    CREDIT_GRANT_APPLICATION ||--o| WALLET_TRANSACTION : "tops up, expiry frozen"
    INVOICE ||--o{ INVOICE_LINE_ITEM : contains
    INVOICE_LINE_ITEM }o--o| SUBSCRIPTION_LINE_ITEM : "debit basis for credit"
    CHECKOUT_SESSION }o--o| SUBSCRIPTION : "pay-first, holds resolved intent"

    SUBSCRIPTION {
        string id PK "SURVIVES, never recreated"
        string plan_id FK "MUTATED, drop Immutable"
        time billing_anchor "UNCHANGED, the metronome"
        time current_period_start "UNCHANGED"
        time current_period_end "UNCHANGED"
        string currency "Immutable, mismatch is a 4xx"
        string billing_period "Immutable until v1"
        time trial_start "CARRY, trial survives"
        time trial_end "CARRY"
        string subscription_type "4xx when hierarchy"
    }

    SUBSCRIPTION_LINE_ITEM {
        string id PK
        string subscription_id FK
        string entity_type "plan REDERIVE, addon follows policy, subscription CARRY"
        string entity_id "plan_id or addon_id"
        string price_id FK "no amount column"
        string addon_association_id FK "which attachment created it"
        decimal quantity
        time start_date "half-open, set to effective_at on open"
        time end_date "set to effective_at on close"
    }

    PRICE {
        string id PK
        string entity_type "PLAN, ADDON, or SUBSCRIPTION override"
        string entity_id
        string lookup_key "line key for FIXED, see section 6"
        string meter_id "line key for USAGE, with filter_values"
        string filter_values "line key for USAGE, with meter_id"
        string parent_price_id
        decimal amount
    }

    ADDON_ASSOCIATION {
        string id PK "the instance, what overrides are keyed by"
        string entity_id FK "subscription id"
        string entity_type "always subscription today"
        string addon_id FK "the catalogue id, NOT unique per sub"
        decimal quantity "NEW, today only a stale metadata string"
        string addon_status "active, pending, cancelled"
        time start_date
        time end_date "set to effective_at on drop"
    }

    CREDIT_GRANT {
        string id PK
        string subscription_id FK
        string plan_id FK "provenance"
        string addon_id FK "catalogue id, ambiguous across attachments"
        string addon_association_id FK "NEW, resolves the ambiguity"
        time credit_grant_anchor
        time end_date "set on drop, row never mutated otherwise"
    }

    ENTITLEMENT_GRANT {
        string id PK
        string subscription_id FK "CARRY, key does not change"
        string entitlement_config_id FK
        decimal quota
        decimal usage "PRESERVED, closes the reset loophole"
    }

    INVOICE_LINE_ITEM {
        string id PK
        string subscription_line_item_id FK "immutable, the debit-basis join"
        decimal amount "the only historical price record"
        time period_start
        time period_end
    }
```



Three of these arrows are the whole design: `plan_id` mutates, `subscription_line_items` slice, and
everything else keeps pointing at an `id` that did not change.

---



## 5. Addon identity

Quantity and instances are different things and both exist.

**Quantity** — one association, one line item, `quantity = N`, set via `override_line_items[].quantity`
and applied by `ProcessSubscriptionPriceOverrides`
([subscription.go:5103](../../internal/ee/service/subscription.go#L5103)).

**Two associations** — two `addon_associations` rows for one `addon_id`. Not caused by a different
size or price: prices belong to the addon, so "Extra Storage 100 GB" and "500 GB" are two catalogue
addons. The real case is a reversed cancellation, documented in `RemoveAddonFromSubscription`:

```
Oct 1   Priority Support active                    -> addon_assoc_A (end_date = nil)
Oct 10  cancelled, effective period end            -> addon_assoc_A (end_date = Nov 1)
Oct 15  customer changes their mind, re-attaches   -> addon_assoc_B (end_date = nil)

Oct 15 -> Nov 1: two concurrent associations, same addon_id
```

> "This handles the case where a previous association was cancelled at period-end (EndDate set)
> while a new recurring association was added on top (EndDate zero)."

A plan change landing Oct 20 cannot say which to drop from `addon_id` alone — A is dying Nov 1
anyway, B is the live one. Hence `EntityChangePolicy.Overrides` is keyed by `addon_associations.id`.

Secondary: `Cadence` is a property of the attachment, not the addon
([ent/schema/addon.go](../../ent/schema/addon.go) carries only id / lookup_key / name / description /
metadata), so one addon can be attached both recurring and onetime. Thin in practice.

**Two bugs this exposes, both fixed as columns.** In the same overlap window,
`CancelFutureSubscriptionGrants(subscription_id, addon_id)` cancels the grants of *both*
attachments, so dropping A also kills B's allocation. Narrow — if A was cleanly cancelled its grant
is already end-dated — but real. Fix: `credit_grants.addon_association_id`. Separately,
`createLineItemFromPrice` writes a literal `"addon_quantity": "1"` into line-item metadata
([subscription.go:5743](../../internal/ee/service/subscription.go#L5743)) while the real quantity is
set later by the override path, so association-level quantity is unreliable. Fix:
`addon_associations.quantity`.

---



## 6. Line key, and the v0 decisions that follow from it

Successor resolution and the response's `line_key` are **one function**, so the report cannot
disagree with what was billed. It answers one question: is this target-plan price the same service as
this current-plan price?

```
USAGE    ->  (meter_id, sorted filter_values)
FIXED    ->  lookup_key, when both prices have a non-empty one
otherwise->  price_id — unique per price, so it pairs with nothing
```

`prices.group_id` is deliberately **not** used. It is a catalogue grouping label
([types/group_entity.go](../../internal/types/group_entity.go)) with no billing semantics, and it is
optional — depending on it would turn line-item continuity into a catalogue-hygiene problem.
`feature_id` is not used either: it does not exist on `prices`, and it is too coarse, because two
prices can share a meter and split on `filter_values` (`region=us` vs `region=eu`) and are genuinely
different charges.

Four cases:


| Current line            | Target line | What happens                                                                     |
| ----------------------- | ----------- | -------------------------------------------------------------------------------- |
| paired, identical price | —           | **carried.** `id`, `start_date` and usage window kept; repointed at the target    |
| paired, different price | —           | close at `effective_at`, open successor                                          |
| paired with nothing     | —           | close at `effective_at` (remove)                                                 |
| —                       | unpaired    | open at `effective_at` (add)                                                     |


Row 1 is what makes a lateral change emit no invoice, and it is the only thing stopping an unchanged
service from getting a new line-item id on every plan change.

Carried does not mean untouched. The row's billing is unchanged — that is what "identical" is
tested for — but everything naming its owner moves to the target plan: `entity_id`,
`plan_display_name`, and `price_id`. A row left on the old plan's price would keep billing off a
plan the subscription has left: editing the target plan's price would not reach it, archiving the
source plan's price would terminate it, and plan-price sync would never notice either, because the
change re-anchors `synced_price_sequence` to the target. Carried rows are reported in
`changed_resources.line_items` with `change_action = "updated"`.

"Identical" is decided by `billsIdentically`, which is deliberately conservative: usage prices and
tier ladders never match, and package size (`transform_quantity`) and pricing unit must be equal.
Anything it cannot compare completely falls through to close-and-open, which is safe.

### v0 decisions

**Settlement nets — one invoice per change.** Charges and credits are summed across *all* entries;
credits become negative invoice lines; nothing is raised when the net is zero and no entry moved.
Today `Compute` buckets each entry into charge *or* credit and `Apply` settles the two independently
([line_item_proration.go:124](../../internal/ee/service/line_item_proration.go#L124)), which is why a
lateral change currently produces an invoice **and** a wallet credit that cancel. This supersedes the
open decision in §8.

**Usage tier ladders restart when a line splits — accepted in v0.** `CalculateCost(price, quantity)`
([price.go:1073](../../internal/ee/service/price.go#L1073)) applies the ladder to one window's
quantity, so a mid-period split bills the first tier twice and the customer pays *more* for having
changed plan. Row 1 above avoids this whenever the price is identical across plans, which is the
common case. Where the price genuinely differs, v0 accepts the restart and test 26 asserts it. The
fix is a tier **offset** — bill the successor's own quantity, priced from the predecessor's
cumulative position — and never a quantity bump, which would double-charge. It also needs a separate
policy for VOLUME tier mode, which reprices every unit at a single tier
([price.go:1111](../../internal/ee/service/price.go#L1111)) and so cannot be offset cleanly. Out of v0.

**Entitlement quota does not rederive.** Grants carry with their usage counter, so the target plan's
quota applies from the next reset, not at `effective_at`. Deliberate: it keeps the counter honest and
closes the mid-period reset loophole. Callers needing the new quota immediately use the entitlement
override API.

**Preview parity is arithmetic parity.** Same request, same instant → same numbers. Preview returns
the `effective_at` it used; `immediate` resolves to `now` at each call, and usage accrued between
preview and execute legitimately changes the result.

**Idempotency** reuses `idempotency.Generator` with a new `ScopePlanChange`
([generator.go](../../internal/idempotency/generator.go)). It scopes the *settlement* — the one
invoice or the one wallet credit — so a retried attempt does not charge twice.

The key is `(subscription_id, target_plan_id, caller key)` when the caller supplies
`idempotency_key`, and `(subscription_id, target_plan_id, subscription version, subscription
updated_at)` when it does not. Both hold the two properties the key needs, which `effective_at`
(read from the clock on every call) held neither of:

- **Stable while an attempt is failing.** A caller that times out and retries derives the same key,
  because a rolled-back attempt leaves the subscription row untouched.
- **Distinct once a change has landed.** `updated_at` moves on any write, so Starter → Pro →
  Starter → Pro does not replay the first change's invoice.

The key is not what makes a double-execute safe on its own: a concurrent second attempt blocks on
the row lock and is then rejected by the `target_plan_id == plan_id` precondition (matrix rows 22
and 23). The key is the backstop for the settlement itself.

---



## 7. Why no `subscription_associations` table

Directionality is not the argument — `addon_associations` already indexes
`(tenant, env, entity_id, entity_type, addon_id)` for "what does this sub have", and a
`(tenant, env, addon_id)` index gives the reverse. One table reads both ways.

The real value would be one homogeneous row per link across all entity types, so the change engine
becomes *iterate associations, apply disposition* instead of one handler per entity type. That is a
genuine argument at six entity types. It is not true at one.

The cost is that a generic table holds only what is common, so one fact lives in two rows that can
disagree:

- `RemoveAddonFromSubscription` is 170 lines that know nothing about a registry. Unless it is patched too, billing correctly stops billing a cancelled addon while the change engine still sees it ACTIVE and tries to prorate a line item that ended two months ago.
- `addon_associations.end_date` and a registry's `effective_to` both mean "when this addon stopped". A period-end cancel sets Nov 1; a registry hook holding only `time.Now()` writes Oct 5. Two truths, 27 days apart, no constraint catches it.
- Any future attach path that forgets the hook leaves the registry silently incomplete — worse than absent, because the engine trusts it.

**Do the two real fixes as columns** (`credit_grants.addon_association_id`,
`addon_associations.quantity`). When more entities get dispositions, add `source` to
`addon_associations` and `coupon_associations` the same way. If the per-entity loop then hurts, build
the generic table **as a view or materialised projection** — derived, not a second source of truth,
and structurally incapable of disagreeing.

**Versioning — background only, not proposed.** Recorded to show why per-association versioning
does not work, should the table ever be revisited. Version would have to be per *subscription*:

```
after a change:  subscriptions.version = 2
  (S, PLAN,   starter, created_in_v=1, ended_in_v=2)
  (S, PLAN,   pro,     created_in_v=2, ended_in_v=NULL)
  (S, ADDON,  assoc_A, created_in_v=1, ended_in_v=2)
  (S, COUPON, assoc_C, created_in_v=1, ended_in_v=NULL)   <- untouched, NOT rewritten

config at version N:
  WHERE created_in_v <= N AND (ended_in_v IS NULL OR ended_in_v > N)
```

Unchanged rows are not duplicated. Per-association versioning cannot answer "config at version N" at
all — PLAN would be at v2 while the coupon is at v1 — leaving only timestamp queries, in which case
the version column is dead weight.

**What v0 gives up:** plan history, which restart provided accidentally as a chain of cancelled rows.
Mitigations: emit `subscription.plan_changed` with `{from_plan_id, to_plan_id, effective_at}`, and
note that *service* history is already recoverable at finer granularity from `invoice_line_items` via
the immutable `subscription_line_item_id` FK.

---



## 8. `addendum_config` — attaching addons, and replace

`addendum_config.addons` attaches addons as part of the change. Combined with `entity_policies`,
**replace** is expressible without a dedicated action:

```json
{ "target_plan_id": "plan_pro",
  "proration_behavior": "create_prorations",
  "entity_policies":  { "addons": { "overrides": { "addon_assoc_A": "drop" } } },
  "addendum_config":  { "addons": [ { "addon_id": "addon_B", "cadence": "recurring" } ] } }
```

**Adding an addon that is already attached is not a conflict.** It creates a second attachment,  
which §5 establishes is legitimate and which `AddAddonToSubscription` already supports. To replace  
rather than stack, drop the existing attachment in the same request. Note this differs from coupons,  
where additive application *is* a bug — `[handleSubCoupons](../../internal/ee/service/subscription.go#L4602)`  
has it today — so the precedence rule must be decided per entity type when `SubscriptionCoupons`  
joins `addendum_config`, not inherited from addons.

```
close plan lines         -> ProrationActionRemoveItem
open  target-plan lines  -> ProrationActionAddItem
dropped addon lines      -> ProrationActionRemoveItem
added addon lines        -> ProrationActionAddItem
      -> Compute once -> one summary
      -> write all line items in one tx
      -> settle once
```

Three gaps to close in the shared settlement:

- `Apply` **returns only** `error`**.** `ChangedResources` needs the invoice and wallet-transaction ids, so the change calls `Compute` and settles itself, or `Apply` widens its return.
- **Charges and credits do not net.** `Apply` raises an invoice for `TotalChargeAmount` *and separately* a wallet credit for `TotalCreditAmount`, so a replace whose drop out-credits the add produces both. §6 decides this: net them, emit credits as negative invoice lines, and raise nothing when the net is zero. The pay-first path already does it (`createAggregatedProrationDraftInvoice` locks charges − credits).
- **Compute skips usage prices** ([line_item_proration.go:99](../../internal/ee/service/line_item_proration.go#L99), *"future consumption is unknown at change time"*). Correct, but it means the settlement never sees usage lines — usage continuity (§6) is decided outside this path.

---



## 9. Persistence delta


| Piece                                       | Change                                                                                                                                                                                                                                                                                    |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `subscriptions.plan_id`                     | drop `.Immutable()`; add to the repository `Update` field list                                                                                                                                                                                                                            |
| `subscriptions.synced_price_sequence`       | **reset on swap** to the target plan's current max. The watermark is only meaningful relative to a `plan_id` — the discovery filter is `synced_price_sequence < TargetSeq` ([planpricesync/repository.go:92](../../internal/domain/planpricesync/repository.go#L92)) — so a carried value silently marks the sub permanently in-sync with the new plan |
| `invoice_line_items.subscription_line_item_id` | populate it in `buildChargeLineItem` ([line_item_proration.go:221](../../internal/ee/service/line_item_proration.go#L221)). Regular invoices set it ([billing.go:289](../../internal/ee/service/billing.go#L289)); proration invoices leave it NULL, so §10's join sees only regular invoices |
| `credit_grants.addon_association_id`        | new nullable immutable column; backfill from `addon_id` where the subscription has exactly one association for that addon, NULL otherwise (§5)                                                                                                                                            |
| `addon_associations.quantity`               | new column, default 1, backfilled from line-item quantity (§5). **Not in v0** — the `addon_quantity` metadata it replaces is written twice and read nowhere, and proration already uses the line item's real quantity. Hygiene, not correctness                                            |


---



## 10. Prerequisite — the credit basis

**Ships first, on v1. Blocking.** Credit is computed from the **list** price
([proration.go:451,463](../../internal/ee/service/proration.go#L451), and again at
[line_item_proration.go:210](../../internal/ee/service/line_item_proration.go#L210)), and the cap
meant to bound it never binds: `capCreditAmount`
([calculator.go:179-200](../../internal/domain/proration/calculator.go#L179)) compares
`OldPricePerUnit × OldQuantity × coefficient` against an `OriginalAmountPaid` that is set to the same
`price.Amount × quantity` un-scaled, and the coefficient is ≤ 1.

Restart masks this — each subscription is credited once and cancelled. Under swap a line item
survives many changes and the error compounds. Fix is the join through the immutable
`invoice_line_items.subscription_line_item_id` FK; the call sites
`getOriginalAmountPaidForLineItem` and `getPreviousCreditsForLineItem` already exist commented out at
[proration.go:441,454](../../internal/ee/service/proration.go#L441).

**The join needs the FK populated first.** Proration invoice lines do not set
`SubscriptionLineItemID` today (§9), so without that one-line fix the basis for any
proration-created charge reads as zero and test 4 cannot pass. Do it in the same change.

Ship it on v1 where the parity harness measures it. Credit basis (over-credit from list price) and
credit capping point in opposite directions — fixing the cap without fixing the basis turns an
over-credit into real money leaving the business, so they land together.

---



## 11. Sequencing

**v0 is planned in phases in [2026-08-12-plan-change-v2-v0-plan.md](2026-08-12-plan-change-v2-v0-plan.md)** —
that document is the implementation source of truth. This list is the whole arc, v0 and beyond.

1. **Credit basis fix** (§10) + populate `SubscriptionLineItemID` on proration lines, on v1. Blocking. — *v0, phase 0*
2. `credit_grants.addon_association_id` + backfill, and scope addon-drop grant cancellation on it. Only matters once a change can drop an addon. — *v0, phase 3*. `addon_associations.quantity` is hygiene — the metadata it replaces is read nowhere — and is **not** in v0.
3. `plan_id` mutable, `synced_price_sequence` reset; `SubRepo.GetForUpdate` mirroring the invoice repo. — *v0, phase 1*
4. `/change/v2` preview + execute, immediate only, plan lines then addons. v1 stays supported. — *v0, phases 2–4*
5. Deferred (`end_of_period`) + collapse the three scheduled executors ([subscription.go:3709](../../internal/ee/service/subscription.go#L3709), [update_billing_period_activities.go:380](../../internal/temporal/activities/subscription/update_billing_period_activities.go#L380), [subscription_schedule.go:246](../../internal/ee/service/subscription_schedule.go#L246)) into one — all three currently pass `time.Now()` instead of the scheduled instant.
6. Payment gating — add plan change to the checkout allowlist.
7. Point the Stripe inbound `handlePlanChange` ([internal/integration/stripe/subscription.go:585](../../internal/integration/stripe/subscription.go#L585)) at the same service, deleting the fourth implementation. Needs a v2 equivalent of `OpeningInvoiceAdjustmentAmount` first.
8. Webhooks: `subscription.updated` + new `subscription.plan_changed`. **Never** `subscription.cancelled` then `subscription.created` for an upgrade.
9. Tier offset for split usage lines, and the VOLUME-mode policy (§6).
10. Interval / cadence / currency change. Until this lands, v1 is not deprecated.

Deleted once v2 is the only in-place path: `inheritPaddleEntityMappings` and its TODO
([subscription_change.go:959](../../internal/ee/service/subscription_change.go#L959)),
`transferLineItemCoupons` (`:1106`), `mergeSubscriptionMetadata` (`:945`, already dead), and most of
`internal/domain/subscription/change.go`.

---



## 12. Verification


| #   | Scenario                                                   | Expected                                                                                                            |
| --- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| 1   | Same-interval upgrade mid-period                           | one row, same `id`, anchor and period bounds unchanged, `plan_id` updated, line items tile with no gap or overlap   |
| 2   | Lateral change, identical price                            | line carried: same `id` and window, repointed at the target's price, **no proration entry, no invoice** (§6 row 1)  |
| 3   | Upgrade then downgrade back in one period                  | total equals true consumption, **with no special-case code**                                                        |
| 4   | Two changes in one period                                  | second credit references the first change's debit, not list price                                                   |
| 5   | Downgrade, credit exceeds the invoice                      | visible credit line, residue to the wallet, not discarded                                                           |
| 6   | `proration_behavior: none`                                 | service swaps, no credit, next regular invoice at the new price                                                     |
| 7   | Metered plan, usage before the change                      | pre-change usage billed at the old rate on the next regular invoice                                                 |
| 8   | Addon carry (default)                                      | zero DML against the association, its line items, grants, entitlements                                              |
| 9   | Addon drop, fixed advance                                  | prorated credit **netted onto the plan-change invoice**, not a separate wallet top-up                               |
| 10  | Addon drop, usage arrear                                   | no credit; usage `[period_start, effective_at)` on the next regular invoice                                         |
| 11  | Addon drop, fixed arrear                                   | prorated charge for the consumed fraction (bills nothing today)                                                     |
| 12  | Addon drop, ONETIME grant                                  | untouched, not clawed back                                                                                          |
| 13  | Addon drop, RECURRING grant                                | future applications cancelled, already-applied period kept                                                          |
| 14  | Drop lands exactly on a period boundary                    | the boundary CGA does not apply                                                                                     |
| 15  | Dropped addon's feature also on the plan                   | access retained, limit lowered                                                                                      |
| 16  | Cancel at period end, re-attach, change inside the overlap | the override targets the right association; the survivor and **its credit grant** are untouched                     |
| 17  | Addon with `quantity = 2`, dropped                         | one line item closed, credit prorated on the full quantity                                                          |
| 18  | Line-item coupon                                           | survives the change and applies to the next invoice                                                                 |
| 19  | Subscription-level coupon                                  | survives                                                                                                            |
| 20  | Entitlement usage counter                                  | preserved across the change                                                                                         |
| 21  | Trial in progress                                          | continues to its natural end                                                                                        |
| 22  | Concurrent double-execute, **different** target plans      | second blocks on the row lock; exactly one change lands, no duplicate line items                                    |
| 23  | Replayed `idempotency_key`                                 | same response, no second change                                                                                     |
| 24  | Interval / hierarchy / phases / pause / currency           | 4xx, no mutation                                                                                                    |
| 25  | Preview vs execute                                         | identical money for the same request at the same instant                                                            |
| 26  | Tiered usage line, price differs across plans              | ladder restarts per window — asserts the accepted v0 limitation (§6), so the fix has a failing test to flip         |
| 27  | Swap, then a plan-price change on the target plan          | the sub is picked up by the plan-price sync — `synced_price_sequence` was reset (§9)                                |


```bash
go test -v -race ./internal/ee/service -run TestSubscriptionChangeV2
make test
make lint-ci
make generate-ent && make generate-migration
make swagger && make sdk-all
```

**End to end** (`make run-local`, `make migrate-local`): customer plus Starter and Pro with matching
`lookup_key`s → subscribe, attach one fixed-advance and one usage-arrear addon, let the first invoice
issue → ingest events against both meters → preview and check dispositions, mapping, money → execute
with `entity_policies.addons.default = "carry"` and an override dropping the arrear addon → assert
subscription id, `billing_anchor`, `current_period_*` and `billing_sequences.last_sequence` unchanged,
`plan_id` changed, carried addon untouched, **one invoice not three** → roll the period and assert
the dropped addon's usage was billed → replay with the same idempotency key → attempt an
interval-changing target and get a 400 with nothing mutated.