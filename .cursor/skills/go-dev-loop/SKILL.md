---
name: go-dev-loop
description: >-
  Standard FlexPrice Go developer loop — format, vet, targeted tests with race detector,
  and Makefile shortcuts. Use when implementing features, before pushing, fixing CI, or when
  the user asks to run tests, verify changes, go dev loop, or pre-push checks.
---

# Go dev loop (FlexPrice)

## Defaults

- Repo root: workspace with `go.mod` and `Makefile`.
- Prefer **`make`** targets when they exist; fall back to raw `go` commands.

## Quick commands (copy order)

```bash
gofmt -w .
go vet ./...
```

**Focused test (preferred during iteration):**

```bash
go test -v -race ./path/to/package -run TestSpecificName
```

**Full suite:**

```bash
make test
```

Other useful Makefile targets from project docs:

- `make test-verbose`
- `make test-coverage`
- Single-package stress: `go test -race ./internal/service -run TestName`

## When changing code

1. Run **`gofmt -w`** on touched packages (or whole repo if unsure).
2. Run **`go vet ./...`** or at minimum `go vet` on affected packages.
3. Run **`go test -race`** for packages you edited plus immediate dependents.
4. If Ent schema changed: **`make generate-ent`** then tests + migrations workflow per **`AGENTS.md`**.
5. If only docs/spec: skip tests unless tooling requires otherwise.

## Anti-patterns

- Claiming tests pass without running them (`verification-before-completion` discipline).
- `go test` without `-race` on concurrency-sensitive packages (`internal/kafka`, `internal/pubsub`, services with goroutines).

## Related

- **API/OpenAPI**: [`flexprice-openapi-sdk`](../flexprice-openapi-sdk/SKILL.md)
- **Architecture docs**: [`architect`](../architect/SKILL.md)
