# Raw Events Reprocessing - Implementation Summary

## Overview
Complete implementation of raw events reprocessing feature that reads from ClickHouse `raw_events` table, transforms using Bento logic, and publishes to Kafka `prod_events_v4` topic.

## Table Schema Optimizations

### ClickHouse Table Structure
```sql
CREATE TABLE flexprice.raw_events (
    PRIMARY KEY (tenant_id, environment_id, external_customer_id, timestamp)
    ORDER BY (tenant_id, environment_id, external_customer_id, timestamp, event_name, id)
    PARTITION BY toYYYYMMDD(timestamp)
    ENGINE = ReplacingMergeTree(version)
)
```

### Query Optimizations Applied

1. **Filter Order**: Follows PRIMARY KEY order for optimal index usage
   - tenant_id, environment_id (always filtered)
   - external_customer_id (optional)
   - timestamp range (optional)
   - event_name (optional)

2. **Keyset Pagination**: Uses tuple comparison matching ORDER BY
   ```sql
   AND (timestamp, event_name, id) < (?, ?, ?)
   ORDER BY timestamp DESC, event_name DESC, id DESC
   ```

3. **Partition Pruning**: Time range filters leverage daily partitioning

## Comprehensive Logging

### Event Tracking Categories

1. **Successfully Published Events**
   - Count: `total_events_published`
   - Logged at: INFO level per event

2. **Dropped Events (Failed Validation)**
   - Count: `total_events_dropped`
   - Reason: Failed Bento validation (missing required fields)
   - Logged with: raw_event_id, external_customer_id, event_name, timestamp, batch position
   - Example reasons:
     - Missing orgId
     - Missing methodName
     - Missing providerName/serviceName
     - Missing data.modelName
     - Missing id or createdAt

3. **Transformation Errors**
   - Count: `total_transformation_errors`
   - Reason: JSON parsing errors, data type issues
   - Logged with: raw_event_id, error details, batch position

4. **Publish Failures**
   - Count: `total_events_failed`
   - Reason: Kafka publish errors
   - Logged with: both raw_event_id and transformed_event_id, error details

### Log Levels

- **INFO**: Batch summaries, dropped events (validation failures), workflow completion
- **WARN**: Transformation errors (parsing/processing issues)
- **ERROR**: Kafka publish failures
- **DEBUG**: Individual event publishing, query execution

### Batch Summary Logs

Each batch logs:
```
batch_size: Number of raw events in batch
batch_published: Successfully published
batch_dropped: Failed validation
batch_transform_errors: Transformation errors
batch_publish_failed: Kafka publish failures
```

### Final Summary Logs

Workflow completion includes:
```
total_events_found: Total raw events retrieved
total_events_published: Successfully published
total_events_dropped: Failed validation
total_transformation_errors: Transformation errors
total_events_failed: Kafka publish failures
success_rate_percent: (published / found) * 100
```

## Tracing Dropped Events

### Query Examples

**Find all dropped events:**
```
grep "validation failed - event dropped" logs.txt
```

**Find transformation errors:**
```
grep "transformation error - event skipped" logs.txt
```

**Find publish failures:**
```
grep "failed to publish transformed event" logs.txt
```

**Get batch summary:**
```
grep "batch processing complete" logs.txt
```

### Log Fields for Debugging

Every dropped/failed event includes:
- `raw_event_id`: Original event ID from raw_events table
- `external_customer_id`: Customer identifier
- `event_name`: Event name from raw_events
- `timestamp`: Event timestamp
- `batch`: Batch number (0-indexed)
- `batch_position`: Position within batch (1-indexed)
- `reason` or `error`: Why it was dropped/failed

## Performance Characteristics

### Scale
- **Billions of events**: Keyset pagination avoids OFFSET performance issues
- **Batch size**: Default 1000, configurable (100-5000 recommended)
- **Timeout**: 1 hour workflow, 55 minutes activity
- **Retries**: 3 attempts with exponential backoff

### Memory Efficiency
- Processes one batch at a time
- Individual event transformation (not batch array)
- Streaming approach - no full dataset in memory

### Index Usage
- Primary key filters (tenant_id, environment_id) always used
- Partition pruning on timestamp range
- Keyset pagination leverages ORDER BY index

## API Usage

### Endpoint
```
POST /events/raw/reprocess
```

### Request Body
```json
{
  "external_customer_id": "customer123",  // optional
  "event_name": "api_request",            // optional
  "start_date": "2024-01-01T00:00:00Z",   // required (RFC3339)
  "end_date": "2024-01-31T23:59:59Z",     // optional (RFC3339) - defaults to current time if not provided
  "batch_size": 1000                       // optional, default 1000
}
```

### Response
```json
{
  "workflow_id": "ReprocessRawEventsWorkflow-customer123-...",
  "run_id": "...",
  "task_queue": "events"
}
```

### Workflow Result
```json
{
  "total_events_found": 10000,
  "total_events_published": 9500,
  "total_events_dropped": 300,
  "total_transformation_errors": 150,
  "total_events_failed": 50,
  "processed_batches": 10,
  "completed_at": "2024-01-31T12:00:00Z"
}
```

## Configuration

### config.yaml
```yaml
raw_events_reprocessing:
  enabled: true
  output_topic: "prod_events_v4"
```

### Environment Variables
Can override via:
- `FLEXPRICE_RAW_EVENTS_REPROCESSING_ENABLED`
- `FLEXPRICE_RAW_EVENTS_REPROCESSING_OUTPUT_TOPIC`

## Monitoring & Observability

### Key Metrics to Track

1. **Success Rate**: `total_events_published / total_events_found`
2. **Drop Rate**: `total_events_dropped / total_events_found`
3. **Error Rate**: `(total_transformation_errors + total_events_failed) / total_events_found`
4. **Throughput**: `total_events_found / workflow_duration`

### Alerts to Configure

- Success rate < 90%
- Transformation error rate > 5%
- Workflow timeout (> 1 hour)
- Kafka publish failure rate > 1%

## Troubleshooting

### High Drop Rate

1. Check validation logs for common patterns:
   ```
   grep "validation failed" | grep -o "reason.*" | sort | uniq -c
   ```

2. Verify raw_events payload structure matches Bento input format

3. Check for missing required fields in source data

### Transformation Errors

1. Review error messages:
   ```
   grep "transformation error" | grep -o "error.*"
   ```

2. Common causes:
   - Invalid JSON in payload column
   - Unexpected data types
   - Malformed timestamps

### Publish Failures

1. Check Kafka connectivity and topic configuration
2. Verify partition key format
3. Check for message size limits (payload too large)

## Files Modified/Created

### Domain Layer
- `internal/domain/events/model.go` - Added RawEvent, FindRawEventsParams, ReprocessRawEventsParams
- `internal/domain/events/repository.go` - Added RawEventRepository interface

### Repository Layer
- `internal/repository/clickhouse/raw_event.go` - NEW: RawEventRepository implementation
- `internal/repository/factory.go` - Added NewRawEventRepository

### Transformation Layer
- `internal/domain/events/transform/transformer.go` - NEW: Complete Bento mapping logic

### Service Layer
- `internal/service/raw_events_reprocessing.go` - NEW: RawEventsReprocessingService
- `internal/service/factory.go` - Added RawEventRepo field

### Temporal Layer
- `internal/temporal/workflows/events/reprocess_raw_events_workflow.go` - NEW
- `internal/temporal/activities/events/reprocess_raw_events_activity.go` - NEW
- `internal/temporal/models/events/reprocess_raw_events.go` - NEW
- `internal/temporal/registration.go` - Registered workflow and activity
- `internal/temporal/service/service.go` - Added buildReprocessRawEventsInput
- `internal/types/temporal.go` - Added TemporalReprocessRawEventsWorkflow

### API Layer
- `internal/api/dto/events.go` - Added ReprocessRawEventsRequest
- `internal/api/v1/events.go` - Added ReprocessRawEvents handler
- `internal/api/router.go` - Added POST /events/raw/reprocess route

### Configuration
- `internal/config/config.go` - Added RawEventsReprocessingConfig
- `internal/config/config.yaml` - Added raw_events_reprocessing section

## Testing Recommendations

### Unit Tests
- Bento transformer with various payload formats
- Validation logic (valid/invalid inputs)
- Keyset pagination logic

### Integration Tests
- End-to-end reprocessing with test data
- Kafka message format validation
- Temporal workflow execution

### Load Tests
- Large batch sizes (5000+)
- Long time ranges (months of data)
- Concurrent workflow executions

## Future Enhancements

1. **Metrics Export**: Prometheus metrics for monitoring
2. **Dead Letter Queue**: Store failed events for manual review
3. **Sampling**: Option to reprocess a percentage of events
4. **Parallel Processing**: Multiple workers for different time ranges
5. **Resume Capability**: Checkpoint and resume from last processed event
