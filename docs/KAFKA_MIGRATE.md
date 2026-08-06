# kafka-migrate

Standalone binary that reconciles Kafka topics against a desired spec: creates missing
topics and grows under-provisioned partition counts. It never deletes topics, never
shrinks partitions, and never changes replication factor on an existing topic (it only
warns on mismatch). Baked into the main Docker image alongside `server` and `migrate`.

## Desired topic spec — where it comes from

1. **`FLEXPRICE_KAFKA_TOPICS`** (JSON env var), if set, is the desired spec and **fully
   replaces** everything below (no merge with config.yaml).
2. Otherwise, `kafka.topics_defaults` + `kafka.topics` in `config.yaml` (Viper-loaded,
   same file/env layering as the rest of the app) are used.

The `config.yaml` values are base/dev topic names (unprefixed, e.g. `events`). A shared
prod cluster almost always needs different (prefixed) names and different partition
counts, so **prod deploys must set `FLEXPRICE_KAFKA_TOPICS`** — if they don't, the binary
logs a loud `WARN` and proceeds with the dev names, which is safe locally but wrong for a
shared cluster.

> **GCP prod (us-west1) is out of scope for this tool today.** Its Kafka topics are
> owned and reconciled by Terraform (`infrastructure/gcp/production/us-west1`,
> `topic_owner: terraform` in that stack's `values.yaml`) — running kafka-migrate in
> apply mode there would fight Terraform over the same topics. Only run `-dry-run`
> against that cluster, if at all, until the ownership model is revisited. Other
> clusters (staging, AWS prod MSK) are not under this constraint.

### Spec shape (same for config.yaml and the JSON env override)

```yaml
topics_defaults:
  replication_factor: 3
  retention_ms: 604800000 # 7d
topics:
  events:
    partitions: 6
  events_dlq:
    partitions: 3
    replication_factor: 1 # per-topic override of the default
```

JSON env equivalent:

```bash
export FLEXPRICE_KAFKA_TOPICS='{
  "defaults": {"replicationFactor": 3, "retentionMs": 604800000},
  "topics": {
    "prod_events": {"partitions": 12},
    "prod_events_dlq": {"partitions": 3, "replicationFactor": 1}
  }
}'
```

## Running it

The binary reads the same `config.yaml`/env-var stack as `server` (`FLEXPRICE_KAFKA_BROKERS`,
TLS/SASL settings, etc.) — it needs a working `kafka.brokers` connection, nothing else.

```bash
# Local, against docker-compose kafka (uses config.yaml's dev topic list)
go run ./cmd/kafka-migrate -dry-run
go run ./cmd/kafka-migrate            # actually creates/grows topics

# From the built image
docker run --rm \
  -e FLEXPRICE_KAFKA_BROKERS='broker1:9092,broker2:9092' \
  <image> ./kafka-migrate -dry-run

# Prod-style run with an explicit topic override
docker run --rm \
  -e FLEXPRICE_KAFKA_BROKERS='...' \
  -e FLEXPRICE_KAFKA_TOPICS='{"defaults":{...},"topics":{...}}' \
  <image> ./kafka-migrate
```

Flags:
- `-dry-run` — logs the planned actions (`WOULD CREATE` / `WOULD GROW` / warnings) without
  touching the cluster. Always run this first against a new environment or spec change.

## Reading the output

```
kafka-migrate: env=production topics=9 source=config dry-run=true
```
`source` is either `config` (config.yaml, dev names — check this isn't prod!) or
`env:FLEXPRICE_KAFKA_TOPICS` (explicit override, expected for prod).

Per-topic actions during apply:
- `created=N` — topic didn't exist, created with desired partitions/RF/`retention.ms` all applied.
- `grown=N` — topic existed with fewer partitions than desired, partitions added.
- `unchanged=N` — already matches.
- `skipped-shrink=N` — topic has **more** partitions than desired; never reduced automatically, review manually.
- `rf-mismatch=N` — replication factor differs from desired; never changed automatically (Kafka RF changes require a partition reassignment, out of scope for this tool), review manually.
- `retention-mismatch=N` — `retention.ms` differs from desired on an existing topic; never changed automatically (warn-only, same as RF), review manually and apply via `kafka-configs --alter` if intended.

Note: RF and retention are only ever applied at **create** time. Once a topic exists,
kafka-migrate will only grow its partitions — RF and retention drift are detected and
logged but never auto-corrected.

## Verifying locally

```bash
docker build -t flexprice:local .
docker run --rm flexprice:local ./kafka-migrate -dry-run
```
