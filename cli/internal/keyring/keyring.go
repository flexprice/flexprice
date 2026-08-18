// Package keyring stores API keys. It prefers the OS keychain and falls back to
// an obfuscated file when no keychain is available — common in containers and WSL.
package keyring

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	oskeyring "github.com/zalando/go-keyring"
)

const service = "flexprice"

// probeTimeout bounds how long Open() waits on the OS keychain probe. Some
// backends (a partially configured Linux secret service with D-Bus reachable
// but no prompt agent to answer an unlock request) block indefinitely instead
// of failing fast, which would otherwise freeze the CLI at startup.
const probeTimeout = 2 * time.Second

// keychainOpTimeout bounds a real Set/Get/Delete call against the OS
// keychain, once Open has already picked OSKeyring as the backend. It is
// longer than probeTimeout on purpose: the probe only needs to detect an
// unusable backend fast at startup, but a real operation can legitimately
// involve the user noticing and answering an OS unlock/consent prompt (a
// macOS Keychain access dialog, a Linux polkit prompt) that a 2s bound would
// false-timeout on. It still guarantees the CLI never hangs forever on a
// truly stuck keychain (e.g. a Linux secret service with D-Bus reachable but
// no prompt agent to answer it) — the same failure mode probeTimeout exists
// to catch, just later in the call path.
//
// It is a var, not a const, so tests can shrink it to exercise the timeout
// path without a multi-second sleep; production code never reassigns it.
var keychainOpTimeout = 8 * time.Second

// Store is the credential backend. Name() is surfaced by whoami so the user can
// always tell where their key actually lives.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

// keychainSet, keychainGet, and keychainDelete are indirections over the
// zalando/go-keyring functions so tests can substitute a fake, deterministic
// backend (e.g. one that blocks past a timeout) without ever touching the
// real OS keychain.
var (
	keychainSet    = oskeyring.Set
	keychainGet    = oskeyring.Get
	keychainDelete = oskeyring.Delete
)

type OSKeyring struct{}

func (OSKeyring) Name() string { return "OS keychain" }

func (OSKeyring) Set(profile, key string) error {
	_, err := withTimeout(keychainOpTimeout, "set", func() (struct{}, error) {
		return struct{}{}, keychainSet(service, profile, key)
	})
	return err
}

func (OSKeyring) Get(profile string) (string, error) {
	return withTimeout(keychainOpTimeout, "get", func() (string, error) {
		return keychainGet(service, profile)
	})
}

func (OSKeyring) Delete(profile string) error {
	_, err := withTimeout(keychainOpTimeout, "delete", func() (struct{}, error) {
		return struct{}{}, keychainDelete(service, profile)
	})
	return err
}

// Open returns the OS keychain when it works, otherwise the file fallback.
// warn is non-empty when the fallback was selected and the caller has not opted
// in via FLEXPRICE_KEY_BACKEND=file; callers print it once.
func Open() (store Store, warn string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("locate home directory: %w", err)
	}
	fileDir := filepath.Join(home, ".flexprice", "keys")

	if os.Getenv("FLEXPRICE_KEY_BACKEND") == "file" {
		return &FileStore{Dir: fileDir}, "", nil
	}

	os_ := OSKeyring{}
	probeErr := probeKeychain()
	if probeErr == nil {
		return os_, "", nil
	}

	return &FileStore{Dir: fileDir},
		fmt.Sprintf("No OS keychain available (%v).\n"+
			"Storing your key in %s with mode 0600 instead.\n"+
			"Set FLEXPRICE_KEY_BACKEND=file to silence this warning.", probeErr, fileDir),
		nil
}

// probeKeychain is the only reliable availability check: on Linux the keychain
// fails at call time when libsecret or D-Bus is absent. It calls the raw
// keychain functions directly (not OSKeyring.Set/Delete) so its own
// probeTimeout bound applies once, rather than nesting inside each method's
// keychainOpTimeout bound and making the startup probe as slow as a real
// operation.
func probeKeychain() error {
	_, err := withTimeout(probeTimeout, "probe", func() (struct{}, error) {
		if err := keychainSet(service, service+".probe", "probe"); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, keychainDelete(service, service+".probe")
	})
	return err
}

// withTimeout runs fn on its own goroutine and bounds it to timeout, the
// pattern probeKeychain originally used inline. Both the probe and the real
// OSKeyring operations need this: a hung OS keychain call must never block
// the CLI forever, whether at startup (probe) or during login/logout/whoami
// (Set/Get/Delete). If fn does not return in time, the goroutine is
// abandoned (best-effort; it may complete later with nobody observing the
// result) and a clear, actionable error is returned instead of hanging.
func withTimeout[T any](timeout time.Duration, op string, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{v, err}
	}()

	select {
	case r := <-done:
		return r.val, r.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf(
			"OS keychain %s timed out after %s; it may be waiting on an OS unlock/consent prompt.\n"+
				"Set FLEXPRICE_KEY_BACKEND=file to bypass the OS keychain.", op, timeout)
	}
}
