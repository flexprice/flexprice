# Flexprice CLI — Maintainer Documentation Design

Date: 2026-08-18
Status: Approved design, ready for implementation planning
Related: `docs/design/2026-08-18-flexprice-cli-design.md`,
`docs/design/2026-08-18-flexprice-cli-docs-design.md` (README/ARCHITECTURE.md/ADRs)

## 1. Purpose

The CLI's implementation is complete (17 tasks) and its user- and
contributor-facing narrative documentation exists (`README.md`,
`ARCHITECTURE.md`, `decisions/`). What is still missing is documentation aimed
at whoever maintains this code next — a human successor or a future agent
working in a directory cold, without having read the design docs that produced
it.

This document designs that documentation: per-directory `AGENTS.md` files
following a pattern already proven in this monorepo (root `AGENTS.md` plus
`internal/service/AGENTS.md`, `internal/e2eprobe/AGENTS.md`), and two short
guides for the two concrete maintenance tasks a contributor will actually face.

This is scoped separately from the CLI code review and the README-vs-competitor
research the user also requested — those are evaluative work (run them, act on
findings) with no design step of their own, and run independently of this plan.
The one dependency between the two: the per-package `AGENTS.md` "Common
pitfalls" sections are grounded in real review findings, not speculation, so
the review runs before those sections are written.

## 2. Decisions

| # | Decision | Rationale |
|---|---|---|
| M1 | Two-tier structure: `cli/AGENTS.md` (constitution) + one per package | Mirrors the monorepo's own proven pattern exactly, rather than inventing a new one |
| M2 | Frontmatter: `layer` + `owns` only, no `synced_sha`/`synced_at` | Those fields imply a sync tool that keeps them current; `cli/` has none once it's a standalone repo, and a stale timestamp implying freshness it doesn't have is worse than no timestamp |
| M3 | Per-package files follow `internal/service/AGENTS.md`'s shape: Purpose, Key files, a critical-path walkthrough, Patterns, Invariants, Pitfalls, Related layers | Proven concise-but-useful shape already in this codebase; `internal/e2eprobe/AGENTS.md`'s longer table-heavy style is for a more complex domain (customer cohorts) that doesn't apply here |
| M4 | Pitfalls sections wait on the code review, not written from a first read-through | The monorepo's own examples (spec/'s cycle-guard note, client/'s retry-safety rule) came from real bugs found during work, not guesses; a pitfalls section not grounded in a real incident is filler |
| M5 | Two guides only: curating a command, adding a hand-written command | These are the two concrete tasks this codebase's own architecture makes routine — everything else (releases, region config) is either already covered by an ADR or rare enough not to need a dedicated walkthrough |
| M6 | Guides live in `cli/guides/`, linked from `ARCHITECTURE.md`'s "Where to look next" | Parallels how `decisions/` is already discovered from that section |

## 3. File structure

```
cli/
├── AGENTS.md                              constitution
├── ARCHITECTURE.md                        (existing, gets one new link)
├── decisions/                             (existing, unchanged)
├── guides/
│   ├── adding-a-command.md
│   └── adding-a-hand-written-command.md
└── internal/
    ├── client/AGENTS.md
    ├── cmd/AGENTS.md
    ├── config/AGENTS.md
    ├── keyring/AGENTS.md
    ├── output/AGENTS.md
    └── spec/AGENTS.md
```

Nine new files, one small edit to `ARCHITECTURE.md`.

## 4. `cli/AGENTS.md` (constitution)

```markdown
---
layer: constitution
owns:
  - "cli/**"
---

# Flexprice CLI — Constitution

> Invariants that must hold everywhere in this repo.
> Package-specific rules → the AGENTS.md in that package.
> Why → decisions/, narrative → ARCHITECTURE.md.

## Stack
Go 1.25 · cobra · kin-openapi · go-keyring · BurntSushi/toml · go-retryablehttp

## Directory map
| Package | Path | One-line rule |
|---|---|---|
| Client | internal/client/ | The only package that makes a network call — ADR 0001 |
| Spec | internal/spec/ | Loads the embedded OpenAPI doc, resolves commands, builds requests — never calls the network |
| Config | internal/config/ | Profiles and credential precedence, no secrets |
| Keyring | internal/keyring/ | API key storage — OS keychain, encrypted-file fallback |
| Output | internal/output/ | Renders responses — the only package that writes to stdout |
| Cmd | internal/cmd/ | The only package that imports cobra; wires the above together |

## Hard invariants
- No second way to make an HTTP request — everything goes through
  client.Client.Do (ADR 0001).
- Never retry a non-idempotent method (POST/PATCH) on 5xx or a transport
  error (ADR 0002).
- Never send x-environment-id — an API key already determines its
  environment.
- No live/test flag on a profile, no --environment flag anywhere — nothing
  reachable by a key reveals its environment (ADR 0003).
- commands.yaml is hand-curated. An unmapped operation gets a derived name
  and a CI warning, never a build failure (ADR 0004).
- Region list comes from the embedded spec's servers[], never hardcoded
  (ADR 0005).
- Comments only where the logic or a constraint is genuinely non-obvious —
  no narration of straightforward code.
- No Co-Authored-By, no mention of Claude/Anthropic/AI assistance, anywhere
  in a commit or a comment.

## Where to look next
- ARCHITECTURE.md — narrative overview, request lifecycle
- decisions/ — why, for the five decisions above
- guides/ — how to do the two most common maintenance tasks
- The package you're working in has its own AGENTS.md — read it first
```

## 5. Per-package `AGENTS.md` — content plan

Each follows `internal/service/AGENTS.md`'s shape. Content summarized here;
full prose is written during implementation, grounded in the actual code and
(for Pitfalls) actual review findings.

**`internal/client/AGENTS.md`** — Purpose: the one HTTP path. Key files:
`client.go` (Do, retryPolicy), `errors.go` (APIError, three envelope shapes).
Critical path: request → retry decision → error normalization. Patterns:
always thread ctx, never add a second request-issuing function. Invariants:
retry policy per ADR 0002, allowlist redaction in `--debug`. Pitfalls: (from
review) whatever the review surfaces about timeout handling, error
envelope edge cases.

**`internal/spec/AGENTS.md`** — Purpose: resolves the embedded OpenAPI spec
into commands and requests; the biggest package (10 files) and the one with
the most non-obvious constraints. Key files: `loader.go`, `registry.go`,
`request.go`, `skeleton.go`, `paginate.go`. Critical path: spec load →
registry resolution → request build → (optionally) skeleton generation.
Patterns: `doc.Paths` is `*openapi3.Paths` not a map, use `.Map()`. Invariants:
depth cap 16, cycle guard breaks breadth not just depth, required-only
skeleton fill policy is deliberate (untouched optional numeric fields as `""`
break server-side binding). Pitfalls: (from review) whatever surfaces about
schema edge cases, the `Webhook Events` stub exclusion.

**`internal/config/AGENTS.md`** — Purpose: profiles and credential
precedence, no secrets. Key files: `config.go`, `resolve.go`. Critical path:
flag → env var → keyring → config file. Patterns: `Profile` has no
environment/live field by design. Invariants: `Save` is atomic (temp file +
rename). Pitfalls: (from review).

**`internal/keyring/AGENTS.md`** — Purpose: API key storage. Key files:
`keyring.go` (Open, OS backend), `file.go` (encrypted fallback). Critical
path: probe → OS keychain or file fallback. Patterns: the probe is bounded at
2s specifically because an unbounded D-Bus call can hang the CLI forever.
Invariants: `Set` is atomic. Pitfalls: (from review, and the real macOS
"Keychain Not Found" dialog incident from this session is worth citing
directly here as a concrete pitfall for anyone testing this package).

**`internal/output/AGENTS.md`** — Purpose: renders responses; the only
package writing to stdout. Key files: `output.go`, `table.go`. Critical path:
raw bytes → envelope detection → format dispatch. Patterns: data to stdout,
everything else to stderr, always. Invariants: row detection prefers `items`
and requires a pagination marker before treating an array field as rows (not
just "first array found"). Pitfalls: (from review) the known binary-response
gap (a PDF response is unguarded against an interactive terminal) belongs
here explicitly, since it's a real, already-identified, deliberately deferred
issue a future contributor needs to know about before they hit it.

**`internal/cmd/AGENTS.md`** — Purpose: the only package importing cobra;
wires everything together. Key files: `root.go` (Globals, wiring),
`resource.go` (spec-driven dispatch), `auth.go`/`env.go`/`config.go`/`init.go`
(hand-written commands), `misc.go`, `raw.go`. Critical path: `NewRootCommand`
→ `bindGlobals` → `AddCommand` (hand-written) → `addResourceCommands` (spec-driven)
→ `addRawCommands`. Patterns: `*Globals` is threaded as a parameter, never a
package variable (a real regression this session caught and fixed).
Invariants: destructive actions always confirm, regardless of profile.
Pitfalls: (from review) whatever surfaces, plus the documented
`collectUnknownFlags` limitation (a space-separated flag value starting with
`--` is silently dropped).

## 6. Guides

### `cli/guides/adding-a-command.md`

Walks through: a backend PR adds an endpoint → CI's default-allow validator
warns it's unmapped, using a derived name → find it in `commands.yaml` → pick
a resource+action name following the existing conventions (domain verbs, not
HTTP verbs) → add a `columns:` entry if it's a list operation → CI passes.
Includes the real example of a mechanical-derivation failure from this
session (alphabetical collision misassigning `entitlements retrieve`) as a
concrete illustration of why hand-curation matters, not just a rule stated in
the abstract.

### `cli/guides/adding-a-hand-written-command.md`

Walks through the actual pattern used by `auth.go`/`misc.go`, not an
idealized one: new file in `internal/cmd/`, a `func newXCommand(g *Globals,
version string) *cobra.Command` constructor, using `runtimeContext(g)` for
credentials and `client.New` for requests, registering in `root.go`'s
`AddCommand(...)`. Notes the file-scope discipline this session used when
parallelizing work on `root.go` (multiple commands' worth of wiring landing
in one file needs care about collisions) as a real, not hypothetical,
maintenance consideration.

## 7. `ARCHITECTURE.md` edit

Add one line to the existing "Where to look next" section:

```markdown
- guides/ — walkthroughs for the two most common maintenance tasks
```

## 8. Sequencing

1. Code review (separate, evaluative, already agreed to run independently of
   this plan) produces findings.
2. Mechanical sections of all `AGENTS.md` files (frontmatter, directory map,
   Purpose, Key files, Patterns, Invariants) can be written any time —
   nothing about them depends on review findings.
3. Pitfalls sections are written or finalized after review findings exist, so
   they cite real issues.
4. The two guides do not depend on the review at all and can be written any
   time.

## 9. Out of scope

A full docs site, per-function godoc comments beyond what Go idioms already
expect, a CONTRIBUTING.md beyond what `README.md`'s existing "Contributing"
section covers, and any documentation of the `listen` subsystem (still
unbuilt, tracked as a separate plan).
