# Bonus Credit Top-Up — PERD (Post-implementation ERD)

- **Author:** Tsage
- **Date:** 2026-07-09
- **Linear:** [FLE-904 — Add support for separate free credit top-up transactions](https://linear.app/flexprice/issue/FLE-904/add-support-for-separate-free-credit-top-up-transactions)
- **Original ERD:** `ERDs/bonus_credits.md` (design doc, not checked into this repo)
- **Status:** Implemented, tests passing, pending production migration run

## Summary

Adds a separate, independently-tracked bonus credit transaction that gets created automatically
when a large **purchased** wallet top-up completes — e.g. buy 5,000 credits, get 750 bonus
credits. The bonus is its own `wallet_transaction` row (`transaction_reason =
PURCHASED_CREDIT_BONUS`), linked to the purchase via a new `parent_transaction_id` column. It
shares the purchase's fate atomically: credited the moment the purchase completes (same request
for direct/auto-complete top-ups, invoice-paid time for pending invoiced top-ups), never before.

Two additions:
- `bonus_credits_to_add` on the top-up request — explicit override, must be omitted or `> 0`.
- A tenant × environment setting, `bonus_credits_topup_config`, holding slabs that map "credits
  purchased" → "bonus credits granted", used only when the caller doesn't pass an explicit
  override.

---

## 1. Original ERD (as designed)

The full original design is preserved at `ERDs/bonus_credits.md`. Key points, for reference:

- **Schema:** one new nullable, immutable column `parent_transaction_id` on `wallet_transaction`,
  plus a composite index `(tenant_id, environment_id, parent_transaction_id, transaction_status)`.
- **New enum value:** `TransactionReasonPurchasedCreditBonus = "PURCHASED_CREDIT_BONUS"`.
- **New setting key:** `bonus_credits_topup_config`, tenant × environment scoped, default
  `{enabled: false, slabs: []}`.
- **New types:** `BonusCreditsTopupConfig`, `BonusCreditsSlab`, `BonusValueType`, `BonusValue`.
- **New operator value:** `GREATER_THAN_EQUAL FilterOperatorType = "gte"`, added to the existing
  `types.FilterOperatorType` enum and reused as `BonusCreditsSlab.Operator`'s type.
- **Resolution flow:** in `TopUpWallet`, after `req.Validate()`, if `req.BonusCreditsToAdd` is nil
  and the reason is a purchased-credit reason, resolve it from the tenant's slab config
  (`findBonusSlab` + `resolveBonusValue`), mutating `req.BonusCreditsToAdd` in place.
- **Creation:** the bonus tx is created in the **same DB transaction** as the purchase tx —
  `completed` immediately if the purchase completes immediately (direct, or invoiced
  auto-complete), `pending` otherwise.
- **Completion:** for the pending-invoice case, `completePurchasedCreditTransaction`'s **existing**
  `s.DB.WithTx` block (no new helper) looks up the pending bonus child via the new
  `GetPendingTransactionByParent` repo method and completes it in the same transaction.
- **Idempotency:** inherited for free from `completePurchasedCreditTransaction`'s existing
  pending-check — a retry against an already-`completed` tx is a no-op.

---

## 2. Deviations from the original ERD

All of these were discussed and explicitly approved during implementation — none are silent.

| # | ERD said | Implementation does | Why |
|---|---|---|---|
| 1 | `BonusCreditsSlab.Operator` validated via `validate:"required,oneof=gte"` struct tag | Explicit `if slab.Operator != GREATER_THAN_EQUAL { ... }` check inside `BonusCreditsTopupConfig.Validate()` | This codebase's convention for enum validation is an explicit check with `ierr.NewError`, not go-playground `oneof` tags — no other enum in the codebase uses `oneof` this way (`TransactionReason.Validate()`, `WalletTxReferenceType.Validate()`, etc. are all explicit) |
| 2 | `BonusValue.Type` validated via `oneof=flat percentage` | Explicit `if slab.Bonus.Type != BonusValueTypeFlat && slab.Bonus.Type != BonusValueTypePercentage { ... }` check, same location | Same reason as #1 |
| 3 | `bonus_credits_to_add: 0` is a valid, deliberate "opt-out" override (§5.1, §8.3 "explicit zero (opt-out)") | `0` (and negative) is a **validation error**; the field must be omitted entirely, or a positive value | Product decision: there is no real caller for "explicitly opt out of a slab bonus for one purchase" — if no bonus is wanted, don't pass the field at all. Simpler contract, one less state to reason about |
| 4 | `findBonusSlab` pseudocode has a `switch s.Operator { case GREATER_THAN_EQUAL: ... }` | Plain `if credits.GreaterThanOrEqual(slabs[i].Threshold)`, no switch | The operator is guaranteed to be `gte` by `BonusCreditsTopupConfig.Validate()` before any slab ever reaches this function — the switch was dead branching over an already-validated invariant |
| 5 | Function named `resolveBonusValue` | Named `resolveBonusCredits` | Cosmetic naming only, no behavior change |
| 6 | §5.2 completion pseudocode re-reads the wallet via a fresh `GetWalletByID` call for the bonus's before/after balance | Reuses the `finalBalance`/`newCreditBalance` local variables already computed for the purchase completion, two lines above, in the same DB transaction | Same value (nothing else can write in between, same tx), saves a redundant DB round-trip |
| 7 | §5.1's "create the bonus tx directly as completed, in the same DB transaction as the purchase tx" for the direct-purchase branch | Bonus creation lives as a small, nil-gated block **inside `processWalletOperation`'s existing `s.DB.WithTx` closure** (guarded by a new optional `WalletOperation.BonusCreditAmount` field), not a separate duplicated function | First attempt duplicated ~150 lines of lock/create/balance/webhook/alert logic into a standalone method to avoid touching the shared `processWalletOperation`. That was overcomplicated and inconsistent with the ERD's own stated philosophy ("a few lines added inside the existing transaction block, no new helper") — which is exactly how the completion side (§5.2) already works. Reverted to the minimal in-place approach; `BonusCreditAmount` stays `nil` (no-op) for every caller except `TopUpWallet`'s credit path |

---

## 3. Bugs found and fixed during implementation

These were not deviations from the ERD — they were mistakes introduced while implementing it,
caught either by careful re-review against the ERD or by live end-to-end testing against a
running server.

1. **Missing `Comment(...)` on the `parent_transaction_id` ent schema field.** The ERD specifies
   a doc comment on the column; it was dropped in an early pass and silently missing until a
   full re-audit against the ERD caught it. Restored, `make generate-ent` re-run.

2. **Bonus tx stuck at `pending` forever for invoiced auto-complete purchases.** In
   `handlePurchasedCreditInvoicedTransaction`, the bonus transaction was created with
   `TxStatus: types.TransactionStatusPending`, then the code mutated
   `bonusTx.TxStatus = types.TransactionStatusCompleted` **after** `CreateTransaction` had already
   persisted it — without ever calling `UpdateTransaction`. Against real Postgres this would leave
   the bonus row permanently `pending` while the wallet balance already reflected its credit. It
   passed the in-memory test suite because the test store keeps the same pointer on `Create`, so
   the later in-process mutation leaked into the "stored" copy and masked the bug. Fixed by
   setting `TxStatus: txStatus` (the same status the purchase tx got) directly in the struct
   literal at creation time.

3. **Missing hint on the slab sort-order validation error.** The "slabs must be sorted descending
   by threshold" error was the only one of the four validation branches in
   `BonusCreditsTopupConfig.Validate()` without a `.WithHint(...)`. The API's error middleware
   (`getDisplayMessage` in `internal/rest/middleware/errhandler.go`) only surfaces a hint if one is
   attached — otherwise it falls back to a generic "An unexpected error occurred", hiding the real
   cause from the API caller. Fixed by adding the hint.

4. **`bonus_credits_topup_config` was unreachable via the settings API entirely.** `GetSettingByKey`
   and `UpdateSettingByKey` in `internal/ee/service/settings.go` — the functions actually called by
   `GET`/`PUT /v1/settings/:key` — each have their own `switch key` dispatcher, separate from
   `types.ValidateSettingValue`'s switch (which *was* updated). The new key was missing from both,
   so every attempt to read or write the setting via the API failed with `"Unknown setting key:
   bonus_credits_topup_config"`, regardless of restart or payload correctness. Fixed by adding the
   missing case to both switches, and added `TestBonusCreditsTopupConfig_APIDispatch` (verified by
   temporarily reverting the fix and confirming the test fails) to close the gap.

5. **Bonus transaction's currency `Amount` field was never set on the direct-purchase path.**
   Found via live testing against a running server: the bonus row showed `"amount":
   0.000000000` next to a correct `"credit_amount": 50.000000000`. The bonus-creation block added
   inside `processWalletOperation` set `CreditAmount` but never `Amount`. Fixed by computing it the
   same way `validateWalletOperation` computes the purchase tx's own `Amount` —
   `s.GetCurrencyAmountFromCredits(*req.BonusCreditAmount, w.TopupConversionRate)` — and added an
   assertion to `TestBonusCreditsCreationAtomicity_Direct` to catch a regression.

---

## 4. Files touched

- `ent/schema/wallettransaction.go` — `parent_transaction_id` field + composite index
- `internal/types/wallet.go` — `TransactionReasonPurchasedCreditBonus`
- `internal/types/search_filter.go` — `GREATER_THAN_EQUAL` operator
- `internal/types/settings.go` — `BonusCreditsTopupConfig`, `BonusCreditsSlab`, `BonusValueType`,
  `BonusValue`, default value, `SettingKey` allow-list, `ValidateSettingValue` case
- `internal/api/dto/wallet.go` — `BonusCreditsToAdd` field + validation
- `internal/domain/wallet/transaction.go` — `ParentTransactionID` field + Ent conversions
- `internal/domain/wallet/repository.go` — `GetPendingTransactionByParent` interface method
- `internal/domain/wallet/operation.go` — `BonusCreditAmount` field on `WalletOperation`
- `internal/repository/ent/wallet.go` — `GetPendingTransactionByParent` Ent-backed implementation
- `internal/testutil/inmemory_wallet_store.go` — `GetPendingTransactionByParent` in-memory
  implementation
- `internal/ee/service/wallet.go` — resolution logic in `TopUpWallet`; bonus creation inside
  `processWalletOperation` (direct) and `handlePurchasedCreditInvoicedTransaction` (invoiced);
  bonus completion inside `completePurchasedCreditTransaction`
- `internal/ee/service/settings.go` — `GetSettingByKey`/`UpdateSettingByKey` dispatch cases
- `internal/ee/service/wallet_test.go` — all new tests (see §5)
- `docs/swagger/*` — regenerated via `make swagger`

---

## 5. Test coverage

All added to the existing `internal/ee/service/wallet_test.go` (no new test files, per project
convention — tests belong alongside the service they test, not in a parallel file):

- `TestFindBonusSlab`, `TestResolveBonusCredits` — pure-function table tests
- `TestBonusCreditsResolution_*` (6 cases) — explicit override, explicit-zero-rejected, disabled
  config, slab match/no-match, no setting row seeded
- `TestBonusCreditsCreationAtomicity_*` (4 cases) — direct, invoiced auto-complete, invoiced
  pending, zero-bonus-no-row — including the `Amount` field regression check (bug #5 above)
- `TestCompletePurchasedCreditTransaction_*` (4 cases) — happy path, no-bonus regression, retry
  idempotency, partial-failure-propagates (documents the in-memory test harness's rollback
  limitation inline)
- `TestBonusCreditsTopupConfig_APIDispatch` — closes bug #4 above, verified against a deliberate
  regression
- `TestTopUpWallet` (pre-existing) — confirmed passing unmodified, proving the default-disabled
  setting doesn't change behavior for any top-up that predates this feature

**Known gap:** the ERD's §8.1 table-driven unit tests for `BonusCreditsTopupConfig.Validate()`
(empty-slabs, non-descending, bad operator, bad bonus type, zero-threshold-catch-all, etc.) were
written once as a standalone `internal/types/settings_test.go`, then deliberately dropped per
review feedback ("service has test files, not types") without being re-homed anywhere. Validation
correctness for that type is currently only exercised indirectly, through `seedBonusConfig`'s calls
to `UpdateSetting` in the service-level tests above. The direct table-driven coverage from §8.1
does not currently exist anywhere in the tree.

All of build / `go vet` / `go test ./internal/...` pass clean at time of writing.

---

## 6. Migration

No live Postgres was available during implementation to run `make generate-migration`, so the
canonical migration file was never generated. Based on `ent/migrate/schema.go`, the expected diff
is:

```sql
ALTER TABLE "wallet_transactions" ADD COLUMN "parent_transaction_id" character varying NULL;

CREATE INDEX "idx_tenant_environment_parent_transaction_status"
    ON "wallet_transactions" ("tenant_id", "environment_id", "parent_transaction_id", "transaction_status");
```

**Action item:** run `make generate-migration` against a live dev DB to produce the canonical,
timestamped file under `migrations/ent/` before this ships to production.

---

## 7. Manual verification against a running server

Confirmed end-to-end against `make run-server`:

- `PUT /v1/settings/bonus_credits_topup_config` with slabs in descending threshold order —
  succeeds, both `GetSettingByKey`/`UpdateSettingByKey` recognize the key (post bug #4 fix).
- `POST /v1/wallets/{id}/top-up` with `transaction_reason: PURCHASED_CREDIT_DIRECT` and an
  explicit `bonus_credits_to_add` — creates two `completed` rows atomically, wallet balance
  reflects both, bonus row's `amount` correctly matches its `credit_amount` (post bug #5 fix).
- `transaction_reason: PURCHASED_CREDIT_BONUS` is correctly rejected on a top-up request — it's a
  system-only reason stamped on the auto-generated child row, never a caller-supplied top-level
  reason.
