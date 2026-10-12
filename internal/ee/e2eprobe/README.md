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

1. **12 features** (each with an embedded meter): 8 baseline aggregations (COUNT, SUM, AVG, COUNT_UNIQUE, LATEST, MAX, SUM_WITH_MULTIPLIER, SUM/api-filter), 3 bucketed meters (MAX/15MIN, SUM/HOUR, MAX/DAY) for the bucketed-meter-probe, and `e2eprobe_sum_commit` for commitment-true-up-probe — the last one carries no entitlement, so billed usage is exactly units x price with no allowance absorbing the first $1.00.
2. **1 shared coupon** (`E2EPROBE_COUPON_10PCT`, 10% percentage, one-time cadence) reused by coupon-application-probe and attached to persistent cust #1's sub.
3. **1 shared tax rate** (`E2EPROBE_TAX_10PCT`, 10% percentage, EXTERNAL scope) reused by tax-application-probe and attached to persistent cust #0's sub.
4. **10 persistent customers** tagged `metadata.e2eprobe_cohort = "persistent"`.
5. **1 plan** (`e2eprobe_plan`) with metadata `e2eprobe = "true"`.
6. **13 prices** attached to the plan: 1 base recurring fixed fee ($19.99/mo) + 1 usage price per feature ($0.01/unit).
7. **7 plan-level soft-limit entitlements**: one on each non-bucketed metered feature EXCEPT `e2eprobe_sum_multiplier_feature` (reserved for the additive grant, see next bullet) and `e2eprobe_sum_commit_feature` (entitlement-free by design).
8. **1 additive grant entitlement** on `e2eprobe_sum_multiplier_feature` (grant_measure=quantity, quota=1000, duration=1 hour, aggregation_mode=additive). Post-create config-echo verified at seed time via raw HTTP GET, since SDK v2.0.24 doesn't expose grant fields on `EntitlementResponse`.
9. **10 subscriptions** — one per persistent customer — on the e2eprobe plan (monthly, anniversary cycle). New subs carry a $5/mo commitment (1.5× overage factor); cust #1's sub additionally carries the shared coupon via SubscriptionCoupons. Draft subscriptions are activated automatically.
10. **1 tax association** linking the shared tax rate to persistent cust #0's subscription (idempotent — covers both new and existing subs).
11. **3 wallets** on the first 3 persistent customers (`e2eprobe-cust-persistent-0/1/2`), each topped up to $100.00 USD.
12. **Payments plan per currency** (`e2eprobe_payments_plan_<currency>`) with one in-advance fixed price, created by payment-autocharge-probe the first time it runs for that currency.

Subscription line items snapshot the plan at create time, so a feature seeded
after the persistent subs were created is invisible to every usage read path
(they filter by active-subscription line items). `seed-ensure` detects that
drift and triggers the plan price sync; `meter-aggregation-probe` skips any
meter that isn't yet on the customer's subscription rather than paging.

Every step is idempotent: re-running seed-ensure against a tenant that already has all entities is a no-op.

## Airgapped bootstrap

An airgapped deployment has no dashboard and no existing key, so there is no way
to provision `E2EPROBE_API_KEY` up front. Supply `E2EPROBE_EMAIL` and
`E2EPROBE_PASSWORD` instead and the probe provisions its own:

1. `POST /auth/signup` — on `409 already_exists`, falls back to `POST /auth/login`.
2. `GET /environments` — signup creates exactly one.
3. `POST /secrets/api/keys` — mints a private key.
4. Patches the key into the Secret named by `E2EPROBE_BOOTSTRAP_SECRET_NAME`.

An explicit `E2EPROBE_API_KEY` always wins, so existing deployments are
unaffected. Enable it in the chart with `e2eprobe.bootstrap.enabled=true`, which
also mounts the ServiceAccount token and creates a Role scoped by name to that
one Secret (`get` and `patch` only).

**Requires the flexprice-native auth provider** (`auth.provider: "flexprice"`,
the chart default). Supabase-backed deployments reject signup with 403/500, and
a tenant enforcing SSO refuses password login. The probe reports both cases
explicitly rather than as a bare status code.

**Restarts do not mint a second key.** Bootstrap reads the Secret first and
reuses a stored key. This matters because environment variables resolve at
container start: a restart inside the same pod re-reads the original empty
value, so without the read a crash loop would mint keys indefinitely — and
nothing revokes them.

**Outside Kubernetes** (`make run-e2eprobe`, plain Docker) there is no Secret to
write, so the credential is logged once instead; otherwise it would be lost.
In-cluster the key is never logged.

### Deploying the probe on its own

To install a probe-only release — useful for testing bootstrap without touching
an existing deployment — disable the chart's two hook Jobs as well as the app
components. Both block the install otherwise: the migration Job runs
`pre-install` and the Temporal namespace Job runs `post-install`, and each hangs
waiting for infrastructure a probe-only release does not deploy.

```bash
helm install <release> . -n <namespace> \
  --set migration.enabled=false \
  --set temporalConfig.enabled=false \
  --set api.enabled=false --set consumer.enabled=false \
  --set worker.enabled=false --set frontend.enabled=false \
  --set postgresql.enabled=false --set kafka.enabled=false \
  --set clickhouse.enabled=false --set temporal.enabled=false \
  --set e2eprobe.enabled=true --set e2eprobe.bootstrap.enabled=true \
  --set e2eprobe.env.E2EPROBE_API_HOST=https://<region>.api.flexprice.io/v1
```

The probe reaches the API over HTTPS and needs no database of its own, so
skipping the migration Job is safe here — do not skip it for a real deployment.

## Architecture

The harness is built around three abstractions:

- **`Check`** — base interface (Name + Kind + Run). Every unit of work is a Check.
- **`Scheduler`** — controls when a Check runs. Four concrete: `Ticker`, `Rate`, `OneShot`, `Listener`.
- **`Runner`** — owns the Reporter, panic recovery, OTEL spans.

Adding a new probe: write `internal/ee/e2eprobe/checks/<name>.go` implementing `Check`; register in `cmd/e2eprobe/main.go` with the appropriate Scheduler.

## Env vars

| Var | Purpose | Default |
| --- | ------- | ------- |
| `E2EPROBE_API_HOST` | Flexprice API base URL (include /v1) | required |
| `E2EPROBE_API_KEY` | API key for the e2eprobe tenant | required, unless `E2EPROBE_EMAIL` + `E2EPROBE_PASSWORD` are set |
| `E2EPROBE_EMAIL` | Signup email for airgapped bootstrap. Used only when `E2EPROBE_API_KEY` is empty | empty |
| `E2EPROBE_PASSWORD` | Signup password (minimum 8 characters) | empty |
| `E2EPROBE_BOOTSTRAP_SECRET_NAME` | Kubernetes Secret the minted key is written to | empty |
| `E2EPROBE_BOOTSTRAP_SECRET_KEY` | Field within that Secret | empty |
| `POD_NAMESPACE` | Namespace of that Secret; set by the chart via the downward API | empty |
| `E2EPROBE_ENABLED` | Master kill switch | `true` |
| `E2EPROBE_DRY_RUN` | Log mutating calls without sending | `false` |
| `E2EPROBE_TENANT_ID` | Tenant ID included in every Slack/OTEL alert for context | empty (optional but recommended) |
| `E2EPROBE_ENVIRONMENT_ID` | Environment ID included in every Slack/OTEL alert for context | empty (optional but recommended) |
| `E2EPROBE_EVENT_INGEST_RATE` | Events/sec for the ingest driver | `5` |
| `E2EPROBE_EVENT_INGEST_SEED` | RNG seed for event deck | derived from start time |
| `E2EPROBE_LISTENER_PORT` | HTTP listener port for webhook checks | `8765` |
| `E2EPROBE_SLACK_WEBHOOK_URL` | Slack incoming webhook (empty disables) | empty |
| `E2EPROBE_SLACK_BOT_TOKEN` | Slack bot token (`xoxb-…`); posts via `chat.postMessage` and wins over the webhook. Requires `E2EPROBE_SLACK_CHANNEL` | empty |
| `E2EPROBE_SLACK_CHANNEL` | Channel for webhook (override) or bot-token (required) delivery | empty |
| `E2EPROBE_OTEL_ENABLED` | Emit OTEL spans | `true` |
| `E2EPROBE_HEARTBEAT_INTERVAL` | How often a structured heartbeat summary is logged (`0` disables) | `1h` |
| `E2EPROBE_JANITOR_MAX_AGE` | Minimum age of an ephemeral entity before the janitor deletes it (applies to both in-memory sweep and Flexprice orphan scan) | `1h` |
| `E2EPROBE_CHECK_<NAME>_ENABLED` | Per-check kill switch | `true` |
| `E2EPROBE_CHECK_<NAME>_INTERVAL` | Per-check interval override (Go duration) | per-check default |
| `E2EPROBE_PAYMENTS_PROVIDERS` | Gateways connected to the probe environment that the payment probes exercise: `stripe`, `chargebee`, `razorpay` (comma-separated). Empty disables payment probes | empty |
| `E2EPROBE_PAYMENTS_<PROVIDER>_CURRENCY` | Currency the gateway's probes bill in | `USD` (stripe, chargebee), `INR` (razorpay) |
| `E2EPROBE_PAYMENTS_SETTLE_TIMEOUT` | How long a payment or checkout session may take to settle | `90s` |
| `E2EPROBE_PAYMENTS_<PROVIDER>_SETTLE_TIMEOUT` | Per-gateway override of the settle timeout | `10m` for razorpay (test-mode mandate debits capture up to minutes later), else the global value |
| `E2EPROBE_PAYMENTS_ASSERT_KNOWN_ISSUES` | Run legs that fail on known, unfixed product bugs (listed in `knownIssueLegs`, e.g. Chargebee refund and decline). Off skips them | `false` |
| `E2EPROBE_BILLING_MATRIX_ENABLED` | Turns on all five billing-matrix checks and their janitor sweep | `false` |
| `E2EPROBE_BILLING_MATRIX_SCENARIOS_PER_RUN` | Scenarios each billing-matrix check runs per tick; the family is covered in rotation | `3` |
| `E2EPROBE_BILLING_MATRIX_ASSERT_KNOWN_ISSUES` | Run billing-matrix scenarios that hit known, unfixed product bugs. Off logs them as skipped instead of failing | `false` |

Standard OTLP env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, etc.) flow through unchanged.

## Checks

| Kind | Name | Schedule | What it does |
| ---- | ---- | -------- | ------------ |
| bootstrap | seed-ensure | OneShot + 6h | Auto-provision: 12 features/meters, 10 customers, 1 plan, 13 prices, 10 subs, 3 wallets; syncs plan prices onto subs when line items drift |
| driver | event-ingest-driver | Rate(5/s) | Varied event ingest using the deck |
| probe | analytics-probe | 2m | `GetUsageAnalytics` rotating params |
| probe | meter-aggregation-probe | 3m | Asserts each seed meter produces >0 usage over a 30-min window (round-robin; all 8 meters covered every 24 min) |
| probe | wallet-balance-probe | 2m | Wallet balance reads |
| probe | wallet-debit-verification | 20m | Phase 1: TopUp read-after-write correctness; Phase 2: event→analytics aggregation pipeline |
| probe | cycle-invoice-probe | 15m | Auto-invoice freshness invariant |
| probe | entitlement-and-usage-probe | 5m | Entitlements + usage rollup |
| scenario | new-customer-lifecycle | 10m | Ephemeral customer/sub + events |
| scenario | cancel-customer-flow | 30m | Cancel oldest ephemeral sub + delete its customer |
| scenario | subscription-modification-flow | 20m | Add line item; verify |
| listener | low-wallet-alert-listener | webhook | Asserts low-balance webhook payloads and tracks per-wallet, per-alert-type receipts |
| probe | low-balance-alert-probe | 5m | Actively drives the canary wallet across its low-balance threshold and asserts the webhook lands within 2m (Slack-pages on absence) |
| probe | bucketed-meter-probe | 12m | Backdated events → GetUsageAnalytics per-bucket assertion (rotates over 15MIN/HOUR/DAY bucketed features) |
| scenario | commitment-true-up-probe | 17m | Ephemeral sub w/ $5 commitment → preview → assert true-up (under leg) or overage math (over leg) |
| scenario | entitlement-enforcement-probe | 8m | Ephemeral sub → ingest 150 events past soft-limit(100) → usage-summary assertion |
| scenario | tax-application-probe | 15m | Ephemeral sub + tax association → preview → assert `preview.Taxes` includes the seed rate and 10% math |
| scenario | coupon-application-probe | 15m | Ephemeral sub w/ SubscriptionCoupons → preview → assert `preview.CouponApplications` references the seed coupon |
| probe | persistent-billing-invariants-probe | 30m | Cycle invoices for pers cust #0/#1 → assert tax on latest + coupon on oldest (ONCE cadence) |
| scenario | entitlement-grant-additive-probe | 15m | Ephemeral sub inheriting plan additive grant → ingest 200 events → assert usage summary populates |
| scenario | payment-link-probe-`<gateway>` | 20m | Ephemeral customer, no saved method → pay_invoice / wallet_topup / auto-charge-fallback each issue a pending session with a payment URL; cancel expires it with no payment or credit; unattended auto-charge is refused with a 4xx. Natural expiry: each run leaves one top-up link untouched on `e2eprobe-cust-pay-<gateway>-expiry`; a later run (past expiry + one 30m cleanup cycle) asserts the cleanup job failed the top-up, removed its draft invoice/payment, credited nothing and expired the session |
| scenario | payment-method-probe-`<gateway>` | 30m | Saved-method listing (API + portal), setup / add-method links. Unsupported operations must 4xx |
| scenario | payment-autocharge-probe-`<gateway>` | 6h (Razorpay 12h: test card mandates cap daily debits) | Runs on the gateway's fixed customer, the same in every region and hardcoded in `config.go`: `e2eprobe-cust-pay-<gateway>-cards` (Stripe, Chargebee) or `e2eprobe-cust-pay-razorpay-mandate`. Its cards or mandates are saved by hand: two good cards plus the declining test card (last4 `0341` Stripe, `0004` Chargebee). Legs run independently and one alert lists every failing leg, grouped by error. With two good cards, `set_default` alternates the default between them each run. Auto-charged top-up settles via webhook before any reconciling read, completes, credits exactly once; pay_invoice ends SUCCEEDED with full amount; create_subscription ends active; a paid quantity change (modify_subscription) and a prorated in-advance addon (add_addon) are gated on checkout and applied on payment; a full back-to-source refund of the paid invoice marks it REFUNDED and settles to the card (Chargebee, Razorpay) or the wallet (Stripe, which has no gateway refunds); the saved declining card neither completes nor credits. Self-provisions `e2eprobe_payments_addon_<currency>` |
| scenario | billing-matrix-opening | 10m | Creates subs across cadence × calendar/anniversary × anchor (none, near, month-end, before start) × proration × timezone × billing model, each with same, shorter, longer, `count>1`, arrear, usage, one-time and addon items; checks anchor, first period, stored `billing_period_count` and every opening-invoice line against the proration oracle |
| scenario | billing-matrix-renewal | 15m | Same matrix plus short last periods; previews the periods where items renew or close (long items on their own grid, arrear `MONTHLY×6`) and checks each amount |
| scenario | billing-matrix-change | 10m | Mid-period addon add/remove (same, shorter, longer, one-time), `line_item_change` (qty, price, arrear), line item API add/delete and immediate cancel, with and without proration; checks every charge and credit |
| scenario | billing-matrix-grants | 15m | Credit grants at creation (sub cadence, annual on monthly, one-time) and addon credit/entitlement grants attached mid-period: prorated amounts and next application on the billing date |
| scenario | billing-matrix-validation | 30m | Requests the spec rejects (anchor past one period, anchor with calendar, incompatible cadences on create/addon/line item API, backdated `create_prorations`, change dates outside the period) and boundary cases it must accept |
| maintenance | janitor | 1h | Archive in-memory ephemerals > 1h; also scans Flexprice for orphan ephemeral customers (Phase 2) and orphan tax associations (Phase 3) and deletes them |

## Billing matrix

`internal/ee/e2eprobe/billingmatrix` checks billing against an independent oracle of the proration
spec (`docs/design/2026-10-03-proration-unification-erd.md`): billing dates are `anchor + k × period`
clamped to month end, and every charge, credit and grant is `time used ÷ full period`. The oracle
re-derives this without importing `internal/types`, and its unit tests pin the design doc's worked
examples. Renewals are observed through `POST /invoices/preview` with explicit periods, so no clock
needs to pass.

The five billing-matrix checks are off by default because they write billing data every tick;
enable them per deployment with `E2EPROBE_BILLING_MATRIX_ENABLED=true`.

Each scenario creates its own plan, prices, `e2eprobe-cust-eph-bm-*` customer and subscription,
and removes them when it finishes: addons detached, subscription cancelled, wallets terminated
(they block customer deletion), then customer, plan and prices deleted. Anything a crash leaves
behind is removed by the janitor's billing-matrix sweep (plans named `e2eprobe-bm *`, customers
prefixed `e2eprobe-cust-eph-bm-`, older than `E2EPROBE_JANITOR_MAX_AGE`). Addons and the usage and
entitlement-grant features are shared fixtures with stable `e2eprobe_bm_fx_*` lookup keys, reused
across runs, because the API refuses to delete an addon that has ever been on a subscription. Scenarios that hit known product bugs are tagged and skipped unless
`E2EPROBE_BILLING_MATRIX_ASSERT_KNOWN_ISSUES=true`.

Run every scenario once against any server:

```bash
E2EPROBE_API_HOST=http://localhost:8080/v1 E2EPROBE_API_KEY=sk_... \
BM_FAMILIES=opening,renewal,change,grants,validation BM_FILTER=QUARTERLY \
go test -tags e2eprobe_integration ./internal/ee/e2eprobe/billingmatrix -run TestLiveMatrix -v -timeout 2h
```

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
