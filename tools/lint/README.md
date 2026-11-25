# tools/lint

Flexprice's own `go/analysis` checks. Prose in `AGENTS.md` says what a rule is for;
the analyzer here is what makes it stick.

| Binary | Analyzers | Scope | Make target |
|---|---|---|---|
| `tools/bin/loglint` | `loglint` | whole repo, must stay clean | `make lint-ci` |
| `tools/bin/flexlint` | `loglint` + `codelint` | lines changed since `LINT_BASE` | `make lint-changed` |
| `tools/bin/changedlines` | filter | keeps `file:line:col:` diagnostics on changed lines | used by `lint-changed` |

`codelint` runs on changed lines only because the code it judges predates the rules.
Silence a finding that is right to keep with a reasoned directive on the line above or
at the end of the line: `//lint:ignore CL004 the wallet table is not activity-tracked`.
A directive with no reason is itself a finding (`CL000`).

## codelint rules

| Rule | Fires on | Why |
|---|---|---|
| CL001 | A comment over 4 lines, a section divider, commented-out code, a comment that only repeats the name below it, a TODO/FIXME with no `FLE-`/issue/URL link, or a ticket reference in an ordinary comment | A comment states a constraint or a quirk the code cannot show. Background, history and tickets go in the PR or the design doc. |
| CL002 | `internal/api/**` importing `internal/repository` or `ent`; `internal/domain/**` importing service, repository or api; a gateway SDK (`stripe-go`, `razorpay-go`, `chargebee-go`) imported outside `internal/integration` | Handlers delegate, domain depends on nothing above it, provider types stay behind the provider interface. |
| CL003 | In `_test.go`: `time.Sleep`; `Skip()` with no reason; an unconditional skip without `// quarantine: FLE-1234 <why>` on the line above; a `Test*` body over 100 lines that is not table-driven | Tests are short, deterministic, and either run or are tracked. |
| CL004 | `Update()…Save/Exec` or `Delete()…Exec` on an entity the activity log registers | The activity hook only sees rows; use `SaveReturning` / `ExecReturning`. `TestTrackedEntities` keeps the list equal to `internal/activity/entities.go`. |
| CL005 | `.Truncate(time.Microsecond)` outside `internal/clock` | One precision contract, held at the four boundaries, never per entity. |
| CL006 | A file in a package named `utils`, `util`, `helpers`, `common`, `misc`, `shared` | A package is named for what it provides. Put the function with the feature that uses it. |
| CL007 | A variable, field or parameter named after its type: `priceMap`, `metersSlice`, `amountStr` | Name the contents: `pricesByID`, `meters`, `amount`. |

## loglint rules

See `internal/logger/README.md`. In short: ctx-first logger API, `"error"` key on every
`Error`, `Warn` only in bootstrap, no `fmt.Print*`, `clock.Now()` not `time.Now()` in
business layers.

## Adding a rule

1. Add `checkX` in its own file under `codelint/`, with a `CL0NN` code and a message that
   says the fix, not just the fault.
2. Add fixtures under `codelint/testdata/src/` with `// want` markers for every finding and
   at least one case that must not fire.
3. Name the rule and what it protects in `AGENTS.md` and in the table above.
4. If the rule is also a golangci-lint linter, enable that in `.golangci.yml` instead.
