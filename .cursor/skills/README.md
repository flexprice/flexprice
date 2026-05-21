# Cursor skills for this repository

Skills are loaded from **`.cursor/skills/<name>/SKILL.md`** (project scope). Personal skills can also live in **`~/.cursor/skills/`** (all projects). Some Cursor builds also read **`.agents/skills/`** — use one layout consistently.

## Bundled here (FlexPrice-focused)

| Skill | When to use |
| ----- | ----------- |
| [`architect`](architect/SKILL.md) | Layering, `docs/*` architecture memory, Graphify + cross-repo `repo-architecture-intelligence` |
| [`go-dev-loop`](go-dev-loop/SKILL.md) | Daily Go loop: `gofmt`, `go vet`, targeted tests, `make test` |
| [`docker-dev-infra`](docker-dev-infra/SKILL.md) | Compose / `make dev-setup`, local URLs, container logs |
| [`flexprice-openapi-sdk`](flexprice-openapi-sdk/SKILL.md) | `make swagger`, SDK/MCP regen, `api/custom/` merge |
| [`pr-self-review`](pr-self-review/SKILL.md) | Ship checklist before opening a PR (security, tests, Ent, API surface) |
| [`gh-workflow`](gh-workflow/SKILL.md) | `gh` for PRs, checks, issues — aligns with team `gh` usage |

Invoke in Agent chat by naming the skill (e.g. *use **go-dev-loop***) or your client’s `@skill` / skill picker.

## Optional: import popular community skills

Public catalogs (CC0 or open licenses; review each repo before vendoring):

- **Curated lists:** search for **`awesome-agent-skills`** / **AgentSkills** on GitHub.
- Typical useful categories for backend teams:** `github-cli`**, **`docker` / `docker-compose`**, **`skill-authoring`**, **`plan`**, notebooks/research helpers.

**How to import one skill**

1. Pick a repo and a single skill folder (must contain `SKILL.md`).
2. Copy **only that folder** into `.cursor/skills/<skill-name>/` (preserve `SKILL.md` + any `scripts/` / `reference.md`).
3. Edit `SKILL.md` if paths or commands assume another stack; for FlexPrice prefer `Makefile` targets from **`AGENTS.md`** / **`CLAUDE.md`**.
4. Extend this README table so teammates discover it.

Avoid duplicating skills you already have globally in `~/.cursor/skills/` — project skills override or stack depending on Cursor version; keep one canonical copy to reduce drift.
