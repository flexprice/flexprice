# Flexprice CLI — README and Contributor Documentation Design

Date: 2026-08-18
Status: Approved design, deferred write pending Tasks 11/13/14
Related: `docs/design/2026-08-18-flexprice-cli-design.md`, `docs/design/2026-08-18-flexprice-cli-implementation-plan.md`

## 1. Purpose

`cli/README.md` today is a two-line stub. Once `flexprice/cli` is public, it is the
first thing an end user, a contributor, and every package manager (Homebrew, `go
install`) surfaces. There is also no contributor-facing architecture documentation —
the design and implementation-plan docs exist, but they live in the monorepo under
`docs/design/` and are not part of what ships to `flexprice/cli`.

This document designs two things:

1. **`cli/README.md`** — rewritten for the end consumer: how to install, run, and use
   the CLI, consistent across every platform it is published to.
2. **`cli/ARCHITECTURE.md` + `cli/decisions/`** — contributor-facing documentation
   explaining the shape of the code and the reasoning behind decisions that were not
   obvious in advance, written the way a new contributor to `flexprice/cli` would need
   it, independent of the monorepo's own `docs/design/`.

Both are written treating `cli/` as if it were already the standalone repository it
becomes on release.

## 2. Decisions

| # | Decision | Rationale |
|---|---|---|
| E1 | Write for the final v1.0 shape, not the current partial state | This never ships mid-build; writing incrementally would mean rewriting the README 3-4 more times as tasks land |
| E2 | Contributor docs: one `ARCHITECTURE.md` + short ADRs in `decisions/`, not a full docs site | Enough for a new contributor to orient without reading source; a full multi-page site has near-empty pages at v1.0 and is easier to grow into later than to fill prematurely |
| E3 | README shows only the three distribution channels backed by a plan task (Homebrew, `install.sh`, `go install`) | The npm wrapper is explicitly optional and unscheduled; an instruction that does not work yet is worse than an absent one |
| E4 | Hand-written docs live at `cli/ARCHITECTURE.md` and `cli/decisions/`, not `cli/docs/` | Task 15 already gitignores `cli/docs/` as the target for generated `cobra doc` command reference. Splitting by "maintained vs. generated" rather than sharing a directory name avoids the two colliding |
| E5 | README written and verified after Tasks 11, 13 and 14 land; ADRs written now | The README's examples need `login`, the resource command tree, and `--edit` to actually exist and be run against, or it risks documenting a command shape that shifts. The five ADRs describe decisions already made and verified during design and implementation review — nothing about them depends on later tasks |

## 3. README.md

### Structure

```
# Flexprice CLI
<one-line pitch>
<30-second quickstart: install → init → first real command>

## Install          (brew / install.sh / go install)
## Quickstart        (init, whoami, first customers/events command)
## What you can do   (resource commands, events, fixtures, --edit, raw escape hatch)
## Authentication    (profiles are environment-scoped)
## Output & scripting (--output json, exit codes, stdout/stderr contract)
## Configuration     (~/.flexprice/config.toml, env vars)
## Full command reference → link to generated docs
## Contributing      (source lives in flexprice/flexprice; PRs go there, issues here)
## License
```

Kept short by design: it is an entry point, not a reference manual. The generated
command docs (Task 15's `cobra doc` output, published to flexprice-docs) are the
reference; the README's job is to get a new user to their first successful command in
under a minute.

### Content rules

- **Every command shown must be real**, cross-checked against the actual
  `cli/spec/commands.yaml` (197 curated commands) rather than written from memory or
  as a plausible-looking example.
- **Self-contained tone.** Does not assume Stripe CLI familiarity as a prerequisite.
  One aside — "if you've used the Stripe CLI, this will feel familiar" — for readers
  who have, but nothing in the instructions depends on that background.
- **Authentication gets its own section, not a flag table entry.** The
  environment-scoped-profile model (an API key belongs to exactly one environment;
  there is no `--environment` flag; switching environments means switching profiles)
  is the one concept most likely to confuse a new user if left implicit, so it is
  explained before the user can hit it as a surprise.
- **Output & scripting section states the stdout/stderr contract explicitly**
  (`--output json > file.json` is always clean) and lists the stable exit codes —
  this is what makes the CLI usable in CI, and it is easy to bury in a flags
  reference where a scripting user would not think to look.

### Verification, not just writing

Once Tasks 11, 13 and 14 land, every command shown in the README is run against the
built binary and its actual output compared to what is documented. A README with a
copy-pasteable command that does not work is worse than a shorter, fully verified one.

## 4. Contributor documentation

### `cli/ARCHITECTURE.md`

Narrative sections, written for someone who has never read the monorepo's design
docs:

1. **Request lifecycle** — input (flags/positional/`--data`/`--edit`) → registry
   lookup (`commands.yaml`) → request builder (`internal/spec`) → the single HTTP
   client (`internal/client`) → output renderer (`internal/output`). One diagram
   showing this path, since it is the shape everything else hangs off.
2. **Why runtime dispatch, not code generation.** 198 operations become commands by
   resolving against an OpenAPI document embedded at build time, not by generating
   198 files. Trade-off: one binary, no generated-code drift, at the cost of a
   `commands.yaml` that must be curated by hand rather than derived.
3. **The "one HTTP path" property.** Every request — hand-written commands,
   spec-dispatched resource commands, and later the fixture engine — goes through
   `internal/client.Do`. This is what makes the retry-safety and auth-header
   guarantees hold everywhere at once instead of needing to be re-verified per
   command.
4. **Auth and profiles.** Why a profile carries no environment name or live/test
   flag (summarized here, detailed in ADR 0003).
5. **Error and exit-code contract.** The three response envelope shapes this API
   actually returns, and how `APIError` normalizes them.

### `cli/decisions/`

Short ADRs, Context/Decision/Consequences format, roughly 30-40 lines each. Not a
process for every future decision — a record of the five made so far that a
contributor would otherwise have to reverse-engineer from source or git history:

| # | Title | Why it earns an ADR |
|---|---|---|
| 0001 | No SDK; one HTTP path through `internal/client` | An earlier draft had two HTTP stacks (hand-written commands via `go-sdk/v2`, spec-dispatched commands via raw requests) before this was caught during design review. The reasoning for rejecting that shape is exactly what a future contributor proposing "let's just use the SDK for X" needs to see first |
| 0002 | Retry only idempotent methods; POST never retries on 5xx | Found during Task 4's code review, not planned upfront: the default retry library retries POST identically to GET, and this API's `CreateSubscriptionRequest` has no idempotency key, so a 5xx after a successful commit could create duplicate subscriptions. The fix and its evidence trail belong in a citable record, not buried in a commit message |
| 0003 | Environment-scoped profiles, no derived live/test flag | Forced by directly probing the API: no endpoint reachable by a key reveals which environment that key belongs to. A contributor who later finds `GET /environments` and assumes it can restore a `--environment` flag needs to see why that was tried and rejected |
| 0004 | Curated `commands.yaml` over mechanical derivation | There is no `GET /customers`; `customers list` resolves to `POST /customers/search`. The mechanically-generated starting point also silently misassigned two real commands via alphabetical collision resolution — concrete evidence for why this file is hand-maintained rather than auto-generated |
| 0005 | Regions read from the spec's `servers[]`, never hardcoded | Small decision, but non-obvious: it means adding a region is a spec change, not a CLI code change, and a contributor should not "helpfully" hardcode a new region string somewhere |

## 5. File layout

```
cli/
├── README.md              # end-user facing, rewritten per §3
├── ARCHITECTURE.md         # contributor facing, §4
├── decisions/
│   ├── 0001-no-sdk-single-http-path.md
│   ├── 0002-retry-only-idempotent-methods.md
│   ├── 0003-environment-scoped-profiles-no-live-flag.md
│   ├── 0004-curated-commands-yaml-over-mechanical-derivation.md
│   └── 0005-region-discovery-from-openapi-servers.md
└── docs/                   # gitignored; generated by `make cli-docs` (Task 15)
```

## 6. Sequencing

This design is approved but the write is **deferred until Tasks 11 (`--edit`), 13
(auth commands) and 14 (resource tree) land** — those are what make `login`, resource
commands, and `--edit` real enough to document and verify against. Writing earlier
risks documenting a command shape that changes before those tasks finish.

The five ADRs, by contrast, describe decisions already made and verified — they can
be written as soon as this design is approved, independent of the sequencing above.

## 7. Out of scope

A full docs site (separate `overview.md`/`commands.md`/`auth.md`/etc. pages), a
CONTRIBUTING.md beyond the README's short section, and any docs.flexprice.io
integration beyond linking to it. These can grow out of the README/ARCHITECTURE split
later if the project needs them; nothing here should be built ahead of that need.
