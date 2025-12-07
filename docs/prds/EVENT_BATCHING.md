# Event Batching Implementation

## Overview

This document describes the event batching implementation for the Event Consumption Service. Instead of writing events to ClickHouse one at a time, events are now batched and written in bulk to improve performance and reduce database load.

## Key Features

1. **Configurable Batch Size**: Set the number of events to accumulate before writing to ClickHouse
2. **Time-Based Flushing**: Automatically flush events after a configured interval to prevent unbounded latency
3. **Thread-Safe**: Uses mutex-based synchronization to handle concurrent message processing
4. **Graceful Shutdown**: Ensures all remaining events are flushed before service shutdown
5. **Background Flusher**: A dedicated goroutine periodically flushes batched events

## Configuration

Two new configuration parameters have been added to `EventProcessingConfig`:

### config.yaml

```yaml
event_processing:
  topic: "events"
  rate_limit: 12
  consumer_group: "flexprice-consumer-local"
  batch_size: 200              # Number of events to batch before writing to ClickHouse
  batch_flush_seconds: 5       # Max seconds to wait before flushing batch
```

### Environment Variables

You can also configure these via environment variables:

```bash
FLEXPRICE_EVENT_PROCESSING_BATCH_SIZE=200
FLEXPRICE_EVENT_PROCESSING_BATCH_FLUSH_SECONDS=5
```

## How It Works

### 1. Initialization

When the `EventConsumptionService` is created:
- A batch buffer is initialized with capacity equal to `batch_size`
- A background flusher goroutine is started
- A ticker is set up to flush batches every `batch_flush_seconds`

### 2. Event Processing

When an event is received from Kafka:
1. The event is unmarshalled and validated
2. Instead of writing immediately to ClickHouse, it's added to the batch buffer
3. If the buffer reaches `batch_size`, it's flushed immediately
4. The event continues to post-processing as before

### 3. Batch Flushing

Batches are flushed in two scenarios:
1. **Size-based**: When the buffer reaches `batch_size` events
2. **Time-based**: Every `batch_flush_seconds` seconds via the background flusher

### 4. Graceful Shutdown

When the service is shutting down:
1. The background flusher goroutine is stopped
2. Any remaining events in the buffer are flushed to ClickHouse
3. The service waits for all operations to complete

## Architecture Changes

### Modified Files

1. **internal/config/config.go**
   - Added `BatchSize` and `BatchFlushSeconds` to `EventProcessingConfig`

2. **internal/service/event_consumption.go**
   - Added batching fields to `eventConsumptionService` struct
   - Modified `processMessage()` to add events to batch instead of writing immediately
   - Added `addToBatch()` method for thread-safe batch accumulation
   - Added `flushBatch()` and `flushBatchUnlocked()` for batch writing
   - Added `startBatchFlusher()` to start the background flusher goroutine
   - Added `Shutdown()` method for graceful shutdown
   - Updated interface to include `Shutdown()` method

3. **cmd/server/main.go**
   - Modified `startRouter()` to accept `eventConsumptionSvc` parameter
   - Added `Shutdown()` call in router's `OnStop` hook
   - Updated all `startRouter()` invocations to pass the service

4. **internal/config/config.yaml**
   - Added batch configuration with default values

## Performance Considerations

### Benefits

1. **Reduced Database Load**: Fewer write operations to ClickHouse
2. **Better Throughput**: Bulk inserts are more efficient than individual inserts
3. **Network Efficiency**: Fewer round trips to the database
4. **Resource Optimization**: More efficient use of database connections

### Trade-offs

1. **Memory Usage**: Events are held in memory until flushed
2. **Latency**: Events may wait up to `batch_flush_seconds` before being written
3. **Error Handling**: If a batch fails, all events in the batch need to be retried

### Tuning Recommendations

For **high-throughput** scenarios:
```yaml
batch_size: 500
batch_flush_seconds: 10
```

For **low-latency** scenarios:
```yaml
batch_size: 100
batch_flush_seconds: 2
```

For **balanced** performance (default):
```yaml
batch_size: 200
batch_flush_seconds: 5
```

## Monitoring

The implementation includes comprehensive logging:

### Startup
```
initialized event consumption service with batching batch_size=200 flush_interval=5s
```

### Batch Flushes
```
flushing batch to ClickHouse batch_size=200
successfully flushed batch to ClickHouse batch_size=200 duration_ms=42
```

### Errors
```
failed to flush batch to ClickHouse error=... batch_size=200
failed to add events to batch error=... event_id=... event_name=...
```

### Shutdown
```
shutting down event consumption service
stopping batch flusher
event consumption service shutdown complete
```

## Error Handling

### Batch Write Failures

If a batch write fails:
1. The error is logged with the batch size
2. Sentry is notified of the exception
3. The batch remains in the buffer (not cleared)
4. Watermill's retry mechanism will re-process the messages

### Individual Event Failures

If an event fails to unmarshal or is invalid:
1. The error is handled per-event (no impact on other events)
2. Watermill's poison queue middleware handles DLQ routing
3. The batch continues to accumulate valid events

## Thread Safety

The implementation uses a mutex (`batchMu`) to ensure thread-safe access to the batch buffer:
- `addToBatch()`: Locks before appending events
- `flushBatch()`: Locks before flushing
- `flushBatchUnlocked()`: Assumes caller holds the lock (for internal use)

This allows Watermill to process multiple messages concurrently while safely accumulating them in the batch.

## Testing Recommendations

### Unit Tests
1. Test batch accumulation with various sizes
2. Test time-based flushing
3. Test size-based flushing
4. Test graceful shutdown with pending events
5. Test concurrent event additions

### Integration Tests
1. Send bursts of events and verify batching behavior
2. Test with different `batch_size` and `batch_flush_seconds` values
3. Monitor ClickHouse write patterns
4. Test graceful shutdown under load

### Load Tests
1. Measure throughput improvement vs. individual writes
2. Monitor memory usage with different batch sizes
3. Measure latency distribution (p50, p95, p99)
4. Test with realistic event rates

## Migration Guide

### For Production Deployment

1. **Review Configuration**: Adjust `batch_size` and `batch_flush_seconds` based on your traffic patterns

2. **Monitor Metrics**: Watch for:
   - Event processing latency
   - ClickHouse write performance
   - Memory usage
   - Batch flush frequency

3. **Gradual Rollout**: Consider deploying to a canary environment first

4. **Rollback Plan**: The old behavior can be simulated by setting:
   ```yaml
   batch_size: 1
   batch_flush_seconds: 0
   ```

## Troubleshooting

### High Memory Usage
- Reduce `batch_size`
- Reduce `batch_flush_seconds`
- Check for slow ClickHouse writes causing buffer buildup

### High Latency
- Reduce `batch_flush_seconds`
- Reduce `batch_size` if batches are taking too long to fill

### ClickHouse Write Errors
- Check ClickHouse connection health
- Review ClickHouse logs for capacity issues
- Consider increasing ClickHouse resources

### Events Not Being Written
- Check logs for batch flush errors
- Verify ClickHouse connectivity
- Check that the service is running in Consumer or Local mode

## Future Enhancements

Potential improvements for future iterations:

1. **Per-Tenant Batching**: Separate batches per tenant for isolation
2. **Dynamic Batch Sizing**: Adjust batch size based on event rate
3. **Metrics Export**: Expose batch metrics via Prometheus
4. **Compression**: Compress batches before sending to ClickHouse
5. **Partial Flush on Error**: Implement more sophisticated error handling
6. **Circuit Breaker**: Add circuit breaker pattern for ClickHouse writes

## References

- [Watermill Documentation](https://watermill.io/)
- [ClickHouse Bulk Inserts](https://clickhouse.com/docs/en/optimize/bulk-inserts)
- [Go sync.Mutex](https://pkg.go.dev/sync#Mutex)

