# 0002 — Retry only idempotent methods; POST never retries on 5xx

## Context

The HTTP client's first implementation (commit `091fcc201`) used
`go-retryablehttp`'s default retry policy, which retries a request up to three
times on any 5xx status or transport error. That policy inspects only the
response status — never the request method — so it retried `POST` identically to
`GET`.

On a billing API that is unsafe. A 502 raised after the server has already
committed a write is indistinguishable, from the client's point of view, from one
raised before the write happened. Retrying `POST /subscriptions` after a
post-commit failure can create a second subscription that bills a real customer.
Checked directly against the API this CLI talks to (fix commit `573e85140`):
`CreateSubscriptionRequest` has no idempotency field at all, and on the endpoints
that do accept a body-level `idempotency_key`, the server generates one
containing the current timestamp when the caller omits it — which differs on
every retry attempt even though `go-retryablehttp` resends a byte-identical body,
so server-side deduplication does not help either.

## Decision

`internal/client.retryPolicy` (`cli/internal/client/client.go:58`) retries a
request only when its method is in `idempotentMethods` — `GET`, `HEAD`, `PUT`,
`DELETE`, `OPTIONS` — or when the response is `429 Too Many Requests`, which is
retried for every method because it explicitly means the server did not process
the request. `POST` and `PATCH` never retry on a 5xx or a transport error.

## Consequences

A `POST` that fails with a 5xx surfaces to the user as a single failed attempt
rather than silently succeeding after duplicating a write. The cost is that a
transient 502 on `POST /events` — the highest-volume write this CLI makes — is
not automatically retried; a future high-volume ingestion path (`events bulk`,
`events simulate`) needs its own application-level retry-with-dedup logic if that
matters, rather than getting it for free from the transport.
