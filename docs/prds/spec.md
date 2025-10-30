# Watermill Kafka Batch Consumer – Specification

## Overview
- Goal: Implement efficient application-level batching for Watermill Kafka consumers while tuning Sarama for buffered fetches.
- Context: Watermill delivers messages singly via channel; Sarama can fetch in batches internally. We accumulate messages with size/timeout and process as a batch.

## Scope
- In-scope: Batch accumulation loop, ack/nack strategy, error handling, configuration tuning, observability.
- Out-of-scope: Replacing Watermill with direct Sarama consumption, multi-topic orchestration, cross-service refactors.

## Objectives
- Reduce per-message overhead by processing batches.
- Preserve ordering within a partition and correctness of acks/nacks.
- Provide tunable batch size and timeout to balance latency and throughput.

## Non-Goals
- Exactly-once processing guarantees (keep at-least-once with Watermill semantics).
- Sophisticated backpressure beyond basic buffering.

## Functional Requirements
- Batch accumulation:
  - Accumulate up to `batchSize` messages or flush on `batchTimeout`.
  - On channel close, flush remaining messages.
- Processing:
  - Invoke `processBatch([]*message.Message)` for each flushed batch.
  - On success, `Ack()` all messages in the batch.
  - On partial failures, configurable policy: either `Nack()` entire batch or implement per-message fallback (default: nack entire batch for simplicity).
- Configuration:
  - App-level: `batchSize` (int), `batchTimeout` (duration).
  - Sarama/Watermill: `Consumer.Fetch.MinBytes`, `Consumer.Fetch.MaxWaitTime`, `ChannelBufferSize`.
- Observability:
  - Log batch flushes with size and age.
  - Metrics: batch size distribution, flush count, processing latency, ack/nack counts.

## Design
- Data flow:
  - Kafka -> Sarama fetch (buffered) -> Watermill subscriber -> `msgs <-chan *message.Message` -> batch accumulator -> `processBatch` -> ack/nack.
- Accumulator algorithm:
  - Maintain slice `batch` and timer `batchTimeout`.
  - On each message: append; if `len(batch) >= batchSize` then flush.
  - On timer: flush partial batch if not empty; reset timer.
- Concurrency:
  - Single accumulator per partition subscription to preserve ordering; multiple goroutines can be used per partition if ordering is not required, but default keeps ordering.

## Configuration Tuning (Sarama via Watermill)
- `Consumer.Fetch.MinBytes`: set to a higher value (e.g., 10KiB) to encourage batch fetches.
- `Consumer.Fetch.MaxWaitTime`: small wait (e.g., 1s) to coalesce fetches.
- `ChannelBufferSize`: increase (e.g., 512) to allow buffering before the app loop drains messages.
- These are set on `saramaConfig := kafka.DefaultSaramaSubscriberConfig()` prior to creating the subscriber.

## Error Handling
- Batch processing error:
  - Default: `Nack()` all messages in the batch to allow redelivery.
  - Optional enhancement: Retry `processBatch` with exponential backoff up to N attempts; if still failing, nack.
- Message-level anomalies:
  - If `processBatch` returns per-message results, consider granular ack/nack; otherwise keep batch-level simplicity.

## Observability & Metrics
- Emit logs at `INFO` for batch flushes: size, elapsed since first message.
- Metrics (Prometheus style):
  - `batch_flush_total` (counter), `batch_size` (histogram), `batch_flush_age_ms` (histogram), `batch_process_latency_ms` (histogram).
  - `messages_acked_total`, `messages_nacked_total`.

## Pseudocode
```go
const batchSize = 100
const batchTimeout = 500 * time.Millisecond

func processBatch(batch []*message.Message) error { /* implement */ return nil }

func consumeInBatches(msgs <-chan *message.Message) {
    batch := make([]*message.Message, 0, batchSize)
    timer := time.NewTimer(batchTimeout)
    defer timer.Stop()

    flush := func() {
        if len(batch) == 0 { return }
        start := time.Now()
        if err := processBatch(batch); err != nil {
            for _, m := range batch { m.Nack() }
        } else {
            for _, m := range batch { m.Ack() }
        }
        _ = start // record latency metric
        batch = batch[:0]
        timer.Reset(batchTimeout)
    }

    for {
        select {
        case msg, ok := <-msgs:
            if !ok { flush(); return }
            batch = append(batch, msg)
            if len(batch) >= batchSize { flush() }
        case <-timer.C:
            flush()
        }
    }
}
```

## Performance Targets (initial)
- Throughput: 2–5x improvement over per-message processing in equivalent workloads.
- Latency: p50 <= `batchTimeout` when not hitting `batchSize`.

## Rollout Plan
- Stage: enable accumulator in a test environment.
- Validate metrics, adjust `batchSize`/`batchTimeout`.
- Deploy gradually; monitor lag and nack rates.

## Risks & Mitigations
- Larger batches increase retry cost: start with moderate sizes; add backoff.
- Timer drift: reset timer after each flush.

## Acceptance Criteria
- Configurable batch processing works with Watermill subscriber.
- Passing tests for batch behavior and ack/nack semantics.
- Metrics emitted and observed in local/integration environment.