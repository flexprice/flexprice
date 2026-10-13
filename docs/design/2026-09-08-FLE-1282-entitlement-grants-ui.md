# FLE-1282 — Entitlement Grants in the Dashboard

- **Ticket:** FLE-1282
- **Date:** 2026-09-08
- **Author:** Ojas Aggarwal
- **Status:** Implemented — pending review

---

## 1. Goal

Entitlement grants were API-only: the dashboard could neither configure them nor show their state. Two things in scope:

1. Make grants configurable and visible in the dashboard.
2. Move metered entitlements onto grants, so the platform stops carrying two quota models.

The second reframes the first: the create form has no legacy branch. A non-recurring allowance is a grant with `grant_duration_unit = subscription_period`; the legacy `usage_limit` / `usage_reset_period` / `is_soft_limit` trio is never sent for a metered feature.

**"Grants only" precisely:** every metered entitlement that carries a finite quota goes through grants. `adjustMeterUsageEntitlement` survives regardless — see §5.

---

## 2. Background

A grant does **not** replace an entitlement. The `entitlements` row stays the configuration; a grant is that configuration materialized as a concrete time window with a usage snapshot.

Both models meet at one place — a switch inside `CalculateMeterUsageCharges`:

```
billing_meter_usage.go
  ├─ grants exist for this meter  → adjustMeterUsageGrants      (grants win)
  ├─ entitlement enabled          → adjustMeterUsageEntitlement (legacy)
  └─ neither                      → raw pricing
```

---

## 3. What shipped

### 3.1 Backend

| # | Change | Why |
|---|---|---|
| 1 | **Tiered prices rejected for both measures** | Only `amount` was rejected at write time. A quantity grant saved cleanly, then `grantPricingGuard` declined at invoice time and control fell to the legacy path, where a nil `usage_limit` reads as *unlimited* → billed $0 |
| 2 | **Subscription overrides inherit grant config** | An override *replaces* its parent in the resolved set. Copying everything except the grant fields silently downgraded that customer's feature to legacy. Pre-existing bug |
| 3 | **Unlimited allowances** | `grant_quota` unset + `subscription_period`. Previously the one metered case with no grant-shaped expression |
| 4 | **Usage summary knows about grants** | `GET /customers/:id/usage` reported every allowance as unlimited with zero usage |
| 5 | **`grant_state` on entitlement reads** | The live allowance — windows, usage, remaining, cycle totals — had no read surface at all |
| 6 | **Grant summary on `AggregatedEntitlement`** | Read APIs could not express "1,000 per hour" before a window existed, so every screen rendered "Unlimited" |

### 3.2 Frontend

- Create form: one mode switch (*recurring* / *once per billing period* / *unlimited*) replacing four flat controls, plus a preview of what the config produces over a cycle
- Value + Usage Reset columns on plan, addon and subscription screens, reading identically for grant-backed and legacy rows
- Live allowance meter on the subscription page; expandable per-window ledger on the customer usage table
- Subscription-creation overrides edit the allowance, not a usage limit

---

## 4. Low-level design

### 4.1 Data model

Only one schema change: `entitlement_grants.unlimited boolean NOT NULL DEFAULT false`.

```
entitlements                          (config — unchanged shape)
  grant_measure, grant_quota*, grant_duration_value*, grant_duration_unit,
  grant_allocation_behavior, aggregation_mode
      * grant_quota NULL + subscription_period  ⇒  unlimited

entitlement_grants                    (runtime — one row per window)
  quota, usage, valid_from, valid_to, grant_status, last_computed_at,
  quota_crossed_at, unlimited ← new
```

`quota` on the grant row is `NOT NULL` and immutable, so unlimited is a flag rather than a nullable quota. Every read of `Quota` already went through three domain methods, so the flag hides inside them:

```go
func (g *EntitlementGrant) IsExhausted() bool { if g == nil || g.Unlimited { return false } ... }
func (g *EntitlementGrant) Overage()          { if g == nil || g.Unlimited { return decimal.Zero } ... }
func (g *EntitlementGrant) Remaining()        { if g == nil || g.Unlimited { return decimal.Zero } ... }
```

Consequence: **billing needed no change for unlimited.** `Overage()` returns zero, so the snapshot path sums to nothing and the merged path sees no crossed window.

### 4.2 Validation

One shared function, two callers, so write-time rules cannot drift:

```
grantMeterEligibility(meter, measure)      entitlement_grant.go
  ├─ MAX aggregation            → reject
  ├─ bucketed meter             → reject
  ├─ bucketed price             → reject
  └─ tiered price               → reject   (both measures)

validateGrantConfig(entitlement)           domain/entitlement/model.go
  ├─ metered features only
  ├─ quota nil ⇒ duration must be subscription_period
  ├─ quota set ⇒ must be positive
  └─ duration ≥ 1 hour

validateGrantSiblingCoherence(entitlement) entitlement_grant.go
  ├─ one aggregation mode per feature
  ├─ one measure per feature
  ├─ additive ⇒ one duration
  └─ no mixing unlimited with bounded      ← new
```

### 4.3 Read API

`grant_state` hangs off the aggregated entitlement — config and its runtime together:

```jsonc
"entitlement": {
  "grant_quota": "1000",              // the promise, available before any window
  "grant_duration_unit": "hour",
  "grant_unlimited": false,
  "grant_state": {                     // the runtime
    "windows": [                       // every window overlapping the cycle, oldest first
      { "grant_id": "eg_...", "quota": "1000", "usage": "1600", "remaining": "0",
        "valid_from": "...", "valid_to": "...", "status": "exhausted",
        "is_active": false, "unlimited": false, "last_computed_at": "..." }
    ],
    "cycle_totals": { "windows": 2, "total_quota": "2000",
                      "total_usage": "2000", "total_overage": "600" }
  }
}
```

Notes:

- **One array, not two.** The live balance is the entry (or entries, for parallel) with `is_active`. It is evaluated against the server clock so clients never compare timestamps.
- `status` is the **quota** state (exhausted or not); `is_active` is the **time** state (window open or not). A window can be `status: active, is_active: false` — it closed without being consumed.
- `windows` empty means no window has opened yet. That is a different fact from zero usage and must render differently (`— / 1,000 · starts on first use`).
- Present on `GET /subscriptions/:id/entitlements`, `GET /customers/:id/entitlements`, and `GET /customers/:id/usage`.

Query shape (`GrantStateByFeature`):

```go
filter.WithCustomerIDs(sub.CustomerID).      // leading column of the lookup index
      WithSubscriptionIDs(sub.ID).
      WithScopeEntityType(feature)
filter.WithCycleOverlap(cycleStart, cycleEnd)
```

`customer_id` is logically redundant with `subscription_id` but is the leading selective column of `(tenant, env, customer_id, valid_to, ...)`. Without it the planner scans the whole tenant's grants.

### 4.4 Override semantics

`OverrideEntitlementRequest` carries the six grant fields. **Omitted means inherit** — an override that changes only the quota keeps the plan's cadence and measure. Nothing else; there is no "clear" mode, because its only reachable outcomes are *unlimited* (already expressible) or *back to legacy* (which grants-only exists to eliminate).

The merged row is validated before insert — an override can push a valid parent into an invalid combination.

---

## 5. What survives the cutover regardless

`adjustMeterUsageEntitlement` cannot be deleted. Five cases keep it alive:

| Case | Why |
|---|---|
| Boolean / static / config features | grant config is metered-only |
| Unlimited on a non-`subscription_period` cadence | rejected; unlimited requires the cycle window |
| Non-bucketed MAX meters | a peak does not decompose over time windows |
| Bucketed SUM + flat-fee price | grants reject any bucketed price; legacy permits it by an explicit exemption |
| Tiered-priced meters | out of scope for grants (§3.1 #1) |

---

## 6. Reference

| Concern | Location |
|---|---|
| Grant/legacy fork | `billing_meter_usage.go`; legacy adjustment at `:421` |
| Grant overage fold | `billing_meter_usage_grants.go:82`; guard at `:263` |
| Window math | `entitlement_grant.go:623`; `subscription_period` branch inside |
| Catch-up loop | `entitlement_grant.go:528` |
| Shared meter rules | `entitlement_grant.go` `grantMeterEligibility`; siblings in `validateGrantSiblingCoherence` |
| Field coherence | `domain/entitlement/model.go` `validateGrantConfig` |
| Read state | `entitlement_grant.go` `GrantStateByFeature`; merge in `billing.go` `attachGrantState` |
| Aggregation | `billing.go:2365` (metered), `:2500` (grouping) |
| Usage summary | `billing.go:3189` |
| Override path | `subscription.go:7079` |

Prior art: `2026-07-08-FLE-959-Entitlements-Revamp.md` (§2 concepts, §7 restrictions), `2026-08-27-entitlement-grant-proration-erd.md` (§6 known gaps).

---

## 7. Open points

Parked deliberately, not overlooked. None of these block the cutover; all of them
became visible while testing it. Ordered by how much they can surprise a customer.

### 7.1 The override target is chosen in the frontend

An override is modelled as an edge between two `entitlements` rows: the
subscription-scoped row carries `parent_entitlement_id`, and
`takeOverGrantWindowsFromParent` computes `delta = override − quota_of(parent)`.
The backend trusts that pointer without re-deriving it.

The pointer is produced in React
(`subscriptionEntitlementHelpers.ts` — `parentEntitlementIdForOverride`), whose
middle fallback is `getParentSource`: the first element of `sources[]` whose
`entity_type` is `plan` or `addon`. That array is built in
`billing.go` `AggregateEntitlements` by appending in the resolver's iteration
order, which nothing constrains.

So on a feature fed by a plan entitlement of 1,000 and an addon entitlement of
100, editing the allowance to 500 mid-cycle yields either `delta = −500` or
`delta = +400` depending on row order — a cycle allowance of 600 or 1,500 for the
same action. This is undefined behaviour rather than a rule that happens to be
wrong.

Two things are tangled here and both want fixing:

- **Layering.** The client knows "the user typed 500 for feature F on
  subscription S". Which entitlements that replaces, and what delta results, is
  billing logic that should not execute in a browser. The endpoint should accept
  `{feature_id, grant_quota}` and resolve the rest server-side.
- **Model.** An override is really a scope statement ("this subscription governs
  feature F"), not a row-to-row edge. Expressed that way there is no target to
  choose, so the ambiguity disappears instead of being resolved badly.

### 7.2 An override replaces one entitlement, not the feature's allowance

Following from 7.1: `filterOverriddenEntitlements` suppresses exactly the row
named by `parent_entitlement_id`. Every other contributor to the same feature
survives and still pools in.

Typing 2,000 against a plan entitlement, on a subscription that also carries a
100-unit addon entitlement for that feature, produces a 2,100-unit window. The
form offers one field per feature, so the number the user typed is not the number
they get. Both write paths behave this way — it is not a mid-cycle artefact.

The fix is to key suppression off `(subscription_id, feature_id)` rather than the
parent pointer, and to compute `delta = override − Σ(all suppressed entitlements)`.
Two constraints come with it:

- **One override per (subscription, feature).** Today the creation screen lists a
  row per entitlement, so two `override_entitlements` entries can name two
  contributors to the same feature. Under feature-level semantics that is two
  suppressors for one slot — the same ordering problem in a new place. The
  creation UI should collapse to one row per additive feature, with validation as
  a backstop.
- **Additive only.** Parallel entitlements produce separate windows that bill
  separately, so there is no single number to override. Keep per-entitlement
  editing there. `aggregation_mode` is already available at the branch point.

Accepted cost: attaching an addon afterwards, to a feature that carries an
override, leaves the customer paying for the addon with no change to their
allowance. That is the correct reading of "this customer gets exactly N", but it
needs a warning at addon-attach time or it reads as a bug.

### 7.3 The two write paths disagree about what may be overridden

`ProcessSubscriptionEntitlementOverrides` (subscription create) rejects addon
entitlements outright — *"only plan entitlements can be overridden"*. The
mid-cycle path (`POST /v1/entitlements` with `entity_type=SUBSCRIPTION` and a
`parent_entitlement_id`) applies no such restriction and will happily override an
addon entitlement.

Same product action, two rules. Whichever way 7.2 resolves, these should agree.

### 7.4 No mid-cycle view of what is actually running

After creation, the edit surface exposes one field per feature and no indication
of where the allowance comes from. The user cannot see that 2,100 is
`2,000 override + 100 addon`, nor remove an override to revert to the plan.

Proposal: one editable row per **grant window** — so one number per additive
feature with a read-only contributor breakdown beneath it, and one row per
entitlement for parallel features. Removing an override maps to
`DELETE /v1/entitlements/:id` on the child plus `settleGrantWindowsForDeletedEC`,
which is already wired. Deleting an addon's contribution should remain an addon
detach, not an entitlement delete — the subscription should not claim to sell an
addon whose allowance has been zeroed.

This wants to ship with 7.2, not before it. On its own it would make the current
per-entitlement behaviour visible rather than correct.

### 7.5 The two billing lanes disagree about when overage starts

`adjustMeterUsageGrants` branches on the number of distinct entitlement config
ids in the fold:

- one id — snapshot lane, `Σ max(0, usage − quota)` per window, which ignores
  `quota_crossed_at` entirely;
- several — interval lane, billing usage that falls inside the merged
  `[quota_crossed_at, valid_to)` range.

A pooled feature therefore charges from the first unit past quota, while a
parallel feature forgives everything consumed before the crossing was detected.
The lanes exist for different reasons and neither is obviously wrong, but the
same customer can experience both. This needs a product decision before the code
can be unified.

### 7.6 `entitlement_config_id` on a grant carries two meanings

For additive features the column holds the **slot key** — the lowest-ULID
contributing entitlement, chosen by `grantCandidatesForFeature` purely to give
the unique index
`(tenant, env, entitlement_config_id, customer, subscription, valid_from)`
something stable to arbitrate on. It is not attribution: a window can sit on an
addon's entitlement id while its quota is mostly the plan's, and an override of
the plan's entitlement re-cuts that window without the id changing.

Reading it as "the entitlement that produced this grant" is a natural and wrong
inference, and it made the 7.1 investigation considerably harder than it needed
to be. Splitting it into `slot_key` plus a contributor list would make grant rows
self-explanatory.

### 7.7 The resolved allowance is reconstructed in four places

"What does this subscription grant for feature F, and from where" has four
independent implementations:

| Site | Produces | Diverges by |
|---|---|---|
| `filterOverriddenEntitlements` | surviving entitlements | suppresses by parent pointer (7.2) |
| `AggregateEntitlements` | per-feature fold + `sources[]` | array order is unconstrained (7.1) |
| `grantCandidatesForFeature` | slot shape and summed quota | picks the lowest-ULID entitlement (7.6) |
| `enrichSubscriptionEntitlements` (frontend) | which entitlement to override | three-tier fallback (7.1) |

Most of 7.1–7.6 lives in the gaps between these. A single
`ResolveFeatureAllowance(sub, featureID) → {Total, Contributors[], Slot}`,
called by every read path and returned to the client so the frontend renders the
breakdown instead of deriving it, collapses the divergence and is the same
groundwork 7.2 and 7.4 both need.

### 7.8 Smaller items

- Swagger is not regenerated for `grant_unlimited`, the `swaggerignore` additions,
  or the now-nullable `remaining`.
- Plan and addon entitlement editing remains disabled in the dashboard
  (`edit={{ enabled: false }}`); only subscription-level editing is reachable.

---

## 8. Reading guide

Call traces by product action, in the order they make sense to read. §6 is the
same information indexed by concern; this section is indexed by what the user did.

Two rules make the rest legible:

- An entitlement is the **rule**; a grant is that rule **materialized as one time
  window with a usage snapshot**. Writing a rule never touches a window directly.
- Usage is **re-measured, never accumulated**. Any figure written into
  `entitlement_grants.usage` by hand is overwritten on the next pass.

### 8.1 Create an entitlement on a plan

Plan page → Add entitlement.

```
POST /v1/entitlements                                      router.go:473
→ EntitlementHandler.CreateEntitlement
→ entitlement.go:48   CreateEntitlement
    :226   deriveGrantConfig             usage_limit → grant_quota; unset → unlimited
    entitlement_grant.go:889  validateEntitlementGrantShape
      :917   grantMeterEligibility       MAX / bucketed / tiered → reject
      domain/entitlement/model.go  validateGrantConfig
      :1004  validateGrantSiblingCoherence   one mode and measure per feature
    EntitlementRepo.Create
    :189   takeOverGrantWindowsFromParent    no-ops — not subscription-scoped
```

No grant is touched. A plan entitlement affects nothing until a subscription
resolves it.

### 8.2 Create a subscription with overrides

New subscription → edit any listed allowance before saving.

```
POST /v1/subscriptions        (override_entitlements[])
→ subscription.go:368
→ subscription.go:7143  ProcessSubscriptionEntitlementOverrides
     validates plan entitlements only — addon overrides are rejected here (§7.3)
     per entry: create a subscription-scoped entitlement,
                parent_entitlement_id = the target,
                grant fields inherited from the parent when omitted (§4.4)
```

Still no grant. Windows open later, in 8.5.

### 8.3 Edit an allowance mid-cycle

Two flows. The frontend chooses by checking whether a subscription-scoped
entitlement already exists for that feature.

**(a) First touch of this feature — create the override**

```
POST /v1/entitlements
  entity_type=SUBSCRIPTION, entity_id=subs_…, parent_entitlement_id=ent_…
→ entitlement.go:48   CreateEntitlement
→ entitlement.go:951  takeOverGrantWindowsFromParent
     delta = override − parent.quota                       source override_created
```

**(b) An override already exists — edit it**

```
PUT /v1/entitlements/:id
→ entitlement.go:719  UpdateEntitlement
     grantConfigMoved(before, after)     six grant fields compared
→ entitlement.go:878  resettleGrantWindows
     delta = new − prior                                   source entitlement_updated
```

Both converge on one path:

```
entitlement.go:901  reissueGrantWindows
  ListGrants                              live feature-scoped windows
  GetSubscriptionGrantECsByFeature        the full contributor set for the feature
→ entitlement_grant.go:163  ReissueEntitlementGrants        [DB.WithTx]
     :85    CloseEntitlementGrants        valid_to = now, usage frozen
     :223   OpenFeatureBasedEntitlementGrants
              successor quota = closed.Remaining() + delta
```

The identity `(old − usage) + (new − old) = new − usage` is what makes this safe:
consumed usage is neither refunded nor charged twice.

### 8.4 Remove an override, delete an entitlement, detach an addon

```
DELETE /v1/entitlements/:id
→ entitlement.go:1006  DeleteEntitlement
→ entitlement.go:970   settleGrantWindowsForDeletedEC
     override → delta = parent − override            source override_removed
     net-new  → handleGrantsForRemovedECs            source entitlement_gone
→ reissueGrantWindows → ReissueEntitlementGrants     (as in 8.3)
```

Addon detach reaches `handleGrantsForRemovedECs` by the same route.

### 8.5 A grant window opens

No API call opens a window. Two triggers only:

```
(a) scheduled       temporal/activities/alerts/alerts.go:71
                  → alert_evaluation.go:177  EvaluateSpendAndEntitlementAlertsForCustomer
                  → alert_evaluation.go:207  EnsureGrantsForSubscriptions

(b) pre-invoice     invoice.go:2231
                  → alert_evaluation.go:222  RefreshEntitlementGrantsForCustomer
                  → alert_evaluation.go:236  EnsureGrantsForSubscriptions
```

Then:

```
entitlement_grant.go:372  EnsureGrantsForSubscriptions
  :455  buildGrantEvalMeta               meters, external customer ids
  :546  openMissingGrants
     :572  eligibleGrantConfigsByFeature    runs the resolver
     :605  grantCandidatesForFeature        additive → one slot, quotas summed,
                                            on the lowest-ULID entitlement (§7.6)
                                            parallel → one slot per entitlement
     :647  openIfSlotFree                   the unique index arbitrates the race
        :681  openOneGrant
           :743  computeGrantWindow         subscription_period branches here
           :845  earliestUncoveredUsage     first_usage allocation
```

Three gates, all off by default, all named after alerts but in fact controlling
whether allowances materialize at all: `FLEXPRICE_TEMPORAL_ENABLED`,
`usage_alerts.enabled`, and the per-tenant `entitlement_alert_config.alert_enabled`.

### 8.6 Usage is measured

Same pass, immediately after opening:

```
alert_evaluation.go:250  evaluateEntitlementGrantsForCustomer
  :314 → :368  refreshEntitlementGrantUsage
     ClickHouse query over [valid_from, min(now, valid_to))
     writes usage absolutely; stamps quota_crossed_at on the first crossing
  :417  transitionEntitlementGrantAlert
```

### 8.7 An invoice is generated

```
invoice.go:2231  RefreshEntitlementGrantsForCustomer     grants made current first
→ billing_meter_usage.go:53  CalculateMeterUsageCharges
     loadEntitlementGrantsByMeterID                      billing_meter_usage_grants.go:35
     ├─ grants found  → :82  adjustMeterUsageGrants
     │      :264  grantPricingGuard        on decline, control falls to legacy
     │      :127  len(ecIDs) <= 1 ? snapshot : :169 mergedOverage      (§7.5)
     ├─ entitlement enabled → billing_meter_usage.go:421  adjustMeterUsageEntitlement
     └─ neither             → raw pricing
```

### 8.8 What each screen calls

| Screen | Client call | Backend |
|---|---|---|
| Plan / Addon entitlements | `EntitlementApi.search` | `QueryEntitlements` |
| Subscription → Entitlements tab | `SubscriptionApi` `GET /:id/entitlements` | `GetAggregatedSubscriptionEntitlementsForSubscription` → `AggregateEntitlements` + `GrantStateByFeature` |
| Customer → Entitlements | `CustomerApi` `GET /:id/entitlements` | `GetCustomerEntitlementsForSubscriptions` |
| Customer → Usage table, `GrantWindowLedger` | `CustomerApi` `GET /:id/usage` | usage summary + `grant_state.windows` |
| Add / edit entitlement form | `EntitlementApi.create` / `.update` | 8.1, 8.3a, 8.3b |
| Subscription create with overrides | `SubscriptionApi` create | 8.2 |

Before any write fires, `subscriptionEntitlementHelpers.ts:76` computes the
`parent_entitlement_id` the request carries — see §7.1.

### 8.9 Suggested order

8.3 and 8.5 are the spine — mid-cycle re-cut and materialization. 8.7 only makes
sense once 8.6 is clear, since billing reads what the refresh pass wrote. Read
8.1 and 8.2 first for vocabulary, then the spine, then measurement and billing.
