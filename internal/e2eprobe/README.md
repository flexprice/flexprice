# E2EProbe API Probe

A long-running synthetic monitor that exercises Flexprice's public APIs against one fixed tenant/environment, asserting correctness across CRUD, event ingestion → analytics, event → wallet debit, cancel → invoice, auto-generated billing-cycle invoices, and webhook delivery.

## Quick start

```bash
export E2EPROBE_API_HOST=https://api.cloud.flexprice.io/v1
export E2EPROBE_API_KEY=<key for the e2eprobe tenant>
export E2EPROBE_SLACK_WEBHOOK_URL=<optional>
export OTEL_EXPORTER_OTLP_ENDPOINT=<optional, e.g. signoz-otel:4317>
make run-e2eprobe
```

## Self-provisioning

`seed-ensure` is fully self-contained — no manual tenant prep is required. On first run (or any run where entities are missing) the harness idempotently provisions:

1. **9 features** (each with an embedded meter, one per aggregation type: COUNT, SUM, AVG, COUNT_UNIQUE, LATEST, MAX, SUM_WITH_MULTIPLIER, WEIGHTED_SUM, SUM/api-filter).
2. **10 persistent customers** tagged `metadata.e2eprobe_cohort = "persistent"`.
3. **1 plan** (`e2eprobe_plan`) with metadata `e2eprobe = "true"`.
4. **10 prices** attached to the plan: 1 base recurring fixed fee ($19.99/mo) + 1 usage price per feature ($0.01/unit).
5. **10 subscriptions** — one per persistent customer — on the e2eprobe plan (monthly, anniversary cycle). Draft subscriptions are activated automatically.
6. **3 wallets** on the first 3 persistent customers (`e2eprobe-cust-persistent-0/1/2`), each topped up to $100.00 USD.

Every step is idempotent: re-running seed-ensure against a tenant that already has all entities is a no-op.

## Architecture

The harness is built around three abstractions:

- **`Check`** — base interface (Name + Kind + Run). Every unit of work is a Check.
- **`Scheduler`** — controls when a Check runs. Four concrete: `Ticker`, `Rate`, `OneShot`, `Listener`.
- **`Runner`** — owns the Reporter, panic recovery, OTEL spans.

Adding a new probe: write `internal/e2eprobe/checks/<name>.go` implementing `Check`; register in `cmd/e2eprobe/main.go` with the appropriate Scheduler.

## Env vars

| Var | Purpose | Default |
| --- | ------- | ------- |
| `E2EPROBE_API_HOST` | Flexprice API base URL (include /v1) | required |
| `E2EPROBE_API_KEY` | API key for the e2eprobe tenant | required |
| `E2EPROBE_ENABLED` | Master kill switch | `true` |
| `E2EPROBE_DRY_RUN` | Log mutating calls without sending | `false` |
| `E2EPROBE_TENANT_ID` | Tenant ID included in every Slack/OTEL alert for context | empty (optional but recommended) |
| `E2EPROBE_ENVIRONMENT_ID` | Environment ID included in every Slack/OTEL alert for context | empty (optional but recommended) |
| `E2EPROBE_EVENT_INGEST_RATE` | Events/sec for the ingest driver | `5` |
| `E2EPROBE_EVENT_INGEST_SEED` | RNG seed for event deck | derived from start time |
| `E2EPROBE_LISTENER_PORT` | HTTP listener port for webhook checks | `8765` |
| `E2EPROBE_SLACK_WEBHOOK_URL` | Slack webhook (empty disables) | empty |
| `E2EPROBE_SLACK_CHANNEL` | Override channel | empty |
| `E2EPROBE_OTEL_ENABLED` | Emit OTEL spans | `true` |
| `E2EPROBE_HEARTBEAT_INTERVAL` | How often a structured heartbeat summary is logged (`0` disables) | `1h` |
| `E2EPROBE_JANITOR_MAX_AGE` | Minimum age of an ephemeral entity before the janitor deletes it (applies to both in-memory sweep and Flexprice orphan scan) | `1h` |
| `E2EPROBE_CHECK_<NAME>_ENABLED` | Per-check kill switch | `true` |
| `E2EPROBE_CHECK_<NAME>_INTERVAL` | Per-check interval override (Go duration) | per-check default |

Standard OTLP env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, etc.) flow through unchanged.

## Checks

| Kind | Name | Schedule | What it does |
| ---- | ---- | -------- | ------------ |
| bootstrap | seed-ensure | OneShot + 6h | Auto-provision: 9 features/meters, 10 customers, 1 plan, 10 prices, 10 subs, 3 wallets |
| driver | event-ingest-driver | Rate(5/s) | Varied event ingest using the deck |
| probe | analytics-probe | 2m | `GetUsageAnalytics` rotating params |
| probe | meter-aggregation-probe | 3m | Asserts each seed meter produces >0 usage over a 30-min window (round-robin; all 9 meters covered every ~27 min) |
| probe | wallet-balance-probe | 2m | Wallet balance reads |
| probe | wallet-debit-verification | 20m | Phase 1: TopUp read-after-write correctness; Phase 2: event→analytics aggregation pipeline |
| probe | cycle-invoice-probe | 15m | Auto-invoice freshness invariant |
| probe | entitlement-and-usage-probe | 5m | Entitlements + usage rollup |
| scenario | new-customer-lifecycle | 10m | Ephemeral customer/sub + events |
| scenario | cancel-customer-flow | 30m | Cancel oldest ephemeral sub + delete its customer |
| scenario | subscription-modification-flow | 20m | Add line item; verify |
| listener | low-wallet-alert-listener | webhook | Asserts low-balance webhook payloads and tracks per-wallet, per-alert-type receipts |
| probe | low-balance-alert-probe | 5m | Actively drives the canary wallet across its low-balance threshold and asserts the webhook lands within 2m (Slack-pages on absence) |
| maintenance | janitor | 1h | Archive in-memory ephemerals > 1h; also scans Flexprice for orphan ephemeral customers from prior restarts and deletes them |

## Webhook pipeline verification (low-balance-alert-probe + low-wallet-alert-listener)

The listener exposes `POST http://<e2eprobe-host>:8765/webhook` (port configurable). Flexprice must be pointed at that URL — either via `webhook.tenants.<tenant_id>.endpoint` in the Flexprice config (native HTTP delivery) or via a Svix endpoint subscribed to `wallet.credit_balance.dropped` / `wallet.ongoing_balance.dropped` (when Svix is enabled).

`seed-ensure` provisions a dedicated `e2eprobe-cust-alert-canary` persistent customer with one wallet initially topped up to **$30** and alert thresholds `{info=25, warning=10, critical=0}` all enabled. The three pre-funded seed wallets carry the same thresholds but sit at $100 with no draining — they exist as a safety net only.

`low-balance-alert-probe` alternates between two legs on the canary wallet:

1. **Drop leg** (wallet state = `ok`): ingest one `e2eprobe_sum` event (default `amount=600`, priced at `$0.01/unit` = `$6.00` of current-period usage) to push ongoing balance below the info threshold, then poll the listener's receipt map for up to **2 minutes**. This grace absorbs typical Kafka + Svix/native-HTTP propagation delay; only truly missing webhooks page.
2. **Recovery leg** (wallet state = `in_alarm`): top-up back to `$30` so Flexprice's binary alert state machine can re-arm.

A missing webhook in the drop leg fails the check → routes through the standard reporter chain → posts to Slack. Full cycle time at defaults: 10 minutes per verification.

## Operational signals

Every `E2EPROBE_HEARTBEAT_INTERVAL` (default 1 hour) the probe emits a single structured log line summarising activity since startup:

```json
{"level":"info","time":"2026-06-12T10:05:00.000Z","msg":"e2eprobe heartbeat","event":"e2eprobe.heartbeat","run_id":"e2eprobe-1749720000","uptime":"5m0s","total_runs":142,"total_failures":0,"success_rate":"100.00%","check.analytics-probe":"3/3","check.event-ingest-driver":"125/125","check.wallet-balance-probe":"3/3"}
```

One line per tick — not per check. Key fields:

| Field | Meaning |
| ----- | ------- |
| `uptime` | Time since process started |
| `total_runs` | All check executions (successes + failures) |
| `total_failures` | Executions that returned an error or panicked |
| `success_rate` | `(total_runs - total_failures) / total_runs × 100` |
| `check.<name>` | `successes/total` for that individual check |

**When to be concerned about silence:** If a heartbeat is missing for more than two intervals (2 hours at default settings) the process has likely crashed or lost its log pipeline. Alert on the absence of `event=e2eprobe.heartbeat` in your log aggregator.

Set `E2EPROBE_HEARTBEAT_INTERVAL=0` to disable heartbeat logging entirely.

## Failure surfacing

Failures fan out to log + Slack + OTEL spans (kind=Error). No retries. Every report includes:
- Global context from config: `tenant_id`, `environment_id` (when set via env vars)
- Per-check structured attributes: `external_customer_id`, `internal_customer_id`, `wallet_id`, `subscription_id`, `plan_id`, `event_name`, etc. — whichever IDs are known at the point of failure
- `check`, `step`, `run_id`, `error` always present

Set `E2EPROBE_TENANT_ID` and `E2EPROBE_ENVIRONMENT_ID` to make Slack alerts immediately actionable without cross-referencing logs.

## Shutdown

SIGTERM cancels all scheduler contexts; the event-ingest AsyncClient flushes; HTTP listener shuts down cleanly. Graceful shutdown is bounded at 30s.
