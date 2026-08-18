# Architecture

This document explains how the Flexprice CLI is put together, for anyone
contributing to `flexprice/cli` who has not read the design documents that
produced it. The decisions with real trade-offs behind them are recorded
separately as short ADRs in [`decisions/`](decisions/); this document is the
narrative that ties them together.

## Request lifecycle

Every command — whether hand-written (`login`, `events`) or generated from the
API spec (`customers list`, `invoices finalize`) — follows the same path:

```
 user input                registry              request builder
(flags, positional  ──▶  commands.yaml   ──▶   internal/spec.BuildRequest
 ID, --data, --edit)     internal/spec         (path/query/body assembly,
                         .Registry.Lookup        flag-vs-schema validation)
                                                         │
                                                         ▼
                                                 internal/client.Client.Do
                                                 (auth header, retry policy,
                                                  timeout, --debug redaction)
                                                         │
                                                         ▼
                                                internal/output.Writer.Render
                                                (table / json / yaml,
                                                 stdout for data, stderr
                                                 for everything else)
```

Every box in that diagram is one package with one job:

- **`internal/spec`** (`loader.go`, `registry.go`, `request.go`) turns the
  embedded OpenAPI document plus `commands.yaml` into a resolvable command
  tree, and turns one resolved command plus user input into an HTTP request.
  Nothing in this package makes a network call.
- **`internal/client`** (`client.go`, `errors.go`) is the only package that
  makes a network call. See [ADR 0001](decisions/0001-no-sdk-single-http-path.md).
- **`internal/output`** (`output.go`, `table.go`) turns a raw JSON response
  into what the terminal or a script sees, and is the only package that
  renders response data — no other package writes to `os.Stdout`.
- **`internal/config`** and **`internal/keyring`** resolve *who* is making the
  request — profile, region, API key — before `internal/client` is ever
  called. See [ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md).
- **`internal/cmd`** is the only package that imports `cobra`. It wires the
  above together into the command tree cobra dispatches.

A sixth package, **`internal/exitcode`**, does not sit in the request path
above; it defines the stable exit codes that `internal/client`'s errors carry
out of the process (see [Error and exit-code contract](#error-and-exit-code-contract)
below).

## Why runtime dispatch, not code generation

The API has 198 callable operations. Generating 198 Go files — one per
command — was considered and rejected: it means a build step nobody remembers
to run, generated code nobody reviews line-by-line, and a repository that grows
by hundreds of files for every new endpoint. Instead, the OpenAPI document is
embedded in the binary via `go:embed` (`cli/spec/embed.go`) and parsed once per
invocation (`internal/spec.Load`, measured at 48–73ms for the current spec in
the implementation spike). The command tree cobra sees is built by walking the
parsed document against `commands.yaml` at startup, not by reading generated
source.

The corresponding cost is that command names cannot be derived automatically
from the spec — see [ADR 0004](decisions/0004-curated-commands-yaml-over-mechanical-derivation.md)
for why, and why that cost is worth paying by hand rather than working around.

## Auth and profiles

A Flexprice API key is scoped to exactly one environment. The CLI's entire auth
model follows from that one fact — see
[ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md) for the
full reasoning, including why there is no `--environment` flag and no
automatic production/development detection.

## Error and exit-code contract

`internal/client.NewAPIError` (`errors.go`) normalizes three response shapes
this API actually returns — a structured `{code, message, http_status_code,
details}` envelope, a bare `{"error": "Unauthorized"}` string from the auth
middleware, and non-JSON bodies from anything sitting in front of the API — into
one `*APIError` type with a stable `ExitCode()` (`internal/exitcode`). Every
command that fails exits with one of those codes; scripts can depend on them
never changing meaning.

## Where to look next

- [`decisions/`](decisions/) — why, for the five decisions with real
  trade-offs behind them.
- [`guides/`](guides/) — walkthroughs for the two most common maintenance
  tasks.
- [`README.md`](README.md) — how to install and use the CLI as a consumer.
