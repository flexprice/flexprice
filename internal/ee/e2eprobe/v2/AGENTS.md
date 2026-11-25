# e2eprobe — contributor guide

> One long-running process, one tenant, one replica. Personas are shared
> mutable fixtures; scenarios compare the API against a deterministic schedule;
> incidents, not runs, reach Slack. Add code here when you want a correctness
> signal the unit and integration tests cannot give you against a live region.

Operations (env, suite file, deployment): [`README.md`](README.md).
Design and the reasoning behind every choice: `docs/design/2026-11-19-e2eprobe-redesign.md`.

## 1. Packages

| Package | Provides |
| --- | --- |
| `e2eprobe` (root) | `Kind`, `Trait`, `Result` (`Pass`, `Skip`, `Fail`, `Mismatch`), the meter catalog, the SDK `Client` and its dry-run wrapper, env `Config` |
| `suite` | the suite file: types, `Load`/`Parse`, `Validate`, `Bindings`, `Describe`, embedded `default.yaml` |
| `world` | fixtures and personas: `Provision`, `Clone`, the ephemeral registry, `Sweep` |
| `ledger` | event profiles, `Batch`, the sent-tick ledger and the `Oracle` |
| `inbox` | the webhook receiver: `Record`, `Find`, `Await`, HTTP handler and server |
| `incident` | the tracker that turns results into incidents, notifiers (log, Slack, OTEL), digest, state file |
| `scenario` | `Scenario`, `Context`, `Catalog`, `Runner`, `/status` |
| `scenarios` | one file per scenario, registered in `register.go` |
| `billingmatrix` | the proration oracle and its scenario catalog (unchanged) |
| `probetest` | the stateful fake `Client` every test package uses |
| `bootstrap` | airgapped signup bootstrap (unchanged) |

## 2. Adding a scenario

1. Create `scenarios/<name>.go` with a private struct implementing
   `scenario.Scenario`: `Name()`, `Kind()` (`KindProbe` reads a persona's state;
   `KindFlow` clones and acts; `KindMaintenance` and `KindPreflight` are
   tenant-level housekeeping), `Requires()` (traits the persona must carry; empty
   means tenant-level) and `Run(ctx, sc)`.
2. Register it with `entry(...)` in the `register<Group>` function of its file
   (or in `scenarios/register.go` for a new group) with its default `Every`.
   Finish the registration with `.checks("...")`, the one-line statement of
   what a run verifies that `describe` prints (a test fails without it). If a
   run ingests events, declare how many with `.ingests(n)` so the suite's daily
   budget counts them; prefer more units per event over more events.
3. Add it to the default suite (`suite/default.yaml`) and to
   `cmd/e2eprobe/v2/suite.example.yaml` if every region should run it.
4. Write `scenarios/<name>_test.go` using `newTestDeps(t)` and `run(t, d, s, persona)`.
   Cover: pass, a mismatch, a skip and an infrastructure error (`probetest.ServerError(503)`
   must yield a `Fail` with `Infra` true).
5. Update the scenario table in `README.md`.

Inside `Run`:

- Everything comes from `sc`: `sc.Persona()`, `sc.Client()`, `sc.World().Fixtures()`,
  `sc.Ledger()`, `sc.Inbox()`, `sc.Suite()`, `sc.Now()` (never `time.Now()`), `sc.Tick()`.
- Assert with `sc.Money`, `sc.Quantity`, `sc.Count`, `sc.Truth`; they collect, so one
  run reports every wrong value. Finish with `sc.Result(step)`.
- `sc.Skip(step, reason)` when a prerequisite is not met: `sc.Persona().Gap(trait)` is
  non-empty, no invoice exists yet, the ledger window is partial. Skips are tracked;
  a binding that skips for `skip_after` becomes a coverage incident, so a skip
  reason must say what would make it run.
- `sc.Fail(step, err)` for API errors. Infrastructure errors are classified
  automatically and feed the region-level rollup instead of paging.
- `sc.Attr(k, v)` for every id you touch; attrs show up in Slack and the digest.
- One-shot work goes through `sc.Clone(ctx)`: a throwaway customer stamped from the
  persona's traits and registered for the janitor. Never reset a persona's state.
- Polls are bounded (`deadline`, `select` on `ctx.Done()`), with interval and timeout as
  struct fields the constructor defaults, so tests shorten them.

## 3. Reads the client allows

Only endpoints backed by `meter_usage` or Postgres: `events/analytics`
(`GetUsageAnalytics`, the one ClickHouse read), subscription usage, customer
usage summary, wallet balance, invoices, previews, entitlements, alert logs,
connections, settings. Raw event listing, event lookup, `events/usage` and
`events/usage/meter` are not on the `Client` on purpose: they aggregate the
`events` table, which is sorted by time only. Bound every ClickHouse read to one
customer and one window of at most a day.

## 4. Adding a trait, a profile or a meter

- **Trait**: constant in `e2eprobe.go`, provisioning step in `world/traits.go`
  (read, then write; record a gap for what cannot be retrofitted), suite validation
  rules in `suite/validate.go`, README table.
- **Profile**: entry in `ledger/profile.go`; offsets are fractions of the tick and
  must be distinct per meter; pin the expected aggregates in `ledger/oracle_test.go`.
- **Meter**: entry in `meters.go` (lookup key and event name follow `e2eprobe_<key>`);
  `world/fixtures.go` provisions it and its price; add it to a profile and to the
  oracle test; soft-limit eligibility is `softLimitEligible`.

## 5. Load budget

`suite.Validate` computes events per day from personas, profiles and the tick
plus every enabled binding's `EventsPerRun` at its interval, and refuses a suite
above `defaults.load.max_events_per_day` (100k by default; the default suite is
about 54k). Keep new reads to one call per persona per run where you can;
`describe` prints the split between profiles and flows.

## 6. Incidents and Slack

Do not post to Slack from a scenario. Return a `Result`; the tracker decides.
Thresholds live in `defaults.alert` and per-entry `alert:` overrides.
Formatting lives in `incident/slack.go` and `incident/digest.go`.

`Notifier` methods return the delivery error. The tracker stamps
`last_notified` only on success and retries an undelivered open or reminder on
its next tick, so a sink must return a rejection rather than log and swallow
it. Infrastructure failures never page from `Observe`: the tick evaluates the
region rollup first and pages a binding only when no rollup covers it.

## 6a. Developer loop

- `/` on the probe's port is the dashboard; `/status` and `/history` are its
  JSON. The history is the tracker's bounded recent-run list per binding,
  persisted in the state file; no database behind it.

- `go run ./cmd/e2eprobe/v2 once --scenario <name> [--persona <p>] [--json]`
  provisions the suite against the configured tenant, runs one binding and
  exits 1 on failure. Slack stays quiet and incident state stays in memory.
- `run --only a,b` narrows a region's suite to the bindings under investigation.
- `E2EPROBE_DRY_RUN=true` logs every mutating call instead of sending it.
- `make test-e2eprobe-v2` runs the module's tests with the race detector.

## 7. Testing

- `probetest.New()` is a stateful fake: created customers resolve by external id,
  subscriptions snapshot plan prices into line items, wallets add top-ups,
  coupons attach at subscription create, tax associations resolve codes.
  Steer behaviour through its exported fields (`Analytics`, `Preview`,
  `Balance`, `*Err`) rather than adding branches to production code.
- `newTestDeps(t)` in `scenarios/testdeps_test.go` provisions the default suite
  against the fake and shares one clock between the fake and the probe
  (`d.now`, `d.advance`).
- Never call a live API from a unit test. `go test -tags e2eprobe_integration`
  holds the live smoke tests.
- Loglint applies: `logger.Error` needs a literal `"error"` key; `Warn` only in
  constructors and `cmd/`.

## 8. Pitfalls

| Pitfall | Reality |
| --- | --- |
| "I'll give the wallet persona an events profile" | Scheduled batches would land inside the debit poll and the expected delta stops being exact. Wallet scenarios drive their own ingest. |
| "I'll assert the current hour" | Rows land with a lag; assert the last full hour after a grace, and skip when the ledger says a batch is missing. |
| "A skip is harmless" | A skip that never clears is silent coverage loss; the tracker pages after `skip_after`. Say what unblocks it. |
| "I'll read raw events to see if ingest landed" | Scans the events table. Read `events/analytics` for the customer and window instead. |
| "This persona has two subscriptions" | Analytics items repeat per subscription; filter on `SubscriptionID == persona.SubscriptionID()`. |
| "Time to add an env var" | Optional configuration goes in the suite file; env is for secrets, endpoints and process knobs. |
| "The janitor can match ephemerals by their display name" | A name proves nothing. Only the `e2eprobe-eph-` prefix or the ephemeral cohort tag authorizes a delete; the previous generation's customers are listed as legacy, never removed. |
| "The over leg needs seven times the events" | Put more units on each event. The budget counts events; the oracle counts units. |
