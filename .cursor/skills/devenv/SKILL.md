---
name: devenv
description: >-
  Dev environment wizard — hybrid by default: .env/.env.local creds for RDS, managed Kafka, etc.;
  Docker only for components the user says are local; idempotent Compose checks when applicable;
  **Local Compose:** always use localhost creds (**§C1** in `.env.local`) before migrations, seeds, `go run`/run-local-*;
  **Effort → model (**§M**):** classify S/M/L, use fast models for checklist work, escalate to **Claude Sonnet 4.x (e.g. 4.7 when offered)** / strongest reasoning tier for hybrid/RDS churn;
  **Parallelism (**§N**):** spawn **Cursor Task subagents** for independent probes or read-only investigate paths when it speeds **§F** convergence.
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

## **`§L`** — **Local-first creds** for migrations, seeds, `make migrate-*`, and local processes

Unless the human **explicitly** said Postgres/ClickHouse/Temporal are **remote** (RDS, hosted CH, Temporal Cloud):

1. **Treat `.env.local` as the safety rail** for anything that runs **Go on the host** (API, worker, **`make run-local-*`**, **`go run cmd/migrate/main.go`** / **`migrate-ent`**, codegen that opens DB if applicable). Base `.env` often repeats **staging or shared URLs** from onboarding. **`migrate-ent` reads `FLEXPRICE_*` like the server** — before running it against **Compose** Postgres, ensure **§C1** is in **`.env.local`** (later wins after sourcing `.env` then `.env.local` with **`set -a`**) **or** override in the invoking shell. Hosts must be **`127.0.0.1`**, Kafka **`localhost:29092`**, **`SSLMODE=disable`** — **never** Compose DNS names (**`postgres`**, **`kafka:9092`**, **`clickhouse:9000`**) from macOS/Linux host processes.

2. **`Makefile` targets that only `docker compose exec` into Postgres/ClickHouse/Kafka** (this repo typically includes **`migrate-postgres`**, **`migrate-clickhouse`**, **`seed-db`**, **`init-kafka`**) execute **inside** Compose with the **Compose image defaults** (**`flexprice` / `flexprice123`**, exec via service names). Those are **implicitly local** as long as the stack is **`docker compose up`** — they **ignore** stray **`FLEXPRICE_POSTGRES_*`** in `.env` for the **`psql` / `clickhouse-client`** invocation (still harmless to keep **§C1** in `.env.local`).

3. **Before `migrate-ent` or `run-local-*`**, run **`rg`** probes from **`§B`**. If **`FLEXPRICE_POSTGRES_HOST`** resolves to **`postgres`** (Docker-only hostname) **or any non-local host** while intending **Compose-backed local DB**, **fix `.env.local` first (**§C1**)** — do not rely on `.env` alone.**

4. **Pattern for shell one-liners when any step uses host Go + env files:**

   ```bash
   set -a && [ -f .env ] && . ./.env; [ -f .env.local ] && . ./.env.local; set +a && make migrate-ent
   ```

   (Use **`migrate-postgres`** / **`migrate-clickhouse`** / **`seed-db`** as written in the **`Makefile`**; those targets use Compose exec against the container. Add **§C1** overrides in **`.env.local`** whenever you combine with **`migrate-ent`** or app servers in the same session.)

5. **When Postgres/ClickHouse/Temporal are intentionally RDS/managed (`§C0`)**, **`§L` does not apply** — migrations and binaries must target **those** **`FLEXPRICE_*`** values and **never** **`§C1`** unless explicitly requested.

---

## **`§M`** — Effort gauge & Cursor model tier (dynamic)

Before (and **while**) running **`devenv`**, **estimate effort** so the session uses the lightest sensible model—and **rescales upward** when evidence shows deeper work.

### Workload tiers (snap once, revise after failures)

| Tier | Signal | Typical devenv examples |
|------|--------|-------------------------|
| **S · light** | Single known-good path, cookbook steps, shallow logs | `docker info`, `docker compose ps`, `pg_isready`, one-shot `curl` health, **`rg`** **`§B`** read-only probes, confirm **`§C1`** snippets exist |
| **M · medium** | Few services in dependency order; `.env`/Compose nuance (**`§L`**, **`migrate-ent`** vs Compose-exec **`migrate-*`**) | **§E1** converge + migrations + **`seed-db`**, topic prompt (**§0.K**), consumer-group tweaks (**§D**), **`run-local-*`** sanity |
| **L · heavy** | Ambiguous topology (RDS + Compose + Temporal split), flaky cross-network (VPN/IP allowlists), obscure migration/offset/topic interplay, **`§F`** loops without convergence across several rounds | Debugging “host vs container DNS”, wrong-cluster risk, Temporal visibility DB vs app Postgres divergence, **`§C0`** stacks |

Escalate **S → M → L** as soon as a step fails unexpectedly or stakes rise (anything that might touch **production-adjacent** creds ⇒ treat as **≥ M**, often **L**).

### Matching **Cursor Agent / Composer** models (names drift by product UI)

Treat “model” labels as **capability tiers**, not exact marketing strings (`Settings` → **`Models`** in Cursor picks the SKU).

| Tier goal | Typical pick | Reserve for |
|-----------|---------------|-------------|
| **Fast / economical** | **Composer** (Fast) — or your **default fast** slot | **`S`** checklists only |
| **Balanced** | **Composer**, **GPT-5-class**, or baseline **Sonnet** | **`M`** most sessions |
| **Strongest reasoning (ceiling)** | **Claude Sonnet 4.x** — **`Sonnet 4.7`** (or newer in the **4.x** line) **when Cursor lists it as the strongest Sonnet tier**; **Opus** / **thinking** variants **if heavier than Sonnet per UI** | **`L`** only — multi-hypothesis infra, RDS/staging bleed, Temporal + Kafka + env correlation |

Rules:

1. **Start at `S` staffing** (`fast`) if user said “bring stack up” and shape is Compose-only + **§L** locked.
2. **Jump to balanced immediately** whenever editing **`.env.local`**, interpreting **`migrate-ent`** errors, or coordinating **Kafka + Temporal + Postgres**.
3. **Tier `L` (strongest reasoning):** Prefer **Claude Sonnet 4.x** capped at **`Sonnet 4.7`** when that is the strongest Sonnet in **Cursor Settings → Models**; switch to **Opus** / **thinking-heavy** tiers only if UI marks them heavier than Sonnet. Use **`L`** for hybrid RDS/managed, security-group/VPN uncertainty, contradictory signals after **`§B`** probes, or explicit risk review before commands.
4. **Subagents & parallelism:** follow **`§N`** — fork **Cursor Task** subagents when workloads are independent; keep **cheap** tiers on scripted subagents and **tier `L`** on parent for integration / contradictions (**§N**).
5. **Never burn top tier** on rote **`docker compose ps`** without a blocker—**rescope down** once green.

Tell the human when **tier bump** pays off (“open a new Composer with **Sonnet 4.7** for RDS + Temporal triage”). If the Cursor UI exposes **automatic** routing, brief them on **tier target** (**S/M/L**) so presets align.

---

## **`§N`** — Subagents (Task tool) & safe parallelism

Use **Cursor subagents** (the **Task** tool: **`generalPurpose`**, **`explore`**, **`shell`**, etc.) when **work splits cleanly** — better wall-clock latency and sharper focus **if** prompts are complete (subagents do **not** see prior chat turns).

### When to deploy subagents (**do** parallelize here)

Launch **multiple subagents in one message** (same round) **only when** branches are **orthogonal**:

| Parallel tracks | Typical prompt shape |
|-----------------|----------------------|
| **§F probes** split | One agent: Postgres **`pg_isready`** + Compose **`postgres`** logs tail hint; another: **`kafka-topics --list`**; another: **`curl` ClickHouse `:8123/ping`** / HTTP; fourth: Temporal UI / **`7233`** reachability (**read-only** probes). Merge in parent → single green/red matrix |
| **Read-only codebase + ops** | **`explore`**: **`grep`/find** how **`migrate-ent`** or **`FLEXPRICE_*`** bind in **`cmd/migrate`** + **`internal/config`**; sibling **`explore`** or **`shell`**: `docker compose ps` + image pull progress (if **heavy**, **`shell`** + **`run_in_background: true`** for long pulls |
| **`L`‑tier investigations** | Parent stays **Sonnet ceiling** (**§M**); children run **cheap** **`explore`/`shell`** for evidence gathering (**no** rewriting **`.env.local`** concurrently from two writers — serialize edits) |

**Background long jobs:** **`docker compose up`**, image pulls, **`make migrate-*`** waits → **`shell`** **`run_in_background: true`**; parent continues planning or launches **other** probes that do not contend on Docker lock.

### When **not** to parallelize (serialization required)

- **Stateful env:** one owner edits **`.env` / `.env.local`** (**§L**); subagents report **recommended lines**, parent applies **once**.
- **Order-sensitive DB:** **`migrate-postgres`** / **`migrate-clickhouse`** **`→`** **`migrate-ent`** **`→`** **`seed-db`** (**§L**, **§E1**) unless Makefile explicitly proves independence — assume **pipeline order**.
- **Single Docker daemon storms:** avoid **four** simultaneous **`docker compose build --no-cache`**; cap concurrent heavy Compose mutations.
- **Kafka topic creation (**§0.K**):** one decision-maker — **never** duplicate **`make init-kafka`** against shared clusters.

### Prompt hygiene for Task subagents

Each subagent **`prompt`** must embed: **repo path**, **intent** (“read‑only probe” vs “run X if safe”), **exit criteria** (“return stdout + last exit code”), **forbidden actions** (**no secrets in reply**, **no `init-kafka` without YES**). Use **`readonly: true`** for **`explore`** when edits are out of scope.

### Parent-agent merge checklist

After parallel returns: reconcile **contradictions** (**Postgres UP** but **`migrate-ent`** DSN mismatch ⇒ **`§L`/`§B`**), bump **`§M`** tier (`L`) if triage spikes, avoid **double-counting** success from duplicate probes.

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

**Local-first migrations & services:** **`§L`** (**host Go → always §C1 in `.env.local`** when Postgres is Compose on localhost).

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

**Prerequisite (**`§L`**): `.env.local` must provide **Compose-on-host** Postgres/ClickHouse (and Temporal if needed for commands) **`§C1`** so **`make migrate-*` / `seed-db`** hit **local** containers, **not** any staging/RDS values left in `.env`.

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

**Parallel probing:** In **`§M`** tiers **`M`/`L`**, when checklist branches are independent, fan out **`§F`** probes through **`§N`** (multiple read‑only **`shell`** / **`explore`** subagents)—merge outcomes in parent **before** **`docker compose`**, migrations, or **`.env.local`** edits mutate state.

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

**Escalate** after **`MAX_ROUNDS`**: summarize last errors; **do not infinite loop**. If probes keep failing with mixed signals ⇒ bump **`§M`** tier (**strongest Sonnet 4.x / `Sonnet 4.7` when available**) and reassess stack assumptions (**RDS vs Compose vs `.env`**).

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
- Run **`migrate-postgres`**, **`migrate-clickhouse`**, **`migrate-ent`**, **`seed-db`**, or **`run-local-*`** assuming `.env` is local—**enforce `§L` + `.env.local` (`§C1`)** whenever Compose Postgres/CH is meant to receive changes.
- Use **`kafka:9092`** from **macOS Go binary**.
- **`make swagger-3-0`** without network (**converter.swagger.io**).
- Insist on **§E1 full infra** when the user already said **Postgres/Kafka/etc. lives in RDS or managed** (`devenv` = **their** `.env` truth + **minimal** Compose).

---

## Related skills

- [`compose`](compose/SKILL.md) — short Docker/Makefile cheat sheet  
- [`godev`](godev/SKILL.md) — `go test` after server lives  
- [`apitest`](apitest/SKILL.md) — master **`curl`** QA after infra GREEN  
- **Superpowers** **`dispatching-parallel-agents`** — general parallel agent patterns; aligns with **`§N`** when spawning multiple Task subagents