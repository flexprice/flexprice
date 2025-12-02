# Event Batching Flow Diagram

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Kafka Topic (events)                       │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                                   │ Messages
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Watermill Consumer (Sarama)                      │
│                    (Multiple workers processing                      │
│                     messages concurrently)                           │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                    ┌──────────────┼──────────────┐
                    │              │              │
                    ▼              ▼              ▼
            ┌───────────┐  ┌───────────┐  ┌───────────┐
            │ Worker 1  │  │ Worker 2  │  │ Worker N  │
            │processMsg │  │processMsg │  │processMsg │
            └───────────┘  └───────────┘  └───────────┘
                    │              │              │
                    └──────────────┼──────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                   EventConsumptionService                            │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    Batch Buffer (Mutex Protected)            │   │
│  │  ┌──────┐ ┌──────┐ ┌──────┐         ┌──────┐               │   │
│  │  │Event1│ │Event2│ │Event3│   ...   │EventN│               │   │
│  │  └──────┘ └──────┘ └──────┘         └──────┘               │   │
│  │                                                               │   │
│  │  Capacity: batch_size (default 200)                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │              Background Flusher Goroutine                    │   │
│  │  ┌──────────────────────────────────────────────┐           │   │
│  │  │  Ticker: Every batch_flush_seconds (5s)      │           │   │
│  │  │                                               │           │   │
│  │  │  On Timer: Flush buffered events             │           │   │
│  │  └──────────────────────────────────────────────┘           │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                       │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                    ┌──────────────┼──────────────┐
                    │ Flush       │ Flush         │
                    │ (Size)      │ (Time)        │
                    ▼             ▼               │
┌─────────────────────────────────────────────────────────────────────┐
│                          ClickHouse                                  │
│                   (Bulk Insert of up to 200 events)                  │
└─────────────────────────────────────────────────────────────────────┘
```

## Detailed Flow

### 1. Event Arrival and Processing

```
Event from Kafka
      │
      ▼
┌─────────────────────────────────┐
│   processMessage()               │
│                                  │
│  1. Unmarshal event             │
│  2. Validate event              │
│  3. Create billing event (if    │
│     configured)                 │
└─────────────────────────────────┘
      │
      ▼
┌─────────────────────────────────┐
│   addToBatch()                   │
│                                  │
│  1. Lock mutex                  │
│  2. Append to buffer            │
│  3. Check buffer size           │
│                                  │
│  If size >= batch_size:         │
│     └─> flushBatchUnlocked()    │
│                                  │
│  4. Unlock mutex                │
└─────────────────────────────────┘
      │
      ▼
Continue to post-processing
(if configured)
```

### 2. Batch Flushing - Two Triggers

#### A. Size-Based Trigger

```
Events accumulate in buffer
      │
      ▼
Buffer size reaches batch_size (200)
      │
      ▼
┌─────────────────────────────────┐
│   flushBatchUnlocked()           │
│   (called from addToBatch)       │
│                                  │
│  1. Insert events via            │
│     eventRepo.BulkInsertEvents() │
│  2. Log success metrics          │
│  3. Clear buffer                 │
└─────────────────────────────────┘
```

#### B. Time-Based Trigger

```
Ticker fires every batch_flush_seconds
      │
      ▼
┌─────────────────────────────────┐
│   Background Flusher Goroutine   │
│                                  │
│  select {                        │
│    case <-ticker.C:              │
│      └─> flushBatch()            │
│    case <-stopCh:                │
│      └─> exit                    │
│  }                               │
└─────────────────────────────────┘
      │
      ▼
┌─────────────────────────────────┐
│   flushBatch()                   │
│                                  │
│  1. Lock mutex                  │
│  2. flushBatchUnlocked()        │
│  3. Unlock mutex                │
└─────────────────────────────────┘
```

### 3. Graceful Shutdown

```
Shutdown signal received
      │
      ▼
┌─────────────────────────────────┐
│   Shutdown()                     │
│                                  │
│  1. Close stopCh                │
│  2. Wait for flusher goroutine  │
│  3. Flush remaining events      │
└─────────────────────────────────┘
      │
      ▼
┌─────────────────────────────────┐
│   flushBatch()                   │
│                                  │
│  Write any remaining events to  │
│  ClickHouse before exit          │
└─────────────────────────────────┘
```

## Thread Safety

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Multiple Goroutines                               │
│                                                                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │  Worker 1    │  │  Worker 2    │  │  Worker N    │              │
│  │              │  │              │  │              │              │
│  │ addToBatch() │  │ addToBatch() │  │ addToBatch() │              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         │                 │                 │                       │
│         └─────────────────┼─────────────────┘                       │
│                           │                                          │
│                           ▼                                          │
│               ┌───────────────────────┐                             │
│               │   Mutex Protection    │                             │
│               │                       │                             │
│               │  Only one goroutine   │                             │
│               │  can modify buffer    │                             │
│               │  at a time            │                             │
│               └───────────────────────┘                             │
│                           │                                          │
│                           ▼                                          │
│               ┌───────────────────────┐                             │
│               │    Batch Buffer       │                             │
│               │  [Event1, Event2, ...]│                             │
│               └───────────────────────┘                             │
│                                                                       │
│  ┌──────────────────────────────────────────────────┐               │
│  │         Background Flusher                       │               │
│  │                                                   │               │
│  │  Timer-based: flushBatch()                       │               │
│  │    └─> Also uses mutex protection                │               │
│  └──────────────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────┘
```

## Performance Comparison

### Before Batching (1 event at a time)

```
Event 1 → Process → Write to ClickHouse (10ms) → ACK
Event 2 → Process → Write to ClickHouse (10ms) → ACK
Event 3 → Process → Write to ClickHouse (10ms) → ACK
...
Event 200 → Process → Write to ClickHouse (10ms) → ACK

Total time: 200 events × 10ms = 2000ms (2 seconds)
Database writes: 200
Network round trips: 200
```

### After Batching (200 events at a time)

```
Events 1-200 → Process → Accumulate in buffer
             → Batch Write to ClickHouse (50ms) → ACK all

Total time: ~50ms (plus processing time)
Database writes: 1
Network round trips: 1

Throughput improvement: ~40x
```

## Error Scenarios

### Scenario 1: Batch Write Fails

```
┌─────────────────────────────────┐
│   Batch of 200 events ready     │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   Write to ClickHouse           │
│   └─> Error (connection lost)   │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   1. Log error + batch size     │
│   2. Notify Sentry              │
│   3. Return error               │
│   4. Buffer NOT cleared         │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   Watermill retries messages    │
│   (based on retry config)       │
└─────────────────────────────────┘
```

### Scenario 2: Individual Event Invalid

```
┌─────────────────────────────────┐
│   Event arrives                 │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   processMessage()              │
│   └─> Unmarshal error           │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   1. Log error                  │
│   2. Check if retriable         │
│   3. Return error               │
│   4. Not added to batch         │
└─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────┐
│   Watermill routes to DLQ       │
│   (via poison queue middleware) │
└─────────────────────────────────┘
```

## Configuration Impact

### High Throughput Configuration
```yaml
batch_size: 500
batch_flush_seconds: 10
```
- Fewer database writes
- Higher memory usage
- Increased latency (up to 10s)
- Better for high-volume scenarios

### Low Latency Configuration
```yaml
batch_size: 100
batch_flush_seconds: 2
```
- More database writes
- Lower memory usage
- Reduced latency (max 2s)
- Better for real-time scenarios

### Balanced Configuration (Default)
```yaml
batch_size: 200
batch_flush_seconds: 5
```
- Good balance of throughput and latency
- Moderate memory usage
- Suitable for most scenarios

## Key Insights

1. **Concurrency**: Multiple Watermill workers can process messages concurrently, but only one can modify the batch buffer at a time (mutex protection).

2. **Dual Trigger**: Batches flush on either size threshold OR time threshold, whichever comes first.

3. **Graceful Degradation**: If ClickHouse is slow, events accumulate in memory. If it fails, Watermill handles retries.

4. **Memory Trade-off**: Holding events in memory improves performance but increases memory footprint.

5. **Latency Bounds**: Maximum event latency is bounded by `batch_flush_seconds`, ensuring events don't wait indefinitely.

6. **Shutdown Safety**: The shutdown sequence ensures no events are lost by flushing all pending events before exit.

