# Synthetic API Probe

A long-running synthetic monitor that exercises Flexprice's public APIs against one fixed tenant/environment, asserting correctness across CRUD, event ingestion → analytics, event → wallet debit, cancel → invoice, auto-generated billing-cycle invoices, and webhook delivery.

## Quick start

```bash
export SYNTHETIC_API_HOST=https://api.cloud.flexprice.io/v1
export SYNTHETIC_API_KEY=<key for the synthetic tenant>
export SYNTHETIC_SLACK_WEBHOOK_URL=<optional>
export OTEL_EXPORTER_OTLP_ENDPOINT=<optional, e.g. signoz-otel:4317>
make run-synthetic
```

## One-time tenant prep (manual)

v1 of seed-ensure auto-creates customers + meters. You'll need to manually pre-provision in the synthetic tenant:

- ≥ 1 plan tagged `metadata.synthetic = "true"` with at least one attached price
- Feature → meter bindings (for each of the 9 seed meters)
- Subscriptions for the 10 persistent customers (one per customer)
- Pre-funded wallets on the first 3 persistent customers (`synthetic-cust-persistent-0`, `-1`, `-2`)

Without these, the affected checks benignly skip. Expanding seed-ensure to cover them is a tracked follow-up.

## Architecture

The harness is built around three abstractions:

- **`Check`** — base interface (Name + Kind + Run). Every unit of work is a Check.
- **`Scheduler`** — controls when a Check runs. Four concrete: `Ticker`, `Rate`, `OneShot`, `Listener`.
- **`Runner`** — owns the Reporter, panic recovery, OTEL spans.

Adding a new probe: write `internal/synthetic/checks/<name>.go` implementing `Check`; register in `cmd/synthetic/main.go` with the appropriate Scheduler.

## Env vars

| Var | Purpose | Default |
| --- | ------- | ------- |
| `SYNTHETIC_API_HOST` | Flexprice API base URL (include /v1) | required |
| `SYNTHETIC_API_KEY` | API key for the synthetic tenant | required |
| `SYNTHETIC_ENABLED` | Master kill switch | `true` |
| `SYNTHETIC_DRY_RUN` | Log mutating calls without sending | `false` |
| `SYNTHETIC_EVENT_INGEST_RATE` | Events/sec for the ingest driver | `5` |
| `SYNTHETIC_EVENT_INGEST_SEED` | RNG seed for event deck | derived from start time |
| `SYNTHETIC_LISTENER_PORT` | HTTP listener port for webhook checks | `8765` |
| `SYNTHETIC_SLACK_WEBHOOK_URL` | Slack webhook (empty disables) | empty |
| `SYNTHETIC_SLACK_CHANNEL` | Override channel | empty |
| `SYNTHETIC_OTEL_ENABLED` | Emit OTEL spans | `true` |
| `SYNTHETIC_CHECK_<NAME>_ENABLED` | Per-check kill switch | `true` |
| `SYNTHETIC_CHECK_<NAME>_INTERVAL` | Per-check interval override (Go duration) | per-check default |

Standard OTLP env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, etc.) flow through unchanged.

## Checks

| Kind | Name | Schedule | What it does |
| ---- | ---- | -------- | ------------ |
| bootstrap | seed-ensure | OneShot + 6h | Create/verify persistent customers + 9 meters |
| driver | event-ingest-driver | Rate(5/s) | Varied event ingest using the deck |
| probe | analytics-probe | 2m | `GetUsageAnalytics` rotating params |
| probe | wallet-balance-probe | 2m | Wallet balance reads |
| probe | wallet-debit-verification | 20m | Quantitative debit assertion |
| probe | cycle-invoice-probe | 15m | Auto-invoice freshness invariant |
| probe | entitlement-and-usage-probe | 5m | Entitlements + usage rollup |
| scenario | new-customer-lifecycle | 10m | Ephemeral customer/sub + events |
| scenario | cancel-customer-flow | 30m | Cancel ephemeral + poll invoice |
| scenario | subscription-modification-flow | 20m | Add line item; verify |
| listener | low-wallet-alert-listener | webhook | Asserts low-balance webhook payloads |
| maintenance | janitor | 1h | Archive synthetic-tagged ephemerals > 4h |

## Webhook wiring (low-wallet-alert-listener)

The listener exposes `POST http://<synthetic-host>:8765/webhook` (port configurable). Wire Flexprice's webhook delivery to it for low-balance alerts. Until wired, the listener sits idle.

## Failure surfacing

Failures fan out to log + Slack + OTEL spans (kind=Error). No retries. Reports include scenario, step, error, and any attributes the check attached.

## Shutdown

SIGTERM cancels all scheduler contexts; the event-ingest AsyncClient flushes; HTTP listener shuts down cleanly. Graceful shutdown is bounded at 30s.
