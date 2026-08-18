---
layer: config
owns:
  - "cli/internal/config/**"
---

# Config Layer

> Profiles and credential precedence. Holds no secrets — API keys live in
> `internal/keyring` and are referenced by `KeyRef`.
> Why → ../../decisions/0003-environment-scoped-profiles-no-live-flag.md.

## Purpose

Loads and saves `~/.flexprice/config.toml`, and resolves which credentials a
given invocation should use.

## Key files
| File | Role |
|---|---|
| `config.go` | `Config`, `Profile`, `Load`, `Save`, `Resolve`, `ProfileName` |
| `resolve.go` | `ResolveContext`, `Overrides`, `RuntimeContext` — the precedence chain |

## Credential precedence

```
ResolveContext(cfg, store, overrides)
  1. --api-key flag / FLEXPRICE_API_KEY env  → wins outright
  2. named or default profile's KeyRef        → looked up in the keyring
  3. --region / --base-url                    → resolves the base URL
  4. neither key nor region resolvable         → clear error naming the fix
```

## Patterns to follow

- `Profile` has no environment name and no live/test flag — this is
  deliberate, not an oversight. See
  [ADR 0003](../../decisions/0003-environment-scoped-profiles-no-live-flag.md)
  before you consider adding one back.
- Every error path in `resolve.go` must be checked for API-key leakage
  before merging a change — no `fmt.Errorf` may interpolate the key value.

## Invariants (must hold)

- `Save` writes atomically: temp file in the same directory, then
  `os.Rename`. Never revert this to `O_TRUNC` — a crash mid-write must not
  destroy the user's existing profiles.
- File and directory permissions are `0600`/`0700`, set explicitly even
  though they are typically no-ops under a normal umask (see Pitfalls).

## Common pitfalls

- **The `Chmod` calls that "harden" permissions after `MkdirAll`/`OpenFile`
  are provably no-ops under any realistic umask.** `MkdirAll(0o700)` and
  `OpenFile(..., 0o600)` already request modes with no group/other bits, and
  umask only clears bits — it cannot add them back. This was verified by
  removing the `Chmod` calls and confirming the permissions test still
  passed under umask `022` and umask `000`. They are kept anyway as defense
  against the requested mode being loosened later, not because they
  currently do anything. Do not read their presence as evidence that
  something would otherwise go wrong today.
- **`toml.Unmarshal` merges into an existing map rather than replacing it.**
  `Load` is safe today because it always starts from a fresh empty
  `Profiles` map, but if a future change reuses a non-empty `*Config` across
  calls, stale profiles would silently survive a `Load` that no longer
  mentions them.

## Related layers

- `internal/keyring` — where the credential a `Profile.KeyRef` points to
  actually lives
- `internal/cmd` — `runtimeContext` in `root.go` is the only caller of
  `ResolveContext`
