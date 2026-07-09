# Razorpay UPI Autopay Auto-Charge — Implementation Handoff

Status: **In progress. Core feature (checkout registration + invoice auto-charge) implemented and committed. Reconciliation sweep deliberately being removed (YAGNI). One uncommitted revert in progress — finish it before doing anything else.**
Date: 2026-07-09
Branch: `feat/razorpay-autocharge`

Read in this order before touching anything: (1) this doc, (2) `docs/superpowers/specs/2026-07-09-razorpay-autocharge-design.md` (the design spec — authoritative for *why*), (3) `docs/superpowers/plans/2026-07-09-razorpay-autocharge-implementation.md` (the 18-task plan this work executes — note both spec and plan live under `docs/superpowers/`, which is **gitignored in this repo**, so they exist locally but were never pushed; if working from a fresh clone, these two files won't be there and this handoff doc is the only durable record).

---

## 1. Goal

Let a customer authorize a Razorpay UPI Autopay mandate at checkout, then auto-charge every subsequent invoice against it (up to its ceiling), falling back to manual payment whenever auto-charge isn't possible — through a **provider-agnostic** `CheckoutProvider` interface, with the mandate token **never stored locally** (always looked up live from Razorpay).

## 2. Immediate next action — finish an in-progress revert

Mid-session, the user decided (YAGNI) to **drop the reconciliation sweep entirely** and focus only on checkout registration + invoice finalize/auto-charge. A revert is **partially done, uncommitted**. Current `git status --porcelain`:

```
 D internal/temporal/activities/cron/razorpay_reconciliation_sweep_activities.go
 D internal/temporal/activities/cron/razorpay_reconciliation_sweep_activities_test.go
M  internal/temporal/models/cron.go
M  internal/temporal/registration.go
M  internal/temporal/service/schedules.go
 D internal/temporal/workflows/cron/razorpay_reconciliation_sweep_workflow.go
M  internal/types/schedule.go
M  internal/types/schedule_test.go
```

The 5 modified files above were reverted cleanly via `git checkout ef9d38eaa^ -- <files>` (that commit touched them exclusively, so this was a clean partial revert). **Still remaining, not yet started:**

- `internal/domain/entityintegrationmapping/repository.go` — remove `ListScopedClaimedByEntityTypesAndProvider` from the `Repository` interface and the `ScopedClaim` type (both added by commit `ef9d38eaa`, needed only by the sweep).
- `internal/repository/ent/entityintegrationmapping.go` — remove the `ListScopedClaimedByEntityTypesAndProvider` implementation (raw SQL, cross-tenant).
- `internal/testutil/inmemory_entityintegrationmapping_store.go` — remove its `ListScopedClaimedByEntityTypesAndProvider` implementation.
- `internal/integration/chargebee/item_sync_test.go` — remove `memMappingRepo`'s `ListScopedClaimedByEntityTypesAndProvider` implementation (a test mock needed to satisfy the `Repository` interface — once the interface method is gone, this goes too).
- `internal/ee/service/invoice.go` — remove the "persist `chargeResult.ProviderPaymentIntentID` onto the claim row's `Metadata["payment_id"]`" block at the end of `AutoChargeInvoice` (added by `ef9d38eaa` specifically so the sweep would have something to `Payment.Fetch` against). **Decide before removing**: this write is cheap, harmless, and forward-compatible (just persists data already in hand) — arguably fine to keep even with the sweep gone, in case reconciliation comes back later. Default to removing it too, per the user's explicit "minimize changes, remove bloat" instruction, but flag this as a judgment call, not a hard requirement.

To find the exact diff to reverse for these five files: `git show ef9d38eaa -- internal/domain/entityintegrationmapping/repository.go internal/repository/ent/entityintegrationmapping.go internal/testutil/inmemory_entityintegrationmapping_store.go internal/integration/chargebee/item_sync_test.go internal/ee/service/invoice.go`. **Caution**: a *later* commit (`99cf7340a`, "refactor(invoice): drop GetByEntity, reuse List with a filter instead") also touched these same files — do not blindly reverse-apply `ef9d38eaa`'s diff wholesale, since it would also re-add the just-removed `GetByEntity` method. Edit by hand, removing only the reconciliation-sweep-specific additions.

After finishing:
```bash
go build ./internal/...   # NOT go build ./... — see §6
go test ./internal/ee/service/... ./internal/integration/razorpay/... ./internal/types/... ./internal/repository/ent/... -v
git add -A internal/domain/entityintegrationmapping/ internal/repository/ent/entityintegrationmapping.go internal/testutil/inmemory_entityintegrationmapping_store.go internal/integration/chargebee/item_sync_test.go internal/ee/service/invoice.go internal/temporal/
git commit -m "revert: remove reconciliation sweep (YAGNI) — focus on checkout + invoice auto-charge only"
```

## 3. What's done and committed (Tasks 1–16 of the 18-task plan)

All on `feat/razorpay-autocharge`, each independently committed with tests, following the plan doc's task numbering:

| # | What | Key files |
|---|---|---|
| 1 | `ierr.ErrNotImplemented` sentinel | `internal/errors/errors.go` |
| 2 | `PaymentMethodTypeUPI`, `PaymentMethodStatusPending` enum values | `internal/types/payment.go` |
| 3 | `IntegrationEntityTypeInvoiceCharge`/`TokenCycleCharge` | `internal/types/entityintegrationmapping.go` |
| 4 | `idempotency.ScopeTokenCycleCharge` | `internal/idempotency/generator.go` |
| 5 | `SettingKeyPaymentMandateLimits`, seeded default ₹1,00,000 UPI ceiling | `internal/types/settings.go`, `internal/ee/service/settings.go` |
| 6 | `CheckoutPaymentProviderConfig`/`RazorpayPaymentProviderConfig` | `internal/types/checkout_configuration.go` |
| 7 | `CheckoutSession.payment_provider_config` (nullable jsonb column) | `ent/schema/checkoutsession.go`, `internal/domain/checkout/model.go` |
| 8 | `Invoice.CollectionMethod` (nullable), copied down at all subscription-linked invoice-creation sites (renewals **and** proration) | `internal/domain/invoice/model.go`, `internal/ee/service/{invoice,line_item_proration,subscription_modification}.go` |
| 9 | Wired `payment_provider_config` through the checkout DTO | `internal/api/dto/checkout_session.go` |
| 10 | Widened `CheckoutProvider` interface: `CreateAuthorizationLink`/`ListSavedPaymentMethods`/`ChargeSavedPaymentMethod` | `internal/interfaces/checkout_provider.go` |
| 11 | Razorpay client: `GetCustomerTokens`, `CreateAuthorizationLink`, `CreateOrder`, `CreateRecurringPayment` | `internal/integration/razorpay/client.go` |
| 12 | Implemented the 3 new methods on `razorpay.CheckoutAdapter` | `internal/integration/razorpay/mandate.go` (new file) |
| 13 | `validateMandateCeiling`/`resolveMandateCeiling` helpers | `internal/ee/service/checkout_session.go` |
| 14 | Checkout routing: `CreateAuthorizationLink` vs `CreatePaymentLink` | `internal/ee/service/checkout_session_actions.go` |
| 15 | **Core**: mandate usability check + idempotent `AutoChargeInvoice`, wired into `performFinalizeInvoiceActions` | `internal/ee/service/invoice.go` |
| 16 | Webhook: `token.confirmed/rejected/cancelled`, stop dropping `payment.authorized` | `internal/integration/razorpay/webhook/{handler,types}.go` |

**Not done**: Task 17 (reconciliation sweep) — being removed per §2. Task 18 (concurrent-autocharge integration test) — never started; blocked on Postgres (see §6).

## 4. Key decisions made (and why) — don't re-litigate these without reason

- **No `PaymentMethod` row is ever written for Razorpay tokens.** The mandate token is looked up live via `ListSavedPaymentMethods` (wraps `Token.All`) on every checkout-dedup check and every invoice-finalize usability check. Zero local storage of a live debit-authorization credential. This was the single biggest design decision, made explicitly to shrink security/compliance surface — don't reintroduce local token storage without revisiting this decision with the user first.
- **Generic 4-method provider interface**, validated against Stripe/PayPal shapes during design (not just Razorpay): `CreatePaymentLink` (existing), `CreateAuthorizationLink`, `ListSavedPaymentMethods`, `ChargeSavedPaymentMethod`. All four are **required** methods on `CheckoutProvider` (not a separate optional interface) — only Razorpay implements them today; Stripe/Moyasar don't implement `CheckoutProvider` at all currently, so no stub methods were needed there.
- **`GatewayMethodID`** is the field name for "the token/payment-method id at the gateway" everywhere — reused from the pre-existing `paymentmethod.PaymentMethod.GatewayMethodID` naming, not invented fresh (`ProviderRef`/`TokenID` were considered and rejected).
- **Settings-level UPI ceiling (`payment_mandate_limits`) is NOT the auto-charge opt-in switch.** It's seeded with a ₹1,00,000 default out of the box (not empty) — the real opt-in gate is *entirely* in the checkout request's own `collection_method`/`preferred_payment_method` fields, normalized explicitly (`normalizeCheckoutPaymentProviderConfig` defaults unset `collection_method` to `send_invoice`). This was corrected mid-design after review — earlier drafts conflated "ceiling exists" with "auto-charge enabled." Never infer this decision from `Subscription.CollectionMethod`'s own DB default (which happens to be `charge_automatically`) — that would silently opt tenants in.
- **Nullable pointer types for optional jsonb-wrapped fields.** `CheckoutSession.PaymentProviderConfig` is `*JSONBCheckoutPaymentProviderConfig`, not a value type — a code-review catch mid-implementation found the first version would have persisted an empty `'{}'` instead of SQL `NULL` for every session that doesn't use this feature. Matches the pre-existing `Result`/`ProviderResult` pointer pattern on the same struct. If you add another optional jsonb field anywhere in this codebase, check whether it needs `field.JSON(name, &types.X{})` (pointer default value — makes Ent generate a pointer Go field) vs. `field.JSON(name, types.X{})` (value field) — this distinction is easy to get wrong and only surfaces as a review finding, not a compile error.
- **Reuse `List(filter)` over adding dedicated lookup methods.** A `GetByEntity` repository method was added in Task 15, then explicitly removed per user feedback: `EntityIntegrationMappingFilter` already supports `EntityType`+`EntityID`+`ProviderTypes`, and the codebase already had an established `List`-with-filter convention for this exact lookup shape (`internal/ee/service/entityintegrationmapping_test.go`'s `TestGetByEntityAndProvider`). Don't add a new repository interface method (forcing every implementer — Ent, testutil, chargebee's test mock — to grow one) when the existing generic `List` already covers it. **Apply this same instinct going forward**: before adding a new repository method, check if `List`/`Count` with the existing filter already does the job.
- **Reconciliation sweep removed (this session, in progress).** YAGNI — the ambiguous-charge-outcome path (`AutoChargeInvoice` leaving a claim `claimed` after a submission whose final result isn't yet known) currently has **no automated resolver**. This is an accepted, explicit gap for now — see §5.

## 5. Nuances and gotchas — read before continuing work

- **A concurrent process/session is (or was) also committing to this exact branch.** Confirmed twice: commits `0294f0850` and `75fdc2f7a` both carry the identical generic message `"chore(dependencies): update Speakeasy version and Go version;"` but contain unrelated, real feature work (the second one contains this feature's `GetByEntity` scaffolding). The repo was also externally switched to `main`, reset, and pulled once mid-session, then switched back. **The user confirmed this is expected/known** — but it means: **always run `git status` / `git branch --show-current` / `git log --oneline -10` at the start of any new work session before trusting any assumption about file state**, and don't be alarmed by unfamiliar commits with generic messages — inspect their actual diff (`git show <sha>`) before deciding whether they're relevant.
- **Never run `git stash pop` blindly.** This repo has pre-existing, unrelated stash entries from other in-progress work (`feat/coupons`, `feat/generic-sdk` at last check) — popping them would corrupt whatever you're working on. If you need to stash something, use `git stash push -- <specific files>`, never a bare `git stash`/`git stash pop`.
- **`go build ./...` fails on `api/custom/go`** (a generated SDK package with pre-existing, unrelated compile errors — confirmed to predate this branch via `git log`). **Always scope builds to `go build ./internal/...`** (and `./ent/...` when ent-generated code changed). Same for `go vet`.
- **Docker/Postgres/Temporal are not running in this dev environment.** Every task so far has been build-verified and unit-tested only (pure functions: `evaluateMandateUsability`, `SelectUsableToken`, `resolveCheckoutPaymentAction`, `normalizeCheckoutPaymentProviderConfig`, `validateMandateCeiling`/`resolveMandateCeiling`; httptest-based tests for the Razorpay client wrapper). **No integration test has run against a real DB.** The transactional correctness of `AutoChargeInvoice`'s claim logic (Task 15) — the single highest-stakes piece of this whole feature — is verified by code review and reasoning only, not by an actual concurrency test. Task 18 was meant to close this gap and never got done. Before this ships, someone needs to: (a) get Postgres running (`docker compose up -d postgres` / `make dev-setup`), (b) run `make generate-migration && make migrate-ent` for the two new columns added in Tasks 7/8 (`checkout_sessions.payment_provider_config`, `invoices.collection_method`) — **migrations were never generated or applied, only the Ent schema/Go code changed** — and (c) write and run the concurrency test Task 18 specified (N goroutines racing `AutoChargeInvoice` for the same invoice, assert exactly one `ChargeSavedPaymentMethod` call).
- **A known, real (if narrow) race condition is documented inline, not fixed**: `AutoChargeInvoice`'s claim-conflict resolution (in `internal/ee/service/invoice.go`) re-reads an existing claim via a plain, unlocked `List` call, not `SELECT ... FOR UPDATE`. The design spec calls for a locked read specifically to close a race where two concurrent attempts could both observe a `"failed"` claim and both re-claim it. This is currently unreachable — **nothing in the shipped code ever sets a claim's status to `"failed"`** (only the now-removed reconciliation sweep would have) — but must be fixed before anything introduces that first `"failed"` transition. Search `internal/ee/service/invoice.go` for `KNOWN GAP` to find the exact spot.
- **Two DB migrations are outstanding and unapplied**: `checkout_sessions.payment_provider_config` (Task 7) and `invoices.collection_method` (Task 8). The Go/Ent code assumes these columns exist; they don't yet in any real database. Generate with `make generate-migration`, apply with `make migrate-ent`, once Postgres is available. **Do this before any code that reads/writes these columns runs against a real DB.**
- **Two verify-at-implementation-time items from the design spec, never resolved** (both require live Razorpay test-mode calls, which weren't available in this session):
  1. Whether `notes.flexprice_customer_id` actually propagates onto `Token` objects returned by `Token.All` (affects a planned defensive cross-check in `internal/integration/razorpay/mandate.go` — currently NOT implemented, since it couldn't be verified; see the comment there).
  2. Whether Razorpay's `subscription_registration/auth_links` and `payments/create/recurring` endpoints genuinely work with only `email` and no `contact`/phone (FlexPrice's `customer.Customer` model has no phone field at all — confirmed via `grep`). The runbook's own tested example payloads for both endpoints include `contact`. This is flagged with `VERIFY AT IMPLEMENTATION TIME` comments in `internal/integration/razorpay/mandate.go` — genuinely unverified, could be a real functional bug in the authorization-link and recurring-charge calls specifically (the pre-existing `CreatePaymentLink` precedent this mirrors is on a *different* endpoint that's confirmed to work email-only).

## 6. Testing approach used throughout — keep following this

- **TDD per task**: write the test first, confirm it fails for the right reason, implement, confirm it passes. Every task in §3 followed this.
- **Pure functions get real unit tests; I/O-heavy code gets httptest or is left for integration testing.** Where a function has no I/O (`evaluateMandateUsability`, `SelectUsableToken`, the checkout-routing decision functions, the ceiling validation helpers), it's fully unit-tested with table-driven tests. Where a function wraps an HTTP call to Razorpay (`internal/integration/razorpay/client.go`), tests use a small `baseURLOverride` seam (an unexported field on `Client`, only ever set by tests, zero-value in production) pointing the real SDK client at an `httptest.Server` — this is a new, reusable test convention introduced this session for a package that previously had zero test infrastructure.
- **No mocking framework anywhere in this codebase** — the established (and followed) convention is small, hand-rolled fake structs per test file (e.g. `fakeConnectionRepo`, `fakeEncryptionService`, `fakePaymentService` — see `internal/integration/razorpay/client_test.go` and `internal/integration/razorpay/webhook/handler_test.go`). Don't introduce `mockery`/`gomock`/etc.
- **Repository-layer and transactional logic (`AutoChargeInvoice`'s claim/lock mechanism) has NOT been tested against a real DB** — see §5. When Postgres becomes available, prioritize: (1) the migrations, (2) a real concurrency test for `AutoChargeInvoice`, (3) the claim-conflict re-read race fix (also §5), in that order — the concurrency test will likely need the race fixed first to actually pass under real contention.
- **Coverage philosophy applied**: cover the decision logic and data transformations exhaustively (every branch of every gate function has a test case); don't try to unit-test wiring/plumbing code that just threads values between already-tested pieces — that's what an eventual integration test (Task 18) is for.

## 7. Minimizing changes — principles applied, keep applying them

- Prefer extending an existing enum (`types.PaymentMethodType`, `types.PaymentMethodStatus`, `types.PaymentStatus`) over inventing a parallel one — done for every "what kind of status/method" concept in this feature.
- Prefer an existing generic mechanism (`List`+filter, the existing `cache.Locker`, the existing `DB.WithTx` transaction pattern, the existing `entity_integration_mapping` table for idempotency claims) over building new infrastructure. The only genuinely new pieces of infrastructure this feature added are: the 4-method provider interface (justified — this is the core deliverable), the `payment_mandate_limits` Settings key (justified — no existing config surface fit), and the two new nullable columns (justified — no existing field fit). Everything else reuses what was already there.
- When review found real gaps (missing `PreferredMethod` validation, wrong nullability, missed proration call sites, unnecessary API calls), fixes were applied surgically — smallest diff that closes the gap, not a rewrite.
- The reconciliation sweep (Task 17) was removed in this session specifically because it was judged to be a large chunk of new infrastructure (a Temporal workflow, an activity, a cross-tenant raw-SQL repository method, schedule registration — ~350 lines) for a concern that isn't the immediate priority. **If reconciliation is needed later, the removed code is fully recoverable from commit `ef9d38eaa`** (`git show ef9d38eaa` shows the complete implementation, which was independently reviewed as high-quality — it correctly followed the `MoyasarAuthPaymentSettlement` workflow convention, handled per-claim tenant/environment re-scoping, and never guessed an ambiguous resolution). Re-introducing it later should look like `git cherry-pick` or copying that commit's diff back, not rebuilding from scratch.

## 8. Suggested next steps, in order

1. Finish the in-progress revert (§2) and commit it.
2. Re-run the full test suite for everything touched: `go test ./internal/... -v 2>&1 | grep -E "FAIL|ok"` (scoped, not `./...`, per §6).
3. Decide whether Task 18 (concurrency integration test) and the two outstanding migrations (§5) are in scope now or explicitly deferred — they require Postgres, which wasn't available this session.
4. If deferring further work, this handoff doc plus the design spec + plan doc (both under the gitignored `docs/superpowers/`, so consider whether to copy their content somewhere tracked if this branch might be picked up from a fresh clone) are the durable record.
5. Before merging: fix the claim-conflict unlocked-read race (§5) if reconciliation or any other "failed"-status-setting path is added; apply the two migrations; verify the two open Razorpay-API questions (§5) against live test mode; run Task 18's concurrency test.
