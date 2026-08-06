---
name: arch
description: >-
  FlexPrice repo architecture — docs/ layering, deps, hotspots, flows, Graphify. Say arch,
  repo architecture, FLEXPRICE structure, onboarding codebase.
---

# **`arch`** — repository architecture

Use when navigating **`cmd/`**, **`internal/`**, **`ent/`** (billing, metering, Temporal, Kafka, integrations).

**Other repos:** **`repo-architecture-intelligence`** (`~/.cursor/skills/repo-architecture-intelligence/SKILL.md`).

**Sibling skills:** [README](../README.md) (**`apitest`**, **`devenv`**, **`godev`**, **`openapi`**, **`pr`**, **`gh`**, **`compose`**).

## Canonical documentation map

| Doc | Purpose |
| --- | ------- |
| `docs/REPO_MAP.md` | Directory census, deployment modes, major systems |
| `docs/ARCHITECTURE.md` | Principles, infra choices, layering, conventions |
| `docs/DEPENDENCY_GRAPH.md` | Fan-in/out, messaging DAG, coupling notes |
| `docs/HOTSPOTS.md` | Large files & structural risks |
| `docs/FLOWS/*.md` | End-to-end narratives for critical paths |

**Rule:** Prefer these artifacts over improvised mental models. If docs drift from code during your change, patch the relevant markdown.

## How to analyze architecture quickly

1. **Identify deployment surface** — `cmd/server/main.go` + `deployment.mode`. Determine whether edits affect API-only paths, Kafka consumers, or Temporal workers (`registerRouterHandlers` & `startTemporalWorker` branches).
2. **Locate the bounded context** — map feature → domain package (`internal/domain/<ctx>`) → repository impl (`repository/ent` vs `repository/clickhouse`) → service (`internal/ee/service/<file>`) → handler (`internal/api/v1`).
3. **Check async edges** — list Kafka topics touched (search config keys under `internal/config`) and Temporal workflow names (`internal/temporal/workflows`).
4. **Assess coupling** — if you need half of `ServiceParams`, consider narrowing new code’s dependencies deliberately.

## How to reason about dependencies

- Follow **canonical direction**: handlers → services → (domain interfaces + repos/integrations) → storage/IO (see diagram in `docs/DEPENDENCY_GRAPH.md`).
- Watch **logical cycles**: Temporal registration reconstructs some services independently of Fx (`internal/temporal/registration.go`).
- Question **globals**: prefer replacing new uses of `GetGlobalTemporalService` with injected interfaces when editing those call sites safely.

## How to persist repository memory

After meaningful structural edits:

| Change type | Update |
| ----------- | ------- |
| New package / major subsystem | `REPO_MAP.md` (+ `ARCHITECTURE.md` boundaries if philosophical) |
| New topic/consumer or coupling | `DEPENDENCY_GRAPH.md` + YAML pointer |
| Emerging risk/god file growth | `HOTSPOTS.md` entry |
| Flow behavior change | Relevant `docs/FLOWS/<name>.md` |

Treat docs as durable context for future automation and onboarding.

## How to refactor safely

1. Freeze scope — isolate one bounded context or one pipeline stage.
2. Add or extend characterization tests closest to seams you cut (heavy services deserve targeted tests despite size).
3. Avoid splitting multi-thousand-line files blindly — extract cohesive types/helpers with stable interfaces (`subscription.go`, `invoice.go`, `billing.go`).
4. For schema changes — always `make generate-ent` + deliberate migration rollout; annotate operational risk if backfill jobs required.
5. For messaging — verify consumer group uniqueness and DLQ configuration when adjusting handler semantics (`internal/pubsub/router`).

## Anti-patterns to flag during review

- Domain importing repositories or Gin types.
- Handlers querying Ent directly.
- Silent topic renames without consumer coordination.
- New global singletons hiding dependencies.
