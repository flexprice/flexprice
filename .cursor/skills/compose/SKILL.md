---
name: compose
description: >-
  FlexPrice Docker/Makefile shortcuts — dev-setup, compose services, Kafka UI profile, logs.
  Deep env wizard is devenv. Trigger: compose, docker dev, dev-setup.
---

# **`compose`** — Docker dev shortcuts

For **modes**, **`.env*`**, Kafka topic quirks, migrations: use **[`devenv`](devenv/SKILL.md)**.

## First-time / full setup

```bash
make dev-setup
```

## Core backing services only

```bash
docker compose up -d postgres kafka clickhouse temporal temporal-ui
```

## Full stack

```bash
make up       # start all services
make down     # stop
make restart-flexprice   # restart app services, not infra
```

## Kafka UI (dev profile)

```bash
docker compose --profile dev up -d kafka-ui
```

## Quick reference URLs (local defaults)

See **`AGENTS.md`** / **`CLAUDE.md`** tables (API `8080`, Temporal UI `8088`, Kafka UI `8084`, ClickHouse `8123`).

## Debugging

```bash
docker compose logs -f flexprice-api
docker compose logs -f flexprice-consumer
docker compose logs -f flexprice-worker
```

## Notes

- Match **`FLEXPRICE_*`** env vars or `.env` to compose services when troubleshooting connection refused errors.
- Do not commit secrets inside compose overrides meant for production.

## Related skills

- [`devenv`](devenv/SKILL.md)
