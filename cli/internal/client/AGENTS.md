---
layer: client
owns:
  - "cli/internal/client/**"
---

# Client Layer

> The only package that makes a network call. Every request — hand-written
> commands, spec-dispatched resource commands, and any future fixture
> engine — goes through here.
> Why → ../../decisions/0001-no-sdk-single-http-path.md,
> ../../decisions/0002-retry-only-idempotent-methods.md.

## Purpose

Issues HTTP requests, applies retry policy, redacts `--debug` output, and
normalizes every error response this API returns into one type. Nothing
outside this package touches `net/http` for talking to Flexprice.

## Key files
| File | Role |
|---|---|
| `client.go` | `Client`, `New`, `Do`, `retryPolicy`, `--debug` redaction |
| `errors.go` | `APIError`, `NewAPIError` — normalizes three response envelope shapes |

## Request path

```
Do(ctx, method, path, query, body)
  → build *http.Request, set x-api-key + User-Agent
  → retryablehttp with retryPolicy as CheckRetry
  → non-2xx → NewAPIError(status, body, method, path)
  → 2xx → raw bytes returned as-is
```

## Patterns to follow

- Every request goes through `Do`. Do not add a second function that issues
  requests, even for a "simple" one-off call.
- Always thread `ctx` through — no request without a context.
- `Options.Timeout` defaults to 30s (`DefaultTimeout`) if unset; do not
  construct a `Client` with an unbounded timeout.

## Invariants (must hold)

- `retryPolicy` retries only `GET`/`HEAD`/`PUT`/`DELETE`/`OPTIONS` on 5xx or
  a transport error, plus `429` for every method. Never add POST/PATCH to
  the retried set without solving the duplicate-write problem first (see
  Pitfalls below).
- `--debug` redaction (`safeKeys` in `client.go`) is an allowlist, not a
  denylist. A field not on the list is redacted even if it looks harmless —
  do not "fix" a redacted field you want to see by widening the list without
  checking whether it can carry free text.
- `New`'s `BaseURL` handling returns an error via `c.baseErr` rather than
  panicking or silently defaulting — checked lazily on the first `Do` call so
  `New` itself has no error return.

## Common pitfalls

- **Retrying a non-idempotent write can duplicate it.** The first
  implementation used `go-retryablehttp`'s default policy, which inspects
  only the status code, never the method — it retried `POST` exactly like
  `GET`. `CreateSubscriptionRequest` has no idempotency key at all, and where
  a body-level `idempotency_key` exists elsewhere, the server generates one
  containing a timestamp if the caller omits it, so it differs per attempt
  even though the retried body is byte-identical — server-side dedup does not
  save you. Fixed in the commit that added `retryPolicy`; full reasoning in
  [ADR 0002](../../decisions/0002-retry-only-idempotent-methods.md). If you
  are ever tempted to add POST to the retried methods, you are re-opening
  this bug.
- **A malformed `--base-url` used to fail deep in the HTTP stack** with a
  confusing `unsupported protocol scheme ""` rather than naming the actual
  cause. `New` now validates `Scheme`/`Host` are present and surfaces a clear
  error instead.

## Related layers

- `internal/spec` — builds the `*Request` this package's `Do` executes
- `internal/cmd` — the only caller of `client.New`/`Do`
- `internal/exitcode` — `APIError.ExitCode()` maps into these constants
