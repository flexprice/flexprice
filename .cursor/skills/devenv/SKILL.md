---
name: devenv
description: >-
  Local dev environment wizard — compose, grep/fix .env*, api/consumer/temporal_worker/local modes,
  consumer groups, verify loop with curl. Trigger: devenv, local dev setup, docker env, run-local.
disable-model-invocation: false
---

# **`devenv`** — local environment (wizard)

Use as a **wizard**: prefer **incremental steps**, **bounded verify loops**, and **safe `.env` handling**.

---

## A. Natural language → intent (maps user chatter to actions)

Interpret phrases like:

| User says | Interpret as |
|-----------|----------------|
| *start local server / API only / run API* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=api`** — use **`make run-local-api`** (or Compose `flexprice-api`) |
| *start consumer / run consumer* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=consumer`** — **`make run-local-consumer`** |
| *start Temporal worker / temporal mode* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=temporal_worker`** — **`go run`** fragment in §C4 (no dedicated Makefile alias today) |
| *everything in one process* | **`mode=local`** — **`make run-local`** (**needs Kafka infra**) |
| *full Docker / dev-setup* | **`make dev-setup`** |
| *change consumer group for local testing* | Edit **`.env.local`** — §D (pick the right **`FLEXPRICE_*`** key) |

If unclear, confirm: **binary on host vs Docker**, and whether **infra** is already running.

---

## B. Inspect `.env` / `.env.local` with **grep** (required technique)

Before telling the user to paste secrets:

1. **Repo root.** Prefer read-only probes:

```bash
# Keys only-ish: still may show assignment values — summarize carefully
rg '^[[:space:]]*FLEXPRICE_(DEPLOYMENT_MODE|POSTGRES_|KAFKA_|CLICKHOUSE_|TEMPORAL_|EVENTPROCESSING_|WEBHOOK_)' .env .env.local 2>/dev/null || true

# Lines that MUST be commented/disabled when app runs ON HOST against Docker infra:
rg '^[[:space:]]*FLEXPRICE_(POSTGRES_(HOST|READER_HOST)|KAFKA_BROKERS|CLICKHOUSE_ADDRESS|TEMPORAL_ADDRESS)' .env .env.local 2>/dev/null || true
```

2. **When reporting in chat**, **redact values** after `=` for **`PASSWORD`**, **`SECRET`**, **`KEY`**, **`TOKEN`**, **`DSN`** (show first/last 2 chars only or `***`).

3. **Edit strategy:** use **`apply_patch`/search_replace** to **comment problematic lines** (prefix `# `) rather than deleting history. Add correct host-oriented lines under a banner in **`.env.local`**:

```bash
# --- Managed by Cursor devenv skill: hybrid (host app) ---
```

Never commit **`.env` / `.env.local`** secrets; `.gitignore` usually excludes them.

---

## C. Hybrid host binaries — `.env.local` correctness

### C1 Lines to **enable** when Go runs **on macOS** and infra is **Compose on localhost**

```bash
FLEXPRICE_POSTGRES_HOST=127.0.0.1
FLEXPRICE_POSTGRES_PORT=5432
FLEXPRICE_POSTGRES_USER=flexprice
FLEXPRICE_POSTGRES_PASSWORD=flexprice123
FLEXPRICE_POSTGRES_DBNAME=flexprice
FLEXPRICE_POSTGRES_SSLMODE=disable
FLEXPRICE_POSTGRES_READER_HOST=127.0.0.1
FLEXPRICE_POSTGRES_READER_PORT=5432

FLEXPRICE_KAFKA_BROKERS=localhost:29092

FLEXPRICE_CLICKHOUSE_ADDRESS=127.0.0.1:9000

FLEXPRICE_TEMPORAL_ADDRESS=127.0.0.1:7233
```

### C2 Lines to **`#`-comment out** when they point at Docker DNS (**wrong on host**)

Typical killers:

```bash
# FLEXPRICE_POSTGRES_HOST=postgres
# FLEXPRICE_POSTGRES_READER_HOST=postgres
# FLEXPRICE_KAFKA_BROKERS=kafka:9092
# FLEXPRICE_CLICKHOUSE_ADDRESS=clickhouse:9000
# FLEXPRICE_TEMPORAL_ADDRESS=temporal:7233
```

Makefile loads **`.env` then `.env.local`** — **`set -a`** (see `Makefile`); **later file wins**. Keep overrides in **`.env.local`** to avoid battling `.env`.

### C3 **Don’t confuse** **`make run-server`**

Runs **`go run cmd/server/main.go`** **without** sourcing `.env` / `.env.local`. Prefer **`make run-local-*`**.

---

## C4 Commands per **deployment mode** (host / hybrid)

| Mode | Meaning | Command (host) |
|------|---------|----------------|
| **`api`** | HTTP API only; ingestion handlers gated off in router | `make run-local-api` |
| **`consumer`** | Kafka-heavy process; **`make dev-setup`**’s **`flexprice-consumer`** equivalent | `make run-local-consumer` |
| **`temporal_worker`** | Temporal workers + minimal router (see `cmd/server/main.go`) | Same env loading pattern; **Makefile has no alias** → run: `(set -a && [ -f .env ] && . ./.env; [ -f .env.local ] && . ./.env.local; set +a && FLEXPRICE_DEPLOYMENT_MODE=temporal_worker go run cmd/server/main.go)` |
| **`local`** | Single process (**API + consumer path + Temporal** where applicable) — **Kafka mandatory** per `cmd/server/main.go` | `make run-local` |

Explain **Temporal worker** rarely needs `:8080` — **§F** pings differ.

---

## D. **Consumer groups** for local experimentation

Goals: avoid clashing teammates’ offsets or isolate consumers against the same topics.

Nested YAML overrides use **`FLEXPRICE_<SECTION>_<FIELD>`** (upper snake). When ambiguous, **`grep consumer_group`** in **`internal/config/config.yaml`** and match **`mapstructure`** in **`internal/config/config.go`**.

| Pipeline (YAML) | Typical env |
|-----------------|------------|
| **`kafka.consumer_group`** | **`FLEXPRICE_KAFKA_CONSUMER_GROUP=<unique>`** |
| **`event_processing.consumer_group`** | **`FLEXPRICE_EVENTPROCESSING_CONSUMER_GROUP=<unique>`** |
| **`event_processing_lazy.consumer_group`** | Often **`FLEXPRICE_EVENTPROCESSINGLAZY_CONSUMER_GROUP`** — **confirm via startup logs** |
| **`meter_usage_tracking`**, **`wallet_balance_alert`**, **`webhook.consumer_group`**, etc. | Derive **`FLEXPRICE_<PARENT_CAPS>_CONSUMER_GROUP`** from nesting |

Practical discipline:

1. Change **only** the groups you need to isolate.
2. **`kafka-consumer-groups`** / startup logs prove the effective id.
3. If ineffective, **`grep BindEnv`** in **`config.go`** for explicit binds.

---

## E. **Recipes** — Docker infra (minimal practical)

Compose **Temporal** **`depends_on: postgres`** — cannot drop Postgres if using bundled Temporal.

### E1 Hybrid infra (`postgres kafka clickhouse temporal temporal-ui`)

```bash
docker compose up -d postgres kafka clickhouse temporal temporal-ui
make migrate-postgres migrate-clickhouse generate-ent migrate-ent seed-db init-kafka
```

### E2 Fully Docker apps

```bash
make dev-setup
```

Rebuild when **Dockerfile** / image deps shift: `docker compose build flexprice-build` then `make restart-flexprice`.

Kafka UI (`--profile dev`): `docker compose --profile dev up -d kafka-ui` → **`http://localhost:8084`**

---

## F. **Verification loop** (bounded; run until OK or escalate)

Pick **`MAX_ROUNDS`** (default **8**) and **`SLEEP_SECONDS`** (default **4** unless hot path).

Each **round**:

1. **Compose status:** `docker compose ps` → **critical services Running** (**postgres kafka clickhouse temporal** at minimum).

2. **Postgres:**

```bash
docker compose exec -T postgres pg_isready -U flexprice -d flexprice
```

3. **Kafka (inside cluster):**

```bash
docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --list
```

4. **ClickHouse:**

```bash
curl -sf http://127.0.0.1:8123/ping >/dev/null && echo OK
```

5. **Mode-specific:**
   - **`api`** or **`local`** (listening on `:8080` from host defaults): **`curl -sf -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/health`** expect **`200`**.
   - **`consumer`**: HTTP ping **optional** — confirm **broker** reachable from host (`localhost:29092`), process **startup logs**, no fatal on consumer init.
   - **`temporal_worker`**: `:8080` may **absent**. Ping **Temporal UI** proxy instead: **`curl -sf http://127.0.0.1:8088 >/dev/null`** **and/or** Temporal container **`7233`** path per ops habits; alternatively ask user **do they see worker registration logs**.

**On failure:**

- Inspect **grep output** §B — uncommented **`postgres`/`kafka:9092`** bindings?
- Retry **`docker compose up -d`**.
- **`sleep`** then next round.

**Escalate** after **`MAX_ROUNDS`**: summarize last errors; **do not infinite loop**.

---

## G. One-time facts (tell user if debugging)

| Service | Host | Port / note |
|---------|------|-------------|
| Kafka from **host** | `localhost` | **29092** (not `9092`) |
| Temporal UI | `127.0.0.1` | **8088** |
| API (host default) | `127.0.0.1` | **8080** |

Webhook topic mismatch risk: **`config.yaml`** **`webhook.topic`** (**`flexprice_system_events`**) vs **`make init-kafka`** list (**`system_events`**). Symptoms → topic creation.

---

## H. Anti-actions

- Paste full **`.env`** into transcripts (secrets).
- Use **`kafka:9092`** from **macOS Go binary**.
- **`make swagger-3-0`** without network (**converter.swagger.io**).

---

## Related skills

- [`compose`](compose/SKILL.md) — short Docker/Makefile cheat sheet  
- [`godev`](godev/SKILL.md) — `go test` after server lives  
- [`apitest`](apitest/SKILL.md) — master **`curl`** QA after infra GREEN