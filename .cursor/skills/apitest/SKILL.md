---
name: apitest
description: >-
  Master HTTP QA: chains devenv then interactive API key / X-Environment-ID then real curls.
  Shards suites to parallel workers. Trigger: apitest, api test, curl localhost, QA local API.
disable-model-invocation: false
---

# **`apitest`** — API QA (master)

This skill acts as **`master`** in a **master / worker** pattern for large workloads: it **plans**, **sequences**, **holds secrets policy**, **merges outcomes**. Delegate **narrow, independent** slabs to **`worker`** runs (parallel Cursor agents or separate chat tasks) — never fork without a stable **BASE_URL**, **mode**, and **credential contract**.

Always compose **[`devenv`](../devenv/SKILL.md)** for **infra + deployment mode** before HTTP unless endpoints are confirmed up.

---

## 0. Prerequisites (delegation)

1. **`devenv` phase** — user intent (host API / consumer / temporal_worker / local / full compose), `.env*` hygiene, **`MAX_ROUNDS`** verify loop GREEN.
2. **Base URL** — default **`http://127.0.0.1:8080`**; path prefix **`/v1`** (see `CLAUDE.md`/`AGENTS.md` — no trailing slash on base).
3. **Trust model** — user runs commands on **their** machine only; orchestrator prefers **stdin env** injection for curls **not logging raw secrets**.

---

## 1. Interactive credential ritual (blocking questions)

Ask **explicitly**, one bundle at a time:

| # | Prompt | Stored as (concept) |
|---|--------|---------------------|
| 1 | API key value for **`x-api-key`** header (often local `sk_local_flexprice_test_key`)? | **`$FLEXPRICE_TEST_API_KEY`** |
| 2 | **`X-Environment-ID`** GUID if authenticated routes scope to an environment (`internal/types/header.go`)? | **`$FLEXPRICE_TEST_ENV_ID`** |
| 3 | Any bearer JWT flow instead/in addition? Rare for pure api-key dashboards. | **`$AUTH_HEADER`** literal |
| 4 | Alternate **HOST** (**staging**/tunnel)? | **`$BASE_URL`** override |

**Operational rule:** Prefer shell:

```bash
read -rs FLEXPRICE_TEST_API_KEY && export FLEXPRICE_TEST_API_KEY && export BASE_URL=http://127.0.0.1:8080 && …
```

**Never** paste full keys back in assistant prose — confirm **present/absent**/length fingerprint only.

---

## 2. Minimal curl toolchain

```bash
# Health (normally unauthenticated outer router — GET /health is root router; swagger says /v1 base for API)
curl -sfS "${BASE_URL:-http://127.0.0.1:8080}/health" | head -c 200 || echo FAIL

# Private v1 example skeleton
curl -sfS "${BASE_URL:-http://127.0.0.1:8080}/v1/some/route" \
  -H "x-api-key: ${FLEXPRICE_TEST_API_KEY}" \
  ${FLEXPRICE_TEST_ENV_ID:+-H "X-Environment-ID: ${FLEXPRICE_TEST_ENV_ID}" } \
  -H "Accept: application/json"
```

Adapt routes from **`docs/swagger/`** (`swagger-3-0.json` / handler paths in `internal/api/router.go`).

Verb matrix: **`-f`** curls fail on **`4xx`/`5xx`** — toggle **`-w '\n%{http_code}\n'`** when debugging semantics.

Large bodies: **`--data-binary @file.json`** plus **`-H 'Content-Type: application/json'`**.

---

## 3. Standard orchestration playbook (deterministic phases)

Tick mentally as **Orchestrator**:

```
[A] Requirement intake — verbs (GET invoice? POST event?), tenancy surface, destructive vs readonly
[B] Compose **devenv** — mode + infra + verify loop GREEN
[C] Credential interactive gather — §1 vars exported in user shell orchestrator echoes
[D] Smoke ladder — health → simplest GET → progressively complex POST
[E] Record actual HTTP codes + trimmed JSON excerpts (truncate >2KB bodies)
[F] Tear-down optional — advise user Ctrl-C server / docker stop if ephemeral
```

**Consumer mode caveat:** **`consumer` / ingestion-only modes** expose **few/no HTTP tests** meaningful — steer user to **`api`** or **`local`** for REST probes while consumer runs sibling process.

---

## 4. Master / worker decomposition (massive workflows)

**When splitting is justified:**

- **`> ~8`** independent endpoints without shared mutable side-effect ordering.
- Geography across **Swagger tags** (**Customers**, **Invoices**, **Events**, …).

**Orchestrator tasks:**

| Task | Responsibility |
|------|----------------|
| Publish **immutable contract snippet** snippet to workers: `BASE_URL`, header list (names only — keys via env mirror), forbid schema drift |
| Freeze **ordering** dependencies (eg create-customer-before-subscription DAG) |
| Merge JSON **pass/fail** report table |

**Worker tasks:**

| Task | Responsibility |
|------|----------------|
| Single tag / subdomain file list bounded |
| curls only — **readonly** repos if parallel (`readonly` Cursor agent mode preferred) |
| Return compact stdout block + stray stderr |

**Mechanism hints (implementation-agnostic wording for Cursor stacks):**

- Use **multiple parallel agent runs** (**background tasks**) when product UI supports partitioning — **isolate prompts** — **avoid shared file edits concurrently** on identical paths.
- If only single agent lane: sequential micro-batches respecting DAG.

Workers **pull** infra state from **`devenv` completion note** (**BASE_URL GREEN**).

Anti-pattern:** workers independently restart docker / rewrite `.env` — **Orchestrator only**.

---

## 5. Automated depth tiers

| Tier | Scope | Typical duration cue |
|------|-------|----------------------|
| **T0 Smoke** | `/health`, one cheap GET **`/v1/...`** | minutes |
| **T1 Tagged** | All GET list endpoints user lists | tens of minutes sequential |
| **T2 Mutation** | Creates + compensating deletes/idempotency | document cleanup |
| **T3 Replay** | Event pipelines / delayed ClickHouse eventual — mark **observe async** |

---

## 6. Failure triage playbook

| Observation | Hypothesis ladder |
|-------------|-------------------|
| `401` / `403` | key / rbac / missing env header / provider mismatch (**`.env`** docker vs host hosts) |
| `404` route | Forgot **`/v1`** prefix vs mount |
| hangs | infra not READY — rerun **`devenv` §Verification** |
| `5xx` | logs `docker compose logs flexprice-api` hybrid OR stdout process |

Attach **Correlation:** request IDs from logging middleware (`internal/rest/middleware`).

---

## 7. Combining with Temporal / queues

Pure HTTP probes **cannot finalize** Metering completeness — annotate **defer validation** (**ClickHouse**/dashboard) referencing **`docs/FLOWS/event-processing.md`**.

Temporal heavy flows: optionally poll **Temporal UI** human step — mark **automate-later**.

---

## 8. Forbidden actions

- Storing echoed secrets in markdown docs / repo commits.
- **Production** curls without explicit dual opt-in (**user typed target** twice).
- **Bypassing RBAC intentional** brute force — escalate with user instead.

---

## Cross-links

| Role | Skill |
|------|-------|
| Bring-up / env | **[`devenv`](../devenv/SKILL.md)** |
| `go test` | **`godev`** |
| Architecture | **`arch`** + `docs/FLOWS/*` |

The **master orchestrator stance:** keep this file **thin on endpoint enumeration** (**Swagger is source**) — prioritize **interaction contract + safety + parallelism rules**.
