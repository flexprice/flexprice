# Cursor skills for this repository

Skills load from **`.cursor/skills/<folder>/SKILL.md`**. Cursor matches the YAML **`name`** field plus folder name — short names keep chat triggers easy (*use **apitest*** / *run **devenv***).

## Skills (short name → folder)

| Say / `name` | Folder | Purpose |
| ------------ | ------ | ------- |
| **`arch`** | [`arch`](arch/SKILL.md) | Layering, `docs/*`, hotspots, flows, Graphify; global skill `repo-architecture-intelligence` |
| **`godev`** | [`godev`](godev/SKILL.md) | fmt, vet, `go test` / `make test` |
| **`devenv`** | [`devenv`](devenv/SKILL.md) | Hybrid env wizard: `.env*` for RDS/managed backends; Compose only what’s local; modes, consumer groups, verify loop |
| **`compose`** | [`compose`](compose/SKILL.md) | Docker / `make dev-setup` shorthand; deep ⇒ **`devenv`** |
| **`openapi`** | [`openapi`](openapi/SKILL.md) | `make swagger`, SDK/MCP, `api/custom/` |
| **`apitest`** | [`apitest`](apitest/SKILL.md) | Master HTTP QA: **`devenv`** + interactive creds + real **`curl`**, shard workers |
| **`pr`** | [`pr`](pr/SKILL.md) | Pre-merge self-review checklist |
| **`gh`** | [`gh`](gh/SKILL.md) | GitHub CLI for PRs, checks, issues |

Invoke: *use **arch***, *use **devenv** wizard*, `@` skill picker — same idea.

**Big workflows:** **`apitest`** (master) → **`devenv`** + **`compose`** (infra) → **`godev`** (unit tests) → **`arch`** + `docs/FLOWS/` (truth). Workers run curl partitions; **one** orchestrator edits `.env*` / Compose.

**Personal skills:** `~/.cursor/skills/` (e.g. `repo-architecture-intelligence`).

## Optional: import community skills

Pick one folder from an **awesome-agent-skills** / **AgentSkills** style repo → copy under **`.cursor/skills/<folder>/`** — trim Makefile paths for FlexPrice (**`AGENTS.md`**).
