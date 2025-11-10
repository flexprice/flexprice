# Kafka Batch Consumption Implementation Guide

## Overview

This guide implements **Approach 1** from the batch consumption research—manual batch accumulation with time-based and size-based flushing. Your current Watermill-based consumer processes one message at a time. This implementation converts it to batch processing while maintaining full Watermill compatibility.

## Architecture Changes

### Before (Single Event Processing)
```
API Event → Kafka Topic → Watermill Consumer (1 event) 
  → Parse JSON → ClickHouse INSERT (1 event) → Post-processing
```

### After (Batch Processing)
```
API Event → Kafka Topic → Watermill Consumer (receives 1 event)
  → Add to Batcher (accumulate 250 events or 5s)
  → Batch Flush (background goroutine)
  → ClickHouse BULK INSERT (250 events at once)
  → Post-processing (all 250 events)
```

## Implementation Details

### 1. New Structs

#### BatchConfig
Configurable parameters for batch processing:
```go
type BatchConfig struct {
    BatchSize     int           // 250 messages (tune based on event size)
    FlushInterval time.Duration // 5 seconds (prevents stale data)
    MaxRetries    int           // 3 attempts with exponential backoff
}
```

#### MessageBatcher
Core batching engine with:
- **Thread-safe message accumulation** (sync.Mutex)
- **Background flush worker** (goroutine + ticker)
- **Dual flush triggers**:
  - Size-based: when `len(messages) >= BatchSize`
  - Time-based: every `FlushInterval`
- **Retry logic**: exponential backoff (100ms → 200ms → 400ms)

#### BatchMessage
Wrapper struct to keep message and parsed event together:
```go
type BatchMessage struct {
    Message *message.Message  // Original Watermill message
    Event   *events.Event      // Parsed event
}
```

### 2. Key Changes to EventConsumptionService

**Old handler**: `processMessage()` → processes 1 event immediately
```go
func (s *eventConsumptionService) processMessage(msg *message.Message) error {
    // Parse event
    // Insert into ClickHouse (1 insert)
    // Publish to post-processing
    // Return immediately
}
```

**New handler**: `processMessageWithBatching()` → adds to batch and returns
```go
func (s *eventConsumptionService) processMessageWithBatching(msg *message.Message) error {
    // Parse event
    batchMsg := &BatchMessage{Message: msg, Event: &event}
    s.messageBatcher.Append(ctx, batchMsg) // Add to batch
    return nil                               // Return immediately!
}
```

**Batch processor**: `processBatch()` → processes all events at once
```go
func (s *eventConsumptionService) processBatch(ctx context.Context, 
    batchMessages []*BatchMessage) error {
    // Collect all 250 events + billing events
    // 1 ClickHouse BULK INSERT for all
    // Post-process all events
}
```

### 3. MessageBatcher Lifecycle

#### Initialization (in NewEventConsumptionService)
```go
// Create batcher with config + process function
mb := NewMessageBatcher(
    BatchConfig{
        BatchSize:     250,
        FlushInterval: 5 * time.Second,
        MaxRetries:    3,
    },
    s.processBatch,  // Function to call on flush
    s.Logger,
    "event_consumption",
)

// Start background worker goroutine
go mb.Start()
```

#### Message Accumulation
```go
// In processMessageWithBatching:
batchMsg := &BatchMessage{Message: msg, Event: &event}
err := s.messageBatcher.Append(ctx, batchMsg)
// Returns immediately, batch accumulates in background
```

#### Flush Trigger Points

1. **Size-based** (eager flush):
   ```go
   if len(mb.messages) >= mb.config.BatchSize {  // 250 messages
       select {
       case mb.flushChan <- struct{}{}:  // Signal flush
       }
   }
   ```

2. **Time-based** (scheduled flush):
   ```go
   case <-ticker.C:  // Every 5 seconds
       if len(mb.messages) > 0 {
           flush(messages)  // Flush whatever we have
       }
   ```

3. **Shutdown** (graceful flush):
   ```go
   case <-mb.stopChan:
       // Flush remaining messages before stopping
       flush(mb.messages)
   ```

#### Flush with Retry
```go
func (mb *MessageBatcher) flush(ctx, messages) error {
    for attempt := 0; attempt < 3; attempt++ {
        err := mb.processFn(ctx, messages)
        if err == nil {
            return nil  // Success
        }
        
        if attempt < 2 {
            backoff := time.Duration(100 * (1 << uint(attempt))) * time.Millisecond
            time.Sleep(backoff)  // 100ms, 200ms
        }
    }
    return lastErr  // Failed after 3 retries
}
```

## Integration with Your Codebase

### Step 1: Copy the Implementation

Replace your `eventConsumptionService` struct definition and methods with the provided implementation. Key replacements:

- Add `batchConfig` and `messageBatcher` fields
- Add `NewMessageBatcher()`, `MessageBatcher.Start()`, `MessageBatcher.Append()`, etc.
- Rename old `processMessage()` → `processMessageWithBatching()`
- Add new `processBatch()` method
- Keep `ProcessRawEvent()` unchanged (for Lambda)

### Step 2: Update Router Registration

Change your router handler registration to use the new method:

```go
// OLD:
router.AddNoPublishHandler(
    "event_consumption_handler",
    cfg.EventProcessing.Topic,
    s.pubSub,
    s.processMessage,      // ❌ OLD
    throttle.Middleware,
)

// NEW:
router.AddNoPublishHandler(
    "event_consumption_handler",
    cfg.EventProcessing.Topic,
    s.pubSub,
    s.processMessageWithBatching,  // ✅ NEW
    throttle.Middleware,
)
```

### Step 3: Configure Batch Parameters

In `NewEventConsumptionService()`:

```go
ev.batchConfig = BatchConfig{
    BatchSize:     250,              // Tune based on event size
    FlushInterval: 5 * time.Second,  // Tune based on latency SLA
    MaxRetries:    3,
}
```

**Tuning Guide**:
- **BatchSize**: 
  - Small events (< 1KB): 500-1000
  - Medium events (1-5KB): 250-500
  - Large events (> 5KB): 100-250
  - Test: batch size × avg event size should be < 50MB (ClickHouse limits)

- **FlushInterval**:
  - Real-time: 1-2 seconds
  - Near real-time: 5 seconds
  - Batch processing: 10+ seconds

### Step 4: ClickHouse Configuration

Ensure your `eventRepo.BulkInsertEvents()` efficiently handles bulk inserts. If it doesn't, optimize:

```go
// In eventRepo.BulkInsertEvents():
func (r *EventRepository) BulkInsertEvents(ctx context.Context, 
    events []*events.Event) error {
    
    // Use PrepareBatch for efficiency
    batch, err := r.conn.PrepareBatch(ctx, "INSERT INTO events")
    if err != nil {
        return err
    }
    
    // Append all events at once
    for _, event := range events {
        err := batch.Append(
            event.Timestamp,
            event.ID,
            event.TenantID,
            event.EventName,
            event.Payload,
            // ... other columns
        )
        if err != nil {
            return err
        }
    }
    
    // Single flush to ClickHouse
    return batch.Send()
}
```

## Testing the Implementation

### Local Testing

1. **Single Event** (verify backward compatibility):
   ```bash
   Send 1 event → Should be flushed within 5 seconds (time-based)
   ```

2. **Batch of 250** (verify size-based flush):
   ```bash
   Send 250 events rapidly → Should flush immediately
   Check logs: "size-based batch flush"
   ```

3. **Mixed Load** (stress test):
   ```bash
   Send 500 events over 10 seconds
   - First 250 should flush immediately (size-based)
   - Remaining 250 should flush at 5s mark (time-based)
   ```

4. **Restart/Shutdown** (graceful handling):
   ```bash
   Kill consumer mid-processing
   Check logs: "flushing remaining messages on shutdown"
   Verify no message loss
   ```

### Monitoring Metrics to Add

```go
// Add to logs for monitoring:
- "batch_size": len(messages)
- "flush_type": "size-based" | "time-based" | "shutdown"
- "flush_interval_ms": elapsed time
- "total_events_inserted": (batch_size * [1 or 2 for billing])
- "post_process_errors": count
- "retry_attempt": attempt number
```

## Performance Expectations

### Before (Single Event)
- 1 ClickHouse INSERT per message
- Latency: ~100-200ms per message
- At 100 RPS: ~100-200 INSERTs/sec (very heavy load)

### After (Batch of 250)
- 1 ClickHouse INSERT per ~2.5 seconds
- Latency: ~100-200ms per batch (same as before, but for 250 events!)
- At 100 RPS: ~2.5 batches/sec = ~1 INSERT/sec (10x reduction!)
- Throughput per message: ~5-50x faster

**Example**: If each single INSERT takes 100ms and adds latency of 100ms:
- Single: 250 messages × 100ms = 25 seconds (queue backs up)
- Batch: 250 messages in 1 batch × 100ms = 0.1 seconds (queue drains quickly)

## Troubleshooting

### Issue: Messages Not Being Flushed

**Check**: Verify batcher was started
```go
// In NewEventConsumptionService:
go ev.messageBatcher.Start()  // ✅ Must have this
```

**Check**: Verify batcher is receiving messages
```go
// Logs should show:
// "batching event from message queue"
// "adding event to batch"
```

### Issue: High Memory Usage

**Cause**: Batch size too large or flush failing
**Fix**:
- Reduce `BatchSize` (default 250)
- Reduce `FlushInterval` (default 5s)
- Check ClickHouse connectivity

### Issue: Stale Data Older Than 5 Seconds

**Normal**: Time-based flush runs every 5 seconds
**If worse**: Check if `processBatch()` is blocking/slow
```go
s.Logger.Debugw("batch processing completed",
    "batch_size", len(batchMessages),
    "duration_ms", time.Since(startTime).Milliseconds(),
)
```

### Issue: Message Duplication

**Cause**: Multiple retries writing same batch
**Fix**: Ensure ClickHouse `INSERT` is idempotent (use ReplacingMergeTree or dedup on `event.ID`)

## Configuration Examples

### Conservative (Low Latency)
```go
BatchConfig{
    BatchSize:     50,               // Flush often
    FlushInterval: 1 * time.Second,  // Max 1s stale data
    MaxRetries:    3,
}
```

### Balanced (Default)
```go
BatchConfig{
    BatchSize:     250,
    FlushInterval: 5 * time.Second,
    MaxRetries:    3,
}
```

### Aggressive (High Throughput)
```go
BatchConfig{
    BatchSize:     1000,
    FlushInterval: 10 * time.Second,
    MaxRetries:    5,
}
```

## Migration Path

1. **Deploy**: Copy new code
2. **Monitor**: Watch for batch flush logs and error rates
3. **Tune**: Adjust `BatchSize` and `FlushInterval` based on metrics
4. **Validate**: Verify event latency and ClickHouse load

## Rollback Plan

If issues arise:
1. Set `BatchSize: 1` and `FlushInterval: 100ms`
2. This becomes equivalent to single-event processing
3. No code changes needed, just config change
4. Then troubleshoot and re-tune

## Future Optimizations

1. **Multiple Batchers**: Use separate batcher per partition for parallelism
2. **Adaptive Batching**: Auto-adjust batch size based on message arrival rate
3. **Dead Letter Queue**: Separate handling for permanently failed batches
4. **Metrics Export**: Prometheus metrics for batch processing latency/size
5. **Post-Processing Batch**: Batch the event publishing too (currently sequential)
