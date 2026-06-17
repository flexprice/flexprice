---
name: godev
description: >-
  FlexPrice Go fmt, vet, race tests, make test. Trigger: godev, run tests, go vet.
---

# **`godev`** — Go dev loop

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
- Single-package stress: `go test -race ./internal/ee/service -run TestName`

## When changing code

1. Run **`gofmt -w`** on touched packages (or whole repo if unsure).
2. Run **`go vet ./...`** or at minimum `go vet` on affected packages.
3. Run **`go test -race`** for packages you edited plus immediate dependents.
4. If Ent schema changed: **`make generate-ent`** then tests + migrations workflow per **`AGENTS.md`**.
5. If only docs/spec: skip tests unless tooling requires otherwise.

## Anti-patterns

- Claiming tests pass without running them (`verification-before-completion` discipline).
- `go test` without `-race` on concurrency-sensitive packages (`internal/kafka`, `internal/pubsub`, services with goroutines).

## Related skills

- [`openapi`](../openapi/SKILL.md)
- [`arch`](../arch/SKILL.md)
