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
```bash
# Generate Go SDK only (current production pipeline)
make go-sdk

# Quick regeneration during development (no clean)
make regenerate-go-sdk

# Clean and rebuild Go SDK
make clean-go-sdk go-sdk
```

### Infrastructure Services Access
Once services are running:
- **FlexPrice API**: http://localhost:8080
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

**Service Layer** (`internal/service/`)
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
4. Implement service in `internal/service/<entity>.go`
5. Create API handler in `internal/api/v1/<entity>.go`
6. Register route in `internal/api/router.go`
7. Add Swagger annotations to handler
8. Run `make swagger` to update API docs

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
Place tests alongside implementation: `internal/service/billing.go` → `internal/service/billing_test.go`

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
- Tests in `internal/service/*_test.go` often use real dependencies

## Configuration

Configuration is managed via Viper with multiple sources:
1. `internal/config/config.yaml` (defaults)
2. Environment variables (prefix: `FLEXPRICE_`)
3. `.env` file (loaded by godotenv)

Environment variables override config.yaml. Example:
- `FLEXPRICE_POSTGRES_HOST` overrides `postgres.host`
- `FLEXPRICE_KAFKA_BROKERS` overrides `kafka.brokers`

## Common Operations

### Running a Single Test
```bash
go test -v -race ./internal/service -run TestBillingService_GenerateInvoice
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
