# Credit Grants in Addons — Design & Test

**Goal:** Let an addon carry credit grants (like a plan does), and deposit those credits into the wallet when the addon is attached to a subscription.

**Approach:** Add `ADDON` as a third source on the existing `credit_grants` table, then reuse the plan → subscription materialization flow as-is.

---

## What we're building

**1. Data model** — make `credit_grants` polymorphic over a third source
- New nullable `addon_id` column + `addon` edge on `credit_grants` (+ partial index `idx_addon_id_not_null`).
- New scope enum value `ADDON`.
- `addon` schema: back-reference `credit_grants` edge.

**2. Define grants on an addon** (`POST /creditgrants` with `scope=ADDON` + `addon_id`)

Adding the request field alone isn't enough — `ADDON` is rejected at three scope-gates today. Mirror every place `PLAN` is handled:
- `types/creditgrant.go`: add `CreditGrantScopeAddon = "ADDON"` to the const **and** to `Scope.Validate()`'s allowed values (first blocker).
- `dto/creditgrant.go` `CreateCreditGrantRequest`: add `AddonID *string` (`json:"addon_id"`).
- `dto/creditgrant.go` `Validate()` scope switch: add `case ADDON` → require `addon_id`, forbid `start_date`/`end_date` (template); update the `default` hint text.
- `dto/creditgrant.go` `ToCreditGrant()` scope switch: add `case ADDON` → set `AddonID`, nil out subscription/start/end/anchor (else the id is silently dropped).
- `service/creditgrant.go` `CreateCreditGrant`: add an "addon exists + published" check (mirror the plan block), and a `case ADDON` doing the uniform-conversion-rate check across the addon's grants.
- Add `GetCreditGrantsByAddon` (repo + service) and `AddonIDs` filter.
- **Do not** initialize a workflow for `scope=ADDON` (leave the SUBSCRIPTION-only branch as-is) — an addon grant is an inert template, like a plan grant.

**3. Apply grants when the addon is attached** (the core hook)
- In the attach-addon flow (`POST /subscriptions/addon`), after the association + line items are committed (inside the same tx):
  - load the addon's grants,
  - clone each into a `scope=SUBSCRIPTION` grant (keep `addon_id` for provenance),
  - run them through the existing credit-grant handler → wallet top-up path.
- Anchor = addon-attach date (mid-cycle grants apply immediately). To make this possible, `handleCreditGrants` was split into `handleCreditGrantsWithStart(startDate)` so the start can be the attach date instead of the subscription start.

**4. Handle removal**
- On addon removal / subscription cancel: cancel **future** applications of that addon's grants only (scoped via `filter.AddonIDs`). Do **not** clawback already-granted credits, and don't touch plan or other-addon grants.

**5. Surface grants in the read API (UI parity with plans)**

The dashboard renders a plan's credit grants from the plan read API. To get the same UI behavior for addons, the addon read API must expose credit grants the same way — no frontend contract invented, just mirror plans:
- `AddonResponse` gets a `CreditGrants []*CreditGrantResponse` field (like `Prices`/`Entitlements`).
- `GetAddon` / `GetAddonByLookupKey` always attach the addon's grants.
- `GetAddons` (list) expands them when the `ExpandCreditGrant` flag is set.
- Dedicated `GET /addons/:id/creditgrants` endpoint (mirrors `GET /plans/:id/creditgrants`).

This is a backend-only change: it makes the data available in the exact shape the plan page already consumes. Whether the addon page in `flexprice-front` renders it depends on that repo (out of scope for this PR).

*Reused untouched:* `credit_grant_applications`, `wallets`, `wallet_transactions`, the recurring cron.

---

## What we'll test

| # | Scenario | Expected |
|---|----------|----------|
| 1 | Create addon grant | `credit_grants` row: `scope=ADDON`, `addon_id` set, `subscription_id` NULL. Wallet unchanged. |
| 2 | Attach addon (one-time grant) | Clone row `scope=SUBSCRIPTION` (with `addon_id`), one `APPLIED` application, wallet balance up, one `wallet_transaction`. |
| 3 | Attach addon mid-cycle | Credits applied immediately, anchored to attach date. |
| 4 | Recurring addon grant → next period | Cron applies the next period's credits automatically. |
| 5 | Remove addon | Future applications for that addon cancelled; wallet balance unchanged; plan grants unaffected. |
| 6 | Attach same addon twice / retry | No double deposit (idempotency holds). |
| 7 | Validation | Reject `scope=ADDON` with missing/unpublished addon, or mismatched conversion rates. |
| 8 | Read API | Addon detail returns `credit_grants`; `GET /addons/:id/creditgrants` lists them. |

**Test types:** unit tests for validation + the clone/apply mapping (`internal/ee/service/creditgrant_test.go`); integration tests (real DB) for scenarios 2–6 end-to-end.
