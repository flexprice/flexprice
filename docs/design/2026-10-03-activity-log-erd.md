# Activity Log — ERD

Date: 2026-10-03
Status: Implemented in PR #2973 (this document reflects the code as built)
Branch: `feat/activity-log`

## 1. Summary

Flexprice needs an activity log: a per-tenant, immutable record of what changed, who changed it, through which channel, and what the before/after values were, exposed in the dashboard and the public API. The primary user is the tenant auditing their own account. Support, debugging, and compliance are secondary users of the same data.

This spec decides three things:

1. **Do not build on `system_events`.** It is the webhook outbox. Its coverage is the webhook catalog, 79% of its rows have no actor, and its payloads are pointers, not snapshots.
2. **Build a new `activity_logs` table in Postgres**, written inside the business transaction, with a 90-day hot window and an archive of older partitions to Parquet on S3.
3. **Capture through a hybrid of ent mutation hooks and explicit service calls.** Hooks guarantee coverage and compute diffs; services supply semantic action names where they know the intent.

Section 10 records the Postgres-versus-ClickHouse decision in full and the migration path if the decision ever needs to change.

## 2. Goals and non-goals

### Goals

- A tenant can answer "what happened to subscription X, who did it, and what changed" from the dashboard or API within seconds of the change.
- Every mutation on a registered entity is captured, whether it came from the dashboard, the API, a Temporal workflow, a gateway webhook, or a Kafka consumer.
- Actor identity distinguishes user, API key, system workflow, and customer portal. An empty actor is a bug, not a blank.
- One domain, one table, one endpoint, one frontend component. Adding an entity is a registry entry, not a new feature.
- Hot data lives in Postgres for 90 days. Older data is archived to Parquet on S3 and remains retrievable through an export job.
- The schema can later hold security events (user, role, API key lifecycle) and failed actions without a redesign.

### Non-goals for v1

- Backfilling history from `system_events`. The log starts empty at launch.
- Security events and failed-action rows. Columns are reserved; nothing writes them.
- Customer-portal activity feed, CSV export UI, and activity-as-webhook.
- Querying cold data from the dashboard. Cold access is an export job.
- Replacing or changing the webhook pipeline. `system_events` is untouched.

## 3. Context: why not `system_events`

`system_events` is written by exactly one caller, the webhook publisher, at publish time. A row exists only where a service chose to emit a webhook.

Findings from production data (4.1M rows, April to October 2026):

| Finding | Value | Consequence |
|---|---|---|
| Rows with empty `created_by` | 79% | "Who" is unanswerable for most rows |
| Rows for `subscription` | 70% | Dominated by cron and billing-workflow emissions, not user actions |
| Rows for `wallet` | 17% | Mostly `wallet.ongoing_balance.updated`, a balance tick |
| Entities with zero rows | plan, price, addon, coupon, tax, credit grant, API key, user | Nobody emits webhooks for them |
| Payload content | `{entity_id, tenant_id}` | The full entity is fetched at delivery time; no before/after |
| Indexes | `(tenant_id, environment_id)` only | Per-entity queries scan the tenant |

The empty-actor rows trace to four entry points that never set an actor, not to one bug:

1. The alert cron activity sets tenant and environment but no user (`internal/temporal/activities/alerts`).
2. Inbound gateway webhooks build the integration event with no user id, and the handler copies that empty value into context (`internal/integration/events/handler.go`).
3. Kafka usage consumers never set a user id.
4. Cron flows that do set one use the subscription's or transaction's original creator, which misattributes system work to a human.

Extending `system_events` was rejected because the outbox and an audit log want opposite things from the same rows: short-lived and mutated in place versus immutable and long-retained. Every audit-worthy action would also have to become a public webhook event name. And the noise (balance ticks, sync failures, cascaded derivations) is structural, not a filter away.

What `system_events` does prove is that every service already passes through a single choke point to build an event, and that the transaction helper already carries per-transaction hooks. Both are reused.

## 4. Requirements

| # | Requirement | Source |
|---|---|---|
| R1 | Entry visible to the tenant on the next request after the change | Primary use case |
| R2 | Actor is one of user, API key (with owning user), system (with workflow or event name), customer portal | Discussion |
| R3 | Updates carry a field-level diff; creates carry a redacted snapshot; deletes carry the id | Competitor review (Lago, Stripe) |
| R4 | Coverage is guaranteed for registered entities regardless of which code path mutates them | system_events gap analysis |
| R5 | Entries for one request are linkable (shared request id) | Plan change touching line items |
| R6 | 90-day hot retention, older data in Parquet on S3 | User decision |
| R7 | Personal data can be erased on request without rewriting history | GDPR |
| R8 | Schema extends to security events and failed actions without migration of existing rows | User decision |
| R9 | API-key callers can read the log | Primary use case is self-audit via API |

## 5. Architecture

### 5.1 Capture pipeline

```
request / workflow / consumer
        │
        ▼
 actor set in context  ──────────────────────────────┐
        │                                            │
        ▼                                            │
 WithTx installs collector (next to post-commit hooks)│
        │                                            │
        ▼                                            │
 service calls repository ──► ent mutation hook      │
        │                     • registry lookup      │
        │                     • fetch old values     │
        │                     • run mutation         │
        │                     • diff, redact         │
        │                     • append to collector  │
        ▼                                            │
 service optionally calls Recorder.Record(ctx, ...)  │
        │   (semantic action + metadata, order-free) │
        ▼                                            │
 WithTx, before Commit: collector.Flush(tx)          │
        │   • merge entries per entity               │
        │   • named action or generic verb           │
        │   • batch INSERT into activity_logs ◄──────┘ actor, request id
        ▼
 Commit (rollback discards log rows with the data)
```

Components, each with one job:

| Component | Package | Responsibility |
|---|---|---|
| Actor | `internal/types` | Struct in context: type, id, label, owning user id. Set by auth middleware, Temporal activity entry points, webhook consumer, portal middleware |
| Registry | `internal/activity` | Per-entity declaration: ent type name, entity type constant, ignored fields, redacted fields, customer-id derivation, label function, action templates |
| Hook | `internal/activity` | Ent hook on the writer client. Computes raw change records for registered entities |
| Collector | `internal/types` (context) + `internal/activity` | Per-transaction accumulator keyed by entity type and id |
| Recorder | `internal/activity` | Service-facing API to name an action and attach metadata |
| Flush | `internal/activity` | Builds rows and inserts them using the transaction |
| Repository | `internal/repository/ent` | Batch insert via raw SQL; list and get via the ent query builder with a custom cursor predicate |
| Service | `internal/ee/service` | Authorization, display composition |
| Handler | `internal/api/v1` | `GET /v1/activity`, `GET /v1/activity/{id}` |
| Archiver | `internal/temporal/workflows/cron` | Partition maintenance, Parquet export, verify, drop |

### 5.2 Actor model

```go
type ActorType string

const (
    ActorTypeUser           ActorType = "user"
    ActorTypeAPIKey         ActorType = "api_key"
    ActorTypeSystem         ActorType = "system"
    ActorTypeCustomerPortal ActorType = "customer_portal"
)

type Actor struct {
    Type   ActorType
    ID     string // user id, api key id, workflow/event name, customer id
    Label  string // display name captured at write time
    UserID string // owning user for api_key and for system actors derived from a request; empty otherwise
}
```

Set at every entry point:

| Entry point | Actor |
|---|---|
| JWT request | `user`, user id, user name or email |
| API-key request (DB-backed) | `api_key`, secret id, secret name, owning user id |
| API-key request (config-backed) | `api_key`, `config`, "Operator key", user id from config |
| Temporal activity | `system`, workflow type name, set once by a worker interceptor so no activity can miss it |
| Webhook consumer and cascader | `system`, source event name |
| Kafka usage consumer | `system`, consumer group name |
| Customer portal | `customer_portal`, customer id, customer name |

`GetUserID(ctx)` continues to work and returns `Actor.UserID` or `Actor.ID` for users, so `created_by` and `updated_by` on every entity improve as a side effect. The recorder refuses to flush a row whose actor type is empty and logs at error level with the request id. That turns a missing actor at a new entry point into a visible bug.

#### Writes the platform derives inside a request

A user request often makes the platform write more rows than the user asked for (a top-up creates an invoice, a checkout session and a payment). Those derived rows are attributed to a `system` actor that keeps the requester as its owner, so the log says what decided the change and who triggered it.

```
POST /wallets/:id/top-up   user = test@gmail.com
  wallet_transaction.created   actor: user   test@gmail.com
  invoice.created              actor: system "Credit purchase billing"   owner: test@gmail.com
  checkout_session.created     actor: system "Checkout"                  owner: test@gmail.com
  payment.created              actor: system "Checkout"                  owner: test@gmail.com
```

How it works: the actor lives in the request `context`. A call site that does derived work wraps the context with `WithDerivedSystemActor(ctx, id, label)`, which swaps in a system actor carrying the requester as `UserID` (a no-op if the actor is already `system`). The ent hook copies the context's actor onto each pending entry when the write happens, and flush writes every entry with its own actor, falling back to the request actor. `created_by` and `updated_by` keep the requester.

Applied to: credit-purchase invoice creation (`credit_purchase_billing`), the pay-first checkout lifecycle (`checkout`), gateway payment status sync (`payment_sync`), and subscription invoice generation (`subscription_billing`).

#### Wallet balances

The wallet's stored `balance` and `credit_balance` change only when a transaction is applied, so they are recorded as `wallet.updated`. The ongoing (real-time) balance is computed on read and never stored. The balance evaluation passes that run on every usage tick are wrapped in `activity.Suppress` and are not recorded.

### 5.3 Ent hook

Registered once on the writer client via `client.Use(activity.Hook(registry))`. Behaviour by operation:

| Op | Before mutation | After mutation | Record |
|---|---|---|---|
| Create | nothing | read the returned entity | `created`, redacted snapshot |
| UpdateOne, Update (bulk-shaped) | resolve ids via the mutation's `IDs(ctx)`; `SELECT` the columns in `m.Fields()` for those ids inside the transaction | nothing | `updated` with `{field: {from, to}}` for fields whose value actually changed; `deleted` if `status` moved to `deleted` |
| DeleteOne, Delete | resolve ids | nothing | `deleted` |

Repository updates in this codebase are bulk-shaped, `Update().Where(id, tenant, env).Set…()`, 82 sites versus 24 single-row sites. Ent cannot provide old values for them, so the hook fetches old values itself with one `SELECT` per mutation. Because those updates set every column on every save, the diff step must drop unchanged fields; a save with no effective change records nothing.

Dropped before diffing: `updated_at`, `updated_by`, `created_at`, `created_by`, and the registry's per-entity ignore list. Redacted before storing: the registry's per-entity redact list; redacted fields appear in `changes` as `{"redacted": true}` so the reader knows the field changed without seeing the value.

The hook never fails the mutation. A diff error produces a degraded record (action known, `changes` null, `metadata.degraded = "diff_error"`) plus an error log and a counter metric.

### 5.4 Collector and flush

`WithTx` installs the collector next to the existing post-commit hook list. Nested `WithTx` calls reuse the outer collector. A mutation outside any transaction (no collector in context) writes its row directly through the writer client.

Flush runs inside `withTx` immediately before `tx.Commit()`:

1. Merge entries per `(entity_type, entity_id)`: first `from`, last `to`, union of fields. A create followed by an update in one transaction is one `created` row with the final snapshot.
2. Resolve action: the name set by the recorder, else `<entity>.<created|updated|deleted>`.
3. Attach actor, source, request id, customer id (from the registry derivation), occurred_at (transaction time).
4. One batch `INSERT`. An insert failure fails the transaction. There is no silent-skip path.

### 5.5 Recorder

```go
// Record names the action for an entity touched in the current transaction and
// attaches metadata. Safe to call before or after the repository write.
func Record(ctx context.Context, e Entry)

type Entry struct {
    EntityType types.SystemEntityType
    EntityID   string
    Action     string            // "subscription.paused"
    Metadata   map[string]any    // action-specific, e.g. {"reason": "...", "proration_amount": "12.50"}
    Changes    map[string]Change // only for raw-SQL paths that bypass ent
}
```

Services call it where intent exists: pause, resume, cancel, plan change, finalize, void, mark paid, refund. The two raw-SQL write paths (`plan_price_sync*.go`, the SQL-expression update in `price.go`) call it with explicit changes.

`Suppress(ctx, reason string) context.Context` disables collection for the scope of a call. It requires a non-empty reason, which is logged at info level with the entity, so suppression is discoverable in logs rather than silent.

## 6. Data model

### 6.1 Table

Hand-written dbmate migration under `migrations/versioned/postgres/`. The ent schema for `activity_logs` exists for typed reads but carries `entsql.Skip()` so ent auto-migration never creates or alters the table. Precedent: `revenue_facts` has an ent schema and a hand-written migration.

```sql
CREATE TABLE activity_logs (
    id                 VARCHAR(50)  NOT NULL,
    tenant_id          VARCHAR(50)  NOT NULL,
    environment_id     VARCHAR(50)  NOT NULL,
    category           VARCHAR(20)  NOT NULL DEFAULT 'business',
    entity_type        VARCHAR(64)  NOT NULL,
    entity_id          VARCHAR(50)  NOT NULL,
    entity_label       VARCHAR(255) NOT NULL DEFAULT '',
    action             VARCHAR(128) NOT NULL,
    actor_type         VARCHAR(20)  NOT NULL,
    actor_id           VARCHAR(128) NOT NULL,
    actor_label        VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id      VARCHAR(50)  NULL,
    source             VARCHAR(20)  NOT NULL,
    customer_id        VARCHAR(50)  NULL,
    subscription_id    VARCHAR(50)  NULL,
    request_id         VARCHAR(64)  NULL,
    outcome            VARCHAR(20)  NOT NULL DEFAULT 'success',
    error_code         VARCHAR(64)  NULL,
    changes            JSONB        NULL,
    snapshot           JSONB        NULL,
    metadata           JSONB        NULL,
    occurred_at        TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (occurred_at, id)
) PARTITION BY RANGE (occurred_at);

CREATE INDEX activity_logs_entity_idx
    ON activity_logs (tenant_id, environment_id, entity_type, entity_id, occurred_at DESC, id DESC);

CREATE INDEX activity_logs_feed_idx
    ON activity_logs (tenant_id, environment_id, occurred_at DESC, id DESC);

CREATE INDEX activity_logs_customer_idx
    ON activity_logs (tenant_id, environment_id, customer_id, occurred_at DESC, id DESC);

CREATE INDEX activity_logs_subscription_idx
    ON activity_logs (tenant_id, environment_id, subscription_id, occurred_at DESC, id DESC);

CREATE INDEX activity_logs_actor_idx
    ON activity_logs (tenant_id, environment_id, actor_type, actor_id, occurred_at DESC, id DESC);
```

Column notes:

- No `BaseMixin`. Its `status`, `updated_at`, `updated_by` imply mutability; rows are never updated except for erasure.
- `category` is `business` in v1. Reserved: `security`.
- `outcome` is `success` in v1. Reserved: `failure`, with `error_code`.
- `source` is one of `dashboard`, `api`, `workflow`, `webhook`, `consumer`, `portal`.
- `customer_id` and `subscription_id` are first-class because they are the two universal roll-ups in billing (Lago stores both). `subscription_id` is derived exactly like `customer_id`: copied from the row when present, else resolved through the same parent lookup. The registry derives it per entity (subscription → `customer_id`, invoice → `customer_id`, wallet → `customer_id`, customer → own id).
- `entity_label` is the human descriptor captured at write time, so history survives renames and deletions, exactly like `actor_label`. Not every entity has a name, so each registry entry defines a descriptor ladder from `LabelFields`: customer `name → external_id → short id`; subscription `lookup_key → short id` (plan and customer names are resolved on read); wallet `name → currency + wallet_type → short id`; invoice `invoice_number → short id`; plan `name → lookup_key → short id`; price `display_name → lookup_key → amount + currency + period → short id`; payment `amount + currency + gateway → short id`. The hook adds `LabelFields` to the old-value `SELECT` it already issues, so capturing the label costs no extra round trip.
- `changes` shape: `{"status": {"from": "active", "to": "paused"}, "email": {"redacted": true}}`.
- `snapshot` is the redacted created entity. Null for updates and deletes. The registry can cap or omit it per entity (invoices).
- `metadata` holds action-specific context and the `degraded` marker.
- Primary key includes `occurred_at` because Postgres requires the partition key in the primary key. `id` alone is still unique in practice (UUID with prefix `act_`).

### 6.2 Indexes

Five indexes, each one sort order of the same tuple, each serving one of the jobs the log is hired for:

| Index | Job |
|---|---|
| `entity_idx` (tenant, env, entity_type, entity_id, time) | "what happened to this entity" |
| `feed_idx` (tenant, env, time) | "what changed recently" |
| `customer_idx` (tenant, env, customer_id, time) | the customer tab roll-up, which is a v1 surface |
| `subscription_idx` (tenant, env, subscription_id, time) | everything under a subscription including children edited on their own |
| `actor_idx` (tenant, env, actor_type, actor_id, time) | "what did this user or API key do", the first question after an incident or an offboarding |

Equality on the leading columns lands on a contiguous run already ordered by time, so every list query is an index range read with early stop. None can substitute for another: a different leading column means a different physical order. Five B-tree inserts per row at roughly a million rows a month is not a cost worth optimising. Queries always carry a time bound so partition pruning applies; the related-changes lookup by `request_id` bounds itself to the parent row's day.

### 6.3 Partitioning

Monthly range partitions on `occurred_at`, named `activity_logs_YYYY_MM`. The archiver (section 9) creates partitions three months ahead on every run, so an insert never hits a missing partition. The migration creates the current and next two months.

### 6.4 Entity registry

```go
type Definition struct {
    EntType       string                       // ent type name, e.g. "Subscription"
    EntityType    types.SystemEntityType
    Table         string                       // for the old-value SELECT
    LabelFields   []string                     // columns the label ladder needs; fetched with old values
    ParentFields  []string                     // columns CustomerLookup needs
    CustomerLookup func(ctx, Querier, fields) (string, error) // parent-row lookup when no customer_id column
    IgnoreFields  []string                     // never diffed (cached balances, counters)
    RedactFields  []string                     // diffed but value hidden
    SnapshotMode  SnapshotMode                 // full | none
    CustomerID    func(fields map[string]any) string
    Label         func(fields map[string]any) string   // "Invoice INV-0042"
    Actions       map[string]ActionTemplate    // optional per-action wording
    FieldLabels   map[string]FieldDisplay      // optional label + formatter hint
}
```

v1 registrations: every entity that reaches a customer, so the customer roll-up is complete.

| Linkage | Entities |
|---|---|
| Own `customer_id` | customer, subscription, invoice, wallet, wallet transaction, entitlement grant, payment method, credit note, invoice line item, subscription line item, checkout session |
| Via `subscription_id` | subscription phase, subscription schedule, subscription pause, credit grant, credit grant application, coupon association, coupon application |
| Via invoice or payment destination | payment, refund |
| Via `entity_type` / `entity_id` | addon association, tax association |
| Catalog, no customer | plan, price |

Entities without a `customer_id` column declare a `CustomerLookup`, one `SELECT` on the parent row inside the same transaction, and `ParentFields` so the parent key is fetched with the old values. A failed lookup logs and leaves the customer empty; the row is still written.

Wave 2 (catalog and settings): feature, entitlement, addon, coupon, meter, price unit, cost sheet, tax rate, group, settings, alert settings, connection, integration mapping. Wave 3 (`category = security`): user, API key secret, tenant, environment. Deferred children with no direct link: payment attempt, credit note line item, tax applied. Never: system events, incoming webhook events, scheduled tasks, tasks, workflow executions, sequences, revenue facts, analytics views, usage records, alert logs. Wallet transactions and entitlement grants are in v1 because background jobs write them on a customer's behalf, and they carry `customer_id`, so the customer roll-up shows "Workflow CreditGrantProcessing created credit 500 credits" with the wallet and customer linked.

### 6.5 Personal data policy

- `changes` stores raw values for business fields. Fields listed in `RedactFields` (tax ids, payment-method details, addresses if required by policy) store `{"redacted": true}`.
- `actor_label` is captured at write time so history survives user renames and deletions. It holds a display name, never an email for API keys.
- Erasure request for a customer: `UPDATE activity_logs SET changes = <redacted>, snapshot = NULL, actor_label = '[erased]' WHERE tenant_id = $1 AND customer_id = $2` on the hot tier, and a rewrite of that tenant's Parquet objects on the cold tier (section 9.4). This is a documented runbook, not a v1 feature.

## 7. Read side

### 7.1 Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/activity` | List with filters and keyset pagination |
| `GET` | `/v1/activity/{id}` | Single entry |

No per-entity routes. Every surface calls the same list endpoint with different filters.

### 7.2 Filter

```go
type ActivityFilter struct {
    EntityType   *string   `form:"entity_type"`
    EntityID     *string   `form:"entity_id"`
    CustomerID   *string   `form:"customer_id"`
    SubscriptionID *string `form:"subscription_id"`
    ActorType    *string   `form:"actor_type"`
    ExcludeActorTypes []string `form:"exclude_actor_types"`
    ActorID      *string   `form:"actor_id"`
    Actions      []string  `form:"actions"`
    RequestID    *string   `form:"request_id"`
    StartTime    *time.Time `form:"start_time"`
    EndTime      *time.Time `form:"end_time"`
    Cursor       *string   `form:"cursor"`   // opaque, encodes (occurred_at, id)
    Limit        *int      `form:"limit"`    // default 50, max 200
}
```

Keyset pagination, not the repo's usual offset plus total count. Offset on an append-only partitioned table degrades with depth, and a total count across partitions is a full index scan per page. The response carries `next_cursor` and `has_more`; no total. The revenue facts filter already uses a cursor, so the shape is not new.

A list request with no `start_time` and no `end_time` defaults to the hot window. Queries always carry a time bound so partition pruning applies.

### 7.3 Response

```json
{
  "items": [{
    "id": "act_01HX…",
    "entity_type": "subscription",
    "entity_id": "sub_01HX…",
    "action": "subscription.paused",
    "actor": { "type": "api_key", "id": "sec_…", "label": "Billing Sync", "user_id": "user_…", "exists": true },
    "source": "api",
    "customer_id": "cus_…",
    "request_id": "req_…",
    "occurred_at": "2026-10-02T09:00:00Z",
    "changes": {
      "status": { "from": "active", "to": "paused", "label": "Status", "format": "enum" }
    },
    "metadata": { "reason": "customer request" },
    "entity_label": "Growth",
    "display": {
      "summary": "Billing Sync paused Growth",
      "entity_label": "Growth",
      "entity_exists": true
    }
  }],
  "next_cursor": "…",
  "has_more": true
}
```

`display` is computed by the service from the registry. Three tiers of wording:

1. **Generic, no code.** `{actor} {verb} {entity type} {entity label}`, for example "Alice updated wallet USD prepaid" or "Workflow CreditGrantProcessing created wallet transaction credit 500 credits". The entity label is the descriptor only, never the type noun, so the noun can be localised. Verb is the last segment of the action; one changed field is appended inline, several become "updated N fields".
2. **Per-action template, one line in the registry.** `subscription.paused → "{actor} paused {entity}"`.
3. **Custom function, rare.** Only when the sentence needs data outside the row. If it feels like one per action, the data belongs in `metadata` at write time instead.

The frontend ships an Arabic locale, so the sentence cannot be the only representation. `display` also carries `parts`, enough for the client to compose the summary through its own i18n:

```json
"display": {
  "summary": "Billing Sync paused Growth",
  "entity_label": "Growth",
  "parts": { "actor": "Billing Sync", "verb": "paused", "entity_type": "subscription", "entity": "Growth" }
}
```

`verb` is the action's last segment (`paused`, `updated`, `plan_changed`) and doubles as the i18n key; `entity_type` lets the client localise the noun. Change values and counts are not repeated in `parts`: the client reads them from `changes` (before/after per field, and its size is the count). API consumers read `summary`; the dashboard reads `parts`.

Field labels and formatter hints (`money`, `date`, `enum`, `boolean`, `text`) come from `FieldLabels` with snake_case humanization as the default. `metadata` is annotated the same way as `changes`, so the frontend never guesses what a key means.

Reference values are detected by id prefix. The repo's prefix table (`cust_`, `subs_`, `plan_`, `price_`, `inv_`, `wallet_`, `pay_`) maps to entity types, so any `from`, `to`, or metadata value carrying a known prefix gets `format: "ref:<entity_type>"` automatically. No registry entry has to declare that `plan_id` points at a plan. The frontend resolves references to names and links at read time.

### 7.4 Authorization

- Entity-scoped reads (`entity_type` and `entity_id` set, or `customer_id` set) require read permission on that entity type through the existing RBAC middleware.
- The tenant-wide feed requires a new `types.EntityActivity` with `ActionRead`.
- API keys are allowed. System actors are therefore visible to API consumers, which is correct for self-audit.
- Every query filters on tenant and environment from context. No cross-tenant path exists.

### 7.5 Frontend

One timeline component and one detail popup, both generic. The popup has five sections, all fed by the item above: header (summary), who (actor, source), when (occurred_at, request id), what changed (three-column diff table driven by `format`), context (entity link, customer link, metadata, "N related changes in this request" via a `request_id` filter). The only per-entity frontend code is the route map from entity type to page. Nested structures are never diffed: a line item is its own registered entity, linked by `request_id`.

## 8. Write-side details that affect other code

- `internal/postgres/client.go` `withTx`: install the collector after post-commit hooks; call flush before `tx.Commit()`; discard on rollback and panic.
- `internal/postgres/client.go` client construction: `writerClient.Use(activity.Hook(registry))`.
- `internal/rest/middleware/auth.go` `setContextValues`: set `Actor` alongside user id.
- Temporal activity entry points that currently call `SetUserID`: call `SetActor` with a system actor. Entry points that set nothing (alerts, gateway integrations, consumers) gain a system actor.
- `internal/webhook/handler/handler.go`: set a system actor from the event name where it sets `CtxUserID`.
- `types.GetUserID` reads from `Actor` first, then the legacy key.

## 9. Retention and archive

### 9.1 Policy

Hot window: 90 days, configurable in `config.yaml` under `activity.hot_window_days`. A partition is archivable once its upper bound is older than the window, so effective hot retention is 90 to 120 days. Per-tenant retention is a later feature and needs no schema change.

### 9.2 Archiver workflow

Temporal cron, daily, registered alongside the other cron workflows in `internal/temporal/service/schedules.go`.

1. **Maintain partitions.** Create any missing monthly partition up to three months ahead.
2. **Find archivable partitions.** Any partition whose range end is before `now - hot_window_days`.
3. **Export.** For each, stream rows ordered by `tenant_id, occurred_at, id` and write Parquet to `s3://<bucket>/activity_logs/tenant_id=<t>/year=<yyyy>/month=<mm>/part-<n>.parquet`, 50k rows per file, using the Arrow Go Parquet writer. Hive-style keys so Athena, DuckDB, and ClickHouse's `s3()` function read them without a load step.
4. **Verify.** Compare the partition row count to rows written, per tenant. Write a manifest object with counts and object checksums.
5. **Drop.** `ALTER TABLE activity_logs DETACH PARTITION …` then `DROP TABLE …`. Only after a verified manifest.
6. **Idempotence.** A rerun finds the manifest, re-verifies counts against the still-attached partition if present, and skips or resumes.

The archiver touches nothing on the write path and ships as its own deliverable after the log itself.

### 9.3 Cold access

Requests for history older than the hot window run as a Temporal export job: query the tenant's Parquet objects for the date range, filter, write a CSV to S3, and return a signed URL. Because ClickHouse is already deployed, the job uses `SELECT … FROM s3('…/tenant_id=<t>/*/*.parquet', Parquet) WHERE …` as the query engine. No new infrastructure, no live S3 reads from the dashboard.

### 9.4 Erasure on cold data

Parquet files are immutable, so an erasure request rewrites the affected `tenant_id=<t>/year=/month=` objects without the customer's rows, then updates the manifest. Partitioning the S3 layout by tenant keeps each rewrite small. Runbook, not a v1 feature.

## 10. Storage decision: Postgres over ClickHouse

### 10.1 What each layout makes cheap

Postgres stores a row as one contiguous chunk with B-tree indexes beside it. That makes keyed reads, single-row transactional inserts, in-place updates, and uniqueness cheap. It makes wide scans, compression, and long-term storage expensive.

ClickHouse stores each column as a sorted, compressed file inside immutable parts with a sparse index. That makes range scans over few columns, compression (10 to 20x on repetitive columns), aggregates, and time-based lifecycle cheap. It makes point lookups, single-row inserts, updates, deletes, and uniqueness expensive, and it has no transactions.

### 10.2 The activity log workload

| Operation | Shape | Favours |
|---|---|---|
| Write one entry per mutation inside the business transaction | single-row transactional insert | Postgres strongly |
| Entity page: last 50 for one subscription | keyed lookup | Postgres; ClickHouse acceptable |
| Tenant feed: newest 50 | range scan, early stop | both |
| Filter by actor or action over a year, export CSV | wide scan, few columns | ClickHouse strongly |
| Erasure, correcting a bad row | update or delete | Postgres strongly |
| Keep years of history cheaply | compression, tiering | ClickHouse strongly |
| Guarantee no duplicate or missing entry | constraints, transactions | Postgres |

The write side is row-shaped. The long-retention read side is column-shaped.

### 10.3 Why Postgres wins for the stated requirements

1. **R1, visibility on the next request.** Same-transaction insert gives read-your-writes with zero lag. ClickHouse requires an outbox, Kafka, a consumer, dedupe on retry, and a "may be delayed" state in the UI.
2. **R4, no missing entries.** A post-commit publish can lose a row if the process dies between commit and publish. Avoiding that needs a transactional outbox, which is a Postgres table anyway. ClickHouse therefore does not remove Postgres from the write path; it adds a pipeline on top.
3. **R6, 90-day hot window with Parquet archive.** This removes ClickHouse's strongest argument. ClickHouse's native S3 tiering only pays off when the product serves multi-year history in place. With a Parquet archive as the requirement, the same archive job is built either way, and the hot tier is under 10M rows and 20 GB, comfortably Postgres-sized.
4. **R7, erasure.** An in-place `UPDATE` on the hot tier versus a part rewrite or a lightweight delete plus merge.
5. **Operational surface.** Nothing new to run. The ClickHouse path adds a topic, a consumer group, a `ReplacingMergeTree` table with a projection, and a reconciliation job.
6. **Testing.** Integration tests run against Postgres through the existing test utilities. The ClickHouse path needs Kafka and ClickHouse in the test loop or a mocked boundary.

### 10.4 When ClickHouse would be the right answer

- The product sells in-place history measured in years, as Lago does with its enterprise add-on.
- Hot-tier volume exceeds roughly 20M rows a month, where Postgres index maintenance and vacuum on the hot partitions start to matter.
- Tenant-wide analytical queries (per-actor counts, action histograms over a year) become a dashboard feature rather than an export.
- The team accepts seconds-to-minutes of lag and the outbox pipeline as the price.

### 10.5 Migration path if the decision changes

The flush step is the only component that knows where rows go. Migration is additive and reversible:

1. **Outbox.** Flush writes to `activity_logs` as today and additionally to a small `activity_outbox` table in the same transaction. Postgres remains the hot tier throughout.
2. **Relay.** A relay publishes outbox rows to a `flexprice_activity_logs` Kafka topic and deletes them on acknowledgement. Same pattern as the system_events relay.
3. **ClickHouse table.** `ReplacingMergeTree` keyed by `id`, `ORDER BY (tenant_id, environment_id, entity_type, entity_id, occurred_at, id)`, `PARTITION BY toYYYYMM(occurred_at)`, a projection ordered by `(tenant_id, environment_id, occurred_at)` for the feed, and `TTL occurred_at + INTERVAL <hot> DAY TO VOLUME 's3_cold'` for tiering. A per-row `retention_days` column enables per-tenant retention through a TTL expression.
4. **Consumer.** Batches inserts; duplicates from retries collapse on merge; reads use `FINAL` or a `GROUP BY id` until merged.
5. **Read cutover.** The service reads from ClickHouse for ranges beyond the Postgres hot window first, then for everything. The API contract does not change; keyset pagination maps directly.
6. **Retire the archiver.** Once ClickHouse holds full history with native tiering, the Parquet archiver becomes optional. Existing Parquet objects load into ClickHouse with `INSERT … SELECT FROM s3(…)`.
7. **Reconciliation.** A daily job compares per-tenant per-day counts between Postgres hot partitions and ClickHouse while both exist.

Nothing in sections 5, 7, or 8 changes. The entity registry, hook, collector, recorder, actor model, and API are store-agnostic by construction.

## 11. Migrations and DDL

- dbmate migration `migrations/versioned/postgres/<ts>_create_activity_logs.sql` creates the parent table, two indexes, and the first three monthly partitions. `make migrate-new name=create_activity_logs` creates the file; `make migrate-up` applies it.
- `ent/schema/activitylog.go` declares the fields for typed reads and carries `entsql.Skip()` on the schema annotation so `client.Schema.Create` in `cmd/migrate/postgres.go` ignores it. Verified against ent v0.14.6 which supports the annotation.
- Reads use the generated ent query builder against the parent table, with the keyset cursor expressed as a custom `sql.P` predicate on `(occurred_at, id)`. Ent `SELECT`s work on a partitioned parent unchanged.
- Batch inserts use raw SQL through `client.Writer(ctx).ExecContext`, following the precedent in `revenue_fact.go`, so one statement inserts all rows of a transaction's flush.

## 12. Testing

Unit, `internal/activity`:

- Diff: bulk-shaped update with all fields set and one changed yields one field; unchanged save yields no record; ignore list honoured; redact list stores the marker; `status → deleted` maps to the delete action.
- Flush merge: create then update in one transaction yields one created row with final snapshot; two updates merge first-from and last-to.
- Recorder: named action wins over generic; metadata attached; call before or after the mutation gives the same result; suppression requires a reason.
- Actor guard: empty actor type fails flush with an error log.
- Display: tier-1 summary for an unregistered action; tier-2 template substitution; humanized field labels.

Integration, Postgres via `internal/testutil`:

- Mutation inside `WithTx` produces a row; rollback produces none; nested `WithTx` flushes once.
- Actor propagates from JWT middleware, API-key middleware, a Temporal activity, and the webhook consumer.
- Keyset pagination returns stable, non-overlapping pages across a partition boundary.
- Entity-scoped read denied without entity permission; feed denied without activity permission.

End to end, against the running `flexprice-api` container per the local-stack skill:

- Create a customer via API key, pause a subscription via JWT, change a plan via workflow; list activity for each entity and the tenant feed; open an entry; confirm actor, source, changes, and related-request linking.
- Archiver: with `hot_window_days=0` on a scratch partition, run the workflow, confirm Parquet objects in a local MinIO bucket, manifest counts, and the partition dropped.

## 13. Rollout

| Phase | Deliverable | Risk |
|---|---|---|
| 1 | Actor in context at every entry point; `GetUserID` reads from it. Ships alone. Improves `created_by` on webhooks immediately | Low. No new tables |
| 2 | Table migration, registry, hook, collector, recorder, flush. Registry empty, so no rows yet | Low. Hook is a no-op for unregistered types |
| 3 | Register customer and subscription; name their semantic actions | Medium. First real rows; watch insert latency and row sizes |
| 4 | Read API, service, display, RBAC entity | Low |
| 5 | Frontend timeline and popup | Low |
| 6 | Register plan, price, invoice, wallet; then the rest, one PR each | Low |
| 7 | Archiver workflow, partition maintenance, cold export job | Medium. Verify-then-drop must be proven on a scratch partition first |

## 14. Future work (out of scope, schema-ready)

- **Security events**: `category = 'security'` rows written by the recorder directly for user invite, role change, API key create and revoke, SSO changes. No ent entity required.
- **Failed actions**: `outcome = 'failure'` rows written by a request-level middleware after a rolled-back transaction, through a second recorder entry point that writes outside the transaction.
- **Customer portal feed**: `source = 'portal'` filter plus an allow-list of actions.
- **Activity as webhook**: a cascade rule from flush to the webhook publisher for tenants that opt in.
- **Per-tenant retention**: a tenant setting the archiver reads per tenant.

## 15. Appendix: competitor comparison

| | Lago | Metronome | Orb | Stripe |
|---|---|---|---|---|
| UI | Developers → Activity Logs list; Activity Logs tab per object | Audit log page with export | Changelog tab per price only | Events page; admin Activity Logs API |
| Actor | user email or API key id | user name/email or API token name | not documented | user, service account, system |
| Source | `front`, `api`, `system` | request id, IP, user agent | not documented | `dashboard`, `scim`, `sso` |
| Diff | yes; full object on create | no, action and description only | yes per price edit | yes, `previous_attributes` |
| Outcome | committed only | success, failure, pending | n/a | yes |
| Pagination | cursor | cursor, max 100 | n/a | cursor |
| Retention | 30 days premium, unlimited enterprise | not stated | not stated | not stated |
| Storage | explicit per-call capture, serialize before and after, Kafka after commit, ClickHouse | not public | not public | not public |

Takeaways applied: the envelope matches the industry shape; `customer_id` is first-class as in Lago; creates carry a snapshot as in Lago; security events are a separate category as Lago does; failures are reserved as Metronome and Stripe record them; our hook-based capture is deliberately stronger than Lago's explicit calls, which is why Lago documents known gaps.

## 16. Implementation notes (changes since design, 2026-10-03)

These are the decisions and corrections made while building the branch. They refine the design above; where they differ from an earlier section, these win.

### 16.1 Normalization and diff (section 5.1, 5.3)
- `Normalize(v any) string` is canonical across representations, which the original diff logic was not. It formats integers with `strconv` (not `%v`, which printed `1e+06`), floats with `FormatFloat(f,'f',-1,64)`, `decimal.Decimal` and `json.Number` via `.String()`, and parses a `[]byte` as a decimal first (so a `lib/pq` NUMERIC column does not lose precision through float64). Named string types and pointers are unwrapped with `reflect` before formatting. `time.Time` is truncated to microseconds to match Postgres storage, so a re-saved in-memory struct logs no spurious change.
- JSON `null`, a `nil` value, and an empty map/slice all normalize to `""`; a `{}`-versus-NULL flip on a JSONB column is treated as no change.
- The bookkeeping field set the diff ignores now includes `tenant_id` and `environment_id` in addition to the timestamps and actor columns.
- `Diff` skips a field that is absent from the old row and normalizes to empty, so a create does not report every NULL column as a change.
- `Op` has an explicit unset zero value (`opUnknown`); a semantic `RecordAction` fired before the hook no longer turns an update into a create.
- Cleared fields are diffed: the hook adds every `m.ClearedFields()` name as a `nil` new value, so setting a nullable column back to NULL is logged as `from: <old>, to: null`.
- Display of a `[]byte` NUMERIC old value is rendered as a decimal string so both sides of a money change match.

### 16.2 Capture wiring (section 5.3, 5.4)
- The hook is installed through a variadic `postgres.WithActivity(reg)` option, built in `cmd/server/main.go` by a `providePostgresClient` wrapper, and only when `config.Activity.Enabled` is true. The collector is installed in `withTx` only when a registry is present, so scripts and tooling never hit the empty-actor guard.
- The non-transactional direct-write path is the hook's `emit` falling back to the mutation's own `Client()` as the executor (the auth-middleware idea in the original draft was dropped).
- Hard deletes resolve a label and the customer/subscription roll-up with a pre-mutation `SELECT` of `LabelFields` + `ParentFields`, so a deleted child still appears in the customer and subscription feeds.
- The old-value `SELECT` and the customer lookup run inside the business transaction with no `SAVEPOINT`; a wrong table or column name would abort the business write, which is why every registration's names are verified against the ent schema.

### 16.3 Actor coverage (section 5.2)
- Signup, login and SAML JIT provisioning set `SystemActor("signup"|"login"|"saml")` with `SourceDashboard`.
- Kafka/pubsub consumers get their actor from a `WithConsumerActor(group, handler)` wrapper at registration; the inbound gateway webhook handlers and two detached `context.Background()` flows (onboarding, price sync) set their own actor.
- Four existing `CtxUserID` override sites (subscription and trial services, one billing activity) are left unchanged; they run only inside Temporal activities where the worker interceptor's system actor applies.
- Eleven `scripts/internal/*` entry points set a `SystemActor("script:<name>")`. Note: scripts do not install the activity hook today, so this is forward-looking.

### 16.4 Config defaults (section 11)
- `ActivityConfig`/`ArchiveConfig` `default:` struct tags are inert in this loader. A `setActivityDefaults(v)` seeds every `activity.*` key via viper `SetDefault` (enabled=true, hot_window_days=90, archive.destination=local, local_dir, key_prefix=activity_logs, rows_per_file=50000), so a deployment whose mounted config.yaml omits the block still boots with a working hot window and a non-zero rows-per-file.

### 16.5 Entity registry corrections (section 6.4)
- `entitlement_grant` uses `scope_entity_id` and `quota` (the design's `feature_id`/`credits` do not exist on that table).
- `payment_method` redacts `gateway_method_id` and `method_details` (not the originally named columns).
- `customerViaPayment` matches `destination_type = 'INVOICE'` (uppercase, from `types.PaymentDestinationTypeInvoice`); lowercase would have left every payment without a `customer_id`.
- `customers` has no `tax_id` column, so that `RedactFields` entry is inert and harmless.
- Wallet alert suppression is scoped to a separate eval context so Temporal auto-top-up transactions still log.

### 16.6 Partitioning, archive, retention (section 6.3, 9)
- The migration adds a `activity_logs_default` DEFAULT partition as a safety net: a row whose month has no partition lands there instead of failing the business write. The archiver never exports or drops the default partition. Deploy note: apply the migration before rolling the binary, or set `FLEXPRICE_ACTIVITY_ENABLED=false` for the first roll.
- The Parquet `archive.Row` carries all 22 columns including `entity_label` and `subscription_id`; `occurred_at` keeps microsecond precision (it is half the primary key).
- An `s3` destination with no storage instance fails fast rather than writing to the worker's local disk. Storage is wired through `ServiceParams.ActivityArchiveStorage`, nil unless archiving is enabled with destination `s3`.
- Drop re-reads the saved manifest and re-verifies file presence and per-tenant counts before `DETACH`/`DROP`.
- Plan-price sync semantic actions run inside `WithTx` so `RecordAction` lands (the raw-SQL sites fire no hook and `RecordAction` outside a transaction is a no-op).

### 16.7 Read side (section 7)
- Swagger was regenerated for `GET /v1/activity` and `/v1/activity/{id}`. The swag toolchain also renames `meter.Meter` to `Meter` and adds `invoice.sync` webhook paths; both reproduce at the pre-branch HEAD and are pre-existing drift, not this feature.

## 17. Current status / next step / open questions

**Status (2026-10-03).** The backend is implemented and verified on branch `feat/activity-log` (24 commits off `origin/develop`). `make lint-ci` exits 0; `go test ./internal/...` passes 81 packages. End-to-end on the `flexprice-api` Docker container passed all 7 steps (create, update, cancel semantic action, tenant feed, 60-way concurrent pagination, workflow actor), and the archive workflow passed all 7 scenarios (export, drop, round-trip, both idempotence runs, extra-row and missing-object safety, disabled gate). A whole-branch review found 0 Critical and 8 Important issues; all 8 were fixed and re-verified. The frontend is a separate, now-complete branch in `flexprice-front-activity-log`.

**Next step.** (1) Decide the migration-month question below. (2) Push `feat/activity-log` and open the PR per Task 11 Step 3 (not yet pushed). Local Postgres predates the DEFAULT partition; run `CREATE TABLE IF NOT EXISTS activity_logs_default PARTITION OF activity_logs DEFAULT;` once if testing locally.

**Open questions.**
- The migration hard-codes partitions for 2026-10 through 2026-12. An environment first migrated after 2026 would create only past months and then wedge the default partition. Open: compute the current month plus two at apply time in a `DO $$ ... $$` block instead of hard-coding. Recommended before merge.
- With archive enabled and destination `s3`, a missing bucket or bad export config stops boot in every mode including API pods. Acceptable, or should archive config validation be worker-only?
- `RecordAction` has no direct-write path; any future caller outside a transaction silently records nothing. Add a fallback, or keep the WithTx-only contract and document it.
- A missing archived object after its manifest is written makes the daily archive workflow fail until manual intervention; it never drops the partition, so no data loss, but it needs an alert.

## Known limitations

- Integration mappings (`entity_integration_mappings`) are not registered, so linking a customer to Zoho Books, QuickBooks, Stripe or Razorpay is not recorded. Only the copy of the Stripe and Razorpay ids in `customers.metadata` shows up.
- Reference detection by id prefix tags any `pay_…` value as a payment reference, which collides with gateway ids such as Razorpay's `pay_…`. The dashboard renders `gateway_*` fields as plain text; the proper fix is an id-shape check here.
- `entity_type` without `entity_id` is rejected (400). Clients filtering by type alone must use `actions`.
- Checkout session reconciliation through `GET /checkout/sessions/:id` is not yet wrapped as a derived write, so its rows are attributed to the requester.
- Payment rows carry no `customer_id` roll-up, so payments do not appear on a customer's timeline.
