# Flexprice CLI — Handoff

Date: 2026-08-18
Branch: `claude/flexprice-cli-design-7f7052` (pushed; `origin` matches `HEAD` at `bb0f1d67d`)
Worktree: `.claude/worktrees/reverent-lehmann-62238a`

## 1. What exists

A working Go CLI at `cli/`, its own module (`github.com/flexprice/cli`, Go 1.25), built to be
pushed to a separate `flexprice/cli` repo on release.

**Verified state at handoff:** `go build ./...`, `go vet ./...`, `gofmt -l .` all clean;
`go test ./... -race` passes across all 8 packages; 168 test functions.

```
cli/
├── main.go                    entry point; honors APIError.ExitCode()
├── spec/                      go:embed data — openapi.json + commands.yaml
├── internal/
│   ├── cmd/       cobra commands: auth, config, env, init, misc, raw, resource, root
│   ├── spec/      loader, registry, request builder, --edit skeleton, pagination
│   ├── client/    THE single HTTP path + error envelope handling
│   ├── output/    table/json/yaml rendering
│   ├── config/    ~/.flexprice/config.toml, credential precedence
│   ├── keyring/   OS keychain + encrypted file fallback
│   ├── style/     the only package that owns color
│   └── exitcode/  stable exit codes (0,1,2,3,4,5)
├── tools/
│   ├── bootstrap-commands/    one-time helper, already used
│   └── gendocs/               cobra→Markdown reference; MANUAL ONLY (see §4)
├── README.md, ARCHITECTURE.md, AGENTS.md
├── decisions/     5 ADRs
└── guides/        adding-a-command, adding-a-hand-written-command
```

**Capabilities:** 197 curated commands across 34 resources; raw `get`/`post`/`delete` escape
hatch; `--edit` opens `$EDITOR` with a pre-filled request body for deep schemas; `--limit` /
`--all` pagination; profiles with credential precedence (flag → env → keyring → config);
arrow-key region picker; block-letter welcome wordmark; colored table output with a status
footer.

## 2. The decisions that matter most

Full reasoning lives in `cli/decisions/`. The five that a newcomer will otherwise
re-litigate:

| ADR | Decision |
|---|---|
| 0001 | No SDK — exactly one HTTP path through `internal/client` |
| 0002 | Retry only idempotent methods; POST never retries on 5xx |
| 0003 | Environment-scoped profiles, no derived live/test flag |
| 0004 | Curated `commands.yaml` over mechanical name derivation |
| 0005 | Regions read from the OpenAPI spec's `servers[]`, never hardcoded |

**ADR 0002 and 0003 are the two with real teeth.** 0002 exists because the default retry
library retries POST identically to GET, and `CreateSubscriptionRequest` has no idempotency
field — a 5xx after a successful commit could have created duplicate subscriptions billing
real customers. 0003 exists because no endpoint reachable by an environment-scoped key reveals
which environment it belongs to; this was verified by probing the live API, not assumed.

## 3. Bugs found during the work, and how

Recorded because the *method* generalizes, not just the fixes:

- **Retry duplicating billing writes** (ADR 0002) — found by a code review that read the
  retry library's source rather than trusting its documented defaults.
- **`BuildRequest` mutated the caller's flag map.** `Input` is passed by value but `Flags` is a
  map, so consumed path/query params were deleted from the *caller's* map. The `--all` loop
  rebuilds the request per page, so `payments list --status succeeded --all` silently dropped
  the filter after page one — returning *more* rows than asked for, with no error. Fixed at the
  source (clone inside `BuildRequest`) so all 197 commands are protected.
- **Table columns misaligned once color was on.** `text/tabwriter` counts ANSI escape bytes as
  visible width. **The tests passed the whole time** — they only assert "escape codes are
  present." Found by rendering real output and looking at it. Now measured with
  `lipgloss.Width`.
- **The welcome box rendered visibly crooked** — a hardcoded 30-char border around a 24-char
  content row. Same discovery method. Now sized to its content.
- **A test that would never fail.** `promptRegion`'s non-TTY guard was covered by a test
  written against the old implementation *before* the rewrite, specifically so it would prove
  the rewrite didn't break scripted/CI callers. Worth copying as a pattern.

The recurring lesson: **tests that assert "something was produced" don't catch "the wrong
thing was produced."** Several defects here were only visible by rendering output and
inspecting it.

## 4. Open items — nothing here is a blocker, all are real

**Needs a human at a real terminal:**
- The arrow-key region picker's actual keyboard interaction has never been driven by a human.
  It compiles against the real `huh` API, its non-TTY fallback is tested, and the spike proved
  it fails fast rather than hanging — but no one has pressed ↑/↓ and Enter. Verify with:
  `HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file ./bin/flexprice init`

**Stale documentation (introduced by the final UI work):** — **both resolved in the
DX polish round, 2026-08-18.**
- ~~`docs/design/2026-08-18-flexprice-cli-interactive-ui-implementation-plan.md` still
  describes the bordered welcome box that the block wordmark replaced.~~ Task 5 in that
  document now carries a "Superseded — do not implement as written" note explaining why.
- ~~`cli/README.md` documents neither the wordmark nor the status footer.~~ Quickstart now
  shows the real wordmark and region picker; "What you can do" shows the status footer,
  mutation receipts and empty states.

**Superseded by the DX polish round** (see
`2026-08-18-flexprice-cli-dx-polish-design.md` and its implementation plan): the CLI now
has an `internal/ui` package owning all human-facing output, grouped root help, spinners,
`--no-input`, `TERM=dumb` handling and SIGINT teardown. Two items in §5 below changed as a
result and are annotated there.

**Before the CLI can be installed by anyone:**
- `flexprice/cli` is **private and unlicensed**. Homebrew, `install.sh` and `go install` all
  fail until it is public with a license set.
- Confirm `SDK_DEPLOY_GIT_TOKEN` has write access to `flexprice/cli` and
  `flexprice/homebrew-tap`.
- Replace the `@flexprice/cli-maintainers` placeholder in `.github/CODEOWNERS`.
- Enable Issues / disable PRs on `flexprice/cli` (source of truth is this monorepo).
- Archive the Rust `flexprice/flexprice-cli`, after speaking to its author.

**Backend changes that would materially improve the CLI (not CLI work):**
- **Expose the active key's environment** — `environment_id` on `/secrets/api/keys`, or a
  `/v1/me`. This is the single highest-value change: it would let the CLI label profiles by
  environment and offer a real production guard instead of confirming every destructive action
  indiscriminately (ADR 0003).
- Add swaggo annotations to `EnvironmentHandler.GetEnvironments` so `GET /v1/environments`
  enters the OpenAPI spec. The CLI currently calls it by literal path because it is absent
  from the spec and cannot be resolved through the registry.
- `POST /v1/subscriptions` has no idempotency key, and where a body-level `idempotency_key` is
  omitted the server generates one containing a timestamp — which differs per retry attempt
  and therefore cannot dedupe. **Any HTTP client with default retry behavior, not just this
  CLI, can double-create billing objects against that API.**

**Deliberately not built:**
- `listen` (webhook forwarding to localhost) — needs new backend endpoints; scoped as its own
  plan, never started.
- An interactive REPL/TUI mode — considered, explicitly out of scope; this is a command-runner.
- `gendocs` is **not** wired into release automation. It runs only via `make cli-docs`. Both
  the tool and the Makefile target carry a TODO so nobody automates it without a decision.

## 5. Known operational hazards

- **Do not run the built binary unattended without `FLEXPRICE_KEY_BACKEND=file` and a scratch
  `HOME`.** `login`/`whoami`/`init` call the real OS keychain regardless of which HTTP backend
  they point at. During this work an unattended run triggered a blocking macOS "Keychain Not
  Found" dialog with a destructive "Reset To Defaults" button. Safe form:
  `HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file ./bin/flexprice <cmd>`
- **`internal/style` auto-detects color** and `go test` never has a terminal attached. Any test
  asserting ANSI codes are present must call `style.EnableForTests()` (usually via `TestMain`),
  or it will fail based on where it runs rather than on correctness.
  **The inverse is the sharper trap, found in the DX polish round:** a test asserting ANSI
  codes are *absent* passes for free under `go test`, because lipgloss suppresses colour on
  its own when no terminal is attached. Such a test can pass with the code under test deleted
  outright. `internal/ui` has a `TestMain` forcing a true-colour profile for exactly this
  reason. When writing an absence assertion, verify it by removing the mechanism it guards.
- **This branch has been worked on by more than one session.** Several commits in the log —
  the `AGENTS.md` series, six `fix(cli)` commits, `d4bca9f2d`, and the maintainer-docs
  design/plan — were made outside the conversation that produced this handoff and have **not**
  been independently verified here. Git author is identical across all commits (the repo's
  configured identity), so authorship cannot be distinguished from the log alone. Treat the
  current green test suite as the shared source of truth, not any individual commit message.
- **Do not `--amend` or rebase already-pushed commits.** Doing so earlier in this work caused a
  `git pull --rebase` conflict for a concurrent session. Land fixes as new commits.

## 6. Where to start reading

1. `cli/README.md` — what it does, from a user's point of view.
2. `cli/ARCHITECTURE.md` — request lifecycle and why runtime dispatch over code generation.
3. `cli/decisions/` — the five decisions above, ~35 lines each.
4. `cli/guides/adding-a-command.md` — the most common contribution task.

Design and plan documents for every phase are under `docs/design/2026-08-18-flexprice-cli-*`.
They record the reasoning, including options considered and rejected; the ADRs are the
condensed version and are the better entry point.
