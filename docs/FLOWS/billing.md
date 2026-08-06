# Flow: Billing

## Trigger

Billing is **scheduled and event-driven**:

- **Temporal schedules** instantiated at worker startup (`EnsureSchedules`).
- Cron-style HTTP stubs under **`/v1/cron/...`** (documented as secondary to Temporal automation).
- State transitions originating from **`SubscriptionService`** / **`BillingService`** as subscription periods advance, usage aggregates finalize, coupons apply, commitments accrue.

## Execution path (conceptual aggregation)

Billing threads multiple engines:

1. **Subscription-period evaluation** selects applicable price components (seat, tiered meter, addons, commitments) — anchored in **`internal/ee/service/billing.go`** (and specialized files like billing meter usage splits).
2. **Usage retrieval** leverages ClickHouse analytics repositories + feature trackers previously populated (`event-processing` pipeline).
3. **Proration & tax** calculators (`domain/proration` + tax services).
4. **Invoice materialization**: line items aggregated, persisted via Ent invoice repositories (**`BillingService`** + **`InvoiceService`** interplay).
5. **Payment intents / processor routing** delegated to integrations via **`IntegrationFactory`** and payment processor service.

Temporal workflows encapsulate risky multi-step arcs (billing runs, integrations sync) invoked from deeper service methods (`GetGlobalTemporalService` hotspots remain).

## Modules touched

- Core: `internal/ee/service/billing*.go`, `subscription.go`, `invoice.go`, `payment*.go`
- Temporal: workflows under `internal/temporal/workflows/subscription`, `invoice`, `cron`
- Domain: pricing, entitlement, invoice, wallet, coupons, addons
- Integrations when external sync required post-billing milestones

## Database operations

- Heavy **PostgreSQL** writes: invoices + line items, subscription phase transitions, entitlement quantities, ledger-like wallet movements (depending on product path).
- **ClickHouse reads** for usage shaping.

## External systems

- PSP connectors (Stripe, Razorpay, Paddle, Moyasar, Nomod, etc.) downstream of invoice finalization cues.
- Optional accounting/export connectors post invoice states.

## Async operations

Temporal workflows retry activities; webhook publisher emits post-state notifications.

## Failure points

| Area | Impact |
| ---- | ------- |
| Idempotency lapses around period boundaries | Double invoice risk |
| ClickHouse staleness vs PG subscription clock | Incorrect rated quantities |
| Integration timeouts | Paid state inconsistent with PSP |
| Massive billing single-file complexity | Regression risk on localized edits |

## Retry behavior

- Temporal activities configurable per registration.
- PSP operations often implement internal retry wrappers (consult specific integration packages).

## State transitions

Representative coarse states (consult domain enums/models for authoritative lists):

```
draft invoice → finalized → paid / void / credited
wallet balance adjusts with applications of credits/grants alongside invoice settlement
subscriptions advance billing periods (active ↔ paused ↔ cancelled variants)
```

## Related flows

- [invoice-lifecycle.md](invoice-lifecycle.md)
- [subscription-lifecycle.md](subscription-lifecycle.md)
