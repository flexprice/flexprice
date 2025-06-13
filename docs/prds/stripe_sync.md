# FlexPrice Stripe Integration - Product Requirements Document

## Overview

This PRD defines the implementation of Stripe integration for FlexPrice, enabling bidirectional synchronization between FlexPrice's usage-based billing system and Stripe's billing infrastructure. The integration provides an enhancement layer for existing Stripe customers while offering a migration pathway.

**Key Constraints:**

- **Volume**: 25M events/month (~35,000/hour average, ~60,000/hour peak)
- **Timeline**: 5-day MVP implementation
- **Architecture**: Minimal changes leveraging existing FlexPrice infrastructure
- **Scope**: Backend-only integration (no frontend components)

## System Architecture

The integration extends FlexPrice's existing clean architecture:

```
┌─────────────────────────────────────────────────────────────┐
│                    Stripe Integration Layer                  │
├─────────────────────────────────────────────────────────────┤
│  API Layer     │ Service Layer  │ Repository   │ Domain     │
│  (webhooks,    │ (orchestration │ (data access │ (entities, │
│   endpoints)   │  & business    │  & external  │  logic)    │
│                │    logic)      │   API calls) │            │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│                    FlexPrice Core System                    │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Tasks

### Task 1: Database Schema Extensions

**Objective**: Add database entities for Stripe integration mapping and sync tracking.

**Files to Modify:**

- `/ent/schema/customer_integration_mapping.go` (create new)
- `/ent/schema/stripe_sync_batch.go` (create new)
- `/ent/schema/stripe_tenant_config.go` (create new)
- `/ent/schema/meter_provider_mapping.go` (create new)

**Actions:**

1. **Create CustomerIntegrationMapping schema** - Add new Ent schema with fields: `customer_id`, `provider_type`, `provider_customer_id`, `tenant_id`, `environment_id`, `metadata`, `created_at`, `updated_at`
2. **Create StripeSyncBatch schema** - Add schema with fields: `id`, `tenant_id`, `environment_id`, `customer_id`, `meter_id`, `event_type`, `aggregated_quantity`, `event_count`, `stripe_event_id`, `sync_status`, `retry_count`, `error_message`, `window_start`, `window_end`, `created_at`, `synced_at`
3. **Create StripeTenantConfig schema** - Add schema with fields: `tenant_id`, `environment_id`, `api_key_encrypted`, `sync_enabled`, `aggregation_window_minutes`, `webhook_config`, `created_at`, `updated_at`
4. **Create MeterProviderMapping schema** - Add schema with fields: `meter_id`, `provider_type`, `provider_meter_id`, `tenant_id`, `environment_id`, `sync_enabled`, `configuration`, `created_at`
5. **Run database migrations** - Generate and execute Ent migrations for new schemas

### Task 2: Domain Layer Extensions

**Objective**: Create domain entities and interfaces for Stripe integration.

**Files to Create:**

- `/internal/domain/integration/customer_mapping.go`
- `/internal/domain/integration/stripe_sync.go`
- `/internal/domain/integration/repository.go`
- `/internal/domain/stripe/client.go`
- `/internal/domain/stripe/entities.go`

**Actions:**

1. **Create CustomerIntegrationMapping domain entity** - Define domain model with validation logic, conversion methods from/to Ent entities
2. **Create StripeSyncBatch domain entity** - Define domain model for sync batch tracking with status management and retry logic
3. **Create integration repository interfaces** - Define interfaces for customer mapping operations, sync batch operations, and configuration management
4. **Create Stripe domain entities** - Define Stripe API request/response models for customer webhooks and meter events
5. **Add domain validation** - Implement validation logic for integration entities and configuration

### Task 3: Repository Layer Implementation

**Objective**: Implement data access layer for integration entities and external Stripe API calls.

**Files to Create:**

- `/internal/repository/ent/customer_integration_mapping.go`
- `/internal/repository/ent/stripe_sync_batch.go`
- `/internal/repository/ent/stripe_tenant_config.go`
- `/internal/repository/ent/meter_provider_mapping.go`
- `/internal/repository/stripe/client.go`

**Files to Modify:**

- `/internal/repository/factory.go`

**Actions:**

1. **Implement CustomerIntegrationMapping repository** - Add CRUD operations with tenant/environment filtering, lookup by provider customer ID, and bulk operations
2. **Implement StripeSyncBatch repository** - Add batch creation, status updates, retry management, and cleanup operations for old batches
3. **Implement StripeTenantConfig repository** - Add configuration CRUD with encrypted API key handling and tenant-specific retrieval
4. **Implement MeterProviderMapping repository** - Add meter mapping CRUD with validation for duplicate mappings and sync status management
5. **Create Stripe API client** - Implement HTTP client for Stripe API calls with rate limiting, retry logic, and webhook signature validation
6. **Update repository factory** - Register new repositories in dependency injection container

### Task 4: Stripe Webhook Handler

**Objective**: Extend existing webhook infrastructure to handle Stripe customer.created webhooks.

**Files to Create:**

- `/internal/api/v1/stripe_webhook.go`
- `/internal/webhook/handler/stripe_handler.go`
- `/internal/webhook/dto/stripe_webhook.go`

**Files to Modify:**

- `/internal/api/router.go`
- `/internal/webhook/module.go`

**Actions:**

1. **Create Stripe webhook endpoint** - Add POST `/webhooks/stripe` endpoint with signature validation, tenant identification, and error handling
2. **Implement Stripe webhook handler** - Add handler for `customer.created` events with metadata parsing, customer lookup/creation, and integration mapping
3. **Create Stripe webhook DTOs** - Define request/response models for Stripe webhook payloads with proper validation
4. **Register webhook routes** - Add Stripe webhook routes to API router with proper middleware
5. **Update webhook module** - Register Stripe webhook handler in dependency injection system

### Task 5: Temporal Workflow Implementation

**Objective**: Create Temporal workflow for hourly event aggregation and Stripe synchronization.

**Files to Create:**

- `/internal/temporal/workflows/stripe_sync_workflow.go`
- `/internal/temporal/activities/stripe_sync_activity.go`
- `/internal/temporal/models/stripe_sync_models.go`

**Files to Modify:**

- `/internal/temporal/registration.go`
- `/internal/temporal/worker.go`

**Actions:**

1. **Create StripeEventSyncWorkflow** - Implement hourly cron workflow with grace period handling, error recovery, and batch processing coordination
2. **Create event aggregation activity** - Query events_processed table for billable events within time window, group by (customer_id, meter_id, event_type), and sum quantities
3. **Create Stripe API sync activity** - Send aggregated events to Stripe meter API with rate limiting, idempotency keys, and error handling
4. **Create batch tracking activity** - Record sync attempts, update status, and manage retry logic for failed batches
5. **Register workflows and activities** - Add new workflow and activities to Temporal registration
6. **Configure cron schedule** - Set up hourly execution with configurable delay for grace period

### Task 6: Service Layer Orchestration

**Objective**: Implement business logic services for Stripe integration operations.

**Files to Create:**

- `/internal/service/stripe_integration.go`
- `/internal/service/customer_integration.go`

**Files to Modify:**

- `/internal/service/params.go`

**Actions:**

1. **Create StripeIntegrationService interface and implementation** - Add methods for configuration management, sync status monitoring, and manual sync triggering
2. **Create CustomerIntegrationService interface and implementation** - Add methods for customer mapping creation/updates, lookup operations, and bulk migration support
3. **Implement webhook processing service** - Add business logic for processing Stripe webhooks, validating metadata, and creating customer mappings
4. **Implement sync orchestration service** - Add logic for coordinating Temporal workflows, managing sync batches, and handling errors
5. **Update service parameters** - Add new repositories and dependencies to service parameter struct

### Task 7: ClickHouse Query Optimization

**Objective**: Optimize events_processed queries for hourly batch aggregation at scale.

**Files to Modify:**

- `/internal/repository/clickhouse/processed_event.go`

**Actions:**

1. **Add GetBillableEventsForSync method** - Create optimized query to fetch events_processed where qty_billable > 0 within time window, grouped by (customer_id, meter_id, event_type)
2. **Implement batch aggregation query** - Use SUM(qty_billable) and COUNT(\*) aggregations with efficient time-based partitioning and tenant/environment filtering
3. **Add integration mapping joins** - Join with customer integration mappings to resolve Stripe customer IDs during aggregation
4. **Optimize for high volume** - Use appropriate ClickHouse FINAL modifiers, proper indexing hints, and batch size limits for 25M+ events/month
5. **Add query performance monitoring** - Include query execution time tracking and result set size monitoring

### Task 8: Configuration Management

**Objective**: Implement tenant-specific configuration management for Stripe integration.

**Files to Create:**

- `/internal/api/v1/stripe_config.go`
- `/internal/config/stripe.go`

**Files to Modify:**

- `/internal/config/config.go`

**Actions:**

1. **Create Stripe configuration API endpoints** - Add CRUD endpoints for tenant Stripe configuration with API key encryption and meter mapping management
2. **Implement configuration validation** - Add validation for Stripe API keys, meter mapping uniqueness, and configuration completeness
3. **Add encryption/decryption for API keys** - Implement secure storage of Stripe API keys using existing FlexPrice encryption patterns
4. **Create configuration DTOs** - Define request/response models for Stripe configuration API with proper field validation
5. **Update global configuration** - Add Stripe integration feature flags and default settings to main configuration structure

### Task 9: Error Handling and Monitoring

**Objective**: Implement comprehensive error handling, retry logic, and monitoring for the integration.

**Files to Create:**

- `/internal/errors/stripe_errors.go`
- `/internal/monitoring/stripe_metrics.go`

**Files to Modify:**

- `/internal/sentry/service.go`

**Actions:**

1. **Define Stripe-specific error types** - Create error types for API failures, rate limits, authentication issues, and configuration errors with proper error codes
2. **Implement retry strategies** - Add exponential backoff for rate limits (5 retries), linear retry for network issues (3 retries), and immediate failure for auth errors
3. **Add circuit breaker pattern** - Implement circuit breaker for Stripe API calls to prevent cascade failures during Stripe outages
4. **Create monitoring metrics** - Add metrics for sync success rates, API call latencies, error rates, and batch processing times
5. **Integrate with Sentry** - Ensure all Stripe integration errors are properly reported to Sentry with contextual information

### Task 10: Migration and Bulk Operations

**Objective**: Implement one-time migration script and bulk customer onboarding utilities.

**Files to Create:**

- `/scripts/stripe_migration.go`
- `/internal/api/v1/stripe_migration.go`

**Actions:**

1. **Create migration script** - Implement script to bulk import existing Stripe customers per tenant+environment with progress tracking and error recovery
2. **Add bulk customer mapping API** - Create endpoint for manual bulk customer mapping with CSV upload support and batch processing
3. **Implement validation and conflict resolution** - Add logic to handle existing customer conflicts, missing external IDs, and partial migration scenarios
4. **Add migration status tracking** - Implement progress tracking, error reporting, and rollback capabilities for migration operations
5. **Create migration utilities** - Add helper functions for data validation, duplicate detection, and mapping verification

## Configuration Requirements

### Environment Variables

- `STRIPE_WEBHOOK_SECRET` - Global webhook signature validation secret
- `STRIPE_SYNC_GRACE_PERIOD_MINUTES` - Configurable grace period (default: 5 minutes)
- `STRIPE_BATCH_SIZE_LIMIT` - Maximum events per sync batch (default: 10000)
- `STRIPE_API_TIMEOUT_SECONDS` - API call timeout (default: 30 seconds)

### Database Configuration

- Set up proper indexes on new tables for optimal query performance
- Configure appropriate partitioning for sync batch tables
- Enable row-level security for tenant isolation

### Temporal Configuration

- Register new workflows and activities in worker
- Configure appropriate retry policies and timeouts
- Set up monitoring and alerting for workflow failures

## API Endpoints

### Stripe Webhook Endpoint

- `POST /webhooks/stripe` - Receive Stripe webhooks with signature validation

### Configuration Management

- `GET /stripe/config` - Get tenant Stripe configuration
- `PUT /stripe/config` - Update tenant Stripe configuration
- `POST /stripe/config/test` - Test Stripe API connection

### Migration and Bulk Operations

- `POST /stripe/migrate/customers` - Bulk migrate existing Stripe customers
- `GET /stripe/migrate/status/{migration_id}` - Get migration status
- `POST /stripe/sync/manual` - Trigger manual sync for debugging

### Monitoring and Status

- `GET /stripe/sync/status` - Get sync status and metrics
- `GET /stripe/sync/batches` - List recent sync batches with filtering

## Data Flow

### Customer Onboarding Flow

1. **Stripe Customer Creation** - Client creates customer in Stripe with metadata: `{external_id: "client_123"}`
2. **Webhook Processing** - Stripe sends `customer.created` webhook to FlexPrice
3. **Customer Mapping** - FlexPrice creates/updates customer and integration mapping
4. **Ready for Events** - Customer is ready to receive usage events

### Event Synchronization Flow

1. **Event Processing** - FlexPrice processes events into events_processed table with qty_billable calculated
2. **Hourly Aggregation** - Temporal workflow queries events_processed for last hour (with grace period)
3. **Batch Creation** - Create sync batches grouped by (customer_id, meter_id, event_type)
4. **Stripe API Sync** - Send aggregated meter events to Stripe with idempotency keys
5. **Status Tracking** - Update batch status and retry failed batches

## Performance Considerations

### ClickHouse Query Optimization

- Use proper time-based partitioning for events_processed queries
- Implement batch size limits to prevent memory issues
- Add query execution time monitoring and alerting

### Stripe API Rate Limiting

- Implement per-tenant rate limiting based on Stripe account limits
- Use circuit breaker pattern to handle extended outages
- Add request queuing for burst traffic handling

### High Volume Handling

- Process events in configurable batch sizes (default: 1000 events per batch)
- Use parallel processing for multiple tenant synchronization
- Implement graceful degradation during peak traffic

## Security Requirements

### API Key Management

- Encrypt Stripe API keys at rest using FlexPrice encryption
- Implement key rotation capability with zero-downtime updates
- Add audit logging for all API key access and modifications

### Webhook Security

- Validate Stripe webhook signatures on all incoming requests
- Implement replay attack protection with timestamp validation
- Add IP allowlisting for Stripe webhook sources

### Data Protection

- Ensure all customer data mapping complies with existing FlexPrice security
- Implement proper tenant isolation for all Stripe integration data
- Add data retention policies for sync batch historical data

## Testing Strategy

### Unit Testing

- Test all new domain entities and validation logic
- Mock Stripe API responses for service layer testing
- Test error handling and retry mechanisms

### Integration Testing

- Test complete webhook processing flow with sample Stripe events
- Test Temporal workflow execution with mock ClickHouse data
- Test API endpoints with proper authentication and validation

### Load Testing

- Test event aggregation performance with 25M+ events dataset
- Test Stripe API rate limit handling under high load
- Test system behavior during Stripe API outages

## Rollout Plan

### Phase 1: Infrastructure (Days 1-2)

- Database schema creation and migrations
- Basic repository and domain layer implementation
- Stripe API client with authentication

### Phase 2: Core Integration (Days 3-4)

- Webhook handler implementation and testing
- Temporal workflow development and testing
- Service layer orchestration and error handling

### Phase 3: Production Readiness (Day 5)

- Configuration management and security implementation
- Monitoring, alerting, and migration utilities
- End-to-end testing and documentation

## Monitoring and Alerting

### Key Metrics

- **Sync Success Rate** - Percentage of successful sync batches per hour
- **API Latency** - Average response time for Stripe API calls
- **Error Rate** - Rate of sync failures by error type
- **Event Processing Lag** - Time between event ingestion and Stripe sync

### Alerts

- **Sync Failure Alert** - Trigger when sync success rate drops below 95%
- **API Error Alert** - Trigger on authentication or configuration errors
- **High Volume Alert** - Trigger when event volume exceeds capacity thresholds
- **Webhook Failure Alert** - Trigger on webhook processing failures

## Success Criteria

### Functional Requirements

- **Customer Sync**: 100% success rate for customer.created webhook processing
- **Event Sync**: Successful aggregation and sync of billable events within 65 minutes (hour + grace period)
- **Error Handling**: All failures properly logged with retry mechanisms working correctly

### Performance Requirements

- **Webhook Response Time**: < 5 seconds for customer.created webhook processing
- **Batch Processing**: Complete hourly sync within 15 minutes for normal volumes
- **API Efficiency**: < 100 Stripe API calls per hour per tenant under normal operation

### Operational Requirements

- **Zero Downtime**: Integration deployment without affecting core FlexPrice functionality
- **Monitoring Coverage**: 100% of critical operations covered by monitoring and alerting
- **Documentation**: Complete API documentation and operational runbooks
