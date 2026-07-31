# Flow: Subscription lifecycle

## Trigger

- Customer onboarding & plan checkout flows through API endpoints (`SubscriptionHandler`).
- Modifications: upgrades/downgrades captured by subscription change/modification/scheduling handlers referenced in Fx (`SubscriptionChange`, `Modification`, `Schedule`).
- Temporal cron workflows enforcing renewals/cancellations (see workflows under `internal/temporal/workflows/cron`).

## Execution path

1. **Provisioning** persists subscription aggregates + line items + phase records (PostgreSQL Ent).
2. **Entitlement derivation** aligns feature access snapshots against plan + addon overlays (`EntitlementService` synergy).
3. **Trial / paused / resumed** nuances encoded in specialized types (`types/pause_mode.go`, subscription schema mixins).
4. **Renewal ticks** orchestrated primarily through Temporal workflows & schedules — HTTP cron endpoints exist but are secondary helpers.
5. **Amendments** route through layered services (change vs modification vs schedules) emphasizing non-breaking migration of billing anchors.
6. **Cancellation & dunning semantics** interplay with alerting (`alert_logs`) and wallets where credits applied.

### Addons across a plan change

A plan change cancels the old subscription and creates a new one, so addons survive only by being re-attached. `SubscriptionChangeRequest.addon_behavior` controls this:

- `carry_over` (default) snapshots the active associations **before** cancellation — cancellation stamps `EndDate` on every association, and that field is the only signal distinguishing a onetime addon from a recurring one — then re-attaches them to the new subscription. An addon that cannot be re-attached (unpublished, no price valid for the new billing period, conflicting metered reset period) fails the whole change rather than silently dropping it.
- `drop` reproduces the historical behaviour: the addons end with the old subscription.

Both behaviours leave the cancellation proration untouched, so the unused portion of an addon is credited either way. Under `carry_over` the re-attach then charges the new period, which nets out; under `drop` the credit is a refund for coverage the customer no longer has.

`PreviewSubscriptionChange` runs the same three checks against a projected subscription and reports them per addon in `addon_impact`, so callers see a blocking failure before executing.

Dominant hotspot: **`internal/ee/service/subscription.go`** (multi-thousand-line orchestrator—see hotspots doc).

## Modules touched

- `internal/ee/service/subscription*.go`
- Related: `billing.go`, `entitlement.go`, `wallet_payment.go`
- Temporal: `internal/temporal/workflows/subscription/**`, correlated activities (`activities/subscription`)
- API: `internal/api/v1/subscription*.go`
- Persistence: subscription + phase + schedule + pause Ent schemas under `ent/schema/subscription*.go`

## Database operations

- PostgreSQL dominates (authoritative lifecycle state transitions).
- ClickHouse ancillary for usage-driven decisions during renewals/proration overlays.

## External systems

- PSP subscription mirrors (Stripe subscription sync workflows in temporal registration).
- Webhook notifications on lifecycle transitions outbound via webhook pipeline.

## Async operations

Heavy reliance on Temporal for multi-step provisioning and PSP alignment.

Kafka less central to core subscription OLTP mutations except indirect usage flows.

## Failure points

Incomplete integration sync leaving external subscription misaligned vs internal entitlement.

Concurrency on overlapping modifications (risk window during schedule application).

Historical global Temporal calls deep in service risking partial initialization in tests or mis-mode deployments.

## Retry behavior

Temporal-driven operations retried at activity granularity; PSP-specific logic may implement additional retry layers.

HTTP-level subscription endpoints remain single-shot requiring client retry semantics.

## State transitions

Approximate arcs (consult domain enums for authoritative wording):

```
pending_activation → active
active → paused
active → cancelled (immediate / end_of_period semantics)
trial → converted / expired
scheduled changes materialize affecting future phases
```

## Related flows

- [billing.md](billing.md)
- [invoice-lifecycle.md](invoice-lifecycle.md)
