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
