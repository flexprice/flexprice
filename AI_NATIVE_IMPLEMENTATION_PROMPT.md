# Claude Code Kickoff Brief — AI-Native Context & Spec System for Flexprice

> Paste this entire file into Claude Code as the opening message, or run `claude` from the repo
> root and say: "Read AI_NATIVE_IMPLEMENTATION_PROMPT.md and begin Phase 0."
> Execute **one phase at a time**. Stop at each `CHECKPOINT` and wait for my approval before continuing.

---

## 0. Your role and the goal

You are a principal engineer setting up an **AI-native development system** across three repos:

- `flexprice/` — Go 1.23 backend (Gin, Uber FX, Ent, Postgres + ClickHouse, Kafka, Temporal)
- `flexprice-front/` — React 18 + TypeScript (Vite, Tailwind, Radix, Zustand, TanStack Query)
- `flexprice-docs/` — Mintlify MDX docs

The goal is a durable system where **the spec is the source of truth and code is a regenerable output**, and where **context is engineered, layered, and incrementally maintained** so any AI tool (Claude Code, Cursor, Codex) gets the right context with minimal reading.

This is **not** a one-shot generation. Build it incrementally, verify each step, and keep every generated artifact small and trustworthy. Confidently-wrong context is worse than no context.

---

## 1. The architecture you are implementing

### Three planes of context

1. **Constitution (always loaded, rarely changes)** — root `AGENTS.md` per repo. Invariants only: the rules that must NEVER be violated. Keep to ~80 lines. Do **not** `@import` layer files into it (imports expand at launch and would bloat every session).

2. **Context map (on-demand, follows the code)** — one `AGENTS.md` per layer, physically co-located in that directory so it loads lazily only when work happens in that subtree.

3. **Specs (per-feature, executable)** — a `specs/<feature>/` folder per feature with `spec.md`, `plan.md`, `tasks.md`, `verification.md`.

### Cross-tool canonical = AGENTS.md

`AGENTS.md` is the de-facto standard read by Codex and others. Make it canonical and point each tool's native mechanism at it:

- **Claude Code**: in each directory that has an `AGENTS.md`, create a `CLAUDE.md` whose entire content is `@AGENTS.md` (single-line import), OR a symlink. Use the `@AGENTS.md` import approach (portable, no symlink issues).
- **Cursor**: generate a thin `.cursor/rules/<layer>.mdc` with `globs:` frontmatter pointing at the layer's path, referencing the same `AGENTS.md`.
- Never duplicate content. Each layer's knowledge lives in exactly one `AGENTS.md`.

### Provenance engine (incremental sync, keyed on git SHA)

Every context file carries frontmatter declaring what it owns and when it last synced. A tool-agnostic skill regenerates only the files whose owned paths changed since their watermark. This is the core efficiency mechanism — never re-traverse unchanged code.

---

## 2. File conventions (use these exactly)

### Context file frontmatter (every layer `AGENTS.md`)

```yaml
---
layer: handler
owns:
  - "internal/api/v1/**"
synced_sha: <git sha this file was last reconciled to>
synced_at: <ISO 8601>
---
```

Body sections (keep each terse — "what a senior needs before touching this dir"):
`Purpose` · `Key files & entry points` · `Patterns to follow` · `Invariants (must hold)` · `Common pitfalls` · `Related layers`.

### Root constitution `AGENTS.md` (per repo)

Invariants only. For the backend, seed from `CLAUDE.md` but distill to hard rules:
multi-tenancy + environment scoping on every entity; idempotency + event-ordering for event processing; no business logic in handlers (service layer mandatory); repository pattern (interfaces in domain, impls in repository); structured logging + context propagation; ClickHouse 90 GB per-query memory cap; unit tests for business logic.

### Spec folder (per feature)

```
specs/<feature>/
  spec.md          # EARS-style acceptance criteria + non-requirements + the 5 billing failure modes
  plan.md          # architecture, affected modules, risks, derived_from: <constitution sha>
  tasks.md         # ordered, bounded tasks (one task = one implementable unit)
  verification.md  # test plan mapping each EARS criterion to a test; derived_from: <spec hash>
```

EARS notation for criteria: `WHEN <condition> THE SYSTEM SHALL <behavior>`.
Mandatory failure-mode coverage for any billing/event feature: **idempotency, event ordering, retries, tenant isolation, backfill**. If a criterion can't state how it's verified, the spec is not done.

### Sync manifest

```yaml
# .context/manifest.yaml
nodes:
  - file: internal/api/v1/AGENTS.md
    owns: ["internal/api/v1/**"]
    synced_sha: <sha>
```

---

## 3. The tool-agnostic sync skill

Create `.context/sync/` containing a script (Go or Python, your call — must run with no proprietary deps) plus a `SKILL.md` describing it so Claude/Cursor/Codex can all invoke it. Algorithm:

1. Read `.context/manifest.yaml`.
2. For each node: `changed = git diff --name-only <synced_sha>..HEAD`, intersect with `owns` globs.
3. If empty → bump `synced_sha` to HEAD, no LLM call.
4. If non-empty → emit a regeneration task containing **only** the node's current `AGENTS.md` + the scoped diff (`git diff <synced_sha>..HEAD -- <owned paths>`). The LLM updates the file minimally, then `synced_sha` advances.
5. Output is a branch/PR, never a silent overwrite.

The script itself does the git math and prepares the prompt bundle; the LLM (whichever tool runs it) does the prose update. Make the staleness check runnable standalone (exit non-zero if any node is behind HEAD) so it can be a CI gate.

---

## 4. Guardrail hooks (CI + git hooks)

Implement as repo scripts wired into CI on merge-to-main and a pre-commit/pre-push hook:

- **Format/lint/test**: `go vet ./...`, `gofmt -l`, `eslint`, targeted test subset.
- **Constitution compliance**: diff-level check for obvious invariant violations (e.g. business logic added under `internal/api/v1/`).
- **Context staleness**: run the sync skill's staleness check; fail if an `owns` glob changed without a context update.
- **Spec coverage**: for feature branches, fail if an EARS criterion has no mapped test in `verification.md`.

---

## 5. Verification strategy (billing-specific — do not skip)

Promote the existing recon/validation skills into the verify gate as a **replay harness**: recompute invoices against real event streams and diff totals against finalized invoices; golden-master tests on pricing math so refactors can't silently move a number. The EARS criteria from each spec must become these tests.

---

## 6. Execution plan — work phase by phase, STOP at each checkpoint

### Phase 0 — Constitutions (start here)
- Create root `AGENTS.md` for all three repos (invariants only, ~80 lines each).
- In each repo root, add `CLAUDE.md` containing only `@AGENTS.md`.
- Add starter `.cursor/rules/constitution.mdc` (alwaysApply) referencing the root `AGENTS.md`.
- Do NOT touch layer files yet.
- **CHECKPOINT 0**: show me the three constitutions and wait.

### Phase 1 — Pilot one subsystem (invoice reprocessing)
- Identify the 3–4 backend directories the invoice/compute path touches (use Glob/Grep; cross-check against `internal/service`, `internal/api/v1`, `internal/repository`, `internal/temporal`).
- Write a Plane-2 `AGENTS.md` (with frontmatter) for each, plus the `@AGENTS.md` `CLAUDE.md` and `.cursor/rules/*.mdc` mirrors.
- Write `specs/invoice-reprocessing/` (all four files) for a real, small reprocessing improvement — EARS criteria, the 5 failure modes, test mapping.
- **CHECKPOINT 1**: show me the pilot context + spec and wait.

### Phase 2 — Sync skill + manifest
- Build `.context/manifest.yaml` (register the Phase-1 nodes) and `.context/sync/` (script + SKILL.md).
- Add the merge-to-main CI job that advances watermarks and the standalone staleness check.
- Demonstrate it: make a trivial change under a registered glob, run the skill, show the regenerated context PR.
- **CHECKPOINT 2**.

### Phase 3 — Guardrail hooks
- Implement the four gates from §4 as scripts + CI wiring + a pre-push hook.
- **CHECKPOINT 3**.

### Phase 4 — Extend to frontend + docs
- Roll Plane-2 `AGENTS.md` across `flexprice-front` (`src/api`, `src/components`, `src/pages`, `src/hooks`) and `flexprice-docs`.
- Wire the `docs` changelog skill so release notes derive from merged specs, not re-discovered from commits.
- Run one full-stack feature spec → implement → verify → sync → docs end to end.
- **CHECKPOINT 4**.

### Phase 5 — Replay harness + golden masters
- Promote recon/invoice-validation into the verify gate; add golden-master pricing tests.
- **CHECKPOINT 5**.

---

## 7. Hard rules while you work

- Small files. Cap each `AGENTS.md` at what a senior needs before touching that directory; if it exceeds ~120 lines, split or trim.
- Keep description (how it works) separate from findings (what's wrong). Improvement notes go in `.context/findings/<dir>.md` dated, never inside the context file.
- Every context/spec artifact is PR-reviewed, never silently overwritten.
- Prefer agentic search (Glob/Grep/Read) to map the codebase; cite the actual files you base each context file on.
- After any structural change, update the manifest and advance watermarks.
- Verify each phase: run the relevant lint/test/staleness checks before declaring a checkpoint done.
- When unsure about scope or an invariant, ask me rather than guess.

## 8. First action

Begin **Phase 0**. Start by reading both `CLAUDE.md` files (backend + frontend) and the docs repo structure, then draft the three root constitutions. Stop at CHECKPOINT 0.
