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
# Start all infrastructure services
docker compose up -d postgres kafka clickhouse temporal temporal-ui

# Run the application locally
go run cmd/server/main.go

# Or use make
make run-server
```

## Testing

```bash
# Run all tests
make test
# or
go test -v -race ./internal/...

# Run a single test
go test -v -race ./internal/service -run TestBillingService_GenerateInvoice

# Run tests with coverage
make test-coverage
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

# Run database migrations
make migrate-ent

# Generate migration file (for production)
make generate-migration
```

## API Documentation

```bash
# Generate Swagger docs
make swagger
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
│   ├── api/             # HTTP handlers and routing (v1/, cron/)
│   ├── domain/          # Domain models and repository interfaces
│   ├── repository/      # Data access layer implementations
│   ├── service/         # Business logic layer
│   ├── temporal/        # Temporal workflows and activities
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
5. **Integration Layer** (`internal/integration/`) — Third-party service integrations, factory pattern

### Key Patterns

- **Dependency Injection**: Uber FX throughout; all deps provided in `cmd/server/main.go` via `fx.Provide()`
- **Repository Pattern**: Interfaces in domain layer, implementations in repository layer
- **Temporal Workflows**: Long-running processes (billing cycles, invoice processing) are Temporal workflows
- **Pub/Sub**: Event processing via Kafka; topics: `events`, `events_lazy`, `events_post_processing`, `system_events`

## Core Domain Concepts

| Concept | Description |
|---------|-------------|
| Tenant | Top-level isolation boundary (company/organization) |
| Environment | Within a tenant (production, staging, development) |
| Customer | End user/organization being billed |
| Plan | Pricing model definition |
| Subscription | Active plan assignment to a customer |
| Meter | Defines what to measure (API calls, compute time, storage) |
| Event | Raw usage data ingested into the system |
| Feature | Capabilities with usage limits or toggles |
| Wallet | Prepaid credit balance for a customer |
| Invoice | Generated billing document |

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

## Testing Conventions

- **File location**: Tests live alongside implementation (e.g., `internal/service/billing_test.go`)
- **Test utilities**: Use `internal/testutil/` for DB setup, fixtures, mocks
- **Table-driven tests**: Preferred for multiple scenarios
- **Integration tests**: Use real DB instances (via testcontainers or docker compose); avoid mocking Ent client

## Configuration

Configuration is managed via Viper:
1. `internal/config/config.yaml` (defaults)
2. Environment variables (prefix: `FLEXPRICE_`)
3. `.env` file (loaded by godotenv)

Example: `FLEXPRICE_POSTGRES_HOST` overrides `postgres.host`

## Deployment Modes

Set via `FLEXPRICE_DEPLOYMENT_MODE`:
- `local` — Runs all services (API, Consumer, Worker) in single process
- `api` — HTTP API only
- `consumer` — Kafka consumer only
- `temporal_worker` — Temporal workers only

## Infrastructure Access (Local Dev)

| Service | URL |
|---------|-----|
| FlexPrice API | http://localhost:8080 |
| Temporal UI | http://localhost:8088 |
| Kafka UI | http://localhost:8084 (requires `--profile dev`) |
| ClickHouse HTTP | http://localhost:8123 |

## License

Core is AGPLv3 licensed. Enterprise features (`internal/ee/`) require a commercial license.
