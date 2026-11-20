# e2eprobe

A long-running synthetic monitor that exercises one Flexprice region the way a
customer does and pages Slack only when something has been wrong for a while.
It owns a set of **personas** (long-lived customers with declared traits), sends
them a **deterministic event schedule**, and runs **scenarios** that compare what
the API reports against what the schedule predicts: usage, wallet balances,
invoices, entitlements, alerts, webhooks and checkouts.

Design: [`docs/design/2026-11-19-e2eprobe-redesign.md`](../../../docs/design/2026-11-19-e2eprobe-redesign.md).
Contributor guide: [`AGENTS.md`](AGENTS.md).

## Quick start

```bash
export E2EPROBE_API_HOST=https://api.cloud.flexprice.io/v1
export E2EPROBE_API_KEY=<key for the probe tenant>
export E2EPROBE_SLACK_WEBHOOK_URL=<optional>
make run-e2eprobe-v2              # runs the embedded default suite

go run ./cmd/e2eprobe/v2 describe    # coverage matrix: personas × scenarios, event budget
go run ./cmd/e2eprobe/v2 validate    # exit 1 with every problem in the suite
go run ./cmd/e2eprobe/v2 describe --suite region.yaml

# developing or confirming one scenario: one run, result on stdout, exit 1 on failure
go run ./cmd/e2eprobe/v2 once --scenario usage --persona taxed
go run ./cmd/e2eprobe/v2 once --scenario preflight --json
go run ./cmd/e2eprobe/v2 run --only ingest,usage   # a subset of the suite, everything else left out
make test-e2eprobe-v2                              # unit tests for the module
```

The probe listens on `E2EPROBE_LISTENER_PORT` (default 8765):

| Path | Purpose |
| --- | --- |
| `POST /webhook` | inbox for the tenant's outbound webhooks |
| `GET /status` | live view: every binding's last result and next run, open incidents, inbox and ledger stats |
| `GET /healthz` | liveness |
| `GET /inbox/stats` | webhook counts by type |

## Configuration

Required and secret settings stay in env; everything about *what runs* lives in
the **suite file**.

| Env | Purpose | Default |
| --- | --- | --- |
| `E2EPROBE_API_HOST` | API base URL including `/v1` | required |
| `E2EPROBE_API_KEY` | key for the probe tenant | required unless bootstrapping |
| `E2EPROBE_EMAIL`, `E2EPROBE_PASSWORD`, `E2EPROBE_BOOTSTRAP_SECRET_NAME`, `E2EPROBE_BOOTSTRAP_SECRET_KEY`, `POD_NAMESPACE` | airgapped signup bootstrap (unchanged, see below) | empty |
| `E2EPROBE_SUITE_FILE` | suite file path | empty: embedded default |
| `E2EPROBE_STATE_FILE` | incident snapshot kept across restarts | `$TMPDIR/e2eprobe-state.json` |
| `E2EPROBE_WEBHOOK_TOKEN` | shared secret the inbox requires in `X-E2EProbe-Token` | empty: no check |
| `E2EPROBE_SLACK_WEBHOOK_URL`, `E2EPROBE_SLACK_BOT_TOKEN`, `E2EPROBE_SLACK_CHANNEL` | Slack delivery; bot token wins and needs a channel | empty: Slack off |
| `E2EPROBE_TENANT_ID`, `E2EPROBE_ENVIRONMENT_ID` | context for alerts | empty |
| `E2EPROBE_ENABLED`, `E2EPROBE_DRY_RUN`, `E2EPROBE_LOG_LEVEL`, `E2EPROBE_LISTENER_PORT`, `E2EPROBE_OTEL_ENABLED` | process knobs | `true`, `false`, `info`, `8765`, `false` |

Removed: `E2EPROBE_CHECK_*`, `E2EPROBE_PAYMENTS_*`, `E2EPROBE_BILLING_MATRIX_*`,
`E2EPROBE_EVENT_INGEST_*`, `E2EPROBE_HEARTBEAT_INTERVAL`, `E2EPROBE_JANITOR_MAX_AGE`.
Each still set logs a warning naming the suite key that replaces it.

### The suite file

[`cmd/e2eprobe/v2/suite.example.yaml`](../../../cmd/e2eprobe/v2/suite.example.yaml) is
the embedded default with every key explained. In short:

```yaml
defaults:   tick, concurrency, alert {after_failures, after_duration, renotify_every, skip_after}, load.max_events_per_day, janitor.max_age, digest.every
tenant:     connections (providers preflight expects), settings (JSON subsets preflight expects), webhooks.silence_after
personas:   name, external_id (adopt an existing customer), traits, events (profile), wallet, commitment, payment
scenarios:  name, every, enabled, personas (explicit list), alert (override), families, per_run, assert_known_issues
```

A scenario runs on every persona carrying its required traits unless it names
personas. The loader rejects unknown scenarios, traits or profiles, a persona
missing a required trait, a trait-bound scenario with no persona, and a schedule
above `max_events_per_day`. In Kubernetes set `e2eprobe.suite` in the chart
values; it renders into a ConfigMap mounted at `/etc/e2eprobe/suite.yaml`.

## Personas and traits

Provisioning (`provision` scenario, at start and every 6h) makes the tenant
match the suite and records a **gap** for anything it cannot retrofit; scenarios
skip on a gap with that reason instead of failing.

| Trait | Guarantees |
| --- | --- |
| `subscription` | customer + active monthly anniversary subscription on `e2eprobe_plan` |
| `tax` | shared 10% rate attached by a subscription-scoped association; activation time recorded |
| `coupon` | shared 10% once coupon attached at subscription create (gap on an adopted subscription) |
| `commitment` | subscription created with the declared commitment and overage factor (gap on an adopted subscription) |
| `wallet` | one USD wallet topped to the declared balance when created |
| `alerts` | wallet thresholds as declared (gap when an adopted wallet differs) |
| `soft_limit` / `additive_grant` | plan-level entitlements exist |
| `bucketed` | subscription carries the bucketed prices (plan price sync repairs drift) |
| `multi_cadence` | quarterly subscription with monthly items |
| `payment` | gateway connection expected; optional fixed customer with hand-saved cards |

Default personas: `baseline-a`, `baseline-b`, `taxed` (tax + multi_cadence, adopts
`e2eprobe-cust-persistent-0`), `couponed`, `committed`, `wallet-a`, `alert-canary`,
`entitled`, `granted`, `bucketed`, `payer-stripe`. Adopting the previous
generation's customers keeps their invoice history feeding the invariants.

## Event schedule and oracle

Every persona with an `events` profile sends one bulk batch per tick
(`POST /events/bulk`, default every 10 minutes). Event ids and timestamps derive
from the tick, so a retry or restart re-sending a tick is idempotent on the
server. The `standard` profile is 27 events across every unbucketed meter plus
the hour-bucketed ones; `bucketed` is 6 events on the 15MIN, HOUR and DAY meters.
Default budget: profile batches from seven standard personas and one bucketed
persona come to about 28k events a day; the flows that drive their own usage
(wallet-debit, the previews, enforcement, grants) add about 25k, so the default
suite sends about 54k a day against a 100k cap, versus about 430k for the
previous random driver. Every flow declares its ingest at registration
(`ingests(n)`), `describe` prints the split and `validate` refuses a suite over
its cap.

The ledger records which ticks were sent; the oracle computes the exact
aggregate each meter must show for any window. A window with a missing batch
(probe downtime) is reported as partial and skipped, never asserted.

## Scenarios

| Scenario | Kind | Binds to | Checks | Reads |
| --- | --- | --- | --- | --- |
| provision | maintenance | tenant | world provisioning | Postgres |
| preflight | preflight | tenant | expected connections, settings, fixtures; persona gaps as attributes | connections, settings |
| ingest | probe (every tick) | events | one bulk batch per tick | ingest |
| usage | probe | events | exact totals for every meter over the last full hour | `events/analytics` |
| wallet-debit | probe | wallet-a | top-up read-after-write; a 500-unit `sum_commit` ingest debits exactly $5.00 | wallet balance |
| low-balance-alert | probe | alerts | drives the canary below critical, awaits `wallet.ongoing_balance.dropped`; a miss is attributed to the alert pipeline or to delivery via alert logs | wallet balance, inbox, `alerts/search` |
| invoice-freshness | probe | subscription | latest cycle invoice within 2 cycles | invoices |
| invoice-tax, -coupon, -commitment, -multi-cadence | probe | tax, coupon, commitment, multi_cadence | shared rate on the latest finalized invoice after activation; coupon on the oldest invoice; true-up math; monthly fan-out | invoices |
| entitlement-usage | probe | soft_limit + events | soft limit of 100 and usage ≥ what the ledger sent this period | usage summary |
| webhook-wiring | probe | tenant | no webhook of any kind for `silence_after` while clones were created | inbox |
| lifecycle, cancel, line-item-change | flow | baseline-a | clone + ingest + usage; cancel and delete the oldest clone; add a line item | subscriptions, usage |
| tax-preview, coupon-preview, commitment-preview | flow | taxed, couponed, committed | preview math on a clone | preview |
| entitlement-enforcement, grant-inheritance | flow | entitled, granted | 150 events past the soft limit; grant echoed and inherited by a clone | usage summary, entitlements |
| checkout-link, payment-methods, auto-charge | flow | payment (regions that declare a payer persona) | hosted links, saved methods, auto-charge legs per gateway | payments |
| billing-matrix-\<family\> | flow | tenant (off by default) | proration and cadence oracle | invoices, preview |
| janitor | maintenance | tenant | removes this generation's ephemerals (`e2eprobe-eph-` prefix or cohort tag) older than `janitor.max_age`; the previous generation's customers are listed as legacy, never deleted | customers |

Read discipline: nothing reads the raw `events` table (`/events/query`,
`/events/lookup`, `/events/usage` and `/events/usage/meter` are not even on the
client). The one ClickHouse read is `/events/analytics`, served from
`meter_usage`, bounded to one customer and one hour.

## Incidents

Every run reports pass, fail or skip. A binding (scenario × persona) opens an
incident when it has failed `after_failures` times in a row **and** for
`after_duration`; Slack gets one message, a reminder every `renotify_every`, and
one recovery message. A binding that skips for `skip_after` opens a coverage
incident. Infrastructure errors (5xx, timeouts) never page per binding; three or
more bindings seeing them within 15 minutes open one region-level incident. State
is snapshotted to `E2EPROBE_STATE_FILE` so restarts keep open incidents. A daily
digest lists open incidents, skipping bindings with reasons, pass counts, webhook
counts and legacy fixtures found in the tenant.

## Webhooks

Point the tenant's webhook delivery at `http://<probe-service>:8765/webhook`
(`webhook.tenants.<tenant_id>.endpoint` in the server config, or a Svix
endpoint). Optionally send `X-E2EProbe-Token` and set `E2EPROBE_WEBHOOK_TOKEN`.
The inbox keeps the last 10k deliveries for 48h; scenarios await the one they
caused. When nothing arrives for `tenant.webhooks.silence_after` after clones
were created, `webhook-wiring` opens one incident with the fix hint.

## Airgapped bootstrap

Unchanged from the previous generation: supply `E2EPROBE_EMAIL` and
`E2EPROBE_PASSWORD` instead of a key; the probe signs up (or logs in on 409),
mints a private key and, in Kubernetes, patches it into
`E2EPROBE_BOOTSTRAP_SECRET_NAME`. A stored key always wins so restarts never
mint a second one. Requires the flexprice-native auth provider.

## Deployment

The chart (`internal/ee/infrastructure/helm/flexprice`, `e2eprobeV2.*` values,
`templates/probe-v2`) runs one replica, mounts the API key, Slack webhook and
inbox token from `e2eprobeV2.existingSecret`, exposes the listener as a
ClusterIP service with liveness and readiness probes on `/healthz`, keeps the
incident snapshot in the pod's `tmp` volume and renders `e2eprobeV2.suite` into
a ConfigMap whose checksum rolls the pod when the suite changes. Image:
`ghcr.io/flexprice/e2eprobe-v2`, built by
`.github/workflows/publish-e2eprobe-v2-image.yml` from `Dockerfile.e2eprobe-v2`.

## Running beside the current probe

This module is the second generation. The current probe keeps running
untouched from `internal/ee/e2eprobe` and `cmd/e2eprobe`, with its own image,
chart component (`e2eprobe.*`) and workflow, until this one has earned trust;
only `bootstrap/` and `billingmatrix/` are shared.

- Every default persona is a customer of its own (`e2eprobe-<name>`), so the two
  generations never write to one customer. Set `external_id` on a persona to
  adopt an existing customer once the old probe is gone.
- The janitor deletes only customers carrying this generation's markers. The
  old probe's ephemerals and persistent customers are listed under `legacy` in
  the sweep report and digest for a manual decision.
- The plan, meters, features, coupon and tax rate are shared by lookup key; both
  generations reconcile them the same way and neither deletes them.
- Payment gateways are not assumed. A region with a gateway adds a payer
  persona and `tenant.connections` to its suite; the fixed cards customer is
  shared with the old probe, so run the payment scenarios in one generation
  at a time.
- Prefer a separate environment (and API key) for v2 while both run. That also
  exercises provisioning from nothing, which the old customers never did.

Swapping over: delete `internal/ee/e2eprobe/*.go`, `checks/`, `cmd/e2eprobe/*.go`,
`templates/probe`, `Dockerfile.e2eprobe` and the old workflow; move `v2/` up one
level and drop `/v2` from the import paths; rename `e2eprobeV2` to `e2eprobe` in
values and region files; point the image name back. Nothing in this module
depends on the old code.

## Live checks

```bash
# provisions the default suite against a live API and prints persona readiness
E2EPROBE_API_HOST=http://localhost:8080/v1 E2EPROBE_API_KEY=sk_... \
  go test -tags e2eprobe_integration ./cmd/e2eprobe/v2/

# runs every billing-matrix scenario once
E2EPROBE_API_HOST=http://localhost:8080/v1 E2EPROBE_API_KEY=sk_... \
  go test -tags e2eprobe_integration ./internal/ee/e2eprobe/billingmatrix -run TestLiveMatrix -v -timeout 2h
```
