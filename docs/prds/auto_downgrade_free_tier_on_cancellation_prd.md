# Auto-Downgrade to Free Tier on Cancellation

**Linear:** [FLE-973](https://linear.app/flexprice/issue/FLE-973/auto-downgrade-to-free-tier-on-cancellation) · **Status:** Draft

## Problem

When a customer cancels a paid subscription today, they're left with **no active
subscription** — and since entitlements are derived live from a customer's active plans,
they silently lose all access.

## What we're building

When a customer cancels their **last** paid subscription, automatically start a subscription
on the tenant's **free plan**, so they land on the free tier instead of losing access.

There's no new "free subscription" concept. A free plan is just a plan whose price is
**fixed, recurring, and $0** — the same definition tenant onboarding already uses.

## Behavior

Downgrade fires only when the cancelled subscription was the customer's **last** active one,
and it starts when the cancellation actually takes effect:

| Cancellation type | Free subscription starts |
|---|---|
| Immediate | now |
| End-of-period / scheduled | when the period ends (deferred) |

Guardrails:
- Customer still has another active paid sub → **no** downgrade.
- Tenant has no free plan → skip silently; cancellation still succeeds.
- The cancelled sub was itself the free plan → **no** re-downgrade (no loop).
- Multiple free plans (not expected) → pick the first.

Entitlements need no special handling: they're computed live from the active plan, so an
active free-plan subscription automatically yields free-tier entitlements. Nothing is copied.

## Design

A single helper, `ensureFreePlanSubscriptionOnCancellation`, called from the two points where
a cancellation takes effect:

1. **Immediate** — `CancelSubscription`, after its transaction commits.
2. **Deferred** — `processSubscriptionPeriod`, when the period boundary is reached.

The free plan is found via `PlanService.FindFreePlan(ctx)` (shared with tenant onboarding).
Both hooks run **post-commit** and are **best-effort** (log on failure; the cancellation has
already succeeded) and **idempotent** (the "no other active sub" check also guards retries).

## Edge cases

- Inherited subscriptions never trigger a downgrade (already rejected by `CancelSubscription`).
- Trialing counts as "active" when checking whether it's the last subscription.
- Verify a $0 plan produces no spurious invoice/webhook on subscription creation.

## Open questions

1. Ship the heuristic ("first $0 plan") as-is, or add a per-tenant `downgrade_target_plan_id`
   setting to make the fallback explicit and opt-in?
2. Do we need a distinct "downgraded to free tier" webhook, or are the existing
   cancellation + subscription-created events enough?
