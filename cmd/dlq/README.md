# dlq — dead-letter queue replay

Drains a watermill poison-queue (dead-letter) topic and routes each message back
to the topic it was originally poisoned from.

## Why this exists

When a consumer handler exhausts its retries, watermill's `PoisonQueue`
middleware writes the message to a DLQ topic with four metadata headers:

| header | meaning |
|---|---|
| `topic_poisoned` | the origin topic — **the replay target** |
| `reason_poisoned` | why it failed (e.g. `dial tcp …: connection refused`) |
| `handler_poisoned` | the handler that rejected it |
| `subscriber_poisoned` | the subscriber (e.g. `kafka.PubSub`) |

Because the origin topic is carried on the message, replay is a mechanical
"read the header, strip the poison headers, republish" — no hard-coded topic
map to drift. This tool does exactly that, plus a loop guard.

## Usage

Broker + auth come from the standard `FLEXPRICE_*` env (same as `server` /
`migrate`). **Always dry-run first** — it prints the routing and a poison-reason
histogram without producing anything:

```bash
go run ./cmd/dlq replay --source staging_events_dlq --dry-run
# or: make dlq-replay SOURCE=staging_events_dlq

# real run once the reasons look transient and the root cause is resolved:
go run ./cmd/dlq replay --source production_event_processing_dlq

# bound a replay to a specific incident window:
go run ./cmd/dlq replay --source production_meter_usage_tracking_service_dlq \
    --since 2026-07-28T06:18:00Z
```

### Flags

| flag | default | purpose |
|---|---|---|
| `--source` | (required) | DLQ topic to drain |
| `--target` | per-message `topic_poisoned` | override destination for ALL messages (use only when the header is missing/wrong) |
| `--group` | `dlq-replay-tool` | consumer group that tracks the resume offset |
| `--since` | resume point | RFC3339; **ignore the resume point** and replay from the first message at/after this time |
| `--from-start` | false | **ignore the resume point** and re-drain from each partition's oldest retained offset |
| `--max` | 0 (no cap) | cap messages replayed |
| `--max-replays` | 3 | quarantine a message once its `replay_count` hits this (loop guard) |
| `--dry-run` | false | show routing + reasons, produce nothing, **commit nothing** |

`--since` and `--from-start` are mutually exclusive.

## Resume semantics (why a second run doesn't re-replay everything)

Kafka does **not** delete a message when you consume it — it stays in the topic
until retention expires. So a naive "read from the beginning" replay would
re-send every message still in the DLQ on every run.

Instead, progress is tracked as **committed offsets under a consumer group**
(`--group`, default `dlq-replay-tool`), and an offset is committed **only after
the message has been successfully republished**. Concretely:

- **Day 1:** 50 messages arrive, you run replay → all 50 republished, offset
  committed to 50.
- **Day 2:** 50 more arrive → the next run resumes at offset 50 and replays only
  the new 50. The day-1 messages are not touched.

A crash mid-run re-replays only the uncommitted tail (at-least-once), never the
whole topic. `--dry-run` commits nothing, so a preview never advances the resume
point. To deliberately replay a window again, use `--from-start` or `--since`.

> The tool tracks offsets under this group id via an offset manager; it does not
> consume as a live group member, so it won't collide with real consumers. Don't
> point a real consumer at the `dlq-replay-tool` group.

## Safety model

- **Bounded:** each partition is drained only up to its end offset at start, so a
  run always terminates and never tails messages produced mid-run.
- **Resumable, commit-after-publish:** offsets advance only past messages that
  were actually republished (or deliberately skipped/quarantined), so a re-run
  never double-sends what already succeeded and never skips what didn't.
- **Loop guard:** every replay stamps/increments a `replay_count` header; once a
  message reaches `--max-replays` it is quarantined (offset advances past it, but
  it is not re-sent). This stops a re-poison loop during an ongoing outage.
- **Contract-preserving:** republish uses watermill's `DefaultMarshaler`, so a
  replayed message is byte-identical to a freshly produced one (keyless, same
  UUID + metadata handling) — only the poison headers are removed.
- **Idempotency is on the consumer side:** flexprice events carry an `id` and the
  billing tables are `ReplacingMergeTree`, so a duplicate replay dedups. Confirm
  the specific handler is idempotent before replaying non-event topics.

## Before you replay — read the reason

Check the `reason_poisoned` histogram in the dry-run:

- **Transient infra** (`connection refused`, `dial tcp … timeout`,
  `context canceled`, `database system is shutting down`) → safe to replay once
  the burst has **stopped** and the root cause is resolved.
- **Schema / code** (`pq: column … does not exist`) → only replay after the fix
  is deployed, or every message will immediately re-poison.

## Deployment

Runs as a Kubernetes Job in-cluster under the consumer's Kafka auth (Workload
Identity / OAUTHBEARER on GMK) — no bastion, no short-lived tokens. Job manifest
lives in the infrastructure repo.
