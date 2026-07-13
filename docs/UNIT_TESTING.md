# Unit Testing Guidelines

> How we write, structure, and measure unit tests in the Flexprice backend.
> Scope: service-layer unit tests (`internal/ee/service/`). Integration/E2E tests (real DBs, testcontainers, `internal/e2eprobe/`) are out of scope here.
>
> Derived from the actual conventions in the codebase as of 2026-07-03 (~1,750 passing test cases in `internal/ee/service`). Coverage snapshot at the end.

---

## 1. Running tests

```bash
# Everything (what CI runs — .github/workflows/test.yml)
make test                          # go test -v -race ./internal/...

# One package
go test -race ./internal/ee/service/...

# One test (and its subtests)
go test -v -race ./internal/ee/service -run TestBillingService

# One table case / subtest
go test -v -race ./internal/ee/service -run 'TestCreateMeter/empty_meter_name'

# Coverage for the service layer
go test -coverprofile=coverage.out ./internal/ee/service/...
go tool cover -func=coverage.out | tail -1     # total %
go tool cover -html=coverage.out               # line-by-line view
```

Rules:
- Tests must pass with `-race`. No sleeps, no goroutine leaks, no shared mutable globals.
- Tests must not require Docker, Postgres, ClickHouse, Kafka, or network. Unit tests run entirely in-memory.
- Test files live alongside the implementation: `foo.go` → `foo_test.go`, same package (`package service`, not `service_test`).

## 2. Test architecture — how services are tested here

We do **not** use gomock/mockery. The whole service layer is tested against **hand-written in-memory repository implementations** in `internal/testutil/` (`inmemory_*.go`, 50+ stores). These implement the real domain repository interfaces, so a service under test runs its real business logic against a fake persistence layer.

The standard harness is `testutil.BaseServiceTestSuite` (`internal/testutil/base_service_suite.go`):

- `SetupTest()` builds a **fresh set of stores per test** (isolation is per-test, not per-suite), a tenant/user/environment/request-scoped context (environment = `testutil.TestEnvironmentID`, so env filtering is actually exercised), an in-memory event publisher, webhook publisher, mock Postgres client, mock PDF generator, and the integration factory.
- Accessors: `s.GetContext()`, `s.GetStores()`, `s.GetLogger()`, `s.GetConfig()`, `s.GetDB()`, `s.GetPublisher()`, `s.GetWebhookPublisher()`, `s.GetCalculator()`, `s.GetNow()`, `s.GetUUID()`.
- `TearDownTest()` clears all stores.

Other test doubles when you need them: `MockPostgresClient`, `MockPDFGenerator`, `MockHTTPClient` (all in `internal/testutil/`). If a dependency has no in-memory store yet, **add one to `internal/testutil/`** (and register it in `Stores` + `setupStores` + `clearStores`) rather than hand-rolling a one-off mock in the test file.

### Canonical suite skeleton

```go
type CouponServiceSuite struct {
    testutil.BaseServiceTestSuite
    service  CouponService
    testData struct {
        customer *customer.Customer
        plan     *plan.Plan
        now      time.Time
    }
}

func TestCouponService(t *testing.T) {
    suite.Run(t, new(CouponServiceSuite))
}

func (s *CouponServiceSuite) SetupTest() {
    s.BaseServiceTestSuite.SetupTest()
    s.setupService()
    s.setupTestData()
}

func (s *CouponServiceSuite) setupService() {
    // newTestServiceParams (internal/ee/service/testsupport_test.go) returns a
    // fully-wired ServiceParams backed by the suite's in-memory stores.
    // Do NOT hand-roll partial ServiceParams structs — services call sibling
    // services internally, and an unwired repo shows up as a nil-pointer panic
    // deep in a code path instead of a clear failure.
    s.service = NewCouponService(newTestServiceParams(&s.BaseServiceTestSuite))
}
```

Reference implementations: `billing_test.go` (suite + `testData` struct), `price_test.go` (suite + table-driven), `entitlement_test.go`.

### Test data

- Create fixtures through the repository interfaces in `setupTestData()` / helpers — never by poking store internals: `s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))`.
- Group shared fixtures in a nested `testData` struct (`s.testData.customer`, `s.testData.meters.apiCalls`, `s.testData.prices.fixed`) so test bodies read declaratively.
- Case-specific data belongs in the table case or a small local helper closure (see `subscription_test.go`'s `createAddonWithUsagePrice`), not in the shared setup.
- For plain-function tests without the suite, get a context from `testutil.SetupContext()` — it carries tenant, user, request ID, **and** `env_sandbox` environment ID. Never use a bare `context.Background()`: services resolve tenant/environment from the context, and a bare context silently tests the wrong thing (see the multi-tenancy invariant in `AGENTS.md`).

## 3. Table-driven tests — the default style

Table-driven with subtests is the preferred shape for any method with more than one scenario. Every case must be **nameable, independent, and readable in isolation**.

```go
func (s *MeterServiceSuite) TestCreateMeter() {
    testCases := []struct {
        name          string
        input         *meter.Meter
        expectedError bool
    }{
        {
            name:          "valid_meter",
            input:         validMeter(),
            expectedError: false,
        },
        {
            name:          "empty_event_name",
            input:         meterWithoutEventName(),
            expectedError: true,
        },
    }

    for _, tc := range testCases {
        s.Run(tc.name, func() {
            err := s.service.CreateMeter(s.GetContext(), tc.input)
            if tc.expectedError {
                s.Error(err)
                return
            }
            s.NoError(err)
            stored, err := s.GetStores().MeterRepo.GetMeter(s.GetContext(), tc.input.ID)
            s.NoError(err)
            s.Equal(tc.input.EventName, stored.EventName)
        })
    }
}
```

Conventions:
- **Case names** are snake_case, behavior-describing sentences: `effective_from_before_price_start_date_returns_error`, not `case_3`. They must be greppable and runnable via `-run 'TestX/case_name'`.
- Always run cases via `s.Run(...)` / `t.Run(...)` — never a bare loop of assertions (a bare loop reports one failure for N cases and can't be run individually).
- Prefer explicit `expected` fields (`expectedError bool`, or better `expectedErrCode string`, `want *dto.X`) over asserting inside per-case closures. Use a per-case `setup func()` field only when cases genuinely need different fixtures.
- Assert **outcomes, not implementation**: after a write, read back through the repo and check state; for calculations, check the numbers. Don't assert log output or internal call ordering.
- Cover, at minimum: the happy path, each validation failure, the not-found path, and the **idempotent/"already done" path** — the service constitution requires every write path to handle "already done" gracefully, so tests must exercise it (e.g., recomputing an already-computed invoice must not double-bill).

Gold-standard examples to imitate: `meter_test.go` (crisp, ~250 lines), `price_test.go` (regression-focused cases with clear names), `billing_rounding_test.go`.

## 4. Assertions

- **testify only** (`stretchr/testify v1.11.1`). In suites use the suite's embedded assertions: `s.NoError(err)`, `s.Equal(want, got)`, `s.Len(items, 2)`, `s.ElementsMatch(...)`. In plain functions use `assert`/`require` with `require` for preconditions that make the rest of the test meaningless (`require.NoError(t, err)` before dereferencing a result).
- **Never panic-check by omission**: check the error before using the result.
- **Decimals** (`shopspring/decimal`): compare with `.Equal()`, never `==` or float conversion: `s.True(got.Amount.Equal(decimal.NewFromInt(10)))`. Construct fixtures with `decimal.NewFromInt` / `decimal.RequireFromString("0.05")`.
- **Errors**: when the service returns typed errors (`ierr` codes), assert the code/behavior, not the message string.
- **Time**: capture one `now := time.Now().UTC()` (or `s.GetNow()`) at setup and derive all fixture timestamps from it. Never assert against a second `time.Now()` taken later, and never compare timestamps with `Equal` across DB round-trips without truncation awareness. All test times are UTC.

## 5. Anti-patterns (blocked in review)

| Anti-pattern | Why | Instead |
|---|---|---|
| Empty placeholder test files (`event_post_processing_test.go` is literally `package service`) | Fakes coverage signal, satisfies nothing | Write the tests or delete the file |
| Monolithic test files (`billing_test.go` is 206 KB / ~6,500 lines) | Unnavigable, merge-conflict magnet | Split by concern: `billing_rounding_test.go`, `billing_commitment_test.go`, `billing_onetime_test.go` already model this |
| Bare `context.Background()` in a service call | Loses tenant/environment scoping — tests a code path production never runs | `testutil.SetupContext()` or `s.GetContext()` |
| One-off hand-rolled mock inside a test file for a shared dependency | Diverges from the real interface, duplicated across files | Add/extend an in-memory store in `internal/testutil/` |
| Asserting only "no error" on a write | The write may have done nothing | Read back through the repo and assert state |
| Copy-pasting a 100-line setup between test funcs | Drift + noise | Hoist into `setupTestData()` or a named helper |
| Sleeps / real timers / wall-clock dependence | Flaky under `-race` and CI load | Inject/capture time once at setup |
| Testing through unexported internals | Locks implementation | Test through the exported service interface |

## 6. Checklist for a new/changed service method

1. Test file exists next to the implementation, same package.
2. Suite embeds `testutil.BaseServiceTestSuite`; service constructed from `ServiceParams` with in-memory stores.
3. Table-driven cases named after behavior; each runnable in isolation.
4. Happy path + every validation branch + not-found + idempotency ("already done") covered.
5. Writes verified by reading back; decimals via `.Equal()`; times derived from one captured `now`.
6. `go test -race ./internal/ee/service/...` green; no Docker/network needed.
7. New shared fakes go in `internal/testutil/`, registered in `Stores`/`setupStores`/`clearStores`.

---

## 7. Coverage state — `internal/ee/service`

**Snapshot: 2026-07-03 (post coverage push)** · `go test -race -coverprofile ./internal/ee/service/...`

| Metric | Value |
|---|---|
| Total statement coverage (as-is) | **54.8%** (13,331 / 24,310 statements) |
| **Core coverage** (excl. `feature_usage_tracking.go`, `event_post_processing.go`, `costsheet_usage_tracking.go` — slated for removal) | **61.1%** (13,148 / 21,534) |
| Test cases passing (incl. subtests, `-race`) | 2,801 |
| Baseline before the push (same day) | 39.5% overall / 42.6% core, 1,757 cases |

The 2026-07-03 push added ~1,050 table-driven cases across 25 new test files covering the billing core: subscription lifecycle/trial/schedule, invoice lifecycle/preview/PDF, billing usage & commitments, billing meter usage, wallet lifecycle/transactions/expiry, payments & payment processing, credit grants, plan/price/priceunit, tax, and coupon validation. Per-file movement: tax 11→85%, coupon_validation 1→96%, payment 32→89%, billing 59→83%, billing_meter_usage 0→84%, creditgrant 63→81%, wallet 50→78%, subscription_payment_processor 49→75%, subscription 51→72%, plan 15→72%, invoice 39→71%, price 67→86%, priceunit 0→90%, subscription_trial 21→88%, subscription_schedule 0→80%, wallet_payment 71→95%, payment_processor 25→63%.

### Biggest remaining levers toward 80% (uncovered statements, excl. files being removed)

| File | Coverage | Uncovered stmts |
|---|---|---|
| subscription.go | 71.9% | 872 |
| invoice.go | 70.6% | 524 |
| oauth.go | 0.0% | 353 |
| connection.go | 0.0% | 331 |
| task.go | 41.0% | 317 |
| scheduled_task.go | 0.0% | 280 |
| meter_usage.go | 70.6% | 276 |
| wallet.go | 78.2% | 252 |
| onboarding.go | 8.9% | 246 |
| billing.go | 83.0% | 242 |
| event.go | 36.5% | 195 |
| dashboard.go | 0.0% | 191 |

Much of the residual gap in the well-covered files is (a) external-integration sync paths (Stripe/Razorpay/Chargebee/QuickBooks/Zoho/Paddle/HubSpot) and Temporal dispatch blocks, and (b) repo-error logging branches that the in-memory stores cannot be made to fail. Getting those needs fault-injecting store doubles and an injectable Temporal test service in `internal/testutil/` — build those before chasing the long tail.

### Full per-file coverage

<details>
<summary>All 89 files (covered / total statements)</summary>

| File | Coverage | Stmts |
|---|---|---|
| env_access.go | 100.0% | 24 / 24 |
| coupon_validation.go | 96.2% | 76 / 79 |
| wallet_payment.go | 95.0% | 152 / 160 |
| coupon.go | 91.1% | 51 / 56 |
| priceunit.go | 89.5% | 51 / 57 |
| payment.go | 89.3% | 175 / 196 |
| subscription_trial.go | 88.3% | 106 / 120 |
| line_item_proration.go | 87.8% | 79 / 90 |
| credit_adjustment.go | 87.5% | 77 / 88 |
| price.go | 85.8% | 518 / 604 |
| tax.go | 85.4% | 340 / 398 |
| sync/export/credit_usage_report.go | 85.0% | 51 / 60 |
| subscription_grouped_invoicing.go | 84.7% | 50 / 59 |
| billing_meter_usage.go | 83.6% | 225 / 269 |
| subscription_modification.go | 83.5% | 410 / 491 |
| sync/export/usage_analytics_export.go | 83.1% | 74 / 89 |
| billing.go | 83.0% | 1182 / 1424 |
| creditgrant.go | 81.0% | 366 / 452 |
| subscription_phase.go | 80.6% | 58 / 72 |
| subscription_schedule.go | 79.5% | 120 / 151 |
| billing_commitment.go | 79.1% | 148 / 187 |
| entitlement.go | 78.4% | 236 / 301 |
| wallet.go | 78.2% | 904 / 1156 |
| subscription_payment_processor.go | 75.4% | 196 / 260 |
| subscription.go | 71.9% | 2228 / 3100 |
| plan.go | 71.8% | 356 / 496 |
| secret.go | 71.4% | 90 / 126 |
| meter.go | 71.1% | 54 / 76 |
| meter_usage.go | 70.6% | 664 / 940 |
| invoice.go | 70.6% | 1259 / 1783 |
| coupon_association.go | 70.1% | 82 / 117 |
| coupon_application.go | 70.1% | 82 / 117 |
| subscription_line_item.go | 69.6% | 265 / 381 |
| creditnote.go | 69.0% | 209 / 303 |
| subscription_modification_tax.go | 65.6% | 59 / 90 |
| subscription_modification_coupon.go | 65.0% | 67 / 103 |
| environment.go | 64.2% | 43 / 67 |
| gemini_pricing.go | 63.4% | 64 / 101 |
| payment_processor.go | 62.5% | 212 / 339 |
| raw_event_consumption.go | 61.5% | 72 / 117 |
| feature.go | 61.5% | 158 / 257 |
| subscription_change.go | 61.2% | 213 / 348 |
| user.go | 59.2% | 100 / 169 |
| subscription_modification_trial_end.go | 57.1% | 44 / 77 |
| sync/export/event_export.go | 55.8% | 53 / 95 |
| customer.go | 55.8% | 110 / 197 |
| proration.go | 55.1% | 136 / 247 |
| subscription_state_handler.go | 50.0% | 6 / 12 |
| auth.go | 50.0% | 21 / 42 |
| addon.go | 48.6% | 105 / 216 |
| entityintegrationmapping.go | 48.5% | 64 / 132 |
| alertlogs.go | 42.2% | 43 / 102 |
| task.go | 41.0% | 220 / 537 |
| streaming_processor.go | 40.6% | 78 / 192 |
| event.go | 36.5% | 112 / 307 |
| file_provider.go | 35.3% | 24 / 68 |
| meter_usage_tracking.go | 28.8% | 51 / 177 |
| group.go | 26.4% | 29 / 110 |
| usage_benchmark_analytics.go | 25.5% | 25 / 98 |
| wallet_balance_alert.go | 24.7% | 19 / 77 |
| settings.go | 13.6% | 15 / 110 |
| tenant.go | 12.7% | 14 / 110 |
| feature_usage_tracking.go | 10.7% | 183 / 1704 — being removed |
| csv_processor.go | 10.0% | 1 / 10 |
| onboarding.go | 8.9% | 24 / 270 |
| file_processor.go | 6.4% | 7 / 110 |
| json_processor.go | 2.0% | 1 / 49 |
| workflow_execution.go | 0.0% | 0 / 35 |
| workflow.go | 0.0% | 0 / 123 |
| usage_benchmark.go | 0.0% | 0 / 81 |
| sync/export/invoice_export.go | 0.0% | 0 / 49 |
| sync/export/credit_topup_export.go | 0.0% | 0 / 34 |
| sync/export/base.go | 0.0% | 0 / 59 |
| subscription_modification_grouped.go | 0.0% | 0 / 38 |
| scheduled_task.go | 0.0% | 0 / 280 |
| revenue_analytics.go | 0.0% | 0 / 41 |
| raw_events_reprocessing.go | 0.0% | 0 / 98 |
| oauth.go | 0.0% | 0 / 353 |
| integration_sync.go | 0.0% | 0 / 38 |
| factory.go | 0.0% | 0 / 1 |
| event_post_processing.go | 0.0% | 0 / 472 — being removed |
| event_consumption.go | 0.0% | 0 / 101 |
| dashboard.go | 0.0% | 0 / 191 |
| customer_portal.go | 0.0% | 0 / 134 |
| costsheet_usage_tracking.go | 0.0% | 0 / 600 — ingestion being removed |
| costsheet.go | 0.0% | 0 / 104 |
| connection.go | 0.0% | 0 / 331 |
| checkout_session_actions.go | 0.0% | 0 / 72 |
| checkout_session.go | 0.0% | 0 / 153 |

</details>

### Suggested attack order for the 80% goal

1. **Deepen the core** — `subscription.go` (872 uncovered) and `invoice.go` (524): the remainder is mostly integration-sync and analytics-mapper paths; needs fault-injecting doubles + an injectable Temporal test service in `internal/testutil/` first.
2. **Mid-size untested services** — `task.go` (import pipeline), `event.go`, `meter_usage.go`, `dashboard.go`, `customer_portal.go`, `checkout_session*.go`: mostly in-memory-testable today.
3. **Infra-dependent 0% files** — `oauth.go`, `connection.go`, `scheduled_task.go`, `onboarding.go`, sync/export: need new doubles (HTTP client fixtures, scheduled-task store, workflow-execution store).
4. **Long tail** — `settings.go`, `tenant.go`, `group.go`, `alertlogs.go`, processors.

To refresh this section: rerun the coverage command above and regenerate the tables.
