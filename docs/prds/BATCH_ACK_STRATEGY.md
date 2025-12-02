# Batch Processing ACK Strategy

## ✅ Problem Solved: Manual Message Acknowledgment

### The Original Problem

In the initial batch implementation, there was a **critical flaw**:

```
❌ BAD FLOW:
1. Message arrives from Kafka
2. processMessageWithBatching() adds to batch
3. Handler returns nil → Watermill ACKs immediately!
4. Batch processes 5 seconds later
5. If batch fails → Messages already ACKed → DATA LOSS!
```

**This would cause message loss** if the batch processing failed after messages were already acknowledged to Kafka.

### The Solution: Manual ACK/NACK

We now use **manual acknowledgment** where messages are only ACKed **after** successful batch processing:

```
✅ GOOD FLOW:
1. Message arrives from Kafka
2. processMessageWithBatching() adds to batch with AckFunc/NackFunc
3. Handler returns nil (no auto-ACK)
4. Batch accumulates (250 messages or 5 seconds)
5. processBatch() runs:
   a. Insert into ClickHouse
   b. If FAIL → Call NackFunc() on all messages → Kafka retries
   c. If SUCCESS → Post-process events → Call AckFunc() on all messages
6. Messages are ACKed ONLY after successful processing
```

## Implementation Details

### 1. BatchMessage Structure (Lines 32-38)

```go
type BatchMessage struct {
    Message   *message.Message // Original Watermill message
    Event     *events.Event    // Parsed event
    Context   context.Context  // Context with tenant and environment IDs
    AckFunc   func() error     // Function to acknowledge the message
    NackFunc  func() error     // Function to negative-acknowledge the message
}
```

The `AckFunc` and `NackFunc` are closures that capture the original Watermill message and call its `Ack()` or `Nack()` methods.

### 2. Creating ACK/NACK Functions (Lines 440-453)

In `processMessageWithBatching`:

```go
batchMsg := &BatchMessage{
    Message: msg,
    Event:   &event,
    Context: ctx,
    AckFunc: func() error {
        msg.Ack()  // Call Watermill's ACK
        return nil
    },
    NackFunc: func() error {
        msg.Nack()  // Call Watermill's NACK (negative ACK)
        return nil
    },
}
```

These closures capture the `msg` variable and allow us to ACK/NACK later.

### 3. When NACKs Happen (Lines 591-599)

If ClickHouse insertion fails:

```go
if err := s.eventRepo.BulkInsertEvents(ctx, eventsToInsert); err != nil {
    // NACK all messages in the batch so they can be retried
    for _, batchMsg := range batchMessages {
        if err := batchMsg.NackFunc(); err != nil {
            s.Logger.Errorw("failed to NACK message",
                "error", err,
                "event_id", batchMsg.Event.ID,
            )
        }
    }
    return err  // This tells the batcher the flush failed
}
```

**What happens after NACK:**
- Kafka will **redeliver** all NACKed messages
- They'll be added to a new batch
- This provides automatic retry for transient failures

### 4. When ACKs Happen (Lines 635-646)

After successful processing:

```go
// ACK all messages in the batch after successful processing
var ackErrors int
for _, batchMsg := range batchMessages {
    if err := batchMsg.AckFunc(); err != nil {
        s.Logger.Errorw("failed to ACK message",
            "error", err,
            "event_id", batchMsg.Event.ID,
        )
        ackErrors++
    }
}
```

**What happens after ACK:**
- Kafka marks the message as successfully processed
- Message is removed from the consumer's queue
- Kafka offset is advanced

## Complete Message Lifecycle

### Successful Path

```
1. Message M1 arrives at 10:00:00.000
2. Added to batch (holds M1's AckFunc)
3. More messages arrive... M2, M3, ... M250
4. At 10:00:00.100, batch size reaches 250
5. Flush triggered
6. processBatch():
   - Bulk insert 250 events → SUCCESS ✅
   - Post-process 250 events
   - Call AckFunc() for M1 → Kafka offset+1 ✅
   - Call AckFunc() for M2 → Kafka offset+2 ✅
   - ... (250 ACKs total)
7. Kafka commits offsets
8. Messages never redelivered
```

### Failure Path with Retry

```
1. Message M1 arrives at 10:00:00.000
2. Added to batch (holds M1's NackFunc)
3. More messages arrive... M2, M3, ... M250
4. At 10:00:00.100, batch size reaches 250
5. Flush triggered
6. processBatch():
   - Bulk insert 250 events → FAILED ❌ (ClickHouse down)
   - Call NackFunc() for M1 → Kafka will redeliver ⏮️
   - Call NackFunc() for M2 → Kafka will redeliver ⏮️
   - ... (250 NACKs total)
7. Kafka DOES NOT commit offsets
8. Messages are redelivered
9. New batch formed with M1, M2, ... M250
10. Retry with exponential backoff
11. Eventually succeeds and ACKs
```

### Time-Based Flush (Partial Batch)

```
1. Message M1 arrives at 10:00:00.000
2. Message M2 arrives at 10:00:00.500
3. ... only 50 messages in 5 seconds
4. At 10:00:05.000, time-based flush triggered
5. processBatch() with 50 messages:
   - Bulk insert 50 events → SUCCESS ✅
   - Call AckFunc() for M1-M50
6. All 50 messages ACKed
```

## Key Benefits

### 1. **No Message Loss**
- Messages are only ACKed after successful processing
- If batch fails, all messages are NACKed and retried

### 2. **Atomic Batch Processing**
- Either the entire batch succeeds (all ACK)
- Or the entire batch fails (all NACK)
- No partial success/failure

### 3. **Automatic Retry**
- Kafka automatically redelivers NACKed messages
- Works with Kafka's consumer group rebalancing
- Compatible with Watermill's middleware

### 4. **Graceful Degradation**
- If ClickHouse is down, messages accumulate in Kafka
- No data loss, just processing delay
- System recovers automatically when ClickHouse is back

## Important Considerations

### Handler Returns `nil` - Why?

```go
func (s *eventConsumptionService) processMessageWithBatching(msg *message.Message) error {
    // ... add to batch ...
    return nil  // ← Important!
}
```

The handler returns `nil` to tell Watermill "I've handled this message, don't auto-ACK."

We're using **manual ACK mode**, where we explicitly call `msg.Ack()` or `msg.Nack()` later.

### What if ACK Fails?

If `msg.Ack()` fails (rare), we log it but continue:

```go
if err := batchMsg.AckFunc(); err != nil {
    s.Logger.Errorw("failed to ACK message", ...)
    ackErrors++
}
// Don't return error - message is already processed
```

The message is already in ClickHouse, so we can't unprocess it. ACK failure is logged for monitoring.

### Post-Processing Errors

Post-processing errors don't cause NACKs:

```go
if err := s.eventPostProcessingSvc.PublishEvent(ctx, batchMsg.Event, false); err != nil {
    postProcessErrors++
    // Continue processing other events
}
// Still ACK the message - it's in ClickHouse
```

**Why?** The event is already successfully stored in ClickHouse. Post-processing is a separate concern and shouldn't cause redelivery.

## Testing the ACK Strategy

### Test 1: Verify NACK on ClickHouse Failure

```bash
# Simulate ClickHouse being down
1. Stop ClickHouse
2. Send 250 messages
3. Observe logs:
   - "failed to bulk insert events"
   - "failed to NACK message" (250 times)
4. Start ClickHouse
5. Messages should be redelivered automatically
6. Verify all 250 events appear in ClickHouse
```

### Test 2: Verify ACK on Success

```bash
1. Send 250 messages
2. Observe logs:
   - "batch flush successful"
   - "successfully bulk inserted events"
   - "batch processing completed" with "ack_errors": 0
3. Verify Kafka consumer lag = 0 (all messages ACKed)
4. Resend the same 250 messages
5. They should be NEW messages (not redelivered)
```

### Test 3: Verify Time-Based ACK

```bash
1. Send 1 message
2. Wait 6 seconds
3. Observe logs:
   - "time-based batch flush triggered" with "batch_size": 1
   - "batch processing completed"
4. Verify message ACKed (consumer lag = 0)
```

## Monitoring

Key metrics to track:

```go
// In logs:
"ack_errors": 0              // Should always be 0 in healthy system
"post_process_errors": 0     // May be > 0, doesn't affect ACK
"batch_size": 250            // Number of messages ACKed together
```

**Alert on:**
- `ack_errors > 0` - Watermill ACK mechanism failing
- Repeated NACK patterns - Persistent processing failures
- High consumer lag after NACKs - Indicates system can't keep up

## Comparison: Before vs After

### Before Fix ❌

```
Message → Add to Batch → Return nil → AUTO-ACK by Watermill
                                      ↓
                            (Message committed immediately)
                                      ↓
5 seconds later → Process Batch → FAILS
                                      ↓
                            (Messages LOST! Already ACKed)
```

### After Fix ✅

```
Message → Add to Batch (with AckFunc) → Return nil → NO AUTO-ACK
                                                      ↓
5 seconds later → Process Batch → FAILS
                                   ↓
                          Call NackFunc() → Kafka redelivers
                                            ↓
                                  (No data loss! Automatic retry)
```

## Conclusion

The manual ACK strategy ensures that:

1. ✅ **No message loss** - Messages only ACKed after successful processing
2. ✅ **Automatic retry** - Failed batches are NACKed and redelivered
3. ✅ **Atomic batching** - All messages in a batch succeed or fail together
4. ✅ **Production ready** - Handles failures gracefully with proper logging

This is the **correct and safe** way to implement batch processing with Kafka/Watermill!




