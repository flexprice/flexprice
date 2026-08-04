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

## Plan changes (upgrade/downgrade)

A plan change (`SubscriptionChangeService`) internally **cancels the old subscription and
creates the replacement** in one transaction. That internal cancel must NOT trigger the
auto-downgrade — otherwise the customer ends up with a stray free subscription next to the
plan they changed to.

Handled via a `SkipAutoDowngrade` flag on the cancel request: the change flow always sets it
(a plan change always creates a replacement — target plan is required and creation is atomic),
so the downgrade is suppressed there. Every other cancel is a real cancel (no replacement) and
the downgrade fires normally.

Note: starting a new subscription via plain `CreateSubscription` does **not** supersede an
existing free one — the platform allows multiple concurrent subscriptions. Only plan changes
replace the running subscription.

## Edge cases

- Inherited subscriptions never trigger a downgrade (already rejected by `CancelSubscription`).
- Trialing counts as "active" when checking whether it's the last subscription.
- Verify a $0 plan produces no spurious invoice/webhook on subscription creation.

## Decisions

- **Events:** the existing cancellation + subscription-created events are sufficient; no new
  "downgraded to free tier" webhook.
- **Deferred timing:** for end-of-period / scheduled cancels, the free sub is started by the
  `POST /v1/cron/subscriptions/update-periods` job (via `UpdateBillingPeriods` →
  `processSubscriptionPeriod`) when it finalizes the cancellation. It starts at the job's run
  time, so a customer can be without an active sub for up to one cron interval. Acceptable v1.

## Open questions

**Which plan is the downgrade target?** Options:

1. **Tenant's default configured plan** — the `plan_id` from the `customer_onboarding`
   setting (`/settings/customer_onboarding`). Explicit and tenant-controlled, but that plan
   can be any plan (e.g. a paid trial), so it may not be the free tier.
2. **Any $0 plan** (current impl) — first plan with a fixed, recurring, $0 price. Always a
   free plan, but implicit and ambiguous if several exist.
3. **Default plan, else $0 plan** — prefer the configured onboarding plan; fall back to the
   $0 heuristic when onboarding isn't configured.
