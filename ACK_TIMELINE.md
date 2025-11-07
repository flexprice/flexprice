# When Do Messages Get ACKed? - Quick Reference

## Timeline Visualization

```
TIME:        0ms          100ms         200ms         ...         5000ms
             │             │             │            │             │
KAFKA:   [M1]──┐      [M2]──┐      [M3]──┐      [M250]──┐         │
             │   │          │   │          │   │            │   │         │
             ▼   │          ▼   │          ▼   │            ▼   │         │
HANDLER: Parse │      Parse │      Parse │        Parse │         │
             │   │          │   │          │   │            │   │         │
             ▼   │          ▼   │          ▼   │            ▼   │         │
BATCH:    [M1]  │      [M1,M2] │    [M1,M2,M3]│  [M1...M250]│         │
             │   │          │   │          │   │            │   │         │
             │   │          │   │          │   │            │   │         │
RETURN:    nil  │       nil  │       nil  │         nil  │         │
             │   │          │   │          │   │            │   │         │
             ▼   │          ▼   │          ▼   │            ▼   │         │
KAFKA:    ⏳ WAIT     ⏳ WAIT     ⏳ WAIT       ⏳ WAIT     │
             │             │             │            │             │
             └─────────────┴─────────────┴────────────┴─────────────┘
                                                                     │
                                                                     ▼
                                           FLUSH TRIGGERED (batch size = 250)
                                                                     │
                                                                     ▼
                                           processBatch() executes
                                                                     │
                                           ┌─────────────────────────┤
                                           │                         │
                                      SUCCESS ✅                 FAILURE ❌
                                           │                         │
                              ┌────────────┤                         ├──────────┐
                              │            │                         │          │
                  ClickHouse INSERT   Post-Process           NACK All       Return Error
                          │            │                         │
                          │            │                    [M1].Nack()
                          │            │                    [M2].Nack()
                          │            │                      ...
                          │            │                    [M250].Nack()
                          │            │                         │
                          │            ▼                         ▼
                          │      ACK All Messages         Kafka Redelivers
                          │            │                         │
                          │       [M1].Ack()              [M1]──┐
                          │       [M2].Ack()              [M2]──┤
                          │          ...                  [M3]──┤  New Batch!
                          │       [M250].Ack()              ... ─┤
                          │            │                   [M250]┘
                          ▼            ▼                         │
                    Kafka Commits  Consumer                      ▼
                     Offsets      Lag = 0               Retry Processing...
```

## Quick Answer: When Does ACK Happen?

### ✅ ACK happens AFTER successful batch processing

**Location in code:** `internal/service/event_consumption.go` lines 635-646

**Conditions:**
1. ClickHouse bulk insert succeeds
2. Post-processing completes (even if some fail)
3. For each message: `msg.Ack()` is called

**Timeline:**
- Message received: `T`
- Added to batch: `T + 0.1ms` (immediate)
- Batch flushed: `T + 5s` (time-based) OR `T + Xs` (size-based)
- Processing completes: `T + 5s + 100ms` (assuming 100ms to process)
- **ACK happens**: `T + 5s + 100ms` ✅

### ❌ NACK happens AFTER failed batch processing

**Location in code:** `internal/service/event_consumption.go` lines 591-599

**Conditions:**
1. ClickHouse bulk insert fails
2. For each message: `msg.Nack()` is called
3. Messages are redelivered by Kafka

**Timeline:**
- Message received: `T`
- Added to batch: `T + 0.1ms`
- Batch flushed: `T + 5s`
- Processing FAILS: `T + 5s + 100ms`
- **NACK happens**: `T + 5s + 100ms` ❌
- **Redelivered**: `T + 5s + 200ms` (Kafka redelivers)
- **Retry**: Added to new batch, process again

## Handler Return vs Message ACK

⚠️ **IMPORTANT DISTINCTION:**

```go
func processMessageWithBatching(msg *message.Message) error {
    // ... add to batch ...
    return nil  // ← Handler returns immediately
}
```

**Handler returns `nil`** at: `T + 0.1ms` (immediate)
**Message gets ACKed** at: `T + 5s + 100ms` (after batch processing)

**There's a 5+ second gap between handler return and ACK!**

This is **intentional** and **safe** because:
- Handler returns `nil` = "I've handled this, don't auto-ACK"
- We store `msg.Ack()` closure in the batch
- We manually call `msg.Ack()` later after processing

## Why This Matters

### ❌ If we ACKed immediately (old approach):

```
T + 0ms:     Message arrives
T + 0.1ms:   Handler returns nil → AUTO-ACK → ⚠️ Committed to Kafka
T + 5s:      Batch processes
T + 5s:      ClickHouse fails → 💥 Message lost! Already ACKed!
```

### ✅ With manual ACK (current approach):

```
T + 0ms:     Message arrives
T + 0.1ms:   Handler returns nil → NO AUTO-ACK → ⏳ Waiting in Kafka
T + 5s:      Batch processes
T + 5s:      ClickHouse fails → Call Nack() → 🔄 Kafka redelivers
T + 10s:     Retry succeeds → Call Ack() → ✅ Committed!
```

## Common Scenarios

### Scenario 1: Normal Load (100 RPS)

```
Messages arrive at 100/sec
Batch size = 250
Time to fill batch = 2.5 seconds

Every message is ACKed 2.5 seconds after arrival
(Size-based flush triggers before time-based)
```

### Scenario 2: Low Load (10 RPS)

```
Messages arrive at 10/sec
Batch size = 250
Time to fill batch = 25 seconds (would take too long)

Time-based flush at 5 seconds triggers
Every message is ACKed 5 seconds after arrival
(Time-based flush triggers before size-based)
```

### Scenario 3: Burst Load (1000 RPS for 1 second)

```
1000 messages arrive in 1 second

Batch 1 (250 msgs): Filled at T+0.25s, flushed, ACKed at T+0.35s
Batch 2 (250 msgs): Filled at T+0.50s, flushed, ACKed at T+0.60s
Batch 3 (250 msgs): Filled at T+0.75s, flushed, ACKed at T+0.85s
Batch 4 (250 msgs): Filled at T+1.00s, flushed, ACKed at T+1.10s

All 1000 messages ACKed within 1.1 seconds
```

### Scenario 4: ClickHouse Outage

```
T + 0s:      ClickHouse goes down
T + 0-5s:    250 messages arrive, batch fills
T + 5s:      Batch flush triggered
T + 5.1s:    ClickHouse INSERT fails
T + 5.1s:    All 250 messages NACKed
T + 5.2s:    Kafka redelivers all 250 messages
T + 5.2-10s: New batch fills with same messages
T + 10s:     Another flush (will fail if CH still down)
... retries continue ...
T + 60s:     ClickHouse comes back online
T + 60.1s:   Next flush succeeds!
T + 60.2s:   All messages ACKed ✅

Result: No message loss, just processing delay
```

## Summary

| Event                     | Timing          | Code Location       |
|---------------------------|-----------------|---------------------|
| Message arrives           | T + 0ms         | Kafka              |
| Handler adds to batch     | T + 0.1ms       | Line 456           |
| Handler returns nil       | T + 0.1ms       | Line 466           |
| Batch flush (size)        | T + 0-5s        | Line 106-127       |
| Batch flush (time)        | T + 5s          | Line 82-104        |
| ClickHouse INSERT         | T + 5s + 0-50ms | Line 584           |
| **ACK (success)**         | **T + 5s + 100ms** | **Line 638**   |
| **NACK (failure)**        | **T + 5s + 100ms** | **Line 593**   |
| Post-processing           | T + 5s + 50ms   | Line 617           |

**Key Takeaway:** Messages are ACKed 5+ seconds AFTER arrival, ONLY AFTER successful processing! 🎯




