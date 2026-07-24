---
layer: constitution
repo: flexprice (Go backend)
synced_sha: 8a1b776e6230d469e02f453f16cc54b5d7596a1a
synced_at: 2026-06-09T00:00:00Z
---

# Flexprice Backend — Constitution

> Invariants that MUST hold in every PR. Violation = block merge.
> Operational details → per-layer AGENTS.md files (load lazily by working in that directory).
> Improvement notes → `.context/findings/` (never in this file).

## Stack
Go 1.23+ · Gin · Uber FX (DI) · Ent (ORM) · PostgreSQL · ClickHouse · Kafka · Temporal

## Directory map
| Layer | Path | One-line rule |
|---|---|---|
| Domain | `internal/domain/` | Interfaces + models; zero external deps |
| Repository | `internal/repository/` | Implements domain interfaces; DB access only |
| Service | `internal/service/` | All business logic; orchestrates repos + services |
| API | `internal/api/v1/` | Parse → validate → delegate to service → respond |
| Temporal | `internal/temporal/` | Long-running workflows + activities |
| Integration | `internal/integration/` | Third-party providers; factory pattern |

## Hard invariants

### Layering (never violate)
- No business logic in `internal/api/v1/` — handlers call services, nothing more.
- No DB calls from handlers — all data access through service → repository chain.
- Domain interfaces in `internal/domain/`; implementations in `internal/repository/`.
- All new deps registered in `cmd/server/main.go` via `fx.Provide()`.

### Multi-tenancy (every entity, every query)
- Every DB entity carries `tenant_id` + `environment_id`.
- Every query filters on both. Missing filter = data leak = critical bug.
- No cross-tenant reads. No shared mutable state between tenants.

### Event processing (billing correctness)
- All event handlers MUST be idempotent — duplicate delivery must not alter state twice.
- Event ordering: do not assume arrival order; use event timestamps, not insertion order.
- Retries: every Kafka consumer and Temporal activity must be safe to retry.
- Backfill: any new aggregation must handle historical events correctly.

### ClickHouse
- Every query bounded by `max_memory_usage = 90GB` (hardcoded — do not remove).
- Analytics queries → ClickHouse. Transactional reads/writes → PostgreSQL.

### Observability
- Structured logging via `internal/logger` (ctx-first API); always propagate `ctx` for trace correlation.
- No `fmt.Println` or bare `log.Print` in production paths.
- **Loglint is a merge gate.** Custom analyzer `tools/loglint` runs via `make lint-ci` (`go vet -vettool=./tools/bin/loglint ./internal/...`). Details: `internal/logger/README.md`.
  - **LL006 (common failure):** every `log.Error(ctx, msg, fields...)` MUST include a literal `"error"` key in `fields`. The linter only accepts a string-literal key — `Error(ctx, err.Error())` alone fails.
  - Correct: `s.logger.Error(ctx, "invoice finalize failed", "error", err, "invoice_id", id)`
  - Also correct: `"error", err.Error()` or `"error", "redacted reason"` when you must not log the raw err.
  - Wrong: `s.logger.Error(ctx, err.Error())` — no `"error"` field.
  - Prefer logging at the boundary that owns the failure; intermediate helpers return errors without logging.
  - Other enforced rules: LL001 deprecated logger methods; LL002 no `logger.L` / `GetLogger()` outside `cmd/`+`scripts/`; LL003 `Warn` only in bootstrap (`cmd/`, `main.go`, `init()`, `internal/config/`, `New*` constructors); LL004 no `fmt.Print*`; LL009 `ctx` is the first arg, not a field.

### Testing
- Unit tests for all service-layer business logic.
- Table-driven tests preferred.
- Integration tests use real DB (testcontainers / docker compose); do not mock Ent client.
- Test files alongside implementation (`internal/service/foo_test.go`).

### Schema / migrations
- Schema changes: `ent/schema/*.go` → `make generate-ent` → `make generate-migration`.
- Never hand-edit generated Ent files.
- ClickHouse migrations in `migrations/clickhouse/`.

---
<!-- Below this line: original Warp-targeted content preserved for reference. -->

# AGENTS.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Quick Start Commands

### Development Setup

```bash
# Complete development environment setup (Docker-based)
make dev-setup

# Run application locally (requires infrastructure running)
go run cmd/server/main.go

# Start only infrastructure services
docker compose up -d postgres kafka clickhouse temporal temporal-ui
```

### Running the Application

The application supports multiple deployment modes via `FLEXPRICE_DEPLOYMENT_MODE` environment variable:

- `local` - Runs all services (API, Consumer, Worker) in a single process
- `api` - Runs only the API server
- `consumer` - Runs only the Kafka consumer for event processing
- `temporal_worker` - Runs only Temporal workflow workers

```bash
# Run in local mode (default)
make run-server

# Using Docker Compose
make up  # Start all services
make down  # Stop all services
make restart-flexprice  # Restart only FlexPrice services (not infrastructure)
```

### Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run tests verbosely
make test-verbose
```

### Database Operations

```bash
# Generate Ent code from schema
make generate-ent

# Run database migrations
make migrate-ent

# Dry-run migrations (see SQL without executing)
make migrate-ent-dry-run

# Generate migration file
make generate-migration

# Run PostgreSQL migrations only
make migrate-postgres

# Run ClickHouse migrations only
make migrate-clickhouse
```

### API Documentation

```bash
# Generate Swagger documentation
make swagger

# Generates both Swagger 2.0 and OpenAPI 3.0 specs in docs/swagger/
```

### SDK Generation

SDKs and the MCP server are generated from the OpenAPI spec. Output layout: **api/** (api/go, api/typescript, api/python, api/mcp).

**Source:** [docs/swagger/swagger-3-0.json](docs/swagger/swagger-3-0.json) (regenerate with `make swagger`).

**Commands:**

```bash
# Single command: validate + generate all SDKs/MCP + merge custom (uses existing docs/swagger/swagger-3-0.json)
make sdk-all

# When you change the API, regenerate the spec first, then run sdk-all
make swagger
make sdk-all

# Validate OpenAPI
make speakeasy-validate

# Generate Go SDK (validate + generate + custom merge + build; uses existing swagger)
make go-sdk

# Quick regeneration (no clean)
make regenerate-go-sdk

# Generate all targets (after configuring workflow targets)
make swagger speakeasy-generate
make merge-custom

# Merge custom files only (after any SDK generation run)
make merge-custom
```

**Custom methods and files:** Custom logic lives in `api/custom/<lang>/` (same path structure as api/<lang>/). It is merged into the generated output after every generation via `make merge-custom`. Do not edit generated files under api/<lang>/ for custom code; edit the custom tree so changes survive regeneration. See [api/custom/README.md](api/custom/README.md). READMEs for each SDK and MCP are maintained in `api/custom/<lang>/README.md` and overwrite the generated README on merge; `api/go`, `api/python`, and `api/typescript` also list README in `.genignore` so a generate run without merge-custom does not overwrite the current README.

**MCP server:** Generated in **api/mcp**. Run from that directory (e.g. `npx . start` or per generated README). Auth: set `FLEXPRICE_API_KEY` or the env var documented in the MCP server README. For large tool sets, use dynamic mode (e.g. `--mode dynamic`) to reduce context size; document in api/mcp README. Only operations whose OpenAPI tags are listed in the MCP allowed-tags configuration are included; the filtered spec is built by `make filter-mcp-spec` (runs automatically before `make sdk-all`). To change which tools are exposed, edit `.speakeasy/mcp/allowed-tags.yaml` and run `make filter-mcp-spec` then `make sdk-all`.

**SDK integration tests:** In **api/tests/** – tests for published SDKs only. Repos: Go [go-sdk](https://github.com/flexprice/go-sdk), Python [python-sdk](https://github.com/flexprice/python-sdk), TypeScript [javascript-sdk](https://github.com/flexprice/javascript-sdk), MCP [mcp-server](https://github.com/flexprice/mcp-server). Published packages: `pip install flexprice`, `npm i @flexprice/sdk`, `npm i @flexprice/mcp-server`. Run `make test-sdk` or `make test-sdks`; see [api/tests/README.md](api/tests/README.md). Context: [SDK PR #1288](https://github.com/flexprice/flexprice/pull/1288).

**Publishing:** Single workflow [.github/workflows/generate-sdks.yml](.github/workflows/generate-sdks.yml): on push to main (path-filtered) or workflow_dispatch it runs generate → push to GitHub repos → publish to npm/PyPI. Secrets: `SPEAKEASY_API_KEY`, `SDK_DEPLOY_GIT_TOKEN`, `NPM_TOKEN`, `PYPI_TOKEN`. See [api/README.md](api/README.md#publishing). To test the full pipeline (including artifact upload), run on GitHub; local `act` runs often fail at upload-artifact due to missing `ACTIONS_RUNTIME_TOKEN`.

**Best practices checklist (per release):**

| Area           | Practices                                                                                                              |
| -------------- | ---------------------------------------------------------------------------------------------------------------------- |
| **OpenAPI**    | operationId, summary, description, tags, schema docs; use overlays for MCP metadata; validate before generate          |
| **SDK**        | Type safety, sdkClassName Flexprice, retries, minimal deps, idiomatic per language, README + repoUrl in .speakeasy/gen/*.yaml |
| **MCP**        | Scopes, clear tool names/descriptions, dynamic mode for scale, mcpbManifestOverlay, auth docs, validateResponse choice |
| **Resilience** | Retries with backoff, timeouts, rate-limit awareness                                                                   |
| **CI**         | Generate on spec change, run merge-custom after generate, build/test SDKs, version and publish                         |

### Infrastructure Services Access

Once services are running:

- **FlexPrice API**: http://localhost:8080
- **API base URL:** When configuring SDKs or MCP, always include `/v1` (e.g. `https://us.api.flexprice.io/v1` or `http://localhost:8080/v1`); no trailing space or slash.
- **Temporal UI**: http://localhost:8088
- **Kafka UI**: http://localhost:8084 (requires `--profile dev`)
- **ClickHouse**: http://localhost:8123

### Kafka Operations

```bash
# Initialize Kafka topics
make init-kafka

# Access Kafka UI
docker compose --profile dev up -d kafka-ui
```

## Architecture Overview

### Technology Stack

- **Language**: Go 1.23+
- **Web Framework**: Gin
- **Dependency Injection**: Uber FX
- **ORM**: Ent (Entity Framework for Go)
- **Databases**: PostgreSQL (transactional), ClickHouse (analytics/events)
- **Message Queue**: Kafka
- **Workflow Engine**: Temporal
- **API Documentation**: Swaggo

### Project Structure

```
flexprice/
├── cmd/
│   ├── server/          # Main application entry point
│   └── migrate/         # Database migration tool
├── ent/
│   └── schema/          # Ent entity schemas (data models)
├── internal/
│   ├── api/             # HTTP handlers and routing
│   │   ├── v1/          # API v1 handlers
│   │   └── cron/        # Scheduled job handlers
│   ├── domain/          # Domain models and interfaces
│   ├── repository/      # Data access layer implementations
│   ├── service/         # Business logic layer
│   ├── temporal/        # Temporal workflows and activities
│   │   ├── workflows/   # Workflow definitions
│   │   └── activities/  # Activity implementations
│   ├── integration/     # Third-party integrations (Stripe, Chargebee, etc.)
│   ├── config/          # Configuration management
│   ├── postgres/        # PostgreSQL client
│   ├── clickhouse/      # ClickHouse client
│   ├── kafka/           # Kafka producer/consumer
│   └── ...              # Other infrastructure packages
├── migrations/
│   ├── postgres/        # PostgreSQL migrations
│   └── clickhouse/      # ClickHouse migrations
└── api/                 # Generated SDKs
    ├── go/
    ├── python/
    └── javascript/
```

### Layered Architecture

**Domain Layer** (`internal/domain/`)

- Core business entities and domain models
- Repository interfaces (not implementations)
- No external dependencies

**Repository Layer** (`internal/repository/`)

- Implements domain repository interfaces
- Direct database access (Ent, ClickHouse, etc.)
- Data mapping and persistence

**Service Layer** (`internal/ee/service/`)

- Business logic orchestration
- Transaction management
- Uses repository interfaces
- Integrates with Temporal workflows

**API Layer** (`internal/api/`)

- HTTP request/response handling
- DTO conversions
- Request validation
- No business logic

**Integration Layer** (`internal/integration/`)

- Third-party service integrations (Stripe, Chargebee, Razorpay, HubSpot, QuickBooks, etc.)
- Factory pattern for provider instantiation

### Key Architectural Patterns

**Dependency Injection**: Uses Uber FX throughout. All dependencies are provided in `cmd/server/main.go` via `fx.Provide()` and consumed via function parameters.

**Repository Pattern**: Interfaces defined in domain layer, implementations in repository layer. Example:

```go
// Domain interface
type EventRepository interface {
    Create(context.Context, *Event) error
}

// Repository implementation
type clickhouseEventRepository struct { ... }
```

**Service Composition**: Services depend on repository interfaces and other services. Complex operations are composed from smaller service methods.

**Temporal Workflows**: Long-running business processes (billing cycles, invoice processing, subscription changes) are implemented as Temporal workflows for reliability and observability.

**Pub/Sub Pattern**: Event processing uses Kafka with multiple consumer groups for different processing stages:

- `events` topic: Raw event ingestion
- `events_lazy` topic: Deferred processing
- `events_post_processing` topic: Post-processing pipeline
- `system_events` topic: Internal system events and webhooks

## Core Domain Concepts

### Tenancy & Multi-Environment

- **Tenant**: Top-level isolation boundary (represents a company/organization)
- **Environment**: Within each tenant (e.g., production, staging, development)
- All entities are scoped to tenant + environment

### Billing Entities

- **Customer**: End user/organization being billed
- **Plan**: Pricing model definition (seats, usage tiers, features)
- **Subscription**: Active plan assignment to a customer
- **Invoice**: Generated billing document
- **Payment**: Payment transaction records

### Metering & Usage

- **Meter**: Defines what to measure (API calls, compute time, storage)
- **Event**: Raw usage data ingested into the system
- **Feature**: Capabilities with usage limits or toggles
- **Entitlement**: Customer's access to features based on plan

### Credits & Discounts

- **Wallet**: Prepaid credit balance for a customer
- **CreditGrant**: Allocation of credits (prepaid or promotional)
- **Coupon**: Discount codes and rules
- **CreditNote**: Refund or credit memo

### Pricing

- **Price**: Atomic pricing unit (per-seat, per-GB, etc.)
- **PriceUnit**: Unit of measurement for pricing
- **Addon**: Optional add-ons to plans
- **CostSheet**: Usage-based pricing calculations

## Key Development Patterns

### Ent Schema Changes

1. Modify schema in `ent/schema/*.go`
2. Run `make generate-ent` to generate code
3. Run `make migrate-ent` to apply to database
4. For production: Use `make generate-migration` to create SQL file

### Adding a New API Endpoint

1. Define domain model in `internal/domain/<entity>/`
2. Create/update Ent schema in `ent/schema/<entity>.go`
3. Implement repository in `internal/repository/<entity>.go`
4. Implement service in `internal/ee/service/<entity>.go`
5. Create API handler in `internal/api/v1/<entity>.go`
6. Register route in `internal/api/router.go`
7. Add Swagger annotations to handler (including `@x-scope` for MCP - see below)
8. Run `make swagger` to update API docs

#### MCP Scope Annotations

FlexPrice MCP tools are categorized by permission scope using the `@x-scope` annotation in Swagger comments. This allows Claude and other MCP clients to mount only specific permission levels (e.g., read-only mode).

**Available scopes:**
- `read` - Read-only operations (GET requests, queries)
- `write` - Create/update operations (POST/PUT/PATCH)
- `delete` - Destructive operations (DELETE, finalization, void)

**When to add `@x-scope`:**

Most operations get automatic scope assignment based on HTTP method (GET→read, POST→write, DELETE→delete). Add `@x-scope` explicitly in these cases:

1. **For clarity** (recommended but optional for standard cases):
   ```go
   // @Summary Get customer
   // @ID getCustomer
   // @Tags Customers
   // @x-scope "read"
   // @Router /customers/{id} [get]
   ```

2. **For overrides** (required when HTTP method doesn't match semantic):
   ```go
   // @Summary Query customers (POST for complex query body, but read-only)
   // @ID queryCustomer
   // @Tags Customers
   // @x-scope "read"  // Override POST→write default
   // @Router /customers/search [post]
   
   // @Summary Finalize invoice (POST but destructive/irreversible)
   // @ID finalizeInvoice
   // @Tags Invoices
   // @x-scope "delete"  // Override POST→write default
   // @Router /invoices/{id}/finalize [post]
   ```

**Scope assignment logic:**
1. If `@x-scope` is present → use explicit scope
2. Otherwise → automatic based on HTTP method (GET/HEAD→read, POST/PUT/PATCH→write, DELETE→delete)

After adding/changing endpoints, regenerate: `make swagger && make sdk-all`

### Creating a Temporal Workflow

1. Define workflow interface in `internal/temporal/workflows/<name>_workflow.go`
2. Implement activities in `internal/temporal/activities/`
3. Register in `internal/temporal/registration.go`
4. Start workflow from service layer using `TemporalService`

### Integrating a Payment Provider

1. Create provider package in `internal/integration/<provider>/`
2. Implement common interfaces (payment, invoice sync, etc.)
3. Register in `internal/integration/factory.go`
4. Add configuration in `internal/config/config.yaml`

### Event Processing Flow

1. Events ingested via API → Kafka (`events` topic)
2. Consumer reads from Kafka
3. Processed by `EventConsumptionService` or `FeatureUsageTrackingService`
4. Stored in ClickHouse for analytics
5. Triggers downstream workflows (metering, alerting, billing)

## Testing Conventions

### Test File Location

Place tests alongside implementation: `internal/ee/service/billing.go` → `internal/ee/service/billing_test.go`

### Test Utilities

Use `internal/testutil/` for:

- Database setup (`testutil.SetupTestDB()`)
- Creating test fixtures
- Mock services and repositories

### Table-Driven Tests

Prefer table-driven tests for multiple scenarios:

```go
tests := []struct {
    name    string
    input   Input
    want    Output
    wantErr bool
}{
    // test cases...
}
```

### Integration Tests

- Use actual database instances (via testcontainers or docker compose)
- Avoid mocking Ent client; use real DB for integration tests
- Tests in `internal/ee/service/*_test.go` often use real dependencies

## Configuration

Configuration is managed via Viper with multiple sources:

1. `internal/config/config.yaml` (defaults)
2. Environment variables (prefix: `FLEXPRICE_`)
3. `.env` file (loaded by godotenv)

Environment variables override config.yaml. Example:

- `FLEXPRICE_POSTGRES_HOST` overrides `postgres.host`
- `FLEXPRICE_KAFKA_BROKERS` overrides `kafka.brokers`

**ClickHouse per-query memory limit:** Every ClickHouse query is bounded by a hardcoded limit of 90 GB (`max_memory_usage`).

## Common Operations

### Running a Single Test

```bash
go test -v -race ./internal/ee/service -run TestBillingService_GenerateInvoice
```

### Viewing Logs

Services use structured logging via `internal/logger`:

```bash
# API logs
docker compose logs -f flexprice-api

# Consumer logs
docker compose logs -f flexprice-consumer

# Worker logs
docker compose logs -f flexprice-worker
```

### Accessing PostgreSQL

```bash
docker compose exec postgres psql -U flexprice -d flexprice
```

### Accessing ClickHouse

```bash
docker compose exec clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice
```

### Temporal UI

Access Temporal UI at http://localhost:8088 to:

- Monitor workflow executions
- Debug failed workflows
- Manually trigger workflows
- View workflow history

## Production Deployment Modes

The application can run in split mode for scalability:

- **API Mode**: Handles HTTP requests only
- **Consumer Mode**: Processes Kafka events only
- **Worker Mode**: Runs Temporal workflows only

Set via environment variable:

```bash
export FLEXPRICE_DEPLOYMENT_MODE=api  # or consumer, temporal_worker
```

Docker Compose demonstrates this pattern with separate services: `flexprice-api`, `flexprice-consumer`, `flexprice-worker`.

## License & Enterprise Features

- Core is AGPLv3 licensed
- Enterprise features (`internal/ee/`) require commercial license
- See LICENSE file for details

## Cursor Cloud specific instructions

Durable, non-obvious notes for running this backend inside a Cursor Cloud VM. Standard
commands live in `SETUP.md`, `LOCAL_TESTING.md`, `docs/REMOTE_DEV_INSTANCE_SETUP.md`, and
the `Makefile`; this section only records the gotchas that are not obvious from those docs.

### Two supported setup approaches
1. **Self-contained Docker local (default in the Cloud VM).** Committed `.env.local` +
   local Docker infra + `make run-local`. Needs no external secrets — this is what the
   Cloud VM is provisioned for and what the steps below use.
2. **Against shared staging infra** (see `docs/REMOTE_DEV_INSTANCE_SETUP.md`). Point a
   locally-built server at already-deployed Postgres/Kafka/ClickHouse/Temporal by adding a
   `./.env` (chmod `600`, staging creds from the team secrets manager, **never committed**)
   and running `make run-server`. Use this only when staging credentials are supplied to
   the VM as secrets; do not create `.env` if you want the local-Docker path.

**Critical env-loading distinction** (drives which approach runs):
`make run-server` loads `.env` only; the `make run-local*` targets load `.env` **then layer
`.env.local` on top**, so local Docker endpoints override any staging values in `.env`.
Likewise `make migrate-local` uses `.env.local` while `make migrate-ent` uses `.env`.

Toolchain PATH (Go pinned by `go.mod`, `typst` from `scripts/install-typst.sh`):
`export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"`.

### Docker daemon is not auto-started
Docker is pre-installed in the VM image but there is **no systemd/service manager**, so
`dockerd` is not running on a fresh session. Start it once per session before any
`docker`/`make` target that touches infra:

```bash
sudo dockerd > /tmp/dockerd.log 2>&1 &      # wait ~5s for the socket
sudo chmod 666 /var/run/docker.sock          # let the non-root user reach the socket
```

The daemon uses `fuse-overlayfs` with the containerd snapshotter disabled (see
`/etc/docker/daemon.json`) — this is required because the VM kernel lacks full overlay2
support. Do not switch the storage driver back to overlay2.

### Bring up the stack (all infra is Docker, app runs via `go run`)
```bash
docker compose up -d postgres kafka clickhouse temporal temporal-ui
make migrate-postgres        # docker exec: creates extensions
make migrate-clickhouse      # docker exec: applies migrations/clickhouse/*
make migrate-local           # Ent schema migration — uses .env.local (NOT make migrate-ent)
make seed-db                 # seeds the default tenant/environment
make init-kafka              # creates topics
make run-local               # single process: API + consumer + workers
```

Then `curl http://localhost:8080/health` → `{"status":"ok"}`. Auth for `/v1/*` needs both
`-H "x-api-key: sk_local_flexprice_test_key"` and
`-H "x-environment-id: 00000000-0000-0000-0000-000000000000"`.

### Non-obvious caveats
- **Use `make migrate-local`, never `make migrate-ent`** for local Ent migrations —
  `migrate-ent` reads `.env` (which can point at production); `migrate-local` reads the
  committed `.env.local`. Same warning in `LOCAL_TESTING.md`.
- **Temporal is disabled by default locally** (`FLEXPRICE_TEMPORAL_ENABLED="false"` in
  `.env.local`), so `make run-local` boots even if the `temporal` container is not up. Set
  it to `true` (and start the container) only when testing workflows.
- **`make init-kafka` does not create the webhook topic.** The webhook consumer subscribes
  to `flexprice_system_events` (from `webhook.topic` in `config.yaml`), which is not in the
  `init-kafka` list. Without it the server logs a harmless-but-noisy `topic ... does not
  exist` reconnect loop. Create it once to silence the loop:
  `docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic flexprice_system_events --partitions 1 --replication-factor 1`.
- **Tests need no external infra.** `internal/testutil` provides in-memory stores, so
  `make test` (Go `-race` over `./internal/...`) runs without the Docker stack. `make test`
  first runs `scripts/install-typst.sh`, which downloads the `typst` binary to
  `~/.local/bin` (needs network) and appends that dir to `~/.bashrc`.
- **`make lint` is non-blocking** (prints `LL008` dev-checkpoint warnings and exits 0). Use
  `make lint-ci` for the errors-only gate.

### Integration sanity suite (`integration-testing-suite/go`)
End-to-end billing lifecycle against a running server (see `make test-suite`). To run it
against the local server, mint a DB-backed key (it embeds the environment, so the suite
needs no `x-environment-id`) and point a target at `localhost:8080/v1`:

```bash
# sign up -> returns JWT + tenant_id; then create a private key scoped to the tenant's env
curl -s -X POST localhost:8080/v1/auth/signup -H 'Content-Type: application/json' \
  -d '{"email":"dev@example.com","password":"password12345","tenant_name":"Dev"}'
# GET /v1/environments (Bearer <token>) -> env id; then:
curl -s -X POST localhost:8080/v1/secrets/api/keys -H "Authorization: Bearer <token>" \
  -H "X-Environment-ID: <env>" -H 'Content-Type: application/json' \
  -d '{"name":"suite","type":"private_key"}'          # returns api_key once (sk_...)

FLEXPRICE_API_KEY=<sk_...> FLEXPRICE_API_HOST=localhost:8080/v1 make test-suite
```

Notes: the suite exits non-zero only on **core** (Phase 1–5) failures; Phase 7 cleanup
failures (e.g. `DeleteTaxAssociation` "unknown content-type … Status 200", an SDK quirk)
are reported but non-fatal. The usage-processing wait (Phase 4) may time out in ~30s on a
cold consumer without failing the run. The suite pins the Go SDK (`go-sdk/v2`, latest —
currently `v2.1.20`), whose `go >= 1.25.10` requirement bumps the suite's `go` directive;
the base toolchain on the box is older, so Go auto-downloads the required `1.25.x` toolchain
(cached after the first build) — do not force `GOTOOLCHAIN=local`.

## Coding style guide

1. Whenever creating new structs, keep them private, and expose their getters and constructors with proper nil handling and use those in code. Keep the structs and it's fields private and only expose them via getters with nil handlings.
2. When updating entities, use their builders. If builder doesn't exist, create it and then use and set only the required fields. Builders should have always initiate by taking in input an existing entity and provide a builder instance of it.
3. Only add comments when some logic or definition is complex to understand or there is an edge case. Don't write comments on generic logic and easy to understand structs and methods.