# Subscription Spend Notifications — Implementation Notes & ERD Diff

**Status:** Implemented
**Author:** Tsage
**Date:** 2026-07-08
**Linear:** [FLE-899 — Configure usage alerts per subscription and line item level and group](https://linear.app/flexprice/issue/FLE-899/configure-usage-alerts-per-subscription-and-line-item-level-and-group)
**Original ERD:** [`ERDs/subscription-spend-notifications.md`](../../../ERDs/subscription-spend-notifications.md)

---

## Overview

This document records what actually shipped for FLE-899 against the original ERD, and the diff
between the two. The original ERD is left untouched — this file is the delta.

The feature adds three independent spend-alert scopes evaluated inline in the meter-usage Kafka
consumer:

- **Part A — Subscription spend:** total usage-based cost across a subscription.
- **Part B — Subscription line item spend:** usage-based cost of a single line item.
- **Part C — Group spend:** usage-based cost summed across every line item whose feature belongs
  to a given group.

All three are backed by a single new `alert_settings` table and evaluated in
`MeterUsageTrackingService.checkSpendBreachForEvent` (`internal/ee/service/meter_usage_tracking.go`),
called from `processEvent` right after `BulkInsertMeterUsage`.

---

## 1. Matches the ERD

These landed exactly as designed:

- `alert_settings` table (ent schema `ent/schema/alertsettings.go`), `alert_setting_id` column +
  index added to `alertlogs`.
- `types.AlertSettings` / `types.AlertInfo` reused unchanged; no wrapper type added.
- New constants: `AlertEntityTypeSubscription` / `AlertEntityTypeSubscriptionLineItem` /
  `AlertEntityTypeGroup`, `AlertTypeSubscriptionSpend` / `AlertTypeSubscriptionLineItemSpend` /
  `AlertTypeSubscriptionGroupSpend`, `UUID_PREFIX_ALERT_SETTINGS = "alert_set"`.
- `LogAlertRequest` gained `AlertSettingID *string` and `PeriodStart *time.Time`; `GetLatestAlert`
  gained matching parameters and branches its dedup lookup on them exactly as described (Section 6 /
  E10 in the original ERD) — period-scoped lookup when `AlertSettingID` is set, unchanged tuple
  lookup when it's `nil`.
- Evaluation is batched across all affected subscriptions — 3 `ListAlertSettings` calls total per
  event, not `3 × N`; `FeatureRepo.List` for group resolution runs only when at least one group
  config exists among the affected subscriptions.
- Evaluation short-circuits before any billing call when nothing is configured for the event's
  affected subscriptions, and again per-subscription when nothing is configured for that one
  subscription.
- `CalculateMeterUsageCharges` (invoicing-grade, commitment/overage aware) is the spend computation
  used for all three parts from a single call per subscription, not `ConvertToBillingCharges`.
- Every error inside the evaluation is logged and swallowed — never returned from `processEvent`.
- Condition validation (`above`-only for subscription-rooted rows) enforced at the service layer,
  not in the shared `types.AlertSettings.Validate()`.
- Tests added to the existing `internal/ee/service/meter_usage_tracking_test.go` following the
  codebase's existing suite/table-driven conventions, named `TestCheckSpendBreachForEvent_*`.

---

## 2. Deliberate deviations from the ERD

### 2.1 `AlertService` and `AlertLogsService` kept separate

**ERD (Section 6):** `AlertLogsService` is replaced by one `AlertService` that owns both
settings CRUD and alert logging.

**Shipped:** Two services remain, both under `internal/ee/service/`:
- `alertlogs.go` — `AlertLogsService`, unchanged, still owns `LogAlert` / `GetLatestAlert` /
  `ListAlertsByEntity` / `ListAlertLogsByFilter`.
- `alert.go` — new `AlertService`, owns only `alert_settings` CRUD
  (`CreateAlertSettings` / `UpdateAlertSettings` / `DeleteAlertSettings` / `GetAlertSettings` /
  `QueryAlertSettings`).

This was an explicit call made during implementation to avoid collapsing two already-distinct
responsibilities (settings CRUD vs. log/dedup bookkeeping) into one interface. Domain package is
`internal/domain/alert` (not `internal/domain/alertsettings` as the ERD's `alertsettings.AlertSettings`
reference implies).

### 2.2 API surface — path and list mechanism differ

**ERD (Section 7):**

| Method | Path |
|---|---|
| `POST` | `/v1/alert-settings` |
| `PUT` | `/v1/alert-settings/:id` |
| `DELETE` | `/v1/alert-settings/:id` |
| `GET` | `/v1/alert-settings` (filtered list) |

**Shipped** (`internal/api/router.go`, nested under the existing `/v1/alerts` group):

| Method | Path |
|---|---|
| `POST` | `/v1/alerts/setting` |
| `POST` | `/v1/alerts/setting/search` (filtered list — POST + body, not `GET` + query params) |
| `GET` | `/v1/alerts/setting/:id` |
| `PUT` | `/v1/alerts/setting/:id` |
| `DELETE` | `/v1/alerts/setting/:id` |

Matches the codebase's existing `POST /search` convention for filtered listing (same pattern as
`AlertLogsHandler.QueryAlertLogs` at `/v1/alerts/search`), rather than introducing a `GET` with query
binding as the ERD assumed.

### 2.3 Group webhook event name is static, not dynamic

**ERD (Section 9, 9.1):** Group alerts build the webhook event name dynamically at publish time from
the group's own (sanitized) name — `subscription.{group_name}_spend.threshold_reached` — via a
`groupSpendWebhookEventName()` helper, explicitly called out as the one non-static event name in the
codebase.

**Shipped:** Static entries, same pattern as Parts A and B (`internal/types/webhook.go`):
```go
WebhookEventSubscriptionGroupSpendThresholdReached   WebhookEventName = "subscription.group_spend.threshold_reached"
WebhookEventSubscriptionGroupSpendThresholdRecovered WebhookEventName = "subscription.group_spend.threshold_recovered"
```
No `groupSpendWebhookEventName()` helper, no `sanitize()` function, no per-group event name
collision risk (E9/14.2's naming-collision test case is moot). `group_id` and `group.name` are
available in the payload body instead, so consumers can still distinguish groups without needing a
distinct event name per group. Decided during implementation because a dynamic, user-controlled
string driving webhook subscription routing was judged an unnecessary integration surface for the
initial release.

### 2.4 Webhook payload embeds full objects, not a slim custom shape

**ERD (Section 9.2):** Each alert type gets its own small, bespoke JSON payload (subscription_id,
customer_id, threshold, current_spend, currency, period_start, triggered_at, etc.) built by hand
per alert type.

**Shipped:** One `SpendAlertEvent` shape (`internal/webhook/dto/alert.go`) shared by all three
scopes, built by `AlertPayloadBuilder.BuildPayload` → `buildSpendAlertPayload`
(`internal/webhook/payload/alert.go`), embedding full resolved objects instead of individually
flattened fields:
```go
type SpendAlertEvent struct {
    Subscription         *dto.SubscriptionResponse
    SubscriptionLineItem *subscription.SubscriptionLineItem // plain domain object, not the DTO wrapper
    Group                *dto.GroupResponse
    AlertType             types.AlertType
    AlertState            types.AlertState
    AlertSettings         *types.AlertSettings
    CurrentSpend          string
    TriggeredAt           time.Time
}
```
This matches how the pre-existing `AlertWebhookPayload` already embeds full `Feature`/`Wallet`/
`Customer` objects rather than a hand-picked subset, so spend alerts follow the same established
convention instead of introducing a second, slimmer payload style. `Subscription.Plan` is stripped
(`= nil`) before embedding to avoid known payload bloat, mirroring the existing mitigation in
`SubscriptionPayloadBuilder`. `GroupService` was wired into the payload builder's `Services`
container (`internal/webhook/payload/factory.go`, `services.go`, `internal/webhook/module.go`) to
support resolving `Group` at delivery time.

---

## 3. Bugs found and fixed during implementation

These are gaps in the ERD's own pseudocode/assumptions, surfaced by testing against a real running
system — not intentional design changes.

### 3.1 `SubRepo.Get` → `SubRepo.GetWithLineItems`

**ERD (Section 8.3):** `sub = SubRepo.Get(ctx, subscriptionID)`.

**Problem:** `CalculateMeterUsageCharges` iterates `sub.LineItems` to build any charge at all;
plain `Get` leaves `LineItems` nil, so `totalUsageCost` was always `decimal.Zero` regardless of
actual usage — Part A alerts never fired.

**Fix:** `internal/ee/service/meter_usage_tracking.go` now calls
`s.SubRepo.GetWithLineItems(ctx, subscriptionID)`, the same method the real invoice-generation path
uses. Confirmed fixed end-to-end against a live run (threshold crossing → `alertlogs` row →
webhook delivered via Svix).

### 3.2 `usageCharges[i].SubscriptionLineItemID` was never populated

**ERD (Section 8.3):** Part B/C matching assumes `usageCharges[i].SubscriptionLineItemID` is set —
`find usageCharges[i] where usageCharges[i].SubscriptionLineItemID == cfg.EntityID`.

**Problem:** `CalculateMeterUsageCharges` (`internal/ee/service/billing_meter_usage.go`) never set
`SubscriptionLineItemID` on the `dto.CreateInvoiceLineItemRequest` values it built, in either the
main per-line-item loop or `buildCumulativeCommitmentCharges`'s per-item allocation. Every Part B/C
lookup silently returned `found = false`, so line-item and group alerts never fired — with no log
line marking the skip.

**Fix:** Added `SubscriptionLineItemID: lo.ToPtr(item.ID)` (and `lo.ToPtr(bc.item.ID)` in the
cumulative-commitment path) to both append sites, matching the already-correct pattern in the
sibling/legacy path at `internal/ee/service/billing.go`. Two other `CreateInvoiceLineItemRequest`
append sites (the synthetic "True Up" / overage charges, which are plan-level, not tied to one line
item) were deliberately left without this field.

This also happened to be a real, independent invoice-correctness bug beyond the alert feature:
`internal/ee/service/invoice.go`'s update/diff logic (`if lo.IsNil(newItem.SubscriptionLineItemID) {
toInsert = ...; continue }`) relies on this field to match a recalculated charge back to an existing
invoice line item; without it, every usage-based charge computed through
`CalculateMeterUsageCharges` would have been inserted as a duplicate line item on invoice
recalculation rather than updated in place.

Confirmed fixed for Part B against a live run.

---

## 4. Known gap — not a code defect

**Group alerts (Part C) require the feature backing the relevant meter to have `group_id` set.**
This is existing, pre-ERD behavior (`Feature.GroupID`, listed in the ERD's Section 1 as something
"already exists and is reused") — not something this build changes. During live testing, a group
alert did not fire because the feature behind the touched meter had `group_id = ""`; diagnostic
logging (`"feature resolved for meter"`, `"touched groups resolved for this subscription"` in
`checkSpendBreachForEvent`) confirmed the evaluation logic correctly excludes ungrouped features
(ERD edge case E7) rather than mis-evaluating them. Resolution is a data/setup step (assign the
feature to the group), not a code change.

---

## 5. Deferred (per ERD Section 12, unchanged)

Not part of this build, as originally scoped:

- **12.1** Periodic alert-state confirmation job (closes E1 / E6).
- **12.2** Per-subscription evaluation throttling.
- **12.3** Generic alert dispatch for additional entity types.
- **12.4** Generic lookup-key resolution on `CreateAlertSettingsRequest`.
