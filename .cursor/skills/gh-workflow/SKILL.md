---
name: gh-workflow
description: >-
  Uses GitHub CLI (gh) for PRs, checks, and issues in FlexPrice repos. Use when opening or
  updating PRs, triaging failing checks, reading issue context, or when the user mentions gh,
  GitHub Actions, pull request workflow.
---

# GitHub CLI workflow (FlexPrice)

## Prerequisites

- **`gh`** installed and **`gh auth login`** completed.
- Respect user rules:** do not change **`git config`**; avoid destructive git unless explicitly requested.

## Before `gh pr create`

Per user workflow preference:

1. **`git status`**, **`git diff`**, branch vs base tracking, **`git log`** / **`git diff base...HEAD`** for full branch story.
2. Draft title + body (summary, test plan).
3. Push: **`git push -u origin HEAD`** when needed (network).

## Create PR

```bash
gh pr create --title "..." --body "$(cat <<'EOF'
## Summary
- ...

## Test plan
- [ ] ...
EOF
)"
```

Use **`gh pr view --web`** to open in browser.

## Checks & failures

```bash
gh pr checks
gh run list --limit 5
gh run view <run-id> --log-failed
```

For a specific failing job, open run URL or inspect logs with `gh`.

## Issues & context

```bash
gh issue view <n>
gh pr view <n>
```

## FlexPrice-specific

- SDK publish workflows and path filters: see **`.github/workflows/`** and **`AGENTS.md`** (SDK pipeline).
- Do not push or publish packages unless the user explicitly asked.

## Related

- **Pre-PR quality**: [`pr-self-review`](../pr-self-review/SKILL.md)
