# Watermill Kafka Batch Consumer – Tasks

## Implementation
- Tune Sarama via Watermill:
  - Set `Consumer.Fetch.MinBytes` (e.g., 10KiB), `Consumer.Fetch.MaxWaitTime` (e.g., 1s), and `ChannelBufferSize` (e.g., 512) on `saramaConfig := kafka.DefaultSaramaSubscriberConfig()`.
- Add batch accumulator:
  - Implement `consumeInBatches(msgs <-chan *message.Message)` with `batchSize` and `batchTimeout`.
  - Provide `processBatch([]*message.Message)` hook for domain processing.
- Ack/Nack policy:
  - Default: on batch success `Ack()` all, on failure `Nack()` all.
  - Optional: future enhancement for per-message fallback.
- Configuration plumbing:
  - Read `batchSize` and `batchTimeout` from application configuration or environment.
  - Document runtime overrides.

## Testing
- Unit tests:
  - Accumulator flushes on size threshold.
  - Accumulator flushes on timeout when not reaching `batchSize`.
  - Channel close flushes remaining messages.
  - Ack/Nack behavior: success acks all; failure nacks all.
- Integration tests:
  - Subscriber receives from Watermill channel; accumulator processes batches; verify metrics and logs.
  - Backpressure sanity: increased `ChannelBufferSize` avoids drops; no message loss.

## Observability
- Logging:
  - Log batch flush with size and age.
- Metrics:
  - Counters: `batch_flush_total`, `messages_acked_total`, `messages_nacked_total`.
  - Histograms: `batch_size`, `batch_flush_age_ms`, `batch_process_latency_ms`.

## Performance & Tuning
- Baseline:
  - Measure throughput and latency before batching.
- Tuning runs:
  - Experiment with `batchSize` and `batchTimeout` values; record impacts.
  - Adjust Sarama fetch settings to balance lag and throughput.

## Rollout
- Enable in staging with conservative defaults.
- Monitor lag, nack rates, and processing latency.
- Incrementally increase `batchSize` as confidence grows.

## Deliverables
- Code changes implementing accumulator and config tuning.
- Tests covering accumulator behavior.
- Documentation for configuration knobs and operational guidance.

## Acceptance Criteria
- All tests pass; batch behavior correct under size and timeout.
- End-to-end consumption with Watermill works; no message loss observed.
- Metrics available and provide visibility into batching performance.