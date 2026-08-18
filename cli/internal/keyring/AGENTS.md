---
layer: keyring
owns:
  - "cli/internal/keyring/**"
---

# Keyring Layer

> API key storage. Prefers the OS keychain, falls back to an encrypted file
> when none is available.

## Purpose

`config.Profile.KeyRef` points here. This package is the only one that
touches an OS credential store or writes key material to disk.

## Key files
| File | Role |
|---|---|
| `keyring.go` | `Store` interface, `OSKeyring`, `Open` (probes and picks a backend) |
| `file.go` | `FileStore` — AES-GCM encrypted file fallback |

## Backend selection

```
Open()
  → FLEXPRICE_KEY_BACKEND=file set?  → FileStore, no probe
  → else: probe the OS keychain (bounded, see Pitfalls)
  → probe succeeds → OSKeyring
  → probe fails or times out → FileStore, with a warning string for the caller to print
```

## Patterns to follow

- Never call the OS keychain without going through `Open`'s bounded probe
  first. A direct, unprobed call can hang indefinitely in a headless or
  no-keychain environment (see Pitfalls) — this is not hypothetical, it has
  happened in this repo's own development.
- Testing this package: `FileStore` tests must construct `&FileStore{Dir:
  t.TempDir()}` directly and never call `Open()` — calling `Open()` in a
  test risks the same real-keychain interaction described below.

## Invariants (must hold)

- The keychain probe in `Open` is bounded (`probeTimeout`, 2s) — this exists
  specifically because the Linux secret-service backend calls
  `svc.Unlock(...)` over D-Bus, which can block indefinitely when D-Bus is
  reachable but no prompt agent exists. Never remove this timeout.
- `FileStore.Set` writes atomically (temp file + rename) for the same reason
  `config.Save` does — two processes racing on the same profile must not
  produce a corrupt file.

## Common pitfalls

- **Running the real CLI binary against the real OS keychain in an
  automated or agent context is genuinely dangerous, not just noisy.**
  During this CLI's own development, an agent running the built binary
  without `FLEXPRICE_KEY_BACKEND=file` set triggered a live macOS "Keychain
  Not Found" system dialog on the developer's actual screen, offering a
  "Reset To Defaults" button that would have reset the user's real default
  keychain — a destructive, unrelated system action. **Any test, script, or
  agent invocation of the built `flexprice` binary that touches credential
  storage must set `FLEXPRICE_KEY_BACKEND=file` and ideally a scratch
  `HOME`.** This is not a style preference; treat it as a hard rule for
  anyone automating this CLI.
- **The encrypted file fallback is not protection against a local
  attacker.** Its AES-GCM key is derived from the hostname and directory
  path — stable, not secret. It stops casual disclosure (an accidental
  `cat`, a backup, shoulder-surfing); the real control is the `0600` file
  mode, not the encryption. Do not describe this to a user as "your key is
  encrypted" in a way that implies more than that.
- **A hostname change makes a stored file-backed key undecryptable.**
  `Get`'s error message names the likely cause and the fix
  (`flexprice login` again) rather than surfacing a bare crypto error —
  keep that framing if you touch this path.
- **The bounded probe used to protect only itself, not the real credential
  operations — a real gap, not a hypothetical.** `Open()`'s 2-second
  `probeTimeout` wrapped only the throwaway probe; once it returned
  `OSKeyring`, the real `Set`/`Get`/`Delete` calls `login`/`logout`/`whoami`
  actually make were direct, unwrapped calls into `zalando/go-keyring` — if
  the keychain session expired or triggered a fresh unlock prompt between
  the probe and the real call, the CLI could still hang indefinitely, which
  is the exact incident the probe exists to prevent. `withTimeout` (a
  generalized version of the probe's own goroutine+timeout pattern) now
  wraps `Set`/`Get`/`Delete` too, at `keychainOpTimeout` (8s, longer than the
  2s probe bound, since a real operation can legitimately involve the user
  responding to an OS prompt). If you add a new method to `OSKeyring`, wrap
  it with `withTimeout` too — an unwrapped call reopens this gap.

## Related layers

- `internal/config` — `Profile.KeyRef` names which backend/entry a profile
  uses
- `internal/cmd` — `runtimeContext` in `root.go` is the only caller of `Open`
