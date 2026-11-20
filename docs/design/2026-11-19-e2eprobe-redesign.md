# e2eprobe redesign — deterministic, persona-driven synthetic monitoring

- **Date:** 2026-11-19
- **Author:** Ankit Malik
- **Status:** Approved (decisions delegated to the author)
- **Module:** `internal/ee/e2eprobe`, `cmd/e2eprobe/v2`, `internal/ee/infrastructure/helm/flexprice/templates/probe`

## 1. Intent

The probe is the only signal that exercises a live Flexprice region the way a customer does:
ingest, metering, invoicing, wallets, alerts, checkouts, webhooks. We want to depend on it for
correctness, not just liveness. Today it cannot carry that weight:

- **Noise.** Every failing tick posts to Slack. There is no notion of how long a check has been
  failing, no recovery message, no distinction between an assertion failure and an unreachable API.
  On-call learned to ignore the channel.
- **Opaque coverage.** Fixtures are positional: persistent customer 0 carries the tax rate,
  customer 1 the coupon, each probe mints its own ephemeral with a hard-coded role. The ingest
  driver sprays seeded-random events across all persistent customers and nothing records what was
  sent. No check can assert an exact total, and nobody can answer "which customer is configured with
  what, and which cases does that cover" without reading twenty files.
- **Unreliable webhook receipt.** One route, a 32-slot buffer, wallet-alert receipts only, in
  memory. Delivery wiring is a server config concern the probe cannot see; when nothing arrives,
  nothing says so.
- **Load.** Five events per second around the clock (about 430k events a day) into a tenant whose
  only purpose is coverage, and reads through `/events/query`, which scans the raw `events` table.
- **Three ways to write a check.** Plain checks, billing-matrix scenarios and payment legs each
  have their own result shape, cleanup and skip semantics.

## 2. Goals, non-goals, anti-goals

**Goals**

1. Deterministic assertions: every usage, wallet and invoice assertion compares against an oracle
   derived from what the probe itself sent, with exact amounts.
2. One way to define a check, one result shape, one place that decides when Slack hears about it.
3. Incident-based alerting: a check pages once when it has been failing past a duration and count
   threshold, reminds on a long interval, and announces recovery. Flaps and startup races never page.
4. Visibility: a single suite file states which personas exist, what each carries, which scenarios
   run on which personas, what tenant-level connections and settings are expected, and what
   webhooks must arrive. `e2eprobe describe` prints that as a coverage matrix; `/status` serves the
   live view.
5. Low, predictable load: a computed event budget, bulk ingest, and reads only through endpoints
   backed by the `meter_usage` table or Postgres.
6. Webhooks as first-class evidence: an inbox that records every delivery, lets scenarios await a
   matching event, and raises one incident when the region's delivery wiring is broken.

**Non-goals**

- Replacing unit or integration tests. The probe asserts behaviour of a deployed region.
- Multi-tenant or multi-replica operation. One probe, one tenant, one environment, one replica.
- Provisioning tenant-level connections or settings. Connections need gateway secrets and settings
  writes need a super-admin user; the probe verifies both and reports drift.
- Load or performance testing.

**Anti-goals**

- A check that can fail without naming the persona, step and expected-versus-actual values.
- A second config surface that drifts from the code (every suite entry is validated against the Go
  catalog at startup; unknown names fail fast).
- Any read against the raw `events` table.

## 3. Constraints

- **Read performance.** `events` is sorted by `(tenant, environment, timestamp, id)` with a coarse
  bloom filter on `external_customer_id`; `meter_usage` is keyed by
  `(tenant, environment, external_customer_id, meter_id, timestamp)`. The only ClickHouse read the
  probe makes is `POST /events/analytics`, which the server answers from `meter_usage` (bucketed
  meters as the sum of each bucket's aggregate). `POST /subscriptions/usage`, `GET /customers/usage`,
  `GET /wallets/{id}/balance/real-time`, entitlement, invoice, preview and the other Postgres-backed
  endpoints are allowed. Banned: `POST /events/query`, `GET /events`, `GET /events/lookup`,
  `POST /events/usage` and `POST /events/usage/meter`, which all aggregate the raw `events` table.
  The probe's `Client` interface simply does not expose the banned calls.
- **Every ClickHouse read is bounded** to one customer, named meters or features, and an explicit
  time window no wider than one day.
- **Ingest load** is declared and budgeted. Default cadence is one bulk batch per persona every
  10 minutes; the computed daily total must stay under the suite's `max_events_per_day`.
- **Deployment surface stays, twice.** The previous generation keeps its chart component, image,
  GHCR workflow and `make run-e2eprobe` untouched while this one runs beside it from
  `internal/ee/e2eprobe/v2` with its own (`e2eprobeV2.*`, `ghcr.io/flexprice/e2eprobe-v2`,
  `publish-e2eprobe-v2-image.yml`, `make run-e2eprobe-v2`). Secrets stay in env: `E2EPROBE_API_HOST`, `E2EPROBE_API_KEY`, Slack webhook or bot token,
  tenant and environment ids, bootstrap credentials. Everything optional moves to the suite file.
- **SDK v2.1.51** is the client. Fields the SDK does not carry go through `RawOps`.
- **Single replica.** Personas are shared mutable fixtures.
- **Webhook delivery** is configured server-side (`webhook.tenants.<id>` or a Svix endpoint); the
  probe cannot register an endpoint and must detect, not fix, missing wiring.
- **loglint** (`make lint-ci`) applies to this package.

## 4. Approaches considered

Three independent axes were decided. Each row lists the consequence of choosing it.

### 4.1 What is the primary unit?

| Approach | How it works | Pros | Cons | Consequence |
| --- | --- | --- | --- | --- |
| **A. Harden in place** | Keep checks, add Slack dedupe, swap `ListRaw` for `usage/meter`, keep env config | Smallest change; nothing to port | Positional fixtures stay; no oracle, so assertions stay "non-empty"; three check styles remain; coverage still opaque | Noise drops, trust does not rise. Rejected. |
| **B. Customer-first** | Declare personas with traits; every scenario is "for each persona with trait T, assert I" | Clean coverage matrix; real cycle invoices and alerts observable over months | One-shot flows (checkout, declined card, cancel, previews on fresh subs) cannot run on a long-lived persona without resetting it; resets corrupt the very history the invariants need | Either no one-shot coverage or a second mechanism anyway. Rejected alone. |
| **C. Scenario-first** | Every scenario declares the fixture shape it needs and gets a fresh ephemeral customer | Clean isolation per run; today's model made explicit | Nothing lives long enough to observe cycle invoices, alert re-arming or webhook wiring; high churn (customers, subscriptions, invoices per tick) is exactly the load we want to cut | Good for previews, useless for the billing lifecycle. Rejected alone. |
| **D. Hybrid (chosen)** | Long-lived personas for what needs real time to pass; throwaway customers stamped from the same trait vocabulary for one-shot flows; both fed by a deterministic per-persona event schedule that doubles as the oracle | Exact assertions on usage, wallet and invoices; coverage matrix is readable; one-shot flows stay isolated; load is a declared budget | Two customer lifetimes to reason about; the schedule and oracle are new code that must be right | Chosen. The split is explicit in the suite file, so the extra concept is visible rather than implicit. |

### 4.2 Where does the definition live?

| Approach | Pros | Cons | Consequence |
| --- | --- | --- | --- |
| **Env vars only** (today) | Nothing new | Dozens of `E2EPROBE_CHECK_*` toggles; no way to express personas or expectations; invisible coverage | Rejected. |
| **Go only** | One language, compiled, testable | Per-region differences (which gateways, which checks on) still need env; the coverage view lives in code | Rejected for the world and enablement. |
| **YAML suite + Go catalog (chosen)** | The suite file is the single readable statement of coverage; regions differ by file; the chart renders it from values | Second syntax; needs validation | Chosen. The Go catalog owns what scenarios, traits and event profiles *exist*; the suite selects and binds them and is validated against the catalog at startup. Assertions never live in YAML. |

### 4.3 How do failures reach people?

| Approach | Pros | Cons | Consequence |
| --- | --- | --- | --- |
| **Stateless per-failure** (today) | Immediate | One Slack line per failing tick; no recovery; flaps page | Rejected. |
| **Digest only** | Quiet | A real regression waits for the digest | Rejected alone. |
| **Incidents with thresholds + daily digest (chosen)** | Pages once, after `after_failures` consecutive failures *and* `after_duration`; reminds every `renotify_every`; announces recovery; coverage loss (a scenario skipping for longer than `skip_after`) and API-wide infrastructure errors are their own single incidents; the digest carries the matrix | State to keep | Chosen. State is in memory with a JSON snapshot so container restarts keep open incidents. |

## 5. Design

### 5.1 Package layout

```
internal/ee/e2eprobe/v2/
  e2eprobe.go       Kind, Status, Result, Attrs: the shared vocabulary
  error.go          CheckError and SDK error enrichment (kept)
  client*.go        SDK adapters (kept; EventOps drops ListRaw and gains IngestBulk)
  otel.go           tracer provider (kept)
  bootstrap/        airgapped signup bootstrap (kept)
  suite/            suite file types, loader with embedded default, validation, Describe
  world/            traits, persona specs, live bindings, provisioning, ephemeral clones, janitor
  ledger/           event profiles, deterministic schedule, bulk sender, oracle
  inbox/            webhook HTTP inbox, store, Await, stats
  incident/         result stream to incident state machine, notifiers (log, Slack, OTEL), digest, state file
  scenario/         Scenario, Context, Binding, Catalog, Runner, /status
  scenarios/        one file per scenario
  billingmatrix/    kept; adapted to the Result type and wrapped as one scenario per family
cmd/e2eprobe/v2/
  main.go           run (default) | validate | describe
  suite.example.yaml
```

Package names say what they provide. No `utils`, `helpers` or `common`.

### 5.2 Suite file

```yaml
version: 1

defaults:
  tick: 10m                 # event schedule cadence for every persona with a profile
  concurrency: 4            # scenarios running at once
  alert:
    after_failures: 3       # consecutive failures before an incident opens
    after_duration: 20m     # and failing for at least this long
    renotify_every: 6h      # reminders while open; 0 disables
    skip_after: 24h         # a scenario skipping this long is a coverage incident
  load:
    max_events_per_day: 50000
  janitor:
    max_age: 1h

tenant:
  connections:              # preflight verifies; the probe never creates connections
    - stripe
  settings:                 # preflight verifies a JSON subset; the probe never writes settings
    wallet_balance_alert_config: { enabled: true }
  webhooks:
    silence_after: 6h       # no delivery of any kind for this long is one incident

personas:
  - name: baseline-a
    external_id: e2eprobe-cust-persistent-3   # optional; keeps invoice history of an old fixture
    traits: [subscription]
    events: standard
  - name: taxed                # also owns the legacy quarterly subscription
    external_id: e2eprobe-cust-persistent-0
    traits: [subscription, tax, multi_cadence]
    events: standard
  - name: couponed
    external_id: e2eprobe-cust-persistent-1
    traits: [subscription, coupon]
    events: standard
  - name: committed
    traits: [subscription, commitment]
    commitment: { amount: "5.00", overage_factor: "1.5" }
    events: standard
  - name: wallet-a             # no events: wallet-debit drives its own usage so the delta is exact
    external_id: e2eprobe-cust-persistent-2
    traits: [subscription, wallet]
    wallet: { balance: "100.00" }
  - name: alert-canary
    external_id: e2eprobe-cust-alert-canary
    traits: [subscription, wallet, alerts]
    wallet: { balance: "30.00", thresholds: { info: "25", warning: "10", critical: "0" } }
  - name: entitled
    traits: [subscription, soft_limit]
    events: standard
  - name: granted
    traits: [subscription, additive_grant]
    events: standard
  - name: bucketed
    traits: [subscription, bucketed]
    events: bucketed
  - name: payer-stripe
    traits: [payment]
    payment: { provider: stripe, currency: USD, fixed_customer: e2eprobe-cust-pay-stripe-cards, decline_last4: "0341" }

scenarios:                  # a scenario absent here is disabled
  - { name: provision, every: 6h }                      # re-provisions fixtures and personas
  - { name: preflight, every: 6h }
  - { name: ingest }                                    # every tick, binds to every persona with events
  - { name: usage, every: 15m }                         # binds to every persona with events
  - { name: wallet-debit, every: 30m, personas: [wallet-a] }
  - { name: invoice-freshness, every: 1h }              # trait: subscription
  - { name: invoice, every: 1h, families: [tax, coupon, commitment, multi-cadence] }
  - { name: entitlement-usage, every: 30m }             # traits: soft_limit + events
  - { name: webhook-wiring, every: 15m }
  - { name: low-balance-alert, every: 10m }             # trait: alerts
  - { name: lifecycle, every: 30m, personas: [baseline-a] }
  - { name: cancel, every: 1h, personas: [baseline-a] }
  - { name: line-item-change, every: 1h, personas: [baseline-a] }
  - { name: tax-preview, every: 1h, personas: [taxed] }
  - { name: coupon-preview, every: 1h, personas: [couponed] }
  - { name: commitment-preview, every: 1h, personas: [committed] }
  - { name: entitlement-enforcement, every: 1h, personas: [entitled] }
  - { name: grant-inheritance, every: 1h, personas: [granted] }
  - { name: checkout-link, every: 30m }                 # trait: payment
  - { name: payment-methods, every: 1h }                # trait: payment
  - { name: auto-charge, every: 6h }                    # trait: payment with fixed_customer
  - { name: billing-matrix, every: 15m, enabled: false, families: [opening, renewal, change, grants, validation], per_run: 3 }
```

Rules:

- `traits` are the vocabulary. Each scenario declares the traits it requires. Binding is by trait
  unless `personas:` names an explicit list; the loader rejects a persona that lacks a required
  trait and a scenario name the catalog does not know.
- `external_id` lets a persona adopt an existing customer so cycle-invoice history continues.
  Provisioning adds what it can (tax association, wallet, thresholds) and records what it cannot
  retrofit (a coupon is attached only at subscription create); scenarios then skip with that reason
  instead of failing.
- Event profiles (`standard`, `bucketed`) are defined in Go next to the oracle because the expected
  aggregate depends on the exact values sent. The suite picks a profile; `describe` prints it.
- The default suite is embedded in the binary so `make run-e2eprobe-v2` works with no file.
  `E2EPROBE_SUITE_FILE` points at an override; the chart renders `e2eprobeV2.suite` into a ConfigMap
  mounted at `/etc/e2eprobe/suite.yaml` and rolls the pod when its checksum changes.

### 5.3 World: traits, personas, clones

Traits and what provisioning guarantees for each:

| Trait | Guarantees on the persona |
| --- | --- |
| `subscription` | Customer exists, active monthly anniversary subscription on the probe plan |
| `tax` | Shared 10% tax rate attached by a subscription-scoped tax association; activation time recorded |
| `coupon` | Shared 10% once-cadence coupon attached at subscription create (not retrofittable) |
| `commitment` | Subscription created with the declared commitment and overage factor (not retrofittable) |
| `wallet` | One USD wallet topped to the declared balance on first provisioning |
| `alerts` | Wallet alert thresholds set as declared; persona excluded from scheduled events |
| `soft_limit` | Plan-level soft-limit entitlement on the count meter applies |
| `additive_grant` | Plan-level additive grant entitlement applies |
| `bucketed` | Subscription includes the bucketed prices (15MIN, HOUR, DAY) |
| `multi_cadence` | Quarterly subscription with monthly items |
| `payment` | Gateway connection expected; optional fixed customer with hand-saved cards |

Shared fixtures (plan, prices, meters, tax rate, coupon, addons for payments) are provisioned once,
idempotently, as today. `world.Provision` replaces `seed_ensure`: one function per trait, each
reading then writing, each recording unretrofittable gaps on the persona.

`Context.Clone(ctx)` stamps an ephemeral customer from the persona's traits
(`e2eprobe-eph-<scenario>-<unixnano>`, metadata `e2eprobe_cohort=ephemeral`,
`e2eprobe_persona=<name>`, `e2eprobe_run_id`) and registers it for the janitor. One-shot
scenarios use clones; persistent scenarios use the persona.

### 5.4 Ledger: schedule and oracle

Every persona with an event profile gets one bulk batch per tick (`POST /events/bulk`). The batch
is a pure function of `(profile, persona, tick)`:

- Event ids are deterministic (`<persona>-<meter>-<tick unix>-<i>`), so a retry or a restart that
  re-sends a tick is idempotent on both `events` (ReplacingMergeTree on id) and `meter_usage`
  (duplicate hash).
- Timestamps fall inside `(tick - interval, tick]` at fixed offsets, so every window a scenario asks
  about is already closed when the batch is sent. Bucketed profiles place sub-batches at offsets
  that make per-bucket MAX and SUM computable.
- The ledger records each sent batch. The oracle answers `Expect(persona, meter, window)` from the
  profile and the sent markers. A window with missing batches (probe downtime) is reported as
  partial and scenarios skip it with that reason rather than asserting a wrong total.

Default `standard` profile per tick: count 5, sum 3 at amount 10, avg 2, max 2 at values 3 and 7,
count-unique 4 over 3 users, latest 2 ending at 9, sum-with-multiplier 2 at amount 5,
filtered sum 3 of which 2 match `source=api`, and 2 each on the two price-bucketed meters. That
is 27 events per persona per tick; seven standard personas plus the bucketed persona at a
10-minute tick is about 28k events a day in eight HTTP calls per tick. Flows that drive their own
usage declare it at registration (`ingests(n)`) and add about 25k, so the default suite is about
54k a day against a 100k cap, down from about 430k.
Offsets inside the tick are fractions, so the schedule stays valid for any tick length.

### 5.5 Scenario framework

```go
type Scenario interface {
    Name() string
    Kind() e2eprobe.Kind         // probe | flow | preflight | maintenance
    Requires() []world.Trait
    Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result
}
```

`Result` is `{Status: pass | fail | skip, Step, Reason, Attrs, Mismatches}`. `Context` gives the
bound persona, the client, the ledger, the inbox, `Clone`, and assertion collectors in the
billing-matrix style (`sc.Money(label, want, got)`, `sc.Count`, `sc.Truth`) so a scenario reports
every mismatch, not the first. A failure never loses the persona, step, ids and want/got.

The runner owns one ticker per binding with start jitter, never overlaps a binding with itself,
caps concurrency with a semaphore, records every result into the incident tracker and the status
view, and wraps each run in an OTEL span. Billing-matrix families keep their rotation inside one
scenario per family.

### 5.6 Incidents

Per binding (scenario × persona) the tracker keeps consecutive failures, failing-since,
skipping-since and the open incident, if any:

- **Open** when consecutive failures reach `after_failures` and the binding has been failing for
  `after_duration`. One Slack message: binding, failing since, run count, step, mismatches, ids.
- **Remind** every `renotify_every` while open.
- **Resolve** on the next pass: one message with the outage length.
- **Coverage incident** when a binding skips continuously for `skip_after`.
- **Infrastructure rollup**: results whose error is a 5xx, timeout or connection failure count
  toward one region-level incident when three or more bindings see them within a window; their
  individual failures do not page while the rollup is open.
- **Webhook silence**: the inbox receiving nothing for `silence_after` while the probe keeps
  creating customers (every clone must produce `customer.created`) is one incident with the fix
  hint (tenant missing from `webhook.tenants`, or no Svix endpoint).

State is snapshotted to `E2EPROBE_STATE_FILE` (default under `TMPDIR`) after each change so a
container restart keeps open incidents and their start times.

A daily digest (Slack) carries the coverage matrix, open incidents, skipping bindings with reasons,
pass rates, inbox counts by event type, event budget used, and legacy fixtures found in the tenant.

Delivery is part of the contract: a `Notifier` returns the delivery error, the tracker stamps
`last_notified` only on success and retries an undelivered open or reminder on its next tick.
Infrastructure failures never page from `Observe`; the tick evaluates the region rollup first and
pages a binding only when no rollup covers it, and the rollup's observation window is persisted
with the rest of the state so a restart inside an outage does not announce a recovery.

### 5.7 Inbox

`POST /webhook` accepts any Flexprice envelope, stores `{received_at, event_type, customer_id,
wallet_id, subscription_id, invoice_id, raw}` in a bounded ring (10k entries or 48h) indexed by
event type and by entity id, and answers 200 immediately. An optional shared secret header
(`E2EPROBE_WEBHOOK_TOKEN`, matched against the header the server is configured to send) rejects
foreign posts with 401. `GET /healthz`, `GET /status` and `GET /inbox/stats` are served on the
same port.

Scenarios call `inbox.Await(ctx, Match{EventType, CustomerID, After}, timeout)`. When a webhook is
missing, the alert scenario queries `POST /alerts/search` for the customer to separate "alert never
fired" from "alert fired, webhook not delivered" and reports the matching step.

### 5.8 Preflight

A `preflight` scenario runs at start and every six hours against the `tenant` block: lists
connections and compares provider types, reads each declared setting and checks the JSON subset,
confirms the plan and shared fixtures exist. Drift is a normal incident with expected-versus-actual
text. Payment personas skip with reason when their provider's connection is missing, so one
misconfigured gateway shows as one preflight incident plus explicit coverage gaps, not a storm.

### 5.9 Load and read discipline

- `suite.Validate` computes events per day from personas, profiles and `tick` plus every enabled
  binding's declared ingest per run at its interval; `run` refuses to start above
  `max_events_per_day` and `describe` prints the split between profiles and flows.
- The `usage` scenario reads one `POST /events/analytics` per persona per run (every meter of the
  profile, bounded to the last closed hour) and compares each against the oracle. Eight ClickHouse
  reads per 15 minutes for the default suite.
- No scenario polls immediately after ingest except the alert and preview flows, which are bounded
  and infrequent.

### 5.10 Coexistence and migration

Both generations run side by side until this one has earned trust, so the move is reversible and
the old probe's incidents keep flowing meanwhile.

- The old code stays byte for byte at `internal/ee/e2eprobe` and `cmd/e2eprobe`; this generation
  lives in `internal/ee/e2eprobe/v2` and `cmd/e2eprobe/v2` and shares only `bootstrap/` and
  `billingmatrix/`. Chart component `templates/probe-v2` with `e2eprobeV2.*` values, image
  `ghcr.io/flexprice/e2eprobe-v2`, workflow `publish-e2eprobe-v2-image.yml` and
  `Dockerfile.e2eprobe-v2` mirror the old ones; the old publish workflow ignores `v2/` paths.
- Every default persona is its own customer (`e2eprobe-<name>`), so the two generations never write
  to one customer and the oracle's totals stay exact. `external_id` on a persona still adopts an
  existing customer for a region that wants the old invoice history after the old probe is gone.
- The janitor deletes only customers carrying this generation's markers (`e2eprobe-eph-` prefix or
  the ephemeral cohort tag). The old probe's ephemerals and persistent customers are listed as
  legacy in the sweep report and digest; removing them is a manual decision.
- Shared by lookup key and reconciled the same way by both generations: the plan and its prices,
  meters, features, entitlements, coupon and tax rate. Neither deletes them.
- The default suite assumes no payment gateway. A region adds a payer persona and
  `tenant.connections`; the hand-provisioned cards customer is shared with the old probe, so the
  payment scenarios run in one generation at a time.
- A separate environment and API key for v2 is the recommended way to run both, and it exercises
  provisioning from nothing, which adopting the old customers never did.
- `E2EPROBE_CHECK_*`, `E2EPROBE_PAYMENTS_*`, `E2EPROBE_BILLING_MATRIX_*`, `E2EPROBE_EVENT_INGEST_*`,
  `E2EPROBE_HEARTBEAT_INTERVAL` and `E2EPROBE_JANITOR_MAX_AGE` are not read by v2; the suite carries
  them. Startup logs a warning for each one still set, naming the suite key that replaces it.
- Swap, once trusted: delete the old Go files, `templates/probe`, `Dockerfile.e2eprobe` and its
  workflow; move `v2/` up and drop `/v2` from import paths; rename `e2eprobeV2` to `e2eprobe` in
  values and region files; point the image name back. Nothing in v2 depends on the old code.

### 5.11 Scenario catalog (ported coverage)

| Scenario | Kind | Binds to | Replaces | Reads |
| --- | --- | --- | --- | --- |
| provision | maintenance | tenant | seed-ensure | plans, features, subscriptions, wallets |
| preflight | preflight | tenant | (new) | connections, settings, plans |
| ingest | probe (every tick) | trait events | event-ingest-driver | ingest |
| usage | probe | trait events | analytics-probe, meter-aggregation-probe, bucketed-meter-probe | analytics |
| wallet-debit | probe | wallet + events | wallet-balance-probe, wallet-debit-verification | wallet balance, analytics |
| invoice-freshness | probe | subscription | cycle-invoice-probe | invoices |
| invoice-tax, invoice-coupon, invoice-commitment, invoice-multi-cadence (the `invoice` suite entry with `families`) | probe | tax, coupon, commitment, multi_cadence | persistent-billing-invariants-probe, multi-cadence-invoice-probe | invoices |
| entitlement-usage | probe | soft_limit, additive_grant | entitlement-and-usage-probe | customer usage summary, entitlements |
| low-balance-alert | probe | alerts | low-balance-alert-probe, low-wallet-alert-listener | wallet balance, inbox, alerts/search |
| webhook-wiring | probe | tenant | (new) | inbox |
| lifecycle | flow | clone | new-customer-lifecycle | customer, subscription, usage |
| cancel | flow | clone | cancel-customer-flow | subscription, invoices |
| line-item-change | flow | clone | subscription-modification-flow | subscription |
| tax-preview, coupon-preview, commitment-preview | flow | clone | tax/coupon/commitment probes | preview |
| entitlement-enforcement, grant-inheritance | flow | clone | enforcement and additive-grant probes | usage summary, entitlements |
| checkout-link, payment-methods, auto-charge | flow | payment | payment-link/method/autocharge probes | payments |
| billing-matrix-\<family\> | flow | own fixtures | billing-matrix-\<family\> | invoices, preview |
| janitor | maintenance | world | janitor | customers, subscriptions |

### 5.12 Visibility surfaces

- `e2eprobe describe [--suite file]`: coverage matrix (persona × trait × scenario with interval),
  event profiles and the computed daily budget, tenant expectations, expected webhooks.
- `e2eprobe validate [--suite file]`: exit non-zero with every validation error.
- `GET /status`: bindings with last result and next run, open incidents, inbox stats, ledger stats.
- Hourly heartbeat log line and daily Slack digest.

`once --scenario <name> [--persona <p>] [--json]` provisions the suite and runs one binding with
Slack quiet and incident state in memory, exiting 1 on failure; `run --only a,b` narrows a
region's suite while debugging.

## 6. Testing

- Unit tests per file with the fake client (`scenarios/fakeclient_test.go`), no network.
- Oracle tests pin each profile's expected aggregate per meter type and the partial-window skip.
- Incident tracker tests drive the state machine through open, remind, resolve, skip and rollup.
- Suite tests cover the embedded default, trait validation, unknown names and the load budget.
- Inbox tests cover store bounds, `Await` matching and the shared secret.
- `go test -tags e2eprobe_integration ./cmd/e2eprobe/v2/` keeps the live smoke; the billing-matrix live
  test is kept.

## 7. Left to the builder

- Exact Slack block layout for open, remind, resolve and digest.
- Jitter distribution and the infrastructure-rollup window (start at 15 minutes).
- Ring buffer sizes.
- Whether `describe` also renders Markdown for pasting into the runbook.

## 8. Rollout

1. Land v2 beside the current probe; nothing changes for regions until `e2eprobeV2.enabled` is set.
2. Enable v2 on staging with its own API key, ideally in its own environment; run `describe` and
   `once --scenario preflight` first, then watch one digest cycle for false pages and skips.
3. Add the staging gateway to a payer persona in `e2eprobeV2.suite`; move the payment scenarios
   over by disabling them in the old probe's env for that region.
4. Repeat per region. When every region runs v2 and the old probe has been quiet for a cycle, do
   the swap in §5.10 and delete the legacy customers the digest lists.
