---
name: context-sync
description: "Sync AGENTS.md context files with the current codebase. Use whenever you want to check if context is stale, update context after code changes, or regenerate a specific layer's AGENTS.md. Triggers on: 'sync context', 'update context', 'is context stale', 'refresh AGENTS.md', 'context out of date', 'update the context files', or any time code has changed and you want the AGENTS.md files to reflect it."
---

# Context Sync Skill

Keeps all Plane-2 `AGENTS.md` files in sync with the codebase using git-SHA watermarks.
Run this any time after code changes to check for and fix stale context.

## What this skill does

1. Reads `.context/manifest.yaml` — the registry of all context nodes and their `synced_sha`.
2. For each node: diffs `synced_sha..HEAD` and intersects with that node's `owns` globs.
3. Nodes with no owned changes → SHA bumped automatically (no LLM work needed).
4. Nodes with owned changes → prints the current `AGENTS.md` + scoped diff as a prompt bundle for the LLM to update minimally.
5. After updates, advances `synced_sha` in the manifest.

## How to run

### Step 1 — Check what's stale
```bash
cd /path/to/flexprice  # repo root
pip install pyyaml --break-system-packages -q
python3 .context/sync/sync.py --check
```
Exits 0 if all current. Exits 1 and lists stale nodes if any are behind HEAD.

### Step 2 — Sync (generate update bundles)
```bash
python3 .context/sync/sync.py --sync
```
- Nodes with no owned changes: SHA bumped automatically, no output.
- Nodes with changes: prints a prompt bundle (current AGENTS.md + scoped diff + instructions).

### Step 3 — Update each stale AGENTS.md
For each printed bundle, the LLM (this session) reads the bundle and produces an updated `AGENTS.md`.
Rules for the update:
- Update "Key files" table if files were added, removed, or renamed.
- Update "Patterns" or "Common pitfalls" if the diff reveals new patterns.
- Do NOT rewrite sections that haven't changed.
- Do NOT add improvement suggestions — those go in `.context/findings/<layer>.md`.
- Bump `synced_sha` to current HEAD and `synced_at` to now in frontmatter.

### Step 4 — Advance the manifest
After saving each updated file:
```bash
python3 .context/sync/sync.py --advance --file internal/service/AGENTS.md
# repeat for each updated file
```

### Step 5 — Verify
```bash
python3 .context/sync/sync.py --check
# Should print: All context nodes are current.
```

## Sync a single node only
```bash
python3 .context/sync/sync.py --sync --file internal/service/AGENTS.md
python3 .context/sync/sync.py --advance --file internal/service/AGENTS.md
```

## Adding a new layer to the registry
When you create a new `AGENTS.md` for a directory, register it:
```yaml
# .context/manifest.yaml — add a new node:
- file: internal/repository/AGENTS.md
  owns:
    - "internal/repository/**"
  synced_sha: <current HEAD SHA from: git rev-parse HEAD>
  synced_at: <ISO 8601 now>
```

## What NOT to do
- Do not rewrite the entire AGENTS.md on every sync — minimal diffs only.
- Do not add critique, TODOs, or "areas to improve" inside AGENTS.md — put those in `.context/findings/`.
- Do not run `--sync` and skip `--advance` — the manifest won't update and the node stays stale.

## Full example session
```
You: sync context
Claude:
  → runs python3 .context/sync/sync.py --check
  → finds internal/service/AGENTS.md is stale (3 files changed)
  → runs --sync, reads the prompt bundle
  → produces updated internal/service/AGENTS.md (minimal changes only)
  → runs --advance --file internal/service/AGENTS.md
  → runs --check again → "All context nodes are current."
```
