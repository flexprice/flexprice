# Architecture memory (persistent context for AI and humans)

This document is **long-term institutional memory**: principles, layering, conventions, constraints, and event patterns. Prefer it over guessing from fragmented files.

**Update triggers:** Changes to layering rules, deployment modes, tenancy model, Kafka topic strategy, Temporal queue layout, RBAC semantics, or any “golden path” ingestion/billing flows.

See also [`REPO_MAP.md`](REPO_MAP.md), [`DEPENDENCY_GRAPH.md`](DEPENDENCY_GRAPH.md), [`HOTSPOTS.md`](HOTSPOTS.md), [`FLOWS/`](FLOWS/).

---

## Product scope (one paragraph)

FlexPrice is monetization infrastructure: **metering**, **pricing**, **subscriptions**, **invoicing/payments**, **credits/wallets**, **integrations** (payments/CRM/accounting/shipping-tax providers), plus **analytics and exports**.

---

## Architectural principles

1. **Domain-first contracts** — Behavior is expressed against interfaces in `internal/domain`; adapters implement persistence and IO.
2. **Explicit composition** — Prefer Uber Fx wiring in `cmd/server/main.go` over ad hoc singletons; **exception today:** Temporal global accessor (`GetGlobalTemporalService`) exists but treat as legacy convenience.
3. **Separation of OLTP vs OLAP** — PostgreSQL (Ent) for authoritative billing state; ClickHouse for high-volume metering tables and aggregates.
4. **Reliability for side effects** — Long-running orchestration belongs in Temporal; Kafka consumers wrap Watermill retries + DLQ/poison behavior.
5. **Multi-tenant safety** — Every request carries tenant/environment context from middleware-derived `context.Context`.
6. **Enterprise boundary** — `internal/ee/**` encapsulates commercially licensed deltas; compose via Fx similarly to core services.

---

## Layering strategy

| Layer | Location | Responsibility | May import |
| ----- | -------- | ------------- | ----------- |
| **Domain** | `internal/domain/` | Entities, repo interfaces | stdlib / shared primitives only |
| **Repository** | `internal/repository/` | Persist domain models via Ent / ClickHouse | domain, infra clients |
| **Service** | `internal/service/` | Business rules orchestration | domain, repos, integrations, Temporal client interfaces, publishers |
| **API** | `internal/api/` | HTTP serialization, routing, coarse validation | services, dto, middleware |
| **Integration** | `internal/integration/` | Third-party adapters | dto/mapping, HTTP clients |

**Anti-pattern:** Handlers performing SQL, Ent calls, or Temporal workflow assembly without going through services (some historical exceptions may exist—do not propagate).

---

## Domain boundaries (coarse aggregates)

Grouped by subdirectory under `internal/domain/` (mirrors bounded contexts):

- **Tenancy:** `tenant`, `environment`, `settings`, `user`, `secret`, `group`
- **Catalog & pricing:** `plan`, `price`, `priceunit`, `addon*`, `feature`, `entitlement`
- **Customer & subscription lifecycle:** `customer`, `subscription` (+ phases, schedules, line items), `coupon*`, tax models
- **Metering:** `events` (+ derivatives like processed/raw/meter usages as repository interfaces permit)
- **Billing documents:** `invoice`, `payment`, `creditnote*`, `wallet`, `creditgrant*`, `workflowexecution`

---

## Infrastructure decisions

| Concern | Choice | Notes |
| ------- | ------ | ----- |
| HTTP | Gin | Recovery + custom middleware ordering in `internal/api/router.go` |
| DI | uber/fx | All providers in `cmd/server/main.go` |
| OLTP DB | PostgreSQL + Ent | Schemas live in `ent/schema` |
| Analytics DB | ClickHouse | Specialized repositories under `repository/clickhouse/` |
| Streaming | Kafka + Watermill | Producer in `kafka.Producer`; consumers via `pubsub` |
| Messaging resilience | Middleware stack | Poison queue / DLQ if `kafka.topic_dlq` configured; Retry with backoff in `internal/pubsub/router/router.go` |
| Workflows | Temporal | Client + worker registry in `internal/temporal` |
| AuthN | JWT (Bearer) OR API keys | `internal/rest/middleware/auth.go`; config keys validated first, DB-backed secrets fallback |
| AuthZ | RBAC service + permission middleware | `internal/rbac`, `middleware.NewPermissionMiddleware` |
| Observability | Sentry, Pyroscope hooks | Wired as Gin middleware |

---

## Deployment & process model

Controlled by **`FLEXPRICE_DEPLOYMENT_MODE`** (see [`REPO_MAP.md`](REPO_MAP.md) table). This is intentional **logical sharding**:

- Separate API latency from Kafka consumer backlog processing.
- Isolate Temporal worker CPU from HTTP.

---

## Event flow patterns

### Metering ingestion (happy path)

1. Authenticated REST call under `/v1/events` (individual or bulk variants per router).
2. `EventService` validates and maps payloads to domain models.
3. `publisher.EventPublisher` routes to Kafka and/or DynamoDB depending on runtime config (`config.Event`).
4. `EventConsumptionService` (and sibling lazy/replay consumers) subscribe via Watermill, apply throttles (`middleware.NewThrottle` per handler registration), persist into ClickHouse.
5. Derivative trackers (`feature_usage_tracking`, cost sheet meter usage, etc.) subscribe to overlapping topics/partitions consistent with YAML consumer groups — **consult `internal/config/config.yaml` when changing concurrency semantics**.

### System / outbound webhooks

Producers enqueue onto configured **system event** Kafka topic; **`internal/webhook`** consumes, builds payloads (pull model against services), emits via Svix or native transports per configuration.

---

## Repository conventions

- **Schemas:** Edit `ent/schema/*.go`; run **`make generate-ent`** then **`make migrate-ent`** (or generate SQL migrations for prod flow per `Makefile`).
- **Migrations folders:**  
  - `migrations/postgres/` — foundational SQL snapshots / seeds  
  - `migrations/clickhouse/` — ClickHouse DDL lineage  
  Always confirm whether Ent auto-migrations vs explicit SQL applies in deployment target before shipping risky DDL.
- **Tests:** Prefer colocated `_test.go` files; reuse `internal/testutil` for DB scaffolding.

---

## Coding patterns

### Service constructors

Prefer explicit dependencies; when inheriting **`ServiceParams`**, be disciplined about which repos you actually touch to reduce accidental coupling.

### Context propagation

Tenant / user / optional environment reside in typed context keys (`internal/types`). Middleware sets them (`setContextValues`); downstream code reads via accessors (e.g., `types.GetTenantID`).

### Error handling strategies

Central error mapping layers exist (varies by handler); preserve user-safe messages vs logged internals.

---

## System constraints / sharp edges

- **ClickHouse memory guard:** Queries are bounded (`max_memory_usage` hardcoded to a large GB limit per project lore in `CLAUDE.md`/`AGENTS.md` — verify constant if tuning analytics).
- **Watermill retries:** Defaults in router (currently max 3 retries, exponential backoff capped) — lengthening blindly can stall partitions.
- **Single binary operational complexity:** Operational confusion if environment variables misconfigure mode (consumer without Kafka fatal in `cmd/server/main.go`).
- **License split:** AGPL core vs EE packages — do not move proprietary logic into wrong tree accidentally.

---

## Graphify (code knowledge graph)

When **`graphify-out/graph.json`** exists in this repo, agents should **query the graph before** large blind searches:

- `graphify query "<question>"` — scoped subgraph for codebase questions
- `graphify path "<A>" "<B>"` — relationships between concepts or symbols
- `graphify explain "<concept>"` — focused explanations from the graph

Navigation helpers when generated: **`graphify-out/wiki/index.md`**; broad fallback: **`graphify-out/GRAPH_REPORT.md`** (only when query/path/explain are not enough).

**After changes that affect package structure, imports, or public surfaces**, refresh the graph from repo root (AST-only, no API cost per internal tooling notes):

```bash
graphify update .
```

**Day-to-day:** run the same command after a large `git pull` or before a long session that will use `graphify query`—whether or not `graphify-out/` already exists (first run typically **creates** the tree; later runs **refresh** it). If your Graphify build documents a separate first-time command, prefer that once, then use `graphify update .` for routine sync.

If Graphify is not installed or the graph has never been generated, rely on this repository’s `docs/REPO_MAP.md`, `docs/DEPENDENCY_GRAPH.md`, and source inspection.

**Cross-repo workflow:** Personal Cursor skill **`repo-architecture-intelligence`** at `~/.cursor/skills/repo-architecture-intelligence/` — tell the agent *refresh Graphify / bootstrap graph / run repo-architecture-intelligence* so it checks for `graph.json`, runs `graphify update .` or falls back to scan-only docs. See **`USAGE.md`** next to that skill for copy-paste prompts.

---

## Continuous documentation maintenance

Treat markdown under `docs/` as **living state**:

1. **Structural change** → update `REPO_MAP.md` + relevant `FLOWS/*` sections.
2. **New coupling** (globals, topics, Temporal schedules) → update `DEPENDENCY_GRAPH.md`.
3. **New risk / large edits** → update `HOTSPOTS.md`.
4. Major architecture shifts → update this file FIRST to avoid conflicting agent guidance.
5. **Graphify present** → run `graphify update .` after material structural edits so query/path/explain stay accurate.

The Cursor rule at `.cursor/rules/architecture.mdc` reinforces this lifecycle.
