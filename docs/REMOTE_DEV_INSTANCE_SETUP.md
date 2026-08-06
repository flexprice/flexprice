# Remote Dev Instance Setup

Guidance for running Flexprice on a **shared remote Linux box** where several
developers each work in their own folder, against **shared cloud/staging
infrastructure** (rather than spinning up local Docker for Postgres, Kafka,
ClickHouse, etc.).

This is meant for AI-agent-driven sessions (Claude Code / Cursor) as well as
humans. It complements [`LOCAL_TESTING.md`](../LOCAL_TESTING.md) (Docker-based
local setup) — use this doc when you want to point a locally-built server at
already-deployed staging resources.

> Paths below use `$HOME` and `<dev>` placeholders. Substitute your own home
> directory and folder name. Never commit real secrets, IPs, tokens, or a
> `.env` file.

---

## Per-folder layout

- Each developer works in their own folder, e.g. `$HOME/<dev>-claude/`.
- The flexprice clone lives at `$HOME/<dev>-claude/flexprice` (i.e. `./flexprice`
  relative to the session working directory).
- Clone if missing: `git clone https://github.com/flexprice/flexprice`.
- **`.env`** must exist at `./flexprice/.env` (permissions `600`). It holds
  staging credentials for Postgres, Redis, ClickHouse, Kafka, Temporal, and the
  various third-party integrations. Obtain it from your team's secrets manager;
  **never commit it or print its values.** The repo already ships `.env.local`
  (local Docker defaults) and `.env.vault` — those are separate from this file.

---

## Toolchain

Install once on the box (versions track `go.mod` and the `install-typst` target):

- **Go** — version pinned by `go.mod` (e.g. `go 1.25.0`); install to `/usr/local/go`.
- **typst** — installed by `scripts/install-typst.sh` to `~/.local/bin/typst`;
  required by the `internal/typst` tests and the `install-typst` make dep.
- `git`, `make`, `gcc`.
- Put the toolchain on `PATH` before building/testing:
  ```bash
  export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"
  ```

---

## Running the server against staging resources

To test local code changes against shared staging infra **without** Docker, run
the plain server target so it loads `.env` directly (the `run-local*` targets
layer `.env.local` on top, which would override staging endpoints with local
Docker ones):

```bash
make run-server        # go run cmd/server/main.go, loads .env
curl http://localhost:8080/health   # -> {"status":"ok"}
```

---

## Working across multiple worktrees

When juggling several branches at once, give each its own git worktree and its
own server port so they never collide — see `make run-wt` / `make wt-ports`
(documented in the Makefile). Each branch gets a deterministic port and they all
share the single `.env` from the main clone, so there is only ever one secrets
file regardless of how many worktrees are active.

```bash
# From the main clone
git worktree add -b <branch> ../flexprice-worktrees/<name>

# From any worktree
make run-wt      # server on this branch's deterministic port
make wt-ports    # list all worktrees, their ports, running/stopped status
```

---

## Testing

```bash
make test              # install-typst + go test -v -race ./internal/...
go test ./internal/... # quicker smoke check (no -race; still needs typst on PATH)
```

> Some event-service tests (`internal/ee/service`, e.g.
> `TestEventService/TestGetEvents`) can be order-sensitive and fail
> intermittently only under the full parallel run; they pass in isolation.

### Integration sanity suite

Run the orchestrated sanity suite against one or more deployed targets (see
[`integration-testing-suite/go`](../integration-testing-suite/go)). Provide
targets via a JSON file — keep real API keys out of version control:

```bash
# targets.json (do not commit real keys)
# [
#   { "name": "staging", "api_host": "api-dev.example/v1", "api_key": "sk_..." },
#   { "name": "local",   "api_host": "localhost:8080/v1",  "api_key": "sk_..." }
# ]
FLEXPRICE_TARGETS_FILE=targets.json make test-suite
```

When a locally-run server is pointed at the same staging database, the same API
keys work against both `localhost` and the deployed host.

---

## Per-developer GitHub auth isolation (shared home)

On a shared box, all folders share one Linux home, so a plain `gh auth login`
writes to `~/.config/gh` and **every folder would push / open PRs as that one
GitHub user.** To keep identities separate, pin a per-folder `gh` config dir:

1. Create a private config dir per folder:
   ```bash
   mkdir -p -m 700 $HOME/<dev>-claude/.gh
   ```
2. Point the folder's sessions at it via `$HOME/<dev>-claude/.claude/settings.json`:
   ```json
   { "env": { "GH_CONFIG_DIR": "/home/<user>/<dev>-claude/.gh" } }
   ```
   (Or export `GH_CONFIG_DIR` in the shell before running `gh`.)
3. Authenticate and wire up git, once, in that folder:
   ```bash
   gh auth login          # token stored in <dev>-claude/.gh, not the shared default
   gh auth setup-git      # git credential helper defers to GH_CONFIG_DIR at runtime
   git -C flexprice config user.name  "<handle>"
   git -C flexprice config user.email "<id>@users.noreply.github.com"
   ```
4. Verify: `gh auth status` shows the account for the active folder.

The credential helper (`!gh auth git-credential`) is identity-agnostic — it
returns whichever token `GH_CONFIG_DIR` points at when a command runs, so each
folder pushes as the right person on the same machine.

> On a developer's own laptop this isn't needed — separate machines already mean
> separate homes and separate `gh` auth. This isolation matters only on a shared
> remote box.

---

## Notes

- Shared remote boxes are often modest (e.g. 2 vCPU). `-race` roughly doubles
  compile/run time, so the first `make test` is slow; the build cache makes
  reruns fast.
- Prefer real dedicated tools over ad-hoc shell where possible; keep secrets in
  `.env` / a secrets manager, never in committed files.
