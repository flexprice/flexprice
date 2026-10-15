# Implementation Plan: `SIGNED_SUM` Meters

| | |
|---|---|
| Ticket | FLE-1510 |
| PRD | [Storage-style metering with `SIGNED_SUM`](./2026-10-15-FLE-1510-signed-sum-metering-prd.md) |
| Updated | 2026-10-15 |

Seven stacked PRs, each targeting the one before it, and the first targeting `develop`. Each PR is safe to merge on its own: PR 1 already floors the billed quantity at 0, and the frontend does not expose the type until PR 6.

## Status

| PR | Branch | State |
|---|---|---|
| 1 Meter type | `ojasaggarwal/fle-1510-signed-sum-meter-type` | Built |
| 2 Checkpoint storage | `ojasaggarwal/fle-1510-signed-sum-checkpoint` | Built |
| 3 Total reader | `ojasaggarwal/fle-1510-signed-sum-total-reader` | Built |
| 4 Billing | `ojasaggarwal/fle-1510-signed-sum-billing` | Built |
| 5 Limits | `ojasaggarwal/fle-1510-signed-sum-limits` | Built |
| 6 Frontend | `flexprice-front` | Not started |
| 7 Docs | `flexprice-docs` | Not started |

Differences from the plan below, as built:

- The billing floor landed in PR 1 (`ConvertToBillingCharges`), not PR 4.
- The period-end handoff is `periodEndHandoff` in PR 4, a copy of the unmerged `LATEST` handoff under a neutral name. It applies only to `SIGNED_SUM` meters.
- With `NEVER`, analytics queries (windows, filters, group-by) still show the window's net. Only scalar billing reads take the running total.
- Saving checkpoints at invoice time never blocks the invoice: a failure is logged, and reads start from the previous checkpoint.
- The subscription entitlements API is unchanged: `grant_state` is empty for `SIGNED_SUM` features. The frontend should show the ceiling against `/customer/usage`.
- Swagger and SDK enums are not regenerated yet.

## PR 1: The meter type

| Change | Where |
|---|---|
| Add `AggregationSignedSum = "SIGNED_SUM"` to `Validate`. It requires a field and does not support expressions in v1. | `internal/types/aggregation.go` |
| Meter create and update: `SIGNED_SUM` accepts `NEVER` and `BILLING_PERIOD` | `internal/api/dto/meter.go` (`Validate`, ~:94) |
| Ingest: `extractQuantity` keeps a negative quantity only for `SIGNED_SUM`; both callers use it unchanged | `internal/ee/service/meter_usage_tracking.go` (`extractQuantity`) |
| Treat `SIGNED_SUM` as a sum in the meter-usage aggregator | `internal/repository/clickhouse/meter_usage_aggregators.go:34` |
| The older events path reads `SIGNED_SUM` as the window's net, raised to 0 when negative. Exact signed totals come only from `meter_usage`. | `internal/repository/clickhouse/aggregators.go`, and the three result scans in `event.go` |
| Analytics columns treat `SIGNED_SUM` as `SUM` | `internal/repository/clickhouse/meter_usage.go` (`buildMeterUsageAggregationColumns`) |
| **Floor on the bill**: a negative billed quantity becomes 0 | `internal/ee/service/meter_usage.go` (`ConvertToBillingCharges`) |
| Swagger and SDK enums | Deferred: `make swagger` on `main` also pulls in unrelated drift (addon price sync). Regenerate on `develop` or in the last PR. |

Grants stay off for `SIGNED_SUM` until PR 5: `grantPriceGuard` already rejects it as non-additive, so billing falls back to the entitlement path, which floors at 0.

Tests: `extractQuantity` keeps the sign for `SIGNED_SUM` and zeroes it for `SUM` and `MAX`; extraction keeps "-4.5"; a negative net bills 0; the ClickHouse mappings treat `SIGNED_SUM` as a sum.

Stored values stay exact from here on: ingestion, checkpoints and reads never floor. Only the billed quantity does.

## PR 2: Checkpoint storage (`NEVER` only)

| Change | Where |
|---|---|
| New Ent schema `meter_usage_checkpoint`: `tenant_id`, `environment_id`, `customer_id`, `meter_id`, `total` (numeric), `aggregated_until`, `invoice_id`. Unique on (tenant, environment, customer, meter, `aggregated_until`). | `ent/schema/meter_usage_checkpoint.go`, then `make generate-ent` and `make generate-migration` |
| Domain model and repository interface: `GetLatestAtOrBefore(customer, meter, t)` and an idempotent `Upsert` keyed on `aggregated_until` | `internal/domain/meterusagecheckpoint/` |
| Ent repository and an in-memory test store | `internal/repository/ent/`, `internal/testutil/` |

Every query filters on tenant and environment.

## PR 3: Reading the total (`NEVER` only)

`BILLING_PERIOD` reads need no new code: the existing period sum over `meter_usage` already returns the signed net once PR 1 stops the clamp.

| Change | Where |
|---|---|
| `SignedSumTotal(ctx, customer, meter, at)` = latest checkpoint at or before `at` + `SUM(qty_total)` over [`aggregated_until`, `at`) | `internal/ee/service/meter_usage.go` |
| No checkpoint yet: sum from the start of history (the one-time bootstrap) | Same |
| Resolve customer external ids the same way the existing meter-usage reads do | Same |

Tests: no checkpoint; a checkpoint plus later changes; a late event behind a checkpoint is ignored; the total can go below zero.

## PR 4: Billing

| Change | Where |
|---|---|
| Usage for the period: `SignedSumTotal` at period end (`NEVER`) or the net over the whole period (`BILLING_PERIOD`) | `internal/ee/service/billing_meter_usage.go` |
| Both modes: only the line item active at period end bills that usage; the ones it replaced bill 0. Reuse or generalise the `LATEST` handoff. | `latest_meter_handoff.go` (stashed in `921c9309`; it must land first or move into this PR) |
| Allowance = sum of `grant_quota` across the feature's entitlements active at period end. Skip `adjustMeterUsageEntitlement` and the grant fold for `SIGNED_SUM`. | `billing_meter_usage.go` (~:286-310) |
| **Floor on the bill only**: billed quantity = max(0, quantity − allowance), applied as the last step before pricing. Nothing upstream is floored. | Same |
| `NEVER`: write the checkpoint when the rollover invoice is created, idempotent on `aggregated_until = period end` so a recompute does not add twice | The invoice creation flow (`internal/ee/service/invoice.go`, near the draft-first step ~:2384) |

The live wallet balance (`wallet.go:3488`) calls `CalculateMeterUsageCharges`, so it follows automatically.

To verify: which usage path the invoice takes. `billing.go:2268` calls `GetUsageBySubscription`; make sure `SIGNED_SUM` uses only the `meter_usage` path.

Tests: the PRD's bill examples for both modes; a price change and a plan change bill once in both modes; a negative billed quantity becomes 0 while the checkpoint keeps the negative value; recomputing an invoice does not move the checkpoint twice.

## PR 5: Limits

| Change | Where |
|---|---|
| Entitlement validation for `SIGNED_SUM` features: `quantity` measure, `additive`, `subscription_period`, and `grant_quota` required unless unlimited | `internal/ee/service/entitlement_grant.go` (`validateEntitlementGrantShape`, ~:904) |
| Do not open grants for `SIGNED_SUM` features | `entitlement_grant.go` (`grantCandidatesForFeature` ~:623, `EnsureGrantsForSubscriptions`) and the addon path `entitlement_grant_proration.go` |
| `/customer/usage` for `SIGNED_SUM`: usage = exact total now (`NEVER`) or this period's net (`BILLING_PERIOD`), limit = sum of `grant_quota`, remaining = limit − usage | `internal/ee/service/billing.go:3034` (`GetCustomerUsageSummary`) |
| The subscription entitlements API returns the ceiling and the total, in the shape the UI expects | `GetSubscriptionEntitlements` handler and service |

## PR 6: Frontend

- Offer `SIGNED_SUM` when creating a meter, with the `reset_usage` selector (`NEVER` or `BILLING_PERIOD`) shown for it. The selector is commented out today (`AddFeature.tsx` ~:893).
- Show "ceiling vs current total" for `SIGNED_SUM` entitlements in place of the grant ledger.

## PR 7: Docs

- A metering guide page for `SIGNED_SUM`: signed events, stable ids, limits, billing at period end.

## Cutover for Optimality (operations, after PR 5)

1. Find where their historical negative changes are stored (PRD open point 1).
2. Create the `SIGNED_SUM` meter and feature, add a usage price for it on the plan (a $0 price if they only need the limit), and attach the entitlement. Without a usage price, no checkpoint is ever written (`meter_usage.go` ~:422 only queries meters on usage line items).
3. Rebuild `meter_usage` for the new meter from raw events. Check that `ReprocessRawEvents` (`raw_events_reprocessing.go:79`) produces rows for a meter created after the events arrived.
4. Write the first checkpoints with a full-history pass.
5. Compare totals with Optimality's own numbers before switching their usage check to `/customer/usage`.
