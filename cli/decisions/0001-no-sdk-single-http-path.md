# 0001 — No SDK; one HTTP path through internal/client

## Context

An early draft of this CLI split commands into two groups: hand-written commands
(login, events, fixtures) calling the published `go-sdk/v2`, and spec-dispatched
resource commands building raw HTTP requests directly. That is two separate HTTP
stacks — two places that set the `x-api-key` header, two retry policies, two error
shapes — reviewed and rejected before any code was written, because it breaks the
one property this CLI depends on: that every request behaves identically no matter
which command issued it.

## Decision

There is exactly one way to make a request: `internal/client.Client.Do`
(`cli/internal/client/client.go:144`). Hand-written commands and spec-dispatched
resource commands both build a `*Request` and hand it to the same `Do` method. The
CLI does not import `go-sdk/v2` or any other Flexprice SDK.

`Do` owns:
- The `x-api-key` header, and the guarantee that `x-environment-id` is never sent
  (see [0003](0003-environment-scoped-profiles-no-live-flag.md)).
- Retry policy (see [0002](0002-retry-only-idempotent-methods.md)).
- A 30-second default request timeout.
- `--debug` dumps, redacted through an allowlist rather than a denylist.
- Turning a non-2xx response into a `*client.APIError`
  (`cli/internal/client/errors.go`), which every caller renders the same way.

## Consequences

A fix or a behavior change to auth, retries, timeouts, or error rendering happens
in one file and applies to every command, including the fixture engine's requests,
without anyone needing to remember to apply it twice. The cost is that
`internal/client` cannot be bypassed for a "quick" request — if a future command
needs something `Do` does not support, that capability is added to `Do`, not
worked around locally.
