@AGENTS.md

# CLAUDE.md

This file provides guidance to Claude Code when working with the Flexprice backend repository.

## Project Overview

Flexprice is a monetization infrastructure platform for AI-native and SaaS companies. It provides usage-based metering, credit management, flexible pricing, and automated invoicing.

- **Language**: Go 1.23+
- **Framework**: Gin (HTTP), Uber FX (DI), Ent (ORM)
- **Databases**: PostgreSQL (transactional), ClickHouse (analytics/events)
- **Messaging**: Kafka
- **Workflow Engine**: Temporal

## Quick Start Commands

```bash
# Complete development environment setup (Docker-based)
make dev-setup

# Start only infrastructure services
docker compose up -d postgres kafka clickhouse temporal temporal-ui

# Run the application locally
make run-server
# or
go run cmd/server/main.go

# Start all services via Docker Compose
make up

# Stop all services
make down

# Restart only FlexPrice services (not infrastructure)
make restart-flexprice
```

## Testing

```bash
# Run all tests
make test

# Run tests verbosely
make test-verbose

# Run tests with coverage
make test-coverage

# Run a single test by name
go test -v -race ./internal/service -run TestBillingService_GenerateInvoice
```

## Linting & Vetting

```bash
# Vet code
go vet ./...

# Format code
gofmt -w .
```

## Database Operations

```bash
# Generate Ent code from schema
make generate-ent

# Apply migrations
make migrate-ent

# Dry-run migration (see SQL without executing)
make migrate-ent-dry-run

# Generate migration SQL file (for production)
make generate-migration

# Run PostgreSQL migrations only
make migrate-postgres

# Run ClickHouse migrations only
make migrate-clickhouse
```

## API Documentation

```bash
# Generate both Swagger 2.0 and OpenAPI 3.0 specs in docs/swagger/
make swagger
```

## SDK Generation

```bash
# Generate Go SDK (current production pipeline)
make go-sdk

# Quick regeneration during development (no clean)
make regenerate-go-sdk
```

## Kafka Operations

```bash
# Initialize Kafka topics
make init-kafka

# Access Kafka UI (requires --profile dev)
docker compose --profile dev up -d kafka-ui
```

## Architecture

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
│   ├── domain/          # Domain models and repository interfaces
│   ├── repository/      # Data access layer implementations
│   ├── service/         # Business logic layer
│   ├── temporal/        # Temporal workflows and activities
│   │   ├── workflows/   # Workflow definitions
│   │   └── activities/  # Activity implementations
│   ├── integration/     # Third-party integrations (Stripe, Chargebee, etc.)
│   ├── config/          # Configuration management
│   ├── kafka/           # Kafka producer/consumer
│   └── testutil/        # Test utilities and fixtures
├── migrations/
│   ├── postgres/        # PostgreSQL migrations
│   └── clickhouse/      # ClickHouse migrations
└── api/                 # Generated SDKs (Go, Python, JavaScript)
```

### Layered Architecture

1. **Domain Layer** (`internal/domain/`) — Core business entities, repository interfaces, no external dependencies
2. **Repository Layer** (`internal/repository/`) — Implements domain interfaces, direct DB access via Ent/ClickHouse
3. **Service Layer** (`internal/service/`) — Business logic orchestration, transaction management
4. **API Layer** (`internal/api/`) — HTTP request/response, DTO conversions, request validation (no business logic)
5. **Integration Layer** (`internal/integration/`) — Third-party service integrations (Stripe, Chargebee, Razorpay, HubSpot, QuickBooks, etc.), factory pattern

### Key Patterns

- **Dependency Injection**: Uber FX throughout; all deps provided in `cmd/server/main.go` via `fx.Provide()`
- **Repository Pattern**: Interfaces in domain layer, implementations in repository layer
- **Service Composition**: Services depend on repository interfaces and other services; complex operations composed from smaller service methods
- **Temporal Workflows**: Long-running processes (billing cycles, invoice processing, subscription changes) are Temporal workflows for reliability and observability
- **Pub/Sub**: Event processing via Kafka topics: `events`, `events_lazy`, `events_post_processing`, `system_events`

## Core Domain Concepts

### Tenancy & Multi-Environment
- **Tenant** — Top-level isolation boundary (company/organization)
- **Environment** — Within each tenant (e.g., production, staging, development); all entities are scoped to tenant + environment

### Billing Entities
- **Customer** — End user/organization being billed
- **Plan** — Pricing model definition (seats, usage tiers, features)
- **Subscription** — Active plan assignment to a customer
- **Invoice** — Generated billing document
- **Payment** — Payment transaction records

### Metering & Usage
- **Meter** — Defines what to measure (API calls, compute time, storage)
- **Event** — Raw usage data ingested into the system
- **Feature** — Capabilities with usage limits or toggles
- **Entitlement** — Customer's access to features based on their plan

### Credits & Discounts
- **Wallet** — Prepaid credit balance for a customer
- **CreditGrant** — Allocation of credits (prepaid or promotional)
- **Coupon** — Discount codes and rules
- **CreditNote** — Refund or credit memo

### Pricing
- **Price** — Atomic pricing unit (per-seat, per-GB, etc.)
- **Addon** — Optional add-ons to plans
- **CostSheet** — Usage-based pricing calculations

## Development Workflows

### Adding a New API Endpoint

1. Define domain model in `internal/domain/<entity>/`
2. Create/update Ent schema in `ent/schema/<entity>.go`
3. Implement repository in `internal/repository/<entity>.go`
4. Implement service in `internal/service/<entity>.go`
5. Create API handler in `internal/api/v1/<entity>.go`
6. Register route in `internal/api/router.go`
7. Add Swagger annotations, then run `make swagger`

### Ent Schema Changes

1. Modify schema in `ent/schema/*.go`
2. Run `make generate-ent`
3. Run `make migrate-ent` (or `make generate-migration` for production SQL)

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

1. Events ingested via API → published to Kafka (`events` topic)
2. Consumer reads from Kafka
3. Processed by `EventConsumptionService` or `FeatureUsageTrackingService`
4. Stored in ClickHouse for analytics
5. Triggers downstream workflows (metering, alerting, billing)

## Testing Conventions

- **File location**: Tests live alongside implementation (e.g., `internal/service/billing_test.go`)
- **Test utilities**: Use `internal/testutil/` for DB setup (`testutil.SetupTestDB()`), fixtures, and mocks
- **Table-driven tests**: Preferred for multiple scenarios
- **Integration tests**: Use real DB instances (via testcontainers or docker compose); avoid mocking Ent client

## Configuration

Configuration is managed via Viper with multiple sources (later sources override earlier):
1. `internal/config/config.yaml` (defaults)
2. Environment variables (prefix: `FLEXPRICE_`)
3. `.env` file (loaded by godotenv)

Examples: `FLEXPRICE_POSTGRES_HOST` overrides `postgres.host`, `FLEXPRICE_KAFKA_BROKERS` overrides `kafka.brokers`

**ClickHouse per-query memory limit:** Every ClickHouse query is bounded by a hardcoded limit of 90 GB (`max_memory_usage`).

## Deployment Modes

Set via `FLEXPRICE_DEPLOYMENT_MODE`:
- `local` — Runs all services (API, Consumer, Worker) in single process
- `api` — HTTP API only
- `consumer` — Kafka consumer only
- `temporal_worker` — Temporal workers only

Docker Compose runs these as separate services: `flexprice-api`, `flexprice-consumer`, `flexprice-worker`.

## Infrastructure Access (Local Dev)

| Service | URL |
|---------|-----|
| FlexPrice API | http://localhost:8080 |
| Temporal UI | http://localhost:8088 |
| Kafka UI | http://localhost:8084 (requires `--profile dev`) |
| ClickHouse HTTP | http://localhost:8123 |

## Common Operations

```bash
# Access PostgreSQL
docker compose exec postgres psql -U flexprice -d flexprice

# Access ClickHouse
docker compose exec clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice

# View service logs
docker compose logs -f flexprice-api
docker compose logs -f flexprice-consumer
docker compose logs -f flexprice-worker
```

Temporal UI (http://localhost:8088): monitor/debug workflow executions, manually trigger workflows, view workflow history.

## License

Core is AGPLv3 licensed. Enterprise features (`internal/ee/`) require a commercial license.

## Graphify

This project can maintain a code knowledge graph under **`graphify-out/`** (when the Graphify CLI is used).

- If **`graphify-out/graph.json`** exists, prefer **`graphify query "<question>"`** for codebase questions, **`graphify path "<A>" "<B>"`** for relationships, and **`graphify explain "<concept>"`** for focused subgraphs.
- If **`graphify-out/wiki/index.md`** exists, use it for broad navigation instead of undirected browsing.
- Read **`graphify-out/GRAPH_REPORT.md`** only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code in ways that change structure or dependencies, run **`graphify update .`** from the repository root to keep the graph current (AST-only). Use the **same command** day-to-day and for a **first-time** graph (creates `graphify-out/` when the CLI supports it); confirm with `graphify --help` if your build uses a separate init step.

Persistent human/agent-oriented architecture docs live under **`docs/`** (`REPO_MAP.md`, `ARCHITECTURE.md`, `DEPENDENCY_GRAPH.md`, `HOTSPOTS.md`, `FLOWS/`). For the same workflow in **any** repository, use the personal Cursor skill **`repo-architecture-intelligence`** in `~/.cursor/skills/repo-architecture-intelligence/SKILL.md` and copy prompts from **`USAGE.md`** in that folder.

