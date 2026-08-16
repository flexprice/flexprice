# e2eprobe — Agent / Contributor Guide

> A long-running synthetic monitor that exercises Flexprice's public API against
> ONE fixed tenant/environment, asserts correctness on billing invariants, and
> pages Slack on failures. Add code here when you want a NEW correctness signal
> that the existing SigNoz metrics + integration tests don't already give you.

For end-user operations (env vars, deployment, checks table) see [`README.md`](README.md).
This file exists to keep new code aligned with the existing patterns and to preserve
the invariants that make the harness a reliable canary rather than a source of noise.

---

## 1. Mental model

Three abstractions carry the whole harness:

| | Interface | Concrete types | Purpose |
| --- | --- | --- | --- |
| **Unit of work** | `Check` (`check.go`) — `Name()`, `Kind()`, `Run(ctx)` | Every file in `checks/` implementing all three | The atomic thing that runs |
| **Scheduling** | `Scheduler` (`scheduler.go`) | `Ticker`, `Rate`, `OneShot`, `Listener` | When it runs |
| **Execution** | `Runner` (`runner.go`) | Only one | Owns Reporter + panic recovery + OTEL spans + heartbeats |

A check's `Kind()` classifies it for Slack routing and heartbeat rollups:

- `bootstrap` — provisions the persistent tenant fixtures (only `seed-ensure`)
- `driver` — generates continuous synthetic traffic (only `event-ingest-driver`)
- `probe` — read-side assertion, doesn't create ephemerals
- `scenario` — end-to-end flow with an ephemeral customer (create → do stuff → cleanup)
- `listener` — webhook receiver (only `low-wallet-alert-listener`)
- `maintenance` — janitor

If you're adding a new check, pick the Kind that matches — it drives failure attribution and heartbeat display.

---

## 2. Customer cohorts

There are **two** cohorts of customers, provisioned differently, cleaned up
differently, and probed differently. Getting the cohort split right is the
single most important design choice for a new check.

### 2a. Persistent (seeded once, live forever)

Provisioned by `seed-ensure` at bootstrap. Never deleted. Currently 11
customers (10 numbered + 1 canary), each with a subscription on the
`e2eprobe_plan`. All new persistent subs carry a `$5/mo` commitment
(`OverageFactor=1.5`).

| External ID | Role | Owned by | Notes |
| --- | --- | --- | --- |
| `e2eprobe-cust-persistent-0` | **Tax-attached** + **prefunded wallet** + **bucketed-meter target** | `seed-ensure` (tax association), `wallet-*-probe` (wallet), `bucketed-meter-probe` (backdated event ingest) | The shared 10% tax rate is attached to this sub's tax association. `bucketed-meter-probe` writes to this customer because it's already receiving ingest traffic and blends in naturally. |
| `e2eprobe-cust-persistent-1` | **Coupon-attached** + **prefunded wallet** | `seed-ensure` (SubscriptionCoupons at sub-create) | The shared 10% coupon is on this sub via `SubscriptionCoupons`. |
| `e2eprobe-cust-persistent-2` | **prefunded wallet** | `wallet-balance-probe`, `wallet-debit-verification` | Third prefunded wallet — no billing attachments. |
| `e2eprobe-cust-persistent-3..9` | Plain persistent | ingest driver | Receive random ingest traffic; no wallets. |
| `e2eprobe-cust-alert-canary` | **$30 wallet** with alert thresholds `{info=25, warning=10, critical=0}` | `low-balance-alert-probe`, `low-wallet-alert-listener` | Actively driven across the info threshold every 5 min. **Not in `IngestCustomerIDs`** — random traffic would pollute its known-state balance. |

`Seeds.PersistentCustomerIDs` is the full list; `Seeds.IngestCustomerIDs`
excludes the canary; `Seeds.PreFundedCustomerIDs` is `[0, 1, 2]`.

**When to use persistent:**
- Read-only invariant checks over cycle invoices (`persistent-billing-invariants-probe`)
- Anything that needs a stable identity across probe iterations (wallet balances, subscription age)
- Tests where creating a fresh customer every iteration would flood ClickHouse or the API

**Don't:** attempt to cleanup or reset a persistent customer's state between runs.
Their state IS the test surface for continuous-canary checks.

### 2b. Ephemeral (created per iteration, janitor-cleaned)

Every scenario probe creates its own ephemeral customer, registers it with
`reg.RegisterEphemeral("customer", extID, now)`, does its work, and moves on.
The janitor archives them after `E2EPROBE_JANITOR_MAX_AGE` (default 1h).

External-ID prefix is `e2eprobe-cust-eph-` plus a role suffix so they're
distinguishable in logs:

| Check | External-ID prefix | Cohort role |
| --- | --- | --- |
| `new-customer-lifecycle` | `e2eprobe-cust-eph-<unixnano>` | Bare ephemeral (customer + sub + 3 events) |
| `cancel-customer-flow` | Consumes the above | Cancels oldest ephemeral |
| `commitment-true-up-probe` | `e2eprobe-cust-eph-commit-<unixnano>` | Commitment leg — alternates under / over |
| `entitlement-enforcement-probe` | `e2eprobe-cust-eph-ent-<unixnano>` | Soft-limit enforcement |
| `tax-application-probe` | `e2eprobe-cust-eph-tax-<unixnano>` | Sub + fresh tax association + preview |
| `coupon-application-probe` | `e2eprobe-cust-eph-coupon-<unixnano>` | Sub w/ SubscriptionCoupons + preview |

**When to use ephemeral:**
- End-to-end billing verification against a preview (throwaway state — no cleanup pollution)
- Checks that need clean starting conditions per iteration
- Anything that would mutate persistent state in a way that breaks other probes

**Metadata contract for ephemerals:** every ephemeral customer gets

```yaml
metadata:
  e2eprobe: "true"
  e2eprobe_cohort: "ephemeral"
  e2eprobe_role: "<probe name>"  # e.g. "ephemeral-tax"
  e2eprobe_run_id: "<runID>"
```

The janitor's Phase-2 orphan sweep matches on external-ID prefix OR name containing "Ephemeral" OR `metadata.e2eprobe_role == "ephemeral"`. The role metadata makes ownership obvious in log searches — pick a distinctive suffix.

---

## 3. Seed resources (shared, org-scoped, immortal)

`seed-ensure` also provisions org-wide resources reused across every probe:

| Resource | Identifier | Owner | Notes |
| --- | --- | --- | --- |
| **Plan** | lookup_key `e2eprobe_plan` | `seed.ensurePlan` | All persistent subs use this plan. |
| **Features (11)** | lookup keys `e2eprobe_count_feature`, `e2eprobe_sum_feature`, ..., `e2eprobe_max_15min_feature`, `e2eprobe_sum_hour_feature`, `e2eprobe_max_day_feature` | `seed.ensureFeatures` | 8 baseline aggregations + 3 bucketed. Feature IDs in `Seeds.FeatureIDs`; bucketed subset in `Seeds.BucketedFeatureIDs`. |
| **Prices (12)** | 1 base recurring + 1 usage price per feature ($0.01/unit) | `seed.ensurePrices` | Prices are internal to the plan; not in Seeds. |
| **Plan-level soft-limit entitlements (7)** | soft-limit 100/month on each **non-bucketed** metered feature EXCEPT the reserved additive-grant feature | `seed.ensurePlanEntitlements` | IDs in `Seeds.PlanEntitlementIDs`. Soft is deliberate — hard would reject the ingest driver's traffic. Was 8 before the 2026-08-13 grants expansion. |
| **Additive grant entitlement (1)** | plan-level on `e2eprobe_sum_multiplier_feature` (grant_measure=quantity, quota=1000, duration=1h, aggregation_mode=additive) | `seed.ensureEntitlementGrants` | ID in `Seeds.GrantEntitlementIDs[AdditiveGrantFeatureLookupKey]`. Config-echo verified via raw HTTP `GetRaw` immediately after create. Reserved feature: does NOT get a soft-limit entitlement (DB rejects two non-parallel entitlements on the same (entity, feature)). |
| **Shared coupon** | code `E2EPROBE_COUPON_10PCT` (10% percentage, ONCE cadence) | `seed.ensureCoupons` | Attached to persistent cust #1 at sub-create via `SubscriptionCoupons`. Reused by `coupon-application-probe`. |
| **Shared tax rate** | code `E2EPROBE_TAX_10PCT` (10% percentage, EXTERNAL scope) | `seed.ensureTaxRates` | Attached to persistent cust #0's sub via `TaxAssociations.Create`. Reused by `tax-application-probe`. |

**Everything above is idempotent** — re-running `seed-ensure` against a
tenant that already has these entities is a no-op (with one caveat below).

### Idempotency gotchas

- **Tax rate** (`ensureTaxRates`): SDK v2.0.24's `TaxRates.GetTaxRates` is broken (server returns `{items, pagination}` but the SDK decoder expects a bare array — malformed swagger annotation on `internal/api/v1/tax.go:86`). The workaround: **skip List, try Create, swallow "already exists"**. Downstream code that needs `Seeds.SharedTaxRateID` falls back to matching by `TaxRate.Code == SharedTaxRateCode`. When you add another list-based idempotency check, verify the SDK's decoder shape matches the server's response shape first — grep `~/go/pkg/mod/github.com/flexprice/go-sdk/v2@v<VERSION>/<name>.go` for `var out .*types\.` at the 200-status branch and confirm it names a struct wrapper (`types.List<X>Response`), NOT a bare `[]types.X`.
- **Commitments / coupon on cust #1**: attached at sub-create only. Existing tenants whose sub already exists WON'T get these retro-fitted — the seed doesn't mutate existing subs (that would break `cycle-invoice-probe`'s baseline). Fresh tenants get full coverage from day one; older tenants continue running until their sub is naturally recreated.
- **Tax association on cust #0**: separate API call, so idempotent for both new and existing subs.

---

## 4. Adding a new check

**File layout**
- New probe → `internal/e2eprobe/checks/<snake_name>.go` + `_test.go`
- New sub-interface method on `Client` → `internal/e2eprobe/client.go`, `client_dryrun.go`, `client_dryrun_test.go`, `checks/fakeclient_test.go`
- Register the check name → `internal/e2eprobe/config.go` (`CheckNames` + `checkDefaultIntervals`)
- Wire it up → `cmd/e2eprobe/main.go`
- Update ops docs → `internal/e2eprobe/README.md` (checks table + self-provisioning list) + `cmd/e2eprobe/.env.example` (new env vars for enabled/interval)

**Constructor signature** — all probes share this shape:

```go
func New<Name>Probe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *<Name>Probe
```

Even if you don't use `lg` today, take the arg — every downstream call point in `main.go` passes it, and consistency matters when someone greps for all probe constructors.

**Kind()** — pick from the enum in `reporter.go`:
- `KindProbe` for read-only assertion probes (no ephemerals)
- `KindScenario` for probes that create + tear down an ephemeral
- (`KindBootstrap`, `KindDriver`, `KindListener`, `KindMaintenance` are already fully owned by the existing checks — don't create new ones without discussing.)

**Error wrapping** — every returned error goes through:

```go
return e2eprobe.Errorf(map[string]string{
    "step":                 "<verb_noun>",             // e.g. "create_sub"
    "external_customer_id": ext,
    "subscription_id":      subID,
    // whichever IDs are known at this point in the flow
}, "human-readable message: %w", err)
```

The `step` tag is what on-call reads first in Slack — pick a distinct verb per code path (`ingest`, `raw_verify`, `analytics_poll`, `preview`, `assert_taxes_present`, `assert_tax_math`, etc). Consistency helps grep across probes.

**Soft-skips** — a probe returning `nil` when its prerequisites aren't met yet is the correct behaviour (seed hasn't finished, dependent feature missing, no invoice cycle has fired yet, ephemeral customer archived mid-run by janitor). Never return an error for "prerequisites not ready" — it turns startup into a Slack storm.

Common soft-skip conditions:

```go
if len(seeds.PlanIDs) == 0 {
    return nil // seed hasn't run yet
}
if seeds.SharedTaxRateCode == "" {
    return nil // depends on the tax rate seed step, skip if not done
}
```

**Polling** — for anything async (aggregation lag, cycle invoice generation), use a bounded poll:

```go
deadline := time.Now().Add(90 * time.Second)
for {
    // ... attempt the check ...
    if success { return nil }
    if time.Now().After(deadline) {
        return e2eprobe.Errorf(map[string]string{"step": "poll_<x>", ...}, "poll timed out")
    }
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
    }
}
```

Never call `time.Sleep` at the top of a Run — the ticker already spaces iterations.

**Logging invariants (loglint enforced by `make lint-ci`):**
- Every `logger.Error(ctx, msg, fields...)` MUST include a literal `"error"` key. Wrong: `s.logger.Error(ctx, err.Error())`. Right: `s.logger.Error(ctx, "failed X", "error", err, "id", id)`.
- `logger.Warn` is reserved for bootstrap paths (config parse warnings, seed setup). In a probe's Run() use `logger.Info` for recovered/skipped conditions or `return e2eprobe.Errorf(...)` for actual failures.
- Don't log at Info from a hot loop (per-event, per-poll) — the ingest driver already spams; probes should log at most once per Run.

**404 handling** — use `isNotFound(err)` (janitor.go). It matches both `*sdkerrors.APIError{StatusCode:404}` (fall-through 4xx path) and `*sdkerrors.ErrorResponse{HTTPStatusCode:404}` (explicit 404-branch handlers like `GetCustomerByExternalID`). Regressing to only-`APIError` matching caused a real Slack storm — see commit `b2ac4985f`.

**409 / "already exists" handling** — use `isAlreadyExists(err)` (seed_ensure.go). Matches `*sdkerrors.ErrorResponse{HTTPStatusCode:409}`, `ErrorCodeAlreadyExists`, or the generic APIError 4xx body containing `"code":"already_exists"`. Use this for **create-only idempotency** where a list-based check isn't feasible (see the tax-rate workaround in section 3).

---

## 5. Client interface conventions

`internal/e2eprobe/client.go` wraps the SDK behind an in-repo `Client`
interface so tests can inject fakes. Every SDK call in probe code goes
through this wrapper.

**Adding a method:**
1. Add the method to the sub-interface (e.g. `SubscriptionOps`).
2. Add the sdkClient adapter — always a one-line pass-through: `func (o subscriptionOps) Foo(...) (...) { return o.s.Foo(...) }`. No logic in adapters.
3. Add the dry-run adapter — read methods pass through, mutating methods log-and-noop. Follow the existing pattern in `client_dryrun.go`.
4. Extend `checks/fakeclient_test.go` and `client_dryrun_test.go` — both files have a `fakeInnerClient` implementing `Client` end-to-end and must compile after your interface change.

**Adding a whole new sub-interface** (Coupons, TaxRates, etc.):
- Define the interface in `client.go`.
- Add the `sdkClient.<X>()` accessor.
- Add the adapter struct + methods.
- Add the dry-run struct + methods to `client_dryrun.go` and its 5 accessors on `dryRunClient`.
- Add a fake struct to `checks/fakeclient_test.go` + the `fakeClient.<X>()` accessor + field. Fake fields default to sensible zero-values; add injectable-response fields (`getErr`, `queryResp`, `listResp`, ...) only when a test needs to steer behaviour.
- Add the same fake in `client_dryrun_test.go` (different fake type — `fakeInnerClient` there).

---

## 6. Testing conventions

- Tests live alongside implementation (`bucketed_meter_probe.go` + `bucketed_meter_probe_test.go`).
- Use the `fakeClient` in `checks/fakeclient_test.go` — no network, no ClickHouse, no goroutines. If your test needs behaviour the fake doesn't have, add an injectable field to the fake (there's a pattern: `<method>Err`, `<method>Resp`, `<method>Calls`).
- Test the four failure modes explicitly:
  - Happy path (all deps present, correct response) → returns nil.
  - Missing seeds (e.g. `PlanIDs` empty) → soft-skip, no error, no side effects.
  - Wrong response shape / wrong values → returns error with correct `step` tag.
  - Ephemeral archived mid-run (isNotFound on GetByExternalID) → soft-skip.
- Never call the real Flexprice API from a unit test — the probe IS the integration test in production.
- For seed idempotency tests: the fake doesn't dedupe by lookup key on repeat `Run()` calls (it's stateless-per-Run for most operations). Simulate the second-run scenario by injecting the appropriate list-response or duplicate-error on the fake between the two Run() calls. See `TestSeedEnsure_TaxRateAlreadyExistsSwallowed` for the pattern.

---

## 7. SDK-server drift — verifying before use

The SDK is Speakeasy-generated from the server's Swagger annotations.
Malformed annotations produce broken clients. Two known instances (as of
SDK v2.0.24 + main HEAD):

1. **`TaxRates.GetTaxRates`** — annotation was `@Success 200 {object} []dto.TaxRateResponse` (malformed — `{object}` with an array type). Server returns `{items, pagination}` but SDK expects bare array. Server annotation is fixed on main (`internal/api/v1/tax.go:86`), but pinned SDK v2.0.24 is still broken → workaround in seed. See section 3 for full context.
2. **`CreateEntitlementRequest`** doesn't expose grant fields (`grant_measure`, `grant_duration_value`, `grant_duration_unit`, `grant_quota`, `aggregation_mode`) though the server accepts them. **Worked around** in the grants coverage (2026-08-13) via `EntitlementOps.CreateWithGrant` (raw HTTP POST) and `EntitlementOps.GetRaw` (raw HTTP GET) — see `client.go` and `seed_ensure.go:ensureEntitlementGrants`. When the SDK regenerates against the current server swagger, delete these methods and use the standard `Create` with the extended request struct.

**When adding a new SDK-call to a probe:** before you rely on any decoder, read the relevant switch branch in the SDK's method (e.g. `taxrates.go` line 207-212). Confirm the type in `var out ...` matches what the server actually returns. If they diverge, file a swagger fix for a future regen AND add a workaround in the probe.

---

## 8. Failure surfacing

Every check failure fans out to:
- Structured log line (JSON, via `internal/logger`).
- Slack post (via `SlackReporter`) — includes `tenant_id`, `environment_id` (when set), plus all the per-check attributes from your `e2eprobe.Errorf(...)` map.
- OTEL span with `kind=Error`.

Slack routing lives in `reporter_slack.go`; the Kind is displayed as a badge on the Slack message. Choose it deliberately — grouping bad choices are hard to un-groove after the on-call learns the pattern.

Heartbeat lines (default hourly) are emitted by the Runner and include
per-check success/total counters. If your check has a long-tail run
(over 5 minutes typical), don't be surprised if it registers 0/1 during
its first heartbeat — that's normal.

---

## 9. Common pitfalls

| Pitfall | Reality |
| --- | --- |
| "I'll just cleanup my ephemeral customer inline before Run() returns" | The janitor already does this on a 1h delay. Adding inline cleanup means a probe failure between create and cleanup leaks an ephemeral — janitor recovers it, but you pay double API traffic on the happy path. Register the ephemeral and let the janitor handle it. |
| "The fake test hits fresh IDs on the second Run, my idempotency test breaks" | The fake IS stateless — it doesn't dedupe by lookup key. Inject the appropriate list-response or duplicate-error between Run() calls to simulate the real API's server-side dedup. |
| "I need to reset a persistent customer's wallet balance to test X" | You can't — the balance is a continuous canary for `wallet-balance-probe`. Create an ephemeral. |
| "I'll return an error when seeds aren't ready yet" | Startup Slack storm. Return nil (soft-skip). |
| "Increased log verbosity to Debug for this one probe" | Global `E2EPROBE_LOG_LEVEL` at Info in prod; Debug is for local staging debugging only. Don't ship code that assumes Debug. |
| "This test needs a real ClickHouse" | The probe IS the integration test in production. Unit tests must run on fakes only. |
| "My probe's error text just says 'preview failed'" | Include the `step`, the customer id, sub id, and the specific field that mismatched. On-call reads these first. |

---

## 10. Cross-references

- Original design: `docs/superpowers/specs/2026-06-10-synthetic-api-probe-design.md`
- Billing-coverage expansion (bucketed meters, commitments, entitlements, tax, coupons): `docs/superpowers/specs/2026-08-12-e2eprobe-billing-coverage-expansion-design.md`
- Implementation plan for the expansion: `docs/superpowers/plans/2026-08-12-e2eprobe-billing-coverage-expansion.md`
- Loglint rules (LL006 etc): `internal/logger/README.md`
- Global rules (loglint gate, tenant-id filter, ClickHouse memory limits): repo-root `AGENTS.md` and `CLAUDE.md`
