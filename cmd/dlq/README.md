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

- **Bounded:** each partition captures its end offset when processing that partition
  begins and drains only up to that boundary.
- **Resumable, commit-after-publish:** offsets advance only past messages that
  were actually republished (or deliberately skipped/quarantined), so a retry resumes from saved progress. A crash before an offset commit
  can replay messages that were already published.
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

## Running it

**From a laptop** (brokers reachable, e.g. India MSK): the CLI above, with the
`FLEXPRICE_KAFKA_*` env of the target cluster.

**Internal admin portal:** use `POST /v1/dlq/replay` on the regional admin
server (`deployment.mode=admin`), authenticated with `X-Admin-Secret`.
The admin deployment needs network access and Kafka credentials for that region.

```bash
curl "$ADMIN_URL/v1/dlq/replay" \
  -H "X-Admin-Secret: $FLEXPRICE_ADMIN_SECRET" \
  -H 'Content-Type: application/json' \
  -d '{"source_topic":"production_event_processing_dlq","dry_run":true}'
```

With `"dry_run": true` the request is a preview: it runs synchronously (ten-minute
timeout), publishes nothing, saves no progress, and returns `scanned`, `replayed`,
`skipped`, `quarantined`, `by_target`, and `by_reason`.

With `"dry_run": false` the request starts `ReplayDLQWorkflow` on the Temporal
worker (task queue `events`) and returns `202` with `workflow_id` and `run_id`.
The admin server does no Kafka work for a real run, so the **worker** in that region
needs Kafka credentials. Poll the run with the returned `run_id`; without it you get the
latest run for that workflow ID, which may be a newer replay:

```bash
curl "$ADMIN_URL/v1/dlq/replay/runs/$WORKFLOW_ID?run_id=$RUN_ID" -H "X-Admin-Secret: $FLEXPRICE_ADMIN_SECRET"
```

`status` is `queued` until a worker starts the activity (a run stuck there means no
worker polls `events` in that region), then `running`, then `completed` with
`summary` or `failed` with `last_error`. Committed offsets keep progress across
activity retries and failed runs. Runs with `max`, `since` or `from_start` are not
retried, since a retry would recount or republish messages.

The workflow ID is `dlq-replay-<source_topic>:<group>`, so a second replay for the
same topic and group returns `409` while one is running. That guard does not cover
the CLI: never run the CLI against a topic/group the admin portal is replaying.

Other request fields: `target_override`, `group`, `since` (RFC3339), `from_start`,
`max`, and `max_replays`. Defaults match the CLI.

The routes are available only on the internal admin server, not the public API.
