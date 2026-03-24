# Local Testing Guide

Step-by-step guide to running Flexprice locally and validating changes end-to-end.
Works for humans and AI agents alike — no manual env var configuration needed.

---

## Pre-configured Local API Key

Use this key for all local API calls — it's baked into `.env.local`:

```
x-api-key:        sk_local_flexprice_test_key
x-environment-id: 00000000-0000-0000-0000-000000000000
```

Tenant ID: `00000000-0000-0000-0000-000000000000` · Environment: Sandbox

---

## How local config works

`.env.local` is a committed file that contains all local infrastructure settings.
When you run via `make run-local*` targets, the Makefile sources `.env` first
then `.env.local` on top — so local Docker endpoints override any production
settings in `.env`, without modifying that file.

**Never add** `godotenv.Load(".env.local")` to application code — that would
affect production deployments.

---

## Quick Start

### 1. Start OrbStack

```bash
open -a OrbStack && sleep 3
```

### 2. Start local infrastructure

```bash
docker compose up -d postgres kafka clickhouse
sleep 10   # wait for Kafka to fully initialise
```

### 3. Create Kafka topics

Run once (idempotent — safe to repeat):

```bash
for topic in raw_events events events_lazy events_post_processing \
             events_backfill system_events onboarding_events balance_alert; do
  docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 \
    --create --topic $topic --partitions 3 --replication-factor 1 2>/dev/null || true
done
```

### 4. Run migrations

```bash
# Postgres SQL migrations (reads docker-compose defaults, safe to run as-is)
make migrate-postgres

# Ent schema migrations against local Postgres (uses .env.local)
make migrate-local

# ClickHouse migrations
make migrate-clickhouse
```

> **Warning:** `make migrate-ent` reads `.env` and will run against production
> if that file points there. Always use `make migrate-local` instead.

### 5. Run the server

**Two separate terminals (mirrors production deployment):**

```bash
# Terminal 1 — API server
make run-local-api

# Terminal 2 — Kafka consumer
make run-local-consumer
```

**Or everything in one process (quickest for local dev):**

```bash
make run-local
```

### 6. Verify

```bash
curl http://localhost:8080/health
# → {"status":"ok"}

curl http://localhost:8080/v1/meters \
  -H "x-api-key: sk_local_flexprice_test_key" \
  -H "x-environment-id: 00000000-0000-0000-0000-000000000000"
# → {"items":[],"pagination":{...}}
```

---

## Shutdown

```bash
# Stop Go processes (Ctrl+C in each terminal, or:)
pkill -f "go run cmd/server"

# Stop Docker services
docker compose down
```

---

## Useful DB Commands

```bash
# PostgreSQL shell
docker compose exec postgres psql -U flexprice -d flexprice

# ClickHouse shell
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice

# Quick ClickHouse queries
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT id, external_customer_id, event_name FROM events ORDER BY timestamp DESC LIMIT 10 FORMAT PrettyCompact"
```

---

## Local Infrastructure Reference

| Service | Host:Port | User | Password | DB |
|---------|-----------|------|----------|----|
| PostgreSQL | `localhost:5432` | `flexprice` | `flexprice123` | `flexprice` |
| ClickHouse | `localhost:9000` | `flexprice` | `flexprice123` | `flexprice` |
| Kafka | `localhost:29092` | — | — | — |

| Seed record | ID |
|-------------|----|
| Default Tenant | `00000000-0000-0000-0000-000000000000` |
| Sandbox Environment | `00000000-0000-0000-0000-000000000000` |
| Production Environment | `00000000-0000-0000-0000-000000000001` |

---

## Common Pitfalls

| Problem | Cause | Fix |
|---------|-------|-----|
| `make migrate-ent` or `make migrate-local` hits production | `.env` loaded, not `.env.local` | Always use `make migrate-local` |
| `address already in use :8080` | Another process on 8080 | `lsof -ti:8080 \| xargs kill` |
| Consumer: `topic does not exist` errors | Topic not created | Run the topic creation loop in Step 3 |
| `{"error":"Unauthorized"}` | Wrong API key | Use `sk_local_flexprice_test_key` from `.env.local` |
| ClickHouse `raw_events` table is empty | Consumer writes to `events`, not `raw_events` | Check the `events` table — that's where transformed events land |
| Settings lookup fails | Missing `x-environment-id` header | Always send `-H "x-environment-id: 00000000-0000-0000-0000-000000000000"` |

---

## See also

- [`SANITY_CHECK.md`](SANITY_CHECK.md) — standard pre-validation checklist to run before every PR
