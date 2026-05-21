---
name: devenv
description: >-
  Dev environment wizard — hybrid by default: .env/.env.local creds for RDS, managed Kafka, etc.;
  Docker only for components the user says are local; idempotent Compose checks when applicable;
  Kafka: prompt before default-topic creation (**`make init-kafka`**).
  Modes api/consumer/temporal_worker/local. Trigger: devenv, local dev, run-local.
disable-model-invocation: false
---

# **`devenv`** — environment wizard (often **hybrid**, not “all-local”)

Use as a **wizard**: confirm **what runs where**, then align **`.env` / `.env.local`** and **Compose** accordingly.

### What “devenv” usually means here

- **`devenv` ≠ “everything in Docker Compose.”** Many setups use **real or shared backends** via **`.env` only**: e.g. **RDS** (Postgres), **managed Kafka**, **hosted ClickHouse**, **Temporal Cloud**.
- **`devenv` = “run the FlexPrice binaries / processes you care about locally”** while **credentials and endpoints live in `.env` / `.env.local`** (`FLEXPRICE_POSTGRES_*`, `FLEXPRICE_KAFKA_BROKERS`, etc.). Treat those files as **the source of truth** for hosts, ports, SSL, IAM-style wiring the app understands.
- **Only spin up Docker (or localhost services) for pieces the user names as local.** Example: Postgres on **RDS** + **only** Kafka + Temporal in Compose → **`docker compose up -d kafka temporal temporal-ui`** (and anything else they list); **do not** insist on **`postgres`** containers or **§E1** blindly.
- **`§C` “127.0.0.1 + Compose defaults” is one recipe** — the **RDS / cloud** variant is: **omit or comment conflicting lines**, set **`FLEXPRICE_POSTGRES_HOST`** (and reader, SSL mode, passwords) to the **managed** endpoints from **`.env`**, and verify connectivity with **their** toolchain (`psql`, VPN, bastion docs) rather than **`docker compose exec postgres`**.

If the user hasn’t said what’s local vs remote, **ask once**: *Which deps are localhost/Compose vs RDS or other hosted?*

---

## 0. Idempotent Docker preflight (when **Compose** is in play)

Goals: avoid useless failures, redundant pulls, or duplicate work when infra is already fine. **Skip heavy Compose paths** when the stack is **fully remote** except app processes on the laptop.

### 0.1 Docker daemon

```bash
docker info >/dev/null 2>&1 && echo OK || echo NO_DOCKER
```

- If **`NO_DOCKER`**: **stop** — instruct the human to **start Docker Desktop**, **OrbStack**, **Colima**, **Rancher Desktop**, etc. (their platform). Optionally: `colima status` / `docker context ls` hints. Do **not** loop forever; wait for confirmation.

### 0.2 What is already running? (reuse healthy stack)

Repo root (`docker-compose.yml` present):

```bash
docker compose ps -a --format json
# Human-readable shorthand:
docker compose ps
```

- Check **only services the user relies on locally** — e.g. if DB is **RDS**, ignore **`postgres`** in Compose for “ready” semantics (still fine if an old **`postgres`** container exists unused).
- If **each Compose-backed dependency they use** (**`kafka`**, **`clickhouse`**, **`temporal`**, etc.) already **running** (+ **healthy**/ready per compose): **skip** duplicate **`docker compose up -d`** unless they asked for **recreate** / compose changed materially.
- **Idempotent converge:** **`docker compose up -d …`** for **that subset** remains safe — prefer **checking first** to reduce chat noise.

### 0.3 App images (`flexprice-api` stack)

Before **`make build-image`** / **`make dev-setup`**:

```bash
docker images flexprice-app:local --format '{{.Repository}}:{{.Tag}}'
```

- Missing image:** expected before first build — OK to **`docker compose build flexprice-build`** or **`make build-image`** once daemon is UP.
- **Caches:** rebuild **only** if Dockerfile / deps / user requests **–no-cache** — avoid needless full rebuild declarations.

### 0.4 When user wants “reset” vs “soft start”

| Intent | Typical action |
|--------|----------------|
| **Soft idempotent** | `docker compose up -d …` reconcile only |
| **Nuclear** | **`make clean-docker`** / **`down -v`** — **destructive**, warn first |

---

## 0.K Kafka broker choice (Compose default vs lighter alternatives)

### Supported default (this repo)

- **`docker-compose.yml`** wires **Confluent `cp-kafka`** with **EXTERNAL listener `localhost:29092`** and **`make init-kafka`**/`docker compose exec kafka …` **kafka-topics** CLIs assumed.
- **`FLEXPRICE_KAFKA_BROKERS`** for hybrid host binaries:** **`localhost:29092`** (matches advertised listener).

### “Can I use Redpanda / Kafka KRaft / other lightweight Kafka-compatible broker?”

- **Prefer real Kafka-compat + same listener story** until you validated **Sarama/Watermill** against the substitute.
- **Redpanda** (or embedded KRaft): **possible** locally **only if**:
  - Network & listener reach **`localhost:29092`** (or you change **`.env.local`** brokers consistently everywhere).
  - **Topic/partition tooling** equivalents exist — **`make init-kafka`** is **kafka-specific CLI** paths; swapping broker ⇒ **adapt or run topic creation manually** against the new tooling.
  - **Auto-create OFF** replicated — topics must still exist (**`kafka.auto.create.topics.enable: false`** pattern).
- Agent stance:** **Recommend stock Compose Kafka** unless user explicitly commits to maintaining alternate compose.** Document lightweight swap as **advanced / unsupported upstream** parity.

### Default Kafka topics (**ask**, then maybe create)

When **bringing Kafka online** or finishing a Compose Kafka setup (**not assumed**):

1. **Ask explicitly:** “Create the repo’s **default FlexPrice topics** (same names as **`make init-kafka`**)?” **Yes / no** — never run topic creation silently against shared clusters.
2. **If Compose `kafka`** and **yes** → from repo root, after Kafka responds to **`kafka-topics --list`**:

```bash
make init-kafka
```

3. **If managed / non-Compose broker** | **yes** → **`make init-kafka` will not run** (`docker compose exec kafka …`). Recreate **the same topic names** with their toolchain (MSK console, **`kafka-topics` against SASL/SSL bootstrap**, IaC). Mirror **`Makefile`**’s **`init-kafka`** list: **`events`**, **`events_lazy`**, **`events_dlq`**, **`events_backfill`**, **`events_post_processing`**, **`events_post_processing_backfill`**, **`system_events`**, **`wallet_alert`**, **`onboarding_events`**, **`staging_benchmarking`**, **`prod_events_v4`**, **`staging_events_backfill`**, **`staging_events`** (partitions/replication/placement policy per **their** policy, not blindly `1`/local).
4. **If no** → skip; user may rely on IaC / existing topics. Warn if app logs complain **unknown topic** / **UNKNOWN_TOPIC_OR_PARTITION**.
5. **Webhook naming:** **`config.yaml`** may expect **`flexprice_system_events`** while **`make init-kafka`** creates **`system_events`** — align topic name with config or extend creation list (**§G**).

---

## A. Natural language → intent (maps user chatter to actions)

**First clarify stack shape** when they mention RDS, staging DB, “only Kafka local”, etc.: map each dependency to **`.env` vars** vs **Compose service name**.

Interpret phrases like:

| User says | Interpret as |
|-----------|----------------|
| *start local server / API only / run API* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=api`** — use **`make run-local-api`** (or Compose `flexprice-api`) |
| *start consumer / run consumer* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=consumer`** — **`make run-local-consumer`** |
| *start Temporal worker / temporal mode* | **Hybrid**, **`FLEXPRICE_DEPLOYMENT_MODE=temporal_worker`** — **`go run`** fragment in §C4 (no dedicated Makefile alias today) |
| *everything in one process* | **`mode=local`** — **`make run-local`** (**needs Kafka infra**) |
| *full Docker / dev-setup* | **`make dev-setup`** |
| *change consumer group for local testing* | Edit **`.env.local`** — §D (pick the right **`FLEXPRICE_*`** key) |

When **Kafka** is part of setup, follow **§0.K “Default Kafka topics”** — **prompt** before **`make init-kafka`** unless the user already said yes to creating default topics in the same turn.

If unclear, confirm: **binary on host vs Docker**, **which backends are Compose vs RDS/managed**, and whether **infra** is already reachable (VPN, SSO, IP allowlists).

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

Use **`rg`** §B **before** editing. **`Makefile` `run-local-*`** sources **`.env` then `.env.local`** — **`set -a`**; **later wins**.

### C0 RDS / managed Postgres (no local `postgres` container)

- Set **`FLEXPRICE_POSTGRES_HOST`** / **`FLEXPRICE_POSTGRES_READER_*`** / **`FLEXPRICE_POSTGRES_SSLMODE`** (often **`require`**) / password from **`.env` or `.env.local`** per your org’s RDS doc — **never paste secrets into chat**.
- **Comment out** Compose-only aliases (**`postgres`**) in `.env` if they hijack **`run-local-*`** precedence.
- **Do not** require **`docker compose exec postgres`** for “green” — use app startup, **`psql`** to **their** endpoint, or DBA-approved checks.

### C1 Lines to **enable** when Go runs **on macOS** and **Postgres/Kafka/ClickHouse/Temporal** are **Compose on localhost**

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

**Run §0 first** (daemon + `docker compose ps`) **only when** the user wants **some** Compose services. **RDS for Postgres:** use **`C0`** + start **Kafka/ClickHouse/Temporal** subsets as needed; repo **Temporal often still wants Postgres somewhere** — if entirely off Compose, confirm **Temporal + visibility DB** story (managed Temporal Cloud vs local Temporal + remote DB vs full Compose).

Compose **Temporal** **`depends_on: postgres`** — cannot drop Postgres **from this Compose file** if using **bundled Temporal** unless you intentionally run Postgres elsewhere compatible with that compose Temporal config.

### E1 Hybrid infra (`postgres kafka clickhouse temporal temporal-ui`)

```bash
docker compose up -d postgres kafka clickhouse temporal temporal-ui
make migrate-postgres migrate-clickhouse generate-ent migrate-ent seed-db
```

**Kafka topics:** **`make init-kafka`** is **optional** — **§0.K** (ask first). If yes: **`make init-kafka`** once Kafka is reachable from Compose.

### E2 Fully Docker apps

```bash
make dev-setup
```

Rebuild when **Dockerfile** / image deps shift: `docker compose build flexprice-build` then `make restart-flexprice`.

Kafka UI (`--profile dev`): `docker compose --profile dev up -d kafka-ui` → **`http://localhost:8084`**

---

## F. **Verification loop** (bounded; run until OK or escalate)

Pick **`MAX_ROUNDS`** (default **8**) and **`SLEEP_SECONDS`** (default **4** unless hot path).

Build a **minimal checklist from the user’s stack** (RDS → skip Compose postgres checks; managed Kafka → skip **`docker compose exec kafka`** unless they still use Compose broker).

Each **round** (round 1 cheaply reuses §0 daemon check if Compose is in play and prior step failed):

1. **Compose status** (if any local Compose deps): **`docker compose ps`** → listed services **Running** where used.

2. **Postgres**  
   - **Compose Postgres:**  

```bash
docker compose exec -T postgres pg_isready -U flexprice -d flexprice
```
   - **RDS / remote:** probe with **their** method (VPN up, **`psql "$DATABASE_URL"`** locally with **values from `.env`** — redact when discussing; or migration smoke via **`make migrate-postgres`** if wired to same DSN).

3. **Kafka**  
   - **Compose Kafka:**

```bash
docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --list
```
   - **Managed / host-only brokers:** **`FLEXPRICE_KAFKA_BROKERS`** from **`.env`**, CLI or **consumer startup logs**; **`make init-kafka`** assumptions may not apply.

4. **ClickHouse** — if local HTTP listener expected:

```bash
curl -sf http://127.0.0.1:8123/ping >/dev/null && echo OK
```
   If ClickHouse is **hosted**, substitute **their** ping or query path.

5. **Mode-specific:**
   - **`api`** or **`local`** (listening on `:8080` from host defaults): **`curl -sf -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/health`** expect **`200`**.
   - **`consumer`**: HTTP ping **optional** — confirm **broker** reachable from host (**`FLEXPRICE_KAFKA_BROKERS`** / `localhost:29092` when using Compose Kafka), process **startup logs**, no fatal on consumer init.
   - **`temporal_worker`**: `:8080` may **absent**. Ping **Temporal UI** proxy instead: **`curl -sf http://127.0.0.1:8088 >/dev/null`** **and/or** Temporal container **`7233`** path per ops habits; alternatively ask user **do they see worker registration logs**.

**On failure:**

- Inspect **grep output** §B — stray **`postgres`/`kafka:9092`** overrides when RDS / managed Kafka is intended?
- **`docker compose up -d`** only for **Compose-backed** services they use — not a universal fix when the failure is VPC / RDS security group / IAM.
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
- Insist on **§E1 full infra** when the user already said **Postgres/Kafka/etc. lives in RDS or managed** (`devenv` = **their** `.env` truth + **minimal** Compose).

---

## Related skills

- [`compose`](compose/SKILL.md) — short Docker/Makefile cheat sheet  
- [`godev`](godev/SKILL.md) — `go test` after server lives  
- [`apitest`](apitest/SKILL.md) — master **`curl`** QA after infra GREEN